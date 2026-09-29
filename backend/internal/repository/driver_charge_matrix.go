package repository

import "context"

type ChargeCell struct {
	DriverID    string `json:"driverId"`
	TypeID      string `json:"typeId"`
	WeekStart   string `json:"weekStart"`
	Included    bool   `json:"included"`
	Amount      string `json:"amount"`
	ScheduleID  string `json:"scheduleId"`
	Version     int    `json:"version"`
	TypeVersion int    `json:"typeVersion"`
}

func (r *DriverChargeRepository) SaveCell(ctx context.Context, c ChargeCell, actor string) error {
	if _, err := chargeWeek(c.WeekStart); err != nil {
		return err
	}
	if c.WeekStart < ChargeCurrentWeek() {
		return chargeInvalid("Changes must start in the current or a future week")
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = lockChargeDrivers(ctx, tx, []string{c.DriverID}); err != nil {
		return err
	}
	if err = lockChargeTypes(ctx, tx); err != nil {
		return err
	}
	data, loads, err := chargeData(ctx, tx, c.DriverID)
	if err != nil {
		return err
	}
	var t *ChargeType
	for i := range data.Types {
		if data.Types[i].ID == c.TypeID {
			t = &data.Types[i]
			break
		}
	}
	if t == nil || t.Version != c.TypeVersion {
		return ErrChargeConflict
	}
	var s *ChargeSchedule
	for i := range data.Schedules {
		candidate := &data.Schedules[i]
		if candidate.TypeID != nil && *candidate.TypeID == c.TypeID && candidate.StartWeek <= c.WeekStart && (candidate.EndWeek == nil || *candidate.EndWeek >= c.WeekStart) {
			s = candidate
			break
		}
	}
	if (s == nil && (c.ScheduleID != "" || c.Version != 0)) || (s != nil && (s.ID != c.ScheduleID || s.Version != c.Version)) {
		return ErrChargeConflict
	}
	if c.Included {
		var active bool
		if err = tx.QueryRow(ctx, `SELECT active FROM drivers WHERE id=$1`, c.DriverID).Scan(&active); err != nil {
			return err
		}
		if !active {
			return chargeInvalid("Reactivate this driver before including them in a charge")
		}
		if t.Archived && (s == nil || phaseAt(*s, c.WeekStart).Paused) {
			return chargeInvalid("This charge type is archived")
		}
		if !allowedChargeAmount(t.Amounts, c.Amount) && (s == nil || phaseAt(*s, c.WeekStart).Amount != c.Amount) {
			return chargeInvalid("Choose one of this charge type's amounts")
		}
	}
	if s == nil {
		if !c.Included {
			return tx.Commit(ctx)
		}
		// A future assignment must not be hidden by a new indefinite assignment.
		for _, future := range data.Schedules {
			if future.TypeID != nil && *future.TypeID == t.ID && future.StartWeek > c.WeekStart {
				return chargeInvalid("This driver has an assignment starting %s; choose that effective week", future.StartWeek)
			}
		}
		err = tx.QueryRow(ctx, `INSERT INTO driver_charge_schedules(driver_id,type_id,kind,name,direction,start_week,eligibility) VALUES($1,$2,'recurring',$3,$4,$5::date,'calendar') RETURNING id::text`, c.DriverID, t.ID, t.Name, t.Direction, c.WeekStart).Scan(&c.ScheduleID)
		if err != nil {
			return err
		}
	} else {
		if err = freezeChargesBefore(ctx, tx, s, c.WeekStart, loads[c.DriverID]); err != nil {
			return err
		}
		for _, o := range s.Occurrences {
			if o.WeekStart >= c.WeekStart && (o.Overridden || o.ConfirmedAt != nil) {
				return chargeInvalid("Correct the saved charge in week %s before changing this schedule", o.WeekStart)
			}
		}
		if !c.Included {
			c.Amount = phaseAt(*s, c.WeekStart).Amount
		}
		// Keep later dated changes intact. This cell describes the selected week.
		if _, err = tx.Exec(ctx, `DELETE FROM driver_charge_occurrences WHERE schedule_id=$1 AND week_start >= $2::date AND NOT overridden AND confirmed_at IS NULL`, s.ID, c.WeekStart); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE driver_charge_schedules SET version=version+1 WHERE id=$1`, s.ID); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(ctx, `INSERT INTO driver_charge_phases(schedule_id,week_start,amount,paused) VALUES($1,$2::date,$3::numeric,$4) ON CONFLICT(schedule_id,week_start) DO UPDATE SET amount=EXCLUDED.amount,paused=EXCLUDED.paused`, c.ScheduleID, c.WeekStart, c.Amount, !c.Included); err != nil {
		return err
	}
	if err = chargeAudit(ctx, tx, c.ScheduleID, t.ID, actor, "recurring_selection", c); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
