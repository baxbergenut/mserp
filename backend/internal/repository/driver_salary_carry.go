package repository

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
)

// A finalized statement settles its individual lines in their native ledgers.
// Only the resulting net shortfall becomes payroll debt. Drafts do not add
// debt or consume it, and a newer frozen statement supersedes the prior balance.
func applyDriverSalaryCarry(ctx context.Context, tx pgx.Tx, report *DriverPayWeek) error {
	rows, err := tx.Query(ctx, `SELECT d.id::text,s.report
 FROM drivers d
 CROSS JOIN LATERAL (
  SELECT week_start,report FROM payroll_settlements
  WHERE driver_id=d.id AND finalized ORDER BY week_start DESC LIMIT 1
 ) s
 WHERE ($2::uuid[] IS NULL OR d.id=ANY($2::uuid[]))
 AND s.week_start<$1::date AND payroll_driver_at(d.id,$1::date)=d.id
 ORDER BY d.id`, report.WeekStart, payrollDriverIDs(ctx))
	if err != nil {
		return err
	}
	type balance struct {
		id     string
		amount string
	}
	balances := []balance{}
	for rows.Next() {
		var id string
		var frozen DriverPayDriver
		if err = rows.Scan(&id, &frozen); err != nil {
			rows.Close()
			return err
		}
		if net := frozen.NetPayCents(); net < 0 {
			balances = append(balances, balance{id, chargeMoney(-net)})
		}
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	index := make(map[string]int, len(report.Drivers))
	for i, d := range report.Drivers {
		index[d.ID] = i
	}
	for _, b := range balances {
		if i, ok := index[b.id]; ok {
			report.Drivers[i].SalaryCarry = b.amount
			continue
		}
		d, err := readIdleDriverPayCard(ctx, tx, report.WeekStart, b.id)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		d.SalaryCarry = b.amount
		report.Drivers = append(report.Drivers, d)
	}
	return nil
}

func guardDriverSalaryCarryOrder(ctx context.Context, tx pgx.Tx, d DriverPayDriver, week string) error {
	if d.SalaryCarry == "" && d.NetPayCents() >= 0 {
		return nil
	}
	var later string
	if err := tx.QueryRow(ctx, `SELECT coalesce(min(week_start)::text,'') FROM payroll_settlements
 WHERE driver_id=$1::uuid AND week_start>$2::date AND finalized`, d.ID, week).Scan(&later); err != nil {
		return err
	}
	if later != "" {
		return chargeInvalid("Reopen %s's later statement for week %s before changing this salary balance", d.FullName, later)
	}
	return nil
}

// Reopening a legacy statement can expose a previously superseded shortfall.
// Check both the changed snapshot and the previous frozen balance before using
// the period-local settlement publication path.
func driverSalaryCarryAffectsOtherWeeks(ctx context.Context, tx pgx.Tx, weeks, roots []string) (bool, error) {
	rows, err := tx.Query(ctx, `SELECT s.report FROM payroll_settlements s
 WHERE s.week_start::text=ANY($1::text[]) AND ('driver:'||s.driver_id::text)=ANY($2::text[])
 UNION ALL
 SELECT p.report FROM payroll_settlements s
 CROSS JOIN LATERAL (
  SELECT report FROM payroll_settlements
  WHERE driver_id=s.driver_id AND week_start<s.week_start AND finalized
  ORDER BY week_start DESC LIMIT 1
 ) p
 WHERE s.week_start::text=ANY($1::text[]) AND ('driver:'||s.driver_id::text)=ANY($2::text[])`, weeks, roots)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var body []byte
		if err = rows.Scan(&body); err != nil {
			return false, err
		}
		var frozen DriverPayDriver
		if err = json.Unmarshal(body, &frozen); err != nil {
			return false, err
		}
		if frozen.SalaryCarry != "" || frozen.NetPayCents() < 0 {
			return true, nil
		}
	}
	return false, rows.Err()
}
