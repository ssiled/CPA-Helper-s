package app

import (
	"net/http"
	"strconv"
	"time"
)

type channelStatusResponse struct {
	Items       []channelStatusItem `json:"items"`
	RefreshedAt string              `json:"refreshed_at"`
}

type channelStatusItem struct {
	ID                        string  `json:"id"`
	Name                      string  `json:"name"`
	Provider                  string  `json:"provider"`
	AccountType               string  `json:"account_type"`
	Status                    string  `json:"status"`
	Available                 bool    `json:"available"`
	StatusCode                *int    `json:"status_code,omitempty"`
	EndpointPingMS            *int    `json:"endpoint_ping_ms,omitempty"`
	PrimaryRemainingPercent   *int    `json:"primary_remaining_percent,omitempty"`
	SecondaryRemainingPercent *int    `json:"secondary_remaining_percent,omitempty"`
	WindowRecords             int     `json:"window_records"`
	WindowSuccessRecords      int     `json:"window_success_records"`
	WindowFailedRecords       int     `json:"window_failed_records"`
	WindowCostUSD             float64 `json:"window_cost_usd"`
	LastCheckedAt             *string `json:"last_checked_at,omitempty"`
	LastHealthyAt             *string `json:"last_healthy_at,omitempty"`
}

func (a *App) handleChannelStatus(w http.ResponseWriter, r *http.Request) error {
	if err := requireMethod(r, http.MethodGet); err != nil {
		return err
	}
	if _, err := a.currentUser(r.Context(), r); err != nil {
		return err
	}
	accounts, err := a.listKeeperAccounts(r.Context())
	if err != nil {
		return err
	}
	windowUsages, err := a.keeperQuotaWindowUsages(r.Context(), accounts)
	if err != nil {
		return err
	}
	items := make([]channelStatusItem, 0, len(accounts))
	for index, account := range accounts {
		items = append(items, channelStatusItemFrom(index, account, windowUsages[account.Name]))
	}
	writeJSON(w, http.StatusOK, channelStatusResponse{Items: items, RefreshedAt: apiDateTime(time.Now())})
	return nil
}

func channelStatusItemFrom(index int, account keeperAccount, usage keeperQuotaWindowUsagePair) channelStatusItem {
	status := "normal"
	available := true
	if account.Disabled {
		status = "disabled"
		available = false
	} else if account.LastStatusCode != nil && *account.LastStatusCode >= 400 {
		status = "error"
		available = false
	} else if isQuotaExhaustedPercent(account.PrimaryUsedPercent) || isQuotaExhaustedPercent(account.SecondaryUsedPercent) {
		status = "quota_exhausted"
		available = false
	}
	windowRecords, windowSuccess, windowFailed, windowCost := channelWindowStats(usage)
	return channelStatusItem{
		ID:                        "channel-" + strconv.Itoa(index+1),
		Name:                      channelDisplayName(index, account),
		Provider:                  "CPA",
		AccountType:               channelStringValue(account.AccountType, "unknown"),
		Status:                    status,
		Available:                 available,
		StatusCode:                account.LastStatusCode,
		EndpointPingMS:            nil,
		PrimaryRemainingPercent:   remainingPercent(account.PrimaryUsedPercent),
		SecondaryRemainingPercent: remainingPercent(account.SecondaryUsedPercent),
		WindowRecords:             windowRecords,
		WindowSuccessRecords:      windowSuccess,
		WindowFailedRecords:       windowFailed,
		WindowCostUSD:             windowCost,
		LastCheckedAt:             apiDateTimePtr(account.LastCheckedAt),
		LastHealthyAt:             apiDateTimePtr(account.LastHealthyAt),
	}
}

func channelDisplayName(index int, account keeperAccount) string {
	accountType := channelStringValue(account.AccountType, "unknown")
	return accountType + " channel " + strconv.Itoa(index+1)
}

func channelWindowStats(usage keeperQuotaWindowUsagePair) (int, int, int, float64) {
	window := usage.Primary
	if window == nil {
		window = usage.Secondary
	}
	if window == nil {
		return 0, 0, 0, 0
	}
	return window.Records, window.SuccessRecords, window.FailedRecords, window.EstimatedCostUSD
}

func isQuotaExhaustedPercent(value *int) bool {
	return value != nil && *value >= 100
}

func remainingPercent(used *int) *int {
	if used == nil {
		return nil
	}
	remaining := 100 - *used
	if remaining < 0 {
		remaining = 0
	}
	return &remaining
}

func channelStringValue(value *string, fallback string) string {
	if value == nil || *value == "" {
		return fallback
	}
	return *value
}
