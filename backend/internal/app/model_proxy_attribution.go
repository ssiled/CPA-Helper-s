package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

const modelProxyRequestAttributionTTL = 7 * 24 * time.Hour

var modelProxyRequestIDHeaders = []string{
	"x-cpa-request-id",
	"x-request-id",
	"request-id",
	"openai-request-id",
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
	apiKeyHash = strings.TrimSpace(apiKeyHash)
	if apiKeyHash == "" {
		return nil
	}
	now := dbTime(time.Now())
	for _, requestID := range requestIDs {
		requestID = strings.TrimSpace(requestID)
		if requestID == "" {
			continue
		}
		if _, err := a.db.ExecContext(ctx, `
			INSERT INTO model_proxy_request_attributions (request_id, api_key_hash, created_at, updated_at)
			VALUES (?, ?, ?, ?)
			ON CONFLICT(request_id) DO UPDATE SET api_key_hash = excluded.api_key_hash, updated_at = excluded.updated_at
		`, requestID, apiKeyHash, now, now); err != nil {
			return err
		}
	}
	_, err := a.db.ExecContext(ctx, `DELETE FROM model_proxy_request_attributions WHERE created_at < ?`, dbTime(time.Now().Add(-modelProxyRequestAttributionTTL)))
	return err
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
	return normalized.APIKeyHash, nil
}
