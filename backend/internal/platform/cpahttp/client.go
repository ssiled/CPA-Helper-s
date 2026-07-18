package cpahttp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	maxResponseBodyBytes = 8 << 20
	maxCachedClients     = 32
)

var (
	sharedTransport = &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          128,
		MaxIdleConnsPerHost:   32,
		MaxConnsPerHost:       128,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		ExpectContinueTimeout: time.Second,
	}
	clientMu    sync.RWMutex
	clientCache = map[time.Duration]*http.Client{}
)

func Client(timeout time.Duration) *http.Client {
	clientMu.RLock()
	client := clientCache[timeout]
	clientMu.RUnlock()
	if client != nil {
		return client
	}
	clientMu.Lock()
	defer clientMu.Unlock()
	if client = clientCache[timeout]; client != nil {
		return client
	}
	client = &http.Client{Transport: sharedTransport, Timeout: timeout}
	if len(clientCache) < maxCachedClients {
		clientCache[timeout] = client
	}
	return client
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
	payload, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBodyBytes+1))
	if err != nil {
		return resp, nil, err
	}
	if len(payload) > maxResponseBodyBytes {
		return resp, nil, ErrResponseTooLarge
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

var ErrResponseTooLarge = errors.New("response body exceeds limit")
