package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestAuthPoolPathParts(t *testing.T) {
	tests := []struct {
		name string
		path string
		want []string
	}{
		{name: "root without trailing slash", path: "/api/auth-pools", want: nil},
		{name: "root with trailing slash", path: "/api/auth-pools/", want: nil},
		{name: "accounts root", path: "/api/auth-pools/accounts", want: []string{"accounts"}},
		{name: "api key account", path: "/api/auth-pools/accounts/api-key", want: []string{"accounts", "api-key"}},
		{name: "bindings root", path: "/api/auth-pools/bindings", want: []string{"bindings"}},
		{name: "binding hash", path: "/api/auth-pools/bindings/hash-1", want: []string{"bindings", "hash-1"}},
		{name: "pool id", path: "/api/auth-pools/pool-a", want: []string{"pool-a"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := authPoolPathParts(test.path)
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("authPoolPathParts(%q) = %#v, want %#v", test.path, got, test.want)
			}
		})
	}
}

func TestListAuthPoolAccountsIncludesRemoteGeminiAndGrok(t *testing.T) {
	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())
	cpa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet && r.URL.Path == "/v0/management/auth-files" {
			_ = json.NewEncoder(w).Encode(map[string]any{"files": []map[string]any{
				{"name": "gemini-account.json", "type": "gemini", "disabled": false},
				{"name": "xai-grok-key.json", "provider": "xai", "disabled": false},
			}})
			return
		}
		http.NotFound(w, r)
	}))
	defer cpa.Close()
	app, err := New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer app.Close()
	_, err = app.db.Exec(`
		UPDATE app_settings SET cliaproxy_url = ?, management_key = ?, codex_keeper_settings = ?
	`, cpa.URL, "secret", `{"cpa_timeout_seconds":2,"max_retries":0}`)
	if err != nil {
		t.Fatalf("update settings: %v", err)
	}
	accounts, err := app.listAuthPoolAccounts(t.Context())
	if err != nil {
		t.Fatalf("listAuthPoolAccounts failed: %v", err)
	}
	got := map[string]string{}
	for _, account := range accounts {
		got[account.Name] = stringPtrValue(account.AccountType)
	}
	if got["gemini-account.json"] != "gemini" {
		t.Fatalf("gemini type = %q, want gemini; all = %#v", got["gemini-account.json"], got)
	}
	if got["xai-grok-key.json"] != "grok" {
		t.Fatalf("grok type = %q, want grok; all = %#v", got["xai-grok-key.json"], got)
	}
}

func TestUpdateAuthPoolProxyConfigWritesCodexConcurrencyLimits(t *testing.T) {
	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())
	var limitsPayload map[string]any
	cpa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v0/management/plugins/cpa-auth-pool/proxy-keys":
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		case r.Method == http.MethodPost && r.URL.Path == "/v0/management/plugins/cpa-auth-pool/codex-concurrency-limits":
			if err := json.NewDecoder(r.Body).Decode(&limitsPayload); err != nil {
				t.Fatalf("decode limits payload: %v", err)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		case r.Method == http.MethodGet && r.URL.Path == "/v0/management/plugins/cpa-auth-pool/status":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"pools":       []any{},
				"bindings":    []any{},
				"concurrency": map[string]any{"counts": map[string]int{}, "limits": map[string]int{"plus": 2, "default": 1}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer cpa.Close()

	app, err := New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer app.Close()
	config, err := app.updateAuthPoolProxyConfig(t.Context(), authPoolProxyConfigPayload{
		Targets: []authPoolProxyTargetPayload{{
			ID: "main", Name: "Main", CPAURL: cpa.URL, ManagementKey: "mgmt", APIKey: "sk-forward", Enabled: true,
		}},
		CodexConcurrencyLimits: map[string]int{"plus": 2, "default": 1, "unknown": 9, "free": -1},
	})
	if err != nil {
		t.Fatalf("updateAuthPoolProxyConfig failed: %v", err)
	}
	limits, ok := limitsPayload["limits"].(map[string]any)
	if !ok {
		t.Fatalf("limits payload = %#v", limitsPayload)
	}
	if limits["plus"] != float64(2) || limits["default"] != float64(1) || limits["free"] != float64(0) {
		t.Fatalf("limits = %#v, want normalized plus/default/free", limits)
	}
	if _, ok := limits["unknown"]; ok {
		t.Fatalf("limits = %#v, unknown tier should be dropped", limits)
	}
	if config.CodexConcurrencyLimits["plus"] != 2 || config.CodexConcurrencyLimits["default"] != 1 {
		t.Fatalf("response limits = %#v", config.CodexConcurrencyLimits)
	}
}

func TestAddAuthPoolAPIKeyAccountWritesCPAConfig(t *testing.T) {
	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())
	var putPayload map[string]any
	cpa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v0/management/xai-api-key":
			_ = json.NewEncoder(w).Encode(map[string]any{"xai-api-key": []map[string]any{{"api-key": "xai-old", "base-url": "https://api.x.ai/v1"}}})
		case r.Method == http.MethodPut && r.URL.Path == "/v0/management/xai-api-key":
			if err := json.NewDecoder(r.Body).Decode(&putPayload); err != nil {
				t.Fatalf("decode put payload: %v", err)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		default:
			http.NotFound(w, r)
		}
	}))
	defer cpa.Close()

	app, err := New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer app.Close()
	_, err = app.db.Exec(`
		UPDATE app_settings SET cliaproxy_url = ?, management_key = ?, codex_keeper_settings = ?
	`, cpa.URL, "secret", `{"cpa_timeout_seconds":2,"max_retries":0}`)
	if err != nil {
		t.Fatalf("update settings: %v", err)
	}
	priority := 7
	websockets := true
	result, err := app.addAuthPoolAPIKeyAccount(t.Context(), authPoolAPIKeyAccountPayload{
		Provider: "grok", APIKey: "xai-new", Prefix: "grok-team", Priority: &priority, Websockets: &websockets,
	})
	if err != nil {
		t.Fatalf("addAuthPoolAPIKeyAccount failed: %v", err)
	}
	if result.Provider != "xai" || result.AccountType != "grok" || result.Count != 2 {
		t.Fatalf("result = %#v, want xai/grok count 2", result)
	}
	items, ok := putPayload["items"].([]any)
	if !ok || len(items) != 2 {
		t.Fatalf("items = %#v, want two items", putPayload["items"])
	}
	created, ok := items[1].(map[string]any)
	if !ok {
		t.Fatalf("created item = %#v", items[1])
	}
	if created["api-key"] != "xai-new" || created["prefix"] != "grok-team" || created["base-url"] != "https://api.x.ai/v1" || created["websockets"] != true {
		t.Fatalf("created item = %#v, want normalized xai key", created)
	}
}
