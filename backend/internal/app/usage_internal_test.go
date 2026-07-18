package app

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSaveUsageMessageStoresReasoningEffortAndTTFT(t *testing.T) {
	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())
	app, err := New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer app.Close()

	raw := `{"api_key":"usage-ttft-key","provider":"openai","model":"gpt-5.5","request_id":"usage-ttft","reasoning_effort":"xhigh","ttft_ms":710,"input_tokens":10,"output_tokens":2}`
	record, created, err := app.saveUsageMessage(context.Background(), []byte(raw))
	if err != nil || !created {
		t.Fatalf("saveUsageMessage created=%v err=%v", created, err)
	}
	if record.ReasoningEffort == nil || *record.ReasoningEffort != "xhigh" {
		t.Fatalf("record reasoning_effort = %#v, want xhigh", record.ReasoningEffort)
	}
	if record.TTFTMS == nil || *record.TTFTMS != 710 {
		t.Fatalf("record ttft_ms = %#v, want 710", record.TTFTMS)
	}

	var reasoningEffort sql.NullString
	var ttftMS sql.NullFloat64
	if err := app.db.QueryRow(`SELECT reasoning_effort, ttft_ms FROM usage_records WHERE id = ?`, record.ID).Scan(&reasoningEffort, &ttftMS); err != nil {
		t.Fatal(err)
	}
	if !reasoningEffort.Valid || reasoningEffort.String != "xhigh" || !ttftMS.Valid || ttftMS.Float64 != 710 {
		t.Fatalf("stored reasoning/ttft = %#v/%#v, want xhigh/710", reasoningEffort, ttftMS)
	}
}

func TestSaveUsageMessageRedactsSecretsBeforePersistence(t *testing.T) {
	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())
	app, err := New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer app.Close()

	const secret = "sk-super-secret-value"
	raw := `{"api_key":"` + secret + `","auth_type":"apikey","source":"` + secret + `","response_headers":{"Set-Cookie":"session=secret-cookie","Authorization":"Bearer secret-token"},"request_id":"usage-redaction"}`
	record, created, err := app.saveUsageMessage(context.Background(), []byte(raw))
	if err != nil || !created {
		t.Fatalf("saveUsageMessage created=%v err=%v", created, err)
	}
	for _, leaked := range []string{secret, "secret-cookie", "secret-token"} {
		if strings.Contains(record.RawJSON, leaked) {
			t.Fatalf("stored raw_json leaked %q: %s", leaked, record.RawJSON)
		}
	}
	if record.Source == nil || *record.Source == secret || !strings.Contains(*record.Source, "...") {
		t.Fatalf("stored source = %v, want masked API key source", record.Source)
	}
}

func TestSaveUsageMessageIgnoresZeroTTFT(t *testing.T) {
	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())
	app, err := New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer app.Close()

	raw := `{"api_key":"usage-zero-ttft-key","provider":"openai","model":"gpt-5.5","request_id":"usage-ttft-zero","ttft_ms":0,"input_tokens":10,"output_tokens":2}`
	record, created, err := app.saveUsageMessage(context.Background(), []byte(raw))
	if err != nil || !created {
		t.Fatalf("saveUsageMessage created=%v err=%v", created, err)
	}
	if record.TTFTMS != nil {
		t.Fatalf("record ttft_ms = %#v, want nil", record.TTFTMS)
	}

	var ttftMS sql.NullFloat64
	if err := app.db.QueryRow(`SELECT ttft_ms FROM usage_records WHERE id = ?`, record.ID).Scan(&ttftMS); err != nil {
		t.Fatal(err)
	}
	if ttftMS.Valid {
		t.Fatalf("stored ttft_ms = %v, want NULL", ttftMS.Float64)
	}
}

func TestSaveUsageMessageUsesModelProxyRequestAttribution(t *testing.T) {
	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())
	app, err := New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer app.Close()

	ctx := context.Background()
	const apiKey = "helper-owned-key-direct-attribution"
	userID := seedQuotaTestUser(t, app, "member")
	seedQuotaTestAPIKey(t, app, userID, apiKey)
	if err := app.recordModelProxyRequestAttributions(ctx, hashAPIKey(apiKey), "req-proxy-attribution"); err != nil {
		t.Fatal(err)
	}

	raw := `{"api_key":"forwarding-key-not-owned-by-helper","provider":"codex","model":"gpt-5.5","request_id":"req-proxy-attribution","input_tokens":10,"output_tokens":2}`
	record, created, err := app.saveUsageMessage(ctx, []byte(raw))
	if err != nil || !created {
		t.Fatalf("saveUsageMessage created=%v err=%v", created, err)
	}
	if record.UsageUsername == nil || *record.UsageUsername != "member" {
		t.Fatalf("usage_username = %#v, want member", record.UsageUsername)
	}
	if record.APIKeyDescription == nil || *record.APIKeyDescription != "VSCode" {
		t.Fatalf("api_key_description = %#v, want VSCode", record.APIKeyDescription)
	}
}

func TestCurrentModelRequestTestRecordsUsageAttribution(t *testing.T) {
	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())

	const responseRequestID = "req-model-test-attribution"
	var helperRequestID string
	cpa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		helperRequestID = r.Header.Get("X-CPA-Helper-Request-Id")
		if helperRequestID == "" || r.Header.Get("X-Request-Id") != helperRequestID {
			t.Fatalf("request IDs = helper %q request %q, want matching non-empty IDs", helperRequestID, r.Header.Get("X-Request-Id"))
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-CPA-Request-Id", responseRequestID)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"pong"}}],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`))
	}))
	defer cpa.Close()

	app, err := New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer app.Close()

	ctx := context.Background()
	const apiKey = "helper-owned-model-test-key"
	const forwardingKey = "cpa-forwarding-model-test-key"
	userID := seedQuotaTestUser(t, app, "member")
	seedQuotaTestAPIKey(t, app, userID, apiKey)
	apiKeyHash := hashAPIKey(apiKey)
	if err := app.saveLocalAuthPool(ctx, authPool{ID: "test", Name: "Test", Models: []string{"gpt-test"}, Visibility: authPoolVisibilityAllUsers, Enabled: true}); err != nil {
		t.Fatalf("save auth pool: %v", err)
	}
	now := dbTime(time.Now())
	if _, err := app.db.Exec(`INSERT INTO user_api_key_pools (api_key_hash, pool_id, created_at, updated_at) VALUES (?, 'test', ?, ?)`, apiKeyHash, now, now); err != nil {
		t.Fatalf("bind API key: %v", err)
	}
	configureKeeperTestCPA(t, app, cpa.URL, func(cfg *AppConfig) {
		cfg.AuthPoolProxyTargets = []AuthPoolProxyTargetConfig{{
			ID: "test", Name: "Test", CPAURL: cpa.URL, APIKey: forwardingKey, Enabled: true,
		}}
	})

	result, err := app.testCurrentUserModelRequest(ctx, &AuthUser{ID: userID, Username: "member"}, modelRequestTestPayload{
		APIKeyHash: apiKeyHash,
		Model:      "gpt-test",
		Message:    "ping",
	})
	if err != nil {
		t.Fatalf("testCurrentUserModelRequest: %v", err)
	}
	if result.Reply != "pong" || result.StatusCode != http.StatusOK || helperRequestID == "" {
		t.Fatalf("model test result = %#v, helper request ID %q", result, helperRequestID)
	}

	var attributedHash, model, endpoint string
	var statusCode int
	if err := app.db.QueryRow(`
		SELECT api_key_hash, model, endpoint, status_code
		FROM model_proxy_request_attributions
		WHERE request_id = ?
	`, responseRequestID).Scan(&attributedHash, &model, &endpoint, &statusCode); err != nil {
		t.Fatalf("query response attribution: %v", err)
	}
	if attributedHash != apiKeyHash || model != "gpt-test" || endpoint != "POST /v1/chat/completions" || statusCode != http.StatusOK {
		t.Fatalf("attribution = hash %q model %q endpoint %q status %d", attributedHash, model, endpoint, statusCode)
	}

	raw := `{"api_key":"` + forwardingKey + `","provider":"codex","model":"gpt-test medium","endpoint":"POST /v1/chat/completions","request_id":"` + responseRequestID + `","input_tokens":10,"output_tokens":2}`
	record, created, err := app.saveUsageMessage(ctx, []byte(raw))
	if err != nil || !created {
		t.Fatalf("save attributed usage created=%v err=%v", created, err)
	}
	if record.UsageUsername == nil || *record.UsageUsername != "member" {
		t.Fatalf("usage_username = %#v, want member", record.UsageUsername)
	}
	if record.APIKeyDescription == nil || *record.APIKeyDescription != "VSCode" {
		t.Fatalf("api_key_description = %#v, want VSCode", record.APIKeyDescription)
	}
}

func TestSaveUsageMessageFallsBackToModelProxyAttributionMetadata(t *testing.T) {
	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())
	app, err := New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer app.Close()

	ctx := context.Background()
	const apiKey = "helper-owned-key-metadata-attribution"
	userID := seedQuotaTestUser(t, app, "alice")
	seedQuotaTestAPIKey(t, app, userID, apiKey)
	startedAt := time.Date(2026, 7, 17, 10, 3, 8, 0, appTimeLocation)
	completedAt := startedAt.Add(74 * time.Second)
	statusCode := http.StatusOK
	if err := app.recordModelProxyRequestAttributionsWithMetadata(ctx, hashAPIKey(apiKey), modelProxyRequestAttributionMetadata{
		Model:       "gpt-5.5",
		Endpoint:    "POST /v1/chat/completions",
		StartedAt:   startedAt,
		CompletedAt: &completedAt,
		StatusCode:  &statusCode,
	}, "cpa-helper-local-request"); err != nil {
		t.Fatal(err)
	}

	raw := `{"api_key":"forwarding-key-not-owned-by-helper","provider":"codex","model":"gpt-5.5 xhigh","endpoint":"POST /v1/chat/completions","request_id":"b69b2bbf","timestamp":"2026-07-17T10:03:08+08:00","input_tokens":10,"output_tokens":2}`
	record, created, err := app.saveUsageMessage(ctx, []byte(raw))
	if err != nil || !created {
		t.Fatalf("saveUsageMessage created=%v err=%v", created, err)
	}
	if record.UsageUsername == nil || *record.UsageUsername != "alice" {
		t.Fatalf("usage_username = %#v, want alice", record.UsageUsername)
	}
	if record.APIKeyDescription == nil || *record.APIKeyDescription != "VSCode" {
		t.Fatalf("api_key_description = %#v, want VSCode", record.APIKeyDescription)
	}
}
