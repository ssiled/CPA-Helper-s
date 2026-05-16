package cpahttp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func Client(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout}
}

func ManagementHeaders(key string) http.Header {
	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+key)
	headers.Set("X-Management-Key", key)
	return headers
}

func MakeURL(baseURL, path string, query url.Values) string {
	base := strings.TrimRight(baseURL, "/")
	target := base + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	return target
}

func DoJSON(ctx context.Context, client *http.Client, method, target string, headers http.Header, body any) (*http.Response, []byte, error) {
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return nil, nil, err
		}
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return nil, nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for key, values := range headers {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return resp, nil, err
	}
	return resp, payload, nil
}

func EnsureHTTPSURL(sourceURL string) error {
	parsed, err := url.Parse(sourceURL)
	if err != nil {
		return err
	}
	if parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return errInvalidURL
	}
	return nil
}

type invalidURLError struct{}

func (invalidURLError) Error() string {
	return "url must be http or https"
}

var errInvalidURL error = invalidURLError{}
