package app

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestBuildChannelStatusItemsUsesPoolsAndHidesAccounts(t *testing.T) {
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, appTimeLocation)
	plusType := "plus"
	teamType := "team"
	secretName := "secret-plus-auth.json"
	secretEmail := "secret@example.com"
	okStatus := http.StatusOK
	errorStatus := http.StatusUnauthorized
	usedPercent := 20
	exhaustedPercent := 100

	items := buildChannelStatusItems([]authPool{
		{ID: "plus-pool", Name: "Plus pool", AccountTypes: []string{"plus"}, Enabled: true},
		{ID: "team-pool", Name: "Team pool", AuthIDs: []string{"team-auth.json"}, Enabled: true},
	}, []keeperAccount{
		{Name: secretName, Email: &secretEmail, AccountType: &plusType, LastStatusCode: &okStatus, PrimaryUsedPercent: &usedPercent},
		{Name: "plus-exhausted.json", AccountType: &plusType, PrimaryUsedPercent: &exhaustedPercent},
		{Name: "team-auth.json", AccountType: &teamType, LastStatusCode: &errorStatus},
	}, []UsageRecord{
		{SourceAccount: &secretEmail, Provider: chStringPtr("openai"), Model: chStringPtr("gpt-test"), InputTokens: 1_000_000, OutputTokens: 500_000, RawJSON: `{}`},
		{AuthIndex: chStringPtr("team-auth.json"), Failed: true, RawJSON: `{}`},
	}, map[[2]string]ModelPrice{
		priceKey("openai", "gpt-test"): {Provider: "openai", Model: "gpt-test", InputUSDPerMillion: 2, OutputUSDPerMillion: 4},
	}, now)

	if len(items) != 2 {
		t.Fatalf("items = %d, want 2", len(items))
	}
	plus := findChannelStatusItem(t, items, "plus-pool")
	if plus.Name != "Plus pool" || plus.AccountCount != 2 || plus.AvailableAccounts != 1 {
		t.Fatalf("plus snapshot = %+v", plus)
	}
	if plus.Status != "degraded" || !plus.Available {
		t.Fatalf("plus status = %q available = %v, want degraded true", plus.Status, plus.Available)
	}
	if plus.WindowRecords != 1 || plus.WindowSuccessRecords != 1 || plus.WindowFailedRecords != 0 || plus.WindowCostUSD != 4 {
		t.Fatalf("plus window = records %d success %d failed %d cost %f", plus.WindowRecords, plus.WindowSuccessRecords, plus.WindowFailedRecords, plus.WindowCostUSD)
	}
	if plus.PrimaryRemainingPercent == nil || *plus.PrimaryRemainingPercent != 40 {
		t.Fatalf("plus remaining = %v, want 40", plus.PrimaryRemainingPercent)
	}

	team := findChannelStatusItem(t, items, "team-pool")
	if team.Status != "error" || team.Available || team.WindowFailedRecords != 1 {
		t.Fatalf("team snapshot = %+v", team)
	}

	payload, err := json.Marshal(items)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	body := string(payload)
	for _, secret := range []string{secretName, secretEmail, "team-auth.json"} {
		if strings.Contains(body, secret) {
			t.Fatalf("channel status leaked account identity %q in %s", secret, body)
		}
	}
}

func TestBuildChannelStatusItemsWeightsRemainingByPoolSize(t *testing.T) {
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, appTimeLocation)
	accountType := "plus"
	usedTenPercent := 10
	accounts := make([]keeperAccount, 0, 10)
	for index := 0; index < 10; index++ {
		account := keeperAccount{Name: strings.Repeat("a", index+1), AccountType: &accountType}
		if index == 0 {
			account.PrimaryUsedPercent = &usedTenPercent
		}
		accounts = append(accounts, account)
	}

	items := buildChannelStatusItems([]authPool{
		{ID: "plus-pool", Name: "Plus pool", AccountTypes: []string{"plus"}, Enabled: true},
	}, accounts, nil, nil, now)
	plus := findChannelStatusItem(t, items, "plus-pool")
	if plus.PrimaryRemainingPercent == nil || *plus.PrimaryRemainingPercent != 99 {
		t.Fatalf("plus remaining = %v, want 99", plus.PrimaryRemainingPercent)
	}
}

func TestChannelPoolStatusUnavailableStates(t *testing.T) {
	exhausted := channelStatusItem{Enabled: true, AccountCount: 2, QuotaExhaustedAccounts: 2}
	if status, available := channelPoolStatus(&exhausted); status != "quota_exhausted" || available {
		t.Fatalf("quota status = %q available = %v", status, available)
	}
	empty := channelStatusItem{Enabled: true}
	if status, available := channelPoolStatus(&empty); status != "empty" || available {
		t.Fatalf("empty status = %q available = %v", status, available)
	}
	disabled := channelStatusItem{Enabled: false, AccountCount: 1}
	applyChannelAccountStats(&disabled, []keeperAccount{{Name: "hidden.json"}})
	if disabled.Status != "disabled" || disabled.Available {
		t.Fatalf("disabled status = %q available = %v", disabled.Status, disabled.Available)
	}
}

func findChannelStatusItem(t *testing.T, items []channelStatusItem, id string) channelStatusItem {
	t.Helper()
	for _, item := range items {
		if item.ID == id {
			return item
		}
	}
	t.Fatalf("missing item %s in %+v", id, items)
	return channelStatusItem{}
}

func chStringPtr(value string) *string {
	return &value
}
