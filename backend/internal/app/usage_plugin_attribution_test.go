package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestMatchPluginUsageAttributionsPropagatesFailoverRequestOwner(t *testing.T) {
	startedAt := time.Date(2026, 7, 21, 11, 31, 33, 250000000, appTimeLocation)
	records := []pluginUsageOwnerRepair{
		{ID: 1, Timestamp: startedAt, RequestID: "a2b4ccd4", Provider: "openai-compatible-https://www.aiwanwu.cc/", Model: "gpt-5.6-sol", AuthIndex: "sk-749...c9bb"},
		{ID: 2, Timestamp: startedAt.Add(50 * time.Millisecond), RequestID: "a2b4ccd4", Provider: "codex", Model: "gpt-5.6-sol medium", AuthIndex: "YolandaDavis47306@outlook.com"},
	}
	events := []authPoolPluginEvent{{
		ID: 18, Timestamp: startedAt.Format(time.RFC3339Nano), Phase: "selection", Status: "selected",
		Provider: "codex", Model: "gpt-5.6-sol", SelectedAuthID: "root_cli_proxy_api_yolandadavis47306_outlook_com_json", APIKeyHash: "owner-hash",
	}}

	matched := matchPluginUsageAttributions(records, events)
	if matched[1] != "owner-hash" || matched[2] != "owner-hash" {
		t.Fatalf("matched = %#v, want both failover rows attributed", matched)
	}
}

func TestMatchPluginUsageAttributionsLeavesConcurrentOwnersAmbiguous(t *testing.T) {
	startedAt := time.Date(2026, 7, 21, 11, 31, 33, 0, appTimeLocation)
	records := []pluginUsageOwnerRepair{
		{ID: 1, Timestamp: startedAt, RequestID: "request-a", Provider: "codex", Model: "gpt-5.6-sol", AuthIndex: "shared-auth"},
		{ID: 2, Timestamp: startedAt, RequestID: "request-b", Provider: "codex", Model: "gpt-5.6-sol", AuthIndex: "shared-auth"},
	}
	events := []authPoolPluginEvent{
		{ID: 1, Timestamp: startedAt.Add(100 * time.Millisecond).Format(time.RFC3339Nano), Phase: "selection", Status: "selected", Provider: "codex", Model: "gpt-5.6-sol", SelectedAuthID: "shared-auth", APIKeyHash: "alice-hash"},
		{ID: 2, Timestamp: startedAt.Add(300 * time.Millisecond).Format(time.RFC3339Nano), Phase: "selection", Status: "selected", Provider: "codex", Model: "gpt-5.6-sol", SelectedAuthID: "shared-auth", APIKeyHash: "bob-hash"},
	}

	if matched := matchPluginUsageAttributions(records, events); len(matched) != 0 {
		t.Fatalf("ambiguous concurrent requests were attributed: %#v", matched)
	}
}

func TestMatchPluginUsageAttributionsSeparatesNonConcurrentOwners(t *testing.T) {
	startedAt := time.Date(2026, 7, 21, 11, 31, 33, 0, appTimeLocation)
	records := []pluginUsageOwnerRepair{
		{ID: 1, Timestamp: startedAt, RequestID: "request-a", Provider: "codex", Model: "gpt-5.6-sol", AuthIndex: "shared-auth"},
		{ID: 2, Timestamp: startedAt.Add(5 * time.Second), RequestID: "request-b", Provider: "codex", Model: "gpt-5.6-sol", AuthIndex: "shared-auth"},
	}
	events := []authPoolPluginEvent{
		{ID: 1, Timestamp: startedAt.Add(100 * time.Millisecond).Format(time.RFC3339Nano), Phase: "selection", Status: "selected", Provider: "codex", Model: "gpt-5.6-sol", SelectedAuthID: "shared-auth", APIKeyHash: "alice-hash"},
		{ID: 2, Timestamp: startedAt.Add(5100 * time.Millisecond).Format(time.RFC3339Nano), Phase: "selection", Status: "selected", Provider: "codex", Model: "gpt-5.6-sol", SelectedAuthID: "shared-auth", APIKeyHash: "bob-hash"},
	}

	matched := matchPluginUsageAttributions(records, events)
	if matched[1] != "alice-hash" || matched[2] != "bob-hash" {
		t.Fatalf("matched = %#v, want separated owners", matched)
	}
}

func TestMatchPluginUsageAttributionsConsumesSelectionAndCompletionOnce(t *testing.T) {
	startedAt := time.Date(2026, 7, 21, 11, 31, 33, 0, appTimeLocation)
	records := []pluginUsageOwnerRepair{
		{ID: 1, Timestamp: startedAt, RequestID: "request-a", Provider: "codex", Model: "gpt-5.6-sol", AuthIndex: "shared-auth", LatencyMS: float64Ptr(2000)},
		{ID: 2, Timestamp: startedAt.Add(2 * time.Second), RequestID: "request-b", Provider: "codex", Model: "gpt-5.6-sol", AuthIndex: "shared-auth"},
	}
	events := []authPoolPluginEvent{
		{ID: 1, AttributionID: 9, Timestamp: startedAt.Format(time.RFC3339Nano), Phase: "selection", Status: "selected", Provider: "codex", Model: "gpt-5.6-sol", SelectedAuthID: "shared-auth", APIKeyHash: "alice-hash"},
		{ID: 2, AttributionID: 9, Timestamp: startedAt.Add(2 * time.Second).Format(time.RFC3339Nano), Phase: "completion", Status: "success", Provider: "codex", Model: "gpt-5.6-sol", SelectedAuthID: "shared-auth", APIKeyHash: "alice-hash"},
	}

	matched := matchPluginUsageAttributions(records, events)
	if matched[1] != "alice-hash" || matched[2] != "" {
		t.Fatalf("selection/completion pair was reused: %#v", matched)
	}
}

func TestRepairRecentUsageOwnerSnapshotsUsesPluginEvents(t *testing.T) {
	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())
	startedAt := time.Now().In(appTimeLocation).Add(-5 * time.Second)
	const apiKey = "helper-plugin-attribution-key"
	apiKeyHash := hashAPIKey(apiKey)
	cpa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v0/management/plugins/cpa-auth-pool/events" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []map[string]any{{
				"id": 12, "timestamp": startedAt.Format(time.RFC3339Nano), "phase": "selection", "status": "selected",
				"provider": "codex", "model": "gpt-5.6-sol", "selected_auth_id": "auth-owner", "api_key_hash": apiKeyHash,
			}},
			"total": 1, "capacity": 500,
		})
	}))
	defer cpa.Close()

	app, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer app.Close()
	userID := seedQuotaTestUser(t, app, "plugin-owner")
	seedQuotaTestAPIKey(t, app, userID, apiKey)
	configureKeeperTestCPA(t, app, cpa.URL, nil)

	successRaw, _ := json.Marshal(map[string]any{
		"api_key": "cpa-forwarding-key", "provider": "codex", "model": "gpt-5.6-sol medium", "endpoint": "POST /v1/chat/completions",
		"request_id": "a2b4ccd4", "timestamp": startedAt.Format(time.RFC3339Nano), "auth_index": "auth-owner", "input_tokens": 10, "output_tokens": 2,
	})
	failureRaw, _ := json.Marshal(map[string]any{
		"api_key": "cpa-forwarding-key", "provider": "openai-compatible-https://www.aiwanwu.cc/", "model": "gpt-5.6-sol", "endpoint": "POST /v1/chat/completions",
		"request_id": "a2b4ccd4", "timestamp": startedAt.Add(50 * time.Millisecond).Format(time.RFC3339Nano), "auth_index": "sk-749...c9bb", "failed": true,
	})
	success, _, err := app.saveUsageMessage(t.Context(), successRaw)
	if err != nil {
		t.Fatal(err)
	}
	failure, _, err := app.saveUsageMessage(t.Context(), failureRaw)
	if err != nil {
		t.Fatal(err)
	}
	if success.UsageUsername != nil || failure.UsageUsername != nil {
		t.Fatalf("records unexpectedly attributed before repair: success=%#v failure=%#v", success.UsageUsername, failure.UsageUsername)
	}

	if err := app.repairRecentUsageOwnerSnapshots(t.Context()); err != nil {
		t.Fatalf("repairRecentUsageOwnerSnapshots: %v", err)
	}
	for _, id := range []int{success.ID, failure.ID} {
		var username, description string
		if err := app.db.QueryRow(`SELECT usage_username, api_key_description FROM usage_records WHERE id = ?`, id).Scan(&username, &description); err != nil {
			t.Fatal(err)
		}
		if username != "plugin-owner" || description != "VSCode" {
			t.Fatalf("record %d owner = %q/%q, want plugin-owner/VSCode", id, username, description)
		}
	}
}

func float64Ptr(value float64) *float64 {
	return &value
}
