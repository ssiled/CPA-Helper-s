package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

const modelProxyBodyLimit = 32 << 20

const authPoolProxyAPIKeyHashHeader = "X-CPA-Helper-API-Key-Hash"

var hopByHopHeaders = map[string]bool{
	"connection":          true,
	"keep-alive":          true,
	"proxy-authenticate":  true,
	"proxy-authorization": true,
	"te":                  true,
	"trailer":             true,
	"transfer-encoding":   true,
	"upgrade":             true,
}

func (a *App) handleModelProxy(w http.ResponseWriter, r *http.Request) error {
	apiKey, err := a.modelProxyAPIKey(r.Context(), r)
	if err != nil {
		return err
	}
	if apiKey.APIKey == nil || strings.TrimSpace(*apiKey.APIKey) == "" {
		return authenticationError("API KEY unavailable")
	}
	if r.URL.Path == "/v1/models" && r.Method == http.MethodGet {
		return a.handleModelProxyModels(w, r, apiKey)
	}
	return a.handleModelProxyForward(w, r, apiKey)
}

func (a *App) modelProxyAPIKey(ctx context.Context, r *http.Request) (UserAPIKey, error) {
	apiKey := bearerToken(r.Header.Get("Authorization"))
	if apiKey == "" {
		apiKey = strings.TrimSpace(r.Header.Get("x-api-key"))
	}
	if apiKey == "" {
		return UserAPIKey{}, authenticationError("API KEY is required")
	}
	binding, err := a.getAPIKey(ctx, hashAPIKey(apiKey))
	if err != nil {
		return UserAPIKey{}, authenticationError("API KEY unavailable")
	}
	if err := a.ensureAPIKeyUserActive(ctx, binding.UserID); err != nil {
		return UserAPIKey{}, err
	}
	return binding, nil
}

func bearerToken(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	parts := strings.Fields(value)
	if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
		return strings.TrimSpace(parts[1])
	}
	return ""
}

func (a *App) ensureAPIKeyUserActive(ctx context.Context, userID int) error {
	user, err := a.getUser(ctx, userID)
	if err != nil {
		return err
	}
	if user.DisabledAt != nil {
		return authenticationError("API KEY unavailable")
	}
	user, err = a.ensureQuotaMonth(ctx, user)
	if err != nil {
		return err
	}
	if user.QuotaPausedAt != nil && quotaHasAvailable(user) {
		_ = a.restoreQuotaPausedUserIfAvailable(ctx, user.ID)
		user, err = a.getUser(ctx, user.ID)
		if err != nil {
			return err
		}
	}
	if user.QuotaPausedAt != nil || !quotaHasAvailable(user) {
		_ = a.pauseUserKeysForQuota(ctx, user.ID, quotaPauseReasonExhausted)
		return forbiddenError("User quota exhausted; API KEY paused")
	}
	return nil
}

func (a *App) handleModelProxyModels(w http.ResponseWriter, r *http.Request, apiKey UserAPIKey) error {
	if err := a.ensureAPIKeyAuthPoolSelected(r.Context(), apiKey.APIKeyHash); err != nil {
		return err
	}
	cfg, err := a.loadConfig(r.Context())
	if err != nil {
		return err
	}
	proxyTarget, upstreamAPIKey, useProxyHeader, err := modelProxyTarget(cfg, strings.TrimSpace(*apiKey.APIKey))
	if err != nil {
		return err
	}
	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+upstreamAPIKey)
	if useProxyHeader {
		headers.Set(authPoolProxyAPIKeyHashHeader, apiKey.APIKeyHash)
	}
	response, payload, err := doJSON(r.Context(), httpClient(modelListTimeout), http.MethodGet, makeURL(proxyTarget.CPAURL, "/v1/models", r.URL.Query()), headers, nil)
	if err != nil {
		return validationError("CPA model list request failed: " + err.Error())
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		writeRawProxyResponse(w, response, payload)
		return nil
	}
	filters, err := a.authPoolModelFiltersForAPIKeys(r.Context(), []string{apiKey.APIKeyHash})
	if err != nil {
		return err
	}
	filter, hasFilter := filters[apiKey.APIKeyHash]
	if !hasFilter {
		writeRawProxyResponse(w, response, payload)
		return nil
	}
	filtered, err := filterRawModelItems(payload, filter)
	if err != nil {
		return validationError("Invalid CPA model list response")
	}
	writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": filtered})
	return nil
}

func filterRawModelItems(payload []byte, filter map[string]bool) ([]any, error) {
	var raw any
	if err := json.Unmarshal(payload, &raw); err != nil {
		return nil, err
	}
	items, err := extractAvailableModelItems(raw)
	if err != nil {
		return nil, err
	}
	filtered := []any{}
	for _, item := range items {
		model := parseAvailableModel(item, AvailableModelSource{})
		if model == nil || !authPoolModelFilterAllows(filter, model.ID) {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered, nil
}

func (a *App) handleModelProxyForward(w http.ResponseWriter, r *http.Request, apiKey UserAPIKey) error {
	if err := a.ensureAPIKeyAuthPoolSelected(r.Context(), apiKey.APIKeyHash); err != nil {
		return err
	}
	body, err := readLimitedProxyBody(r)
	if err != nil {
		return validationError("Request body too large")
	}
	if model := modelFromProxyBody(body); model != "" {
		if err := a.ensureAPIKeyModelAllowedByPool(r.Context(), apiKey.APIKeyHash, model); err != nil {
			return err
		}
	}
	cfg, err := a.loadConfig(r.Context())
	if err != nil {
		return err
	}
	proxyTarget, upstreamAPIKey, useProxyHeader, err := modelProxyTarget(cfg, strings.TrimSpace(*apiKey.APIKey))
	if err != nil {
		return err
	}
	target := makeURL(proxyTarget.CPAURL, r.URL.Path, r.URL.Query())
	request, err := http.NewRequestWithContext(r.Context(), r.Method, target, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header = modelProxyRequestHeaders(r, upstreamAPIKey, apiKey.APIKeyHash, useProxyHeader)
	requestID := newModelProxyRequestID()
	request.Header.Set("X-Request-Id", requestID)
	request.Header.Set("X-CPA-Helper-Request-Id", requestID)
	response, err := httpClient(0).Do(request)
	if err != nil {
		return validationError("CPA request failed: " + err.Error())
	}
	defer response.Body.Close()
	if shouldBufferModelProxyResponse(response) {
		payload, err := io.ReadAll(response.Body)
		if err != nil {
			return err
		}
		responseRequestID := modelProxyRequestIDFromResponse(response, payload)
		if err := a.recordModelProxyRequestAttributions(r.Context(), apiKey.APIKeyHash, requestID, responseRequestID); err != nil {
			return err
		}
		writeRawProxyResponse(w, response, payload)
		return nil
	}
	responseRequestID := modelProxyRequestIDFromResponse(response, nil)
	if err := a.recordModelProxyRequestAttributions(r.Context(), apiKey.APIKeyHash, requestID, responseRequestID); err != nil {
		return err
	}
	copyProxyHeaders(w.Header(), response.Header)
	w.WriteHeader(response.StatusCode)
	_, _ = io.Copy(w, response.Body)
	return nil
}

func readLimitedProxyBody(r *http.Request) ([]byte, error) {
	defer r.Body.Close()
	body, err := io.ReadAll(io.LimitReader(r.Body, modelProxyBodyLimit+1))
	if err != nil {
		return nil, err
	}
	if len(body) > modelProxyBodyLimit {
		return nil, http.ErrBodyReadAfterClose
	}
	return body, nil
}

func modelFromProxyBody(body []byte) string {
	if len(bytes.TrimSpace(body)) == 0 {
		return ""
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return ""
	}
	model, _ := payload["model"].(string)
	return strings.TrimSpace(model)
}

func modelProxyTarget(cfg AppConfig, fallbackAPIKey string) (AuthPoolProxyTargetConfig, string, bool, error) {
	if target, ok := activeAuthPoolProxyTarget(cfg); ok {
		apiKey := strings.TrimSpace(target.APIKey)
		if apiKey == "" {
			return AuthPoolProxyTargetConfig{}, "", false, validationError("Set CPA forwarding API KEY in plugin settings first")
		}
		return target, apiKey, true, nil
	}
	apiKey := strings.TrimSpace(fallbackAPIKey)
	if apiKey == "" {
		return AuthPoolProxyTargetConfig{}, "", false, validationError("API KEY unavailable")
	}
	cpaURL := strings.TrimRight(strings.TrimSpace(cfg.Collector.CLIProxyURL), "/")
	if cpaURL == "" {
		return AuthPoolProxyTargetConfig{}, "", false, validationError("CPA URL is required")
	}
	return AuthPoolProxyTargetConfig{CPAURL: cpaURL}, apiKey, false, nil
}

func modelProxyRequestHeaders(r *http.Request, apiKey string, apiKeyHash string, includeHelperHash bool) http.Header {
	headers := http.Header{}
	for key, values := range r.Header {
		lower := strings.ToLower(key)
		if hopByHopHeaders[lower] || lower == "authorization" || lower == "x-api-key" || lower == "cookie" || lower == "content-length" {
			continue
		}
		for _, value := range values {
			headers.Add(key, value)
		}
	}
	if includeHelperHash {
		headers.Set(authPoolProxyAPIKeyHashHeader, strings.TrimSpace(apiKeyHash))
	}
	if strings.HasSuffix(strings.ToLower(strings.TrimRight(r.URL.Path, "/")), "/messages") {
		headers.Set("x-api-key", apiKey)
		if strings.TrimSpace(headers.Get("anthropic-version")) == "" {
			headers.Set("anthropic-version", "2023-06-01")
		}
		return headers
	}
	headers.Set("Authorization", "Bearer "+apiKey)
	return headers
}

func shouldBufferModelProxyResponse(response *http.Response) bool {
	if response == nil {
		return false
	}
	contentType := strings.ToLower(response.Header.Get("Content-Type"))
	if strings.Contains(contentType, "text/event-stream") {
		return false
	}
	if strings.Contains(contentType, "application/json") || strings.Contains(contentType, "+json") {
		return response.ContentLength < 0 || response.ContentLength <= modelProxyBodyLimit
	}
	return false
}

func copyProxyHeaders(target, source http.Header) {
	for key, values := range source {
		if hopByHopHeaders[strings.ToLower(key)] || strings.EqualFold(key, "content-length") {
			continue
		}
		for _, value := range values {
			target.Add(key, value)
		}
	}
}

func writeRawProxyResponse(w http.ResponseWriter, response *http.Response, payload []byte) {
	copyProxyHeaders(w.Header(), response.Header)
	w.WriteHeader(response.StatusCode)
	_, _ = w.Write(payload)
}
