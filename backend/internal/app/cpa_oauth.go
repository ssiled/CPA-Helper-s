package app

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const cpaOAuthTimeout = 15 * time.Second

var cpaOAuthProviders = map[string]string{
	"codex":       "/v0/management/codex-auth-url",
	"anthropic":   "/v0/management/anthropic-auth-url",
	"gemini":      "/v0/management/gemini-cli-auth-url",
	"antigravity": "/v0/management/antigravity-auth-url",
	"kimi":        "/v0/management/kimi-auth-url",
}

type cpaOAuthAuthURLRequest struct {
	Provider  string `json:"provider"`
	ProjectID string `json:"project_id"`
}

type cpaOAuthCallbackRequest struct {
	Provider    string `json:"provider"`
	RedirectURL string `json:"redirect_url"`
}

func (a *App) handleCPAOAuth(w http.ResponseWriter, r *http.Request) error {
	if _, err := a.readyUser(r.Context(), r); err != nil {
		return err
	}

	parts := splitPath(r.URL.Path, "/api/cpa-oauth")
	if len(parts) != 1 {
		return notFoundError("Not Found")
	}

	switch parts[0] {
	case "providers":
		if err := requireMethod(r, http.MethodGet); err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, map[string]any{"providers": cpaOAuthProviderList()})
		return nil
	case "auth-url":
		if err := requireMethod(r, http.MethodPost); err != nil {
			return err
		}
		return a.handleCPAOAuthURL(w, r)
	case "status":
		if err := requireMethod(r, http.MethodGet); err != nil {
			return err
		}
		return a.handleCPAOAuthStatus(w, r)
	case "callback":
		if err := requireMethod(r, http.MethodPost); err != nil {
			return err
		}
		return a.handleCPAOAuthCallback(w, r)
	default:
		return notFoundError("Not Found")
	}
}

func (a *App) handleCPAOAuthURL(w http.ResponseWriter, r *http.Request) error {
	var payload cpaOAuthAuthURLRequest
	if err := decodeJSON(r, &payload); err != nil {
		return err
	}
	provider, endpoint, err := normalizeCPAOAuthProvider(payload.Provider)
	if err != nil {
		return err
	}
	query := url.Values{}
	if provider == "gemini" && strings.TrimSpace(payload.ProjectID) != "" {
		query.Set("project_id", strings.TrimSpace(payload.ProjectID))
	}
	result, err := a.cpaOAuthRequest(r, http.MethodGet, endpoint, query, nil)
	if err != nil {
		return err
	}
	result["provider"] = provider
	writeJSON(w, http.StatusOK, result)
	return nil
}

func (a *App) handleCPAOAuthStatus(w http.ResponseWriter, r *http.Request) error {
	state := strings.TrimSpace(r.URL.Query().Get("state"))
	if state == "" {
		return validationError("OAuth state is required")
	}
	query := url.Values{}
	query.Set("state", state)
	result, err := a.cpaOAuthRequest(r, http.MethodGet, "/v0/management/get-auth-status", query, nil)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, result)
	return nil
}

func (a *App) handleCPAOAuthCallback(w http.ResponseWriter, r *http.Request) error {
	var payload cpaOAuthCallbackRequest
	if err := decodeJSON(r, &payload); err != nil {
		return err
	}
	provider, _, err := normalizeCPAOAuthProvider(payload.Provider)
	if err != nil {
		return err
	}
	redirectURL := strings.TrimSpace(payload.RedirectURL)
	if redirectURL == "" {
		return validationError("OAuth callback URL is required")
	}
	result, err := a.cpaOAuthRequest(r, http.MethodPost, "/v0/management/oauth-callback", nil, map[string]any{
		"provider":     provider,
		"redirect_url": redirectURL,
	})
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, result)
	return nil
}

func (a *App) cpaOAuthRequest(r *http.Request, method string, endpoint string, query url.Values, body any) (map[string]any, error) {
	cfg, err := a.loadConfig(r.Context())
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(cfg.Collector.CLIProxyURL) == "" || strings.TrimSpace(cfg.Collector.ManagementKey) == "" {
		return nil, validationError(apiKeySyncMissingConfigMessage)
	}
	response, payload, err := doJSON(r.Context(), httpClient(cpaOAuthTimeout), method, makeURL(cfg.Collector.CLIProxyURL, endpoint, query), managementHeaders(cfg.Collector.ManagementKey), body)
	if err != nil {
		return nil, appError("upstream_error", http.StatusBadGateway, "CPA OAuth 接口暂时不可用")
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, appError("upstream_error", http.StatusBadGateway, "CPA OAuth 接口返回失败")
	}
	var result map[string]any
	if err := json.Unmarshal(payload, &result); err != nil {
		return nil, appError("upstream_error", http.StatusBadGateway, "CPA OAuth 接口返回格式无效")
	}
	return result, nil
}

func normalizeCPAOAuthProvider(value string) (string, string, error) {
	provider := strings.ToLower(strings.TrimSpace(value))
	switch provider {
	case "claude":
		provider = "anthropic"
	case "gemini-cli":
		provider = "gemini"
	case "openai":
		provider = "codex"
	}
	endpoint, ok := cpaOAuthProviders[provider]
	if !ok {
		return "", "", validationError("Unsupported CPA OAuth provider")
	}
	return provider, endpoint, nil
}

func cpaOAuthProviderList() []map[string]string {
	return []map[string]string{
		{"id": "codex", "label": "Codex / OpenAI"},
		{"id": "anthropic", "label": "Claude"},
		{"id": "gemini", "label": "Gemini CLI"},
		{"id": "antigravity", "label": "Antigravity"},
		{"id": "kimi", "label": "Kimi"},
	}
}
