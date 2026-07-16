-- +goose Up
CREATE TABLE IF NOT EXISTS channel_status_snapshots (
  pool_id VARCHAR(120) PRIMARY KEY,
  pool_name VARCHAR(240) NOT NULL,
  description TEXT,
  enabled BOOLEAN NOT NULL DEFAULT 1,
  account_types TEXT NOT NULL DEFAULT '[]',
  account_count INTEGER NOT NULL DEFAULT 0,
  available_accounts INTEGER NOT NULL DEFAULT 0,
  disabled_accounts INTEGER NOT NULL DEFAULT 0,
  error_accounts INTEGER NOT NULL DEFAULT 0,
  quota_exhausted_accounts INTEGER NOT NULL DEFAULT 0,
  status VARCHAR(40) NOT NULL,
  available BOOLEAN NOT NULL DEFAULT 0,
  status_code INTEGER,
  primary_remaining_percent INTEGER,
  secondary_remaining_percent INTEGER,
  window_start_at DATETIME NOT NULL,
  window_end_at DATETIME NOT NULL,
  window_records INTEGER NOT NULL DEFAULT 0,
  window_success_records INTEGER NOT NULL DEFAULT 0,
  window_failed_records INTEGER NOT NULL DEFAULT 0,
  window_cost_usd REAL NOT NULL DEFAULT 0,
  last_checked_at DATETIME,
  last_healthy_at DATETIME,
  last_error TEXT,
  refreshed_at DATETIME NOT NULL,
  updated_at DATETIME NOT NULL
);

CREATE INDEX IF NOT EXISTS ix_channel_status_snapshots_status ON channel_status_snapshots(status);
CREATE INDEX IF NOT EXISTS ix_channel_status_snapshots_refreshed_at ON channel_status_snapshots(refreshed_at);

-- +goose Down
DROP TABLE IF EXISTS channel_status_snapshots;
