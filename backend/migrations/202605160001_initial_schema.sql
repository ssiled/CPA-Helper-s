-- +goose Up
CREATE TABLE IF NOT EXISTS app_settings (
	id INTEGER PRIMARY KEY,
	collector_enabled BOOLEAN NOT NULL DEFAULT 0,
	cliaproxy_url VARCHAR(500) NOT NULL DEFAULT 'http://127.0.0.1:8317',
	management_key VARCHAR(1000) NOT NULL DEFAULT '',
	queue_name VARCHAR(120) NOT NULL DEFAULT 'usage',
	batch_size INTEGER NOT NULL DEFAULT 100,
	poll_interval_seconds REAL NOT NULL DEFAULT 2.0,
	retry_interval_seconds REAL NOT NULL DEFAULT 10.0,
	codex_keeper_settings TEXT NOT NULL DEFAULT '{}',
	codex_keeper_priority_rules TEXT NOT NULL DEFAULT '{}',
	session_secret VARCHAR(200) NOT NULL,
	created_at DATETIME NOT NULL,
	updated_at DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS users (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	username VARCHAR(120) NOT NULL UNIQUE,
	password_hash VARCHAR(200),
	password_salt VARCHAR(64),
	is_admin BOOLEAN NOT NULL DEFAULT 0,
	nickname VARCHAR(240) NOT NULL DEFAULT '',
	disabled_at DATETIME,
	created_at DATETIME NOT NULL,
	updated_at DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS user_api_keys (
	api_key_hash VARCHAR(64) PRIMARY KEY,
	user_id INTEGER NOT NULL,
	api_key VARCHAR(400),
	description VARCHAR(240) NOT NULL DEFAULT '',
	created_at DATETIME NOT NULL,
	updated_at DATETIME NOT NULL,
	FOREIGN KEY(user_id) REFERENCES users(id)
);

CREATE TABLE IF NOT EXISTS usage_records (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	created_at DATETIME NOT NULL,
	timestamp DATETIME NOT NULL,
	usage_username VARCHAR(120),
	api_key_description VARCHAR(240),
	provider VARCHAR(120),
	model VARCHAR(180),
	endpoint VARCHAR(240),
	source VARCHAR(120),
	request_id VARCHAR(240),
	auth VARCHAR(120),
	latency_ms REAL,
	failed BOOLEAN NOT NULL DEFAULT 0,
	input_tokens INTEGER NOT NULL DEFAULT 0,
	output_tokens INTEGER NOT NULL DEFAULT 0,
	cached_tokens INTEGER NOT NULL DEFAULT 0,
	reasoning_tokens INTEGER NOT NULL DEFAULT 0,
	total_tokens INTEGER NOT NULL DEFAULT 0,
	dedupe_key VARCHAR(80) NOT NULL UNIQUE,
	raw_json TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS model_prices (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	provider VARCHAR(120) NOT NULL,
	model VARCHAR(180) NOT NULL,
	input_usd_per_million REAL NOT NULL DEFAULT 0,
	output_usd_per_million REAL NOT NULL DEFAULT 0,
	cached_usd_per_million REAL NOT NULL DEFAULT 0,
	reasoning_usd_per_million REAL NOT NULL DEFAULT 0,
	source VARCHAR(40) NOT NULL DEFAULT 'manual',
	source_model VARCHAR(180),
	auto_synced BOOLEAN NOT NULL DEFAULT 0,
	last_synced_at DATETIME,
	updated_at DATETIME NOT NULL,
	CONSTRAINT uq_model_prices_provider_model UNIQUE (provider, model)
);

CREATE TABLE IF NOT EXISTS collector_state (
	id INTEGER PRIMARY KEY,
	running BOOLEAN NOT NULL DEFAULT 0,
	last_poll_at DATETIME,
	last_success_at DATETIME,
	last_error TEXT,
	remote_enabled BOOLEAN,
	records_collected INTEGER NOT NULL DEFAULT 0,
	updated_at DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS codex_keeper_auth_states (
	auth_name VARCHAR(500) PRIMARY KEY,
	email VARCHAR(320),
	account_type VARCHAR(80),
	disabled BOOLEAN NOT NULL DEFAULT 0,
	priority INTEGER,
	restore_priority INTEGER,
	latest_action TEXT,
	last_error TEXT,
	last_status_code INTEGER,
	primary_used_percent INTEGER,
	secondary_used_percent INTEGER,
	primary_reset_at DATETIME,
	secondary_reset_at DATETIME,
	quota_threshold INTEGER,
	last_checked_at DATETIME,
	last_healthy_at DATETIME,
	created_at DATETIME NOT NULL,
	updated_at DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS codex_keeper_runs (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	mode VARCHAR(20) NOT NULL,
	state VARCHAR(20) NOT NULL,
	detail TEXT,
	started_at DATETIME NOT NULL,
	finished_at DATETIME,
	total INTEGER NOT NULL DEFAULT 0,
	healthy INTEGER NOT NULL DEFAULT 0,
	status_disabled INTEGER NOT NULL DEFAULT 0,
	status_enabled INTEGER NOT NULL DEFAULT 0,
	priority_degraded INTEGER NOT NULL DEFAULT 0,
	priority_restored INTEGER NOT NULL DEFAULT 0,
	skipped INTEGER NOT NULL DEFAULT 0,
	network_error INTEGER NOT NULL DEFAULT 0,
	created_at DATETIME NOT NULL,
	updated_at DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS codex_keeper_run_accounts (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	run_id INTEGER NOT NULL,
	auth_name VARCHAR(500) NOT NULL,
	email VARCHAR(320),
	result VARCHAR(40) NOT NULL,
	account_type VARCHAR(80),
	priority INTEGER,
	disabled BOOLEAN,
	keeper_action VARCHAR(40) NOT NULL DEFAULT 'none',
	primary_used_percent INTEGER,
	secondary_used_percent INTEGER,
	quota_threshold INTEGER,
	last_status_code INTEGER,
	last_error TEXT,
	latest_action TEXT,
	checked_at DATETIME NOT NULL,
	created_at DATETIME NOT NULL,
	FOREIGN KEY(run_id) REFERENCES codex_keeper_runs(id)
);

-- +goose Down
DROP TABLE IF EXISTS codex_keeper_run_accounts;
DROP TABLE IF EXISTS codex_keeper_runs;
DROP TABLE IF EXISTS codex_keeper_auth_states;
DROP TABLE IF EXISTS collector_state;
DROP TABLE IF EXISTS model_prices;
DROP TABLE IF EXISTS usage_records;
DROP TABLE IF EXISTS user_api_keys;
DROP TABLE IF EXISTS users;
DROP TABLE IF EXISTS app_settings;
