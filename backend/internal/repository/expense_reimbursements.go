package repository

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"
)

// Reimburse is a credit to the selected driver, independent of employment,
// tariff, expense responsibility and truck settlement terms. Each expense is
// included exactly once, in its own Monday-Sunday week. Existing team routing
// and frozen settlement snapshots apply in the same way as other auto charges.
func expenseReimbursementCreditsBulk(ctx context.Context, tx pgx.Tx, drivers []string, week string) (map[string][]PayAutoCharge, error) {
	rows, err := tx.Query(ctx, `SELECT driver_id::text,id::text,
 coalesce(nullif(btrim(expense_type),''),category),amount::text,expense_date::text
 FROM expenses WHERE driver_id=ANY($1::uuid[]) AND lower(btrim(payment_type))='reimburse'
 AND expense_date >= $2::date AND expense_date < $2::date+7 AND amount>0
 ORDER BY expense_date,id`, drivers, week)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string][]PayAutoCharge{}
	for rows.Next() {
		var driver, id, name, amount, date string
		if err = rows.Scan(&driver, &id, &name, &amount, &date); err != nil {
			return nil, err
		}
		result[driver] = append(result[driver], PayAutoCharge{Name: name, Amount: amount, Source: "expense-reimburse:" + id, ExpenseDate: date})
	}
	return result, rows.Err()
}

// Older frozen statements predate expenseDate metadata. Add only the immutable
// source date to the response; never rewrite the frozen amounts or saved JSON.
func populateExpenseReimbursementDates(ctx context.Context, tx pgx.Tx, statements []DriverPayDriver) error {
	ids := []string{}
	for _, statement := range statements {
		for _, row := range statement.AutoCharges {
			if row.ExpenseDate == "" && strings.HasPrefix(row.Source, "expense-reimburse:") {
				ids = append(ids, strings.TrimPrefix(row.Source, "expense-reimburse:"))
			}
		}
	}
	if len(ids) == 0 {
		return nil
	}
	rows, err := tx.Query(ctx, `SELECT id::text,expense_date::text FROM expenses WHERE id=ANY($1::uuid[])`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	dates := map[string]string{}
	for rows.Next() {
		var id, date string
		if err = rows.Scan(&id, &date); err != nil {
			return err
		}
		dates["expense-reimburse:"+id] = date
	}
	if err = rows.Err(); err != nil {
		return err
	}
	for _, statement := range statements {
		for i := range statement.AutoCharges {
			row := &statement.AutoCharges[i]
			if row.ExpenseDate == "" {
				row.ExpenseDate = dates[row.Source]
			}
		}
	}
	return nil
}
