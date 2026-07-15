package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
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
	Pools    []authPool        `json:"pools"`
	Bindings []authPoolBinding `json:"bindings"`
}

type authPoolPayload struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	AuthIDs      []string `json:"auth_ids"`
	AccountTypes []string `json:"account_types"`
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
		return authPoolStatus{}, err
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
	pool := authPool{ID: strings.TrimSpace(payload.ID), Name: strings.TrimSpace(payload.Name), Description: strings.TrimSpace(payload.Description), AuthIDs: payload.AuthIDs, AccountTypes: payload.AccountTypes, Enabled: true}
	if pool.ID == "" || pool.Name == "" {
		return authPool{}, validationError("pool id and name are required")
	}
	var response struct {
		Pool authPool `json:"pool"`
	}
	if err := a.authPoolPluginRequest(ctx, http.MethodPost, "/pools", pool, &response); err != nil {
		return authPool{}, err
	}
	return response.Pool, nil
}

func (a *App) deleteAuthPool(ctx context.Context, id string) error {
	if err := a.authPoolPluginRequest(ctx, http.MethodDelete, "/pools?id="+urlQueryEscape(id), nil, nil); err != nil {
		return err
	}
	_, err := a.db.ExecContext(ctx, `DELETE FROM user_api_key_pools WHERE pool_id = ?`, id)
	return err
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

func (a *App) authPoolPluginRequest(ctx context.Context, method, path string, body any, target any) error {
	cfg, err := a.loadConfig(ctx)
	if err != nil {
		return err
	}
	if strings.TrimSpace(cfg.Collector.CLIProxyURL) == "" || strings.TrimSpace(cfg.Collector.ManagementKey) == "" {
		return validationError("CPA URL and management key are required")
	}
	response, payload, err := doJSON(ctx, httpClient(apiKeySyncTimeout), method, makeURL(cfg.Collector.CLIProxyURL, "/v0/management/plugins/"+authPoolPluginID+path, nil), managementHeaders(cfg.Collector.ManagementKey), body)
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

func urlQueryEscape(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, "%", "%25"), " ", "%20")
}
