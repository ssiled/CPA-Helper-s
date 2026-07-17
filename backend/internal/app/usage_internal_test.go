package app

import (
	"context"
	"database/sql"
	"net/http"
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
