package migrations

import (
	"context"
	"database/sql"

	"github.com/pressly/goose/v3"
)

func init() {
	goose.AddMigrationNoTxContext(upModelProxyRequestAttributionMetadata, nil)
}

func upModelProxyRequestAttributionMetadata(ctx context.Context, db *sql.DB) (err error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	cols, err := tableColumns(ctx, tx, "model_proxy_request_attributions")
	if err != nil {
		return err
	}
	if len(cols) == 0 {
		if err := execStatements(ctx, tx, createModelProxyRequestAttributionsTable()); err != nil {
			return err
		}
	} else {
		statements := []struct {
			column string
			sql    string
		}{
			{"model", `ALTER TABLE model_proxy_request_attributions ADD COLUMN model VARCHAR(240)`},
			{"endpoint", `ALTER TABLE model_proxy_request_attributions ADD COLUMN endpoint VARCHAR(240)`},
			{"started_at", `ALTER TABLE model_proxy_request_attributions ADD COLUMN started_at DATETIME`},
			{"completed_at", `ALTER TABLE model_proxy_request_attributions ADD COLUMN completed_at DATETIME`},
			{"status_code", `ALTER TABLE model_proxy_request_attributions ADD COLUMN status_code INTEGER`},
		}
		for _, statement := range statements {
			if !cols[statement.column] {
				if _, err := tx.ExecContext(ctx, statement.sql); err != nil {
					return err
				}
			}
		}
		if _, err := tx.ExecContext(ctx, `UPDATE model_proxy_request_attributions SET started_at = COALESCE(started_at, created_at) WHERE started_at IS NULL`); err != nil {
			return err
		}
	}
	if err := execStatements(ctx, tx,
		`CREATE INDEX IF NOT EXISTS ix_model_proxy_request_attributions_api_key_hash ON model_proxy_request_attributions(api_key_hash)`,
		`CREATE INDEX IF NOT EXISTS ix_model_proxy_request_attributions_created_at ON model_proxy_request_attributions(created_at)`,
		`CREATE INDEX IF NOT EXISTS ix_model_proxy_request_attributions_started_at ON model_proxy_request_attributions(started_at)`,
		`CREATE INDEX IF NOT EXISTS ix_model_proxy_request_attributions_model_endpoint ON model_proxy_request_attributions(model, endpoint)`,
	); err != nil {
		return err
	}
	return tx.Commit()
}

func createModelProxyRequestAttributionsTable() string {
	return `CREATE TABLE IF NOT EXISTS model_proxy_request_attributions (
		request_id VARCHAR(240) PRIMARY KEY,
		api_key_hash VARCHAR(64) NOT NULL,
		model VARCHAR(240),
		endpoint VARCHAR(240),
		started_at DATETIME,
		completed_at DATETIME,
		status_code INTEGER,
		created_at DATETIME NOT NULL,
		updated_at DATETIME NOT NULL
	)`
}
