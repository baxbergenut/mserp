package repository

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"mserp/internal/db"
	"time"
)

// Acquire the existing financial lock before pinning the input snapshot. This
// makes a save receipt's input revision describe the data actually validated,
// including when a source sync commits while the financial write is running.
func (r *DriverPayRepository) beginPayrollWrite(ctx context.Context, roots ...string) (pgx.Tx, func(), error) {
	return r.beginPayrollWriteProtocol(ctx, false, "", "", roots...)
}

type payrollResourceTransaction struct {
	pgx.Tx
	resources *payrollResourceSet
	single    bool
	// The shared source gate and validated row guards keep table revisions
	// fixed throughout this transaction. Resource epochs still change on writes.
	sourceFence *string
}

func payrollConcurrent(tx pgx.Tx) bool {
	_, ok := tx.(*payrollResourceTransaction)
	return ok
}

func (r *DriverPayRepository) beginPreparedPayrollWrite(ctx context.Context, actor, request, expected string, roots ...string) (pgx.Tx, func(), error) {
	if request != "" {
		var parsed pgtype.UUID
		if parsed.Scan(request) != nil || !parsed.Valid {
			return nil, nil, chargeInvalid("Invalid request identity")
		}
	}
	return r.beginPayrollWriteProtocol(ctx, r.resourceConcurrency && request != "" && expected != "", actor, request, roots...)
}

func (r *DriverPayRepository) beginPayrollWriteProtocol(ctx context.Context, concurrent bool, actor, request string, roots ...string) (pgx.Tx, func(), error) {
	conn, err := r.pool.Acquire(ctx)
	if err != nil {
		return nil, nil, err
	}
	var releaseResources func()
	lock, unlock := `SELECT pg_advisory_lock(736281940)`, `SELECT pg_advisory_unlock(736281940)`
	if concurrent {
		lock, unlock = `SELECT pg_advisory_lock_shared(736281940)`, `SELECT pg_advisory_unlock_shared(736281940)`
	}
	receiptLocked := false
	cleanup := func() {
		if releaseResources != nil {
			releaseResources()
		}
		releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if receiptLocked {
			if _, e := conn.Exec(releaseCtx, `SELECT pg_advisory_unlock(hashtextextended($1,182))`, actor+":"+request); e != nil {
				_ = conn.Conn().Close(context.Background())
			}
		}
		if _, e := conn.Exec(releaseCtx, unlock); e != nil {
			_ = conn.Conn().Close(context.Background())
		}
		conn.Release()
	}
	end := db.Phase(ctx, "financial_lock_wait")
	_, err = conn.Exec(ctx, lock)
	end()
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	var resources *payrollResourceSet
	replay := false
	if concurrent {
		var protocol string
		if err = conn.QueryRow(ctx, `SELECT protocol_mode FROM payroll_projection_control WHERE singleton`).Scan(&protocol); err != nil || protocol != "resources" {
			cleanup()
			if err == nil {
				err = ErrPayrollResourceChanged
			}
			return nil, nil, err
		}
	}
	if actor != "" && request != "" {
		// Recover a committed receipt before discovery, even if its original
		// component was subsequently removed or capture coverage has changed.
		end := db.Phase(ctx, "receipt_lock_wait")
		_, err = conn.Exec(ctx, `SELECT pg_advisory_lock(hashtextextended($1,182))`, actor+":"+request)
		end()
		if err == nil {
			receiptLocked = true
			err = conn.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM payroll_command_receipts WHERE actor_id=$1::uuid AND request_id=$2::uuid)`, actor, request).Scan(&replay)
		}
		if err != nil {
			cleanup()
			return nil, nil, err
		}
	}
	if (r.resourceLockAudit || concurrent) && !replay {
		discovery, e := conn.Begin(ctx)
		if e != nil {
			cleanup()
			return nil, nil, e
		}
		if len(roots) == 0 || (len(roots) == 1 && roots[0] == "") {
			// Whole-week commands may introduce allocations for any eligible row.
			// The audit phase conservatively claims every principal across weeks.
			roots = nil
			var rows pgx.Rows
			rows, e = discovery.Query(ctx, `SELECT 'driver:'||id FROM drivers UNION SELECT 'truck:'||id FROM trucks UNION SELECT 'owner:'||id FROM investors`)
			if e == nil {
				for rows.Next() {
					var root string
					if e = rows.Scan(&root); e != nil {
						break
					}
					roots = append(roots, root)
				}
				rows.Close()
				if e == nil {
					e = rows.Err()
				}
			}
		}
		if e == nil {
			resources, e = discoverPayrollResources(ctx, discovery, roots)
		}
		_ = discovery.Rollback(ctx)
		if e == nil {
			end := db.Phase(ctx, "resource_lock_wait")
			releaseResources, e = acquirePayrollResources(ctx, conn, resources)
			end()
		}
		if e != nil {
			cleanup()
			return nil, nil, e
		}
	}
	tx, err := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	if resources != nil {
		if err = revalidatePayrollResources(ctx, tx, roots, resources); err != nil {
			_ = tx.Rollback(ctx)
			cleanup()
			return nil, nil, err
		}
		body, e := json.Marshal(resources.keys)
		if e == nil {
			_, e = tx.Exec(ctx, `SELECT set_config('mserp.payroll.resources',$1,true),
 set_config('mserp.payroll.protocol_epoch',(SELECT protocol_epoch::text FROM payroll_projection_control WHERE singleton),true)`, string(body))
		}
		if e != nil {
			_ = tx.Rollback(ctx)
			cleanup()
			return nil, nil, e
		}
	}
	if concurrent && !replay {
		tx = &payrollResourceTransaction{Tx: tx, resources: resources, single: len(roots) == 1}
	}
	return tx, func() { _ = tx.Rollback(context.Background()); cleanup() }, nil
}
