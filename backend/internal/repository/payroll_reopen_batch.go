package repository

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/jackc/pgx/v5"
)

// Reopening changes confirmations and metadata, never collections. All selected
// rows were validated under the command's source/resource fences. Apply their
// transitions together while retaining every original individual audit record.
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
		detail, err := json.Marshal(map[string]string{"weekStart": week, "reason": reason})
		if err != nil {
			return err
		}
		// Retain repeated schedule confirmations if present in historical data:
		// versions advance once and an audit remains for every original entry.
		_, err = tx.Exec(ctx, `WITH counts AS MATERIALIZED (
 SELECT id,count(*)::integer count FROM unnest($1::uuid[]) schedule(id) GROUP BY id
 ), occurrences AS (
 UPDATE driver_charge_occurrences o SET confirmed_at=NULL,confirmed_by=NULL,version=o.version+c.count
 FROM counts c WHERE o.schedule_id=c.id AND o.week_start=$2::date RETURNING o.schedule_id
 ), schedules AS (
 UPDATE driver_charge_schedules s SET version=s.version+c.count FROM counts c WHERE s.id=c.id RETURNING s.id
 ) INSERT INTO driver_charge_events(schedule_id,type_id,actor_id,action,details)
 SELECT id,NULL,nullif($3,'')::uuid,'payroll_reopened',$4::jsonb
 FROM unnest($1::uuid[]) WITH ORDINALITY schedule(id,position) ORDER BY position`, schedules, week, actor, detail)
		if err != nil {
			return err
		}
	}
	_, err = tx.Exec(ctx, `INSERT INTO payroll_settlement_events(driver_id,week_start,version,action,actor_id,reason,report)
 SELECT driver_id,week_start,version,'reopened',nullif($3,'')::uuid,$4,report
 FROM payroll_settlements WHERE week_start=$1::date AND driver_id=ANY($2::uuid[])
 ORDER BY array_position($2::uuid[],driver_id)`, week, ids, actor, reason)
	return err
}

func reopenPreparedInvestorWeek(ctx context.Context, tx pgx.Tx, report DriverPayWeek, actor, reason string, cards []DriverPayDriver) error {
	ids := make([]string, 0, len(cards))
	type audit struct {
		Truck   string         `json:"truck"`
		Details map[string]any `json:"details"`
	}
	audits := make([]audit, 0, len(cards))
	for _, card := range cards {
		ids = append(ids, card.ID)
		audits = append(audits, audit{card.ID, map[string]any{"reason": reason, "override": false, "issues": report.Issues, "report": card}})
	}
	body, err := json.Marshal(audits)
	if err != nil {
		return err
	}
	changed, err := tx.Exec(ctx, `UPDATE investor_pay_weeks SET finalized=false,version=version+1
 WHERE week_start=$1::date AND truck_id=ANY($2::uuid[]) AND finalized`, report.WeekStart, ids)
	if err != nil {
		return err
	}
	if changed.RowsAffected() != int64(len(ids)) {
		return ErrDriverPayConflict
	}
	_, err = tx.Exec(ctx, `INSERT INTO truck_settlement_events(truck_id,week_start,action,actor_id,details)
 SELECT (entry->>'truck')::uuid,$2::date,'reopened',nullif($3,'')::uuid,entry->'details'
 FROM jsonb_array_elements($1::jsonb) WITH ORDINALITY event(entry,position) ORDER BY position`, body, report.WeekStart, actor)
	return err
}
