-- +goose Up
ALTER TABLE auth_pools
  ADD COLUMN visibility VARCHAR(32) NOT NULL DEFAULT 'admins_only';

-- Preserve the pre-visibility behavior for pools that already had explicit
-- user entitlements.
UPDATE auth_pools
SET visibility = 'selected_users'
WHERE EXISTS (
  SELECT 1
  FROM auth_pool_entitlements
  WHERE auth_pool_entitlements.pool_id = auth_pools.id
);

-- +goose Down
ALTER TABLE auth_pools DROP COLUMN visibility;
