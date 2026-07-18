package app

import (
	"context"
	"testing"
)

func TestLoadConfigCacheReturnsIndependentCopies(t *testing.T) {
	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())
	app, err := NewWithOptions(context.Background(), NewOptions{Migrate: true})
	if err != nil {
		t.Fatalf("NewWithOptions failed: %v", err)
	}
	defer app.Close()

	first, err := app.loadConfig(context.Background())
	if err != nil {
		t.Fatalf("loadConfig failed: %v", err)
	}
	first.CodexKeeper.EnabledProviders[0] = "mutated"
	first.CodexKeeperPriorityRule["plus"] = 99
	first.AuthPoolProxyTargets = append(first.AuthPoolProxyTargets, AuthPoolProxyTargetConfig{CPAURL: "http://mutated"})

	second, err := app.loadConfig(context.Background())
	if err != nil {
		t.Fatalf("loadConfig failed: %v", err)
	}
	if second.CodexKeeper.EnabledProviders[0] == "mutated" || second.CodexKeeperPriorityRule["plus"] == 99 || len(second.AuthPoolProxyTargets) != 0 {
		t.Fatalf("cached config was mutated through a returned copy: %+v", second)
	}
}

func BenchmarkLoadConfigCached(b *testing.B) {
	b.Setenv("CPA_HELPER_DATA_DIR", b.TempDir())
	app, err := NewWithOptions(context.Background(), NewOptions{Migrate: true})
	if err != nil {
		b.Fatalf("NewWithOptions failed: %v", err)
	}
	defer app.Close()
	b.ResetTimer()
	for range b.N {
		if _, err := app.loadConfig(context.Background()); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkLoadConfigUncached(b *testing.B) {
	b.Setenv("CPA_HELPER_DATA_DIR", b.TempDir())
	app, err := NewWithOptions(context.Background(), NewOptions{Migrate: true})
	if err != nil {
		b.Fatalf("NewWithOptions failed: %v", err)
	}
	defer app.Close()
	b.ResetTimer()
	for range b.N {
		app.invalidateConfigCache()
		if _, err := app.loadConfig(context.Background()); err != nil {
			b.Fatal(err)
		}
	}
}
