package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type PayrollSettlement struct {
	Finalized   bool       `json:"finalized"`
	Version     int        `json:"version"`
	FinalizedAt time.Time  `json:"finalizedAt"`
	FinalizedBy string     `json:"finalizedBy"`
	ReopenedAt  *time.Time `json:"reopenedAt"`
	Reason      string     `json:"reason"`
}
type SettlementEvent struct {
	Action    string          `json:"action"`
	Version   int             `json:"version"`
	Actor     string          `json:"actor"`
	Reason    string          `json:"reason"`
	CreatedAt time.Time       `json:"createdAt"`
	Report    DriverPayDriver `json:"report"`
}

func payrollRevision(report DriverPayWeek) string {
	report.Revision = ""
	b, _ := json.Marshal(report)
	hash := sha256.Sum256(b)
	return hex.EncodeToString(hash[:])
}
func overlayPayrollSettlements(ctx context.Context, tx pgx.Tx, report DriverPayWeek, driver string) (DriverPayWeek, error) {
	rows, err := tx.Query(ctx, `SELECT s.driver_id,s.finalized,s.version,s.finalized_at,coalesce(u.username,''),s.reopened_at,s.reason,s.report
 FROM payroll_settlements s LEFT JOIN app_users u ON u.id=s.finalized_by WHERE s.week_start=$1::date AND ($2='' OR s.driver_id::text=$2)`, report.WeekStart, driver)
	if err != nil {
		return report, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var meta PayrollSettlement
		var body []byte
		if err = rows.Scan(&id, &meta.Finalized, &meta.Version, &meta.FinalizedAt, &meta.FinalizedBy, &meta.ReopenedAt, &meta.Reason, &body); err != nil {
			return report, err
		}
		var frozen DriverPayDriver
		if err = json.Unmarshal(body, &frozen); err != nil {
			return report, err
		}
		found := false
		for i := range report.Drivers {
			if report.Drivers[i].ID == id {
				found = true
				if meta.Finalized {
					report.Drivers[i] = frozen
				}
				report.Drivers[i].Settlement = &meta
				break
			}
		}
		if !found && meta.Finalized {
			frozen.Settlement = &meta
			report.Drivers = append(report.Drivers, frozen)
		}
	}
	if err = rows.Err(); err != nil {
		return report, err
	}
	sort.Slice(report.Drivers, func(i, j int) bool {
		a, b := report.Drivers[i], report.Drivers[j]
		if a.FullName == b.FullName {
			return a.ID < b.ID
		}
		return a.FullName < b.FullName
	})
	report.Revision = payrollRevision(report)
	return report, nil
}
func assertPayrollOpen(ctx context.Context, tx pgx.Tx, driver, week string) error {
	var closed bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM payroll_settlements WHERE driver_id=$1 AND week_start=$2::date AND finalized)`, driver, week).Scan(&closed); err != nil {
		return err
	}
	if closed {
		return chargeInvalid("Reopen this driver settlement before editing payroll")
	}
	return nil
}

// The exclusive session lock is acquired before opening the snapshot transaction.
// Ordinary charge writes take its shared counterpart, retaining per-driver concurrency.
func (r *DriverPayRepository) Settle(ctx context.Context, week time.Time, driver, revision, actor, reason string, reopen bool) (DriverPayWeek, error) {
	empty := DriverPayWeek{}
	if week.Format(time.DateOnly) > ChargeCurrentWeek() {
		return empty, chargeInvalid("Future weeks cannot be finalized")
	}
	if reopen && (strings.TrimSpace(reason) == "" || len([]rune(reason)) > 2000) {
		return empty, chargeInvalid("Provide a reason for reopening, up to 2,000 characters")
	}
	conn, err := r.pool.Acquire(ctx)
	if err != nil {
		return empty, err
	}
	defer conn.Release()
	if _, err = conn.Exec(ctx, `SELECT pg_advisory_lock(736281940)`); err != nil {
		return empty, err
	}
	defer func() {
		unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, unlockErr := conn.Exec(unlockCtx, `SELECT pg_advisory_unlock(736281940)`); unlockErr != nil {
			// Never return a connection holding a session lock to the pool.
			_ = conn.Conn().Close(context.Background())
		}
	}()
	tx, err := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return empty, err
	}
	defer tx.Rollback(ctx)
	report, err := readDriverPayWeek(ctx, tx, week, "")
	if err != nil {
		return empty, err
	}
	if revision == "" || revision != report.Revision {
		return empty, ErrDriverPayConflict
	}
	selected := 0
	pending := map[string][]string{}
	for _, d := range report.Drivers {
		if driver != "" && d.ID != driver {
			continue
		}
		closed := d.Settlement != nil && d.Settlement.Finalized
		if closed != reopen {
			continue
		}
		selected++
		if reopen {
			var ids []string
			if err = tx.QueryRow(ctx, `UPDATE payroll_settlements SET finalized=false,version=version+1,reopened_at=now(),reopened_by=nullif($3,'')::uuid,reason=$4 WHERE driver_id=$1 AND week_start=$2::date RETURNING confirmed_schedules`, d.ID, report.WeekStart, actor, strings.TrimSpace(reason)).Scan(&ids); err != nil {
				return empty, err
			}
			for _, id := range ids {
				if _, err = tx.Exec(ctx, `UPDATE driver_charge_occurrences SET confirmed_at=NULL,confirmed_by=NULL,version=version+1 WHERE schedule_id=$1 AND week_start=$2::date`, id, report.WeekStart); err != nil {
					return empty, err
				}
				if _, err = tx.Exec(ctx, `UPDATE driver_charge_schedules SET version=version+1 WHERE id=$1`, id); err != nil {
					return empty, err
				}
				if err = chargeAudit(ctx, tx, id, "", actor, "payroll_reopened", map[string]string{"weekStart": report.WeekStart, "reason": reason}); err != nil {
					return empty, err
				}
			}
		} else {
			if err = saveDriverPayCosts(ctx, tx, d, d.Edits, actor); err != nil {
				return empty, err
			}
			if len(d.Issues) > 0 {
				return empty, chargeInvalid("Resolve settlement issues for %s: %s", d.FullName, strings.Join(d.Issues, "; "))
			}
			for _, load := range d.Loads {
				if load.Fee == "" || len(load.Issues) > 0 {
					return empty, chargeInvalid("Resolve the loads needing review for %s before finalizing", d.FullName)
				}
			}
			deductions := append([]ExpenseDeduction(nil), d.Edits.ExpenseDeductions...)
			for i := range deductions {
				deductions[i].Apply = !deductions[i].Saved
			}
			if _, err = saveExpenseDeductions(ctx, tx, d.ID, report.WeekStart, actor, deductions); err != nil {
				return empty, err
			}
			data, loads, e := chargeData(ctx, tx, d.ID)
			if e != nil {
				return empty, e
			}
			confirmed := []string{}
			for _, row := range d.Edits.GeneratedCharges {
				if row.ConfirmedAt != nil {
					continue
				}
				for i := range data.Schedules {
					if data.Schedules[i].ID == row.ScheduleID {
						if err = freezeChargesBefore(ctx, tx, &data.Schedules[i], report.WeekStart, loads[d.ID]); err != nil {
							return empty, err
						}
					}
				}
				row.Overridden = true // Pin every finalized deduction against later source changes.
				if row.Kind == "installment" && strings.HasPrefix(row.Amount, "-") {
					now := time.Now()
					row.ConfirmedAt = &now
					row.ConfirmedBy = &actor
					confirmed = append(confirmed, row.ScheduleID)
				}
				if err = storeOccurrence(ctx, tx, row); err != nil {
					return empty, err
				}
				if _, err = tx.Exec(ctx, `UPDATE driver_charge_schedules SET version=version+1 WHERE id=$1`, row.ScheduleID); err != nil {
					return empty, err
				}
				if err = chargeAudit(ctx, tx, row.ScheduleID, "", actor, "payroll_finalized", row); err != nil {
					return empty, err
				}
			}
			pending[d.ID] = confirmed
		}
		if !reopen {
			continue
		}
		action := "finalized"
		if reopen {
			action = "reopened"
		}
		if _, err = tx.Exec(ctx, `INSERT INTO payroll_settlement_events(driver_id,week_start,version,action,actor_id,reason,report)
   SELECT driver_id,week_start,version,$3,nullif($4,'')::uuid,$5,report FROM payroll_settlements WHERE driver_id=$1 AND week_start=$2::date`, d.ID, report.WeekStart, action, actor, reason); err != nil {
			return empty, err
		}
	}
	if selected == 0 {
		return empty, chargeInvalid("No eligible driver settlements selected")
	}
	// All selected financial writes are complete under the exclusive payroll
	// lock. Calculate the resulting fleet once, then freeze those exact rows.
	if len(pending) > 0 {
		updated, e := readDriverPayWeek(ctx, tx, week, "")
		if e != nil {
			return empty, e
		}
		for _, d := range updated.Drivers {
			confirmed, ok := pending[d.ID]
			if !ok {
				continue
			}
			frozen := d
			frozen.Settlement = nil
			body, e := json.Marshal(frozen)
			if e != nil {
				return empty, e
			}
			if _, err = tx.Exec(ctx, `INSERT INTO payroll_settlements(driver_id,week_start,report,confirmed_schedules,finalized_by) VALUES($1,$2::date,$3,$4,nullif($5,'')::uuid)
   ON CONFLICT(driver_id,week_start) DO UPDATE SET version=payroll_settlements.version+1,finalized=true,report=excluded.report,confirmed_schedules=excluded.confirmed_schedules,finalized_by=excluded.finalized_by,finalized_at=now(),reopened_at=NULL,reopened_by=NULL,reason=''`, d.ID, report.WeekStart, body, confirmed, actor); err != nil {
				return empty, err
			}
			if _, err = tx.Exec(ctx, `INSERT INTO payroll_settlement_events(driver_id,week_start,version,action,actor_id,reason,report)
 SELECT driver_id,week_start,version,'finalized',nullif($3,'')::uuid,'',report FROM payroll_settlements WHERE driver_id=$1 AND week_start=$2::date`, d.ID, report.WeekStart, actor); err != nil {
				return empty, err
			}
			delete(pending, d.ID)
		}
		if len(pending) != 0 {
			return empty, ErrDriverPayConflict
		}
	}
	result, err := readDriverPayWeek(ctx, tx, week, "")
	if err != nil {
		return empty, err
	}
	return result, tx.Commit(ctx)
}

func (r *DriverPayRepository) SettlementHistory(ctx context.Context, driver, week string) ([]SettlementEvent, error) {
	rows, err := r.pool.Query(ctx, `SELECT e.action,e.version,coalesce(u.username,''),e.reason,e.created_at,e.report FROM payroll_settlement_events e LEFT JOIN app_users u ON u.id=e.actor_id WHERE e.driver_id=$1 AND e.week_start=$2::date ORDER BY e.version DESC`, driver, week)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []SettlementEvent{}
	for rows.Next() {
		var e SettlementEvent
		var body []byte
		if err = rows.Scan(&e.Action, &e.Version, &e.Actor, &e.Reason, &e.CreatedAt, &body); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(body, &e.Report); err != nil {
			return nil, err
		}
		result = append(result, e)
	}
	return result, rows.Err()
}
