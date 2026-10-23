package repository

import (
	"context"
	"fmt"
	"time"
)

// Calendar precision determines when a reminder is actionable. A month-only
// estimate expires at month end; unknown returns never establish a deadline.
func leaveReturnDeadline(plan *DriverLeavePlan, status, today string) string {
	if plan == nil || plan.Validate() != nil {
		return ""
	}
	value := ""
	switch status {
	case "home":
		if plan.HomeStart != "" && plan.HomeStart <= today {
			value = plan.HomeEnd
		}
	case "vacation":
		value = plan.VacationReturn
	}
	if len(value) == 7 {
		d, _ := time.Parse("2006-01", value)
		return d.AddDate(0, 1, -1).Format(time.DateOnly)
	}
	return value
}

func (r *CustomTaskRepository) RefreshDriverLeaveTasks(ctx context.Context, now time.Time) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT fleet_status_lock(),pg_advisory_xact_lock(730140)`); err != nil {
		return err
	}
	zone, _ := time.LoadLocation("America/New_York")
	today := now.In(zone).Format(time.DateOnly)
	rows, err := tx.Query(ctx, `SELECT d.id,d.full_name,d.dispatcher_id,CASE WHEN fleet_status_enabled() THEN d.availability_status WHEN b.status IN ('HOME','HOME (YARD)') THEN 'home' WHEN b.status='VACATION' THEN 'vacation' ELSE '' END,b.leave_plan FROM drivers d JOIN driver_board b ON b.driver_id=d.id WHERE d.active`)
	if err != nil {
		return err
	}
	type due struct {
		id, name, status, key string
		dispatcher            *string
	}
	wanted := []due{}
	for rows.Next() {
		var v due
		var plan *DriverLeavePlan
		if err = rows.Scan(&v.id, &v.name, &v.dispatcher, &v.status, &plan); err != nil {
			rows.Close()
			return err
		}
		v.key = leaveReturnDeadline(plan, v.status, today)
		if v.key != "" && v.key < today {
			wanted = append(wanted, v)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	ids := []string{}
	for _, v := range wanted {
		ids = append(ids, v.id)
		title := fmt.Sprintf("%s is overdue back from %s", v.name, map[string]string{"home": "home time", "vacation": "vacation"}[v.status])
		notes := fmt.Sprintf("Expected back: %s. The return date has passed and the driver is still marked %s. Confirm the driver's return, update availability, or extend the return date.", v.key, v.status)
		// A corrected deadline starts a new reminder cycle; unchanged runs never
		// create duplicates or generate task events.
		if _, err = tx.Exec(ctx, `UPDATE system_task_records SET completed_at=now(),completed_by_name='System',outcome='Return plan changed' WHERE kind='driver_leave_return' AND service_driver_id=$1 AND completed_at IS NULL AND leave_key<>$2`, v.id, v.status+":"+v.key); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO system_task_records(id,kind,title,notes,created_at,service_driver_id,leave_dispatcher_id,leave_key) VALUES(gen_random_uuid(),'driver_leave_return',$1,$2,now(),$3,$4,$5) ON CONFLICT(service_driver_id) WHERE kind='driver_leave_return' AND completed_at IS NULL DO UPDATE SET title=EXCLUDED.title,notes=EXCLUDED.notes,leave_dispatcher_id=EXCLUDED.leave_dispatcher_id WHERE (system_task_records.title,system_task_records.notes,system_task_records.leave_dispatcher_id) IS DISTINCT FROM (EXCLUDED.title,EXCLUDED.notes,EXCLUDED.leave_dispatcher_id)`, title, notes, v.id, v.dispatcher, v.status+":"+v.key)
		if err != nil {
			return err
		}
	}
	_, err = tx.Exec(ctx, `UPDATE system_task_records SET completed_at=now(),completed_by_name='System',outcome='Driver returned or return plan is no longer overdue' WHERE kind='driver_leave_return' AND completed_at IS NULL AND NOT(service_driver_id=ANY($1::uuid[]))`, ids)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
