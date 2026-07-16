package app

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestChannelStatusItemFromHidesIdentityAndMapsStatus(t *testing.T) {
	accountType := "plus"
	email := "secret@example.com"
	name := "auth-secret-file.json"
	okStatus := http.StatusOK
	usedPercent := 20
	item := channelStatusItemFrom(0, keeperAccount{
		Name:                 name,
		Email:                &email,
		AccountType:          &accountType,
		LastStatusCode:       &okStatus,
		PrimaryUsedPercent:   &usedPercent,
		SecondaryUsedPercent: nil,
	}, keeperQuotaWindowUsagePair{Primary: &keeperQuotaWindowUsage{
		Records:          10,
		SuccessRecords:   8,
		FailedRecords:    2,
		EstimatedCostUSD: 0.1234,
	}})

	if item.Name != "plus channel 1" {
		t.Fatalf("Name = %q, want plus channel 1", item.Name)
	}
	if item.Status != "normal" || !item.Available {
		t.Fatalf("status = %q available = %v, want normal true", item.Status, item.Available)
	}
	if item.PrimaryRemainingPercent == nil || *item.PrimaryRemainingPercent != 80 {
		t.Fatalf("primary remaining = %v, want 80", item.PrimaryRemainingPercent)
	}
	if item.WindowRecords != 10 || item.WindowSuccessRecords != 8 || item.WindowFailedRecords != 2 || item.WindowCostUSD != 0.1234 {
		t.Fatalf("window stats = %+v", item)
	}

	payload, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	body := string(payload)
	if strings.Contains(body, name) || strings.Contains(body, email) {
		t.Fatalf("channel status leaked identity in %s", body)
	}
}

func TestChannelStatusItemFromUnavailableStates(t *testing.T) {
	accountType := "team"
	errorStatus := http.StatusInternalServerError
	exhaustedPercent := 100

	tests := []struct {
		name    string
		account keeperAccount
		status  string
	}{
		{
			name: "disabled",
			account: keeperAccount{
				AccountType: &accountType,
				Disabled:    true,
			},
			status: "disabled",
		},
		{
			name: "http error",
			account: keeperAccount{
				AccountType:    &accountType,
				LastStatusCode: &errorStatus,
			},
			status: "error",
		},
		{
			name: "quota exhausted",
			account: keeperAccount{
				AccountType:        &accountType,
				PrimaryUsedPercent: &exhaustedPercent,
			},
			status: "quota_exhausted",
		},
	}

	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			item := channelStatusItemFrom(index, test.account, keeperQuotaWindowUsagePair{})
			if item.Status != test.status || item.Available {
				t.Fatalf("status = %q available = %v, want %q false", item.Status, item.Available, test.status)
			}
		})
	}
}
