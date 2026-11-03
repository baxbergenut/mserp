package repository

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"
)

// Reopening changes confirmations and metadata, never collections. All selected
// rows were validated under the command's source/resource fences. Apply their
// transitions together without creating action-history snapshots.
func reopenPreparedDriverWeek(ctx context.Context, tx pgx.Tx, week, actor, reason string, ids []string) error {
	rows, err := tx.Query(ctx, `UPDATE payroll_settlements SET finalized=false,version=version+1,
 reopened_at=now(),reopened_by=nullif($3,'')::uuid,reason=$4
 WHERE week_start=$1::date AND driver_id=ANY($2::uuid[]) AND finalized
 RETURNING driver_id::text,confirmed_schedules`, week, ids, actor, strings.TrimSpace(reason))
	if err != nil {
		return err
	}
	confirmed := map[string][]string{}
	for rows.Next() {
		var id string
		var schedules []string
		if err = rows.Scan(&id, &schedules); err != nil {
			rows.Close()
			return err
		}
		confirmed[id] = schedules
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	if len(confirmed) != len(ids) {
		return ErrDriverPayConflict
	}
	schedules := []string{}
	for _, id := range ids {
		schedules = append(schedules, confirmed[id]...)
	}
	if len(schedules) > 0 {
		// Retain repeated schedule confirmations if present in historical data:
		// versions advance once for every original entry.
		_, err = tx.Exec(ctx, `WITH counts AS MATERIALIZED (
 SELECT id,count(*)::integer count FROM unnest($1::uuid[]) schedule(id) GROUP BY id
 ), occurrences AS (
 UPDATE driver_charge_occurrences o SET confirmed_at=NULL,confirmed_by=NULL,version=o.version+c.count
 FROM counts c WHERE o.schedule_id=c.id AND o.week_start=$2::date RETURNING o.schedule_id
 ) UPDATE driver_charge_schedules s SET version=s.version+c.count FROM counts c WHERE s.id=c.id`, schedules, week)
		if err != nil {
			return err
		}
	}
	return nil
}

func reopenPreparedInvestorWeek(ctx context.Context, tx pgx.Tx, report DriverPayWeek, cards []DriverPayDriver) error {
	ids := make([]string, 0, len(cards))
	for _, card := range cards {
		ids = append(ids, card.ID)
	}
	changed, err := tx.Exec(ctx, `UPDATE investor_pay_weeks SET finalized=false,version=version+1
 WHERE week_start=$1::date AND truck_id=ANY($2::uuid[]) AND finalized`, report.WeekStart, ids)
	if err != nil {
		return err
	}
	if changed.RowsAffected() != int64(len(ids)) {
		return ErrDriverPayConflict
	}
	return nil
}
