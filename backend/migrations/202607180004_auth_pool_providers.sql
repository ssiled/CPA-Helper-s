-- +goose Up
ALTER TABLE auth_pools
  ADD COLUMN providers_json TEXT NOT NULL DEFAULT '[]';

-- +goose Down
ALTER TABLE auth_pools DROP COLUMN providers_json;
