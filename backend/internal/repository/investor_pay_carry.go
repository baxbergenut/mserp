package repository

import (
	"context"
	"github.com/jackc/pgx/v5"
)

// Only the latest finalized statement owns an outstanding balance. Drafts
// display it without consuming it; a paid finalization closes that balance.
func applyInvestorCarry(ctx context.Context, tx pgx.Tx, report *DriverPayWeek) error {
	rows, err := tx.Query(ctx, `SELECT s.week_start::text,s.report FROM (
 SELECT DISTINCT ON (truck_id) truck_id,week_start,report FROM investor_pay_weeks
 WHERE finalized AND ($2::uuid[] IS NULL OR truck_id=ANY($2::uuid[]))
 ORDER BY truck_id,week_start DESC) s
 WHERE s.week_start<$1::date AND coalesce(s.report->>'retainedPay','')<>''`, report.WeekStart, payrollTruckIDs(ctx))
	if err != nil {
		return err
	}
	type balance struct {
		week string
		card DriverPayDriver
	}
	var balances []balance
	for rows.Next() {
		var b balance
		if err = rows.Scan(&b.week, &b.card); err != nil {
			rows.Close()
			return err
		}
		balances = append(balances, b)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	for _, b := range balances {
		amount, err := chargeCents(b.card.RetainedPay)
		if err != nil {
			return err
		}
		if amount <= 0 {
			continue
		}
		index := -1
		for i := range report.Drivers {
			if report.Drivers[i].ID == b.card.ID {
				index = i
				break
			}
		}
		if index < 0 {
			// Keep a retained balance reachable even after terms end or a truck stops
			// operating. Do not replay the old statement's earnings or deductions.
			d := DriverPayDriver{ID: b.card.ID, FullName: b.card.FullName, InvestorID: b.card.InvestorID, TruckID: b.card.TruckID, TruckUnit: b.card.TruckUnit, IsOwnerOperator: true, PayType: b.card.PayType, PayRate: b.card.PayRate, Loads: []DriverPayLoad{}, FuelTotal: "0.00", TollTotal: "0.00", Edits: DriverPayEdits{DriverID: b.card.ID, WeekStart: report.WeekStart, Comments: map[string]string{}, Adjustments: []DriverPayAdjustment{}}}
			if err = readInvestorEdits(ctx, tx, &d); err != nil {
				return err
			}
			report.Drivers = append(report.Drivers, d)
			index = len(report.Drivers) - 1
		}
		d := &report.Drivers[index]
		if d.Settlement != nil && d.Settlement.Finalized {
			continue
		}
		if d.InvestorID != b.card.InvestorID {
			d.Issues = append(d.Issues, "Retained money belongs to the previous investor; reopen and resolve that settlement before finalizing this truck")
			continue
		}
		d.InvestorCarry = chargeMoney(amount)
		d.InvestorCarryWeek = b.week
		d.TruckInactive = false // A payable balance remains available for settlement.
	}
	return nil
}

func guardInvestorCarryOrder(ctx context.Context, tx pgx.Tx, d DriverPayDriver, week string, keep bool) error {
	var prior, later string
	var previousOwner string
	err := tx.QueryRow(ctx, `SELECT coalesce((SELECT report->>'retainedPay' FROM investor_pay_weeks WHERE truck_id=$1 AND week_start<$2::date AND finalized ORDER BY week_start DESC LIMIT 1),''),
 coalesce((SELECT owner_id::text FROM investor_pay_weeks WHERE truck_id=$1 AND week_start<$2::date AND finalized ORDER BY week_start DESC LIMIT 1),''),
 coalesce((SELECT min(week_start)::text FROM investor_pay_weeks WHERE truck_id=$1 AND week_start>$2::date AND finalized),'')`, d.ID, week).Scan(&prior, &previousOwner, &later)
	if err != nil {
		return err
	}
	// This is a financial identity guard, including when review is overridden.
	if prior != "" && previousOwner != d.InvestorID {
		return chargeInvalid("Resolve retained money for the previous investor before finalizing this truck")
	}
	if later != "" && (keep || d.RetainedPay != "" || d.InvestorCarry != "" || prior != "") {
		return chargeInvalid("Reopen the later statement for week %s before changing this retained investor balance", later)
	}
	return nil
}

func investorCarryAffectsOtherWeeks(ctx context.Context, tx pgx.Tx, weeks, roots []string) (bool, error) {
	var affects bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM investor_pay_weeks s
 WHERE s.week_start::text=ANY($1::text[]) AND ('truck:'||s.truck_id::text)=ANY($2::text[])
 AND (coalesce(s.report->>'retainedPay','')<>'' OR coalesce(s.report->>'investorCarry','')<>''
 OR EXISTS(SELECT 1 FROM investor_pay_weeks p WHERE p.truck_id=s.truck_id AND p.week_start<s.week_start AND p.finalized AND coalesce(p.report->>'retainedPay','')<>'')))`, weeks, roots).Scan(&affects)
	return affects, err
}
