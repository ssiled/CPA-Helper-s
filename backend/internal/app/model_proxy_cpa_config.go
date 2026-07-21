package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const (
	modelProxyDefaultResponseHeaderTimeout = 30 * time.Second
	modelProxyResponseHeaderSafetyMargin   = 30 * time.Second
	modelProxyBootstrapRetryMargin         = 5 * time.Second
	modelProxyCPAConfigTTL                 = time.Minute
	modelProxyCPAConfigFallbackTTL         = 15 * time.Second
	modelProxyMaxKeepaliveSeconds          = 3600
	modelProxyMaxBootstrapRetries          = 12
)

type modelProxyCPAConfig struct {
	Streaming struct {
		KeepaliveSeconds int `json:"keepalive-seconds"`
		BootstrapRetries int `json:"bootstrap-retries"`
	} `json:"streaming"`
	NonstreamKeepaliveInterval int `json:"nonstream-keepalive-interval"`
}

type modelProxyCPAConfigCacheEntry struct {
	config    modelProxyCPAConfig
	synced    bool
	expiresAt time.Time
}

func (a *App) modelProxyResponseHeaderTimeout(ctx context.Context, cfg AppConfig, target AuthPoolProxyTargetConfig) time.Duration {
	if strings.TrimSpace(target.ManagementKey) == "" && sameCPAURL(target.CPAURL, cfg.Collector.CLIProxyURL) {
		target.ManagementKey = strings.TrimSpace(cfg.Collector.ManagementKey)
	}
	streamConfig, synced := a.cachedModelProxyCPAConfig(ctx, target)
	if !synced {
		return modelProxyDefaultResponseHeaderTimeout
	}
	return effectiveModelProxyResponseHeaderTimeout(streamConfig)
}

func (a *App) cachedModelProxyCPAConfig(ctx context.Context, target AuthPoolProxyTargetConfig) (modelProxyCPAConfig, bool) {
	cacheKey := normalizeCPAURL(target.CPAURL)
	if cacheKey == "" || strings.TrimSpace(target.ManagementKey) == "" {
		return modelProxyCPAConfig{}, false
	}

	a.modelProxyCPAConfigMu.Lock()
	defer a.modelProxyCPAConfigMu.Unlock()
	now := time.Now()
	previous, hasPrevious := a.modelProxyCPAConfigCache[cacheKey]
	if hasPrevious && now.Before(previous.expiresAt) {
		return previous.config, previous.synced
	}

	streamConfig, err := fetchModelProxyCPAConfig(ctx, target)
	synced := err == nil
	ttl := modelProxyCPAConfigTTL
	if !synced {
		ttl = modelProxyCPAConfigFallbackTTL
		if hasPrevious && previous.synced {
			streamConfig = previous.config
			synced = true
		} else {
			streamConfig = modelProxyCPAConfig{}
		}
	}
	if a.modelProxyCPAConfigCache == nil {
		a.modelProxyCPAConfigCache = make(map[string]modelProxyCPAConfigCacheEntry)
	}
	a.modelProxyCPAConfigCache[cacheKey] = modelProxyCPAConfigCacheEntry{
		config:    streamConfig,
		synced:    synced,
		expiresAt: now.Add(ttl),
	}
	return streamConfig, synced
}

func fetchModelProxyCPAConfig(ctx context.Context, target AuthPoolProxyTargetConfig) (modelProxyCPAConfig, error) {
	response, payload, err := doJSON(
		ctx,
		httpClient(apiKeySyncTimeout),
		http.MethodGet,
		makeURL(target.CPAURL, "/v0/management/config", nil),
		managementHeaders(target.ManagementKey),
		nil,
	)
	if err != nil {
		return modelProxyCPAConfig{}, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return modelProxyCPAConfig{}, fmt.Errorf("CPA management config returned HTTP %d", response.StatusCode)
	}
	var streamConfig modelProxyCPAConfig
	if err := json.Unmarshal(payload, &streamConfig); err != nil {
		return modelProxyCPAConfig{}, fmt.Errorf("decode CPA management config: %w", err)
	}
	return streamConfig, nil
}

func effectiveModelProxyResponseHeaderTimeout(streamConfig modelProxyCPAConfig) time.Duration {
	keepaliveSeconds := maxPositiveInt(
		streamConfig.Streaming.KeepaliveSeconds,
		streamConfig.NonstreamKeepaliveInterval,
	)
	keepaliveSeconds = minPositiveInt(keepaliveSeconds, modelProxyMaxKeepaliveSeconds)
	retries := streamConfig.Streaming.BootstrapRetries
	if retries < 0 {
		retries = 0
	}
	if retries > modelProxyMaxBootstrapRetries {
		retries = modelProxyMaxBootstrapRetries
	}

	timeout := modelProxyDefaultResponseHeaderTimeout
	if keepaliveSeconds > 0 {
		keepalive := time.Duration(keepaliveSeconds) * time.Second
		if keepalive > timeout {
			timeout = keepalive
		}
		timeout += modelProxyResponseHeaderSafetyMargin
	}
	return timeout + time.Duration(retries)*modelProxyBootstrapRetryMargin
}

func normalizeCPAURL(value string) string {
	return strings.ToLower(strings.TrimRight(strings.TrimSpace(value), "/"))
}

func sameCPAURL(left, right string) bool {
	return normalizeCPAURL(left) != "" && normalizeCPAURL(left) == normalizeCPAURL(right)
}

func maxPositiveInt(values ...int) int {
	result := 0
	for _, value := range values {
		if value > result {
			result = value
		}
	}
	return result
}

func minPositiveInt(value, maximum int) int {
	if value <= 0 {
		return 0
	}
	if value > maximum {
		return maximum
	}
	return value
}
