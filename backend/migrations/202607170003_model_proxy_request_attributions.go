package migrations

import (
	"context"
	"database/sql"

	"github.com/pressly/goose/v3"
)

func init() {
	goose.AddMigrationNoTxContext(upModelProxyRequestAttributions, nil)
}

func upModelProxyRequestAttributions(ctx context.Context, db *sql.DB) (err error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	statements := []string{
		`CREATE TABLE IF NOT EXISTS model_proxy_request_attributions (
			request_id VARCHAR(240) PRIMARY KEY,
			api_key_hash VARCHAR(64) NOT NULL,
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS ix_model_proxy_request_attributions_api_key_hash ON model_proxy_request_attributions(api_key_hash)`,
		`CREATE INDEX IF NOT EXISTS ix_model_proxy_request_attributions_created_at ON model_proxy_request_attributions(created_at)`,
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return tx.Commit()
}
