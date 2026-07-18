package app

import (
	"testing"
	"time"
)

var benchmarkChannelStatusItems []channelStatusItem

func benchmarkChannelStatusFixture(count int) ([]authPool, []keeperAccount, []UsageRecord, time.Time) {
	now := time.Date(2026, 7, 18, 12, 0, 0, 0, appTimeLocation)
	authID := "benchmark-auth.json"
	records := make([]UsageRecord, 0, count)
	for index := 0; index < count; index++ {
		records = append(records, UsageRecord{
			ID:        index + 1,
			Timestamp: now.Add(-time.Duration(count-index) * time.Second),
			AuthIndex: chStringPtr(authID),
			Failed:    index%10 == 0,
			RawJSON:   `{}`,
		})
	}
	return []authPool{{ID: "benchmark-pool", Name: "Benchmark", AuthIDs: []string{authID}, Enabled: true}}, []keeperAccount{{Name: authID}}, records, now
}

func BenchmarkChannelStatusFullRebuild20K(b *testing.B) {
	pools, accounts, records, now := benchmarkChannelStatusFixture(20_000)
	b.ResetTimer()
	for range b.N {
		benchmarkChannelStatusItems = buildChannelStatusItems(pools, accounts, records, nil, now)
	}
}

func BenchmarkChannelStatusIncremental20(b *testing.B) {
	pools, accounts, records, now := benchmarkChannelStatusFixture(20_000)
	items := buildChannelStatusItems(pools, accounts, records, nil, now)
	cache := newChannelStatusUsageCache(items, "members", "prices", now.Add(-channelStatusWindowDuration), len(records))
	expired := append([]UsageRecord(nil), records[:10]...)
	added := make([]UsageRecord, 0, 10)
	for index := 0; index < 10; index++ {
		added = append(added, UsageRecord{
			ID:        len(records) + index + 1,
			Timestamp: now.Add(time.Duration(index+1) * time.Second),
			AuthIndex: chStringPtr("benchmark-auth.json"),
			RawJSON:   `{}`,
		})
	}
	nextNow := now.Add(10 * time.Second)
	b.ResetTimer()
	for range b.N {
		result, _, err := buildIncrementalChannelStatusItemsContext(b.Context(), pools, accounts, expired, added, nil, nextNow, cache)
		if err != nil {
			b.Fatal(err)
		}
		benchmarkChannelStatusItems = result
	}
}
