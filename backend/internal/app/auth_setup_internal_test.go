package app

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestConcurrentFirstAdminSetupCreatesOneAdministrator(t *testing.T) {
	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())
	app, err := NewWithOptions(context.Background(), NewOptions{Migrate: true})
	if err != nil {
		t.Fatalf("NewWithOptions failed: %v", err)
	}
	defer app.Close()

	start := make(chan struct{})
	errors := make(chan error, 2)
	var workers sync.WaitGroup
	for index := 0; index < 2; index++ {
		workers.Add(1)
		go func(index int) {
			defer workers.Done()
			<-start
			body := fmt.Sprintf(`{"username":"admin-%d","password":"pass1234","nickname":"Admin"}`, index)
			request := httptest.NewRequest("POST", "/api/auth/setup", bytes.NewBufferString(body))
			request.Header.Set("Content-Type", "application/json")
			errors <- app.handleSetupFirstAdmin(httptest.NewRecorder(), request)
		}(index)
	}
	close(start)
	workers.Wait()
	close(errors)

	successes := 0
	conflicts := 0
	for err := range errors {
		if err == nil {
			successes++
			continue
		}
		if appErr, ok := err.(*AppError); ok && appErr.Code == "conflict" {
			conflicts++
			continue
		}
		t.Fatalf("unexpected setup error: %v", err)
	}
	count, err := app.userCount(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if successes != 1 || conflicts != 1 || count != 1 {
		t.Fatalf("successes=%d conflicts=%d users=%d, want 1/1/1", successes, conflicts, count)
	}
}

func TestSessionCookieUsesSecureFlagForHTTPSProxy(t *testing.T) {
	request := httptest.NewRequest("POST", "http://example.test/api/auth/login", nil)
	request.Header.Set("X-Forwarded-Proto", "https")
	recorder := httptest.NewRecorder()
	if err := setSessionCookie(recorder, request, 1, "test-secret"); err != nil {
		t.Fatal(err)
	}
	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].Secure || !cookies[0].HttpOnly {
		t.Fatalf("session cookie = %+v, want Secure and HttpOnly", cookies)
	}
}

func TestDecodeJSONRejectsTrailingAndOversizedBodies(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "trailing JSON", body: `{"value":1}{"value":2}`},
		{name: "oversized JSON", body: `{"value":"` + strings.Repeat("x", (4<<20)+1) + `"}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(test.body))
			var payload map[string]any
			if err := decodeJSON(request, &payload); err == nil {
				t.Fatal("decodeJSON accepted an invalid or oversized body")
			}
		})
	}
}
