package cpahttp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestClientReusesConfiguredClient(t *testing.T) {
	left := Client(8 * time.Second)
	right := Client(8 * time.Second)
	if left != right {
		t.Fatal("Client returned different instances for the same timeout")
	}
	transport, ok := left.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport type = %T", left.Transport)
	}
	if transport.MaxIdleConnsPerHost < 2 || transport.MaxConnsPerHost <= 0 || transport.ResponseHeaderTimeout <= 0 {
		t.Fatalf("transport limits were not configured: %+v", transport)
	}
}

func TestDoJSONRejectsOversizedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", maxResponseBodyBytes+1)))
	}))
	defer server.Close()

	_, _, err := DoJSON(context.Background(), Client(5*time.Second), http.MethodGet, server.URL, nil, nil)
	if !errors.Is(err, ErrResponseTooLarge) {
		t.Fatalf("DoJSON error = %v, want ErrResponseTooLarge", err)
	}
}
