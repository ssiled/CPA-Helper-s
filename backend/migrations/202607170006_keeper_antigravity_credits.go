package migrations

import (
	"context"
	"database/sql"

	"github.com/pressly/goose/v3"
)

func init() {
	goose.AddMigrationNoTxContext(upKeeperAntigravityCredits, nil)
}

func upKeeperAntigravityCredits(ctx context.Context, db *sql.DB) (err error) {
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
	if !cols["credits_amount"] {
		if _, err := tx.ExecContext(ctx, `ALTER TABLE codex_keeper_auth_states ADD COLUMN credits_amount REAL`); err != nil {
			return err
		}
	}
	if !cols["credits_minimum_amount"] {
		if _, err := tx.ExecContext(ctx, `ALTER TABLE codex_keeper_auth_states ADD COLUMN credits_minimum_amount REAL`); err != nil {
			return err
		}
	}
	if !cols["credits_tier_id"] {
		if _, err := tx.ExecContext(ctx, `ALTER TABLE codex_keeper_auth_states ADD COLUMN credits_tier_id VARCHAR(180)`); err != nil {
			return err
		}
	}
	return tx.Commit()
}
