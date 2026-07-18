package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"reflect"
	"sort"
	"strings"
	"time"
)

const (
	authPoolPluginID             = "cpa-auth-pool"
	authPoolModelSyncTimeout     = 30 * time.Second
	authPoolModelSyncBackoffBase = 5 * time.Second
	authPoolModelSyncBackoffMax  = 5 * time.Minute
	authPoolResolvedSyncTimeout  = 15 * time.Second
	authPoolResolvedSyncInterval = 30 * time.Second
	authPoolProviderCacheTTL     = 15 * time.Second

	authPoolVisibilityAdminsOnly = "admins_only"
	authPoolVisibilityAllUsers   = "all_users"
	authPoolVisibilitySelected   = "selected_users"
)

type authPool struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Description     string   `json:"description,omitempty"`
	AuthIDs         []string `json:"auth_ids"`
	ResolvedAuthIDs []string `json:"resolved_auth_ids,omitempty"`
	AccountTypes    []string `json:"account_types,omitempty"`
	Providers       []string `json:"providers,omitempty"`
	Models          []string `json:"models,omitempty"`
	Visibility      string   `json:"visibility,omitempty"`
	AllowedUserIDs  []int    `json:"allowed_user_ids"`
	Enabled         bool     `json:"enabled"`
	Accounts        []any    `json:"accounts,omitempty"`
}

type authPoolBinding struct {
	APIKeyHash string `json:"api_key_hash"`
	PoolID     string `json:"pool_id"`
	UserID     int    `json:"user_id,omitempty"`
	Username   string `json:"username,omitempty"`
}

type authPoolStatus struct {
	PluginVersion          string                    `json:"plugin_version,omitempty"`
	ConcurrencyScope       string                    `json:"concurrency_scope,omitempty"`
	ConcurrencyStrategy    string                    `json:"concurrency_strategy,omitempty"`
	SchedulerPriorities    bool                      `json:"scheduler_priorities,omitempty"`
	Pools                  []authPool                `json:"pools"`
	Bindings               []authPoolBinding         `json:"bindings"`
	AuthTypes              map[string]string         `json:"auth_types,omitempty"`
	TypePriorities         map[string]int            `json:"type_priorities,omitempty"`
	AuthPriorityOverrides  map[string]int            `json:"auth_priority_overrides,omitempty"`
	CodexConcurrencyLimits map[string]int            `json:"codex_concurrency_limits,omitempty"`
	Concurrency            *authPoolConcurrency      `json:"concurrency,omitempty"`
	ConcurrencySlots       []authPoolConcurrencySlot `json:"concurrency_slots,omitempty"`
	PluginInstalled        bool                      `json:"plugin_installed"`
	PluginError            string                    `json:"plugin_error,omitempty"`
}

type authPoolConcurrency struct {
	Counts map[string]int `json:"counts"`
	Limits map[string]int `json:"limits"`
}

type authPoolConcurrencySlot struct {
	AuthID           string `json:"auth_id"`
	Tier             string `json:"tier"`
	Count            int    `json:"count"`
	StartedAt        string `json:"started_at,omitempty"`
	ExpiresAt        string `json:"expires_at,omitempty"`
	RemainingSeconds int64  `json:"remaining_seconds,omitempty"`
}

type authPoolAccountsResponse struct {
	Items []keeperAccountResponse `json:"items"`
}

type authPoolProxyConfigResponse struct {
	CPAURL                 string                    `json:"cpa_url"`
	APIKeySet              bool                      `json:"api_key_set"`
	APIKeyPreview          string                    `json:"api_key_preview"`
	Mode                   string                    `json:"mode"`
	PluginInstalled        bool                      `json:"plugin_installed"`
	PluginError            string                    `json:"plugin_error,omitempty"`
	PluginVersion          string                    `json:"plugin_version,omitempty"`
	ConcurrencyScope       string                    `json:"concurrency_scope,omitempty"`
	ConcurrencyStrategy    string                    `json:"concurrency_strategy,omitempty"`
	Targets                []authPoolProxyTargetView `json:"targets"`
	CodexConcurrencyLimits map[string]int            `json:"codex_concurrency_limits,omitempty"`
	Concurrency            *authPoolConcurrency      `json:"concurrency,omitempty"`
	ConcurrencySlots       []authPoolConcurrencySlot `json:"concurrency_slots,omitempty"`
}

type authPoolProxyConfigPayload struct {
	APIKey                 *string                      `json:"api_key"`
	Targets                []authPoolProxyTargetPayload `json:"targets"`
	CodexConcurrencyLimits map[string]int               `json:"codex_concurrency_limits"`
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
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Description    string   `json:"description"`
	AuthIDs        []string `json:"auth_ids"`
	AccountTypes   []string `json:"account_types"`
	Providers      []string `json:"providers,omitempty"`
	Models         []string `json:"models,omitempty"`
	Visibility     string   `json:"visibility"`
	AllowedUserIDs []int    `json:"allowed_user_ids"`
}

func normalizeAuthPoolVisibility(value string, allowedUserIDs []int) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		if len(normalizeAuthPoolUserIDs(allowedUserIDs)) > 0 {
			return authPoolVisibilitySelected, nil
		}
		return authPoolVisibilityAdminsOnly, nil
	}
	switch value {
	case authPoolVisibilityAdminsOnly, authPoolVisibilityAllUsers, authPoolVisibilitySelected:
		return value, nil
	default:
		return "", validationError("invalid auth pool visibility")
	}
}

func authPoolVisibleToUser(pool authPool, user *AuthUser) bool {
	if user == nil || user.IsAdmin {
		return true
	}
	switch pool.Visibility {
	case authPoolVisibilityAllUsers:
		return true
	case authPoolVisibilitySelected:
		return authPoolUserIDAllowed(pool.AllowedUserIDs, user.ID)
	default:
		return false
	}
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
		if refresh := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("refresh"))); refresh == "1" || refresh == "true" {
			a.invalidateAuthPoolProviderCache()
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
	if len(parts) == 1 && parts[0] == "plugin-events" {
		if !user.IsAdmin {
			return forbiddenError("admin required")
		}
		switch r.Method {
		case http.MethodGet:
			response, err := a.authPoolPluginEvents(r.Context(), authPoolPluginEventLimit(r))
			if err != nil {
				return err
			}
			writeJSON(w, http.StatusOK, response)
			return nil
		case http.MethodDelete:
			response, err := a.clearAuthPoolPluginEvents(r.Context())
			if err != nil {
				return err
			}
			writeJSON(w, http.StatusOK, response)
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
		a.failAuthPoolPrioritySync(err)
		status.PluginInstalled = false
		status.PluginError = err.Error()
		return status, nil
	}
	status.PluginInstalled = true
	if status.SchedulerPriorities {
		a.cacheAuthPoolPriorities(status.AuthTypes, status.TypePriorities, status.AuthPriorityOverrides, true)
	} else {
		err := validationError("CPA cpa-auth-pool plugin version is too old: missing /auth-priorities scheduler priority support")
		a.failAuthPoolPrioritySync(err)
		status.PluginError = err.Error()
	}
	localBindings, err := a.localAuthPoolBindings(ctx, nil)
	if err != nil {
		return authPoolStatus{}, err
	}
	localPools, err := a.localAuthPools(ctx)
	if err != nil {
		return authPoolStatus{}, err
	}
	restoredPluginState := false
	if len(status.Pools) == 0 && len(localPools) > 0 {
		if err := a.restoreLocalAuthPoolsToPlugin(ctx, localPools); err != nil {
			status.PluginError = err.Error()
			status.Pools = localPools
		} else {
			restoredPluginState = true
		}
	}
	if len(status.Bindings) == 0 && len(localBindings) > 0 {
		if err := a.restoreLocalAuthPoolBindingsToPlugin(ctx, localBindings); err != nil {
			if status.PluginError == "" {
				status.PluginError = err.Error()
			}
			status.Bindings = localBindings
		} else {
			restoredPluginState = true
		}
	}
	if restoredPluginState {
		var restored authPoolStatus
		if err := a.authPoolPluginRequest(ctx, http.MethodGet, "/status", nil, &restored); err == nil {
			status = restored
			status.PluginInstalled = true
			if status.SchedulerPriorities {
				a.cacheAuthPoolPriorities(status.AuthTypes, status.TypePriorities, status.AuthPriorityOverrides, true)
			} else {
				err := validationError("CPA cpa-auth-pool plugin version is too old: missing /auth-priorities scheduler priority support")
				a.failAuthPoolPrioritySync(err)
				status.PluginError = err.Error()
			}
		} else if status.PluginError == "" {
			status.Pools = localPools
			status.Bindings = localBindings
			status.PluginError = err.Error()
		}
	}
	if len(status.Pools) > 0 {
		_ = a.saveLocalAuthPools(ctx, status.Pools)
	}
	if len(status.Bindings) > 0 {
		_ = a.saveLocalAuthPoolBindings(ctx, status.Bindings)
	}
	if authPoolsNeedModelSync(status.Pools) {
		a.syncAuthPoolModelsAsync()
	}
	// Account files can be added directly in CPA after a pool was created. Keep
	// the plugin's resolved dynamic membership fresh without waiting for a model
	// catalog refresh or an admin to edit the pool.
	if len(status.Pools) > 0 {
		a.syncAuthPoolResolvedAuthIDsAsync()
	}
	localBindings, err = a.localAuthPoolBindings(ctx, user)
	if err != nil {
		return authPoolStatus{}, err
	}
	status.Pools = authPoolsVisibleToUser(status.Pools, localPools, user)
	status.Bindings = authPoolBindingsVisibleToPools(localBindings, status.Pools)
	return status, nil
}

func (a *App) syncAuthPoolModelsAsync() {
	now := time.Now()
	a.authPoolSyncMu.Lock()
	if a.authPoolSyncRun || now.Before(a.authPoolSyncNext) {
		a.authPoolSyncMu.Unlock()
		return
	}
	a.authPoolSyncRun = true
	a.authPoolSyncMu.Unlock()

	started := a.startBackgroundTask(func(parent context.Context) {
		ctx, cancel := context.WithTimeout(parent, authPoolModelSyncTimeout)
		defer cancel()
		err := a.syncAuthPoolModels(ctx)
		a.authPoolSyncMu.Lock()
		a.authPoolSyncRun = false
		if err == nil {
			a.authPoolSyncFail = 0
			a.authPoolSyncNext = time.Time{}
		} else {
			a.authPoolSyncFail++
			a.authPoolSyncNext = time.Now().Add(authPoolModelSyncBackoff(a.authPoolSyncFail))
		}
		a.authPoolSyncMu.Unlock()
		if err != nil && parent.Err() == nil {
			log.Printf("sync auth pool models failed: %v", err)
		}
	})
	if started {
		return
	}
	a.authPoolSyncMu.Lock()
	a.authPoolSyncRun = false
	a.authPoolSyncMu.Unlock()
}

func (a *App) syncAuthPoolResolvedAuthIDsAsync() {
	now := time.Now()
	a.authPoolResolvedSyncMu.Lock()
	if a.authPoolResolvedSyncRun || now.Before(a.authPoolResolvedNext) {
		a.authPoolResolvedSyncMu.Unlock()
		return
	}
	a.authPoolResolvedSyncRun = true
	a.authPoolResolvedSyncMu.Unlock()
	started := a.startBackgroundTask(func(parent context.Context) {
		ctx, cancel := context.WithTimeout(parent, authPoolResolvedSyncTimeout)
		defer cancel()
		err := a.syncAuthPoolResolvedAuthIDs(ctx)
		a.authPoolResolvedSyncMu.Lock()
		a.authPoolResolvedSyncRun = false
		if err == nil {
			a.authPoolResolvedFail = 0
			a.authPoolResolvedNext = time.Now().Add(authPoolResolvedSyncInterval)
		} else {
			a.authPoolResolvedFail++
			a.authPoolResolvedNext = time.Now().Add(authPoolModelSyncBackoff(a.authPoolResolvedFail))
		}
		a.authPoolResolvedSyncMu.Unlock()
		if err != nil && parent.Err() == nil {
			log.Printf("sync auth pool memberships failed: %v", err)
		}
	})
	if started {
		return
	}
	a.authPoolResolvedSyncMu.Lock()
	a.authPoolResolvedSyncRun = false
	a.authPoolResolvedSyncMu.Unlock()
}

func (a *App) syncAuthPoolResolvedAuthIDs(ctx context.Context) error {
	var status authPoolStatus
	if err := a.authPoolPluginRequest(ctx, http.MethodGet, "/status", nil, &status); err != nil {
		return a.failAuthPoolPrioritySync(err)
	}
	if !status.SchedulerPriorities {
		return a.failAuthPoolPrioritySync(validationError("CPA cpa-auth-pool plugin version is too old: missing /auth-priorities scheduler priority support"))
	}
	accounts, err := a.listAuthPoolAccounts(ctx)
	if err != nil {
		return a.failAuthPoolPrioritySync(err)
	}
	if len(status.Pools) > 0 {
		desired := authPoolResolvedAuthIDsForSync(status.Pools, accounts)
		changed := false
		for _, pool := range status.Pools {
			if !sameNormalizedStringList(pool.ResolvedAuthIDs, desired[pool.ID]) {
				changed = true
				break
			}
		}
		if changed {
			if err := a.authPoolPluginRequest(ctx, http.MethodPost, "/auth-models", map[string]any{
				"pool_resolved_auth_ids": desired,
			}, nil); err != nil {
				return a.failAuthPoolPrioritySync(err)
			}
			for index := range status.Pools {
				status.Pools[index].ResolvedAuthIDs = append([]string(nil), desired[status.Pools[index].ID]...)
			}
			if err := a.saveLocalAuthPools(ctx, status.Pools); err != nil {
				return a.failAuthPoolPrioritySync(err)
			}
		}
	}
	return a.syncAuthPoolSchedulerPrioritiesWithStatus(ctx, accounts, status)
}

func (a *App) syncAuthPoolSchedulerPriorities(ctx context.Context, accounts []keeperAccount) error {
	var status authPoolStatus
	if err := a.authPoolPluginRequest(ctx, http.MethodGet, "/status", nil, &status); err != nil {
		return a.failAuthPoolPrioritySync(err)
	}
	return a.syncAuthPoolSchedulerPrioritiesWithStatus(ctx, accounts, status)
}

func (a *App) syncAuthPoolSchedulerPrioritiesWithStatus(ctx context.Context, accounts []keeperAccount, status authPoolStatus) error {
	if !status.SchedulerPriorities {
		return a.failAuthPoolPrioritySync(validationError("CPA cpa-auth-pool plugin version is too old: missing /auth-priorities scheduler priority support"))
	}
	cfg, err := a.loadConfig(ctx)
	if err != nil {
		return a.failAuthPoolPrioritySync(err)
	}
	typePriorities := normalizePriorityRules(cfg.CodexKeeperPriorityRule)
	authTypes := make(map[string]string, len(accounts)*2)
	manualOverrides := map[string]int{}
	tracked := make([]keeperAccount, 0, len(accounts))
	presentKeys := map[string]struct{}{}
	for _, account := range accounts {
		accountType := strings.ToLower(strings.TrimSpace(stringPtrValue(account.AccountType)))
		keys := keeperAuthPriorityKeys(account)
		if len(keys) == 0 {
			continue
		}
		for _, key := range keys {
			presentKeys[key] = struct{}{}
			if accountType != "" {
				authTypes[key] = accountType
			}
		}
		tracked = append(tracked, account)
		migrationPriority := account.Priority
		if migrationPriority != nil && *migrationPriority == -1 && account.LegacyRestorePriority != nil {
			migrationPriority = account.LegacyRestorePriority
		}
		if migrationPriority != nil && *migrationPriority > 20 {
			if *migrationPriority > 100 {
				return a.failAuthPoolPrioritySync(validationError(fmt.Sprintf("account %s logical priority %d exceeds the supported maximum 100", account.Name, *migrationPriority)))
			}
			for _, key := range keys {
				manualOverrides[key] = *migrationPriority
			}
		}
	}
	for authID, priority := range status.AuthPriorityOverrides {
		key := keeperAuthPriorityKey(authID)
		if _, ok := presentKeys[key]; ok {
			manualOverrides[key] = priority
		}
	}
	payload := map[string]any{
		"auth_types":              authTypes,
		"type_priorities":         typePriorities,
		"replace_overrides":       true,
		"auth_priority_overrides": manualOverrides,
	}
	var response struct {
		Status authPoolStatus `json:"status"`
	}
	if err := a.authPoolPluginRequest(ctx, http.MethodPost, "/auth-priorities", payload, &response); err != nil {
		return a.failAuthPoolPrioritySync(err)
	}
	responseTypes := response.Status.AuthTypes
	if responseTypes == nil {
		responseTypes = authTypes
	}
	responseTypePriorities := response.Status.TypePriorities
	if responseTypePriorities == nil {
		responseTypePriorities = typePriorities
	}
	responseOverrides := response.Status.AuthPriorityOverrides
	if responseOverrides == nil || (len(responseOverrides) == 0 && len(manualOverrides) > 0) {
		responseOverrides = manualOverrides
	}
	a.cacheAuthPoolPriorities(responseTypes, responseTypePriorities, responseOverrides, true)
	for _, account := range tracked {
		if account.Disabled || account.Priority == nil || *account.Priority < 0 || *account.Priority == 0 {
			continue
		}
		zero := 0
		if err := a.setKeeperRemotePriority(ctx, cfg, account.Name, &zero); err != nil {
			return a.failAuthPoolPrioritySync(err)
		}
		if _, err := a.db.ExecContext(ctx, `
			UPDATE codex_keeper_auth_states
			SET priority = 0, updated_at = ?
			WHERE auth_name = ?
		`, dbTime(time.Now()), account.Name); err != nil {
			return a.failAuthPoolPrioritySync(err)
		}
	}
	return nil
}

func (a *App) cacheAuthPoolPriorities(authTypes map[string]string, typePriorities, overrides map[string]int, enabled bool) {
	a.authPoolPriorityMu.Lock()
	defer a.authPoolPriorityMu.Unlock()
	a.authPoolPriorityMode = enabled
	a.authPoolPriorityError = ""
	now := time.Now()
	a.authPoolPrioritySyncAt = &now
	a.authPoolAuthTypes = normalizeAuthPriorityStringMap(authTypes)
	a.authPoolTypePriorities = cloneIntMapValues(typePriorities)
	a.authPoolAuthOverrides = normalizeAuthPriorityIntMap(overrides)
}

func (a *App) failAuthPoolPrioritySync(err error) error {
	if err == nil {
		return nil
	}
	a.authPoolPriorityMu.Lock()
	a.authPoolPriorityMode = false
	a.authPoolPriorityError = err.Error()
	a.authPoolPriorityMu.Unlock()
	return err
}

func (a *App) authPoolPriorityStatus() (bool, string, *time.Time) {
	a.authPoolPriorityMu.RLock()
	defer a.authPoolPriorityMu.RUnlock()
	var syncedAt *time.Time
	if a.authPoolPrioritySyncAt != nil {
		value := *a.authPoolPrioritySyncAt
		syncedAt = &value
	}
	return a.authPoolPriorityMode, a.authPoolPriorityError, syncedAt
}

func (a *App) authPoolPriorityModeEnabled() bool {
	a.authPoolPriorityMu.RLock()
	defer a.authPoolPriorityMu.RUnlock()
	return a.authPoolPriorityMode
}

func (a *App) applyAuthPoolLogicalPriorities(accounts []keeperAccount) {
	a.authPoolPriorityMu.RLock()
	defer a.authPoolPriorityMu.RUnlock()
	if !a.authPoolPriorityMode {
		return
	}
	for index := range accounts {
		if accounts[index].Priority != nil && *accounts[index].Priority < 0 {
			continue
		}
		keys := keeperAuthPriorityKeys(accounts[index])
		if priority, ok := firstKeeperAuthPriority(a.authPoolAuthOverrides, keys); ok {
			value := priority
			accounts[index].Priority = &value
			continue
		}
		accountType := strings.ToLower(strings.TrimSpace(stringPtrValue(accounts[index].AccountType)))
		if priority, ok := a.authPoolTypePriorities[accountType]; ok {
			value := priority
			accounts[index].Priority = &value
		}
	}
}

func (a *App) updateAuthPoolPriorityOverride(ctx context.Context, authID string, priority *int) error {
	authID = keeperAuthPriorityKey(authID)
	if authID == "" {
		return validationError("auth id is required")
	}
	payload := map[string]any{}
	if priority == nil {
		payload["remove_overrides"] = []string{authID}
	} else {
		payload["auth_priority_overrides"] = map[string]int{authID: *priority}
	}
	var response struct {
		Status authPoolStatus `json:"status"`
	}
	if err := a.authPoolPluginRequest(ctx, http.MethodPost, "/auth-priorities", payload, &response); err != nil {
		return err
	}
	a.authPoolPriorityMu.Lock()
	if a.authPoolAuthOverrides == nil {
		a.authPoolAuthOverrides = map[string]int{}
	}
	if priority == nil {
		delete(a.authPoolAuthOverrides, authID)
	} else {
		a.authPoolAuthOverrides[authID] = *priority
	}
	a.authPoolPriorityMode = true
	a.authPoolPriorityError = ""
	now := time.Now()
	a.authPoolPrioritySyncAt = &now
	a.authPoolPriorityMu.Unlock()
	return nil
}

func cloneStringMapValues(values map[string]string) map[string]string {
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func cloneIntMapValues(values map[string]int) map[string]int {
	cloned := make(map[string]int, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func keeperAuthPriorityKey(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "\\", "/")
	if index := strings.LastIndex(value, "/"); index >= 0 {
		value = value[index+1:]
	}
	var builder strings.Builder
	lastUnderscore := false
	for _, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') {
			builder.WriteRune(char)
			lastUnderscore = false
			continue
		}
		if builder.Len() > 0 && !lastUnderscore {
			builder.WriteByte('_')
			lastUnderscore = true
		}
	}
	key := strings.Trim(builder.String(), "_")
	for _, prefix := range []string{"root_cli_proxy_api_", "root_cli_proxy_", "cli_proxy_api_"} {
		if strings.HasPrefix(key, prefix) {
			return strings.TrimPrefix(key, prefix)
		}
	}
	return key
}

func keeperAuthPriorityKeys(account keeperAccount) []string {
	seen := map[string]struct{}{}
	keys := []string{}
	for _, value := range []string{account.Name, stringPtrValue(account.AuthIndex), stringPtrValue(account.Email)} {
		if key := keeperAuthPriorityKey(value); key != "" {
			if _, exists := seen[key]; !exists {
				seen[key] = struct{}{}
				keys = append(keys, key)
			}
		}
	}
	return keys
}

func firstKeeperAuthPriority(values map[string]int, keys []string) (int, bool) {
	for _, key := range keys {
		if value, ok := values[key]; ok {
			return value, true
		}
	}
	return 0, false
}

func normalizeAuthPriorityStringMap(values map[string]string) map[string]string {
	result := make(map[string]string, len(values))
	for key, value := range values {
		key = keeperAuthPriorityKey(key)
		value = strings.ToLower(strings.TrimSpace(value))
		if key != "" && value != "" {
			result[key] = value
		}
	}
	return result
}

func normalizeAuthPriorityIntMap(values map[string]int) map[string]int {
	result := make(map[string]int, len(values))
	for key, value := range values {
		if key = keeperAuthPriorityKey(key); key != "" {
			result[key] = value
		}
	}
	return result
}

func sameNormalizedStringList(left, right []string) bool {
	return reflect.DeepEqual(normalizedStringList(left), normalizedStringList(right))
}

func authPoolModelSyncBackoff(failures int) time.Duration {
	if failures <= 1 {
		return authPoolModelSyncBackoffBase
	}
	delay := authPoolModelSyncBackoffBase
	for attempt := 1; attempt < failures && delay < authPoolModelSyncBackoffMax; attempt++ {
		delay *= 2
		if delay >= authPoolModelSyncBackoffMax {
			return authPoolModelSyncBackoffMax
		}
	}
	return delay
}

func (a *App) upsertAuthPool(ctx context.Context, payload authPoolPayload) (authPool, error) {
	visibility, err := normalizeAuthPoolVisibility(payload.Visibility, payload.AllowedUserIDs)
	if err != nil {
		return authPool{}, err
	}
	allowedUserIDs := normalizeAuthPoolUserIDs(payload.AllowedUserIDs)
	if visibility != authPoolVisibilitySelected {
		allowedUserIDs = []int{}
	}
	pool := authPool{ID: strings.TrimSpace(payload.ID), Name: strings.TrimSpace(payload.Name), Description: strings.TrimSpace(payload.Description), AuthIDs: payload.AuthIDs, AccountTypes: payload.AccountTypes, Providers: payload.Providers, Models: payload.Models, Visibility: visibility, AllowedUserIDs: allowedUserIDs, Enabled: true}
	if pool.ID == "" || pool.Name == "" {
		return authPool{}, validationError("pool id and name are required")
	}
	var accounts []keeperAccount
	if len(pool.AuthIDs) > 0 || len(pool.AccountTypes) > 0 || len(pool.Models) == 0 {
		var err error
		accounts, err = a.listAuthPoolAccounts(ctx)
		if err != nil {
			return authPool{}, err
		}
		pool.ResolvedAuthIDs = authIDsForPoolModelSync(pool, accounts)
	}
	pool.Providers = authPoolProvidersForSync(pool, accounts)
	if len(pool.Models) == 0 && (len(pool.AuthIDs) > 0 || len(pool.AccountTypes) > 0) {
		models, err := a.resolveAuthPoolModelsFromAccounts(ctx, pool, accounts)
		if err != nil {
			return authPool{}, err
		}
		pool.Models = models
	}
	var response struct {
		Pool authPool `json:"pool"`
	}
	if err := a.authPoolPluginRequest(ctx, http.MethodPost, "/pools", pool, &response); err != nil {
		return authPool{}, err
	}
	response.Pool.Visibility = pool.Visibility
	response.Pool.AllowedUserIDs = pool.AllowedUserIDs
	response.Pool.Providers = append([]string(nil), pool.Providers...)
	if err := a.saveLocalAuthPool(ctx, response.Pool); err != nil {
		return authPool{}, err
	}
	if err := a.replaceAuthPoolAccess(ctx, response.Pool.ID, pool.Visibility, pool.AllowedUserIDs); err != nil {
		return authPool{}, err
	}
	if err := a.syncAuthPoolModels(ctx); err != nil {
		log.Printf("sync auth pool models after pool update failed: %v", err)
	}
	a.refreshChannelStatusAfterChange()
	return response.Pool, nil
}

func (a *App) deleteAuthPool(ctx context.Context, id string) error {
	if err := a.authPoolPluginRequest(ctx, http.MethodDelete, "/pools?id="+urlQueryEscape(id), nil, nil); err != nil {
		return err
	}
	_, err := a.db.ExecContext(ctx, `DELETE FROM user_api_key_pools WHERE pool_id = ?`, id)
	if err == nil {
		_, _ = a.db.ExecContext(ctx, `DELETE FROM auth_pools WHERE id = ?`, id)
		if syncErr := a.syncAuthPoolModels(ctx); syncErr != nil {
			log.Printf("sync auth pool models after pool deletion failed: %v", syncErr)
		}
		a.refreshChannelStatusAfterChange()
	}
	return err
}

func (a *App) saveLocalAuthPools(ctx context.Context, pools []authPool) error {
	for _, pool := range pools {
		if err := a.saveLocalAuthPool(ctx, pool); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) saveLocalAuthPool(ctx context.Context, pool authPool) error {
	pool.ID = strings.TrimSpace(pool.ID)
	pool.Name = strings.TrimSpace(pool.Name)
	if pool.ID == "" || pool.Name == "" {
		return nil
	}
	if strings.TrimSpace(pool.Visibility) == "" {
		var storedVisibility string
		err := a.db.QueryRowContext(ctx, `SELECT visibility FROM auth_pools WHERE id = ?`, pool.ID).Scan(&storedVisibility)
		switch {
		case err == nil:
			pool.Visibility = storedVisibility
		case errors.Is(err, sql.ErrNoRows):
			pool.Visibility = authPoolVisibilityAdminsOnly
		default:
			return err
		}
	}
	visibility, err := normalizeAuthPoolVisibility(pool.Visibility, pool.AllowedUserIDs)
	if err != nil {
		return err
	}
	pool.Visibility = visibility
	authIDsJSON, err := marshalStringList(pool.AuthIDs)
	if err != nil {
		return err
	}
	resolvedAuthIDsJSON, err := marshalStringList(pool.ResolvedAuthIDs)
	if err != nil {
		return err
	}
	accountTypesJSON, err := marshalStringList(pool.AccountTypes)
	if err != nil {
		return err
	}
	modelsJSON, err := marshalStringList(pool.Models)
	if err != nil {
		return err
	}
	providersJSON, err := marshalStringList(pool.Providers)
	if err != nil {
		return err
	}
	now := dbTime(time.Now())
	_, err = a.db.ExecContext(ctx, `
		INSERT INTO auth_pools (id, name, description, auth_ids_json, resolved_auth_ids_json, account_types_json, providers_json, models_json, visibility, enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			description = excluded.description,
			auth_ids_json = excluded.auth_ids_json,
			resolved_auth_ids_json = excluded.resolved_auth_ids_json,
			account_types_json = excluded.account_types_json,
			providers_json = excluded.providers_json,
			models_json = excluded.models_json,
			visibility = excluded.visibility,
			enabled = excluded.enabled,
			updated_at = excluded.updated_at
	`, pool.ID, pool.Name, strings.TrimSpace(pool.Description), authIDsJSON, resolvedAuthIDsJSON, accountTypesJSON, providersJSON, modelsJSON, pool.Visibility, pool.Enabled, now, now)
	return err
}

func (a *App) localAuthPools(ctx context.Context) ([]authPool, error) {
	rows, err := a.db.QueryContext(ctx, `
		SELECT id, name, COALESCE(description, ''), auth_ids_json, resolved_auth_ids_json, account_types_json, providers_json, models_json, visibility, enabled
		FROM auth_pools
		ORDER BY id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	pools := []authPool{}
	for rows.Next() {
		var pool authPool
		var authIDsJSON, resolvedAuthIDsJSON, accountTypesJSON, providersJSON, modelsJSON string
		if err := rows.Scan(&pool.ID, &pool.Name, &pool.Description, &authIDsJSON, &resolvedAuthIDsJSON, &accountTypesJSON, &providersJSON, &modelsJSON, &pool.Visibility, &pool.Enabled); err != nil {
			return nil, err
		}
		pool.AuthIDs = unmarshalStringList(authIDsJSON)
		pool.ResolvedAuthIDs = unmarshalStringList(resolvedAuthIDsJSON)
		pool.AccountTypes = unmarshalStringList(accountTypesJSON)
		pool.Providers = unmarshalStringList(providersJSON)
		pool.Models = unmarshalStringList(modelsJSON)
		pools = append(pools, pool)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	entitlements, err := a.localAuthPoolEntitlements(ctx)
	if err != nil {
		return nil, err
	}
	for index := range pools {
		pools[index].AllowedUserIDs = entitlements[pools[index].ID]
		if pools[index].AllowedUserIDs == nil {
			pools[index].AllowedUserIDs = []int{}
		}
	}
	return pools, nil
}

func (a *App) replaceAuthPoolEntitlements(ctx context.Context, poolID string, userIDs []int) error {
	visibility := authPoolVisibilityAdminsOnly
	if len(normalizeAuthPoolUserIDs(userIDs)) > 0 {
		visibility = authPoolVisibilitySelected
	}
	return a.replaceAuthPoolAccess(ctx, poolID, visibility, userIDs)
}

func (a *App) replaceAuthPoolAccess(ctx context.Context, poolID, visibility string, userIDs []int) error {
	poolID = strings.TrimSpace(poolID)
	if poolID == "" {
		return validationError("pool id is required")
	}
	var err error
	visibility, err = normalizeAuthPoolVisibility(visibility, userIDs)
	if err != nil {
		return err
	}
	userIDs = normalizeAuthPoolUserIDs(userIDs)
	if visibility != authPoolVisibilitySelected {
		userIDs = []int{}
	}
	revokedBindings, err := a.authPoolBindingsRevokedByVisibility(ctx, poolID, visibility, userIDs)
	if err != nil {
		return err
	}
	if err := a.removeAuthPoolBindingsFromPlugin(ctx, revokedBindings); err != nil {
		return err
	}
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		a.restoreAuthPoolBindingsInPlugin(ctx, revokedBindings)
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM auth_pool_entitlements WHERE pool_id = ?`, poolID); err != nil {
		a.restoreAuthPoolBindingsInPlugin(ctx, revokedBindings)
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE auth_pools SET visibility = ?, updated_at = ? WHERE id = ?`, visibility, dbTime(time.Now()), poolID); err != nil {
		a.restoreAuthPoolBindingsInPlugin(ctx, revokedBindings)
		return err
	}
	now := dbTime(time.Now())
	for _, userID := range userIDs {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO auth_pool_entitlements (pool_id, user_id, created_at)
			VALUES (?, ?, ?)
		`, poolID, userID, now); err != nil {
			a.restoreAuthPoolBindingsInPlugin(ctx, revokedBindings)
			return validationError("auth pool contains an invalid user")
		}
	}
	for _, binding := range revokedBindings {
		if _, err := tx.ExecContext(ctx, `DELETE FROM user_api_key_pools WHERE api_key_hash = ? AND pool_id = ?`, binding.APIKeyHash, poolID); err != nil {
			a.restoreAuthPoolBindingsInPlugin(ctx, revokedBindings)
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		a.restoreAuthPoolBindingsInPlugin(ctx, revokedBindings)
		return err
	}
	return nil
}

func (a *App) authPoolBindingsRevokedByVisibility(ctx context.Context, poolID, visibility string, allowedUserIDs []int) ([]authPoolBinding, error) {
	if visibility == authPoolVisibilityAllUsers {
		return nil, nil
	}
	query := `
		SELECT p.api_key_hash, p.pool_id, k.user_id, COALESCE(u.username, '')
		FROM user_api_key_pools p
		JOIN user_api_keys k ON k.api_key_hash = p.api_key_hash
		JOIN users u ON u.id = k.user_id
		WHERE p.pool_id = ? AND u.is_admin = 0
	`
	args := []any{poolID}
	if visibility == authPoolVisibilitySelected && len(allowedUserIDs) > 0 {
		placeholders := make([]string, 0, len(allowedUserIDs))
		for _, userID := range allowedUserIDs {
			placeholders = append(placeholders, "?")
			args = append(args, userID)
		}
		query += ` AND k.user_id NOT IN (` + strings.Join(placeholders, ",") + `)`
	}
	query += ` ORDER BY p.api_key_hash`
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

func (a *App) removeAuthPoolBindingsFromPlugin(ctx context.Context, bindings []authPoolBinding) error {
	removed := make([]authPoolBinding, 0, len(bindings))
	for _, binding := range bindings {
		if err := a.authPoolPluginRequest(ctx, http.MethodDelete, "/bindings?api_key_hash="+urlQueryEscape(binding.APIKeyHash), nil, nil); err != nil {
			a.restoreAuthPoolBindingsInPlugin(ctx, removed)
			return err
		}
		removed = append(removed, binding)
	}
	return nil
}

func (a *App) restoreAuthPoolBindingsInPlugin(ctx context.Context, bindings []authPoolBinding) {
	for _, binding := range bindings {
		if err := a.authPoolPluginRequest(ctx, http.MethodPost, "/bindings", binding, nil); err != nil {
			log.Printf("restore auth pool binding after local failure failed: api_key_hash=%s error=%v", binding.APIKeyHash, err)
		}
	}
}

func (a *App) localAuthPoolEntitlements(ctx context.Context) (map[string][]int, error) {
	rows, err := a.db.QueryContext(ctx, `
		SELECT pool_id, user_id
		FROM auth_pool_entitlements
		ORDER BY pool_id, user_id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string][]int{}
	for rows.Next() {
		var poolID string
		var userID int
		if err := rows.Scan(&poolID, &userID); err != nil {
			return nil, err
		}
		result[poolID] = append(result[poolID], userID)
	}
	return result, rows.Err()
}

func normalizeAuthPoolUserIDs(userIDs []int) []int {
	seen := map[int]bool{}
	normalized := make([]int, 0, len(userIDs))
	for _, userID := range userIDs {
		if userID <= 0 || seen[userID] {
			continue
		}
		seen[userID] = true
		normalized = append(normalized, userID)
	}
	sort.Ints(normalized)
	return normalized
}

func authPoolsVisibleToUser(pools []authPool, localPools []authPool, user *AuthUser) []authPool {
	accessByPoolID := make(map[string]authPool, len(localPools))
	for _, pool := range localPools {
		pool.AllowedUserIDs = normalizeAuthPoolUserIDs(pool.AllowedUserIDs)
		accessByPoolID[strings.TrimSpace(pool.ID)] = pool
	}
	visible := make([]authPool, 0, len(pools))
	for _, pool := range pools {
		access := accessByPoolID[strings.TrimSpace(pool.ID)]
		pool.Visibility = access.Visibility
		if pool.Visibility == "" {
			pool.Visibility = authPoolVisibilityAdminsOnly
		}
		pool.AllowedUserIDs = access.AllowedUserIDs
		if pool.AllowedUserIDs == nil {
			pool.AllowedUserIDs = []int{}
		}
		if !authPoolVisibleToUser(pool, user) {
			continue
		}
		visible = append(visible, pool)
	}
	return visible
}

func authPoolUserIDAllowed(allowedUserIDs []int, userID int) bool {
	for _, allowedUserID := range allowedUserIDs {
		if allowedUserID == userID {
			return true
		}
	}
	return false
}

func authPoolBindingsVisibleToPools(bindings []authPoolBinding, pools []authPool) []authPoolBinding {
	visiblePoolIDs := make(map[string]struct{}, len(pools))
	for _, pool := range pools {
		visiblePoolIDs[strings.TrimSpace(pool.ID)] = struct{}{}
	}
	visible := make([]authPoolBinding, 0, len(bindings))
	for _, binding := range bindings {
		if _, ok := visiblePoolIDs[strings.TrimSpace(binding.PoolID)]; ok {
			visible = append(visible, binding)
		}
	}
	return visible
}

func (a *App) restoreLocalAuthPoolsToPlugin(ctx context.Context, pools []authPool) error {
	for _, pool := range pools {
		if strings.TrimSpace(pool.ID) == "" || strings.TrimSpace(pool.Name) == "" {
			continue
		}
		var response struct {
			Pool authPool `json:"pool"`
		}
		if err := a.authPoolPluginRequest(ctx, http.MethodPost, "/pools", pool, &response); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) restoreLocalAuthPoolBindingsToPlugin(ctx context.Context, bindings []authPoolBinding) error {
	for _, binding := range bindings {
		binding.APIKeyHash = strings.TrimSpace(binding.APIKeyHash)
		binding.PoolID = strings.TrimSpace(binding.PoolID)
		if binding.APIKeyHash == "" || binding.PoolID == "" {
			continue
		}
		var response struct {
			Binding authPoolBinding `json:"binding"`
		}
		if err := a.authPoolPluginRequest(ctx, http.MethodPost, "/bindings", binding, &response); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) saveLocalAuthPoolBindings(ctx context.Context, bindings []authPoolBinding) error {
	now := dbTime(time.Now())
	for _, binding := range bindings {
		apiKeyHash := strings.TrimSpace(binding.APIKeyHash)
		poolID := strings.TrimSpace(binding.PoolID)
		if apiKeyHash == "" || poolID == "" {
			continue
		}
		if _, err := a.db.ExecContext(ctx, `
			INSERT INTO user_api_key_pools (api_key_hash, pool_id, created_at, updated_at)
			SELECT ?, ?, ?, ?
			WHERE EXISTS (SELECT 1 FROM user_api_keys WHERE api_key_hash = ?)
			ON CONFLICT(api_key_hash) DO UPDATE SET pool_id = excluded.pool_id, updated_at = excluded.updated_at
		`, apiKeyHash, poolID, now, now, apiKeyHash); err != nil {
			return err
		}
	}
	return nil
}

func marshalStringList(values []string) (string, error) {
	cleaned := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			cleaned = append(cleaned, value)
		}
	}
	raw, err := json.Marshal(cleaned)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func unmarshalStringList(raw string) []string {
	values := []string{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &values); err != nil {
		return []string{}
	}
	cleaned := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			cleaned = append(cleaned, value)
		}
	}
	return cleaned
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
	var status authPoolStatus
	if err := a.authPoolPluginRequestWithConfig(ctx, cfg, http.MethodGet, "/status", nil, &status); err != nil {
		response.PluginError = err.Error()
	} else {
		response.PluginInstalled = true
		response.PluginVersion = status.PluginVersion
		response.ConcurrencyScope = status.ConcurrencyScope
		response.ConcurrencyStrategy = status.ConcurrencyStrategy
		response.CodexConcurrencyLimits = authPoolConcurrencyLimitsFromStatus(status)
		response.Concurrency = status.Concurrency
		response.ConcurrencySlots = status.ConcurrencySlots
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
	if payload.CodexConcurrencyLimits != nil {
		if err := a.updateAuthPoolCodexConcurrencyLimits(ctx, cfg, payload.CodexConcurrencyLimits); err != nil {
			return authPoolProxyConfigResponse{}, err
		}
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

func (a *App) updateAuthPoolCodexConcurrencyLimits(ctx context.Context, cfg AppConfig, limits map[string]int) error {
	normalized := normalizeAuthPoolCodexConcurrencyLimits(limits)
	targets := normalizeAuthPoolProxyTargets(cfg.AuthPoolProxyTargets)
	updated := false
	for _, target := range targets {
		if !target.Enabled {
			continue
		}
		if strings.TrimSpace(target.CPAURL) == "" || strings.TrimSpace(target.ManagementKey) == "" {
			return validationError("CPA URL and management key are required for enabled proxy targets")
		}
		if err := a.authPoolPluginRequestWithTarget(ctx, target, http.MethodPost, "/codex-concurrency-limits", map[string]any{"limits": normalized}, nil); err != nil {
			return err
		}
		updated = true
	}
	if !updated {
		return a.authPoolPluginRequestWithConfig(ctx, cfg, http.MethodPost, "/codex-concurrency-limits", map[string]any{"limits": normalized}, nil)
	}
	return nil
}

func authPoolConcurrencyLimitsFromStatus(status authPoolStatus) map[string]int {
	if status.Concurrency != nil && len(status.Concurrency.Limits) > 0 {
		return normalizeAuthPoolCodexConcurrencyLimits(status.Concurrency.Limits)
	}
	return normalizeAuthPoolCodexConcurrencyLimits(status.CodexConcurrencyLimits)
}

func normalizeAuthPoolCodexConcurrencyLimits(limits map[string]int) map[string]int {
	result := map[string]int{"default": 0}
	for tier, limit := range limits {
		tier = normalizeAuthPoolConcurrencyTier(tier)
		if tier == "" {
			continue
		}
		if limit < 0 {
			limit = 0
		}
		result[tier] = limit
	}
	return result
}

func normalizeAuthPoolConcurrencyTier(value string) string {
	value = sanitizeAuthPoolAccountType(value)
	switch value {
	case "chatgpt_free", "codex_free", "openai_free":
		return "free"
	case "chatgpt_plus", "codex_plus", "openai_plus":
		return "plus"
	case "chatgpt_team", "codex_team", "openai_team":
		return "team"
	case "chatgpt_pro", "codex_pro", "openai_pro":
		return "pro"
	case "chatgpt_enterprise", "codex_enterprise", "openai_enterprise":
		return "enterprise"
	case "chatgpt_business", "codex_business", "openai_business":
		return "business"
	case "free", "plus", "team", "pro", "enterprise", "business", "edu", "default":
		return value
	default:
		return ""
	}
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
	if err := a.syncAuthPoolModels(ctx); err != nil {
		log.Printf("sync auth pool models after account update failed: %v", err)
	}
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
	if err := a.ensureAuthPoolBindingAccess(ctx, user, poolID); err != nil {
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

func (a *App) ensureAuthPoolBindingAccess(ctx context.Context, user *AuthUser, poolID string) error {
	exists, enabled, visibility, err := a.localAuthPoolAvailability(ctx, poolID)
	if err != nil {
		return err
	}
	if !exists {
		var status authPoolStatus
		if err := a.authPoolPluginRequest(ctx, http.MethodGet, "/status", nil, &status); err != nil {
			return err
		}
		if err := a.saveLocalAuthPools(ctx, status.Pools); err != nil {
			return err
		}
		exists, enabled, visibility, err = a.localAuthPoolAvailability(ctx, poolID)
		if err != nil {
			return err
		}
	}
	if !exists {
		return notFoundError("auth pool not found")
	}
	if !enabled {
		return validationError("auth pool is disabled")
	}
	if user != nil && user.IsAdmin {
		return nil
	}
	if user == nil {
		return forbiddenError("auth pool access denied")
	}
	if visibility == authPoolVisibilityAllUsers {
		return nil
	}
	if visibility != authPoolVisibilitySelected {
		return forbiddenError("auth pool access denied")
	}
	var entitled int
	if err := a.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM auth_pool_entitlements
		WHERE pool_id = ? AND user_id = ?
	`, poolID, user.ID).Scan(&entitled); err != nil {
		return err
	}
	if entitled == 0 {
		return forbiddenError("auth pool access denied")
	}
	return nil
}

func (a *App) localAuthPoolAvailability(ctx context.Context, poolID string) (bool, bool, string, error) {
	var count int
	var enabled bool
	var visibility string
	if err := a.db.QueryRowContext(ctx, `
		SELECT COUNT(*), COALESCE(MAX(enabled), 0), COALESCE(MAX(visibility), '')
		FROM auth_pools
		WHERE id = ?
	`, poolID).Scan(&count, &enabled, &visibility); err != nil {
		return false, false, "", err
	}
	if visibility == "" {
		visibility = authPoolVisibilityAdminsOnly
	}
	return count > 0, enabled, visibility, nil
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
	pools, err := a.localAuthPools(ctx)
	if err != nil {
		return nil, err
	}
	poolFilters := map[string]map[string]bool{}
	for _, pool := range pools {
		poolID := strings.TrimSpace(pool.ID)
		if poolID != "" && pool.Enabled {
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
	pools, err := a.localAuthPools(ctx)
	if err != nil {
		return err
	}
	for _, pool := range pools {
		if strings.TrimSpace(pool.ID) != poolID {
			continue
		}
		if !pool.Enabled {
			return validationError("当前 API KEY 绑定的号池已禁用")
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
		if path == "/auth-priorities" {
			return validationError("CPA cpa-auth-pool plugin version is too old: missing /auth-priorities scheduler priority support")
		}
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
	if err := a.syncAuthPoolSchedulerPrioritiesWithStatus(ctx, accounts, status); err != nil {
		return err
	}
	authIDs := authIDsForModelSync(status.Pools, accounts)
	if len(authIDs) == 0 {
		return nil
	}
	poolResolvedAuthIDs := authPoolResolvedAuthIDsForSync(status.Pools, accounts)
	if err := a.authPoolPluginRequest(ctx, http.MethodPost, "/auth-models", map[string]any{
		"pool_resolved_auth_ids": poolResolvedAuthIDs,
	}, nil); err != nil {
		return err
	}
	authModels, err := a.fetchAuthPoolModelSnapshot(ctx, authIDs, accounts)
	if err != nil {
		return err
	}
	poolModels := authPoolModelsForSync(status.Pools, accounts, authModels)
	return a.authPoolPluginRequest(ctx, http.MethodPost, "/auth-models", map[string]any{
		"auth_models":            authModels,
		"pool_models":            poolModels,
		"pool_resolved_auth_ids": poolResolvedAuthIDs,
	}, nil)
}

func (a *App) resolveAuthPoolModelsFromAccounts(ctx context.Context, pool authPool, accounts []keeperAccount) ([]string, error) {
	authIDs := authIDsForPoolModelSync(pool, accounts)
	if len(authIDs) == 0 {
		return nil, validationError("auth pool has no eligible accounts")
	}
	authModels, err := a.fetchAuthPoolModelSnapshot(ctx, authIDs, accounts)
	if err != nil {
		return nil, err
	}
	models := authPoolModelsForSync([]authPool{pool}, accounts, authModels)[pool.ID]
	if len(models) == 0 {
		return nil, validationError("auth pool accounts returned no models")
	}
	return models, nil
}

func authPoolResolvedAuthIDsForSync(pools []authPool, accounts []keeperAccount) map[string][]string {
	resolved := make(map[string][]string, len(pools))
	for _, pool := range pools {
		poolID := strings.TrimSpace(pool.ID)
		if poolID == "" {
			continue
		}
		resolved[poolID] = authIDsForPoolModelSync(pool, accounts)
	}
	return resolved
}

func (a *App) fetchAuthPoolModelSnapshot(ctx context.Context, authIDs []string, accounts []keeperAccount) (map[string][]string, error) {
	cfg, err := a.loadConfig(ctx)
	if err != nil {
		return nil, err
	}
	authModels := make(map[string][]string, len(authIDs))
	failures := make([]string, 0)
	totalModels := 0
	modelsByAuthID := make(map[string][]string, len(accounts))
	for _, account := range accounts {
		if len(account.Models) == 0 {
			continue
		}
		modelsByAuthID[strings.ToLower(strings.TrimSpace(account.Name))] = append([]string(nil), account.Models...)
	}
	for _, authID := range authIDs {
		if models := modelsByAuthID[strings.ToLower(strings.TrimSpace(authID))]; len(models) > 0 {
			authModels[authID] = models
			totalModels += len(models)
			continue
		}
		models, fetchErr := a.fetchAuthFileModels(ctx, cfg, authID)
		if fetchErr != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", authID, fetchErr))
			continue
		}
		authModels[authID] = models
		totalModels += len(models)
	}
	if len(failures) > 0 {
		return nil, fmt.Errorf("auth pool model catalog refresh failed: %s", strings.Join(failures, "; "))
	}
	if totalModels == 0 {
		return nil, validationError("auth pool model catalog is empty; keeping the last successful snapshot")
	}
	return authModels, nil
}

func authPoolsNeedModelSync(pools []authPool) bool {
	for _, pool := range pools {
		if (len(pool.AuthIDs) > 0 || len(pool.AccountTypes) > 0) && len(pool.Models) == 0 {
			return true
		}
		if len(pool.AccountTypes) > 0 && len(pool.ResolvedAuthIDs) == 0 {
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
	remoteAccounts, remoteErr := a.listKeeperRemoteAuthFiles(ctx, cfg)
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
	providerAccounts, providerErr := a.listCPAProviderChannelsCached(ctx)
	for _, account := range providerAccounts {
		name := strings.TrimSpace(account.Name)
		if name == "" {
			continue
		}
		byName[name] = account
	}
	if remoteErr != nil && len(byName) == 0 {
		if providerErr != nil {
			return nil, remoteErr
		}
		return providerAccounts, nil
	}
	if providerErr != nil {
		log.Printf("list CPA provider channels failed: %v", providerErr)
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

type cpaOpenAICompatibilityResponse struct {
	Channels []cpaOpenAICompatibilityChannel `json:"openai-compatibility"`
}

type cpaOpenAICompatibilityChannel struct {
	Name          string                        `json:"name"`
	Disabled      bool                          `json:"disabled"`
	Prefix        string                        `json:"prefix"`
	Models        []cpaOpenAICompatibilityModel `json:"models"`
	APIKeyEntries []json.RawMessage             `json:"api-key-entries"`
}

type cpaOpenAICompatibilityModel struct {
	Name        string `json:"name"`
	Alias       string `json:"alias"`
	DisplayName string `json:"display-name"`
}

func (a *App) listCPAProviderChannels(ctx context.Context) ([]keeperAccount, error) {
	var response cpaOpenAICompatibilityResponse
	if err := a.cpaManagementJSON(ctx, http.MethodGet, "/v0/management/openai-compatibility", nil, &response); err != nil {
		return nil, err
	}
	accounts := make([]keeperAccount, 0)
	for _, channel := range response.Channels {
		name := strings.TrimSpace(channel.Name)
		if name == "" {
			continue
		}
		provider := openAICompatibilityProviderKey(name)
		models := cpaOpenAICompatibilityModels(channel.Models)
		credentialCount := len(channel.APIKeyEntries)
		if credentialCount == 0 {
			credentialCount = 1
		}
		display := "CPA AI 提供商 · " + name
		accountType := "openai-compatible"
		providerValue := provider
		source := "ai_provider"
		accounts = append(accounts, keeperAccount{
			Name:            "cpa-provider:" + provider,
			DisplayName:     &display,
			CredentialCount: credentialCount,
			AccountType:     &accountType,
			Provider:        &providerValue,
			Source:          &source,
			Models:          append([]string(nil), models...),
			Disabled:        channel.Disabled,
		})
	}
	return accounts, nil
}

func (a *App) listCPAProviderChannelsCached(ctx context.Context) ([]keeperAccount, error) {
	cfg, err := a.loadConfig(ctx)
	if err != nil {
		return nil, err
	}
	target, ok := primaryCPAManagementTarget(cfg)
	if !ok {
		return nil, validationError("CPA URL and management key are required")
	}
	cacheKey := strings.TrimRight(strings.TrimSpace(target.CPAURL), "/")
	now := time.Now()
	a.authPoolProviderCacheMu.Lock()
	defer a.authPoolProviderCacheMu.Unlock()
	if a.authPoolProviderCacheKey == cacheKey && now.Sub(a.authPoolProviderCacheAt) < authPoolProviderCacheTTL {
		return cloneKeeperAccounts(a.authPoolProviderCache), nil
	}
	accounts, err := a.listCPAProviderChannels(ctx)
	if err != nil {
		return nil, err
	}
	a.authPoolProviderCacheKey = cacheKey
	a.authPoolProviderCacheAt = now
	a.authPoolProviderCache = cloneKeeperAccounts(accounts)
	return cloneKeeperAccounts(accounts), nil
}

func (a *App) invalidateAuthPoolProviderCache() {
	a.authPoolProviderCacheMu.Lock()
	a.authPoolProviderCacheAt = time.Time{}
	a.authPoolProviderCacheKey = ""
	a.authPoolProviderCache = nil
	a.authPoolProviderCacheMu.Unlock()
}

func cloneKeeperAccounts(accounts []keeperAccount) []keeperAccount {
	result := make([]keeperAccount, len(accounts))
	for index, account := range accounts {
		result[index] = account
		result[index].Models = append([]string(nil), account.Models...)
	}
	return result
}

func cpaOpenAICompatibilityModels(models []cpaOpenAICompatibilityModel) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(models)*2)
	for _, model := range models {
		for _, value := range []string{model.Alias, model.Name} {
			value = strings.TrimSpace(value)
			if value != "" && !seen[value] {
				seen[value] = true
				result = append(result, value)
			}
		}
	}
	sortStringsCaseInsensitive(result)
	return result
}

func openAICompatibilityProviderKey(name string) string {
	// Keep this identical to CLIProxyAPI's OpenAICompatibleProviderKey; names are not sanitized there.
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" || name == "openai-compatibility" || strings.HasPrefix(name, "openai-compatible-") {
		if name == "" {
			return "openai-compatibility"
		}
		return name
	}
	return "openai-compatible-" + name
}

func authPoolProvidersForSync(pool authPool, accounts []keeperAccount) []string {
	manualIDs := normalizedLookup(pool.AuthIDs)
	typeIDs := normalizedLookup(pool.AccountTypes)
	seen := map[string]bool{}
	providers := make([]string, 0)
	for _, account := range accounts {
		if !channelAccountMatchesPool(account, manualIDs, typeIDs) || account.Provider == nil {
			continue
		}
		provider := strings.TrimSpace(*account.Provider)
		if provider != "" && !seen[strings.ToLower(provider)] {
			seen[strings.ToLower(provider)] = true
			providers = append(providers, provider)
		}
	}
	sortStringsCaseInsensitive(providers)
	return providers
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
	if local.DisplayName == nil {
		local.DisplayName = remote.DisplayName
	}
	if local.Provider == nil {
		local.Provider = remote.Provider
	}
	if local.Source == nil {
		local.Source = remote.Source
	}
	if len(local.Models) == 0 {
		local.Models = append([]string(nil), remote.Models...)
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
