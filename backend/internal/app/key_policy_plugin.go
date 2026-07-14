package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

const keyPolicyPluginID = "cpa-key-policy"

type keyPolicyAttributes struct {
	RPM                 int                  `json:"rpm"`
	Models              []keyPolicyModelRule `json:"models"`
	Aliases             []keyPolicyAliasRef  `json:"aliases"`
	DailyLimitUSD       float64              `json:"daily_limit_usd"`
	WeeklyLimitUSD      float64              `json:"weekly_limit_usd"`
	AllowModelsEndpoint bool                 `json:"allow_models_endpoint"`
}

type keyPolicyPluginPayload struct {
	ID                  string               `json:"id"`
	Name                string               `json:"name"`
	Enabled             bool                 `json:"enabled"`
	RPM                 int                  `json:"rpm"`
	Models              []keyPolicyModelRule `json:"models,omitempty"`
	Aliases             []keyPolicyAliasRef  `json:"aliases,omitempty"`
	DailyLimitUSD       float64              `json:"daily_limit_usd,omitempty"`
	WeeklyLimitUSD      float64              `json:"weekly_limit_usd,omitempty"`
	AllowModelsEndpoint bool                 `json:"allow_models_endpoint,omitempty"`
}

type keyPolicyModelRule struct {
	Alias       string `json:"alias"`
	Provider    string `json:"provider"`
	TargetModel string `json:"target_model"`
	Group       string `json:"group,omitempty"`
}

type keyPolicyAliasRef struct {
	Alias string `json:"alias"`
}

type keyPolicyPluginCreateResponse struct {
	PlainKey  string `json:"plain_key"`
	Generated bool   `json:"generated"`
	Key       struct {
		ID         string `json:"id"`
		Name       string `json:"name"`
		KeyPreview string `json:"key_preview"`
	} `json:"key"`
}

func (a *App) createKeyPolicyPluginKey(ctx context.Context, cfg AppConfig, userID int, username, description string, policy keyPolicyAttributes) (string, error) {
	if strings.TrimSpace(cfg.Collector.ManagementKey) == "" {
		return "", validationError("?????????? CPA management key")
	}
	payload := keyPolicyPayload(userID, username, description, policy)
	response, body, err := doJSON(ctx, httpClient(apiKeySyncTimeout), http.MethodPost, makeURL(cfg.Collector.CLIProxyURL, "/v0/management/plugins/"+keyPolicyPluginID+"/keys", nil), managementHeaders(cfg.Collector.ManagementKey), payload)
	if err != nil {
		return "", remoteAPIKeyError("?? key-policy ????", err)
	}
	if response.StatusCode == http.StatusNotFound || response.StatusCode == http.StatusMethodNotAllowed {
		return "", validationError("CPA ??????? cpa-plugin-key-policy ??")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", validationError(fmt.Sprintf("?? key-policy ???????HTTP %d %s", response.StatusCode, strings.TrimSpace(string(body))))
	}
	var result keyPolicyPluginCreateResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return "", validationError("key-policy ????????")
	}
	if strings.TrimSpace(result.PlainKey) == "" {
		return "", validationError("key-policy ????? plain_key")
	}
	return result.PlainKey, nil
}

func (a *App) updateKeyPolicyPluginKey(ctx context.Context, cfg AppConfig, userID int, username, description string, policy keyPolicyAttributes) error {
	if strings.TrimSpace(cfg.Collector.ManagementKey) == "" {
		return validationError("?????????? CPA management key")
	}
	payload := keyPolicyPayload(userID, username, description, policy)
	response, body, err := doJSON(ctx, httpClient(apiKeySyncTimeout), http.MethodPatch, makeURL(cfg.Collector.CLIProxyURL, "/v0/management/plugins/"+keyPolicyPluginID+"/keys", nil), managementHeaders(cfg.Collector.ManagementKey), payload)
	if err != nil {
		return remoteAPIKeyError("?? key-policy ????", err)
	}
	if response.StatusCode == http.StatusNotFound || response.StatusCode == http.StatusMethodNotAllowed {
		return validationError("CPA ??????? cpa-plugin-key-policy ??")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return validationError(fmt.Sprintf("?? key-policy ???????HTTP %d %s", response.StatusCode, strings.TrimSpace(string(body))))
	}
	return nil
}

func keyPolicyPayload(userID int, username, description string, policy keyPolicyAttributes) keyPolicyPluginPayload {
	return keyPolicyPluginPayload{
		ID:                  fmt.Sprintf("cpa-helper-user-%d", userID),
		Name:                strings.TrimSpace(username + " - " + description),
		Enabled:             true,
		RPM:                 policy.RPM,
		Models:              policy.Models,
		Aliases:             policy.Aliases,
		DailyLimitUSD:       policy.DailyLimitUSD,
		WeeklyLimitUSD:      policy.WeeklyLimitUSD,
		AllowModelsEndpoint: policy.AllowModelsEndpoint,
	}
}
