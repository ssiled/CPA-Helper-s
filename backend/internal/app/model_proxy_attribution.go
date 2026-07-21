package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

const modelProxyRequestAttributionTTL = 7 * 24 * time.Hour
const modelProxyRequestAttributionWindow = 30 * time.Minute
const modelProxyRequestAttributionCleanupInterval = time.Hour
const modelProxyRequestAttributionCleanupTimeout = 30 * time.Second

var modelProxyRequestIDHeaders = []string{
	"x-cpa-request-id",
	"x-request-id",
	"request-id",
	"openai-request-id",
}

type modelProxyRequestAttributionMetadata struct {
	Model       string
	Endpoint    string
	StartedAt   time.Time
	CompletedAt *time.Time
	StatusCode  *int
}

func newModelProxyRequestID() string {
	return "cpa-helper-" + uuid.NewString()
}

func modelProxyRequestIDFromResponse(response *http.Response, payload []byte) string {
	if response != nil {
		for _, header := range modelProxyRequestIDHeaders {
			if value := strings.TrimSpace(response.Header.Get(header)); value != "" {
				return value
			}
		}
	}
	return modelProxyRequestIDFromBody(payload)
}

func modelProxyRequestIDFromBody(payload []byte) string {
	if len(payload) == 0 || !json.Valid(payload) {
		return ""
	}
	var parsed any
	if err := json.Unmarshal(payload, &parsed); err != nil {
		return ""
	}
	if value := toString(findFirst(parsed, "request_id", "requestId")); value != nil {
		return strings.TrimSpace(*value)
	}
	if value := toString(findFirst(parsed, "id")); value != nil {
		return strings.TrimSpace(*value)
	}
	return ""
}

func (a *App) recordModelProxyRequestAttributions(ctx context.Context, apiKeyHash string, requestIDs ...string) error {
	return a.recordModelProxyRequestAttributionsWithMetadata(ctx, apiKeyHash, modelProxyRequestAttributionMetadata{StartedAt: time.Now()}, requestIDs...)
}

func (a *App) recordModelProxyRequestAttributionsWithMetadata(ctx context.Context, apiKeyHash string, meta modelProxyRequestAttributionMetadata, requestIDs ...string) error {
	apiKeyHash = strings.TrimSpace(apiKeyHash)
	if apiKeyHash == "" {
		return nil
	}
	startedAt := meta.StartedAt
	if startedAt.IsZero() {
		startedAt = time.Now()
	}
	model := nullableTrimmedString(meta.Model)
	endpoint := nullableTrimmedString(meta.Endpoint)
	startedValue := dbTime(startedAt)
	var completedValue any
	if meta.CompletedAt != nil && !meta.CompletedAt.IsZero() {
		completedValue = dbTime(*meta.CompletedAt)
	}
	var statusValue any
	if meta.StatusCode != nil {
		statusValue = *meta.StatusCode
	}
	now := dbTime(time.Now())
	uniqueRequestIDs := make([]string, 0, len(requestIDs))
	seenRequestIDs := make(map[string]struct{}, len(requestIDs))
	for _, requestID := range requestIDs {
		requestID = strings.TrimSpace(requestID)
		if requestID == "" {
			continue
		}
		if _, seen := seenRequestIDs[requestID]; seen {
			continue
		}
		seenRequestIDs[requestID] = struct{}{}
		uniqueRequestIDs = append(uniqueRequestIDs, requestID)
	}
	if len(uniqueRequestIDs) == 0 {
		return nil
	}
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, requestID := range uniqueRequestIDs {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO model_proxy_request_attributions (
				request_id, api_key_hash, model, endpoint, started_at, completed_at, status_code, created_at, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(request_id) DO UPDATE SET
				api_key_hash = excluded.api_key_hash,
				model = COALESCE(NULLIF(excluded.model, ''), model),
				endpoint = COALESCE(NULLIF(excluded.endpoint, ''), endpoint),
				started_at = COALESCE(excluded.started_at, started_at),
				completed_at = COALESCE(excluded.completed_at, completed_at),
				status_code = COALESCE(excluded.status_code, status_code),
				updated_at = excluded.updated_at
		`, requestID, apiKeyHash, model, endpoint, startedValue, completedValue, statusValue, now, now); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	a.scheduleModelProxyAttributionCleanup()
	return nil
}

func (a *App) scheduleModelProxyAttributionCleanup() {
	now := time.Now()
	a.attributionMu.Lock()
	if now.Before(a.attributionNext) {
		a.attributionMu.Unlock()
		return
	}
	a.attributionNext = now.Add(modelProxyRequestAttributionCleanupInterval)
	a.attributionMu.Unlock()
	a.startBackgroundTask(func(parent context.Context) {
		ctx, cancel := context.WithTimeout(parent, modelProxyRequestAttributionCleanupTimeout)
		defer cancel()
		if _, err := a.db.ExecContext(ctx, `DELETE FROM model_proxy_request_attributions WHERE created_at < ?`, dbTime(time.Now().Add(-modelProxyRequestAttributionTTL))); err != nil && parent.Err() == nil {
			log.Printf("cleanup model proxy request attributions failed: %v", err)
		}
	})
}

func nullableTrimmedString(value string) any {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return value
}

func (a *App) modelProxyRequestAttribution(ctx context.Context, requestID string) (string, bool, error) {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return "", false, nil
	}
	var apiKeyHash string
	err := a.db.QueryRowContext(ctx, `SELECT api_key_hash FROM model_proxy_request_attributions WHERE request_id = ?`, requestID).Scan(&apiKeyHash)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return apiKeyHash, strings.TrimSpace(apiKeyHash) != "", nil
}

func (a *App) usageOwnerAPIKeyHash(ctx context.Context, normalized normalizedUsage) (string, error) {
	if normalized.RequestID != nil {
		if apiKeyHash, ok, err := a.modelProxyRequestAttribution(ctx, *normalized.RequestID); err != nil {
			return "", err
		} else if ok {
			return apiKeyHash, nil
		}
	}
	if ok, err := a.userAPIKeyHashExists(ctx, normalized.APIKeyHash); err != nil {
		return "", err
	} else if ok {
		return normalized.APIKeyHash, nil
	}
	if apiKeyHash, ok, err := a.modelProxyRequestAttributionForUsage(ctx, normalized); err != nil {
		return "", err
	} else if ok {
		return apiKeyHash, nil
	}
	return normalized.APIKeyHash, nil
}

func (a *App) userAPIKeyHashExists(ctx context.Context, apiKeyHash string) (bool, error) {
	apiKeyHash = strings.TrimSpace(apiKeyHash)
	if apiKeyHash == "" {
		return false, nil
	}
	var count int
	if err := a.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM user_api_keys WHERE api_key_hash = ?`, apiKeyHash).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

type modelProxyAttributionCandidate struct {
	APIKeyHash string
	Model      string
	Endpoint   string
	MatchedAt  time.Time
	Distance   time.Duration
}

func (a *App) modelProxyRequestAttributionForUsage(ctx context.Context, normalized normalizedUsage) (string, bool, error) {
	usageTime := normalized.Timestamp
	if usageTime.IsZero() {
		usageTime = time.Now().In(appTimeLocation)
	}
	start := usageTime.Add(-modelProxyRequestAttributionWindow)
	end := usageTime.Add(modelProxyRequestAttributionWindow)
	rows, err := a.db.QueryContext(ctx, `
		SELECT api_key_hash, COALESCE(model, ''), COALESCE(endpoint, ''), CAST(COALESCE(started_at, created_at) AS TEXT)
		FROM model_proxy_request_attributions
		WHERE api_key_hash != ''
		  AND COALESCE(started_at, created_at) >= ?
		  AND COALESCE(started_at, created_at) <= ?
		ORDER BY ABS(strftime('%s', COALESCE(started_at, created_at)) - strftime('%s', ?)) ASC, updated_at DESC
		LIMIT 300
	`, dbTime(start), dbTime(end), dbTime(usageTime))
	if err != nil {
		return "", false, err
	}
	defer rows.Close()

	usageModel := stringPtrValue(normalized.Model)
	usageEndpoint := stringPtrValue(normalized.Endpoint)
	var best *modelProxyAttributionCandidate
	ambiguous := false
	for rows.Next() {
		var candidate modelProxyAttributionCandidate
		var matchedAtText string
		if err := rows.Scan(&candidate.APIKeyHash, &candidate.Model, &candidate.Endpoint, &matchedAtText); err != nil {
			return "", false, err
		}
		if !modelProxyAttributionModelMatches(candidate.Model, usageModel) {
			continue
		}
		if !modelProxyAttributionEndpointMatches(candidate.Endpoint, usageEndpoint) {
			continue
		}
		matchedAt, ok := parseDBTime(matchedAtText)
		if !ok {
			continue
		}
		candidate.MatchedAt = matchedAt
		candidate.Distance = absDuration(matchedAt.Sub(usageTime))
		if best == nil || candidate.Distance < best.Distance {
			copyCandidate := candidate
			best = &copyCandidate
			ambiguous = false
			continue
		}
		if candidate.Distance == best.Distance && candidate.APIKeyHash != best.APIKeyHash {
			ambiguous = true
		}
	}
	if err := rows.Err(); err != nil {
		return "", false, err
	}
	if best == nil || ambiguous {
		return "", false, nil
	}
	return best.APIKeyHash, true, nil
}

func modelProxyAttributionModelMatches(candidateModel, usageModel string) bool {
	candidateModel = strings.TrimSpace(candidateModel)
	usageModel = strings.TrimSpace(usageModel)
	if candidateModel == "" || usageModel == "" {
		return true
	}
	candidateLower := strings.ToLower(candidateModel)
	usageLower := strings.ToLower(usageModel)
	return candidateLower == usageLower ||
		strings.HasPrefix(usageLower, candidateLower+" ") ||
		strings.HasPrefix(candidateLower, usageLower+" ")
}

func modelProxyAttributionEndpointMatches(candidateEndpoint, usageEndpoint string) bool {
	candidateEndpoint = strings.TrimSpace(candidateEndpoint)
	usageEndpoint = strings.TrimSpace(usageEndpoint)
	if candidateEndpoint == "" || usageEndpoint == "" {
		return true
	}
	if strings.EqualFold(candidateEndpoint, usageEndpoint) {
		return true
	}
	candidateMethod, candidatePath := modelProxyEndpointParts(candidateEndpoint)
	usageMethod, usagePath := modelProxyEndpointParts(usageEndpoint)
	if candidatePath == "" || usagePath == "" || candidatePath != usagePath {
		return false
	}
	return candidateMethod == "" || usageMethod == "" || candidateMethod == usageMethod
}

func modelProxyEndpointParts(value string) (string, string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", ""
	}
	fields := strings.Fields(value)
	if len(fields) >= 2 && strings.HasPrefix(fields[1], "/") {
		return strings.ToUpper(fields[0]), normalizeModelProxyEndpointPath(fields[1])
	}
	if strings.HasPrefix(value, "/") {
		return "", normalizeModelProxyEndpointPath(value)
	}
	return "", strings.ToLower(value)
}

func normalizeModelProxyEndpointPath(value string) string {
	value = strings.TrimSpace(value)
	if index := strings.Index(value, "?"); index >= 0 {
		value = value[:index]
	}
	return strings.ToLower(strings.TrimRight(value, "/"))
}

func absDuration(value time.Duration) time.Duration {
	if value < 0 {
		return -value
	}
	return value
}

func (a *App) repairRecentUsageOwnerSnapshots(ctx context.Context) error {
	rows, err := a.db.QueryContext(ctx, `SELECT id, CAST(timestamp AS TEXT), request_id, provider, model, endpoint, auth_index, latency_ms, raw_json
		FROM usage_records
		WHERE (usage_username IS NULL OR usage_username = '' OR api_key_description IS NULL OR api_key_description = '')
		  AND timestamp >= ?
		ORDER BY timestamp DESC
		LIMIT 500`, dbTime(time.Now().Add(-modelProxyRequestAttributionTTL)))
	if err != nil {
		return err
	}
	defer rows.Close()

	type pendingUsageRecord struct {
		ID        int
		Timestamp string
		RequestID sql.NullString
		Provider  sql.NullString
		Model     sql.NullString
		Endpoint  sql.NullString
		AuthIndex sql.NullString
		LatencyMS sql.NullFloat64
		RawJSON   string
	}
	pending := []pendingUsageRecord{}
	for rows.Next() {
		var record pendingUsageRecord
		if err := rows.Scan(&record.ID, &record.Timestamp, &record.RequestID, &record.Provider, &record.Model, &record.Endpoint, &record.AuthIndex, &record.LatencyMS, &record.RawJSON); err != nil {
			return err
		}
		pending = append(pending, record)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	pluginPending := make([]pluginUsageOwnerRepair, 0, len(pending))
	knownRequestOwners := map[string]string{}
	ambiguousRequestOwners := map[string]bool{}
	for _, record := range pending {
		normalized, err := normalizeUsage([]byte(record.RawJSON))
		if err != nil {
			continue
		}
		if parsed, ok := parseDBTime(record.Timestamp); ok {
			normalized.Timestamp = parsed
		}
		if value := nullableString(record.RequestID); value != nil {
			normalized.RequestID = value
		}
		if value := nullableString(record.Provider); value != nil {
			normalized.Provider = value
		}
		if value := nullableString(record.Model); value != nil {
			normalized.Model = value
		}
		if value := nullableString(record.Endpoint); value != nil {
			normalized.Endpoint = value
		}
		ownerAPIKeyHash, err := a.usageOwnerAPIKeyHash(ctx, normalized)
		if err != nil {
			return err
		}
		usageUsername, description, err := a.usageOwnerSnapshot(ctx, ownerAPIKeyHash)
		if err != nil {
			return err
		}
		if usageUsername == nil && description == nil {
			pluginRecord := pluginUsageOwnerRepair{
				ID:        record.ID,
				Timestamp: normalized.Timestamp,
				RequestID: stringPtrValue(normalized.RequestID),
				Provider:  stringPtrValue(normalized.Provider),
				Model:     stringPtrValue(normalized.Model),
				AuthIndex: stringPtrValue(normalized.AuthIndex),
			}
			if record.AuthIndex.Valid {
				pluginRecord.AuthIndex = strings.TrimSpace(record.AuthIndex.String)
			}
			if record.LatencyMS.Valid {
				latency := record.LatencyMS.Float64
				pluginRecord.LatencyMS = &latency
			}
			pluginPending = append(pluginPending, pluginRecord)
			continue
		}
		if _, err := a.db.ExecContext(ctx, `UPDATE usage_records SET usage_username = COALESCE(NULLIF(usage_username, ''), ?), api_key_description = COALESCE(NULLIF(api_key_description, ''), ?) WHERE id = ?`, usageUsername, description, record.ID); err != nil {
			return err
		}
		requestID := strings.TrimSpace(stringPtrValue(normalized.RequestID))
		if requestID != "" && requestID != "-" {
			if current := knownRequestOwners[requestID]; current != "" && current != ownerAPIKeyHash {
				ambiguousRequestOwners[requestID] = true
			} else {
				knownRequestOwners[requestID] = ownerAPIKeyHash
			}
		}
	}
	remaining := pluginPending[:0]
	for _, record := range pluginPending {
		requestID := strings.TrimSpace(record.RequestID)
		apiKeyHash := knownRequestOwners[requestID]
		if requestID == "" || requestID == "-" || apiKeyHash == "" || ambiguousRequestOwners[requestID] {
			remaining = append(remaining, record)
			continue
		}
		usageUsername, description, err := a.usageOwnerSnapshot(ctx, apiKeyHash)
		if err != nil {
			return err
		}
		if _, err := a.db.ExecContext(ctx, `UPDATE usage_records SET usage_username = COALESCE(NULLIF(usage_username, ''), ?), api_key_description = COALESCE(NULLIF(api_key_description, ''), ?) WHERE id = ?`, usageUsername, description, record.ID); err != nil {
			return err
		}
	}
	pluginPending = remaining
	if len(pluginPending) == 0 {
		return nil
	}
	events := a.cachedAuthPoolPluginEventsForAttribution(ctx)
	validEvents := make([]authPoolPluginEvent, 0, len(events))
	validHashes := map[string]bool{}
	for _, event := range events {
		hash := strings.TrimSpace(event.APIKeyHash)
		if hash == "" {
			continue
		}
		valid, known := validHashes[hash]
		if !known {
			username, description, err := a.usageOwnerSnapshot(ctx, hash)
			if err != nil {
				return err
			}
			valid = username != nil || description != nil
			validHashes[hash] = valid
		}
		if valid {
			validEvents = append(validEvents, event)
		}
	}
	for recordID, apiKeyHash := range matchPluginUsageAttributions(pluginPending, validEvents) {
		usageUsername, description, err := a.usageOwnerSnapshot(ctx, apiKeyHash)
		if err != nil {
			return err
		}
		if usageUsername == nil && description == nil {
			continue
		}
		if _, err := a.db.ExecContext(ctx, `UPDATE usage_records SET usage_username = COALESCE(NULLIF(usage_username, ''), ?), api_key_description = COALESCE(NULLIF(api_key_description, ''), ?) WHERE id = ?`, usageUsername, description, recordID); err != nil {
			return err
		}
	}
	return nil
}
