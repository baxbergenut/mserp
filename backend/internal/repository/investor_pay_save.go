package repository

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"strings"
	"time"
)

func (r *DriverPayRepository) SaveInvestorPay(ctx context.Context, edits DriverPayEdits, actor string) (DriverPayEdits, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return edits, err
	}
	defer tx.Rollback(ctx)
	if err = lockTruckAccounting(ctx, tx); err != nil {
		return edits, err
	}
	week, err := chargeWeek(edits.WeekStart)
	if err != nil {
		return edits, err
	}
	report, err := readInvestorPay(ctx, tx, week)
	if err != nil {
		return edits, err
	}
	var card *DriverPayDriver
	for i := range report.Drivers {
		if report.Drivers[i].ID == edits.DriverID {
			card = &report.Drivers[i]
		}
	}
	if card == nil {
		return edits, chargeInvalid("This truck has no investor settlement for this week")
	}
	if card.Settlement != nil && card.Settlement.Finalized {
		return edits, chargeInvalid("Reopen this investor settlement before editing")
	}
	if edits.Version != card.Edits.Version {
		return edits, ErrDriverPayConflict
	}
	if len(edits.GeneratedCharges) > 0 {
		return edits, chargeInvalid("Manage recurring truck fees on Charges")
	}
	if err = validateInvestorCostEdits(*card, edits); err != nil {
		return edits, err
	}
	if err = saveInvestorExpenses(ctx, tx, *card, actor, edits.ExpenseDeductions); err != nil {
		return edits, err
	}
	edits.ExpenseDeductions = nil
	body, err := json.Marshal(edits)
	if err != nil {
		return edits, err
	}
	var version int
	if edits.Version == 0 {
		err = tx.QueryRow(ctx, `INSERT INTO investor_pay_weeks(truck_id,week_start,owner_id,edits) VALUES($1,$2::date,$3,$4) ON CONFLICT DO NOTHING RETURNING version`, card.ID, edits.WeekStart, card.InvestorID, body).Scan(&version)
	} else {
		err = tx.QueryRow(ctx, `UPDATE investor_pay_weeks SET edits=$3,version=version+1,owner_id=$5 WHERE truck_id=$1 AND week_start=$2::date AND version=$4 AND NOT finalized RETURNING version`, card.ID, edits.WeekStart, body, edits.Version, card.InvestorID).Scan(&version)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return edits, ErrDriverPayConflict
	}
	if err != nil {
		return edits, err
	}
	edits.Version = version
	edits.ExpenseDeductions, err = investorExpenseDeductions(ctx, tx, card.ID, card.InvestorID, edits.WeekStart, false)
	if err != nil {
		return edits, err
	}
	if err = truckAudit(ctx, tx, card.ID, edits.WeekStart, "pay_edits", actor, edits); err != nil {
		return edits, err
	}
	return edits, tx.Commit(ctx)
}
func (r *DriverPayRepository) SettleInvestor(ctx context.Context, week time.Time, truck, revision, actor, reason string, reopen bool) (DriverPayWeek, error) {
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
		if _, e := conn.Exec(unlockCtx, `SELECT pg_advisory_unlock(736281940)`); e != nil {
			_ = conn.Conn().Close(context.Background())
		}
	}()
	tx, err := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return empty, err
	}
	defer tx.Rollback(ctx)
	if err = lockTruckAccounting(ctx, tx); err != nil {
		return empty, err
	}
	report, err := readInvestorPay(ctx, tx, week)
	if err != nil {
		return empty, err
	}
	if revision == "" || revision != report.Revision {
		return empty, ErrDriverPayConflict
	}
	if !reopen && len(report.Issues) > 0 {
		return empty, chargeInvalid("Resolve unattributed loads before finalizing Investor Pay")
	}
	count := 0
	for _, d := range report.Drivers {
		if (truck != "" && d.ID != truck) || (truck == "" && d.TruckInactive) {
			continue
		}
		closed := d.Settlement != nil && d.Settlement.Finalized
		if closed != reopen {
			continue
		}
		count++
		if reopen {
			if _, err = tx.Exec(ctx, `UPDATE investor_pay_weeks SET finalized=false,version=version+1 WHERE truck_id=$1 AND week_start=$2::date`, d.ID, report.WeekStart); err != nil {
				return empty, err
			}
		} else {
			if err = validateInvestorCostEdits(d, d.Edits); err != nil {
				return empty, err
			}
			if len(d.Issues) > 0 {
				return empty, chargeInvalid("Resolve %s / %s: %s", d.FullName, d.TruckUnit, strings.Join(d.Issues, "; "))
			}
			for _, l := range d.Loads {
				if l.Fee == "" || len(l.Issues) > 0 {
					return empty, chargeInvalid("Resolve loads needing review for truck %s", d.TruckUnit)
				}
			}
			expenses := append([]ExpenseDeduction(nil), d.Edits.ExpenseDeductions...)
			for i := range expenses {
				expenses[i].Apply = !expenses[i].Saved
			}
			if err = saveInvestorExpenses(ctx, tx, d, actor, expenses); err != nil {
				return empty, err
			}
			d.Edits.ExpenseDeductions, err = investorExpenseDeductions(ctx, tx, d.ID, d.InvestorID, report.WeekStart, false)
			if err != nil {
				return empty, err
			}
			var username string
			if err = tx.QueryRow(ctx, `SELECT username FROM app_users WHERE id=$1`, actor).Scan(&username); err != nil {
				return empty, err
			}
			d.Settlement = &PayrollSettlement{Finalized: true, Version: d.Edits.Version + 1, FinalizedAt: time.Now(), FinalizedBy: username}
			d.Edits.Version++
			body, e := json.Marshal(d)
			if e != nil {
				return empty, e
			}
			edits, e := json.Marshal(d.Edits)
			if e != nil {
				return empty, e
			}
			if _, err = tx.Exec(ctx, `INSERT INTO investor_pay_weeks(truck_id,week_start,owner_id,edits,report,finalized,finalized_at,finalized_by) VALUES($1,$2::date,$3,$4,$5,true,now(),$6) ON CONFLICT(truck_id,week_start) DO UPDATE SET owner_id=excluded.owner_id,edits=excluded.edits,report=excluded.report,finalized=true,version=investor_pay_weeks.version+1,finalized_at=now(),finalized_by=excluded.finalized_by`, d.ID, report.WeekStart, d.InvestorID, edits, body, actor); err != nil {
				return empty, err
			}
		}
		action := "finalized"
		if reopen {
			action = "reopened"
		}
		if err = truckAudit(ctx, tx, d.ID, report.WeekStart, action, actor, map[string]any{"reason": reason, "report": d}); err != nil {
			return empty, err
		}
	}
	if count == 0 {
		return empty, chargeInvalid("No eligible investor settlements selected")
	}
	result, err := readInvestorPay(ctx, tx, week)
	if err != nil {
		return empty, err
	}
	return investorPayView(result, truck), tx.Commit(ctx)
}

func validateInvestorCostEdits(card DriverPayDriver, edits DriverPayEdits) error {
	for _, row := range []struct {
		name, source string
		amount       *string
	}{
		{"Fuel", card.FuelTotal, edits.FuelOverride}, {"Toll", card.TollTotal, edits.TollOverride},
	} {
		if row.amount == nil {
			continue
		}
		due, err := chargeCents(row.source)
		if err != nil {
			return err
		}
		if err = validatePaySourceAmount(row.name, *row.amount, -due); err != nil {
			return err
		}
	}
	return nil
}
