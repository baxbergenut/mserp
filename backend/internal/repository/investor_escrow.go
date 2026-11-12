package repository

import (
	"context"
	"github.com/jackc/pgx/v5"
)

func investorEscrowDeductions(ctx context.Context, tx pgx.Tx, truck, owner, week string) ([]ExpenseDeduction, error) {
	rows, err := tx.Query(ctx, `SELECT e.id::text,e.start_date::text,e.amount::text,
 escrow_collection_available(e.id,$3::date)::text,
 coalesce(w.amount,escrow_collection_available(e.id,$3::date))::text,
 (e.amount-e.opening_paid-coalesce(p.prior,0)+coalesce(r.prior,0)-coalesce(w.amount,0))::text,
 (e.amount-e.opening_paid-coalesce(p.prior,0)+coalesce(r.prior,0))::text,e.balance_version,w.escrow_id IS NOT NULL
 FROM driver_escrows e LEFT JOIN driver_escrow_payments w ON w.escrow_id=e.id AND w.week_start=$3::date
 LEFT JOIN LATERAL(SELECT sum(amount) prior FROM driver_escrow_payments WHERE escrow_id=e.id AND week_start<$3::date) p ON true
 LEFT JOIN LATERAL(SELECT sum(amount) prior FROM driver_escrow_releases WHERE escrow_id=e.id AND week_start<$3::date AND NOT cancelled) r ON true
 WHERE e.truck_id=$1 AND e.owner_id=$2 AND e.start_date<$3::date+7
 AND (w.escrow_id IS NOT NULL OR (investor_truck_account_eligible(e.truck_id,e.owner_id,$3::date,$3::date+7) AND escrow_collection_available(e.id,$3::date)>0 AND NOT escrow_collection_stopped(e.id,$3::date)))
 ORDER BY e.start_date,e.id`, truck, owner, week)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ExpenseDeduction{}
	for rows.Next() {
		d := ExpenseDeduction{Name: "Escrow", Source: "escrow"}
		if err = rows.Scan(&d.ExpenseID, &d.ExpenseDate, &d.Total, &d.Available, &d.Amount, &d.Remaining, &d.OpeningBalance, &d.Version, &d.Saved); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// Use the report week: legacy saved edits may omit their weekStart.
func addInvestorAccounts(ctx context.Context, tx pgx.Tx, d *DriverPayDriver, data ChargeData, loads map[string]map[string]bool, week string) error {
	escrow, err := investorEscrowDeductions(ctx, tx, d.ID, d.InvestorID, week)
	if err != nil {
		return err
	}
	d.Edits.ExpenseDeductions = append(d.Edits.ExpenseDeductions, escrow...)
	data.Schedules = truckOwnerSchedules(data.Schedules, d.ID, d.InvestorID)
	eligible, checkErr := investorAccountEligible(ctx, tx, d.ID, d.InvestorID, week)
	if checkErr != nil {
		return checkErr
	}
	if !eligible {
		data.Schedules = nil
	}
	generated, e := generatedForWeek(data, loads, week)
	err = e
	d.Edits.GeneratedCharges = generated[d.ID]
	if err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `SELECT r.id::text,r.amount::text FROM driver_escrow_releases r JOIN driver_escrows e ON e.id=r.escrow_id
 WHERE e.truck_id=$1 AND e.owner_id=$2 AND r.week_start=$3::date AND NOT r.cancelled AND r.payroll_return IS NULL AND r.correction_reason IS NULL`, d.ID, d.InvestorID, week)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, amount string
		if err = rows.Scan(&id, &amount); err != nil {
			return err
		}
		d.AutoCharges = append(d.AutoCharges, PayAutoCharge{Name: "Escrow release", Amount: amount, Source: "escrow-release:" + id})
	}
	return rows.Err()
}

func saveInvestorEscrow(ctx context.Context, tx pgx.Tx, d DriverPayDriver, actor string, items []ExpenseDeduction) error {
	current, err := investorEscrowDeductions(ctx, tx, d.ID, d.InvestorID, d.Edits.WeekStart)
	if err != nil {
		return err
	}
	balances := map[string]ExpenseDeduction{}
	for _, v := range current {
		balances[v.ExpenseID] = v
	}
	seen := map[string]bool{}
	for _, v := range items {
		if v.Source != "escrow" || !v.Apply {
			continue
		}
		c, ok := balances[v.ExpenseID]
		if !ok || seen[v.ExpenseID] || v.Version != c.Version {
			return ErrDriverPayConflict
		}
		seen[v.ExpenseID] = true
		n, e := chargeCents(v.Amount)
		limit, _ := chargeCents(c.Available)
		if e != nil || n < 0 || n > limit {
			return chargeInvalid("Escrow deduction exceeds the available balance")
		}
		if _, err = tx.Exec(ctx, `INSERT INTO driver_escrow_payments(escrow_id,week_start,amount,updated_by) VALUES($1,$2::date,$3::numeric,nullif($4,'')::uuid)
 ON CONFLICT(escrow_id,week_start) DO UPDATE SET amount=excluded.amount,updated_by=excluded.updated_by,updated_at=now()`, v.ExpenseID, d.Edits.WeekStart, v.Amount, actor); err != nil {
			return err
		}
	}
	return nil
}
