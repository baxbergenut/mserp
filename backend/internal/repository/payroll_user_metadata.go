package repository

import (
	"context"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// The database validates every row before allowing this gate-free metadata path.
func beginPayrollUserMetadata(ctx context.Context, pool *pgxpool.Pool) (pgx.Tx, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, "SELECT set_config('mserp.payroll.user_metadata','on',true)"); err != nil {
		_ = tx.Rollback(ctx)
		return nil, err
	}
	return tx, nil
}
