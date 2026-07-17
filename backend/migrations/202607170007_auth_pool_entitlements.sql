-- +goose Up
ALTER TABLE auth_pools
  ADD COLUMN resolved_auth_ids_json TEXT NOT NULL DEFAULT '[]';

CREATE TABLE IF NOT EXISTS auth_pool_entitlements (
  pool_id VARCHAR(120) NOT NULL,
  user_id INTEGER NOT NULL,
  created_at DATETIME NOT NULL,
  PRIMARY KEY (pool_id, user_id),
  FOREIGN KEY(pool_id) REFERENCES auth_pools(id) ON DELETE CASCADE,
  FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_auth_pool_entitlements_user_id
  ON auth_pool_entitlements(user_id);

-- +goose Down
DROP INDEX IF EXISTS idx_auth_pool_entitlements_user_id;
DROP TABLE IF EXISTS auth_pool_entitlements;
ALTER TABLE auth_pools DROP COLUMN resolved_auth_ids_json;
