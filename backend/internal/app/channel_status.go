package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	channelStatusRefreshInterval = 5 * time.Minute
	channelStatusRefreshTimeout  = 60 * time.Second
	channelStatusWindowDuration  = 7 * 24 * time.Hour
)

type ChannelStatusRunner struct {
	app  *App
	mu   sync.Mutex
	stop chan struct{}
	done chan struct{}
}

type channelStatusResponse struct {
	Items       []channelStatusItem `json:"items"`
	RefreshedAt *string             `json:"refreshed_at,omitempty"`
}

type channelStatusItem struct {
	ID                        string   `json:"id"`
	Name                      string   `json:"name"`
	Description               string   `json:"description,omitempty"`
	Enabled                   bool     `json:"enabled"`
	AccountTypes              []string `json:"account_types"`
	Status                    string   `json:"status"`
	Available                 bool     `json:"available"`
	AccountCount              int      `json:"account_count"`
	AvailableAccounts         int      `json:"available_accounts"`
	DisabledAccounts          int      `json:"disabled_accounts"`
	ErrorAccounts             int      `json:"error_accounts"`
	QuotaExhaustedAccounts    int      `json:"quota_exhausted_accounts"`
	StatusCode                *int     `json:"status_code,omitempty"`
	PrimaryRemainingPercent   *int     `json:"primary_remaining_percent,omitempty"`
	SecondaryRemainingPercent *int     `json:"secondary_remaining_percent,omitempty"`
	WindowStartAt             string   `json:"window_start_at"`
	WindowEndAt               string   `json:"window_end_at"`
	WindowRecords             int      `json:"window_records"`
	WindowSuccessRecords      int      `json:"window_success_records"`
	WindowFailedRecords       int      `json:"window_failed_records"`
	WindowCostUSD             float64  `json:"window_cost_usd"`
	LastCheckedAt             *string  `json:"last_checked_at,omitempty"`
	LastHealthyAt             *string  `json:"last_healthy_at,omitempty"`
	LastError                 *string  `json:"last_error,omitempty"`
	RefreshedAt               string   `json:"refreshed_at"`
}

func NewChannelStatusRunner(app *App) *ChannelStatusRunner {
	return &ChannelStatusRunner{app: app}
}

func (r *ChannelStatusRunner) Start() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.done != nil {
		select {
		case <-r.done:
		default:
			return
		}
	}
	r.stop = make(chan struct{})
	r.done = make(chan struct{})
	go r.loop()
}

func (r *ChannelStatusRunner) Stop() {
	r.mu.Lock()
	stop := r.stop
	done := r.done
	if stop == nil || done == nil {
		r.mu.Unlock()
		return
	}
	select {
	case <-stop:
	default:
		close(stop)
	}
	r.mu.Unlock()
	<-done
}

func (r *ChannelStatusRunner) RefreshAsync() {
	go r.refresh("event")
}

func (a *App) refreshChannelStatusAfterChange() {
	if a.channelStatus != nil {
		a.channelStatus.RefreshAsync()
	}
}

func (r *ChannelStatusRunner) loop() {
	defer func() {
		r.mu.Lock()
		if r.done != nil {
			close(r.done)
		}
		r.mu.Unlock()
	}()

	r.refresh("startup")
	ticker := time.NewTicker(channelStatusRefreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-r.stop:
			return
		case <-ticker.C:
			r.refresh("ticker")
		}
	}
}

func (r *ChannelStatusRunner) refresh(reason string) {
	ctx, cancel := context.WithTimeout(context.Background(), channelStatusRefreshTimeout)
	defer cancel()
	if err := r.app.refreshChannelStatusSnapshot(ctx); err != nil {
		log.Printf("refresh channel status snapshots failed (%s): %v", reason, err)
	}
}

func (a *App) handleChannelStatus(w http.ResponseWriter, r *http.Request) error {
	if err := requireMethod(r, http.MethodGet); err != nil {
		return err
	}
	if _, err := a.currentUser(r.Context(), r); err != nil {
		return err
	}
	items, refreshedAt, err := a.listChannelStatusSnapshots(r.Context())
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, channelStatusResponse{Items: items, RefreshedAt: refreshedAt})
	return nil
}

func (a *App) refreshChannelStatusSnapshot(ctx context.Context) error {
	var status authPoolStatus
	if err := a.authPoolPluginRequest(ctx, http.MethodGet, "/status", nil, &status); err != nil {
		return err
	}
	accounts, err := a.listKeeperAccounts(ctx)
	if err != nil {
		return err
	}
	records, err := a.channelStatusWindowRecords(ctx, time.Now().In(appTimeLocation).Add(-channelStatusWindowDuration))
	if err != nil {
		return err
	}
	prices, err := a.priceMap(ctx)
	if err != nil {
		return err
	}
	items := buildChannelStatusItems(status.Pools, accounts, records, prices, time.Now().In(appTimeLocation))
	return a.replaceChannelStatusSnapshots(ctx, items)
}

func (a *App) listChannelStatusSnapshots(ctx context.Context) ([]channelStatusItem, *string, error) {
	rows, err := a.db.QueryContext(ctx, `
		SELECT pool_id, pool_name, description, enabled, account_types, account_count,
		       available_accounts, disabled_accounts, error_accounts, quota_exhausted_accounts,
		       status, available, status_code, primary_remaining_percent, secondary_remaining_percent,
		       CAST(window_start_at AS TEXT), CAST(window_end_at AS TEXT), window_records,
		       window_success_records, window_failed_records, window_cost_usd,
		       CAST(last_checked_at AS TEXT), CAST(last_healthy_at AS TEXT), last_error,
		       CAST(refreshed_at AS TEXT)
		FROM channel_status_snapshots
		ORDER BY pool_name, pool_id
	`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	items := []channelStatusItem{}
	var latest *time.Time
	for rows.Next() {
		item, refreshed, err := scanChannelStatusItem(rows)
		if err != nil {
			return nil, nil, err
		}
		items = append(items, item)
		if refreshed != nil && (latest == nil || refreshed.After(*latest)) {
			latest = refreshed
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	return items, apiDateTimePtr(latest), nil
}

func scanChannelStatusItem(scanner interface{ Scan(dest ...any) error }) (channelStatusItem, *time.Time, error) {
	var item channelStatusItem
	var description, accountTypesJSON, windowStart, windowEnd, lastChecked, lastHealthy, lastError, refreshedAt sql.NullString
	var statusCode, primaryRemaining, secondaryRemaining sql.NullInt64
	if err := scanner.Scan(
		&item.ID, &item.Name, &description, &item.Enabled, &accountTypesJSON, &item.AccountCount,
		&item.AvailableAccounts, &item.DisabledAccounts, &item.ErrorAccounts, &item.QuotaExhaustedAccounts,
		&item.Status, &item.Available, &statusCode, &primaryRemaining, &secondaryRemaining,
		&windowStart, &windowEnd, &item.WindowRecords, &item.WindowSuccessRecords, &item.WindowFailedRecords,
		&item.WindowCostUSD, &lastChecked, &lastHealthy, &lastError, &refreshedAt,
	); err != nil {
		return channelStatusItem{}, nil, err
	}
	item.Description = strings.TrimSpace(description.String)
	item.AccountTypes = parseJSONStringSlice(accountTypesJSON.String)
	item.StatusCode = nullableInt64(statusCode)
	item.PrimaryRemainingPercent = nullableInt64(primaryRemaining)
	item.SecondaryRemainingPercent = nullableInt64(secondaryRemaining)
	item.WindowStartAt = apiDateTimeFromDBString(windowStart.String)
	item.WindowEndAt = apiDateTimeFromDBString(windowEnd.String)
	item.LastCheckedAt = apiDateTimePtrFromDBString(lastChecked.String)
	item.LastHealthyAt = apiDateTimePtrFromDBString(lastHealthy.String)
	item.LastError = nullableCleanString(lastError)
	item.RefreshedAt = apiDateTimeFromDBString(refreshedAt.String)
	if refreshed, ok := parseDBTime(refreshedAt.String); ok {
		return item, &refreshed, nil
	}
	return item, nil, nil
}

func (a *App) replaceChannelStatusSnapshots(ctx context.Context, items []channelStatusItem) error {
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM channel_status_snapshots`); err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO channel_status_snapshots (
			pool_id, pool_name, description, enabled, account_types, account_count,
			available_accounts, disabled_accounts, error_accounts, quota_exhausted_accounts,
			status, available, status_code, primary_remaining_percent, secondary_remaining_percent,
			window_start_at, window_end_at, window_records, window_success_records,
			window_failed_records, window_cost_usd, last_checked_at, last_healthy_at,
			last_error, refreshed_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	now := dbTime(time.Now())
	for _, item := range items {
		accountTypes, _ := json.Marshal(item.AccountTypes)
		if _, err := stmt.ExecContext(ctx,
			item.ID, item.Name, item.Description, item.Enabled, string(accountTypes), item.AccountCount,
			item.AvailableAccounts, item.DisabledAccounts, item.ErrorAccounts, item.QuotaExhaustedAccounts,
			item.Status, item.Available, item.StatusCode, item.PrimaryRemainingPercent, item.SecondaryRemainingPercent,
			item.WindowStartAt, item.WindowEndAt, item.WindowRecords, item.WindowSuccessRecords,
			item.WindowFailedRecords, item.WindowCostUSD, item.LastCheckedAt, item.LastHealthyAt,
			item.LastError, item.RefreshedAt, now,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (a *App) channelStatusWindowRecords(ctx context.Context, start time.Time) ([]UsageRecord, error) {
	rows, err := a.db.QueryContext(ctx, `SELECT id, CAST(timestamp AS TEXT), usage_username, api_key_description, provider, model, reasoning_effort, endpoint, source,
		source_account, request_id, auth, auth_index, latency_ms, ttft_ms, failed, input_tokens, output_tokens, cached_tokens,
		cache_read_tokens, cache_creation_tokens, reasoning_tokens, total_tokens, dedupe_key, raw_json
		FROM usage_records WHERE timestamp >= ?`, dbTime(start))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanUsageRecords(rows)
}

func buildChannelStatusItems(pools []authPool, accounts []keeperAccount, records []UsageRecord, prices map[[2]string]ModelPrice, now time.Time) []channelStatusItem {
	items := make([]channelStatusItem, 0, len(pools))
	windowStart := now.Add(-channelStatusWindowDuration)
	for _, pool := range pools {
		members := channelPoolAccounts(pool, accounts)
		memberKeys := channelMemberKeys(members)
		item := channelStatusItem{
			ID:            strings.TrimSpace(pool.ID),
			Name:          strings.TrimSpace(pool.Name),
			Description:   strings.TrimSpace(pool.Description),
			Enabled:       pool.Enabled,
			AccountTypes:  normalizedStringList(pool.AccountTypes),
			WindowStartAt: dbTime(windowStart),
			WindowEndAt:   dbTime(now),
			RefreshedAt:   dbTime(now),
		}
		if item.Name == "" {
			item.Name = item.ID
		}
		applyChannelAccountStats(&item, members)
		applyChannelUsageStats(&item, memberKeys, records, prices)
		items = append(items, item)
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Name == items[j].Name {
			return items[i].ID < items[j].ID
		}
		return items[i].Name < items[j].Name
	})
	return items
}

func applyChannelAccountStats(item *channelStatusItem, accounts []keeperAccount) {
	item.AccountCount = len(accounts)
	if !item.Enabled {
		item.Status = "disabled"
		item.Available = false
	}
	var lastChecked, lastHealthy *time.Time
	var primaryRemaining, secondaryRemaining channelRemainingAccumulator
	for _, account := range accounts {
		status, available := channelAccountStatus(account)
		if available {
			item.AvailableAccounts++
		}
		switch status {
		case "disabled":
			item.DisabledAccounts++
		case "error":
			item.ErrorAccounts++
		case "quota_exhausted":
			item.QuotaExhaustedAccounts++
		}
		if account.LastStatusCode != nil && (item.StatusCode == nil || *account.LastStatusCode > *item.StatusCode) {
			code := *account.LastStatusCode
			item.StatusCode = &code
		}
		primaryRemaining.add(account.PrimaryUsedPercent)
		secondaryRemaining.add(account.SecondaryUsedPercent)
		lastChecked = maxTimePtr(lastChecked, account.LastCheckedAt)
		lastHealthy = maxTimePtr(lastHealthy, account.LastHealthyAt)
	}
	item.PrimaryRemainingPercent = primaryRemaining.percent(len(accounts))
	item.SecondaryRemainingPercent = secondaryRemaining.percent(len(accounts))
	item.LastCheckedAt = apiDateTimePtr(lastChecked)
	item.LastHealthyAt = apiDateTimePtr(lastHealthy)
	if item.Status != "" {
		return
	}
	item.Status, item.Available = channelPoolStatus(item)
}

func applyChannelUsageStats(item *channelStatusItem, memberKeys map[string]bool, records []UsageRecord, prices map[[2]string]ModelPrice) {
	if len(memberKeys) == 0 {
		return
	}
	for _, record := range records {
		if !usageRecordMatchesChannel(record, memberKeys) {
			continue
		}
		item.WindowRecords++
		if record.Failed {
			item.WindowFailedRecords++
		} else {
			item.WindowSuccessRecords++
		}
		cost, _ := recordCost(record, prices)
		item.WindowCostUSD = mathRound(item.WindowCostUSD+cost, 8)
	}
}

func channelPoolStatus(item *channelStatusItem) (string, bool) {
	if item.AccountCount == 0 {
		return "empty", false
	}
	if item.AvailableAccounts == item.AccountCount {
		return "normal", true
	}
	if item.AvailableAccounts > 0 {
		return "degraded", true
	}
	if item.QuotaExhaustedAccounts == item.AccountCount {
		return "quota_exhausted", false
	}
	if item.DisabledAccounts == item.AccountCount {
		return "disabled", false
	}
	return "error", false
}

func channelAccountStatus(account keeperAccount) (string, bool) {
	if account.Disabled {
		return "disabled", false
	}
	if account.LastStatusCode != nil && *account.LastStatusCode >= 400 {
		return "error", false
	}
	if isQuotaExhaustedPercent(account.PrimaryUsedPercent) || isQuotaExhaustedPercent(account.SecondaryUsedPercent) {
		return "quota_exhausted", false
	}
	return "normal", true
}

func channelPoolAccounts(pool authPool, accounts []keeperAccount) []keeperAccount {
	manualIDs := normalizedLookup(pool.AuthIDs)
	typeIDs := normalizedLookup(pool.AccountTypes)
	members := []keeperAccount{}
	seen := map[string]bool{}
	for _, account := range accounts {
		if channelAccountMatchesPool(account, manualIDs, typeIDs) {
			key := strings.TrimSpace(account.Name)
			if key == "" || seen[key] {
				continue
			}
			seen[key] = true
			members = append(members, account)
		}
	}
	return members
}

func channelAccountMatchesPool(account keeperAccount, manualIDs map[string]bool, typeIDs map[string]bool) bool {
	if len(manualIDs) > 0 {
		for _, value := range []string{account.Name, stringPtrValue(account.AuthIndex), stringPtrValue(account.Email)} {
			if manualIDs[normalizeChannelKey(value)] {
				return true
			}
		}
	}
	if len(typeIDs) > 0 && typeIDs[normalizeChannelKey(stringPtrValue(account.AccountType))] {
		return true
	}
	return false
}

func channelMemberKeys(accounts []keeperAccount) map[string]bool {
	keys := map[string]bool{}
	for _, account := range accounts {
		for _, value := range []string{account.Name, stringPtrValue(account.AuthIndex), stringPtrValue(account.Email)} {
			key := normalizeChannelKey(value)
			if key != "" {
				keys[key] = true
			}
		}
	}
	return keys
}

func usageRecordMatchesChannel(record UsageRecord, memberKeys map[string]bool) bool {
	for _, value := range usageRecordChannelKeys(record) {
		if memberKeys[normalizeChannelKey(value)] {
			return true
		}
	}
	return false
}

func usageRecordChannelKeys(record UsageRecord) []string {
	keys := []string{
		stringPtrValue(record.SourceAccount),
		stringPtrValue(record.AuthIndex),
		stringPtrValue(record.Auth),
	}
	for _, field := range []string{"auth_index", "authIndex", "auth_name", "authName", "account_id", "accountId", "source_account", "sourceAccount", "email"} {
		keys = append(keys, stringPtrValue(rawJSONStringField(record.RawJSON, field)))
	}
	return keys
}

func isQuotaExhaustedPercent(value *int) bool {
	return value != nil && *value >= 100
}

type channelRemainingAccumulator struct {
	total int
	known bool
}

func (a *channelRemainingAccumulator) add(used *int) {
	if used == nil {
		a.total += 100
		return
	}
	a.known = true
	a.total += remainingPercentValue(*used)
}

func (a channelRemainingAccumulator) percent(accountCount int) *int {
	if accountCount <= 0 || !a.known {
		return nil
	}
	value := int(float64(a.total)/float64(accountCount) + 0.5)
	return &value
}

func remainingPercentValue(used int) int {
	remaining := 100 - used
	if remaining < 0 {
		return 0
	}
	if remaining > 100 {
		return 100
	}
	return remaining
}

func remainingPercent(used *int) *int {
	if used == nil {
		return nil
	}
	remaining := remainingPercentValue(*used)
	return &remaining
}

func maxTimePtr(current *time.Time, next *time.Time) *time.Time {
	if next == nil {
		return current
	}
	if current == nil || next.After(*current) {
		value := *next
		return &value
	}
	return current
}

func normalizedLookup(values []string) map[string]bool {
	lookup := map[string]bool{}
	for _, value := range values {
		key := normalizeChannelKey(value)
		if key != "" {
			lookup[key] = true
		}
	}
	return lookup
}

func normalizedStringList(values []string) []string {
	seen := map[string]bool{}
	result := []string{}
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		key := normalizeChannelKey(trimmed)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, trimmed)
	}
	sort.Strings(result)
	return result
}

func normalizeChannelKey(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func stringPtrValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func parseJSONStringSlice(value string) []string {
	if strings.TrimSpace(value) == "" {
		return []string{}
	}
	var items []string
	if err := json.Unmarshal([]byte(value), &items); err != nil {
		return []string{}
	}
	return normalizedStringList(items)
}

func nullableInt64(value sql.NullInt64) *int {
	if !value.Valid {
		return nil
	}
	parsed := int(value.Int64)
	return &parsed
}

func nullableCleanString(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	cleaned := strings.TrimSpace(value.String)
	if cleaned == "" {
		return nil
	}
	return &cleaned
}

func apiDateTimeFromDBString(value string) string {
	if parsed, ok := parseDBTime(value); ok {
		return apiDateTime(parsed)
	}
	return ""
}

func apiDateTimePtrFromDBString(value string) *string {
	formatted := apiDateTimeFromDBString(value)
	if formatted == "" {
		return nil
	}
	return &formatted
}
