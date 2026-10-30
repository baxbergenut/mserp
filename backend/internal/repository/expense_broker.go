package repository

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"
)

// Broker reminders are current collection notes, never payroll credits/debits.
// Match the loads already routed onto the statement, including frozen loads and
// weekend transfers, rather than today's driver/truck assignment.
type BrokerReimbursementReminder struct {
	ExpenseID   string `json:"expenseId"`
	LoadNumber  string `json:"loadNumber"`
	ExpenseType string `json:"expenseType"`
	ExpenseDate string `json:"expenseDate"`
	Amount      string `json:"amount"`
}

func decorateBrokerReimbursements(ctx context.Context, tx pgx.Tx, report *DriverPayWeek) error {
	numbers := []string{}
	seen := map[string]bool{}
	for i := range report.Drivers {
		report.Drivers[i].BrokerReimbursements = nil
		for _, l := range report.Drivers[i].Loads {
			n := strings.ToLower(strings.TrimSpace(l.LoadNumber))
			if n != "" && !seen[n] {
				seen[n] = true
				numbers = append(numbers, n)
			}
		}
	}
	if len(numbers) == 0 {
		return nil
	}
	rows, err := tx.Query(ctx, `SELECT id::text, to_jsonb(e)->>'broker_load_number',
 coalesce(expense_type,'Expense'),coalesce(expense_date::text,''),coalesce(amount::text,'0.00')
 FROM expenses e WHERE lower(btrim(covered_by))='broker'
 AND lower(btrim(to_jsonb(e)->>'broker_load_number'))=ANY($1::text[])
 ORDER BY expense_date,id`, numbers)
	if err != nil {
		return err
	}
	defer rows.Close()
	byNumber := map[string][]BrokerReimbursementReminder{}
	for rows.Next() {
		var item BrokerReimbursementReminder
		if err = rows.Scan(&item.ExpenseID, &item.LoadNumber, &item.ExpenseType, &item.ExpenseDate, &item.Amount); err != nil {
			return err
		}
		key := strings.ToLower(strings.TrimSpace(item.LoadNumber))
		byNumber[key] = append(byNumber[key], item)
	}
	if err = rows.Err(); err != nil {
		return err
	}
	for i := range report.Drivers {
		seen := map[string]bool{}
		for _, l := range report.Drivers[i].Loads {
			for _, item := range byNumber[strings.ToLower(strings.TrimSpace(l.LoadNumber))] {
				if !seen[item.ExpenseID] {
					report.Drivers[i].BrokerReimbursements = append(report.Drivers[i].BrokerReimbursements, item)
					seen[item.ExpenseID] = true
				}
			}
		}
	}
	return nil
}
