package app

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestFetchModelProxyCPAConfigParsesStreamingFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v0/management/config" {
			t.Fatalf("path = %q, want management config", r.URL.Path)
		}
		if r.Header.Get("X-Management-Key") != "mgmt-secret" {
			t.Fatalf("management key header = %q", r.Header.Get("X-Management-Key"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"streaming":{"keepalive-seconds":300,"bootstrap-retries":5},"nonstream-keepalive-interval":300}`))
	}))
	defer server.Close()

	config, err := fetchModelProxyCPAConfig(context.Background(), AuthPoolProxyTargetConfig{
		CPAURL:        server.URL,
		ManagementKey: "mgmt-secret",
	})
	if err != nil {
		t.Fatalf("fetchModelProxyCPAConfig failed: %v", err)
	}
	if config.Streaming.KeepaliveSeconds != 300 || config.Streaming.BootstrapRetries != 5 || config.NonstreamKeepaliveInterval != 300 {
		t.Fatalf("config = %+v, want 300 / 5 / 300", config)
	}
	if got, want := effectiveModelProxyResponseHeaderTimeout(config), 355*time.Second; got != want {
		t.Fatalf("response header timeout = %s, want %s", got, want)
	}
}

func TestModelProxyCPAConfigCacheAvoidsPerRequestFetch(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"streaming":{"keepalive-seconds":300,"bootstrap-retries":5},"nonstream-keepalive-interval":300}`))
	}))
	defer server.Close()

	app := &App{}
	target := AuthPoolProxyTargetConfig{CPAURL: server.URL, ManagementKey: "mgmt-secret"}
	const concurrency = 20
	var wait sync.WaitGroup
	errors := make(chan time.Duration, concurrency)
	for range concurrency {
		wait.Add(1)
		go func() {
			defer wait.Done()
			if got := app.modelProxyResponseHeaderTimeout(context.Background(), AppConfig{}, target); got != 355*time.Second {
				errors <- got
			}
		}()
	}
	wait.Wait()
	close(errors)
	for got := range errors {
		t.Errorf("response header timeout = %s, want 355s", got)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("management config calls = %d, want 1", got)
	}
}

func TestModelProxyCPAConfigFailureFallsBackAndIsCached(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	app := &App{}
	target := AuthPoolProxyTargetConfig{CPAURL: server.URL, ManagementKey: "mgmt-secret"}
	for range 2 {
		if got := app.modelProxyResponseHeaderTimeout(context.Background(), AppConfig{}, target); got != modelProxyDefaultResponseHeaderTimeout {
			t.Fatalf("fallback timeout = %s, want %s", got, modelProxyDefaultResponseHeaderTimeout)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("failed management config calls = %d, want cached fallback", got)
	}
}

func TestModelProxyCPAConfigRefreshFailureKeepsLastGoodConfig(t *testing.T) {
	var fail atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fail.Load() {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"streaming":{"keepalive-seconds":300,"bootstrap-retries":5},"nonstream-keepalive-interval":300}`))
	}))
	defer server.Close()

	app := &App{}
	target := AuthPoolProxyTargetConfig{CPAURL: server.URL, ManagementKey: "mgmt-secret"}
	if got := app.modelProxyResponseHeaderTimeout(context.Background(), AppConfig{}, target); got != 355*time.Second {
		t.Fatalf("initial response header timeout = %s, want 355s", got)
	}
	fail.Store(true)
	cacheKey := normalizeCPAURL(server.URL)
	app.modelProxyCPAConfigMu.Lock()
	entry := app.modelProxyCPAConfigCache[cacheKey]
	entry.expiresAt = time.Now().Add(-time.Second)
	app.modelProxyCPAConfigCache[cacheKey] = entry
	app.modelProxyCPAConfigMu.Unlock()

	if got := app.modelProxyResponseHeaderTimeout(context.Background(), AppConfig{}, target); got != 355*time.Second {
		t.Fatalf("stale response header timeout = %s, want last good 355s", got)
	}
}

func TestModelProxyUpstreamTimeoutReturnsGatewayTimeout(t *testing.T) {
	err := modelProxyUpstreamError(timeoutTestError{})
	var appErr *AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("error type = %T, want *AppError", err)
	}
	if appErr.Status != http.StatusGatewayTimeout || appErr.Code != "gateway_timeout" {
		t.Fatalf("app error = %+v, want 504 gateway_timeout", appErr)
	}
}

type timeoutTestError struct{}

func (timeoutTestError) Error() string   { return "timeout awaiting response headers" }
func (timeoutTestError) Timeout() bool   { return true }
func (timeoutTestError) Temporary() bool { return true }
