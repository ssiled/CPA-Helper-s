package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

type pluginStoreListResponse struct {
	PluginsEnabled bool                     `json:"plugins_enabled"`
	PluginsDir     string                   `json:"plugins_dir"`
	Sources        []pluginStoreSource      `json:"sources"`
	SourceErrors   []pluginStoreSourceError `json:"source_errors,omitempty"`
	Plugins        []pluginStoreEntry       `json:"plugins"`
}

type pluginStoreSource struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	URL  string `json:"url"`
}

type pluginStoreSourceError struct {
	SourceID   string `json:"source_id"`
	SourceName string `json:"source_name"`
	SourceURL  string `json:"source_url"`
	Message    string `json:"message"`
}

type pluginStoreEntry struct {
	StoreID             string                `json:"store_id"`
	SourceID            string                `json:"source_id"`
	SourceName          string                `json:"source_name"`
	SourceURL           string                `json:"source_url"`
	ID                  string                `json:"id"`
	Name                string                `json:"name"`
	Description         string                `json:"description"`
	Author              string                `json:"author"`
	Version             string                `json:"version"`
	Repository          string                `json:"repository"`
	InstallType         string                `json:"install_type"`
	AuthRequired        bool                  `json:"auth_required"`
	AuthConfigured      bool                  `json:"auth_configured"`
	Platforms           []pluginStorePlatform `json:"platforms,omitempty"`
	Logo                string                `json:"logo,omitempty"`
	Homepage            string                `json:"homepage,omitempty"`
	License             string                `json:"license,omitempty"`
	Tags                []string              `json:"tags,omitempty"`
	Installed           bool                  `json:"installed"`
	InstalledVersion    string                `json:"installed_version"`
	InstalledSourceID   string                `json:"installed_source_id,omitempty"`
	InstallSourceStatus string                `json:"install_source_status,omitempty"`
	Path                string                `json:"path"`
	Configured          bool                  `json:"configured"`
	Registered          bool                  `json:"registered"`
	Enabled             bool                  `json:"enabled"`
	EffectiveEnabled    bool                  `json:"effective_enabled"`
	UpdateAvailable     bool                  `json:"update_available"`
}

type pluginStorePlatform struct {
	GOOS   string `json:"goos"`
	GOARCH string `json:"goarch"`
}

type pluginStoreInstallPayload struct {
	Version string `json:"version,omitempty"`
	Source  string `json:"source,omitempty"`
}

type pluginStoreInstallResponse struct {
	Status          string `json:"status"`
	SourceID        string `json:"source_id"`
	SourceName      string `json:"source_name"`
	SourceURL       string `json:"source_url"`
	ID              string `json:"id"`
	Version         string `json:"version"`
	InstallType     string `json:"install_type"`
	Path            string `json:"path"`
	PluginsEnabled  bool   `json:"plugins_enabled"`
	RestartRequired bool   `json:"restart_required"`
}

func (a *App) handlePluginStore(w http.ResponseWriter, r *http.Request) error {
	user, err := a.currentUser(r.Context(), r)
	if err != nil {
		return err
	}
	if !user.IsAdmin {
		return forbiddenError("admin required")
	}
	parts := pluginStorePathParts(r.URL.Path)
	if len(parts) == 0 {
		if r.Method != http.MethodGet {
			return methodNotAllowed()
		}
		var response pluginStoreListResponse
		if err := a.cpaManagementJSONWithQuery(r.Context(), http.MethodGet, "/v0/management/plugin-store", r.URL.Query(), nil, &response); err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, response)
		return nil
	}
	if len(parts) == 2 && parts[1] == "install" {
		if r.Method != http.MethodPost {
			return methodNotAllowed()
		}
		var payload pluginStoreInstallPayload
		if err := decodeJSON(r, &payload); err != nil {
			return err
		}
		query := url.Values{}
		if strings.TrimSpace(payload.Source) != "" {
			query.Set("source", strings.TrimSpace(payload.Source))
		}
		if strings.TrimSpace(payload.Version) != "" {
			query.Set("version", strings.TrimSpace(payload.Version))
		}
		var response pluginStoreInstallResponse
		remotePath := "/v0/management/plugin-store/" + url.PathEscape(parts[0]) + "/install"
		if err := a.cpaManagementJSONWithQuery(r.Context(), http.MethodPost, remotePath, query, map[string]string{"version": strings.TrimSpace(payload.Version)}, &response); err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, response)
		return nil
	}
	return notFoundError("plugin store route not found")
}

func pluginStorePathParts(path string) []string {
	trimmed := strings.Trim(strings.TrimPrefix(path, "/api/plugin-store"), "/")
	if trimmed == "" {
		return nil
	}
	parts := strings.Split(trimmed, "/")
	for index, part := range parts {
		decoded, err := url.PathUnescape(part)
		if err == nil {
			parts[index] = decoded
		}
	}
	return parts
}

func (a *App) cpaManagementJSONWithQuery(ctx context.Context, method, path string, query url.Values, body any, target any) error {
	cfg, err := a.loadConfig(ctx)
	if err != nil {
		return err
	}
	cpaTarget, ok := primaryCPAManagementTarget(cfg)
	if !ok {
		return validationError("CPA URL and management key are required")
	}
	response, payload, err := doJSON(ctx, httpClient(apiKeySyncTimeout), method, makeURL(cpaTarget.CPAURL, path, query), managementHeaders(cpaTarget.ManagementKey), body)
	if err != nil {
		return remoteAPIKeyError("CPA management request failed", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return validationError(fmt.Sprintf("CPA management request failed: HTTP %d %s", response.StatusCode, strings.TrimSpace(string(payload))))
	}
	if target != nil && len(payload) > 0 {
		if err := json.Unmarshal(payload, target); err != nil {
			return validationError("CPA management returned invalid JSON")
		}
	}
	return nil
}
