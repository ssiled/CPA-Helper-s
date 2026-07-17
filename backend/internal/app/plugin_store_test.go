package app_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	backendApp "cpa-helper/backend/internal/app"
)

func TestPluginStoreProxiesListAndInstall(t *testing.T) {
	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())

	var gotListAuth string
	var gotInstallAuth string
	var gotInstallSource string
	var gotInstallVersion string
	var gotInstallBody struct {
		Version string `json:"version"`
	}

	cpa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v0/management/plugin-store":
			gotListAuth = r.Header.Get("Authorization")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"plugins_enabled": true,
				"plugins_dir":     "plugins",
				"sources":         []map[string]any{{"id": "official", "name": "Official", "url": "https://plugins.example/registry.json"}},
				"plugins": []map[string]any{{
					"store_id":          "official/sample-plugin",
					"source_id":         "official",
					"source_name":       "Official",
					"source_url":        "https://plugins.example/registry.json",
					"id":                "sample-plugin",
					"name":              "Sample Plugin",
					"version":           "0.1.0",
					"install_type":      "github-release",
					"installed":         false,
					"configured":        false,
					"registered":        false,
					"enabled":           false,
					"effective_enabled": false,
					"update_available":  false,
					"auth_required":     false,
					"auth_configured":   false,
					"installed_version": "",
					"path":              "",
				}},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/v0/management/plugin-store/sample-plugin/install":
			gotInstallAuth = r.Header.Get("Authorization")
			gotInstallSource = r.URL.Query().Get("source")
			gotInstallVersion = r.URL.Query().Get("version")
			if err := json.NewDecoder(r.Body).Decode(&gotInstallBody); err != nil {
				t.Fatalf("decode install body: %v", err)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status":           "installed",
				"source_id":        "official",
				"source_name":      "Official",
				"source_url":       "https://plugins.example/registry.json",
				"id":               "sample-plugin",
				"version":          "0.1.0",
				"install_type":     "github-release",
				"path":             "plugins/linux/amd64/sample-plugin-v0.1.0.so",
				"plugins_enabled":  true,
				"restart_required": true,
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
		"password": "secret123",
		"nickname": "Admin",
	}, nil, nil)
	requestJSON(t, handler, http.MethodPut, "/api/settings", map[string]any{
		"cliaproxy_url":     cpa.URL,
		"management_key":    "mgmt-secret",
		"collector_enabled": false,
	}, cookies, nil)

	var listResponse struct {
		PluginsEnabled bool `json:"plugins_enabled"`
		Plugins        []struct {
			ID string `json:"id"`
		} `json:"plugins"`
	}
	requestJSON(t, handler, http.MethodGet, "/api/plugin-store", nil, cookies, &listResponse)
	if gotListAuth != "Bearer mgmt-secret" {
		t.Fatalf("list Authorization = %q, want management bearer", gotListAuth)
	}
	if !listResponse.PluginsEnabled || len(listResponse.Plugins) != 1 || listResponse.Plugins[0].ID != "sample-plugin" {
		t.Fatalf("list response = %#v", listResponse)
	}

	var installResponse struct {
		ID              string `json:"id"`
		RestartRequired bool   `json:"restart_required"`
	}
	requestJSON(t, handler, http.MethodPost, "/api/plugin-store/sample-plugin/install", map[string]any{
		"source":  "official",
		"version": "0.1.0",
	}, cookies, &installResponse)
	if gotInstallAuth != "Bearer mgmt-secret" {
		t.Fatalf("install Authorization = %q, want management bearer", gotInstallAuth)
	}
	if gotInstallSource != "official" || gotInstallVersion != "0.1.0" || gotInstallBody.Version != "0.1.0" {
		t.Fatalf("install source/version = %q/%q body %#v", gotInstallSource, gotInstallVersion, gotInstallBody)
	}
	if installResponse.ID != "sample-plugin" || !installResponse.RestartRequired {
		t.Fatalf("install response = %#v", installResponse)
	}
}

func TestPluginStoreRejectsNonAdmin(t *testing.T) {
	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())

	app, err := backendApp.New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer app.Close()

	handler := app.Routes()
	adminCookies := requestJSON(t, handler, http.MethodPost, "/api/auth/setup", map[string]any{
		"username": "admin",
		"password": "secret123",
		"nickname": "Admin",
	}, nil, nil)
	requestJSON(t, handler, http.MethodPost, "/api/users", map[string]any{
		"username": "user",
		"password": "secret123",
		"nickname": "User",
		"is_admin": false,
	}, adminCookies, nil)
	userCookies := requestJSON(t, handler, http.MethodPost, "/api/auth/login", map[string]any{
		"username": "user",
		"password": "secret123",
	}, nil, nil)

	requestJSONExpectStatus(t, handler, http.MethodGet, "/api/plugin-store", nil, userCookies, http.StatusForbidden)
}
