package app_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	backendApp "cpa-helper/backend/internal/app"
)

type userDeleteTestUser struct {
	ID       int    `json:"id"`
	Username string `json:"username"`
}

type userDeleteTestAPIKey struct {
	APIKeyHash string `json:"api_key_hash"`
}

func TestDeleteUserRemovesUserAndLocalAPIKeys(t *testing.T) {
	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())

	remoteKeys := []string{"sk-delete-member-key"}
	cpa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v0/management/api-keys" {
			http.NotFound(w, r)
			return
		}
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{"api-keys": remoteKeys})
		case http.MethodPut:
			if err := json.NewDecoder(r.Body).Decode(&remoteKeys); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"api-keys": remoteKeys})
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
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
		"management_key":    "test-management-key",
		"collector_enabled": false,
	}, adminCookies, nil)

	member := userDeleteTestUser{}
	requestJSON(t, handler, http.MethodPost, "/api/users", map[string]any{
		"username": "member",
		"password": "member-password",
		"nickname": "Member",
		"is_admin": false,
	}, adminCookies, &member)

	boundKey := userDeleteTestAPIKey{}
	requestJSON(t, handler, http.MethodPost, "/api/users/"+strconv.Itoa(member.ID)+"/api-keys", map[string]any{
		"api_key":     "sk-delete-member-key",
		"description": "member-key",
	}, adminCookies, &boundKey)
	if boundKey.APIKeyHash == "" {
		t.Fatal("created API key hash is empty")
	}

	requestJSONExpectStatus(t, handler, http.MethodDelete, "/api/users/"+strconv.Itoa(member.ID), nil, adminCookies, http.StatusNoContent)
	if len(remoteKeys) != 0 {
		t.Fatalf("remote API key was not removed: %#v", remoteKeys)
	}

	var users []userDeleteTestUser
	requestJSON(t, handler, http.MethodGet, "/api/users", nil, adminCookies, &users)
	for _, user := range users {
		if user.ID == member.ID || user.Username == "member" {
			t.Fatalf("deleted user still present: %#v", user)
		}
	}

	var keys []userDeleteTestAPIKey
	requestJSON(t, handler, http.MethodGet, "/api/users/observed-api-keys", nil, adminCookies, &keys)
	for _, key := range keys {
		if key.APIKeyHash == boundKey.APIKeyHash {
			t.Fatalf("deleted user API key still present: %#v", key)
		}
	}
}

func TestDeleteInitialAdminIsRejected(t *testing.T) {
	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())

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

	requestJSONExpectStatus(t, handler, http.MethodDelete, "/api/users/1", nil, adminCookies, http.StatusConflict)
}
