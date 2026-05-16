package app

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

func TestRunMigrationsCreatesGooseVersionAndFinalSchema(t *testing.T) {
	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())

	app, err := New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer app.Close()

	if !testColumnExists(t, app.db, "usage_records", "usage_username") {
		t.Fatal("usage_records.usage_username was not created")
	}
	if testColumnExists(t, app.db, "usage_records", "api_key_hash") {
		t.Fatal("old usage_records.api_key_hash should not exist")
	}

	var version int64
	if err := app.db.QueryRow(`SELECT MAX(version_id) FROM goose_db_version`).Scan(&version); err != nil {
		t.Fatalf("query goose version: %v", err)
	}
	if version != 202605160002 {
		t.Fatalf("goose version = %d, want 202605160002", version)
	}
}

func TestRunMigrationsRepairsOldPythonSchemaWithoutOldCode(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("CPA_HELPER_DATA_DIR", dataDir)
	dbDir := filepath.Join(dataDir, "db")
	if err := ensureTestDir(dbDir); err != nil {
		t.Fatal(err)
	}

	db, err := sql.Open("sqlite", filepath.Join(dbDir, "cpa_helper.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	apiKey := "sk-old-test"
	apiKeyHash := hashAPIKey(apiKey)
	oldSQL := []string{
		`CREATE TABLE usage_records (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			created_at DATETIME NOT NULL,
			timestamp DATETIME NOT NULL,
			api_key_hash VARCHAR(64) NOT NULL,
			api_key_masked VARCHAR(80) NOT NULL,
			provider VARCHAR(120),
			model VARCHAR(180),
			endpoint VARCHAR(240),
			source VARCHAR(120),
			request_id VARCHAR(240),
			auth VARCHAR(120),
			latency_ms REAL,
			failed BOOLEAN NOT NULL,
			input_tokens INTEGER NOT NULL,
			output_tokens INTEGER NOT NULL,
			cached_tokens INTEGER NOT NULL,
			reasoning_tokens INTEGER NOT NULL,
			total_tokens INTEGER NOT NULL,
			dedupe_key VARCHAR(80) NOT NULL UNIQUE,
			raw_json TEXT NOT NULL
		)`,
		`CREATE TABLE model_prices (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			provider VARCHAR(120) NOT NULL,
			model VARCHAR(180) NOT NULL,
			input_usd_per_million REAL NOT NULL,
			output_usd_per_million REAL NOT NULL,
			cached_usd_per_million REAL NOT NULL,
			reasoning_usd_per_million REAL NOT NULL,
			updated_at DATETIME NOT NULL,
			CONSTRAINT uq_model_prices_provider_model UNIQUE (provider, model)
		)`,
		`CREATE TABLE api_key_aliases (
			api_key_hash VARCHAR(64) PRIMARY KEY,
			alias VARCHAR(120) NOT NULL,
			updated_at DATETIME NOT NULL
		)`,
		`CREATE TABLE collector_state (
			id INTEGER PRIMARY KEY,
			running BOOLEAN NOT NULL,
			last_poll_at DATETIME,
			last_success_at DATETIME,
			last_error TEXT,
			remote_enabled BOOLEAN,
			records_collected INTEGER NOT NULL,
			updated_at DATETIME NOT NULL
		)`,
		`CREATE TABLE alembic_version (
			version_num VARCHAR(32) NOT NULL
		)`,
		`INSERT INTO alembic_version (version_num) VALUES ('20260513_0001')`,
	}
	for _, statement := range oldSQL {
		if _, err := db.Exec(statement); err != nil {
			_ = db.Close()
			t.Fatalf("create old schema: %v", err)
		}
	}
	if _, err := db.Exec(`
		INSERT INTO api_key_aliases (api_key_hash, alias, updated_at)
		VALUES (?, 'alice', '2026-05-04 00:00:00')
	`, apiKeyHash); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		INSERT INTO usage_records (
			created_at, timestamp, api_key_hash, api_key_masked, provider, model,
			endpoint, source, request_id, auth, latency_ms, failed, input_tokens,
			output_tokens, cached_tokens, reasoning_tokens, total_tokens,
			dedupe_key, raw_json
		) VALUES (
			'2026-05-04 00:00:00', '2026-05-04 00:00:00', ?, 'sk...test',
			'openai', 'gpt-test', '/v1/chat/completions', 'queue', 'req-1',
			'bearer', 12.5, 0, 10, 20, 0, 0, 30, 'dedupe-1', ?
		)
	`, apiKeyHash, `{"api_key":"`+apiKey+`","auth":"bearer"}`); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	app, err := New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer app.Close()

	if testColumnExists(t, app.db, "usage_records", "api_key_hash") {
		t.Fatal("old usage_records.api_key_hash should be removed")
	}
	if testTableExists(t, app.db, "api_key_aliases") {
		t.Fatal("old api_key_aliases table should be removed")
	}
	if testTableExists(t, app.db, "alembic_version") {
		t.Fatal("old alembic_version table should be removed")
	}

	var username, storedAPIKey, usageUsername string
	if err := app.db.QueryRow(`SELECT username FROM users WHERE username = 'alice'`).Scan(&username); err != nil {
		t.Fatalf("migrated user not found: %v", err)
	}
	if err := app.db.QueryRow(`SELECT api_key FROM user_api_keys WHERE api_key_hash = ?`, apiKeyHash).Scan(&storedAPIKey); err != nil {
		t.Fatalf("migrated api key binding not found: %v", err)
	}
	if storedAPIKey != apiKey {
		t.Fatalf("stored api key = %q, want %q", storedAPIKey, apiKey)
	}
	if err := app.db.QueryRow(`SELECT usage_username FROM usage_records WHERE dedupe_key = 'dedupe-1'`).Scan(&usageUsername); err != nil {
		t.Fatalf("migrated usage record not found: %v", err)
	}
	if usageUsername != username {
		t.Fatalf("usage username = %q, want %q", usageUsername, username)
	}
}

func testTableExists(t *testing.T, db *sql.DB, table string) bool {
	t.Helper()
	var name string
	err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&name)
	return err == nil
}

func testColumnExists(t *testing.T, db *sql.DB, table, column string) bool {
	t.Helper()
	rows, err := db.Query(`PRAGMA table_info("` + table + `")`)
	if err != nil {
		t.Fatalf("pragma table_info(%s): %v", table, err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, columnType string
		var notNull int
		var defaultValue any
		var pk int
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &pk); err != nil {
			t.Fatal(err)
		}
		if name == column {
			return true
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return false
}

func ensureTestDir(path string) error {
	return os.MkdirAll(path, 0o755)
}
