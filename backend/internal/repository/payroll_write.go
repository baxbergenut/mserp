package repository

import (
	"context"
	"github.com/jackc/pgx/v5"
	"mserp/internal/db"
	"time"
)

// Acquire the existing financial lock before pinning the input snapshot. This
// makes a save receipt's input revision describe the data actually validated,
// including when a source sync commits while the financial write is running.
func (r *DriverPayRepository) beginPayrollWrite(ctx context.Context) (pgx.Tx, func(), error) {
	conn, err := r.pool.Acquire(ctx)
	if err != nil {
		return nil, nil, err
	}
	cleanup := func() {
		releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, e := conn.Exec(releaseCtx, `SELECT pg_advisory_unlock(736281940)`); e != nil {
			_ = conn.Conn().Close(context.Background())
		}
		conn.Release()
	}
	end := db.Phase(ctx, "financial_lock_wait")
	_, err = conn.Exec(ctx, `SELECT pg_advisory_lock(736281940)`)
	end()
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	tx, err := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	return tx, func() { _ = tx.Rollback(context.Background()); cleanup() }, nil
}
