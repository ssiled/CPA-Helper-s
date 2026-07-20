package app

import (
	"context"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	authPoolPluginEventDefaultLimit = 100
	authPoolPluginEventMaxLimit     = 500
)

type authPoolPluginEventCandidate struct {
	ID           string   `json:"id"`
	Provider     string   `json:"provider,omitempty"`
	Priority     int      `json:"priority,omitempty"`
	Status       string   `json:"status,omitempty"`
	AccountTypes []string `json:"account_types,omitempty"`
}

type authPoolPluginEvent struct {
	ID               uint64                         `json:"id"`
	Timestamp        string                         `json:"timestamp"`
	Phase            string                         `json:"phase"`
	Status           string                         `json:"status"`
	Reason           string                         `json:"reason,omitempty"`
	ErrorCode        string                         `json:"error_code,omitempty"`
	ErrorMessage     string                         `json:"error_message,omitempty"`
	ErrorDetail      string                         `json:"error_detail,omitempty"`
	PlanType         string                         `json:"plan_type,omitempty"`
	ResetsAt         int64                          `json:"resets_at,omitempty"`
	ResetsInSeconds  int64                          `json:"resets_in_seconds,omitempty"`
	HTTPStatus       int                            `json:"http_status,omitempty"`
	DurationMS       int64                          `json:"duration_ms,omitempty"`
	Provider         string                         `json:"provider,omitempty"`
	Model            string                         `json:"model,omitempty"`
	Stream           bool                           `json:"stream,omitempty"`
	PoolID           string                         `json:"pool_id,omitempty"`
	PoolName         string                         `json:"pool_name,omitempty"`
	UserID           int                            `json:"user_id,omitempty"`
	Username         string                         `json:"username,omitempty"`
	SelectedAuthID   string                         `json:"selected_auth_id,omitempty"`
	SelectedPriority *int                           `json:"selected_priority,omitempty"`
	SelectedState    string                         `json:"selected_state,omitempty"`
	CandidateCount   int                            `json:"candidate_count"`
	MatchedCount     int                            `json:"matched_count"`
	InputCandidates  int                            `json:"input_candidates"`
	PoolMatched      int                            `json:"pool_matched_candidates"`
	Eligible         int                            `json:"eligible_candidates"`
	MatchedAuthIDs   []string                       `json:"matched_auth_ids,omitempty"`
	AccountTypes     []string                       `json:"account_types,omitempty"`
	Candidates       []authPoolPluginEventCandidate `json:"candidates,omitempty"`
	TargetID         string                         `json:"target_id"`
	TargetName       string                         `json:"target_name"`
}

type authPoolPluginEventTargetError struct {
	TargetID   string `json:"target_id"`
	TargetName string `json:"target_name"`
	Error      string `json:"error"`
}

type authPoolPluginEventsResponse struct {
	Items    []authPoolPluginEvent            `json:"items"`
	Total    int                              `json:"total"`
	Capacity int                              `json:"capacity"`
	Errors   []authPoolPluginEventTargetError `json:"errors"`
}

type clearAuthPoolPluginEventsResponse struct {
	Cleared int                              `json:"cleared"`
	Errors  []authPoolPluginEventTargetError `json:"errors"`
}

func authPoolPluginEventLimit(r *http.Request) int {
	limit, _ := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("limit")))
	if limit <= 0 {
		return authPoolPluginEventDefaultLimit
	}
	if limit > authPoolPluginEventMaxLimit {
		return authPoolPluginEventMaxLimit
	}
	return limit
}

func (a *App) authPoolPluginEvents(ctx context.Context, limit int) (authPoolPluginEventsResponse, error) {
	cfg, err := a.loadConfig(ctx)
	if err != nil {
		return authPoolPluginEventsResponse{}, err
	}
	targets := authPoolPluginMonitoringTargets(cfg)
	if len(targets) == 0 {
		return authPoolPluginEventsResponse{}, validationError("CPA URL and management key are required")
	}
	response := authPoolPluginEventsResponse{Items: []authPoolPluginEvent{}, Errors: []authPoolPluginEventTargetError{}}
	for _, target := range targets {
		var remote struct {
			Items    []authPoolPluginEvent `json:"items"`
			Total    int                   `json:"total"`
			Capacity int                   `json:"capacity"`
		}
		path := "/events?limit=" + strconv.Itoa(limit)
		if err := a.authPoolPluginRequestWithTarget(ctx, target, http.MethodGet, path, nil, &remote); err != nil {
			response.Errors = append(response.Errors, authPoolPluginEventTargetError{TargetID: target.ID, TargetName: target.Name, Error: err.Error()})
			continue
		}
		response.Total += remote.Total
		response.Capacity += remote.Capacity
		for _, event := range remote.Items {
			event.TargetID = target.ID
			event.TargetName = target.Name
			response.Items = append(response.Items, event)
		}
	}
	sort.SliceStable(response.Items, func(i, j int) bool {
		left, leftErr := time.Parse(time.RFC3339Nano, response.Items[i].Timestamp)
		right, rightErr := time.Parse(time.RFC3339Nano, response.Items[j].Timestamp)
		if leftErr == nil && rightErr == nil {
			return left.After(right)
		}
		return response.Items[i].Timestamp > response.Items[j].Timestamp
	})
	if len(response.Items) > limit {
		response.Items = response.Items[:limit]
	}
	return response, nil
}

func (a *App) clearAuthPoolPluginEvents(ctx context.Context) (clearAuthPoolPluginEventsResponse, error) {
	cfg, err := a.loadConfig(ctx)
	if err != nil {
		return clearAuthPoolPluginEventsResponse{}, err
	}
	targets := authPoolPluginMonitoringTargets(cfg)
	if len(targets) == 0 {
		return clearAuthPoolPluginEventsResponse{}, validationError("CPA URL and management key are required")
	}
	response := clearAuthPoolPluginEventsResponse{Errors: []authPoolPluginEventTargetError{}}
	for _, target := range targets {
		var remote struct {
			Cleared int `json:"cleared"`
		}
		if err := a.authPoolPluginRequestWithTarget(ctx, target, http.MethodDelete, "/events", nil, &remote); err != nil {
			response.Errors = append(response.Errors, authPoolPluginEventTargetError{TargetID: target.ID, TargetName: target.Name, Error: err.Error()})
			continue
		}
		response.Cleared += remote.Cleared
	}
	return response, nil
}

func authPoolPluginMonitoringTargets(cfg AppConfig) []AuthPoolProxyTargetConfig {
	targets := make([]AuthPoolProxyTargetConfig, 0)
	for _, target := range normalizeAuthPoolProxyTargets(cfg.AuthPoolProxyTargets) {
		if !target.Enabled || strings.TrimSpace(target.CPAURL) == "" || strings.TrimSpace(target.ManagementKey) == "" {
			continue
		}
		targets = append(targets, target)
	}
	if len(targets) > 0 {
		return targets
	}
	if target, ok := primaryCPAManagementTarget(cfg); ok {
		return []AuthPoolProxyTargetConfig{target}
	}
	return []AuthPoolProxyTargetConfig{}
}
