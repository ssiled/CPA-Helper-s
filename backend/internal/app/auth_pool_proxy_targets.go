package app

import (
	"encoding/json"
	"fmt"
	"strings"
)

type AuthPoolProxyTargetConfig struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	CPAURL        string `json:"cpa_url"`
	ManagementKey string `json:"management_key,omitempty"`
	APIKey        string `json:"api_key,omitempty"`
	Enabled       bool   `json:"enabled"`
}

func decodeAuthPoolProxyTargetsJSON(value string) []AuthPoolProxyTargetConfig {
	value = strings.TrimSpace(value)
	if value == "" {
		return []AuthPoolProxyTargetConfig{}
	}
	var targets []AuthPoolProxyTargetConfig
	if err := json.Unmarshal([]byte(value), &targets); err != nil {
		return []AuthPoolProxyTargetConfig{}
	}
	return normalizeAuthPoolProxyTargets(targets)
}

func encodeAuthPoolProxyTargetsJSON(targets []AuthPoolProxyTargetConfig) string {
	data, err := json.Marshal(normalizeAuthPoolProxyTargets(targets))
	if err != nil {
		return "[]"
	}
	return string(data)
}

func normalizeAuthPoolProxyTargets(input []AuthPoolProxyTargetConfig) []AuthPoolProxyTargetConfig {
	result := make([]AuthPoolProxyTargetConfig, 0, len(input))
	seen := map[string]int{}
	for _, target := range input {
		normalized := AuthPoolProxyTargetConfig{
			ID:            strings.TrimSpace(target.ID),
			Name:          strings.TrimSpace(target.Name),
			CPAURL:        strings.TrimRight(strings.TrimSpace(target.CPAURL), "/"),
			ManagementKey: strings.TrimSpace(target.ManagementKey),
			APIKey:        strings.TrimSpace(target.APIKey),
			Enabled:       target.Enabled,
		}
		if normalized.CPAURL == "" && normalized.ManagementKey == "" && normalized.APIKey == "" && normalized.Name == "" {
			continue
		}
		if normalized.ID == "" {
			normalized.ID = fmt.Sprintf("cpa-%d", len(result)+1)
		}
		if count := seen[normalized.ID]; count > 0 {
			normalized.ID = fmt.Sprintf("%s-%d", normalized.ID, count+1)
		}
		seen[normalized.ID]++
		if normalized.Name == "" {
			if normalized.CPAURL != "" {
				normalized.Name = normalized.CPAURL
			} else {
				normalized.Name = normalized.ID
			}
		}
		result = append(result, normalized)
	}
	return result
}

func activeAuthPoolProxyTarget(cfg AppConfig) (AuthPoolProxyTargetConfig, bool) {
	for _, target := range normalizeAuthPoolProxyTargets(cfg.AuthPoolProxyTargets) {
		if !target.Enabled {
			continue
		}
		if strings.TrimSpace(target.CPAURL) == "" || strings.TrimSpace(target.APIKey) == "" {
			continue
		}
		return target, true
	}
	if strings.TrimSpace(cfg.AuthPoolProxyAPIKey) != "" && strings.TrimSpace(cfg.Collector.CLIProxyURL) != "" {
		return AuthPoolProxyTargetConfig{
			ID:            "default",
			Name:          "Default CPA",
			CPAURL:        strings.TrimRight(strings.TrimSpace(cfg.Collector.CLIProxyURL), "/"),
			ManagementKey: strings.TrimSpace(cfg.Collector.ManagementKey),
			APIKey:        strings.TrimSpace(cfg.AuthPoolProxyAPIKey),
			Enabled:       true,
		}, true
	}
	return AuthPoolProxyTargetConfig{}, false
}

func authPoolProxyModeEnabled(cfg AppConfig) bool {
	_, ok := activeAuthPoolProxyTarget(cfg)
	return ok
}

func primaryCPAManagementTarget(cfg AppConfig) (AuthPoolProxyTargetConfig, bool) {
	if target, ok := activeAuthPoolProxyTarget(cfg); ok && strings.TrimSpace(target.ManagementKey) != "" {
		return target, true
	}
	if strings.TrimSpace(cfg.Collector.CLIProxyURL) != "" && strings.TrimSpace(cfg.Collector.ManagementKey) != "" {
		return AuthPoolProxyTargetConfig{
			ID:            "default",
			Name:          "Default CPA",
			CPAURL:        strings.TrimRight(strings.TrimSpace(cfg.Collector.CLIProxyURL), "/"),
			ManagementKey: strings.TrimSpace(cfg.Collector.ManagementKey),
			APIKey:        strings.TrimSpace(cfg.AuthPoolProxyAPIKey),
			Enabled:       true,
		}, true
	}
	return AuthPoolProxyTargetConfig{}, false
}
