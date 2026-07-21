package app

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestModelProxyAdmissionQueueAndFastReject(t *testing.T) {
	controller := newModelProxyAdmissionController()
	release, reason := controller.acquire(context.Background(), 1, 1, 500)
	if reason != "" {
		t.Fatalf("first acquire reason = %q", reason)
	}

	type acquireResult struct {
		release func()
		reason  string
	}
	queued := make(chan acquireResult, 1)
	go func() {
		nextRelease, nextReason := controller.acquire(context.Background(), 1, 1, 500)
		queued <- acquireResult{release: nextRelease, reason: nextReason}
	}()
	deadline := time.Now().Add(time.Second)
	for {
		controller.mu.Lock()
		queueLength := len(controller.waiters)
		controller.mu.Unlock()
		if queueLength == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("second request did not enter the queue")
		}
		time.Sleep(time.Millisecond)
	}

	if extraRelease, extraReason := controller.acquire(context.Background(), 1, 1, 500); extraReason != "queue_full" || extraRelease != nil {
		t.Fatalf("third acquire = release %v reason %q, want queue_full", extraRelease != nil, extraReason)
	}
	release()
	result := <-queued
	if result.reason != "" || result.release == nil {
		t.Fatalf("queued acquire = release %v reason %q", result.release != nil, result.reason)
	}
	result.release()
}

func TestModelProxyAdmissionQueueTimeout(t *testing.T) {
	controller := newModelProxyAdmissionController()
	release, _ := controller.acquire(context.Background(), 1, 1, 100)
	defer release()
	started := time.Now()
	queuedRelease, reason := controller.acquire(context.Background(), 1, 1, 25)
	if reason != "queue_timeout" || queuedRelease != nil {
		t.Fatalf("queued acquire = release %v reason %q", queuedRelease != nil, reason)
	}
	if time.Since(started) < 20*time.Millisecond {
		t.Fatal("queue timeout returned too early")
	}
}

func TestReadLimitedProxyBodySpillsAndReplays(t *testing.T) {
	payload := []byte(`{"model":"gpt-5.6-sol","input":"` + strings.Repeat("x", modelProxyMemoryBodyLimit) + `"}`)
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(payload))
	body, err := readLimitedProxyBody(request)
	if err != nil {
		t.Fatal(err)
	}
	if body.path == "" || len(body.memory) != 0 {
		t.Fatalf("body storage = path %q memory %d, want temp file", body.path, len(body.memory))
	}
	path := body.path
	if model := modelFromProxyBody(body); model != "gpt-5.6-sol" {
		t.Fatalf("model = %q", model)
	}
	reader, err := body.open()
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil || !bytes.Equal(replayed, payload) {
		t.Fatalf("replayed body mismatch: err=%v size=%d", err, len(replayed))
	}
	body.close()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("temporary request body still exists: %v", err)
	}
}

func TestScanTopLevelModelAfterLargeUnknownValue(t *testing.T) {
	payload := `{"input":"` + strings.Repeat("x", modelProxyMemoryBodyLimit+1) + `","metadata":{"model":"wrong"},"model":"gpt-5.6-sol"}`
	if model := scanTopLevelModel(strings.NewReader(payload)); model != "gpt-5.6-sol" {
		t.Fatalf("model = %q", model)
	}
}

func TestStreamModelProxyResponseFlushesSSE(t *testing.T) {
	w := &flushResponseWriter{header: http.Header{}}
	response := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader("data: one\n\n")),
	}
	streamModelProxyResponse(w, response)
	if got := w.header.Get("X-Accel-Buffering"); got != "no" {
		t.Fatalf("X-Accel-Buffering = %q", got)
	}
	if w.flushes < 2 {
		t.Fatalf("flush count = %d, want header and data flush", w.flushes)
	}
	if w.body.String() != "data: one\n\n" {
		t.Fatalf("body = %q", w.body.String())
	}
}

func TestWriteModelProxyRateLimitUsesOpenAIEnvelope(t *testing.T) {
	recorder := httptest.NewRecorder()
	writeModelProxyRateLimit(recorder, "queue_full")
	response := recorder.Result()
	if response.StatusCode != http.StatusTooManyRequests || response.Header.Get("Retry-After") != "1" {
		t.Fatalf("status/retry-after = %d/%q", response.StatusCode, response.Header.Get("Retry-After"))
	}
	if body := recorder.Body.String(); !strings.Contains(body, `"type":"rate_limit_error"`) || !strings.Contains(body, `"code":"rate_limit_exceeded"`) {
		t.Fatalf("rate-limit body = %s", body)
	}
}

type flushResponseWriter struct {
	header  http.Header
	body    bytes.Buffer
	status  int
	flushes int
}

func (w *flushResponseWriter) Header() http.Header             { return w.header }
func (w *flushResponseWriter) WriteHeader(status int)          { w.status = status }
func (w *flushResponseWriter) Write(value []byte) (int, error) { return w.body.Write(value) }
func (w *flushResponseWriter) Flush()                          { w.flushes++ }
