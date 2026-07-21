-- +goose Up
ALTER TABLE app_settings
  ADD COLUMN model_proxy_max_concurrency INTEGER NOT NULL DEFAULT 64;
ALTER TABLE app_settings
  ADD COLUMN model_proxy_queue_size INTEGER NOT NULL DEFAULT 32;
ALTER TABLE app_settings
  ADD COLUMN model_proxy_queue_timeout_ms INTEGER NOT NULL DEFAULT 2000;
ALTER TABLE auth_pools
  ADD COLUMN max_concurrency INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE auth_pools DROP COLUMN max_concurrency;
ALTER TABLE app_settings DROP COLUMN model_proxy_queue_timeout_ms;
ALTER TABLE app_settings DROP COLUMN model_proxy_queue_size;
ALTER TABLE app_settings DROP COLUMN model_proxy_max_concurrency;
