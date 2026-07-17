package app_test

import (
	"net/http"
	"strings"
	"testing"

	backendApp "cpa-helper/backend/internal/app"
)

func TestSettingsResponseDoesNotExposeManagementKey(t *testing.T) {
	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())

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

	const secret = "mgmt-secret-real-value"
	var saved struct {
		ManagementKey        string `json:"management_key"`
		ManagementKeySet     bool   `json:"management_key_set"`
		ManagementKeyPreview string `json:"management_key_preview"`
	}
	requestJSON(t, handler, http.MethodPut, "/api/settings", map[string]any{
		"cliaproxy_url":     "http://127.0.0.1:8317",
		"model_request_url": "http://127.0.0.1:8317",
		"management_key":    secret,
		"collector_enabled": false,
	}, cookies, &saved)
	if saved.ManagementKey != "" {
		t.Fatalf("PUT /api/settings exposed management_key = %q", saved.ManagementKey)
	}
	if !saved.ManagementKeySet || saved.ManagementKeyPreview == "" {
		t.Fatalf("PUT /api/settings key flags = set %v preview %q, want saved preview only", saved.ManagementKeySet, saved.ManagementKeyPreview)
	}
	if strings.Contains(saved.ManagementKeyPreview, secret) {
		t.Fatalf("PUT /api/settings preview leaked full management key %q", saved.ManagementKeyPreview)
	}

	var loaded struct {
		ManagementKey        string `json:"management_key"`
		ManagementKeySet     bool   `json:"management_key_set"`
		ManagementKeyPreview string `json:"management_key_preview"`
	}
	requestJSON(t, handler, http.MethodGet, "/api/settings", nil, cookies, &loaded)
	if loaded.ManagementKey != "" {
		t.Fatalf("GET /api/settings exposed management_key = %q", loaded.ManagementKey)
	}
	if !loaded.ManagementKeySet || loaded.ManagementKeyPreview == "" {
		t.Fatalf("GET /api/settings key flags = set %v preview %q, want saved preview only", loaded.ManagementKeySet, loaded.ManagementKeyPreview)
	}
	if strings.Contains(loaded.ManagementKeyPreview, secret) {
		t.Fatalf("GET /api/settings preview leaked full management key %q", loaded.ManagementKeyPreview)
	}

	requestJSON(t, handler, http.MethodPut, "/api/settings", map[string]any{
		"cliaproxy_url":     "http://127.0.0.1:8317",
		"model_request_url": "http://127.0.0.1:8317",
		"collector_enabled": false,
	}, cookies, &loaded)
	if !loaded.ManagementKeySet || loaded.ManagementKeyPreview == "" {
		t.Fatalf("PUT /api/settings without key cleared saved key: set %v preview %q", loaded.ManagementKeySet, loaded.ManagementKeyPreview)
	}
}
