package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuthPoolPluginEventsAddsTargetContext(t *testing.T) {
	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())
	cpa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet && r.URL.Path == "/v0/management/plugins/cpa-auth-pool/events" {
			if got := r.URL.Query().Get("limit"); got != "25" {
				t.Fatalf("limit = %q, want 25", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"items": []map[string]any{{
					"id": 8, "timestamp": "2026-07-17T21:00:00+08:00", "phase": "completion", "status": "failed", "pool_id": "002", "selected_auth_id": "auth-a",
					"error_code": "usage_limit_reached", "error_message": "The usage limit has been reached", "error_detail": `{"error":{"type":"usage_limit_reached"}}`,
					"plan_type": "free", "resets_at": 1787123950, "resets_in_seconds": 2588673,
				}},
				"total": 8, "capacity": 500,
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer cpa.Close()

	app, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer app.Close()
	configureKeeperTestCPA(t, app, cpa.URL, nil)

	response, err := app.authPoolPluginEvents(t.Context(), 25)
	if err != nil {
		t.Fatalf("authPoolPluginEvents: %v", err)
	}
	if response.Total != 8 || response.Capacity != 500 || len(response.Items) != 1 {
		t.Fatalf("response = %#v", response)
	}
	if response.Items[0].TargetID != "default" || response.Items[0].TargetName != "Default CPA" || response.Items[0].SelectedAuthID != "auth-a" {
		t.Fatalf("event = %#v, want default target auth-a", response.Items[0])
	}
	event := response.Items[0]
	if event.ErrorCode != "usage_limit_reached" || event.PlanType != "free" || event.ResetsAt != 1787123950 || event.ResetsInSeconds != 2588673 || event.ErrorDetail == "" {
		t.Fatalf("structured failure event = %#v", event)
	}
}

func TestClearAuthPoolPluginEventsAggregatesTargets(t *testing.T) {
	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())
	cpa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodDelete && r.URL.Path == "/v0/management/plugins/cpa-auth-pool/events" {
			_ = json.NewEncoder(w).Encode(map[string]any{"cleared": 12})
			return
		}
		http.NotFound(w, r)
	}))
	defer cpa.Close()

	app, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer app.Close()
	configureKeeperTestCPA(t, app, cpa.URL, nil)

	response, err := app.clearAuthPoolPluginEvents(t.Context())
	if err != nil {
		t.Fatalf("clearAuthPoolPluginEvents: %v", err)
	}
	if response.Cleared != 12 || len(response.Errors) != 0 {
		t.Fatalf("response = %#v", response)
	}
}

func TestAuthPoolPluginEventsKeepsHealthyTargetsWhenOneFails(t *testing.T) {
	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())
	healthy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []map[string]any{{
				"id": 1, "timestamp": "2026-07-17T13:00:00Z", "phase": "selection", "status": "blocked", "reason": "no_eligible_candidates",
			}},
			"total": 1, "capacity": 500,
		})
	}))
	defer healthy.Close()

	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "plugin unavailable", http.StatusServiceUnavailable)
	}))
	defer failing.Close()

	app, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer app.Close()
	configureKeeperTestCPA(t, app, healthy.URL, func(cfg *AppConfig) {
		cfg.AuthPoolProxyTargets = []AuthPoolProxyTargetConfig{
			{ID: "healthy", Name: "Healthy CPA", CPAURL: healthy.URL, ManagementKey: "mgmt", Enabled: true},
			{ID: "failing", Name: "Failing CPA", CPAURL: failing.URL, ManagementKey: "mgmt", Enabled: true},
		}
	})

	response, err := app.authPoolPluginEvents(t.Context(), 25)
	if err != nil {
		t.Fatalf("authPoolPluginEvents: %v", err)
	}
	if len(response.Items) != 1 || response.Items[0].TargetID != "healthy" {
		t.Fatalf("items = %#v, want healthy target event", response.Items)
	}
	if len(response.Errors) != 1 || response.Errors[0].TargetID != "failing" {
		t.Fatalf("errors = %#v, want failing target error", response.Errors)
	}
}
