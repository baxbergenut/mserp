package repository

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// Reimburse is a credit to the selected driver, independent of employment,
// tariff, expense responsibility and truck settlement terms. Each expense is
// included exactly once, in its own Monday-Sunday week. Existing team routing
// and frozen settlement snapshots apply in the same way as other auto charges.
func expenseReimbursementCreditsBulk(ctx context.Context, tx pgx.Tx, drivers []string, week string) (map[string][]PayAutoCharge, error) {
	rows, err := tx.Query(ctx, `SELECT driver_id::text,id::text,
 coalesce(nullif(btrim(expense_type),''),category),amount::text
 FROM expenses WHERE driver_id=ANY($1::uuid[]) AND lower(btrim(payment_type))='reimburse'
 AND expense_date >= $2::date AND expense_date < $2::date+7 AND amount>0
 ORDER BY expense_date,id`, drivers, week)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string][]PayAutoCharge{}
	for rows.Next() {
		var driver, id, name, amount string
		if err = rows.Scan(&driver, &id, &name, &amount); err != nil {
			return nil, err
		}
		result[driver] = append(result[driver], PayAutoCharge{Name: name, Amount: amount, Source: "expense-reimburse:" + id})
	}
	return result, rows.Err()
}
