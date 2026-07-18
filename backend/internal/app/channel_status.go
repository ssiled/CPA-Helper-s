package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	channelStatusRefreshInterval    = 5 * time.Minute
	channelStatusRefreshTimeout     = 60 * time.Second
	channelStatusWindowDuration     = 7 * 24 * time.Hour
	channelStatusRecentRequestCount = 360
)

type ChannelStatusRunner struct {
	app             *App
	mu              sync.Mutex
	stop            chan struct{}
	done            chan struct{}
	refreshRequests chan string
	usageCache      *channelStatusUsageCache
}

type channelStatusResponse struct {
	Items       []channelStatusItem `json:"items"`
	RefreshedAt *string             `json:"refreshed_at,omitempty"`
}

type channelStatusItem struct {
	ID                        string                       `json:"id"`
	Name                      string                       `json:"name"`
	Description               string                       `json:"description,omitempty"`
	Enabled                   bool                         `json:"enabled"`
	AccountTypes              []string                     `json:"account_types"`
	Status                    string                       `json:"status"`
	Available                 bool                         `json:"available"`
	AccountCount              int                          `json:"account_count"`
	AvailableAccounts         int                          `json:"available_accounts"`
	DisabledAccounts          int                          `json:"disabled_accounts"`
	ErrorAccounts             int                          `json:"error_accounts"`
	QuotaExhaustedAccounts    int                          `json:"quota_exhausted_accounts"`
	StatusCode                *int                         `json:"status_code,omitempty"`
	PrimaryRemainingPercent   *int                         `json:"primary_remaining_percent,omitempty"`
	SecondaryRemainingPercent *int                         `json:"secondary_remaining_percent,omitempty"`
	WindowStartAt             string                       `json:"window_start_at"`
	WindowEndAt               string                       `json:"window_end_at"`
	WindowRecords             int                          `json:"window_records"`
	WindowSuccessRecords      int                          `json:"window_success_records"`
	WindowFailedRecords       int                          `json:"window_failed_records"`
	WindowCostUSD             float64                      `json:"window_cost_usd"`
	RecentWindowStartAt       string                       `json:"recent_window_start_at"`
	RecentWindowEndAt         string                       `json:"recent_window_end_at"`
	RecentRequests            []channelStatusRecentRequest `json:"recent_requests"`
	LastCheckedAt             *string                      `json:"last_checked_at,omitempty"`
	LastHealthyAt             *string                      `json:"last_healthy_at,omitempty"`
	LastError                 *string                      `json:"last_error,omitempty"`
	RefreshedAt               string                       `json:"refreshed_at"`
}

type channelStatusRecentRequest struct {
	ID        int    `json:"-"`
	Timestamp string `json:"timestamp"`
	Failed    bool   `json:"failed"`
}

type channelStatusUsageAggregate struct {
	Records        int
	SuccessRecords int
	FailedRecords  int
	CostUSD        float64
}

type channelStatusUsageCache struct {
	MembershipSignature string
	PriceSignature      string
	WindowStart         time.Time
	LastUsageID         int
	Aggregates          map[string]channelStatusUsageAggregate
	Recent              map[string]channelStatusRecentRequestBuffer
}

type channelStatusRecentRequestBuffer struct {
	items [channelStatusRecentRequestCount]channelStatusRecentRequest
	next  int
	size  int
}

func (b *channelStatusRecentRequestBuffer) add(record UsageRecord) {
	if record.Timestamp.IsZero() {
		return
	}
	b.items[b.next] = channelStatusRecentRequest{ID: record.ID, Timestamp: dbTime(record.Timestamp), Failed: record.Failed}
	b.next = (b.next + 1) % channelStatusRecentRequestCount
	if b.size < channelStatusRecentRequestCount {
		b.size++
	}
}

func (b channelStatusRecentRequestBuffer) rawSnapshot() []channelStatusRecentRequest {
	requests := make([]channelStatusRecentRequest, b.size)
	start := (b.next - b.size + channelStatusRecentRequestCount) % channelStatusRecentRequestCount
	for index := 0; index < b.size; index++ {
		requests[index] = b.items[(start+index)%channelStatusRecentRequestCount]
	}
	return requests
}

func (b channelStatusRecentRequestBuffer) snapshot() []channelStatusRecentRequest {
	requests := make([]channelStatusRecentRequest, channelStatusRecentRequestCount)
	offset := channelStatusRecentRequestCount - b.size
	copy(requests[offset:], b.rawSnapshot())
	return requests
}

func (b *channelStatusRecentRequestBuffer) replace(requests []channelStatusRecentRequest) {
	*b = channelStatusRecentRequestBuffer{}
	if len(requests) > channelStatusRecentRequestCount {
		requests = requests[len(requests)-channelStatusRecentRequestCount:]
	}
	for _, request := range requests {
		if strings.TrimSpace(request.Timestamp) == "" {
			continue
		}
		b.items[b.next] = request
		b.next = (b.next + 1) % channelStatusRecentRequestCount
		if b.size < channelStatusRecentRequestCount {
			b.size++
		}
	}
}

func (b *channelStatusRecentRequestBuffer) merge(records []UsageRecord) {
	if len(records) == 0 {
		return
	}
	requests := b.rawSnapshot()
	for _, record := range records {
		if record.Timestamp.IsZero() {
			continue
		}
		requests = append(requests, channelStatusRecentRequest{ID: record.ID, Timestamp: dbTime(record.Timestamp), Failed: record.Failed})
	}
	sort.SliceStable(requests, func(i, j int) bool {
		left, leftOK := parseDBTime(requests[i].Timestamp)
		right, rightOK := parseDBTime(requests[j].Timestamp)
		if leftOK && rightOK && !left.Equal(right) {
			return left.Before(right)
		}
		return requests[i].ID < requests[j].ID
	})
	b.replace(requests)
}

func (b *channelStatusRecentRequestBuffer) pruneBefore(cutoff time.Time) {
	requests := b.rawSnapshot()
	filtered := requests[:0]
	for _, request := range requests {
		if timestamp, ok := parseDBTime(request.Timestamp); ok && !timestamp.Before(cutoff) {
			filtered = append(filtered, request)
		}
	}
	b.replace(filtered)
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
	r.refreshRequests = make(chan string, 1)
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
	r.mu.Lock()
	requests := r.refreshRequests
	done := r.done
	stop := r.stop
	r.mu.Unlock()
	if requests == nil || done == nil || stop == nil {
		return
	}
	select {
	case <-done:
		return
	case <-stop:
		return
	default:
	}
	select {
	case requests <- "event":
	default:
	}
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
		case reason := <-r.refreshRequests:
			r.refresh(reason)
		case <-ticker.C:
			r.refresh("ticker")
		}
	}
}

func (r *ChannelStatusRunner) refresh(reason string) {
	parent := r.app.backgroundCtx
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, channelStatusRefreshTimeout)
	defer cancel()
	cache, err := r.app.refreshChannelStatusSnapshot(ctx, r.usageCache)
	if err != nil {
		log.Printf("refresh channel status snapshots failed (%s): %v", reason, err)
		return
	}
	r.usageCache = cache
}

func (a *App) handleChannelStatus(w http.ResponseWriter, r *http.Request) error {
	if err := requireMethod(r, http.MethodGet); err != nil {
		return err
	}
	user, err := a.currentUser(r.Context(), r)
	if err != nil {
		return err
	}
	items, refreshedAt, err := a.listChannelStatusSnapshots(r.Context(), user)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, channelStatusResponse{Items: items, RefreshedAt: refreshedAt})
	return nil
}

func (a *App) refreshChannelStatusSnapshot(ctx context.Context, cache *channelStatusUsageCache) (*channelStatusUsageCache, error) {
	now := time.Now().In(appTimeLocation)
	var status authPoolStatus
	if err := a.authPoolPluginRequest(ctx, http.MethodGet, "/status", nil, &status); err != nil {
		return cache, err
	}
	accounts, err := a.listAuthPoolAccounts(ctx)
	if err != nil {
		return cache, err
	}
	// Reconcile dynamic pool membership during the periodic status refresh so
	// accounts added directly to CPA become eligible without a UI action.
	a.syncAuthPoolResolvedAuthIDsAsync()
	prices, err := a.priceMap(ctx)
	if err != nil {
		return cache, err
	}
	membershipSignature := channelStatusMembershipSignature(status.Pools, accounts)
	priceSignature := channelStatusPriceSignature(prices)
	latestUsageID, err := a.channelStatusLatestUsageID(ctx)
	if err != nil {
		return cache, err
	}
	windowStart := now.Add(-channelStatusWindowDuration)
	if !channelStatusCacheReusable(cache, membershipSignature, priceSignature, windowStart, latestUsageID) {
		records, err := a.channelStatusWindowRecordsBetween(ctx, windowStart, now)
		if err != nil {
			return cache, err
		}
		items, err := buildChannelStatusItemsContext(ctx, status.Pools, accounts, records, prices, now)
		if err != nil {
			return cache, err
		}
		nextCache := newChannelStatusUsageCache(items, membershipSignature, priceSignature, windowStart, latestUsageID)
		if err := a.replaceChannelStatusSnapshots(ctx, items); err != nil {
			return cache, err
		}
		return nextCache, nil
	}

	expired, err := a.channelStatusWindowRecordsRange(ctx, cache.WindowStart, windowStart, cache.LastUsageID)
	if err != nil {
		return cache, err
	}
	added, err := a.channelStatusUsageRecordsAfterID(ctx, cache.LastUsageID, windowStart, now)
	if err != nil {
		return cache, err
	}
	items, nextCache, err := buildIncrementalChannelStatusItemsContext(ctx, status.Pools, accounts, expired, added, prices, now, cache)
	if err != nil {
		return cache, err
	}
	nextCache.MembershipSignature = membershipSignature
	nextCache.PriceSignature = priceSignature
	nextCache.WindowStart = windowStart
	nextCache.LastUsageID = latestUsageID
	if err := a.replaceChannelStatusSnapshots(ctx, items); err != nil {
		return cache, err
	}
	return nextCache, nil
}

func (a *App) listChannelStatusSnapshots(ctx context.Context, user *AuthUser) ([]channelStatusItem, *string, error) {
	rows, err := a.db.QueryContext(ctx, `
		SELECT pool_id, pool_name, description, enabled, account_types, account_count,
		       available_accounts, disabled_accounts, error_accounts, quota_exhausted_accounts,
		       status, available, status_code, primary_remaining_percent, secondary_remaining_percent,
		       CAST(window_start_at AS TEXT), CAST(window_end_at AS TEXT), window_records,
		       window_success_records, window_failed_records, window_cost_usd,
		       CAST(recent_window_start_at AS TEXT), CAST(recent_window_end_at AS TEXT), recent_buckets_json,
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
	if user == nil || user.IsAdmin {
		return items, apiDateTimePtr(latest), nil
	}
	localPools, err := a.localAuthPools(ctx)
	if err != nil {
		return nil, nil, err
	}
	return channelStatusItemsVisibleToUser(items, localPools, user), apiDateTimePtr(latest), nil
}

func channelStatusItemsVisibleToUser(items []channelStatusItem, pools []authPool, user *AuthUser) []channelStatusItem {
	if user == nil || user.IsAdmin {
		return items
	}
	poolByID := make(map[string]authPool, len(pools))
	for _, pool := range pools {
		pool.AllowedUserIDs = normalizeAuthPoolUserIDs(pool.AllowedUserIDs)
		if pool.Visibility == "" {
			pool.Visibility = authPoolVisibilityAdminsOnly
		}
		poolByID[strings.TrimSpace(pool.ID)] = pool
	}
	visible := make([]channelStatusItem, 0, len(items))
	for _, item := range items {
		pool, ok := poolByID[strings.TrimSpace(item.ID)]
		if ok && authPoolVisibleToUser(pool, user) {
			visible = append(visible, item)
		}
	}
	return visible
}

func scanChannelStatusItem(scanner interface{ Scan(dest ...any) error }) (channelStatusItem, *time.Time, error) {
	var item channelStatusItem
	var description, accountTypesJSON, windowStart, windowEnd, recentWindowStart, recentWindowEnd, recentBucketsJSON, lastChecked, lastHealthy, lastError, refreshedAt sql.NullString
	var statusCode, primaryRemaining, secondaryRemaining sql.NullInt64
	if err := scanner.Scan(
		&item.ID, &item.Name, &description, &item.Enabled, &accountTypesJSON, &item.AccountCount,
		&item.AvailableAccounts, &item.DisabledAccounts, &item.ErrorAccounts, &item.QuotaExhaustedAccounts,
		&item.Status, &item.Available, &statusCode, &primaryRemaining, &secondaryRemaining,
		&windowStart, &windowEnd, &item.WindowRecords, &item.WindowSuccessRecords, &item.WindowFailedRecords,
		&item.WindowCostUSD, &recentWindowStart, &recentWindowEnd, &recentBucketsJSON,
		&lastChecked, &lastHealthy, &lastError, &refreshedAt,
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
	item.RecentWindowStartAt = apiDateTimeFromDBString(recentWindowStart.String)
	item.RecentWindowEndAt = apiDateTimeFromDBString(recentWindowEnd.String)
	if err := json.Unmarshal([]byte(recentBucketsJSON.String), &item.RecentRequests); err != nil {
		item.RecentRequests = nil
	}
	item.RecentRequests = normalizeChannelStatusRecentRequests(item.RecentRequests)
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
			window_failed_records, window_cost_usd, recent_window_start_at, recent_window_end_at,
			recent_buckets_json, last_checked_at, last_healthy_at,
			last_error, refreshed_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	now := dbTime(time.Now())
	for _, item := range items {
		accountTypes, _ := json.Marshal(item.AccountTypes)
		recentRequests, _ := json.Marshal(item.RecentRequests)
		if _, err := stmt.ExecContext(ctx,
			item.ID, item.Name, item.Description, item.Enabled, string(accountTypes), item.AccountCount,
			item.AvailableAccounts, item.DisabledAccounts, item.ErrorAccounts, item.QuotaExhaustedAccounts,
			item.Status, item.Available, item.StatusCode, item.PrimaryRemainingPercent, item.SecondaryRemainingPercent,
			item.WindowStartAt, item.WindowEndAt, item.WindowRecords, item.WindowSuccessRecords,
			item.WindowFailedRecords, item.WindowCostUSD, item.RecentWindowStartAt, item.RecentWindowEndAt,
			string(recentRequests), item.LastCheckedAt, item.LastHealthyAt,
			item.LastError, item.RefreshedAt, now,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (a *App) channelStatusWindowRecords(ctx context.Context, start time.Time) ([]UsageRecord, error) {
	return a.channelStatusUsageRecordsQuery(ctx, `SELECT id, CAST(timestamp AS TEXT), provider, model, source_account, auth, auth_index, failed,
		input_tokens, output_tokens, cached_tokens, cache_read_tokens, cache_creation_tokens,
		reasoning_tokens, total_tokens, raw_json
		FROM usage_records WHERE timestamp >= ?
		ORDER BY timestamp ASC, id ASC`, dbTime(start))
}

func (a *App) channelStatusWindowRecordsBetween(ctx context.Context, start, end time.Time) ([]UsageRecord, error) {
	return a.channelStatusUsageRecordsQuery(ctx, `SELECT id, CAST(timestamp AS TEXT), provider, model, source_account, auth, auth_index, failed,
		input_tokens, output_tokens, cached_tokens, cache_read_tokens, cache_creation_tokens,
		reasoning_tokens, total_tokens, raw_json
		FROM usage_records WHERE timestamp >= ? AND timestamp <= ?
		ORDER BY timestamp ASC, id ASC`, dbTime(start), dbTime(end))
}

func (a *App) channelStatusWindowRecordsRange(ctx context.Context, start, end time.Time, maxID int) ([]UsageRecord, error) {
	if !end.After(start) {
		return []UsageRecord{}, nil
	}
	return a.channelStatusUsageRecordsQuery(ctx, `SELECT id, CAST(timestamp AS TEXT), provider, model, source_account, auth, auth_index, failed,
		input_tokens, output_tokens, cached_tokens, cache_read_tokens, cache_creation_tokens,
		reasoning_tokens, total_tokens, raw_json
		FROM usage_records WHERE timestamp >= ? AND timestamp < ? AND id <= ?
		ORDER BY timestamp ASC, id ASC`, dbTime(start), dbTime(end), maxID)
}

func (a *App) channelStatusUsageRecordsAfterID(ctx context.Context, afterID int, start, end time.Time) ([]UsageRecord, error) {
	return a.channelStatusUsageRecordsQuery(ctx, `SELECT id, CAST(timestamp AS TEXT), provider, model, source_account, auth, auth_index, failed,
		input_tokens, output_tokens, cached_tokens, cache_read_tokens, cache_creation_tokens,
		reasoning_tokens, total_tokens, raw_json
		FROM usage_records WHERE id > ? AND timestamp >= ? AND timestamp <= ?
		ORDER BY timestamp ASC, id ASC`, afterID, dbTime(start), dbTime(end))
}

func (a *App) channelStatusUsageRecordsQuery(ctx context.Context, query string, args ...any) ([]UsageRecord, error) {
	rows, err := a.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanChannelStatusUsageRecords(rows)
}

func (a *App) channelStatusLatestUsageID(ctx context.Context) (int, error) {
	var id int
	err := a.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(id), 0) FROM usage_records`).Scan(&id)
	return id, err
}

func buildChannelStatusItems(pools []authPool, accounts []keeperAccount, records []UsageRecord, prices map[[2]string]ModelPrice, now time.Time) []channelStatusItem {
	items, _ := buildChannelStatusItemsContext(context.Background(), pools, accounts, records, prices, now)
	return items
}

func buildChannelStatusItemsContext(ctx context.Context, pools []authPool, accounts []keeperAccount, records []UsageRecord, prices map[[2]string]ModelPrice, now time.Time) ([]channelStatusItem, error) {
	items := make([]channelStatusItem, 0, len(pools))
	poolIndexesByMemberKey := make(map[string][]int)
	windowStart := now.Add(-channelStatusWindowDuration)
	for _, pool := range pools {
		members := channelPoolAccounts(pool, accounts)
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
		itemIndex := len(items)
		items = append(items, item)
		for memberKey := range channelMemberKeys(members) {
			poolIndexesByMemberKey[memberKey] = append(poolIndexesByMemberKey[memberKey], itemIndex)
		}
	}
	seenPoolMarkers := make([]int, len(items))
	matchedPoolIndexes := make([]int, 0, 2)
	recentRequestBuffers := make([]channelStatusRecentRequestBuffer, len(items))
	for recordIndex, record := range records {
		if recordIndex%1024 == 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
			}
		}
		matchedPoolIndexes = appendChannelUsageRecordPoolIndexes(
			matchedPoolIndexes[:0], record, poolIndexesByMemberKey, seenPoolMarkers, recordIndex+1,
		)
		if len(matchedPoolIndexes) == 0 {
			continue
		}
		cost, _ := recordCost(record, prices)
		for _, itemIndex := range matchedPoolIndexes {
			item := &items[itemIndex]
			item.WindowRecords++
			if record.Failed {
				item.WindowFailedRecords++
			} else {
				item.WindowSuccessRecords++
			}
			item.WindowCostUSD = mathRound(item.WindowCostUSD+cost, 8)
			recentRequestBuffers[itemIndex].add(record)
		}
	}
	for index := range items {
		items[index].RecentRequests = recentRequestBuffers[index].snapshot()
		items[index].RecentWindowStartAt, items[index].RecentWindowEndAt = channelStatusRecentRequestBounds(items[index].RecentRequests)
		applyChannelRecentRequestStatus(&items[index])
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Name == items[j].Name {
			return items[i].ID < items[j].ID
		}
		return items[i].Name < items[j].Name
	})
	return items, nil
}

func newChannelStatusUsageCache(items []channelStatusItem, membershipSignature, priceSignature string, windowStart time.Time, lastUsageID int) *channelStatusUsageCache {
	cache := &channelStatusUsageCache{
		MembershipSignature: membershipSignature,
		PriceSignature:      priceSignature,
		WindowStart:         windowStart,
		LastUsageID:         lastUsageID,
		Aggregates:          make(map[string]channelStatusUsageAggregate, len(items)),
		Recent:              make(map[string]channelStatusRecentRequestBuffer, len(items)),
	}
	for _, item := range items {
		cache.Aggregates[item.ID] = channelStatusUsageAggregate{
			Records:        item.WindowRecords,
			SuccessRecords: item.WindowSuccessRecords,
			FailedRecords:  item.WindowFailedRecords,
			CostUSD:        item.WindowCostUSD,
		}
		var buffer channelStatusRecentRequestBuffer
		buffer.replace(item.RecentRequests)
		cache.Recent[item.ID] = buffer
	}
	return cache
}

func channelStatusCacheReusable(cache *channelStatusUsageCache, membershipSignature, priceSignature string, windowStart time.Time, latestUsageID int) bool {
	return cache != nil &&
		cache.MembershipSignature == membershipSignature &&
		cache.PriceSignature == priceSignature &&
		cache.Aggregates != nil && cache.Recent != nil &&
		!windowStart.Before(cache.WindowStart) &&
		latestUsageID >= cache.LastUsageID
}

func cloneChannelStatusUsageCache(cache *channelStatusUsageCache) *channelStatusUsageCache {
	cloned := &channelStatusUsageCache{
		MembershipSignature: cache.MembershipSignature,
		PriceSignature:      cache.PriceSignature,
		WindowStart:         cache.WindowStart,
		LastUsageID:         cache.LastUsageID,
		Aggregates:          make(map[string]channelStatusUsageAggregate, len(cache.Aggregates)),
		Recent:              make(map[string]channelStatusRecentRequestBuffer, len(cache.Recent)),
	}
	for poolID, aggregate := range cache.Aggregates {
		cloned.Aggregates[poolID] = aggregate
	}
	for poolID, buffer := range cache.Recent {
		cloned.Recent[poolID] = buffer
	}
	return cloned
}

func buildIncrementalChannelStatusItemsContext(ctx context.Context, pools []authPool, accounts []keeperAccount, expired, added []UsageRecord, prices map[[2]string]ModelPrice, now time.Time, cache *channelStatusUsageCache) ([]channelStatusItem, *channelStatusUsageCache, error) {
	items, err := buildChannelStatusItemsContext(ctx, pools, accounts, nil, prices, now)
	if err != nil {
		return nil, cache, err
	}
	indexes := channelStatusPoolIndexesForItems(items, pools, accounts)
	nextCache := cloneChannelStatusUsageCache(cache)
	if err := applyChannelStatusUsageDelta(ctx, items, indexes, nextCache, expired, prices, -1, time.Time{}, time.Time{}); err != nil {
		return nil, cache, err
	}
	windowStart := now.Add(-channelStatusWindowDuration)
	if err := applyChannelStatusUsageDelta(ctx, items, indexes, nextCache, added, prices, 1, windowStart, now); err != nil {
		return nil, cache, err
	}
	for index := range items {
		item := &items[index]
		aggregate := nextCache.Aggregates[item.ID]
		item.WindowRecords = aggregate.Records
		item.WindowSuccessRecords = aggregate.SuccessRecords
		item.WindowFailedRecords = aggregate.FailedRecords
		item.WindowCostUSD = mathRound(aggregate.CostUSD, 8)
		buffer := nextCache.Recent[item.ID]
		buffer.pruneBefore(windowStart)
		nextCache.Recent[item.ID] = buffer
		item.RecentRequests = buffer.snapshot()
		item.RecentWindowStartAt, item.RecentWindowEndAt = channelStatusRecentRequestBounds(item.RecentRequests)
		applyChannelRecentRequestStatus(item)
	}
	return items, nextCache, nil
}

func channelStatusPoolIndexesForItems(items []channelStatusItem, pools []authPool, accounts []keeperAccount) map[string][]int {
	itemIndexByID := make(map[string]int, len(items))
	for index := range items {
		itemIndexByID[items[index].ID] = index
	}
	indexes := make(map[string][]int)
	for _, pool := range pools {
		itemIndex, ok := itemIndexByID[strings.TrimSpace(pool.ID)]
		if !ok {
			continue
		}
		for memberKey := range channelMemberKeys(channelPoolAccounts(pool, accounts)) {
			indexes[memberKey] = append(indexes[memberKey], itemIndex)
		}
	}
	return indexes
}

func applyChannelStatusUsageDelta(ctx context.Context, items []channelStatusItem, indexes map[string][]int, cache *channelStatusUsageCache, records []UsageRecord, prices map[[2]string]ModelPrice, direction int, start, end time.Time) error {
	seenPoolMarkers := make([]int, len(items))
	matchedPoolIndexes := make([]int, 0, 2)
	recentByPool := make(map[string][]UsageRecord)
	for recordIndex, record := range records {
		if recordIndex%1024 == 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
		}
		if direction > 0 && (!start.IsZero() && record.Timestamp.Before(start) || !end.IsZero() && record.Timestamp.After(end)) {
			continue
		}
		matchedPoolIndexes = appendChannelUsageRecordPoolIndexes(matchedPoolIndexes[:0], record, indexes, seenPoolMarkers, recordIndex+1)
		if len(matchedPoolIndexes) == 0 {
			continue
		}
		cost, _ := recordCost(record, prices)
		for _, itemIndex := range matchedPoolIndexes {
			poolID := items[itemIndex].ID
			aggregate := cache.Aggregates[poolID]
			aggregate.Records += direction
			if record.Failed {
				aggregate.FailedRecords += direction
			} else {
				aggregate.SuccessRecords += direction
			}
			aggregate.CostUSD = mathRound(aggregate.CostUSD+float64(direction)*cost, 8)
			if aggregate.Records < 0 {
				aggregate = channelStatusUsageAggregate{}
			} else {
				if aggregate.SuccessRecords < 0 {
					aggregate.SuccessRecords = 0
				}
				if aggregate.FailedRecords < 0 {
					aggregate.FailedRecords = 0
				}
				if aggregate.CostUSD < 0 && aggregate.CostUSD > -0.00000001 {
					aggregate.CostUSD = 0
				}
			}
			cache.Aggregates[poolID] = aggregate
			if direction > 0 {
				recentByPool[poolID] = append(recentByPool[poolID], record)
			}
		}
	}
	for poolID, recent := range recentByPool {
		buffer := cache.Recent[poolID]
		buffer.merge(recent)
		cache.Recent[poolID] = buffer
	}
	return nil
}

func channelStatusMembershipSignature(pools []authPool, accounts []keeperAccount) string {
	type poolSignature struct {
		ID              string   `json:"id"`
		Enabled         bool     `json:"enabled"`
		AuthIDs         []string `json:"auth_ids"`
		ResolvedAuthIDs []string `json:"resolved_auth_ids"`
		AccountTypes    []string `json:"account_types"`
		MemberKeys      []string `json:"member_keys"`
	}
	signatures := make([]poolSignature, 0, len(pools))
	for _, pool := range pools {
		memberLookup := channelMemberKeys(channelPoolAccounts(pool, accounts))
		memberKeys := make([]string, 0, len(memberLookup))
		for key := range memberLookup {
			memberKeys = append(memberKeys, key)
		}
		sort.Strings(memberKeys)
		signatures = append(signatures, poolSignature{
			ID:              strings.TrimSpace(pool.ID),
			Enabled:         pool.Enabled,
			AuthIDs:         normalizedStringList(pool.AuthIDs),
			ResolvedAuthIDs: normalizedStringList(pool.ResolvedAuthIDs),
			AccountTypes:    normalizedStringList(pool.AccountTypes),
			MemberKeys:      memberKeys,
		})
	}
	sort.Slice(signatures, func(i, j int) bool { return signatures[i].ID < signatures[j].ID })
	return channelStatusJSONSignature(signatures)
}

func channelStatusPriceSignature(prices map[[2]string]ModelPrice) string {
	type priceSignature struct {
		Provider                   string   `json:"provider"`
		Model                      string   `json:"model"`
		InputUSDPerMillion         float64  `json:"input"`
		OutputUSDPerMillion        float64  `json:"output"`
		CacheReadUSDPerMillion     float64  `json:"cache_read"`
		CacheCreationUSDPerMillion float64  `json:"cache_creation"`
		RequestUSD                 *float64 `json:"request,omitempty"`
		BillingUnit                string   `json:"billing_unit"`
	}
	keys := make([][2]string, 0, len(prices))
	for key := range prices {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i][0] == keys[j][0] {
			return keys[i][1] < keys[j][1]
		}
		return keys[i][0] < keys[j][0]
	})
	ordered := make([]priceSignature, 0, len(keys))
	for _, key := range keys {
		price := prices[key]
		ordered = append(ordered, priceSignature{
			Provider:                   price.Provider,
			Model:                      price.Model,
			InputUSDPerMillion:         price.InputUSDPerMillion,
			OutputUSDPerMillion:        price.OutputUSDPerMillion,
			CacheReadUSDPerMillion:     price.CacheReadUSDPerMillion,
			CacheCreationUSDPerMillion: price.CacheCreationUSDPerMillion,
			RequestUSD:                 price.RequestUSD,
			BillingUnit:                price.BillingUnit,
		})
	}
	return channelStatusJSONSignature(ordered)
}

func channelStatusJSONSignature(value any) string {
	raw, _ := json.Marshal(value)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func scanChannelStatusUsageRecords(rows *sql.Rows) ([]UsageRecord, error) {
	records := make([]UsageRecord, 0)
	for rows.Next() {
		var record UsageRecord
		var timestamp, provider, model, sourceAccount, auth, authIndex, rawJSON sql.NullString
		if err := rows.Scan(
			&record.ID, &timestamp, &provider, &model, &sourceAccount, &auth, &authIndex, &record.Failed,
			&record.InputTokens, &record.OutputTokens, &record.CachedTokens,
			&record.CacheReadTokens, &record.CacheCreationTokens, &record.ReasoningTokens,
			&record.TotalTokens, &rawJSON,
		); err != nil {
			return nil, err
		}
		if parsed, ok := parseDBTime(timestamp.String); ok {
			record.Timestamp = parsed
		}
		record.Provider = nullableString(provider)
		record.Model = nullableString(model)
		record.SourceAccount = nullableString(sourceAccount)
		record.Auth = nullableString(auth)
		record.AuthIndex = nullableString(authIndex)
		record.RawJSON = rawJSON.String
		records = append(records, record)
	}
	return records, rows.Err()
}

func channelStatusRecentRequestBounds(requests []channelStatusRecentRequest) (string, string) {
	start := ""
	end := ""
	for _, request := range requests {
		if strings.TrimSpace(request.Timestamp) == "" {
			continue
		}
		if start == "" {
			start = request.Timestamp
		}
		end = request.Timestamp
	}
	return start, end
}

func normalizeChannelStatusRecentRequests(requests []channelStatusRecentRequest) []channelStatusRecentRequest {
	normalized := make([]channelStatusRecentRequest, 0, channelStatusRecentRequestCount)
	for _, request := range requests {
		if strings.TrimSpace(request.Timestamp) == "" {
			continue
		}
		request.Timestamp = apiDateTimeFromDBString(request.Timestamp)
		if request.Timestamp == "" {
			continue
		}
		normalized = append(normalized, request)
	}
	if len(normalized) > channelStatusRecentRequestCount {
		normalized = normalized[len(normalized)-channelStatusRecentRequestCount:]
	}
	result := make([]channelStatusRecentRequest, channelStatusRecentRequestCount)
	copy(result[channelStatusRecentRequestCount-len(normalized):], normalized)
	return result
}

func appendChannelUsageRecordPoolIndexes(matched []int, record UsageRecord, poolIndexesByMemberKey map[string][]int, seenPoolMarkers []int, marker int) []int {
	for _, value := range usageRecordChannelKeys(record) {
		key := normalizeChannelKey(value)
		if key == "" {
			continue
		}
		for _, itemIndex := range poolIndexesByMemberKey[key] {
			if itemIndex < 0 || itemIndex >= len(seenPoolMarkers) || seenPoolMarkers[itemIndex] == marker {
				continue
			}
			seenPoolMarkers[itemIndex] = marker
			matched = append(matched, itemIndex)
		}
	}
	return matched
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
		primaryUsed, secondaryUsed := channelAccountUsedPercents(account)
		primaryRemaining.add(primaryUsed)
		secondaryRemaining.add(secondaryUsed)
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

func applyChannelRecentRequestStatus(item *channelStatusItem) {
	if !item.Enabled {
		item.Status = "disabled"
		item.Available = false
		return
	}
	if item.WindowRecords <= 0 {
		return
	}
	if item.WindowFailedRecords == 0 {
		item.Status = "normal"
		item.Available = true
		return
	}
	if item.WindowFailedRecords > item.WindowSuccessRecords {
		item.Status = "error"
		item.Available = false
		return
	}
	item.Status = "degraded"
	item.Available = true
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
	primaryUsed, secondaryUsed := channelAccountUsedPercents(account)
	if isQuotaExhaustedPercent(primaryUsed) || isQuotaExhaustedPercent(secondaryUsed) {
		return "quota_exhausted", false
	}
	return "normal", true
}

func channelAccountUsedPercents(account keeperAccount) (*int, *int) {
	primary := account.PrimaryUsedPercent
	secondary := account.SecondaryUsedPercent
	if keeperAntigravityAccount(account) {
		if value := keeperAntigravityQuotaUsedPercent(account, true); value != nil {
			primary = value
		}
		if value := keeperAntigravityQuotaUsedPercent(account, false); value != nil {
			secondary = value
		}
	}
	return primary, secondary
}

func channelPoolAccounts(pool authPool, accounts []keeperAccount) []keeperAccount {
	manualIDs := normalizedLookup(append(append([]string(nil), pool.AuthIDs...), pool.ResolvedAuthIDs...))
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

type channelUsageIdentity struct {
	AuthIndexSnake     any `json:"auth_index"`
	AuthIndexCamel     any `json:"authIndex"`
	AuthNameSnake      any `json:"auth_name"`
	AuthNameCamel      any `json:"authName"`
	AccountIDSnake     any `json:"account_id"`
	AccountIDCamel     any `json:"accountId"`
	SourceAccountSnake any `json:"source_account"`
	SourceAccountCamel any `json:"sourceAccount"`
	Email              any `json:"email"`
}

func usageRecordChannelKeys(record UsageRecord) []string {
	keys := make([]string, 0, 12)
	keys = append(keys,
		stringPtrValue(record.SourceAccount),
		stringPtrValue(record.AuthIndex),
		stringPtrValue(record.Auth),
	)
	if !usageRecordNeedsRawIdentity(record) {
		return keys
	}
	var payload channelUsageIdentity
	if json.Unmarshal([]byte(record.RawJSON), &payload) != nil {
		return keys
	}
	for _, field := range []any{
		payload.AuthIndexSnake, payload.AuthIndexCamel,
		payload.AuthNameSnake, payload.AuthNameCamel,
		payload.AccountIDSnake, payload.AccountIDCamel,
		payload.SourceAccountSnake, payload.SourceAccountCamel,
		payload.Email,
	} {
		if value := channelUsageString(field); value != "" {
			keys = append(keys, value)
		}
	}
	return keys
}

func usageRecordNeedsRawIdentity(record UsageRecord) bool {
	if strings.TrimSpace(stringPtrValue(record.SourceAccount)) == "" && strings.TrimSpace(stringPtrValue(record.AuthIndex)) == "" {
		return true
	}
	raw := []byte(record.RawJSON)
	for _, marker := range [][]byte{
		[]byte(`"auth_name"`), []byte(`"authName"`),
		[]byte(`"account_id"`), []byte(`"accountId"`),
		[]byte(`"source_account"`), []byte(`"sourceAccount"`),
		[]byte(`"email"`),
	} {
		if bytes.Contains(raw, marker) {
			return true
		}
	}
	return false
}

func channelUsageString(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(typed)
	default:
		return ""
	}
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
