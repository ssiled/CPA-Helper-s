-- +goose Up
ALTER TABLE auth_pools
  ADD COLUMN scheduling_strategy VARCHAR(32) NOT NULL DEFAULT 'round-robin';

-- +goose Down
ALTER TABLE auth_pools DROP COLUMN scheduling_strategy;
