package repository

import (
	"context"
	"github.com/jackc/pgx/v5"
)

// Finalized net shortfalls belong to the same truck and investor. Drafts never
// consume debt, and current ownership changes never transfer it to a new payee.
func applyInvestorSalaryCarry(ctx context.Context, tx pgx.Tx, report *DriverPayWeek) error {
	rows, err := tx.Query(ctx, `SELECT s.week_start::text,s.owner_id::text,s.report
 FROM trucks t CROSS JOIN LATERAL (
 SELECT week_start,owner_id,report FROM investor_pay_weeks
 WHERE truck_id=t.id AND finalized ORDER BY week_start DESC LIMIT 1
 ) s WHERE s.week_start<$1::date
 AND ($2::uuid[] IS NULL OR t.id=ANY($2::uuid[]))
 AND t.owner_id=s.owner_id ORDER BY t.id`, report.WeekStart, payrollTruckIDs(ctx))
	if err != nil {
		return err
	}
	type balance struct {
		week, owner string
		source      DriverPayDriver
	}
	var balances []balance
	for rows.Next() {
		var b balance
		if err = rows.Scan(&b.week, &b.owner, &b.source); err != nil {
			rows.Close()
			return err
		}
		if b.source.NetPayCents() < 0 {
			balances = append(balances, b)
		}
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	for _, b := range balances {
		index := -1
		for i, d := range report.Drivers {
			if d.ID == b.source.ID {
				index = i
				break
			}
		}
		if index < 0 {
			// Debt-only weeks must not replay historical loads, fees or credits.
			d := DriverPayDriver{ID: b.source.ID, TruckID: b.source.ID, InvestorID: b.owner,
				FullName: b.source.FullName, TruckUnit: b.source.TruckUnit, IsOwnerOperator: true,
				PayType: "gross_percentage", PayRate: b.source.PayRate, Loads: []DriverPayLoad{}, FuelTotal: "0.00", TollTotal: "0.00",
				Edits: DriverPayEdits{DriverID: b.source.ID, WeekStart: report.WeekStart, Comments: map[string]string{}, Adjustments: []DriverPayAdjustment{}}}
			if err = readInvestorEdits(ctx, tx, &d); err != nil {
				return err
			}
			report.Drivers = append(report.Drivers, d)
			index = len(report.Drivers) - 1
		}
		d := &report.Drivers[index]
		if d.InvestorID != b.owner || (d.Settlement != nil && d.Settlement.Finalized) {
			continue
		}
		d.SalaryCarry = chargeMoney(-b.source.NetPayCents())
		d.SalaryCarryWeek = b.week
	}
	return nil
}

func guardInvestorSalaryCarryOrder(ctx context.Context, tx pgx.Tx, d DriverPayDriver, week string) error {
	if d.SalaryCarry == "" && d.NetPayCents() >= 0 {
		return nil
	}
	var later string
	if err := tx.QueryRow(ctx, `SELECT coalesce(min(week_start)::text,'') FROM investor_pay_weeks
 WHERE truck_id=$1::uuid AND owner_id=$2::uuid AND week_start>$3::date AND finalized`, d.ID, d.InvestorID, week).Scan(&later); err != nil {
		return err
	}
	if later != "" {
		return chargeInvalid("Reopen %s's later statement for week %s before changing this salary balance", d.FullName, later)
	}
	return nil
}

func investorSalaryCarryAffectsOtherWeeks(ctx context.Context, tx pgx.Tx, weeks, roots []string) (bool, error) {
	rows, err := tx.Query(ctx, `SELECT s.report FROM investor_pay_weeks s
 WHERE s.week_start::text=ANY($1::text[]) AND ('truck:'||s.truck_id::text)=ANY($2::text[])
 UNION ALL SELECT p.report FROM investor_pay_weeks s CROSS JOIN LATERAL (
 SELECT report FROM investor_pay_weeks WHERE truck_id=s.truck_id AND owner_id=s.owner_id
 AND week_start<s.week_start AND finalized ORDER BY week_start DESC LIMIT 1
 ) p WHERE s.week_start::text=ANY($1::text[]) AND ('truck:'||s.truck_id::text)=ANY($2::text[])`, weeks, roots)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var d DriverPayDriver
		if err = rows.Scan(&d); err != nil {
			return false, err
		}
		if d.SalaryCarry != "" || d.NetPayCents() < 0 {
			return true, nil
		}
	}
	return false, rows.Err()
}
