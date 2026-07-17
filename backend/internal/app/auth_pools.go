package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

const authPoolPluginID = "cpa-auth-pool"

type authPool struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Description  string   `json:"description,omitempty"`
	AuthIDs      []string `json:"auth_ids"`
	AccountTypes []string `json:"account_types,omitempty"`
	Models       []string `json:"models,omitempty"`
	Enabled      bool     `json:"enabled"`
	Accounts     []any    `json:"accounts,omitempty"`
}

type authPoolBinding struct {
	APIKeyHash string `json:"api_key_hash"`
	PoolID     string `json:"pool_id"`
	UserID     int    `json:"user_id,omitempty"`
	Username   string `json:"username,omitempty"`
}

type authPoolStatus struct {
	Pools           []authPool        `json:"pools"`
	Bindings        []authPoolBinding `json:"bindings"`
	PluginInstalled bool              `json:"plugin_installed"`
	PluginError     string            `json:"plugin_error,omitempty"`
}

type authPoolAccountsResponse struct {
	Items []keeperAccountResponse `json:"items"`
}

type authPoolProxyConfigResponse struct {
	CPAURL          string                    `json:"cpa_url"`
	APIKeySet       bool                      `json:"api_key_set"`
	APIKeyPreview   string                    `json:"api_key_preview"`
	Mode            string                    `json:"mode"`
	PluginInstalled bool                      `json:"plugin_installed"`
	PluginError     string                    `json:"plugin_error,omitempty"`
	Targets         []authPoolProxyTargetView `json:"targets"`
}

type authPoolProxyConfigPayload struct {
	APIKey  *string                      `json:"api_key"`
	Targets []authPoolProxyTargetPayload `json:"targets"`
}

type authPoolAPIKeyAccountPayload struct {
	Provider   string `json:"provider"`
	APIKey     string `json:"api_key"`
	Prefix     string `json:"prefix"`
	BaseURL    string `json:"base_url"`
	ProxyURL   string `json:"proxy_url"`
	Priority   *int   `json:"priority"`
	Websockets *bool  `json:"websockets"`
}

type authPoolAPIKeyAccountResponse struct {
	Provider    string `json:"provider"`
	AccountType string `json:"account_type"`
	Count       int    `json:"count"`
}

type authPoolPayload struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	AuthIDs      []string `json:"auth_ids"`
	AccountTypes []string `json:"account_types"`
	Models       []string `json:"models,omitempty"`
}

type authPoolBindingPayload struct {
	APIKeyHash string `json:"api_key_hash"`
	PoolID     string `json:"pool_id"`
}

func (a *App) handleAuthPools(w http.ResponseWriter, r *http.Request) error {
	user, err := a.currentUser(r.Context(), r)
	if err != nil {
		return err
	}
	parts := authPoolPathParts(r.URL.Path)
	if len(parts) == 0 {
		switch r.Method {
		case http.MethodGet:
			status, err := a.authPoolStatus(r.Context(), user)
			if err != nil {
				return err
			}
			writeJSON(w, http.StatusOK, status)
			return nil
		case http.MethodPost:
			if !user.IsAdmin {
				return forbiddenError("admin required")
			}
			var payload authPoolPayload
			if err := decodeJSON(r, &payload); err != nil {
				return err
			}
			pool, err := a.upsertAuthPool(r.Context(), payload)
			if err != nil {
				return err
			}
			writeJSON(w, http.StatusOK, pool)
			return nil
		default:
			return methodNotAllowed()
		}
	}
	if len(parts) == 1 && parts[0] == "accounts" {
		if r.Method != http.MethodGet {
			return methodNotAllowed()
		}
		if !user.IsAdmin {
			return forbiddenError("admin required")
		}
		accounts, err := a.listAuthPoolAccounts(r.Context())
		if err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, authPoolAccountsResponse{Items: keeperAccountResponses(accounts, nil)})
		return nil
	}
	if len(parts) == 2 && parts[0] == "accounts" && parts[1] == "api-key" {
		if r.Method != http.MethodPost {
			return methodNotAllowed()
		}
		if !user.IsAdmin {
			return forbiddenError("admin required")
		}
		var payload authPoolAPIKeyAccountPayload
		if err := decodeJSON(r, &payload); err != nil {
			return err
		}
		result, err := a.addAuthPoolAPIKeyAccount(r.Context(), payload)
		if err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, result)
		return nil
	}
	if len(parts) == 1 && parts[0] == "proxy-config" {
		if !user.IsAdmin {
			return forbiddenError("admin required")
		}
		switch r.Method {
		case http.MethodGet:
			config, err := a.authPoolProxyConfig(r.Context())
			if err != nil {
				return err
			}
			writeJSON(w, http.StatusOK, config)
			return nil
		case http.MethodPut:
			var payload authPoolProxyConfigPayload
			if err := decodeJSON(r, &payload); err != nil {
				return err
			}
			config, err := a.updateAuthPoolProxyConfig(r.Context(), payload)
			if err != nil {
				return err
			}
			writeJSON(w, http.StatusOK, config)
			return nil
		default:
			return methodNotAllowed()
		}
	}
	if len(parts) == 1 && parts[0] == "bindings" {
		if r.Method != http.MethodPost {
			return methodNotAllowed()
		}
		var payload authPoolBindingPayload
		if err := decodeJSON(r, &payload); err != nil {
			return err
		}
		binding, err := a.bindAPIKeyToAuthPool(r.Context(), user, payload)
		if err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, binding)
		return nil
	}
	if len(parts) == 2 && parts[0] == "bindings" {
		if r.Method != http.MethodDelete {
			return methodNotAllowed()
		}
		if err := a.unbindAPIKeyFromAuthPool(r.Context(), user, parts[1]); err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
		return nil
	}
	if len(parts) == 1 {
		if r.Method != http.MethodDelete {
			return methodNotAllowed()
		}
		if !user.IsAdmin {
			return forbiddenError("admin required")
		}
		if err := a.deleteAuthPool(r.Context(), parts[0]); err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
		return nil
	}
	return notFoundError("auth pool route not found")
}

func authPoolPathParts(path string) []string {
	if path == "/api/auth-pools" || path == "/api/auth-pools/" {
		return nil
	}
	return splitPath(path, "/api/auth-pools/")
}

func (a *App) authPoolStatus(ctx context.Context, user *AuthUser) (authPoolStatus, error) {
	var status authPoolStatus
	if err := a.authPoolPluginRequest(ctx, http.MethodGet, "/status", nil, &status); err != nil {
		status.PluginInstalled = false
		status.PluginError = err.Error()
		return status, nil
	}
	status.PluginInstalled = true
	if authPoolsNeedModelSync(status.Pools) {
		go func() {
			syncCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			_ = a.syncAuthPoolModels(syncCtx)
		}()
	}
	localBindings, err := a.localAuthPoolBindings(ctx, user)
	if err != nil {
		return authPoolStatus{}, err
	}
	if len(localBindings) > 0 {
		status.Bindings = localBindings
	}
	return status, nil
}

func (a *App) upsertAuthPool(ctx context.Context, payload authPoolPayload) (authPool, error) {
	pool := authPool{ID: strings.TrimSpace(payload.ID), Name: strings.TrimSpace(payload.Name), Description: strings.TrimSpace(payload.Description), AuthIDs: payload.AuthIDs, AccountTypes: payload.AccountTypes, Models: payload.Models, Enabled: true}
	if pool.ID == "" || pool.Name == "" {
		return authPool{}, validationError("pool id and name are required")
	}
	var response struct {
		Pool authPool `json:"pool"`
	}
	if err := a.authPoolPluginRequest(ctx, http.MethodPost, "/pools", pool, &response); err != nil {
		return authPool{}, err
	}
	_ = a.syncAuthPoolModels(ctx)
	a.refreshChannelStatusAfterChange()
	return response.Pool, nil
}

func (a *App) deleteAuthPool(ctx context.Context, id string) error {
	if err := a.authPoolPluginRequest(ctx, http.MethodDelete, "/pools?id="+urlQueryEscape(id), nil, nil); err != nil {
		return err
	}
	_, err := a.db.ExecContext(ctx, `DELETE FROM user_api_key_pools WHERE pool_id = ?`, id)
	if err == nil {
		_ = a.syncAuthPoolModels(ctx)
		a.refreshChannelStatusAfterChange()
	}
	return err
}

func (a *App) authPoolProxyConfig(ctx context.Context) (authPoolProxyConfigResponse, error) {
	cfg, err := a.loadConfig(ctx)
	if err != nil {
		return authPoolProxyConfigResponse{}, err
	}
	response := authPoolProxyConfigResponse{
		CPAURL:          cfg.Collector.CLIProxyURL,
		APIKeySet:       strings.TrimSpace(cfg.AuthPoolProxyAPIKey) != "",
		APIKeyPreview:   maskAPIKey(cfg.AuthPoolProxyAPIKey),
		Mode:            "legacy",
		PluginInstalled: false,
		Targets:         authPoolProxyTargetViews(cfg.AuthPoolProxyTargets),
	}
	if target, ok := activeAuthPoolProxyTarget(cfg); ok {
		response.Mode = "proxy"
		response.CPAURL = target.CPAURL
		response.APIKeySet = strings.TrimSpace(target.APIKey) != ""
		response.APIKeyPreview = maskAPIKey(target.APIKey)
	}
	if err := a.authPoolPluginRequestWithConfig(ctx, cfg, http.MethodGet, "/status", nil, nil); err != nil {
		response.PluginError = err.Error()
	} else {
		response.PluginInstalled = true
	}
	return response, nil
}

func (a *App) updateAuthPoolProxyConfig(ctx context.Context, payload authPoolProxyConfigPayload) (authPoolProxyConfigResponse, error) {
	cfg, err := a.loadConfig(ctx)
	if err != nil {
		return authPoolProxyConfigResponse{}, err
	}
	if payload.APIKey != nil {
		cfg.AuthPoolProxyAPIKey = strings.TrimSpace(*payload.APIKey)
	}
	if payload.Targets != nil {
		cfg.AuthPoolProxyTargets = mergeAuthPoolProxyTargetSecrets(cfg.AuthPoolProxyTargets, authPoolProxyTargetConfigs(payload.Targets))
		cfg.AuthPoolProxyAPIKey = ""
	}
	if err := a.registerAuthPoolProxyTargets(ctx, cfg); err != nil {
		return authPoolProxyConfigResponse{}, err
	}
	if err := a.saveConfig(ctx, cfg); err != nil {
		return authPoolProxyConfigResponse{}, err
	}
	return a.authPoolProxyConfig(ctx)
}

func authPoolProxyTargetViews(targets []AuthPoolProxyTargetConfig) []authPoolProxyTargetView {
	items := normalizeAuthPoolProxyTargets(targets)
	views := make([]authPoolProxyTargetView, 0, len(items))
	for _, target := range items {
		views = append(views, authPoolProxyTargetView{
			ID:                   target.ID,
			Name:                 target.Name,
			CPAURL:               target.CPAURL,
			ManagementKeySet:     strings.TrimSpace(target.ManagementKey) != "",
			ManagementKeyPreview: maskAPIKey(target.ManagementKey),
			APIKeySet:            strings.TrimSpace(target.APIKey) != "",
			APIKeyPreview:        maskAPIKey(target.APIKey),
			Enabled:              target.Enabled,
		})
	}
	return views
}

func authPoolProxyTargetConfigs(payload []authPoolProxyTargetPayload) []AuthPoolProxyTargetConfig {
	items := make([]AuthPoolProxyTargetConfig, 0, len(payload))
	for _, target := range payload {
		items = append(items, AuthPoolProxyTargetConfig{
			ID:            strings.TrimSpace(target.ID),
			Name:          strings.TrimSpace(target.Name),
			CPAURL:        strings.TrimRight(strings.TrimSpace(target.CPAURL), "/"),
			ManagementKey: strings.TrimSpace(target.ManagementKey),
			APIKey:        strings.TrimSpace(target.APIKey),
			Enabled:       target.Enabled,
		})
	}
	return normalizeAuthPoolProxyTargets(items)
}

func mergeAuthPoolProxyTargetSecrets(existing []AuthPoolProxyTargetConfig, next []AuthPoolProxyTargetConfig) []AuthPoolProxyTargetConfig {
	byID := map[string]AuthPoolProxyTargetConfig{}
	for _, target := range normalizeAuthPoolProxyTargets(existing) {
		byID[target.ID] = target
	}
	merged := make([]AuthPoolProxyTargetConfig, 0, len(next))
	for _, target := range normalizeAuthPoolProxyTargets(next) {
		if old, ok := byID[target.ID]; ok {
			if strings.TrimSpace(target.ManagementKey) == "" {
				target.ManagementKey = old.ManagementKey
			}
			if strings.TrimSpace(target.APIKey) == "" {
				target.APIKey = old.APIKey
			}
		}
		merged = append(merged, target)
	}
	return merged
}

func (a *App) registerAuthPoolProxyTargets(ctx context.Context, cfg AppConfig) error {
	for _, target := range normalizeAuthPoolProxyTargets(cfg.AuthPoolProxyTargets) {
		if !target.Enabled || strings.TrimSpace(target.APIKey) == "" {
			continue
		}
		if strings.TrimSpace(target.CPAURL) == "" || strings.TrimSpace(target.ManagementKey) == "" {
			return validationError("CPA URL and management key are required for enabled proxy targets")
		}
		if err := a.authPoolPluginRequestWithTarget(ctx, target, http.MethodPost, "/proxy-keys", map[string]any{"api_key": target.APIKey}, nil); err != nil {
			return err
		}
	}
	if len(normalizeAuthPoolProxyTargets(cfg.AuthPoolProxyTargets)) == 0 && strings.TrimSpace(cfg.AuthPoolProxyAPIKey) != "" {
		if target, ok := activeAuthPoolProxyTarget(cfg); ok {
			return a.authPoolPluginRequestWithTarget(ctx, target, http.MethodPost, "/proxy-keys", map[string]any{"api_key": target.APIKey}, nil)
		}
	}
	return nil
}

func maskAPIKey(apiKey string) string {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return ""
	}
	runes := []rune(apiKey)
	if len(runes) <= 10 {
		return strings.Repeat("*", len(runes))
	}
	starCount := len(runes) - 9
	if starCount < 3 {
		starCount = 3
	}
	return string(runes[:5]) + strings.Repeat("*", starCount) + string(runes[len(runes)-4:])
}

func (a *App) addAuthPoolAPIKeyAccount(ctx context.Context, payload authPoolAPIKeyAccountPayload) (authPoolAPIKeyAccountResponse, error) {
	provider, accountType, endpoint, responseKey, defaultBaseURL, err := normalizeAuthPoolAPIKeyProvider(payload.Provider)
	if err != nil {
		return authPoolAPIKeyAccountResponse{}, err
	}
	apiKey := strings.TrimSpace(payload.APIKey)
	if apiKey == "" {
		return authPoolAPIKeyAccountResponse{}, validationError("api key is required")
	}
	baseURL := strings.TrimSpace(payload.BaseURL)
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	entry := map[string]any{"api-key": apiKey}
	if prefix := strings.TrimSpace(payload.Prefix); prefix != "" {
		entry["prefix"] = prefix
	}
	if baseURL != "" {
		entry["base-url"] = baseURL
	}
	if proxyURL := strings.TrimSpace(payload.ProxyURL); proxyURL != "" {
		entry["proxy-url"] = proxyURL
	}
	if payload.Priority != nil {
		entry["priority"] = *payload.Priority
	}
	if provider == "xai" && payload.Websockets != nil {
		entry["websockets"] = *payload.Websockets
	}

	var current map[string]json.RawMessage
	if err := a.cpaManagementJSON(ctx, http.MethodGet, endpoint, nil, &current); err != nil {
		return authPoolAPIKeyAccountResponse{}, err
	}
	items := upsertCPAConfigKeyItem(cpaConfigKeyItems(current, responseKey), entry, apiKey)
	if err := a.cpaManagementJSON(ctx, http.MethodPut, endpoint, map[string]any{"items": items}, nil); err != nil {
		return authPoolAPIKeyAccountResponse{}, err
	}
	_ = a.syncAuthPoolModels(ctx)
	a.refreshChannelStatusAfterChange()
	return authPoolAPIKeyAccountResponse{Provider: provider, AccountType: accountType, Count: len(items)}, nil
}

func normalizeAuthPoolAPIKeyProvider(value string) (provider string, accountType string, endpoint string, responseKey string, defaultBaseURL string, err error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "gemini", "google", "gemini-api-key", "google-api-key":
		return "gemini", "gemini", "/v0/management/gemini-api-key", "gemini-api-key", "", nil
	case "grok", "xai", "x.ai", "supergrok", "xai-api-key":
		return "xai", "grok", "/v0/management/xai-api-key", "xai-api-key", "https://api.x.ai/v1", nil
	default:
		return "", "", "", "", "", validationError("unsupported account provider")
	}
}

func cpaConfigKeyItems(raw map[string]json.RawMessage, responseKey string) []map[string]any {
	if raw == nil {
		return []map[string]any{}
	}
	var items []map[string]any
	if data := raw[responseKey]; len(data) > 0 {
		_ = json.Unmarshal(data, &items)
	}
	if items == nil {
		items = []map[string]any{}
	}
	return items
}

func upsertCPAConfigKeyItem(items []map[string]any, entry map[string]any, apiKey string) []map[string]any {
	apiKey = strings.TrimSpace(apiKey)
	for index, item := range items {
		if strings.TrimSpace(keeperString(item["api-key"])) == apiKey {
			items[index] = entry
			return items
		}
	}
	return append(items, entry)
}

func (a *App) bindAPIKeyToAuthPool(ctx context.Context, user *AuthUser, payload authPoolBindingPayload) (authPoolBinding, error) {
	apiKeyHash := strings.TrimSpace(payload.APIKeyHash)
	poolID := strings.TrimSpace(payload.PoolID)
	if apiKeyHash == "" || poolID == "" {
		return authPoolBinding{}, validationError("api key hash and pool id are required")
	}
	if err := a.ensureAPIKeyPoolAccess(ctx, user, apiKeyHash); err != nil {
		return authPoolBinding{}, err
	}
	binding := authPoolBinding{APIKeyHash: apiKeyHash, PoolID: poolID, UserID: user.ID, Username: user.Username}
	var response struct {
		Binding authPoolBinding `json:"binding"`
	}
	if err := a.authPoolPluginRequest(ctx, http.MethodPost, "/bindings", binding, &response); err != nil {
		return authPoolBinding{}, err
	}
	now := dbTime(time.Now())
	_, err := a.db.ExecContext(ctx, `
		INSERT INTO user_api_key_pools (api_key_hash, pool_id, created_at, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(api_key_hash) DO UPDATE SET pool_id = excluded.pool_id, updated_at = excluded.updated_at
	`, apiKeyHash, poolID, now, now)
	if err != nil {
		return authPoolBinding{}, err
	}
	return response.Binding, nil
}

func (a *App) unbindAPIKeyFromAuthPool(ctx context.Context, user *AuthUser, apiKeyHash string) error {
	apiKeyHash = strings.TrimSpace(apiKeyHash)
	if apiKeyHash == "" {
		return validationError("api key hash is required")
	}
	if err := a.ensureAPIKeyPoolAccess(ctx, user, apiKeyHash); err != nil {
		return err
	}
	if err := a.authPoolPluginRequest(ctx, http.MethodDelete, "/bindings?api_key_hash="+urlQueryEscape(apiKeyHash), nil, nil); err != nil {
		return err
	}
	_, err := a.db.ExecContext(ctx, `DELETE FROM user_api_key_pools WHERE api_key_hash = ?`, apiKeyHash)
	return err
}

func (a *App) ensureAPIKeyPoolAccess(ctx context.Context, user *AuthUser, apiKeyHash string) error {
	var exists int
	query := `SELECT COUNT(*) FROM user_api_keys WHERE api_key_hash = ?`
	args := []any{apiKeyHash}
	if user != nil && !user.IsAdmin {
		query += ` AND user_id = ?`
		args = append(args, user.ID)
	}
	if err := a.db.QueryRowContext(ctx, query, args...).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return notFoundError("api key not found")
	}
	return nil
}

func (a *App) localAuthPoolBindings(ctx context.Context, user *AuthUser) ([]authPoolBinding, error) {
	query := `
		SELECT p.api_key_hash, p.pool_id, k.user_id, COALESCE(u.username, '')
		FROM user_api_key_pools p
		JOIN user_api_keys k ON k.api_key_hash = p.api_key_hash
		LEFT JOIN users u ON u.id = k.user_id
	`
	args := []any{}
	if user != nil && !user.IsAdmin {
		query += ` WHERE k.user_id = ?`
		args = append(args, user.ID)
	}
	query += ` ORDER BY COALESCE(u.username, ''), p.api_key_hash`
	rows, err := a.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	bindings := []authPoolBinding{}
	for rows.Next() {
		var binding authPoolBinding
		if err := rows.Scan(&binding.APIKeyHash, &binding.PoolID, &binding.UserID, &binding.Username); err != nil {
			return nil, err
		}
		bindings = append(bindings, binding)
	}
	return bindings, rows.Err()
}

func (a *App) localAuthPoolIDForAPIKey(ctx context.Context, apiKeyHash string) (string, bool, error) {
	apiKeyHash = strings.TrimSpace(apiKeyHash)
	if apiKeyHash == "" {
		return "", false, nil
	}
	rows, err := a.db.QueryContext(ctx, `SELECT pool_id FROM user_api_key_pools WHERE api_key_hash = ?`, apiKeyHash)
	if err != nil {
		return "", false, err
	}
	defer rows.Close()
	if !rows.Next() {
		return "", false, rows.Err()
	}
	var poolID string
	if err := rows.Scan(&poolID); err != nil {
		return "", false, err
	}
	poolID = strings.TrimSpace(poolID)
	return poolID, poolID != "", rows.Err()
}

func authPoolAllowsModel(pool authPool, model string) bool {
	model = strings.TrimSpace(model)
	if model == "" {
		return false
	}
	for _, allowed := range pool.Models {
		if strings.EqualFold(strings.TrimSpace(allowed), model) {
			return true
		}
	}
	return false
}

func authPoolModelFilterAllows(filter map[string]bool, model string) bool {
	if filter == nil {
		return true
	}
	return filter[strings.ToLower(strings.TrimSpace(model))]
}

func authPoolRequiredError() error {
	return validationError("当前 API KEY 必须先选择请求号池")
}

func (a *App) ensureAPIKeyAuthPoolSelected(ctx context.Context, apiKeyHash string) error {
	cfg, err := a.loadConfig(ctx)
	if err != nil {
		return err
	}
	if !authPoolProxyModeEnabled(cfg) {
		return nil
	}
	_, bound, err := a.localAuthPoolIDForAPIKey(ctx, apiKeyHash)
	if err != nil {
		return err
	}
	if !bound {
		return authPoolRequiredError()
	}
	return nil
}

func authPoolModelFilter(pool authPool) map[string]bool {
	filter := map[string]bool{}
	for _, model := range pool.Models {
		model = strings.TrimSpace(model)
		if model != "" {
			filter[strings.ToLower(model)] = true
		}
	}
	return filter
}

func (a *App) authPoolModelFiltersForAPIKeys(ctx context.Context, apiKeyHashes []string) (map[string]map[string]bool, error) {
	filters := map[string]map[string]bool{}
	cfg, err := a.loadConfig(ctx)
	if err != nil {
		return nil, err
	}
	if !authPoolProxyModeEnabled(cfg) {
		return filters, nil
	}
	if len(apiKeyHashes) == 0 {
		return filters, nil
	}
	requested := map[string]bool{}
	for _, hash := range apiKeyHashes {
		if hash = strings.TrimSpace(hash); hash != "" {
			requested[hash] = true
		}
	}
	bindings, err := a.localAuthPoolBindings(ctx, nil)
	if err != nil {
		return nil, err
	}
	keyPools := map[string]string{}
	for _, binding := range bindings {
		if requested[binding.APIKeyHash] {
			keyPools[binding.APIKeyHash] = strings.TrimSpace(binding.PoolID)
		}
	}
	for apiKeyHash := range requested {
		if strings.TrimSpace(keyPools[apiKeyHash]) == "" {
			return nil, authPoolRequiredError()
		}
	}
	var status authPoolStatus
	if err := a.authPoolPluginRequestWithConfig(ctx, cfg, http.MethodGet, "/status", nil, &status); err != nil {
		return nil, err
	}
	poolFilters := map[string]map[string]bool{}
	for _, pool := range status.Pools {
		poolID := strings.TrimSpace(pool.ID)
		if poolID != "" {
			poolFilters[poolID] = authPoolModelFilter(pool)
		}
	}
	for apiKeyHash, poolID := range keyPools {
		filters[apiKeyHash] = poolFilters[poolID]
		if filters[apiKeyHash] == nil {
			filters[apiKeyHash] = map[string]bool{}
		}
	}
	return filters, nil
}

func (a *App) ensureAPIKeyModelAllowedByPool(ctx context.Context, apiKeyHash string, model string) error {
	cfg, err := a.loadConfig(ctx)
	if err != nil {
		return err
	}
	if !authPoolProxyModeEnabled(cfg) {
		return nil
	}
	poolID, bound, err := a.localAuthPoolIDForAPIKey(ctx, apiKeyHash)
	if err != nil {
		return err
	}
	if !bound {
		return authPoolRequiredError()
	}
	var status authPoolStatus
	if err := a.authPoolPluginRequestWithConfig(ctx, cfg, http.MethodGet, "/status", nil, &status); err != nil {
		return err
	}
	for _, pool := range status.Pools {
		if strings.TrimSpace(pool.ID) != poolID {
			continue
		}
		if authPoolAllowsModel(pool, model) {
			return nil
		}
		return validationError("测试模型不属于当前 API KEY 绑定的号池")
	}
	return validationError("当前 API KEY 绑定的号池不存在")
}

func (a *App) authPoolPluginRequest(ctx context.Context, method, path string, body any, target any) error {
	cfg, err := a.loadConfig(ctx)
	if err != nil {
		return err
	}
	return a.authPoolPluginRequestWithConfig(ctx, cfg, method, path, body, target)
}

func (a *App) authPoolPluginRequestWithConfig(ctx context.Context, cfg AppConfig, method, path string, body any, target any) error {
	cpaTarget, ok := primaryCPAManagementTarget(cfg)
	if !ok {
		return validationError("CPA URL and management key are required")
	}
	return a.authPoolPluginRequestWithTarget(ctx, cpaTarget, method, path, body, target)
}

func (a *App) authPoolPluginRequestWithTarget(ctx context.Context, cpaTarget AuthPoolProxyTargetConfig, method, path string, body any, target any) error {
	if strings.TrimSpace(cpaTarget.CPAURL) == "" || strings.TrimSpace(cpaTarget.ManagementKey) == "" {
		return validationError("CPA URL and management key are required")
	}
	response, payload, err := doJSON(ctx, httpClient(apiKeySyncTimeout), method, makeURL(cpaTarget.CPAURL, "/v0/management/plugins/"+authPoolPluginID+path, nil), managementHeaders(cpaTarget.ManagementKey), body)
	if err != nil {
		return remoteAPIKeyError("auth-pool request failed", err)
	}
	if response.StatusCode == http.StatusNotFound || response.StatusCode == http.StatusMethodNotAllowed {
		return validationError("CPA cpa-auth-pool plugin is not installed or enabled")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return validationError(fmt.Sprintf("auth-pool request failed: HTTP %d %s", response.StatusCode, strings.TrimSpace(string(payload))))
	}
	if target != nil && len(payload) > 0 {
		if err := json.Unmarshal(payload, target); err != nil {
			return validationError("auth-pool returned invalid JSON")
		}
	}
	return nil
}

func (a *App) cpaManagementJSON(ctx context.Context, method, path string, body any, target any) error {
	cfg, err := a.loadConfig(ctx)
	if err != nil {
		return err
	}
	cpaTarget, ok := primaryCPAManagementTarget(cfg)
	if !ok {
		return validationError("CPA URL and management key are required")
	}
	response, payload, err := doJSON(ctx, httpClient(apiKeySyncTimeout), method, makeURL(cpaTarget.CPAURL, path, nil), managementHeaders(cpaTarget.ManagementKey), body)
	if err != nil {
		return remoteAPIKeyError("CPA management request failed", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return validationError(fmt.Sprintf("CPA management request failed: HTTP %d %s", response.StatusCode, strings.TrimSpace(string(payload))))
	}
	if target != nil && len(payload) > 0 {
		if err := json.Unmarshal(payload, target); err != nil {
			return validationError("CPA management returned invalid JSON")
		}
	}
	return nil
}

func (a *App) syncAuthPoolModels(ctx context.Context) error {
	var status authPoolStatus
	if err := a.authPoolPluginRequest(ctx, http.MethodGet, "/status", nil, &status); err != nil {
		return err
	}
	accounts, err := a.listAuthPoolAccounts(ctx)
	if err != nil {
		return err
	}
	authIDs := authIDsForModelSync(status.Pools, accounts)
	if len(authIDs) == 0 {
		return nil
	}
	cfg, err := a.loadConfig(ctx)
	if err != nil {
		return err
	}
	authModels := map[string][]string{}
	fetched := 0
	for _, authID := range authIDs {
		models, err := a.fetchAuthFileModels(ctx, cfg, authID)
		if err != nil {
			continue
		}
		authModels[authID] = models
		fetched++
	}
	if fetched == 0 {
		return nil
	}
	poolModels := authPoolModelsForSync(status.Pools, accounts, authModels)
	return a.authPoolPluginRequest(ctx, http.MethodPost, "/auth-models", map[string]any{"auth_models": authModels, "pool_models": poolModels}, nil)
}

func authPoolsNeedModelSync(pools []authPool) bool {
	for _, pool := range pools {
		if (len(pool.AuthIDs) > 0 || len(pool.AccountTypes) > 0) && len(pool.Models) == 0 {
			return true
		}
	}
	return false
}

func (a *App) listAuthPoolAccounts(ctx context.Context) ([]keeperAccount, error) {
	localAccounts, err := a.listKeeperAccounts(ctx)
	if err != nil {
		return nil, err
	}
	byName := make(map[string]keeperAccount, len(localAccounts))
	for _, account := range localAccounts {
		if name := strings.TrimSpace(account.Name); name != "" {
			byName[name] = account
		}
	}
	cfg, err := a.loadConfig(ctx)
	if err != nil {
		return nil, err
	}
	remoteAccounts, err := a.listKeeperRemoteAuthFiles(ctx, cfg)
	if err != nil {
		if len(localAccounts) > 0 {
			return localAccounts, nil
		}
		return nil, err
	}
	for _, item := range remoteAccounts {
		remote := authPoolAccountFromRemoteAuthFile(item)
		name := strings.TrimSpace(remote.Name)
		if name == "" {
			continue
		}
		if local, ok := byName[name]; ok {
			byName[name] = mergeAuthPoolAccount(local, remote)
			continue
		}
		byName[name] = remote
	}
	accounts := make([]keeperAccount, 0, len(byName))
	for _, account := range byName {
		accounts = append(accounts, account)
	}
	sort.Slice(accounts, func(i, j int) bool {
		leftType := strings.ToLower(stringPtrValue(accounts[i].AccountType))
		rightType := strings.ToLower(stringPtrValue(accounts[j].AccountType))
		if leftType == rightType {
			return strings.ToLower(accounts[i].Name) < strings.ToLower(accounts[j].Name)
		}
		return leftType < rightType
	})
	return accounts, nil
}

func authPoolAccountFromRemoteAuthFile(item map[string]any) keeperAccount {
	accountType := authPoolRemoteAccountType(item)
	return keeperAccount{
		Name:           firstKeeperString(item, "name", "file_name", "filename", "id", "path"),
		Email:          keeperStringPtr(item["email"], item["account_email"], item["user_email"], item["username"], item["subject"]),
		AuthIndex:      keeperRemoteAuthIndex(item),
		AccountType:    &accountType,
		Disabled:       keeperBool(item["disabled"]),
		Priority:       keeperIntPtr(item["priority"]),
		LastStatusCode: keeperIntPtr(item["last_status_code"], item["status_code"]),
		LastError:      keeperStringPtr(item["last_error"], item["error"]),
	}
}

func mergeAuthPoolAccount(local keeperAccount, remote keeperAccount) keeperAccount {
	local.Disabled = remote.Disabled
	if local.Email == nil {
		local.Email = remote.Email
	}
	if local.AuthIndex == nil {
		local.AuthIndex = remote.AuthIndex
	}
	if local.AccountType == nil || stringPtrValue(local.AccountType) == "unknown" {
		local.AccountType = remote.AccountType
	}
	if local.Priority == nil {
		local.Priority = remote.Priority
	}
	if local.LastStatusCode == nil {
		local.LastStatusCode = remote.LastStatusCode
	}
	if local.LastError == nil {
		local.LastError = remote.LastError
	}
	return local
}

func authPoolRemoteAccountType(item map[string]any) string {
	values := []string{}
	for _, key := range []string{"account_type", "accountType", "provider", "type", "kind", "source", "service", "channel", "name", "file_name", "filename", "id"} {
		if value := keeperString(item[key]); value != "" {
			values = append(values, value)
		}
	}
	if path := keeperString(item["path"]); path != "" {
		values = append(values, path)
	}
	text := strings.ToLower(strings.Join(values, " "))
	switch {
	case strings.Contains(text, "gemini") || strings.Contains(text, "google"):
		return "gemini"
	case strings.Contains(text, "grok") || strings.Contains(text, "xai") || strings.Contains(text, "x.ai"):
		return "grok"
	case strings.Contains(text, "claude") || strings.Contains(text, "anthropic"):
		return "claude"
	}
	if value := accountTypeFromKeeperDetail(item, nil); value != nil && strings.TrimSpace(*value) != "unknown" {
		return strings.TrimSpace(*value)
	}
	if value := sanitizeAuthPoolAccountType(text); value != "" {
		return value
	}
	return "unknown"
}

func sanitizeAuthPoolAccountType(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.NewReplacer("-", "_", " ", "_", ".", "_", "@", "_", "/", "_", "\\", "_").Replace(value)
	parts := strings.FieldsFunc(value, func(r rune) bool { return r == '_' })
	cleaned := []string{}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			cleaned = append(cleaned, part)
		}
	}
	return strings.Join(cleaned, "_")
}

func firstKeeperString(item map[string]any, keys ...string) string {
	for _, key := range keys {
		value := keeperString(item[key])
		if value == "" {
			continue
		}
		if key == "path" {
			parts := strings.FieldsFunc(value, func(r rune) bool { return r == '/' || r == '\\' })
			if len(parts) > 0 {
				value = parts[len(parts)-1]
			}
		}
		return value
	}
	return ""
}
func authIDsForModelSync(pools []authPool, accounts []keeperAccount) []string {
	seen := map[string]bool{}
	ids := []string{}
	for _, pool := range pools {
		for _, id := range authIDsForPoolModelSync(pool, accounts) {
			id = strings.TrimSpace(id)
			if id != "" && !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}
	sortStringsCaseInsensitive(ids)
	return ids
}

func authIDsForPoolModelSync(pool authPool, accounts []keeperAccount) []string {
	manualIDs := normalizedLookup(pool.AuthIDs)
	typeIDs := normalizedLookup(pool.AccountTypes)
	seen := map[string]bool{}
	ids := []string{}
	for _, id := range pool.AuthIDs {
		id = strings.TrimSpace(id)
		if id != "" && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	if len(typeIDs) > 0 {
		for _, account := range accounts {
			if !channelAccountMatchesPool(account, manualIDs, typeIDs) {
				continue
			}
			id := strings.TrimSpace(account.Name)
			if id != "" && !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}
	sortStringsCaseInsensitive(ids)
	return ids
}

func authPoolModelsForSync(pools []authPool, accounts []keeperAccount, authModels map[string][]string) map[string][]string {
	poolModels := map[string][]string{}
	for _, pool := range pools {
		poolID := strings.TrimSpace(pool.ID)
		if poolID == "" {
			continue
		}
		seen := map[string]bool{}
		models := []string{}
		for _, authID := range authIDsForPoolModelSync(pool, accounts) {
			for _, model := range authModels[strings.TrimSpace(authID)] {
				model = strings.TrimSpace(model)
				if model == "" || seen[model] {
					continue
				}
				seen[model] = true
				models = append(models, model)
			}
		}
		sortStringsCaseInsensitive(models)
		poolModels[poolID] = models
	}
	return poolModels
}

func (a *App) fetchAuthFileModels(ctx context.Context, cfg AppConfig, authID string) ([]string, error) {
	query := url.Values{}
	query.Set("name", authID)
	response, payload, err := doJSON(ctx, httpClient(modelListTimeout), http.MethodGet, makeURL(cfg.Collector.CLIProxyURL, "/v0/management/auth-files/models", query), managementHeaders(cfg.Collector.ManagementKey), nil)
	if err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("auth models request failed: HTTP %d", response.StatusCode)
	}
	var raw any
	if err := json.Unmarshal(payload, &raw); err != nil {
		return nil, err
	}
	items, err := extractAvailableModelItems(raw)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	models := []string{}
	for _, item := range items {
		model := parseAvailableModel(item, AvailableModelSource{})
		if model == nil || strings.TrimSpace(model.ID) == "" || seen[model.ID] {
			continue
		}
		seen[model.ID] = true
		models = append(models, model.ID)
	}
	sortStringsCaseInsensitive(models)
	return models, nil
}

func sortStringsCaseInsensitive(values []string) {
	sort.Slice(values, func(i, j int) bool {
		left := strings.ToLower(values[i])
		right := strings.ToLower(values[j])
		if left == right {
			return values[i] < values[j]
		}
		return left < right
	})
}

func urlQueryEscape(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, "%", "%25"), " ", "%20")
}

type authPoolProxyTargetView struct {
	ID                   string `json:"id"`
	Name                 string `json:"name"`
	CPAURL               string `json:"cpa_url"`
	ManagementKeySet     bool   `json:"management_key_set"`
	ManagementKeyPreview string `json:"management_key_preview"`
	APIKeySet            bool   `json:"api_key_set"`
	APIKeyPreview        string `json:"api_key_preview"`
	Enabled              bool   `json:"enabled"`
}

type authPoolProxyTargetPayload struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	CPAURL        string `json:"cpa_url"`
	ManagementKey string `json:"management_key"`
	APIKey        string `json:"api_key"`
	Enabled       bool   `json:"enabled"`
}
