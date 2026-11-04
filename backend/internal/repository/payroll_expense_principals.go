package repository

// Reporting a driver/truck on an invoice is provenance, not a shared debt.
// These are the actual deduction/reimbursement recipients plus saved investor
// collectors. Keep every date and balance here: historical reservations and
// corrections must still connect the same financial resource.
const payrollExpensePrincipalsSQL = `SELECT id expense_id,'driver:'||charge_driver_id principal FROM expenses
 WHERE charge_driver_id IS NOT NULL AND lower(btrim(coalesce(payment_type,'')))<>'reimburse'
 UNION SELECT id,'driver:'||driver_id FROM expenses WHERE driver_id IS NOT NULL AND lower(btrim(payment_type))='reimburse'
 UNION SELECT id,'truck:'||truck_id FROM expenses WHERE truck_id IS NOT NULL AND lower(btrim(covered_by))='truck owner' AND lower(btrim(coalesce(payment_type,'')))<>'reimburse'
 UNION SELECT id,'owner:'||owner_id FROM expenses WHERE owner_id IS NOT NULL AND lower(btrim(covered_by))='truck owner' AND lower(btrim(coalesce(payment_type,'')))<>'reimburse'
 UNION SELECT expense_id,'truck:'||investor_truck_id FROM expense_payments WHERE investor_truck_id IS NOT NULL`

const payrollPeriodSharedEdgesSQL = `WITH expense_principals AS MATERIALIZED (` + payrollExpensePrincipalsSQL + `)
 SELECT 'owner:'||id,'driver:'||driver_id FROM investors WHERE driver_id IS NOT NULL
 UNION SELECT 'truck:'||id,'owner:'||owner_id FROM trucks WHERE owner_id IS NOT NULL
 UNION SELECT a.principal,b.principal FROM expense_principals a JOIN expense_principals b USING(expense_id)
 UNION SELECT 'driver:'||driver_id,'truck:'||truck_id FROM payroll_load_placements WHERE truck_id IS NOT NULL`

const payrollPeriodCompanyOwnershipLabelSQL = `SELECT 'owner:'||i.id AS key FROM investors i
 WHERE i.id='` + CompanyOwnerID + `' AND i.is_company
 AND NOT EXISTS(SELECT 1 FROM expenses e WHERE e.owner_id=i.id AND lower(btrim(e.covered_by))='truck owner' AND lower(btrim(coalesce(e.payment_type,'')))<>'reimburse')
 AND NOT EXISTS(SELECT 1 FROM truck_settlement_terms t WHERE t.owner_id=i.id)
 AND NOT EXISTS(SELECT 1 FROM investor_pay_weeks w WHERE w.owner_id=i.id)
 AND NOT EXISTS(SELECT 1 FROM pay_source_collections c WHERE c.owner_id=i.id)`
