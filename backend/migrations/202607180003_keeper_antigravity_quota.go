package migrations

import (
	"context"
	"database/sql"

	"github.com/pressly/goose/v3"
)

func init() {
	goose.AddMigrationNoTxContext(upKeeperAntigravityQuota, nil)
}

func upKeeperAntigravityQuota(ctx context.Context, db *sql.DB) (err error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	cols, err := tableColumns(ctx, tx, "codex_keeper_auth_states")
	if err != nil {
		return err
	}
	if !cols["antigravity_quota_json"] {
		if _, err := tx.ExecContext(ctx, `ALTER TABLE codex_keeper_auth_states ADD COLUMN antigravity_quota_json TEXT`); err != nil {
			return err
		}
	}
	return tx.Commit()
}
