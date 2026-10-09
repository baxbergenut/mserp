package repository

import (
	"context"
	"encoding/json"
	"strings"
)

// Durable review identities are created with the termination transaction. The
// worker materializes only due tasks, including dates missed during downtime.
func (r *CustomTaskRepository) GenerateEscrowTasks(ctx context.Context) error {
	_, err := r.pool.Exec(ctx, `INSERT INTO system_task_records(id,kind,title,notes,created_at)
 SELECT e.id,'escrow_release','Release escrow: '||e.driver_name,
 'Termination: '||e.termination_date::text||E'\nEscrow review due: '||e.due_date::text,
 e.due_date::timestamp AT TIME ZONE 'America/New_York'
 FROM escrow_release_reviews e JOIN drivers d ON d.id=e.driver_id AND d.termination_id=e.id
 WHERE e.completed_at IS NULL AND e.due_date<=(now() AT TIME ZONE 'America/New_York')::date
 ON CONFLICT(id) DO UPDATE SET title=excluded.title,notes=excluded.notes,created_at=excluded.created_at
 WHERE system_task_records.completed_at IS NULL AND
 (system_task_records.title,system_task_records.notes,system_task_records.created_at)
 IS DISTINCT FROM (excluded.title,excluded.notes,excluded.created_at)`)
	return err
}

type EscrowTaskDetail struct {
	Decision        string   `json:"decision"`
	DriverID        string   `json:"driverId"`
	TerminationDate string   `json:"terminationDate"`
	DueDate         string   `json:"dueDate"`
	Escrows         []Escrow `json:"escrows"`
}
type EscrowTaskDecision struct {
	Decision string         `json:"decision"`
	Reason   string         `json:"reason"`
	Versions map[string]int `json:"versions"`
}

func (r *EscrowRepository) TaskDetail(ctx context.Context, id string) (EscrowTaskDetail, error) {
	visible, err := systemTaskVisible(ctx, r.pool, "escrow_release", false)
	if err != nil {
		return EscrowTaskDetail{}, err
	}
	if !visible {
		return EscrowTaskDetail{}, ErrNotFound
	}
	var result EscrowTaskDetail
	err = r.pool.QueryRow(ctx, `SELECT coalesce(e.driver_id::text,''),e.termination_date::text,e.due_date::text
 FROM escrow_release_reviews e JOIN system_task_records s ON s.id=e.id WHERE e.id=$1 AND s.completed_at IS NULL
 AND e.due_date<=(now() AT TIME ZONE 'America/New_York')::date`, id).Scan(&result.DriverID, &result.TerminationDate, &result.DueDate)
	if err != nil {
		return result, mapNotFound(err)
	}
	result.Escrows = []Escrow{}
	if result.DriverID != "" {
		// Return every escrow, without silently truncating the financial decision.
		for page := 1; ; page++ {
			escrows, err := r.List(ctx, EscrowQuery{Pagination: Pagination{Page: page, PageSize: 100}, DriverID: result.DriverID, IncludeInactive: true})
			if err != nil {
				return result, err
			}
			result.Escrows = append(result.Escrows, escrows.Items...)
			if page >= escrows.TotalPages {
				break
			}
		}
	}
	var held, released string
	err = r.pool.QueryRow(ctx, `WITH b AS (`+escrowBalancesSQL+`) SELECT coalesce(sum(held_amount),0)::text,
 coalesce((SELECT sum(r.amount) FROM driver_escrow_releases r JOIN driver_escrows e ON e.id=r.escrow_id WHERE e.driver_id=nullif($1,'')::uuid AND NOT r.cancelled AND (r.created_at AT TIME ZONE 'America/New_York')::date >= $2::date),0)::text FROM b WHERE driver_id=nullif($1,'')::uuid`, result.DriverID, result.TerminationDate).Scan(&held, &released)
	if err != nil {
		return result, err
	}
	h, _ := chargeCents(held)
	v, _ := chargeCents(released)
	result.Decision = escrowReviewDecision(h, v)
	return result, nil
}

func escrowReviewDecision(held, released int64) string {
	if released <= 0 {
		return "kept"
	}
	if held > 0 {
		return "partially_released"
	}
	return "released"
}

// Completion never moves money. It validates actual releases under the same
// driver/escrow locks as financial writes and records the reviewed balances.
func (r *EscrowRepository) CompleteTask(ctx context.Context, id, actor string, input EscrowTaskDecision) error {
	input.Reason = strings.TrimSpace(input.Reason)
	if len([]rune(input.Reason)) > 5000 || strings.ContainsRune(input.Reason, 0) {
		return chargeInvalid("Reason must contain at most 5,000 characters without null characters")
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	visible, err := systemTaskVisible(ctx, tx, "escrow_release", true)
	if err != nil {
		return err
	}
	if !visible {
		return ErrNotFound
	}
	var driver, termination string
	err = tx.QueryRow(ctx, `SELECT coalesce(e.driver_id::text,''),e.termination_date::text FROM escrow_release_reviews e
 JOIN system_task_records s ON s.id=e.id WHERE e.id=$1 AND s.completed_at IS NULL
 AND e.due_date<=(now() AT TIME ZONE 'America/New_York')::date`, id).Scan(&driver, &termination)
	if err != nil {
		return mapNotFound(err)
	}
	if driver != "" {
		if err = lockChargeDrivers(ctx, tx, []string{driver}); err != nil {
			return err
		}
	}
	// Lock review after the driver to match the driver's review trigger order.
	var completed, due bool
	if err = tx.QueryRow(ctx, `SELECT completed_at IS NOT NULL,termination_date::text,due_date<=(now() AT TIME ZONE 'America/New_York')::date FROM escrow_release_reviews WHERE id=$1 FOR UPDATE`, id).Scan(&completed, &termination, &due); err != nil {
		return err
	}
	if completed || !due {
		return ErrEscrowReleaseConflict
	}
	rows, err := tx.Query(ctx, `SELECT id::text,balance_version FROM driver_escrows WHERE driver_id=nullif($1,'')::uuid ORDER BY id FOR UPDATE`, driver)
	if err != nil {
		return err
	}
	count := 0
	for rows.Next() {
		var escrow string
		var version int
		if err = rows.Scan(&escrow, &version); err != nil {
			rows.Close()
			return err
		}
		count++
		if input.Versions[escrow] != version {
			rows.Close()
			return ErrEscrowReleaseConflict
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if count != len(input.Versions) {
		return ErrEscrowReleaseConflict
	}
	var held, released string
	var snapshot []byte
	err = tx.QueryRow(ctx, `WITH b AS (`+escrowBalancesSQL+`)
 SELECT coalesce(sum(held_amount),0)::text,
 coalesce((SELECT sum(r.amount) FROM driver_escrow_releases r JOIN driver_escrows e ON e.id=r.escrow_id
 WHERE e.driver_id=nullif($1,'')::uuid AND NOT r.cancelled AND (r.created_at AT TIME ZONE 'America/New_York')::date >= $2::date),0)::text,
 coalesce(jsonb_agg(jsonb_build_object('escrowId',id,'version',balance_version,'held',held_amount,'released',released_amount)),'[]'::jsonb)
 FROM b WHERE driver_id=nullif($1,'')::uuid`, driver, termination).Scan(&held, &released, &snapshot)
	if err != nil {
		return err
	}
	heldCents, err := chargeCents(held)
	if err != nil {
		return err
	}
	releasedCents, err := chargeCents(released)
	if err != nil {
		return err
	}
	input.Decision = escrowReviewDecision(heldCents, releasedCents)
	if input.Decision != "released" && input.Reason == "" {
		return chargeInvalid("Provide a reason for escrow that is partially released or retained")
	}
	if _, err = tx.Exec(ctx, `UPDATE escrow_release_reviews SET decision=$2,reason=$3,balance_snapshot=$4::jsonb,completed_by=$5,completed_at=now() WHERE id=$1`, id, input.Decision, input.Reason, json.RawMessage(snapshot), actor); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE system_task_records SET completed_at=now(),completed_by_name=coalesce((SELECT username FROM app_users WHERE id=$2),''),outcome=$3 WHERE id=$1`, id, actor, input.Decision+": "+input.Reason)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
