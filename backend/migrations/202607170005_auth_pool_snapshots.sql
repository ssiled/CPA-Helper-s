-- +goose Up
CREATE TABLE IF NOT EXISTS auth_pools (
  id VARCHAR(120) PRIMARY KEY,
  name VARCHAR(180) NOT NULL,
  description TEXT,
  auth_ids_json TEXT NOT NULL DEFAULT '[]',
  account_types_json TEXT NOT NULL DEFAULT '[]',
  models_json TEXT NOT NULL DEFAULT '[]',
  enabled BOOLEAN NOT NULL DEFAULT 1,
  created_at DATETIME NOT NULL,
  updated_at DATETIME NOT NULL
);

-- +goose Down
DROP TABLE IF EXISTS auth_pools;
