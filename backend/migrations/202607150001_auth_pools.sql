-- +goose Up
CREATE TABLE IF NOT EXISTS user_api_key_pools (
  api_key_hash VARCHAR(64) PRIMARY KEY,
  pool_id VARCHAR(120) NOT NULL,
  created_at DATETIME NOT NULL,
  updated_at DATETIME NOT NULL,
  FOREIGN KEY(api_key_hash) REFERENCES user_api_keys(api_key_hash) ON DELETE CASCADE
);

-- +goose Down
DROP TABLE IF EXISTS user_api_key_pools;
