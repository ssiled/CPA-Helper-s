package app_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	backendApp "cpa-helper/backend/internal/app"
)

type apiKeyCreateResponse struct {
	APIKey     string `json:"api_key"`
	APIKeyHash string `json:"api_key_hash"`
}

func bindTestAPIKeyToAuthPool(t *testing.T, handler http.Handler, cookies []*http.Cookie, apiKeyHash string) {
	t.Helper()
	requestJSON(t, handler, http.MethodPost, "/api/auth-pools/bindings", map[string]any{
		"api_key_hash": apiKeyHash,
		"pool_id":      "test",
	}, cookies, nil)
}

func TestListAPIKeysReturnsEmptyArrayForFreshAccount(t *testing.T) {
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

	var keys []apiKeyCreateResponse
	requestJSON(t, handler, http.MethodGet, "/api/api-keys", nil, cookies, &keys)
	if keys == nil {
		t.Fatal("fresh API key list decoded as nil; want empty JSON array")
	}
	if len(keys) != 0 {
		t.Fatalf("fresh API key list length = %d, want 0", len(keys))
	}
}

func TestAccountModelRequestGuideUsesConfiguredURL(t *testing.T) {
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
	requestJSON(t, handler, http.MethodPut, "/api/settings", map[string]any{
		"cliaproxy_url":     "http://127.0.0.1:8317",
		"model_request_url": "http://models.example.local/proxy",
		"management_key":    "test-management-key",
		"collector_enabled": false,
	}, cookies, nil)

	var guide struct {
		ModelRequestURL    string `json:"model_request_url"`
		OpenAIBaseURL      string `json:"openai_base_url"`
		ChatCompletionsURL string `json:"chat_completions_url"`
	}
	requestJSON(t, handler, http.MethodGet, "/api/account/model-request", nil, cookies, &guide)
	if guide.ModelRequestURL != "http://models.example.local/proxy" {
		t.Fatalf("model_request_url = %q", guide.ModelRequestURL)
	}
	if guide.OpenAIBaseURL != "http://models.example.local/proxy/v1" {
		t.Fatalf("openai_base_url = %q", guide.OpenAIBaseURL)
	}
	if guide.ChatCompletionsURL != "http://models.example.local/proxy/v1/chat/completions" {
		t.Fatalf("chat_completions_url = %q", guide.ChatCompletionsURL)
	}

	requestJSON(t, handler, http.MethodPut, "/api/settings", map[string]any{
		"model_request_url": "http://models.example.local/v1",
	}, cookies, nil)
	requestJSON(t, handler, http.MethodGet, "/api/account/model-request", nil, cookies, &guide)
	if guide.ModelRequestURL != "http://models.example.local/v1" {
		t.Fatalf("model_request_url with existing /v1 = %q", guide.ModelRequestURL)
	}
	if guide.OpenAIBaseURL != "http://models.example.local/v1" {
		t.Fatalf("openai_base_url with existing /v1 = %q", guide.OpenAIBaseURL)
	}
	if guide.ChatCompletionsURL != "http://models.example.local/v1/chat/completions" {
		t.Fatalf("chat_completions_url with existing /v1 = %q", guide.ChatCompletionsURL)
	}
}

func TestCreateAPIKeyUsesKeyPolicyPluginWhenInstalled(t *testing.T) {
	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())

	pluginCalls := 0
	cpa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/v0/management/plugins/cpa-key-policy/keys" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		pluginCalls++
		if got := r.Header.Get("Authorization"); got != "Bearer mgmt-secret" {
			t.Fatalf("Authorization = %q, want management bearer", got)
		}
		var payload struct {
			ID      string `json:"id"`
			Name    string `json:"name"`
			Enabled bool   `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if payload.ID != "cpa-helper-user-1" || payload.Name == "" || !payload.Enabled {
			t.Fatalf("plugin payload = %#v", payload)
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"plain_key": "cpa_plugin_test_key",
			"generated": true,
			"key":       map[string]any{"id": payload.ID, "name": payload.Name, "key_preview": "cpa_...key"},
		})
	}))
	defer cpa.Close()

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
	requestJSON(t, handler, http.MethodPut, "/api/settings", map[string]any{
		"cliaproxy_url":     cpa.URL,
		"model_request_url": cpa.URL,
		"management_key":    "mgmt-secret",
	}, cookies, nil)

	created := apiKeyCreateResponse{}
	requestJSON(t, handler, http.MethodPost, "/api/api-keys", map[string]any{
		"description": "Plugin key",
	}, cookies, &created)

	if pluginCalls != 1 {
		t.Fatalf("plugin calls = %d, want 1", pluginCalls)
	}
	if created.APIKey == "" {
		t.Fatalf("api key is empty")
	}
}

func TestAccountModelRequestTestUsesCurrentUserAPIKey(t *testing.T) {
	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())

	const proxyKey = "sk-cpa-proxy"
	var expectedHash string
	chatCalls := 0
	cpa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v0/management/plugins/cpa-auth-pool/proxy-keys" && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(map[string]any{"proxy_key_count": 1})
		case r.URL.Path == "/v0/management/plugins/cpa-auth-pool/status" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{"pools": []map[string]any{{"id": "test", "name": "Test", "enabled": true, "models": []string{"gpt-test"}}}})
		case r.URL.Path == "/v0/management/plugins/cpa-auth-pool/bindings" && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(map[string]any{"binding": map[string]any{"api_key_hash": expectedHash, "pool_id": "test"}})
		case r.URL.Path == "/v1/chat/completions" && r.Method == http.MethodPost:
			chatCalls++
			if got := r.Header.Get("Authorization"); got != "Bearer "+proxyKey {
				t.Fatalf("Authorization = %q, want proxy key", got)
			}
			if got := r.Header.Get("X-CPA-Helper-API-Key-Hash"); got != expectedHash {
				t.Fatalf("helper key hash header = %q, want %q", got, expectedHash)
			}
			var payload struct {
				Model    string `json:"model"`
				Messages []struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				} `json:"messages"`
				Stream bool `json:"stream"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if payload.Model != "gpt-test" {
				t.Fatalf("model = %q, want gpt-test", payload.Model)
			}
			if payload.Stream {
				t.Fatal("stream = true, want false")
			}
			if len(payload.Messages) != 1 || payload.Messages[0].Role != "user" || payload.Messages[0].Content != "ping" {
				t.Fatalf("messages = %#v, want one user ping message", payload.Messages)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{
					{"message": map[string]any{"role": "assistant", "content": "pong"}},
				},
				"usage": map[string]any{"prompt_tokens": 2, "completion_tokens": 1, "total_tokens": 3},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer cpa.Close()

	app, err := backendApp.New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer app.Close()

	handler := app.Routes()
	cookies := requestJSON(t, handler, http.MethodPost, "/api/auth/setup", map[string]any{
		"username": "admin",
		"password": "password123",
		"nickname": "Admin",
	}, nil, nil)
	requestJSON(t, handler, http.MethodPut, "/api/settings", map[string]any{
		"cliaproxy_url":     cpa.URL,
		"model_request_url": cpa.URL,
		"management_key":    "mgmt-secret",
		"collector_enabled": false,
	}, cookies, nil)
	requestJSON(t, handler, http.MethodPut, "/api/auth-pools/proxy-config", map[string]any{
		"api_key": proxyKey,
	}, cookies, nil)

	created := apiKeyCreateResponse{}
	requestJSON(t, handler, http.MethodPost, "/api/api-keys", map[string]any{
		"description": "VSCode",
	}, cookies, &created)
	expectedHash = created.APIKeyHash
	bindTestAPIKeyToAuthPool(t, handler, cookies, created.APIKeyHash)

	var result struct {
		Endpoint   string         `json:"endpoint"`
		Model      string         `json:"model"`
		Reply      string         `json:"reply"`
		StatusCode int            `json:"status_code"`
		DurationMS int64          `json:"duration_ms"`
		Usage      map[string]any `json:"usage"`
	}
	requestJSON(t, handler, http.MethodPost, "/api/account/model-request/test", map[string]any{
		"api_key_hash": created.APIKeyHash,
		"model":        "gpt-test",
		"message":      "ping",
	}, cookies, &result)

	if result.Endpoint != "chat_completions" || result.Model != "gpt-test" || result.Reply != "pong" || result.StatusCode != http.StatusOK {
		t.Fatalf("test response = %#v, want model/reply/status", result)
	}
	if result.DurationMS < 0 {
		t.Fatalf("duration_ms = %d, want non-negative", result.DurationMS)
	}
	if got := result.Usage["total_tokens"]; got != float64(3) {
		t.Fatalf("usage total_tokens = %#v, want 3", got)
	}
	if chatCalls != 1 {
		t.Fatalf("chat calls = %d, want 1", chatCalls)
	}
}

func TestAccountModelRequestTestSupportsResponsesEndpoint(t *testing.T) {
	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())

	const proxyKey = "sk-cpa-proxy"
	var expectedHash string
	responsesCalls := 0
	cpa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v0/management/plugins/cpa-auth-pool/proxy-keys" && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(map[string]any{"proxy_key_count": 1})
		case r.URL.Path == "/v0/management/plugins/cpa-auth-pool/status" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{"pools": []map[string]any{{"id": "test", "name": "Test", "enabled": true, "models": []string{"gpt-response-test"}}}})
		case r.URL.Path == "/v0/management/plugins/cpa-auth-pool/bindings" && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(map[string]any{"binding": map[string]any{"api_key_hash": expectedHash, "pool_id": "test"}})
		case r.URL.Path == "/v1/responses" && r.Method == http.MethodPost:
			responsesCalls++
			if got := r.Header.Get("Authorization"); got != "Bearer "+proxyKey {
				t.Fatalf("Authorization = %q, want proxy key", got)
			}
			if got := r.Header.Get("X-CPA-Helper-API-Key-Hash"); got != expectedHash {
				t.Fatalf("helper key hash header = %q, want %q", got, expectedHash)
			}
			var payload struct {
				Model  string `json:"model"`
				Input  string `json:"input"`
				Stream bool   `json:"stream"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if payload.Model != "gpt-response-test" {
				t.Fatalf("model = %q, want gpt-response-test", payload.Model)
			}
			if payload.Input != "ping responses" {
				t.Fatalf("input = %q, want ping responses", payload.Input)
			}
			if payload.Stream {
				t.Fatal("stream = true, want false")
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"output_text": "responses pong",
				"usage":       map[string]any{"input_tokens": 2, "output_tokens": 1, "total_tokens": 3},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer cpa.Close()

	app, err := backendApp.New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer app.Close()

	handler := app.Routes()
	cookies := requestJSON(t, handler, http.MethodPost, "/api/auth/setup", map[string]any{
		"username": "admin",
		"password": "password123",
		"nickname": "Admin",
	}, nil, nil)
	requestJSON(t, handler, http.MethodPut, "/api/settings", map[string]any{
		"cliaproxy_url":     cpa.URL,
		"model_request_url": cpa.URL,
		"management_key":    "mgmt-secret",
		"collector_enabled": false,
	}, cookies, nil)
	requestJSON(t, handler, http.MethodPut, "/api/auth-pools/proxy-config", map[string]any{
		"api_key": proxyKey,
	}, cookies, nil)

	created := apiKeyCreateResponse{}
	requestJSON(t, handler, http.MethodPost, "/api/api-keys", map[string]any{
		"description": "VSCode",
	}, cookies, &created)
	expectedHash = created.APIKeyHash
	bindTestAPIKeyToAuthPool(t, handler, cookies, created.APIKeyHash)

	var result struct {
		Endpoint   string         `json:"endpoint"`
		Model      string         `json:"model"`
		Reply      string         `json:"reply"`
		StatusCode int            `json:"status_code"`
		Usage      map[string]any `json:"usage"`
	}
	requestJSON(t, handler, http.MethodPost, "/api/account/model-request/test", map[string]any{
		"api_key_hash": created.APIKeyHash,
		"endpoint":     "responses",
		"model":        "gpt-response-test",
		"message":      "ping responses",
	}, cookies, &result)

	if result.Endpoint != "responses" || result.Model != "gpt-response-test" || result.Reply != "responses pong" || result.StatusCode != http.StatusOK {
		t.Fatalf("test response = %#v, want responses endpoint/model/reply/status", result)
	}
	if got := result.Usage["total_tokens"]; got != float64(3) {
		t.Fatalf("usage total_tokens = %#v, want 3", got)
	}
	if responsesCalls != 1 {
		t.Fatalf("responses calls = %d, want 1", responsesCalls)
	}
}

func TestAccountModelRequestTestSupportsClaudeMessagesEndpoint(t *testing.T) {
	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())

	const proxyKey = "sk-cpa-proxy"
	var expectedHash string
	claudeCalls := 0
	cpa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v0/management/plugins/cpa-auth-pool/proxy-keys" && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(map[string]any{"proxy_key_count": 1})
		case r.URL.Path == "/v0/management/plugins/cpa-auth-pool/status" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{"pools": []map[string]any{{"id": "test", "name": "Test", "enabled": true, "models": []string{"claude-test"}}}})
		case r.URL.Path == "/v0/management/plugins/cpa-auth-pool/bindings" && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(map[string]any{"binding": map[string]any{"api_key_hash": expectedHash, "pool_id": "test"}})
		case r.URL.Path == "/v1/messages" && r.Method == http.MethodPost:
			claudeCalls++
			if got := r.Header.Get("x-api-key"); got != proxyKey {
				t.Fatalf("x-api-key = %q, want proxy key", got)
			}
			if got := r.Header.Get("X-CPA-Helper-API-Key-Hash"); got != expectedHash {
				t.Fatalf("helper key hash header = %q, want %q", got, expectedHash)
			}
			if got := r.Header.Get("anthropic-version"); got != "2023-06-01" {
				t.Fatalf("anthropic-version = %q, want 2023-06-01", got)
			}
			var payload struct {
				Model    string `json:"model"`
				MaxToken int    `json:"max_tokens"`
				Messages []struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				} `json:"messages"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if payload.Model != "claude-test" {
				t.Fatalf("model = %q, want claude-test", payload.Model)
			}
			if payload.MaxToken != 1024 {
				t.Fatalf("max_tokens = %d, want 1024", payload.MaxToken)
			}
			if len(payload.Messages) != 1 || payload.Messages[0].Role != "user" || payload.Messages[0].Content != "ping claude" {
				t.Fatalf("messages = %#v, want one user ping claude message", payload.Messages)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"content": []map[string]any{
					{"type": "text", "text": "claude pong"},
				},
				"usage": map[string]any{"input_tokens": 2, "output_tokens": 1},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer cpa.Close()

	app, err := backendApp.New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer app.Close()

	handler := app.Routes()
	cookies := requestJSON(t, handler, http.MethodPost, "/api/auth/setup", map[string]any{
		"username": "admin",
		"password": "password123",
		"nickname": "Admin",
	}, nil, nil)
	requestJSON(t, handler, http.MethodPut, "/api/settings", map[string]any{
		"cliaproxy_url":     cpa.URL,
		"model_request_url": cpa.URL,
		"management_key":    "mgmt-secret",
		"collector_enabled": false,
	}, cookies, nil)
	requestJSON(t, handler, http.MethodPut, "/api/auth-pools/proxy-config", map[string]any{
		"api_key": proxyKey,
	}, cookies, nil)

	created := apiKeyCreateResponse{}
	requestJSON(t, handler, http.MethodPost, "/api/api-keys", map[string]any{
		"description": "VSCode",
	}, cookies, &created)
	expectedHash = created.APIKeyHash
	bindTestAPIKeyToAuthPool(t, handler, cookies, created.APIKeyHash)

	var result struct {
		Endpoint   string         `json:"endpoint"`
		Model      string         `json:"model"`
		Reply      string         `json:"reply"`
		StatusCode int            `json:"status_code"`
		Usage      map[string]any `json:"usage"`
	}
	requestJSON(t, handler, http.MethodPost, "/api/account/model-request/test", map[string]any{
		"api_key_hash": created.APIKeyHash,
		"endpoint":     "claude_messages",
		"model":        "claude-test",
		"message":      "ping claude",
	}, cookies, &result)

	if result.Endpoint != "claude_messages" || result.Model != "claude-test" || result.Reply != "claude pong" || result.StatusCode != http.StatusOK {
		t.Fatalf("test response = %#v, want claude endpoint/model/reply/status", result)
	}
	if got := result.Usage["output_tokens"]; got != float64(1) {
		t.Fatalf("usage output_tokens = %#v, want 1", got)
	}
	if claudeCalls != 1 {
		t.Fatalf("claude calls = %d, want 1", claudeCalls)
	}
}

func TestAccountModelRequestTestRejectsOtherUserAPIKey(t *testing.T) {
	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())

	cpa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v0/management/api-keys" && r.Method == http.MethodPatch {
			_ = json.NewEncoder(w).Encode(map[string]any{"api-keys": []string{"ok"}})
			return
		}
		http.NotFound(w, r)
	}))
	defer cpa.Close()

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
		"cliaproxy_url":     cpa.URL,
		"model_request_url": cpa.URL,
		"management_key":    "test-management-key",
		"collector_enabled": false,
	}, adminCookies, nil)

	created := apiKeyCreateResponse{}
	requestJSON(t, handler, http.MethodPost, "/api/api-keys", map[string]any{
		"description": "Admin",
	}, adminCookies, &created)
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

	requestJSONExpectStatus(t, handler, http.MethodPost, "/api/account/model-request/test", map[string]any{
		"api_key_hash": created.APIKeyHash,
		"model":        "gpt-test",
		"message":      "ping",
	}, memberCookies, http.StatusNotFound)
}

func TestCreateGeneratedAPIKeyDoesNotRequireCPAConfig(t *testing.T) {
	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())

	cpa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v0/management/api-keys" && r.Method == http.MethodPatch {
			_ = json.NewEncoder(w).Encode(map[string]any{"api-keys": []string{"ok"}})
			return
		}
		http.NotFound(w, r)
	}))
	defer cpa.Close()

	app, err := backendApp.New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer app.Close()

	handler := app.Routes()
	cookies := requestJSON(t, handler, http.MethodPost, "/api/auth/setup", map[string]any{
		"username": "admin",
		"password": "password123",
		"nickname": "Admin",
	}, nil, nil)
	requestJSON(t, handler, http.MethodPut, "/api/settings", map[string]any{
		"cliaproxy_url":     cpa.URL,
		"management_key":    "mgmt-secret",
		"collector_enabled": false,
	}, cookies, nil)

	created := apiKeyCreateResponse{}
	requestJSON(t, handler, http.MethodPost, "/api/api-keys", map[string]any{
		"description": "VSCode",
	}, cookies, &created)
	if created.APIKey == "" || created.APIKeyHash == "" {
		t.Fatalf("created API key response is missing key fields: %#v", created)
	}
}

func TestCreateGeneratedAPIKeyDoesNotSyncToCPAInAuthPoolProxyMode(t *testing.T) {
	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())

	remoteCalls := 0
	cpa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v0/management/plugins/cpa-auth-pool/proxy-keys" && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(map[string]any{"proxy_key_count": 1})
		case r.URL.Path == "/v0/management/plugins/cpa-auth-pool/status" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{"pools": []map[string]any{}})
		case r.URL.Path == "/v0/management/api-keys":
			remoteCalls++
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	defer cpa.Close()

	app, err := backendApp.New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer app.Close()

	handler := app.Routes()
	cookies := requestJSON(t, handler, http.MethodPost, "/api/auth/setup", map[string]any{
		"username": "admin",
		"password": "admin-pass",
		"nickname": "Admin",
	}, nil, nil)
	requestJSON(t, handler, http.MethodPut, "/api/auth-pools/proxy-config", map[string]any{
		"targets": []map[string]any{{
			"id":             "main",
			"name":           "Main CPA",
			"cpa_url":        cpa.URL,
			"management_key": "mgmt-secret",
			"api_key":        "sk-cpa-proxy",
			"enabled":        true,
		}},
	}, cookies, nil)

	created := apiKeyCreateResponse{}
	requestJSON(t, handler, http.MethodPost, "/api/api-keys", map[string]any{
		"description": "VSCode",
	}, cookies, &created)

	if created.APIKey == "" || created.APIKeyHash == "" {
		t.Fatalf("created API key response is missing key fields: %#v", created)
	}
	if remoteCalls != 0 {
		t.Fatalf("CPA api-key sync calls = %d, want 0", remoteCalls)
	}
}

func TestModelProxyRequiresAuthPoolBinding(t *testing.T) {
	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())

	cpaCalls := 0
	cpa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v0/management/plugins/cpa-auth-pool/proxy-keys" && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(map[string]any{"proxy_key_count": 1})
		case r.URL.Path == "/v1/models" || r.URL.Path == "/v1/chat/completions":
			cpaCalls++
			_ = json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": []map[string]any{{"id": "gpt-test"}}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer cpa.Close()

	app, err := backendApp.New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer app.Close()

	handler := app.Routes()
	cookies := requestJSON(t, handler, http.MethodPost, "/api/auth/setup", map[string]any{
		"username": "admin",
		"password": "pass1234",
		"nickname": "Admin",
	}, nil, nil)
	requestJSON(t, handler, http.MethodPut, "/api/settings", map[string]any{
		"cliaproxy_url":     cpa.URL,
		"management_key":    "mgmt-secret",
		"collector_enabled": false,
	}, cookies, nil)
	requestJSON(t, handler, http.MethodPut, "/api/auth-pools/proxy-config", map[string]any{
		"api_key": "sk-cpa-proxy",
	}, cookies, nil)

	created := apiKeyCreateResponse{}
	requestJSON(t, handler, http.MethodPost, "/api/api-keys", map[string]any{"description": "Proxy key"}, cookies, &created)

	requestJSONExpectStatus(t, handler, http.MethodPost, "/api/account/model-request/test", map[string]any{
		"api_key_hash": created.APIKeyHash,
		"model":        "gpt-test",
		"message":      "ping",
	}, cookies, http.StatusUnprocessableEntity)

	request := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	request.Header.Set("Authorization", "Bearer "+created.APIKey)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("GET /v1/models returned %d: %s", recorder.Code, recorder.Body.String())
	}
	if cpaCalls != 0 {
		t.Fatalf("CPA calls = %d, want 0 before auth pool binding", cpaCalls)
	}
}

func TestAvailableModelsUsesProxyForwardingKey(t *testing.T) {
	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())

	const proxyKey = "sk-cpa-proxy"
	var created apiKeyCreateResponse
	cpa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v0/management/plugins/cpa-auth-pool/proxy-keys" && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(map[string]any{"proxy_key_count": 1})
		case r.URL.Path == "/v0/management/plugins/cpa-auth-pool/status" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"pools": []map[string]any{{"id": "free", "name": "Free", "enabled": true, "models": []string{"gpt-free"}}},
			})
		case r.URL.Path == "/v0/management/plugins/cpa-auth-pool/bindings" && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(map[string]any{"binding": map[string]any{"api_key_hash": created.APIKeyHash, "pool_id": "free"}})
		case r.URL.Path == "/v1/models" && r.Method == http.MethodGet:
			if got := r.Header.Get("Authorization"); got != "Bearer "+proxyKey {
				t.Fatalf("Authorization = %q, want proxy key", got)
			}
			if got := r.Header.Get("X-CPA-Helper-API-Key-Hash"); got != "" {
				t.Fatalf("helper key hash header = %q, want empty for plugin model catalog", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": []map[string]any{
				{"id": "gpt-free", "object": "model", "safe_label": "visible", "api_key": proxyKey, "token": "Bearer " + proxyKey},
				{"id": "gpt-other", "object": "model", "credential": proxyKey},
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer cpa.Close()

	app, err := backendApp.New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer app.Close()

	handler := app.Routes()
	cookies := requestJSON(t, handler, http.MethodPost, "/api/auth/setup", map[string]any{
		"username": "admin",
		"password": "pass1234",
		"nickname": "Admin",
	}, nil, nil)
	requestJSON(t, handler, http.MethodPut, "/api/settings", map[string]any{
		"cliaproxy_url":     cpa.URL,
		"management_key":    "mgmt-secret",
		"collector_enabled": false,
	}, cookies, nil)
	requestJSON(t, handler, http.MethodPut, "/api/auth-pools/proxy-config", map[string]any{
		"api_key": proxyKey,
	}, cookies, nil)
	requestJSON(t, handler, http.MethodPost, "/api/api-keys", map[string]any{"description": "Proxy key"}, cookies, &created)
	requestJSON(t, handler, http.MethodPost, "/api/auth-pools/bindings", map[string]any{
		"api_key_hash": created.APIKeyHash,
		"pool_id":      "free",
	}, cookies, nil)

	var response struct {
		HasAPIKeys           bool `json:"has_api_keys"`
		APIKeyCount          int  `json:"api_key_count"`
		QueryableAPIKeyCount int  `json:"queryable_api_key_count"`
		Models               []struct {
			ID       string         `json:"id"`
			Metadata map[string]any `json:"metadata"`
			Sources  []struct {
				APIKeyHash    string `json:"api_key_hash"`
				APIKeyPreview string `json:"api_key_preview"`
				Description   string `json:"description"`
			} `json:"sources"`
		} `json:"models"`
	}
	requestJSON(t, handler, http.MethodGet, "/api/account/models", nil, cookies, &response)
	if !response.HasAPIKeys || response.APIKeyCount != 1 || response.QueryableAPIKeyCount != 1 {
		t.Fatalf("key counts = has %v count %d queryable %d, want plugin key available", response.HasAPIKeys, response.APIKeyCount, response.QueryableAPIKeyCount)
	}
	if len(response.Models) != 2 || response.Models[0].ID != "gpt-free" || response.Models[1].ID != "gpt-other" {
		t.Fatalf("models = %#v, want all CPA models from plugin key", response.Models)
	}
	for _, model := range response.Models {
		if len(model.Sources) != 1 {
			t.Fatalf("model %s sources = %#v, want one sanitized plugin source", model.ID, model.Sources)
		}
		source := model.Sources[0]
		if source.APIKeyHash == "" || source.APIKeyHash == created.APIKeyHash || source.APIKeyPreview == "" || source.APIKeyPreview == proxyKey || source.Description == "" {
			t.Fatalf("model %s source = %#v, want sanitized plugin source", model.ID, source)
		}
		if _, ok := model.Metadata["api_key"]; ok {
			t.Fatalf("model %s metadata leaked api_key: %#v", model.ID, model.Metadata)
		}
		if _, ok := model.Metadata["token"]; ok {
			t.Fatalf("model %s metadata leaked token: %#v", model.ID, model.Metadata)
		}
		if _, ok := model.Metadata["credential"]; ok {
			t.Fatalf("model %s metadata leaked credential: %#v", model.ID, model.Metadata)
		}
	}
	body, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("Marshal response failed: %v", err)
	}
	bodyText := string(body)
	for _, secret := range []string{proxyKey, created.APIKey, created.APIKeyHash} {
		if secret != "" && strings.Contains(bodyText, secret) {
			t.Fatalf("available models response leaked secret material %q in %s", secret, bodyText)
		}
	}
}

func TestModelProxyModelsFiltersToBoundPool(t *testing.T) {
	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())

	var created apiKeyCreateResponse
	cpa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v0/management/plugins/cpa-auth-pool/proxy-keys" && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(map[string]any{"proxy_key_count": 1})
		case r.URL.Path == "/v0/management/plugins/cpa-auth-pool/status" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"pools": []map[string]any{{"id": "free", "name": "Free", "enabled": true, "models": []string{"gpt-free"}}},
			})
		case r.URL.Path == "/v0/management/plugins/cpa-auth-pool/bindings" && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(map[string]any{"binding": map[string]any{"api_key_hash": created.APIKeyHash, "pool_id": "free"}})
		case r.URL.Path == "/v1/models" && r.Method == http.MethodGet:
			if got := r.Header.Get("Authorization"); got != "Bearer sk-cpa-proxy" {
				t.Fatalf("Authorization = %q, want proxy key forwarded", got)
			}
			if got := r.Header.Get("X-CPA-Helper-API-Key-Hash"); got != created.APIKeyHash {
				t.Fatalf("helper key hash header = %q, want %q", got, created.APIKeyHash)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": []map[string]any{
				{"id": "gpt-free", "object": "model"},
				{"id": "gpt-other", "object": "model"},
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer cpa.Close()

	app, err := backendApp.New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer app.Close()

	handler := app.Routes()
	cookies := requestJSON(t, handler, http.MethodPost, "/api/auth/setup", map[string]any{
		"username": "admin",
		"password": "password123",
		"nickname": "Admin",
	}, nil, nil)
	requestJSON(t, handler, http.MethodPut, "/api/settings", map[string]any{
		"cliaproxy_url":     cpa.URL,
		"management_key":    "mgmt-secret",
		"collector_enabled": false,
	}, cookies, nil)
	requestJSON(t, handler, http.MethodPut, "/api/auth-pools/proxy-config", map[string]any{
		"api_key": "sk-cpa-proxy",
	}, cookies, nil)
	requestJSON(t, handler, http.MethodPost, "/api/api-keys", map[string]any{"description": "Proxy key"}, cookies, &created)
	requestJSON(t, handler, http.MethodPost, "/api/auth-pools/bindings", map[string]any{
		"api_key_hash": created.APIKeyHash,
		"pool_id":      "free",
	}, cookies, nil)

	request := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	request.Header.Set("Authorization", "Bearer "+created.APIKey)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET /v1/models returned %d: %s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&payload); err != nil {
		t.Fatalf("decode models: %v", err)
	}
	if len(payload.Data) != 1 || payload.Data[0]["id"] != "gpt-free" {
		t.Fatalf("models = %#v, want only gpt-free", payload.Data)
	}
}
