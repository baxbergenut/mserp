package repository

import (
	"context"
	"github.com/jackc/pgx/v5"
)

// Whole-week finalization already calculated these balances uncached under the
// financial lock. Lock and fence every principal once, then retain the ordinary
// payment/audit triggers with set-based writes. Any error rolls back the week.
func finalizePayrollExpenses(ctx context.Context, tx pgx.Tx, kind, week, actor string, cards []DriverPayDriver) error {
	type allocation struct {
		item             ExpenseDeduction
		statement, owner string
	}
	selected := map[string]allocation{}
	ids := []string{}
	for _, d := range cards {
		if d.Settlement != nil && d.Settlement.Finalized || kind == "investor" && d.TruckInactive {
			continue
		}
		for _, v := range d.Edits.ExpenseDeductions {
			if v.Saved {
				continue
			}
			if _, exists := selected[v.ExpenseID]; exists {
				return ErrDriverPayConflict
			}
			n, e := chargeCents(v.Amount)
			limit, le := chargeCents(v.Available)
			if e != nil || le != nil || n < 0 || n > limit {
				return chargeInvalid("Expense deduction exceeds its available balance")
			}
			selected[v.ExpenseID] = allocation{v, d.ID, d.InvestorID}
			ids = append(ids, v.ExpenseID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	found := map[string]bool{}
	rows, err := tx.Query(ctx, `SELECT id::text,balance_version,
 CASE WHEN lower(btrim(payment_type))='reimburse' THEN 'reimburse' ELSE '' END,
 coalesce(payroll_driver_at(CASE WHEN lower(btrim(payment_type))='reimburse' THEN driver_id ELSE charge_driver_id END,$2::date)::text,''),
 coalesce(truck_id::text,''),coalesce(owner_id::text,''),lower(btrim(covered_by))
 FROM expenses WHERE id=ANY($1::uuid[]) ORDER BY id FOR UPDATE`, ids, week)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id, source, driver, truck, owner, payer string
		var version int
		if err = rows.Scan(&id, &version, &source, &driver, &truck, &owner, &payer); err != nil {
			rows.Close()
			return err
		}
		a := selected[id]
		if version != a.item.Version || source != a.item.Source || (kind == "driver" && driver != a.statement) ||
			(kind == "investor" && (truck != a.statement || owner != a.owner || payer != "truck owner" || source != "")) {
			rows.Close()
			return ErrDriverPayConflict
		}
		found[id] = true
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	rows, err = tx.Query(ctx, `SELECT id::text,driver_id::text,balance_version FROM driver_escrows WHERE id=ANY($1::uuid[]) ORDER BY id FOR UPDATE`, ids)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id, driver string
		var version int
		if err = rows.Scan(&id, &driver, &version); err != nil {
			rows.Close()
			return err
		}
		a := selected[id]
		if found[id] || kind != "driver" || a.item.Source != "escrow" || version != a.item.Version || driver != a.statement {
			rows.Close()
			return ErrDriverPayConflict
		}
		found[id] = true
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if len(found) != len(selected) {
		return ErrDriverPayConflict
	}
	// Use the same three destination ledgers and immutable per-row receipts.
	for _, source := range []string{"", "reimburse", "escrow"} {
		keys, amounts, trucks := []string{}, []string{}, []string{}
		for _, id := range ids {
			a := selected[id]
			if a.item.Source == source {
				keys = append(keys, id)
				amounts = append(amounts, a.item.Amount)
				trucks = append(trucks, a.statement)
			}
		}
		if len(keys) == 0 {
			continue
		}
		var resultCount int64
		if source == "" && kind == "investor" {
			tag, e := tx.Exec(ctx, `INSERT INTO expense_payments(expense_id,week_start,amount,updated_by,investor_truck_id)
 SELECT id,$2::date,amount::numeric,nullif($4,'')::uuid,truck FROM unnest($1::uuid[],$3::text[],$5::uuid[]) v(id,amount,truck) ORDER BY id
 ON CONFLICT(expense_id,week_start) DO UPDATE SET amount=excluded.amount,updated_by=excluded.updated_by,updated_at=now()
 WHERE expense_payments.investor_truck_id=excluded.investor_truck_id`, keys, week, amounts, actor, trucks)
			if e != nil {
				return e
			}
			resultCount = tag.RowsAffected()
		} else {
			table, key := "expense_payments", "expense_id"
			if source == "reimburse" {
				table = "expense_reimbursement_payments"
			}
			if source == "escrow" {
				table, key = "driver_escrow_payments", "escrow_id"
			}
			tag, e := tx.Exec(ctx, `INSERT INTO `+table+`(`+key+`,week_start,amount,updated_by)
 SELECT id,$2::date,amount::numeric,nullif($4,'')::uuid FROM unnest($1::uuid[],$3::text[]) v(id,amount) ORDER BY id
 ON CONFLICT(`+key+`,week_start) DO UPDATE SET amount=excluded.amount,updated_by=excluded.updated_by,updated_at=now()`, keys, week, amounts, actor)
			if e != nil {
				return e
			}
			resultCount = tag.RowsAffected()
		}
		if resultCount != int64(len(keys)) {
			return ErrDriverPayConflict
		}
		if source == "" {
			if _, err = tx.Exec(ctx, `UPDATE expenses SET updated_at=now() WHERE id=ANY($1::uuid[])`, keys); err != nil {
				return err
			}
		}
	}
	return nil
}
