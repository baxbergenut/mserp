package repository

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"
	"mserp/internal/db"
)

type payrollBalanceBatchKey struct{}
type payrollBalanceScope struct {
	Week    string   `json:"week"`
	Drivers []string `json:"drivers"`
}
type payrollBalanceBatch struct {
	tx       pgx.Tx
	coverage map[string]map[string]bool
	rows     map[string]map[string][]ExpenseDeduction
}

// Created only after canonical writes, for the exact affected period scopes.
// This is a transaction-local calculation input, never a cross-request cache.
// Keep the single-period SQL as the independent reference and use the same
// ordered ledger semantics for past balances and future reservations here.
func payrollBalanceBatchContext(ctx context.Context, tx pgx.Tx, scopes []payrollBalanceScope) (context.Context, error) {
	defer db.Phase(ctx, "balance_batch")()
	batch := &payrollBalanceBatch{tx: tx, coverage: map[string]map[string]bool{}, rows: map[string]map[string][]ExpenseDeduction{}}
	for _, scope := range scopes {
		batch.coverage[scope.Week] = map[string]bool{}
		batch.rows[scope.Week] = map[string][]ExpenseDeduction{}
		for _, id := range scope.Drivers {
			batch.coverage[scope.Week][id] = true
		}
	}
	if len(scopes) == 0 {
		return ctx, nil
	}
	body, err := json.Marshal(scopes)
	if err != nil {
		return ctx, err
	}
	parts := []struct{ source, query string }{
		{"", payrollExpenseBalanceBatchSQL("")}, {"reimburse", payrollExpenseBalanceBatchSQL("reimburse")},
	}
	available, err := accessorialLedgerAvailable(ctx, tx)
	if err != nil {
		return ctx, err
	}
	if available {
		parts = append(parts, struct{ source, query string }{"accessorial", payrollExpenseBalanceBatchSQL("accessorial")})
	}
	parts = append(parts, struct{ source, query string }{"escrow", payrollEscrowBalanceBatchSQL})
	for _, part := range parts {
		rows, err := tx.Query(ctx, part.query, string(body))
		if err != nil {
			return ctx, err
		}
		for rows.Next() {
			var week, driver string
			d := ExpenseDeduction{Source: part.source}
			targets := []any{&week, &driver, &d.ExpenseID}
			if part.source != "escrow" {
				targets = append(targets, &d.Name, &d.Category)
			} else {
				d.Name = "Escrow"
			}
			targets = append(targets, &d.ExpenseDate, &d.Total, &d.Available, &d.Amount, &d.Remaining, &d.OpeningBalance, &d.Version, &d.Saved)
			if part.source == "" {
				targets = append(targets, &d.LiveBalance)
			}
			if err = rows.Scan(targets...); err != nil {
				rows.Close()
				return ctx, err
			}
			batch.rows[week][driver] = append(batch.rows[week][driver], d)
		}
		rows.Close()
		if err = rows.Err(); err != nil {
			return ctx, err
		}
	}
	return context.WithValue(ctx, payrollBalanceBatchKey{}, batch), nil
}

// Materialize each eligible principal and its ledger once. Cross it with only
// the explicitly selected periods, retaining the original Thursday-exclusive
// invoice window and all-period reservations (including later saved amounts).
func payrollExpenseBalanceBatchSQL(source string) string {
	driver, total, ledger := "charge_driver_id", "expense_collectible_total(e)", "expense_payments"
	external := expenseOwnerExternalSQL
	eligible := "(lower(btrim(coalesce(payment_type,'')))<>'reimburse' OR coalesce((to_jsonb(e)->>'owner_recovery')::boolean,false)) AND (NOT driver_settled OR expense_balance_adjustment(e.id)<>0)"
	destination := "w.investor_truck_id IS NULL AND "
	invoiceWindow, periodWindow := "", ""
	liveColumn := ",lower(btrim(e.covered_by))='driver'"
	if source == "reimburse" {
		driver, total, ledger = "driver_id", "e.amount", "expense_reimbursement_payments"
		eligible = "lower(btrim(payment_type))='reimburse'"
		destination = ""
		liveColumn = ""
		invoiceWindow = " AND e.expense_date<(SELECT max(week)+10 FROM scopes)"
		periodWindow = " AND e.expense_date<s.week+10"
		external = "expense_external_total(e.id)"
	} else if source == "accessorial" {
		driver, total, ledger = "driver_id", "e.amount", "expense_accessorial_payments"
		eligible = "payment_type='Driver payroll'"
		destination, liveColumn, external = "", "", "0"
	}
	return `WITH scopes AS MATERIALIZED (
 SELECT * FROM jsonb_to_recordset($1::jsonb) s(week date,drivers uuid[])
 ), relevant AS MATERIALIZED (
 SELECT e.*,e.` + driver + ` AS payer,` + total + ` AS collectible,` + external + ` AS external_paid
 FROM expenses e WHERE e.` + driver + ` IN (SELECT unnest(drivers) FROM scopes)
 AND ` + eligible + ` AND accounting_live_source('expense',e.id,e.expense_date)
 ` + invoiceWindow + ` AND e.amount>=0
 ), payments AS MATERIALIZED (
 SELECT p.* FROM ` + ledger + ` p JOIN relevant e ON e.id=p.expense_id
 ), period_payments AS MATERIALIZED (
 SELECT e.id,s.week,coalesce(sum(p.amount) FILTER(WHERE p.week_start<>s.week),0) other,
 coalesce(sum(p.amount) FILTER(WHERE p.week_start<s.week),0) prior
 FROM relevant e JOIN scopes s ON e.payer=ANY(s.drivers)` + periodWindow + `
 LEFT JOIN payments p ON p.expense_id=e.id GROUP BY e.id,s.week
 ) SELECT p.week::text,e.payer::text,e.id,coalesce(nullif(btrim(e.expense_type),''),e.category),e.category,
 e.expense_date::text,e.collectible::text,(e.collectible-e.external_paid-p.other)::text,
 coalesce(w.amount,e.collectible-e.external_paid-p.other)::text,
 (e.collectible-e.external_paid-p.prior-coalesce(w.amount,0))::text,
 (e.collectible-e.external_paid-p.prior)::text,e.balance_version,w.expense_id IS NOT NULL` + liveColumn + `
 FROM period_payments p JOIN relevant e ON e.id=p.id
 LEFT JOIN payments w ON w.expense_id=e.id AND w.week_start=p.week
 WHERE ` + destination + `(w.expense_id IS NOT NULL OR e.collectible-e.external_paid>p.other)
 ORDER BY p.week,e.expense_date,e.id`
}

// Keep the payment checkpoint before the release checkpoint within a week.
// Removing the target collection when computing replacement capacity is also
// period-specific; a later release must never fund an earlier payment.
const payrollEscrowBalanceBatchSQL = `WITH scopes AS MATERIALIZED (
 SELECT * FROM jsonb_to_recordset($1::jsonb) s(week date,drivers uuid[])
 ), escrows AS MATERIALIZED (
 SELECT e.* FROM driver_escrows e WHERE e.driver_id IN (SELECT unnest(drivers) FROM scopes)
 AND accounting_live_source('escrow',e.id,e.start_date) AND e.start_date<(SELECT max(week)+7 FROM scopes)
 ), relevant AS MATERIALIZED (
 SELECT e.*,s.week target FROM escrows e JOIN scopes s ON e.driver_id=ANY(s.drivers) AND e.start_date<s.week+7
 ), payments AS MATERIALIZED (
 SELECT p.* FROM driver_escrow_payments p JOIN escrows e ON e.id=p.escrow_id
 ), events AS MATERIALIZED (
 SELECT p.escrow_id id,p.week_start,0 phase,-sum(p.amount) delta FROM payments p GROUP BY p.escrow_id,p.week_start
 UNION ALL SELECT e.id,r.week_start,1,sum(r.amount) FROM escrows e JOIN driver_escrow_releases r ON r.escrow_id=e.id AND NOT r.cancelled GROUP BY e.id,r.week_start
 ), period_events AS MATERIALIZED (
 SELECT e.id,e.target,v.week_start,v.phase,CASE WHEN v.phase=0 AND v.week_start=e.target THEN 0 ELSE v.delta END delta,
 v.phase=0 AND v.week_start>=e.target checkpoint FROM relevant e JOIN events v ON v.id=e.id
 UNION ALL SELECT id,target,target,0,0::numeric,true FROM relevant
 ), steps AS (
 SELECT id,target,week_start,phase,sum(delta) delta,bool_or(checkpoint) checkpoint FROM period_events GROUP BY id,target,week_start,phase
 ), running AS (
 SELECT id,target,week_start,checkpoint,sum(delta) OVER(PARTITION BY id,target ORDER BY week_start,phase ROWS UNBOUNDED PRECEDING) delta FROM steps
 ), capacity AS MATERIALIZED (
 SELECT e.id,e.target,min(e.amount-e.opening_paid+r.delta) available FROM relevant e JOIN running r ON r.id=e.id AND r.target=e.target
 WHERE r.checkpoint AND r.week_start>=e.target GROUP BY e.id,e.target
 ), opening AS (
 SELECT id,target,sum(delta) delta FROM period_events WHERE week_start<target GROUP BY id,target
 ) SELECT e.target::text,e.driver_id::text,e.id,e.start_date::text,e.amount::text,
 c.available::text,coalesce(w.amount,c.available)::text,
 (e.amount-e.opening_paid+coalesce(o.delta,0)-coalesce(w.amount,0))::text,
 (e.amount-e.opening_paid+coalesce(o.delta,0))::text,e.balance_version,w.escrow_id IS NOT NULL
 FROM relevant e JOIN capacity c ON c.id=e.id AND c.target=e.target
 LEFT JOIN payments w ON w.escrow_id=e.id AND w.week_start=e.target
 LEFT JOIN opening o ON o.id=e.id AND o.target=e.target
 WHERE w.escrow_id IS NOT NULL OR (c.available>0 AND NOT escrow_payroll_suppressed(e.id,e.driver_id,e.target)) ORDER BY e.target,e.start_date,e.id`

func payrollBatchedDeductions(ctx context.Context, tx pgx.Tx, drivers []string, week, source string) (map[string][]ExpenseDeduction, bool) {
	batch, _ := ctx.Value(payrollBalanceBatchKey{}).(*payrollBalanceBatch)
	authoritative, _ := ctx.Value(payrollAuthoritativeKey{}).(bool)
	if batch == nil || batch.tx != tx || authoritative || drivers == nil || batch.coverage[week] == nil {
		return nil, false
	}
	for _, id := range drivers {
		if !batch.coverage[week][id] {
			return nil, false
		}
	}
	result := map[string][]ExpenseDeduction{}
	for _, id := range drivers {
		for _, row := range batch.rows[week][id] {
			if source == "all" || row.Source == source {
				// These structs contain scalar values. Returning separate slices
				// keeps team routing and caller edits out of the shared input.
				result[id] = append(result[id], row)
			}
		}
	}
	return result, true
}
