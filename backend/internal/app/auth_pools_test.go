package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
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
		{name: "plugin events", path: "/api/auth-pools/plugin-events", want: []string{"plugin-events"}},
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

func TestAuthPoolModelSyncAsyncCoalescesAndBacksOff(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	cpa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v0/management/plugins/cpa-auth-pool/status" {
			http.NotFound(w, r)
			return
		}
		if calls.Add(1) == 1 {
			close(started)
		}
		<-release
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer cpa.Close()

	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())
	app, err := NewWithOptions(context.Background(), NewOptions{Migrate: true})
	if err != nil {
		t.Fatalf("NewWithOptions failed: %v", err)
	}
	defer app.Close()
	cfg, err := app.loadConfig(context.Background())
	if err != nil {
		t.Fatalf("loadConfig failed: %v", err)
	}
	cfg.Collector.CLIProxyURL = cpa.URL
	cfg.Collector.ManagementKey = "secret"
	if err := app.saveConfig(context.Background(), cfg); err != nil {
		t.Fatalf("saveConfig failed: %v", err)
	}

	app.syncAuthPoolModelsAsync()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("model sync did not start")
	}
	app.syncAuthPoolModelsAsync()
	if got := calls.Load(); got != 1 {
		t.Fatalf("concurrent model sync calls = %d, want 1", got)
	}
	close(release)
	deadline := time.Now().Add(2 * time.Second)
	for {
		app.authPoolSyncMu.Lock()
		running := app.authPoolSyncRun
		next := app.authPoolSyncNext
		app.authPoolSyncMu.Unlock()
		if !running {
			if next.IsZero() || !next.After(time.Now()) {
				t.Fatalf("failed sync did not set backoff: %v", next)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("model sync did not finish")
		}
		time.Sleep(10 * time.Millisecond)
	}
	app.syncAuthPoolModelsAsync()
	time.Sleep(30 * time.Millisecond)
	if got := calls.Load(); got != 1 {
		t.Fatalf("model sync retried during backoff: calls=%d", got)
	}
}

func TestAuthPoolResolvedSyncAsyncUsesSuccessInterval(t *testing.T) {
	var calls atomic.Int32
	cpa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/v0/management/plugins/cpa-auth-pool/status" {
			calls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{"pools": []any{}})
			return
		}
		http.NotFound(w, r)
	}))
	defer cpa.Close()

	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())
	app, err := NewWithOptions(context.Background(), NewOptions{Migrate: true})
	if err != nil {
		t.Fatalf("NewWithOptions failed: %v", err)
	}
	defer app.Close()
	cfg, err := app.loadConfig(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	cfg.Collector.CLIProxyURL = cpa.URL
	cfg.Collector.ManagementKey = "secret"
	if err := app.saveConfig(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}

	app.syncAuthPoolResolvedAuthIDsAsync()
	deadline := time.Now().Add(2 * time.Second)
	for {
		app.authPoolResolvedSyncMu.Lock()
		running := app.authPoolResolvedSyncRun
		next := app.authPoolResolvedNext
		app.authPoolResolvedSyncMu.Unlock()
		if !running {
			if next.IsZero() || !next.After(time.Now()) {
				t.Fatalf("successful resolved sync did not set a refresh interval: %v", next)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("resolved sync did not finish")
		}
		time.Sleep(10 * time.Millisecond)
	}
	app.syncAuthPoolResolvedAuthIDsAsync()
	time.Sleep(30 * time.Millisecond)
	if got := calls.Load(); got != 1 {
		t.Fatalf("resolved sync repeated inside success interval: calls=%d", got)
	}
}

func TestAuthPoolStatusRestoresLocalPoolsWhenPluginStateIsEmpty(t *testing.T) {
	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())
	postCount := 0
	bindingPostCount := 0
	restoredPools := []authPool{}
	restoredBindings := []authPoolBinding{}
	cpa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v0/management/plugins/cpa-auth-pool/status":
			_ = json.NewEncoder(w).Encode(map[string]any{"pools": restoredPools, "bindings": restoredBindings})
		case r.Method == http.MethodPost && r.URL.Path == "/v0/management/plugins/cpa-auth-pool/pools":
			postCount++
			var pool authPool
			if err := json.NewDecoder(r.Body).Decode(&pool); err != nil {
				t.Fatalf("decode pool: %v", err)
			}
			restoredPools = append(restoredPools, pool)
			_ = json.NewEncoder(w).Encode(map[string]any{"pool": pool})
		case r.Method == http.MethodPost && r.URL.Path == "/v0/management/plugins/cpa-auth-pool/bindings":
			bindingPostCount++
			var binding authPoolBinding
			if err := json.NewDecoder(r.Body).Decode(&binding); err != nil {
				t.Fatalf("decode binding: %v", err)
			}
			restoredBindings = append(restoredBindings, binding)
			_ = json.NewEncoder(w).Encode(map[string]any{"binding": binding})
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
	configureKeeperTestCPA(t, app, cpa.URL, nil)
	seed := authPool{ID: "plus", Name: "Plus", Description: "plus pool", AuthIDs: []string{"auth-a"}, AccountTypes: []string{"plus"}, Models: []string{"gpt-5.5 xhigh"}, Enabled: true}
	if err := app.saveLocalAuthPool(context.Background(), seed); err != nil {
		t.Fatalf("save local pool: %v", err)
	}
	now := dbTime(time.Now())
	apiKeyHash := hashAPIKey("sk-local")
	if _, err := app.db.Exec(`
		INSERT INTO users (id, username, is_admin, created_at, updated_at) VALUES (10, 'pool-user', 0, ?, ?)
	`, now, now); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := app.db.Exec(`
		INSERT INTO user_api_keys (api_key_hash, user_id, api_key, description, created_at, updated_at) VALUES (?, 10, 'sk-local', 'local', ?, ?)
	`, apiKeyHash, now, now); err != nil {
		t.Fatalf("insert api key: %v", err)
	}
	if _, err := app.db.Exec(`
		INSERT INTO user_api_key_pools (api_key_hash, pool_id, created_at, updated_at) VALUES (?, 'plus', ?, ?)
	`, apiKeyHash, now, now); err != nil {
		t.Fatalf("insert api key pool: %v", err)
	}

	status, err := app.authPoolStatus(context.Background(), nil)
	if err != nil {
		t.Fatalf("authPoolStatus failed: %v", err)
	}
	if postCount != 1 {
		t.Fatalf("restore posts = %d, want 1", postCount)
	}
	if bindingPostCount != 1 {
		t.Fatalf("binding restore posts = %d, want 1", bindingPostCount)
	}
	if len(status.Pools) != 1 || status.Pools[0].ID != seed.ID || status.Pools[0].Models[0] != seed.Models[0] {
		t.Fatalf("status pools = %#v, want restored seed", status.Pools)
	}
	if len(restoredBindings) != 1 || restoredBindings[0].APIKeyHash != apiKeyHash || restoredBindings[0].PoolID != seed.ID {
		t.Fatalf("restored bindings = %#v, want local api key binding", restoredBindings)
	}
}

func TestBindAPIKeyToAuthPoolRequiresUserEntitlement(t *testing.T) {
	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())
	bindingCalls := 0
	bindingDeleteCalls := 0
	cpa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v0/management/plugins/cpa-auth-pool/bindings":
			bindingCalls++
			var binding authPoolBinding
			if err := json.NewDecoder(r.Body).Decode(&binding); err != nil {
				t.Fatalf("decode binding: %v", err)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"binding": binding})
			return
		case r.Method == http.MethodDelete && r.URL.Path == "/v0/management/plugins/cpa-auth-pool/bindings":
			bindingDeleteCalls++
			_ = json.NewEncoder(w).Encode(map[string]any{"deleted": true})
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
	configureKeeperTestCPA(t, app, cpa.URL, nil)

	ctx := context.Background()
	pool := authPool{ID: "paid", Name: "Paid", AuthIDs: []string{"auth-paid"}, Models: []string{"gpt-paid"}, Enabled: true}
	if err := app.saveLocalAuthPool(ctx, pool); err != nil {
		t.Fatalf("saveLocalAuthPool: %v", err)
	}
	now := dbTime(time.Now())
	apiKeyHash := hashAPIKey("sk-user")
	if _, err := app.db.Exec(`
		INSERT INTO users (id, username, is_admin, created_at, updated_at)
		VALUES (10, 'user-a', 0, ?, ?)
	`, now, now); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := app.db.Exec(`
		INSERT INTO user_api_keys (api_key_hash, user_id, api_key, description, created_at, updated_at)
		VALUES (?, 10, 'sk-user', 'user key', ?, ?)
	`, apiKeyHash, now, now); err != nil {
		t.Fatalf("insert api key: %v", err)
	}
	user := &AuthUser{ID: 10, Username: "user-a"}
	payload := authPoolBindingPayload{APIKeyHash: apiKeyHash, PoolID: pool.ID}

	if _, err := app.bindAPIKeyToAuthPool(ctx, user, payload); err == nil || !strings.Contains(err.Error(), "auth pool access denied") {
		t.Fatalf("bind without entitlement error = %v, want access denied", err)
	}
	if bindingCalls != 0 {
		t.Fatalf("plugin binding calls = %d, want 0 before authorization", bindingCalls)
	}

	if err := app.replaceAuthPoolEntitlements(ctx, pool.ID, []int{user.ID}); err != nil {
		t.Fatalf("replaceAuthPoolEntitlements: %v", err)
	}
	binding, err := app.bindAPIKeyToAuthPool(ctx, user, payload)
	if err != nil {
		t.Fatalf("bind with entitlement: %v", err)
	}
	if binding.PoolID != pool.ID || binding.APIKeyHash != apiKeyHash {
		t.Fatalf("binding = %#v, want authorized pool binding", binding)
	}
	if bindingCalls != 1 {
		t.Fatalf("plugin binding calls = %d, want 1 after authorization", bindingCalls)
	}
	if err := app.replaceAuthPoolEntitlements(ctx, pool.ID, nil); err != nil {
		t.Fatalf("revoke auth pool entitlement: %v", err)
	}
	if bindingDeleteCalls != 1 {
		t.Fatalf("plugin binding delete calls = %d, want 1 after entitlement revocation", bindingDeleteCalls)
	}
	bindings, err := app.localAuthPoolBindings(ctx, nil)
	if err != nil {
		t.Fatalf("localAuthPoolBindings: %v", err)
	}
	if len(bindings) != 0 {
		t.Fatalf("bindings after entitlement revocation = %#v, want none", bindings)
	}
	if _, err := app.bindAPIKeyToAuthPool(ctx, user, payload); err == nil || !strings.Contains(err.Error(), "auth pool access denied") {
		t.Fatalf("bind after entitlement revocation error = %v, want access denied", err)
	}
}

func TestAuthPoolModelChecksUseLocalLastGoodSnapshot(t *testing.T) {
	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())
	app, err := New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer app.Close()
	configureKeeperTestCPA(t, app, "http://127.0.0.1:1", func(cfg *AppConfig) {
		cfg.AuthPoolProxyTargets = []AuthPoolProxyTargetConfig{{
			ID: "offline", Name: "Offline CPA", CPAURL: "http://127.0.0.1:1", ManagementKey: "mgmt", APIKey: "proxy-key", Enabled: true,
		}}
	})

	ctx := context.Background()
	if err := app.saveLocalAuthPool(ctx, authPool{ID: "paid", Name: "Paid", Models: []string{"gpt-paid"}, Enabled: true}); err != nil {
		t.Fatalf("saveLocalAuthPool: %v", err)
	}
	now := dbTime(time.Now())
	apiKeyHash := hashAPIKey("sk-local-model")
	if _, err := app.db.Exec(`INSERT INTO users (id, username, is_admin, created_at, updated_at) VALUES (20, 'model-user', 0, ?, ?)`, now, now); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := app.db.Exec(`INSERT INTO user_api_keys (api_key_hash, user_id, api_key, description, created_at, updated_at) VALUES (?, 20, 'sk-local-model', 'model key', ?, ?)`, apiKeyHash, now, now); err != nil {
		t.Fatalf("insert api key: %v", err)
	}
	if _, err := app.db.Exec(`INSERT INTO user_api_key_pools (api_key_hash, pool_id, created_at, updated_at) VALUES (?, 'paid', ?, ?)`, apiKeyHash, now, now); err != nil {
		t.Fatalf("insert binding: %v", err)
	}

	if err := app.ensureAPIKeyModelAllowedByPool(ctx, apiKeyHash, "gpt-paid"); err != nil {
		t.Fatalf("ensureAPIKeyModelAllowedByPool used remote state: %v", err)
	}
	filters, err := app.authPoolModelFiltersForAPIKeys(ctx, []string{apiKeyHash})
	if err != nil {
		t.Fatalf("authPoolModelFiltersForAPIKeys used remote state: %v", err)
	}
	if !filters[apiKeyHash]["gpt-paid"] {
		t.Fatalf("local model filters = %#v, want gpt-paid", filters)
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
	app.invalidateConfigCache()
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

func TestUpsertAuthPoolSendsResolvedDynamicAuthIDs(t *testing.T) {
	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())
	var savedPool authPool
	cpa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v0/management/auth-files":
			_ = json.NewEncoder(w).Encode(map[string]any{"files": []map[string]any{
				{"name": "karenmclean0894+go1@gmail.com.json", "account_type": "k12"},
				{"name": "codex-voguish_voyage_7e@icloud.com-plus.json", "account_type": "plus"},
			}})
		case r.Method == http.MethodGet && r.URL.Path == "/v0/management/auth-files/models":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{{"id": "gpt-5.5"}}})
		case r.Method == http.MethodPost && r.URL.Path == "/v0/management/plugins/cpa-auth-pool/pools":
			if err := json.NewDecoder(r.Body).Decode(&savedPool); err != nil {
				t.Fatalf("decode pool: %v", err)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"pool": savedPool})
		case r.Method == http.MethodGet && r.URL.Path == "/v0/management/plugins/cpa-auth-pool/status":
			_ = json.NewEncoder(w).Encode(map[string]any{"pools": []authPool{savedPool}})
		case r.Method == http.MethodPost && r.URL.Path == "/v0/management/plugins/cpa-auth-pool/auth-models":
			_ = json.NewEncoder(w).Encode(map[string]any{"synced": true})
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
	configureKeeperTestCPA(t, app, cpa.URL, nil)

	pool, err := app.upsertAuthPool(t.Context(), authPoolPayload{
		ID:           "002",
		Name:         "plus/team",
		AccountTypes: []string{"k12", "team", "plus"},
	})
	if err != nil {
		t.Fatalf("upsertAuthPool: %v", err)
	}
	want := []string{"codex-voguish_voyage_7e@icloud.com-plus.json", "karenmclean0894+go1@gmail.com.json"}
	if !reflect.DeepEqual(savedPool.ResolvedAuthIDs, want) {
		t.Fatalf("saved resolved auth ids = %#v, want %#v", savedPool.ResolvedAuthIDs, want)
	}
	if !reflect.DeepEqual(pool.ResolvedAuthIDs, want) {
		t.Fatalf("response resolved auth ids = %#v, want %#v", pool.ResolvedAuthIDs, want)
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

func TestUpsertAuthPoolRejectsCatalogFailureBeforePluginMutation(t *testing.T) {
	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())
	poolPosts := 0
	cpa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v0/management/auth-files":
			_ = json.NewEncoder(w).Encode(map[string]any{"files": []map[string]any{{"name": "auth-a", "type": "codex"}}})
		case r.Method == http.MethodGet && r.URL.Path == "/v0/management/auth-files/models":
			http.Error(w, "catalog unavailable", http.StatusBadGateway)
		case r.Method == http.MethodPost && r.URL.Path == "/v0/management/plugins/cpa-auth-pool/pools":
			poolPosts++
			_ = json.NewEncoder(w).Encode(map[string]any{"pool": map[string]any{"id": "paid", "name": "Paid", "enabled": true}})
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
	configureKeeperTestCPA(t, app, cpa.URL, nil)

	_, err = app.upsertAuthPool(t.Context(), authPoolPayload{ID: "paid", Name: "Paid", AuthIDs: []string{"auth-a"}})
	if err == nil || !strings.Contains(err.Error(), "catalog refresh failed") {
		t.Fatalf("upsertAuthPool error = %v, want catalog refresh failure", err)
	}
	if poolPosts != 0 {
		t.Fatalf("plugin pool posts = %d, want 0 when model preflight fails", poolPosts)
	}
}

func TestSyncAuthPoolModelsKeepsLastSnapshotOnPartialFailure(t *testing.T) {
	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())
	membershipPosts := 0
	modelSnapshotPosts := 0
	cpa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v0/management/plugins/cpa-auth-pool/status":
			_ = json.NewEncoder(w).Encode(map[string]any{"pools": []map[string]any{{
				"id": "paid", "name": "Paid", "auth_ids": []string{"auth-a", "auth-b"}, "models": []string{"last-good"}, "enabled": true,
			}}})
		case r.Method == http.MethodGet && r.URL.Path == "/v0/management/auth-files":
			_ = json.NewEncoder(w).Encode(map[string]any{"files": []map[string]any{
				{"name": "auth-a", "type": "codex"},
				{"name": "auth-b", "type": "codex"},
			}})
		case r.Method == http.MethodGet && r.URL.Path == "/v0/management/auth-files/models" && r.URL.Query().Get("name") == "auth-a":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{{"id": "gpt-a"}}})
		case r.Method == http.MethodGet && r.URL.Path == "/v0/management/auth-files/models" && r.URL.Query().Get("name") == "auth-b":
			http.Error(w, "temporary failure", http.StatusBadGateway)
		case r.Method == http.MethodPost && r.URL.Path == "/v0/management/plugins/cpa-auth-pool/auth-models":
			var payload map[string]json.RawMessage
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode auth models payload: %v", err)
			}
			if _, ok := payload["auth_models"]; ok {
				modelSnapshotPosts++
			} else {
				membershipPosts++
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
	configureKeeperTestCPA(t, app, cpa.URL, nil)

	err = app.syncAuthPoolModels(t.Context())
	if err == nil || !strings.Contains(err.Error(), "auth-b") {
		t.Fatalf("syncAuthPoolModels error = %v, want auth-b failure", err)
	}
	if membershipPosts != 1 {
		t.Fatalf("membership posts = %d, want 1 before model refresh", membershipPosts)
	}
	if modelSnapshotPosts != 0 {
		t.Fatalf("model snapshot posts = %d, want 0 on partial refresh", modelSnapshotPosts)
	}
}

func TestAuthPoolsNeedModelSyncForUnresolvedDynamicPool(t *testing.T) {
	if !authPoolsNeedModelSync([]authPool{{
		ID:           "002",
		AccountTypes: []string{"k12", "team", "plus"},
		Models:       []string{"gpt-5.5"},
	}}) {
		t.Fatal("dynamic pool without resolved auth ids should require synchronization")
	}
	if authPoolsNeedModelSync([]authPool{{
		ID:              "002",
		AccountTypes:    []string{"k12", "team", "plus"},
		ResolvedAuthIDs: []string{"karen.json"},
		Models:          []string{"gpt-5.5"},
	}}) {
		t.Fatal("resolved dynamic pool with models should not require synchronization")
	}
}

func TestSyncAuthPoolResolvedAuthIDsRefreshesNewDynamicAccount(t *testing.T) {
	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())
	var membershipPayload map[string]any
	var priorityPayload map[string]any
	var patchedPriority *int
	cpa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v0/management/plugins/cpa-auth-pool/status":
			_ = json.NewEncoder(w).Encode(map[string]any{"pools": []map[string]any{{
				"id": "002", "name": "plus/team", "account_types": []string{"k12", "team", "plus"},
				"resolved_auth_ids": []string{"old.json"}, "models": []string{"gpt-5.5"}, "enabled": true,
			}}})
		case r.Method == http.MethodGet && r.URL.Path == "/v0/management/auth-files":
			_ = json.NewEncoder(w).Encode(map[string]any{"files": []map[string]any{{
				"name": "new.json", "type": "codex", "account_type": "team", "priority": 50, "disabled": false,
			}}})
		case r.Method == http.MethodPost && r.URL.Path == "/v0/management/plugins/cpa-auth-pool/auth-models":
			if err := json.NewDecoder(r.Body).Decode(&membershipPayload); err != nil {
				t.Fatalf("decode membership payload: %v", err)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		case r.Method == http.MethodPost && r.URL.Path == "/v0/management/plugins/cpa-auth-pool/auth-priorities":
			if err := json.NewDecoder(r.Body).Decode(&priorityPayload); err != nil {
				t.Fatalf("decode priority payload: %v", err)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"status": map[string]any{
				"scheduler_priorities":    true,
				"auth_types":              priorityPayload["auth_types"],
				"type_priorities":         priorityPayload["type_priorities"],
				"auth_priority_overrides": priorityPayload["auth_priority_overrides"],
			}})
		case r.Method == http.MethodPatch && r.URL.Path == "/v0/management/auth-files/fields":
			var payload struct {
				Priority *int `json:"priority"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode priority patch: %v", err)
			}
			patchedPriority = payload.Priority
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
	configureKeeperTestCPA(t, app, cpa.URL, nil)

	if err := app.syncAuthPoolResolvedAuthIDs(t.Context()); err != nil {
		t.Fatalf("syncAuthPoolResolvedAuthIDs failed: %v", err)
	}
	resolved, ok := membershipPayload["pool_resolved_auth_ids"].(map[string]any)
	if !ok {
		t.Fatalf("membership payload = %#v, want pool_resolved_auth_ids", membershipPayload)
	}
	ids, ok := resolved["002"].([]any)
	if !ok || len(ids) != 1 || ids[0] != "new.json" {
		t.Fatalf("resolved IDs = %#v, want new.json", resolved["002"])
	}
	overrides, ok := priorityPayload["auth_priority_overrides"].(map[string]any)
	if !ok || overrides["new.json"] != float64(50) {
		t.Fatalf("priority overrides = %#v, want new.json=50", priorityPayload["auth_priority_overrides"])
	}
	if replace, _ := priorityPayload["replace_overrides"].(bool); !replace {
		t.Fatalf("replace_overrides = %#v, want true", priorityPayload["replace_overrides"])
	}
	if patchedPriority == nil || *patchedPriority != 0 {
		t.Fatalf("patched host priority = %v, want 0", patchedPriority)
	}
}

func TestApplyAuthPoolLogicalPrioritiesUsesOverrideThenType(t *testing.T) {
	app := &App{}
	app.cacheAuthPoolPriorities(
		map[string]string{"manual.json": "k12", "typed.json": "plus", "degraded.json": "free"},
		map[string]int{"k12": 5, "plus": 12, "free": 1},
		map[string]int{"manual.json": 50},
		true,
	)
	zero := 0
	degraded := -1
	accounts := []keeperAccount{
		{Name: "manual.json", AccountType: stringPtr("k12"), Priority: &zero},
		{Name: "typed.json", AccountType: stringPtr("plus"), Priority: &zero},
		{Name: "degraded.json", AccountType: stringPtr("free"), Priority: &degraded},
	}
	app.applyAuthPoolLogicalPriorities(accounts)
	if accounts[0].Priority == nil || *accounts[0].Priority != 50 {
		t.Fatalf("manual priority = %v, want 50", accounts[0].Priority)
	}
	if accounts[1].Priority == nil || *accounts[1].Priority != 12 {
		t.Fatalf("type priority = %v, want 12", accounts[1].Priority)
	}
	if accounts[2].Priority == nil || *accounts[2].Priority != -1 {
		t.Fatalf("degraded priority = %v, want -1", accounts[2].Priority)
	}
}

func TestCacheAuthPoolPrioritiesReplacesRemovedOverrides(t *testing.T) {
	app := &App{}
	app.cacheAuthPoolPriorities(nil, nil, map[string]int{"removed.json": 50}, true)
	app.cacheAuthPoolPriorities(map[string]string{}, map[string]int{}, nil, true)
	app.authPoolPriorityMu.RLock()
	_, exists := app.authPoolAuthOverrides["removed.json"]
	app.authPoolPriorityMu.RUnlock()
	if exists {
		t.Fatal("stale auth priority override remained after a full status refresh")
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
	app.invalidateConfigCache()
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
