package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
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
	if plus.Status != "normal" || !plus.Available {
		t.Fatalf("plus status = %q available = %v, want normal true from recent successful requests", plus.Status, plus.Available)
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

func TestBuildChannelStatusItemsCountsOverlappingRecordOncePerPool(t *testing.T) {
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, appTimeLocation)
	authA := "auth-a.json"
	authB := "auth-b.json"
	items := buildChannelStatusItems([]authPool{
		{ID: "pool-a", Name: "Pool A", AuthIDs: []string{authA}, Enabled: true},
		{ID: "pool-b", Name: "Pool B", AuthIDs: []string{authB}, Enabled: true},
	}, []keeperAccount{
		{Name: authA},
		{Name: authB},
	}, []UsageRecord{{
		AuthIndex: chStringPtr(authA),
		RawJSON:   `{"auth_index":"auth-a.json","authName":"auth-b.json"}`,
	}}, nil, now)

	for _, poolID := range []string{"pool-a", "pool-b"} {
		item := findChannelStatusItem(t, items, poolID)
		if item.WindowRecords != 1 || item.WindowSuccessRecords != 1 || item.WindowFailedRecords != 0 {
			t.Fatalf("%s window = records %d success %d failed %d, want 1/1/0", poolID, item.WindowRecords, item.WindowSuccessRecords, item.WindowFailedRecords)
		}
	}
}

func TestBuildChannelStatusItemsBuildsRecentTimeBuckets(t *testing.T) {
	now := time.Date(2026, 7, 18, 12, 0, 0, 0, appTimeLocation)
	authID := "recent-auth.json"
	items := buildChannelStatusItems([]authPool{
		{ID: "recent-pool", Name: "Recent pool", AuthIDs: []string{authID}, Enabled: true},
	}, []keeperAccount{{Name: authID}}, []UsageRecord{
		{Timestamp: now.Add(-50 * time.Minute), AuthIndex: chStringPtr(authID), RawJSON: `{}`},
		{Timestamp: now.Add(-50 * time.Minute), AuthIndex: chStringPtr(authID), Failed: true, RawJSON: `{}`},
		{Timestamp: now.Add(-time.Minute), AuthIndex: chStringPtr(authID), Failed: true, RawJSON: `{}`},
		{Timestamp: now.Add(-2 * time.Hour), AuthIndex: chStringPtr(authID), Failed: true, RawJSON: `{}`},
	}, nil, now)

	item := findChannelStatusItem(t, items, "recent-pool")
	if len(item.RecentBuckets) != channelStatusRecentBucketCount {
		t.Fatalf("recent buckets = %d, want %d", len(item.RecentBuckets), channelStatusRecentBucketCount)
	}
	successes, failures, mixedBuckets := 0, 0, 0
	for _, bucket := range item.RecentBuckets {
		successes += bucket.SuccessRecords
		failures += bucket.FailedRecords
		if bucket.SuccessRecords > 0 && bucket.FailedRecords > 0 {
			mixedBuckets++
		}
	}
	if successes != 1 || failures != 2 || mixedBuckets != 1 {
		t.Fatalf("recent bucket totals = success %d failed %d mixed %d, want 1/2/1", successes, failures, mixedBuckets)
	}
	if item.WindowRecords != 4 || item.WindowFailedRecords != 3 {
		t.Fatalf("7-day totals = records %d failed %d, want 4/3", item.WindowRecords, item.WindowFailedRecords)
	}
}

func TestChannelStatusSnapshotRoundTripsRecentBuckets(t *testing.T) {
	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())
	app, err := NewWithOptions(context.Background(), NewOptions{Migrate: true})
	if err != nil {
		t.Fatalf("NewWithOptions failed: %v", err)
	}
	defer app.Close()

	now := time.Date(2026, 7, 18, 12, 0, 0, 0, appTimeLocation)
	buckets := make([]channelStatusRecentBucket, channelStatusRecentBucketCount)
	buckets[3] = channelStatusRecentBucket{SuccessRecords: 2, FailedRecords: 1}
	item := channelStatusItem{
		ID:                  "roundtrip-pool",
		Name:                "Roundtrip pool",
		Enabled:             true,
		Status:              "degraded",
		Available:           true,
		WindowStartAt:       dbTime(now.Add(-channelStatusWindowDuration)),
		WindowEndAt:         dbTime(now),
		RecentWindowStartAt: dbTime(now.Add(-channelStatusRecentWindowDuration)),
		RecentWindowEndAt:   dbTime(now),
		RecentBuckets:       buckets,
		RefreshedAt:         dbTime(now),
	}
	if err := app.replaceChannelStatusSnapshots(context.Background(), []channelStatusItem{item}); err != nil {
		t.Fatalf("replaceChannelStatusSnapshots failed: %v", err)
	}
	stored, _, err := app.listChannelStatusSnapshots(context.Background())
	if err != nil {
		t.Fatalf("listChannelStatusSnapshots failed: %v", err)
	}
	if len(stored) != 1 || len(stored[0].RecentBuckets) != channelStatusRecentBucketCount {
		t.Fatalf("stored snapshots = %+v", stored)
	}
	if stored[0].RecentBuckets[3].SuccessRecords != 2 || stored[0].RecentBuckets[3].FailedRecords != 1 {
		t.Fatalf("stored recent bucket = %+v", stored[0].RecentBuckets[3])
	}
}

func TestBuildChannelStatusItemsContextStopsWhenCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := buildChannelStatusItemsContext(ctx, nil, nil, []UsageRecord{{RawJSON: `{}`}}, nil, time.Now())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("buildChannelStatusItemsContext error = %v, want context.Canceled", err)
	}
}

func TestChannelStatusRefreshAsyncCoalescesRequests(t *testing.T) {
	runner := &ChannelStatusRunner{
		stop:            make(chan struct{}),
		done:            make(chan struct{}),
		refreshRequests: make(chan string, 1),
	}
	var requests sync.WaitGroup
	for range 100 {
		requests.Add(1)
		go func() {
			defer requests.Done()
			runner.RefreshAsync()
		}()
	}
	requests.Wait()
	if got := len(runner.refreshRequests); got != 1 {
		t.Fatalf("queued refresh requests = %d, want 1", got)
	}
	if reason := <-runner.refreshRequests; reason != "event" {
		t.Fatalf("refresh reason = %q, want event", reason)
	}
	close(runner.stop)
	runner.RefreshAsync()
	if got := len(runner.refreshRequests); got != 0 {
		t.Fatalf("queued refresh requests after stop = %d, want 0", got)
	}
}

func TestChannelStatusWindowRecordsReadsOnlyRequiredFields(t *testing.T) {
	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())
	app, err := NewWithOptions(context.Background(), NewOptions{Migrate: true})
	if err != nil {
		t.Fatalf("NewWithOptions failed: %v", err)
	}
	defer app.Close()

	raw := `{"provider":"openai","model":"gpt-test","request_id":"channel-window","source_account":"source@example.com","auth":"auth-label","auth_index":"auth.json","input_tokens":10,"output_tokens":4,"cached_tokens":3,"cache_read_tokens":2,"cache_creation_tokens":1,"reasoning_tokens":5,"total_tokens":14}`
	if _, created, saveErr := app.saveUsageMessage(context.Background(), []byte(raw)); saveErr != nil || !created {
		t.Fatalf("saveUsageMessage created=%v error=%v", created, saveErr)
	}
	records, err := app.channelStatusWindowRecords(context.Background(), time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("channelStatusWindowRecords failed: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("records = %d, want 1", len(records))
	}
	record := records[0]
	if record.Timestamp.IsZero() {
		t.Fatal("record timestamp was not loaded")
	}
	if stringPtrValue(record.Provider) != "openai" || stringPtrValue(record.Model) != "gpt-test" || stringPtrValue(record.Auth) != "auth-label" || stringPtrValue(record.AuthIndex) != "auth.json" {
		t.Fatalf("record identity fields = %+v", record)
	}
	if record.InputTokens != 10 || record.OutputTokens != 4 || record.CachedTokens != 3 || record.CacheReadTokens != 2 || record.CacheCreationTokens != 1 || record.ReasoningTokens != 5 || record.TotalTokens != 14 {
		t.Fatalf("record token fields = %+v", record)
	}
	keys := normalizedLookup(usageRecordChannelKeys(record))
	if !keys[normalizeChannelKey("source@example.com")] {
		t.Fatalf("record channel keys = %+v, want source@example.com from raw_json", keys)
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

func TestApplyChannelRecentRequestStatusPrefersWindowResults(t *testing.T) {
	allSuccess := channelStatusItem{Enabled: true, AccountCount: 75, AvailableAccounts: 2, QuotaExhaustedAccounts: 73, Status: "degraded", Available: true, WindowRecords: 998, WindowSuccessRecords: 998}
	applyChannelRecentRequestStatus(&allSuccess)
	if allSuccess.Status != "normal" || !allSuccess.Available {
		t.Fatalf("all-success status = %q available = %v, want normal true", allSuccess.Status, allSuccess.Available)
	}

	mixed := channelStatusItem{Enabled: true, AccountCount: 8, AvailableAccounts: 2, QuotaExhaustedAccounts: 6, Status: "degraded", Available: true, WindowRecords: 1201, WindowSuccessRecords: 1200, WindowFailedRecords: 1}
	applyChannelRecentRequestStatus(&mixed)
	if mixed.Status != "degraded" || !mixed.Available {
		t.Fatalf("mixed status = %q available = %v, want degraded true", mixed.Status, mixed.Available)
	}

	mostlyFailed := channelStatusItem{Enabled: true, AccountCount: 8, AvailableAccounts: 2, Status: "normal", Available: true, WindowRecords: 9, WindowSuccessRecords: 4, WindowFailedRecords: 5}
	applyChannelRecentRequestStatus(&mostlyFailed)
	if mostlyFailed.Status != "error" || mostlyFailed.Available {
		t.Fatalf("mostly-failed status = %q available = %v, want error false", mostlyFailed.Status, mostlyFailed.Available)
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
