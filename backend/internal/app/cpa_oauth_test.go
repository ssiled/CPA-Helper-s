package app_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	backendApp "cpa-helper/backend/internal/app"
)

func TestCPAOAuthAuthURLIsAvailableToLoggedInMembers(t *testing.T) {
	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())

	upstreamCalls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls++
		if r.Method != http.MethodGet || r.URL.Path != "/v0/management/codex-auth-url" {
			t.Fatalf("unexpected upstream request %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-management-key" {
			t.Fatalf("Authorization = %q, want management bearer", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "ok",
			"url":    "https://login.example/oauth",
			"state":  "oauth-state",
		})
	}))
	defer upstream.Close()

	app, err := backendApp.New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer app.Close()

	handler := app.Routes()
	adminCookies := requestJSON(t, handler, http.MethodPost, "/api/auth/setup", map[string]any{
		"username": "admin",
		"password": "test-password",
		"nickname": "Admin",
	}, nil, nil)
	requestJSON(t, handler, http.MethodPut, "/api/settings", map[string]any{
		"cliaproxy_url":  upstream.URL,
		"management_key": "test-management-key",
	}, adminCookies, nil)
	requestJSON(t, handler, http.MethodPost, "/api/users", map[string]any{
		"username": "member",
		"password": "member-password",
		"nickname": "Member",
		"is_admin": false,
	}, adminCookies, nil)
	memberCookies := requestJSON(t, handler, http.MethodPost, "/api/auth/login", map[string]any{
		"username": "member",
		"password": "member-password",
	}, nil, nil)

	var response struct {
		Provider string `json:"provider"`
		Status   string `json:"status"`
		URL      string `json:"url"`
		State    string `json:"state"`
	}
	requestJSON(t, handler, http.MethodPost, "/api/cpa-oauth/auth-url", map[string]any{
		"provider": "codex",
	}, memberCookies, &response)
	if upstreamCalls != 1 {
		t.Fatalf("upstream calls = %d, want 1", upstreamCalls)
	}
	if response.Provider != "codex" || response.Status != "ok" || response.URL == "" || response.State != "oauth-state" {
		t.Fatalf("response = %#v, want proxied OAuth auth URL", response)
	}
}

func TestCPAOAuthRequiresLoginAndCPAConfig(t *testing.T) {
	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())

	app, err := backendApp.New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer app.Close()

	handler := app.Routes()
	cookies := requestJSON(t, handler, http.MethodPost, "/api/auth/setup", map[string]any{
		"username": "admin",
		"password": "test-password",
		"nickname": "Admin",
	}, nil, nil)

	requestJSONExpectStatus(t, handler, http.MethodGet, "/api/cpa-oauth/providers", nil, nil, http.StatusUnauthorized)
	requestJSONExpectStatus(t, handler, http.MethodPost, "/api/cpa-oauth/auth-url", map[string]any{
		"provider": "codex",
	}, cookies, http.StatusUnprocessableEntity)
}
