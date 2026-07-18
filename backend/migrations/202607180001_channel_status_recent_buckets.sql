-- +goose Up
ALTER TABLE channel_status_snapshots ADD COLUMN recent_window_start_at DATETIME;
ALTER TABLE channel_status_snapshots ADD COLUMN recent_window_end_at DATETIME;
ALTER TABLE channel_status_snapshots ADD COLUMN recent_buckets_json TEXT NOT NULL DEFAULT '[]';

-- +goose Down
ALTER TABLE channel_status_snapshots DROP COLUMN recent_buckets_json;
ALTER TABLE channel_status_snapshots DROP COLUMN recent_window_end_at;
ALTER TABLE channel_status_snapshots DROP COLUMN recent_window_start_at;
