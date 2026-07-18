package app

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestReadRespRejectsOversizedFrames(t *testing.T) {
	tests := []string{
		"$8388609\r\n",
		"*10001\r\n",
		"$-2\r\n",
	}
	for _, input := range tests {
		if _, err := readResp(bufio.NewReader(strings.NewReader(input))); err == nil {
			t.Fatalf("readResp accepted oversized or invalid frame %q", input)
		}
	}
}

func TestConsumeRespQueueUsesHTTPManagementUsageQueue(t *testing.T) {
	requested := false
	cpa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested = true
		if r.URL.Path != "/v0/management/usage-queue" {
			t.Fatalf("path = %q, want /v0/management/usage-queue", r.URL.Path)
		}
		if got := r.URL.Query().Get("count"); got != "2" {
			t.Fatalf("count query = %q, want 2", got)
		}
		if got := r.Header.Get("X-Management-Key"); got != "test-management-key" {
			t.Fatalf("management header = %q, want test-management-key", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]any{
			map[string]any{"request_id": "req-http", "total_tokens": 12},
			`{"request_id":"req-string"}`,
			nil,
		})
	}))
	defer cpa.Close()

	items, err := consumeRespQueue(context.Background(), CollectorConfig{
		CLIProxyURL:   cpa.URL,
		ManagementKey: "test-management-key",
		QueueName:     "usage",
		BatchSize:     2,
	})
	if err != nil {
		t.Fatalf("consumeRespQueue returned error: %v", err)
	}
	if !requested {
		t.Fatal("HTTP usage queue endpoint was not requested")
	}
	if len(items) != 2 {
		t.Fatalf("items len = %d, want 2: %#v", len(items), items)
	}
	var first map[string]any
	if err := json.Unmarshal([]byte(items[0]), &first); err != nil {
		t.Fatalf("first item is not JSON object: %q", items[0])
	}
	if first["request_id"] != "req-http" {
		t.Fatalf("first request_id = %#v, want req-http", first["request_id"])
	}
	if items[1] != `{"request_id":"req-string"}` {
		t.Fatalf("second item = %q, want encoded string payload", items[1])
	}
}

func TestUsesRespQueueProtocolOnlyForExplicitRawProtocols(t *testing.T) {
	tests := map[string]bool{
		"https://api.example.com":     false,
		"http://127.0.0.1:8317":       false,
		"api.example.com:8317":        false,
		"tcp://127.0.0.1:8317":        true,
		"redis://127.0.0.1:8317":      true,
		"resp://127.0.0.1:8317":       true,
		"wss://api.example.com/ws":    false,
		"https://api.example.com:443": false,
	}
	for rawURL, want := range tests {
		if got := usesRespQueueProtocol(rawURL); got != want {
			t.Fatalf("usesRespQueueProtocol(%q) = %v, want %v", rawURL, got, want)
		}
	}
}

func TestSyncRemoteUsageEnabledClearsDisabledCollectorStaleErrorOnSuccess(t *testing.T) {
	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())
	cpa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v0/management/usage-statistics-enabled" {
			t.Fatalf("path = %q, want /v0/management/usage-statistics-enabled", r.URL.Path)
		}
		if got := r.Header.Get("X-Management-Key"); got != "test-management-key" {
			t.Fatalf("management header = %q, want test-management-key", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]bool{"usage-statistics-enabled": true})
	}))
	defer cpa.Close()

	app, err := New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer app.Close()
	app.collector.Stop()

	ctx := context.Background()
	cfg, err := app.loadConfig(ctx)
	if err != nil {
		t.Fatalf("loadConfig failed: %v", err)
	}
	cfg.Collector.CLIProxyURL = cpa.URL
	cfg.Collector.ManagementKey = "test-management-key"
	cfg.Collector.Enabled = false

	staleError := "remote usage toggle query failed: timeout"
	if err := app.collector.updateState(ctx, collectorPatch{LastError: &staleError}); err != nil {
		t.Fatalf("updateState failed: %v", err)
	}
	app.collector.mu.Lock()
	app.collector.lastRemoteSyncAt = time.Time{}
	app.collector.mu.Unlock()

	app.collector.syncRemoteUsageEnabled(ctx, cfg)

	state, err := app.collectorState(ctx)
	if err != nil {
		t.Fatalf("collectorState failed: %v", err)
	}
	if state.LastError != nil {
		t.Fatalf("last error = %q, want cleared", *state.LastError)
	}
	if state.RemoteEnabled == nil || !*state.RemoteEnabled {
		t.Fatalf("remote enabled = %v, want true", state.RemoteEnabled)
	}
}

func TestSyncRemoteUsageEnabledKeepsEnabledCollectorErrorOnSuccess(t *testing.T) {
	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())
	cpa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]bool{"usage-statistics-enabled": true})
	}))
	defer cpa.Close()

	app, err := New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer app.Close()
	app.collector.Stop()

	ctx := context.Background()
	cfg, err := app.loadConfig(ctx)
	if err != nil {
		t.Fatalf("loadConfig failed: %v", err)
	}
	cfg.Collector.CLIProxyURL = cpa.URL
	cfg.Collector.ManagementKey = "test-management-key"
	cfg.Collector.Enabled = true

	collectorError := "usage queue HTTP 500"
	if err := app.collector.updateState(ctx, collectorPatch{LastError: &collectorError}); err != nil {
		t.Fatalf("updateState failed: %v", err)
	}
	app.collector.mu.Lock()
	app.collector.lastRemoteSyncAt = time.Time{}
	app.collector.mu.Unlock()

	app.collector.syncRemoteUsageEnabled(ctx, cfg)

	state, err := app.collectorState(ctx)
	if err != nil {
		t.Fatalf("collectorState failed: %v", err)
	}
	if state.LastError == nil || *state.LastError != collectorError {
		t.Fatalf("last error = %v, want %q", state.LastError, collectorError)
	}
}
