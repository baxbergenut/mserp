package repository

import (
	"context"
	"github.com/jackc/pgx/v5"
	"strings"
	"time"
)

func generatedForWeek(data ChargeData, loads map[string]map[string]bool, week string) (map[string][]ChargeOccurrence, error) {
	out := map[string][]ChargeOccurrence{}
	for _, s := range data.Schedules {
		rows, err := projectCharges(s, week, loads[s.DriverID], false)
		if err != nil {
			return nil, err
		}
		for _, o := range rows {
			if o.WeekStart == week {
				out[s.DriverID] = append(out[s.DriverID], o)
			}
		}
	}
	return out, nil
}
func saveGeneratedCharges(ctx context.Context, tx pgx.Tx, driver, week, actor string, submitted []ChargeOccurrence) ([]ChargeOccurrence, error) {
	if err := lockChargeTypes(ctx, tx); err != nil {
		return nil, err
	}
	data, loads, err := chargeData(ctx, tx, driver)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, input := range submitted {
		if input.WeekStart != week || seen[input.ScheduleID] {
			return nil, chargeInvalid("Charge rows must have distinct sources and belong to the selected week")
		}
		seen[input.ScheduleID] = true
		var s *ChargeSchedule
		for i := range data.Schedules {
			if data.Schedules[i].ID == input.ScheduleID {
				s = &data.Schedules[i]
				break
			}
		}
		if s == nil || s.Version != input.ScheduleVersion || s.TypeVersion != input.TypeVersion {
			return nil, ErrChargeConflict
		}
		rows, err := projectCharges(*s, week, loads[driver], false)
		if err != nil {
			return nil, err
		}
		var current *ChargeOccurrence
		for i := range rows {
			if rows[i].WeekStart == week {
				current = &rows[i]
				break
			}
		}
		if current == nil || current.Version != input.Version || current.ScheduledAmount != input.ScheduledAmount {
			return nil, ErrChargeConflict
		}
		input.Name = strings.TrimSpace(input.Name)
		if s.Kind == "recurring" && input.Name != current.Name {
			return nil, chargeInvalid("Recurring charge labels cannot be edited in Driver Pay")
		}
		n, err := chargeCents(input.Amount)
		if err != nil || len([]rune(input.Name)) < 1 || len([]rune(input.Name)) > 200 || (s.Kind == "installment" && n > 0) {
			return nil, chargeInvalid("Provide a name and valid amount; installments must be negative or zero")
		}
		input.Amount = chargeMoney(n)
		if !input.Reset && input.Name == current.Name && input.Amount == current.Amount {
			continue
		}
		if current.ConfirmedAt != nil {
			return nil, chargeInvalid("Reopen the confirmed installment before editing it")
		}
		if err = freezeChargesBefore(ctx, tx, s, week, loads[driver]); err != nil {
			return nil, err
		}
		if input.Reset {
			if _, err = tx.Exec(ctx, `DELETE FROM driver_charge_occurrences WHERE schedule_id=$1 AND week_start=$2::date`, s.ID, week); err != nil {
				return nil, err
			}
			kept := []ChargeOccurrence{}
			for _, o := range s.Occurrences {
				if o.WeekStart != week {
					kept = append(kept, o)
				}
			}
			s.Occurrences = kept
		} else {
			o := *current
			o.Name = input.Name
			o.Amount = input.Amount
			o.Overridden = true
			if err = storeOccurrence(ctx, tx, o); err != nil {
				return nil, err
			}
			kept := []ChargeOccurrence{}
			for _, old := range s.Occurrences {
				if old.WeekStart != week {
					kept = append(kept, old)
				}
			}
			s.Occurrences = append(kept, o)
		}
		if _, err = projectCharges(*s, "2100-12-27", loads[driver], false); err != nil {
			return nil, err
		}
		if _, err = tx.Exec(ctx, `UPDATE driver_charge_schedules SET version=version+1 WHERE id=$1`, s.ID); err != nil {
			return nil, err
		}
		action := "weekly_override"
		if input.Reset {
			action = "weekly_reset"
		}
		if err = chargeAudit(ctx, tx, s.ID, "", actor, action, map[string]any{"before": current, "after": input}); err != nil {
			return nil, err
		}
	}
	data, loads, err = chargeData(ctx, tx, driver)
	if err != nil {
		return nil, err
	}
	generated, err := generatedForWeek(data, loads, week)
	if err != nil {
		return nil, err
	}
	out := generated[driver]
	if out == nil {
		out = []ChargeOccurrence{}
	}
	return out, nil
}
func (r *DriverChargeRepository) Confirm(ctx context.Context, c ChargeConfirm, actor string, reopen bool) error {
	if _, err := chargeWeek(c.WeekStart); err != nil {
		return err
	}
	if c.WeekStart > ChargeCurrentWeek() {
		return chargeInvalid("Future deductions cannot be confirmed")
	}
	if len(c.Rows) < 1 || len(c.Rows) > 100 {
		return chargeInvalid("Select 1–100 installment deductions")
	}
	if reopen && (strings.TrimSpace(c.Reason) == "" || len([]rune(c.Reason)) > 2000) {
		return chargeInvalid("Provide a reason for reopening (up to 2,000 characters)")
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = lockChargeDrivers(ctx, tx, []string{c.DriverID}); err != nil {
		return err
	}
	if err = assertPayrollOpen(ctx, tx, c.DriverID, c.WeekStart); err != nil {
		return err
	}
	data, loads, err := chargeData(ctx, tx, c.DriverID)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, input := range c.Rows {
		if seen[input.ScheduleID] || input.WeekStart != c.WeekStart {
			return chargeInvalid("Invalid installment selection")
		}
		seen[input.ScheduleID] = true
		var s *ChargeSchedule
		for i := range data.Schedules {
			if data.Schedules[i].ID == input.ScheduleID {
				s = &data.Schedules[i]
				break
			}
		}
		if s == nil || s.Kind != "installment" {
			return chargeInvalid("Select this driver's installment deductions")
		}
		rows, err := projectCharges(*s, c.WeekStart, loads[c.DriverID], false)
		if err != nil {
			return err
		}
		var current *ChargeOccurrence
		for i := range rows {
			if rows[i].WeekStart == c.WeekStart {
				current = &rows[i]
				break
			}
		}
		if current == nil {
			return ErrChargeConflict
		}
		if !reopen && current.ConfirmedAt != nil && input.Amount == current.Amount && input.Name == current.Name {
			continue
		}
		if s.Version != input.ScheduleVersion || s.TypeVersion != input.TypeVersion || current.Version != input.Version || current.Amount != input.Amount || current.Name != input.Name {
			return ErrChargeConflict
		}
		if reopen && current.ConfirmedAt == nil {
			return chargeInvalid("This installment is not confirmed")
		}
		amount, _ := chargeCents(current.Amount)
		if amount >= 0 {
			return chargeInvalid("Only a nonzero deduction can be confirmed")
		}
		if err = freezeChargesBefore(ctx, tx, s, c.WeekStart, loads[c.DriverID]); err != nil {
			return err
		}
		before := *current
		action := "confirmed"
		if reopen {
			current.ConfirmedAt = nil
			current.ConfirmedBy = nil
			action = "reopened"
		} else {
			now := time.Now()
			current.ConfirmedAt = &now
			current.ConfirmedBy = &actor
		}
		if err = storeOccurrence(ctx, tx, *current); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE driver_charge_schedules SET version=version+1 WHERE id=$1`, s.ID); err != nil {
			return err
		}
		if err = chargeAudit(ctx, tx, s.ID, "", actor, action, map[string]any{"before": before, "after": current, "reason": c.Reason}); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func pauseDriverCharges(ctx context.Context, tx pgx.Tx, driver, week, actor string) error {
	if err := lockChargeTypes(ctx, tx); err != nil {
		return err
	}
	data, loads, err := chargeData(ctx, tx, driver)
	if err != nil {
		return err
	}
	if len(data.Schedules) == 0 {
		return nil
	}
	if _, err = chargeWeek(week); err != nil {
		return chargeInvalid("Choose the Monday when this driver's charges should pause")
	}
	if week < ChargeCurrentWeek() {
		return chargeInvalid("Pause charges in the current or a future week")
	}
	for _, s := range data.Schedules {
		if s.EndWeek != nil && *s.EndWeek < week {
			continue
		}
		effective := week
		if effective < s.StartWeek {
			effective = s.StartWeek
		}
		if err = freezeChargesBefore(ctx, tx, &s, effective, loads[driver]); err != nil {
			return err
		}
		for _, o := range s.Occurrences {
			if o.WeekStart >= effective && (o.Overridden || o.ConfirmedAt != nil) {
				return chargeInvalid("Correct the saved charge in week %s before pausing this driver", o.WeekStart)
			}
		}
		p := phaseAt(s, effective)
		if _, err = tx.Exec(ctx, `DELETE FROM driver_charge_phases WHERE schedule_id=$1 AND week_start >= $2::date`, s.ID, effective); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO driver_charge_phases(schedule_id,week_start,amount,paused) VALUES($1,$2::date,$3::numeric,true)`, s.ID, effective, p.Amount); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `DELETE FROM driver_charge_occurrences WHERE schedule_id=$1 AND week_start >= $2::date AND NOT overridden AND confirmed_at IS NULL`, s.ID, effective); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE driver_charge_schedules SET version=version+1 WHERE id=$1`, s.ID); err != nil {
			return err
		}
		if err = chargeAudit(ctx, tx, s.ID, "", actor, "driver_deactivated", map[string]string{"weekStart": effective}); err != nil {
			return err
		}
	}
	return nil
}
