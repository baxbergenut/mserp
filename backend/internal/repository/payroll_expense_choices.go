package repository

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"
)

// Choices belong to one open statement. They are not payments or reservations.
// An absent choice means collect the current unpaid balance at finalization.
type payrollExpenseChoice struct {
	Mode   string `json:"mode"`
	Amount string `json:"amount,omitempty"`
}

type payrollExpenseChoices map[string]payrollExpenseChoice

func readPayrollExpenseChoices(ctx context.Context, tx pgx.Tx, kind, week string, ids []string) (map[string]payrollExpenseChoices, error) {
	table, key := "driver_pay_weeks", "driver_id"
	if kind == "investor" {
		table, key = "investor_pay_weeks", "truck_id"
	}
	rows, err := tx.Query(ctx, `SELECT `+key+`::text,expense_choices FROM `+table+` WHERE week_start=$1::date AND `+key+`=ANY($2::uuid[])`, week, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string]payrollExpenseChoices{}
	for rows.Next() {
		var id string
		var body []byte
		if err = rows.Scan(&id, &body); err != nil {
			return nil, err
		}
		var choices payrollExpenseChoices
		if err = json.Unmarshal(body, &choices); err != nil {
			return nil, err
		}
		result[id] = choices
	}
	return result, rows.Err()
}

func mergePayrollExpenseChoices(before payrollExpenseChoices, current, input []ExpenseDeduction) (payrollExpenseChoices, error) {
	choices := payrollExpenseChoices{}
	for id, choice := range before {
		choices[id] = choice
	}
	balances := map[string]ExpenseDeduction{}
	for _, row := range current {
		if row.Source == "" {
			balances[row.ExpenseID] = row
		}
	}
	seen := map[string]bool{}
	for _, row := range input {
		if row.Source != "" || !row.Apply {
			continue
		}
		balance, exists := balances[row.ExpenseID]
		if !exists || seen[row.ExpenseID] || balance.Version != row.Version || balance.Saved {
			return nil, ErrDriverPayConflict
		}
		seen[row.ExpenseID] = true
		choice := payrollExpenseChoice{Mode: row.Mode, Amount: row.Amount}
		switch choice.Mode {
		case "auto":
			delete(choices, row.ExpenseID)
			continue
		case "skip":
			choice.Amount = "0.00"
		case "", "fixed":
			choice.Mode = "fixed" // Explicit amount edits from older clients.
			n, err := chargeCents(choice.Amount)
			limit, limitErr := chargeCents(balance.Available)
			if err != nil || limitErr != nil || n < 0 || n > limit {
				return nil, chargeInvalid("Expense deduction must be between zero and the available balance (%s)", balance.Available)
			}
			choice.Amount = chargeMoney(n)
		default:
			return nil, chargeInvalid("Choose automatic, a fixed amount, or skip for the expense")
		}
		choices[row.ExpenseID] = choice
	}
	return choices, nil
}

func applyPayrollExpenseChoices(rows []ExpenseDeduction, choices payrollExpenseChoices) error {
	for i := range rows {
		row := &rows[i]
		if row.Source != "" || row.Saved {
			continue
		}
		row.Mode = "auto"
		row.Amount = row.Available
		if choice, ok := choices[row.ExpenseID]; ok {
			row.Mode = choice.Mode
			switch choice.Mode {
			case "auto":
			case "skip":
				row.Amount = "0.00"
			case "fixed":
				row.Amount = choice.Amount
			default:
				return chargeInvalid("Invalid saved expense deduction choice")
			}
		}
		available, err := chargeCents(row.Available)
		if err != nil {
			return err
		}
		amount, err := chargeCents(row.Amount)
		if err != nil {
			return err
		}
		// Keep an over-limit fixed choice visible for review, never silently
		// shrink a reviewed deduction. The posting limit still rejects it.
		row.OpeningBalance = row.Available
		row.Remaining = chargeMoney(available - amount)
	}
	return nil
}

func applyPayrollReportExpenseChoices(ctx context.Context, tx pgx.Tx, kind string, report *DriverPayWeek) error {
	ids := []string{}
	for _, d := range report.Drivers {
		if d.Settlement == nil || !d.Settlement.Finalized {
			ids = append(ids, d.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	choices, err := readPayrollExpenseChoices(ctx, tx, kind, report.WeekStart, ids)
	if err != nil {
		return err
	}
	for i := range report.Drivers {
		d := &report.Drivers[i]
		if d.Settlement != nil && d.Settlement.Finalized {
			continue
		}
		if err = applyPayrollExpenseChoices(d.Edits.ExpenseDeductions, choices[d.ID]); err != nil {
			return err
		}
	}
	return nil
}

// Credits and escrow keep their existing accounting workflow.
func nonExpenseDeductions(input []ExpenseDeduction) []ExpenseDeduction {
	rows := []ExpenseDeduction{}
	for _, row := range input {
		if row.Source != "" {
			rows = append(rows, row)
		}
	}
	return rows
}

// Reopening returns exactly this frozen statement's expense collections. The
// caller first clears finalized metadata in this same transaction, so ordinary
// ledger guards and audit receipts still apply to every reversal.
func reversePayrollExpenses(ctx context.Context, tx pgx.Tx, kind, week string, cards []DriverPayDriver) error {
	type posting struct {
		Expense   string `json:"expense"`
		Statement string `json:"statement"`
		Amount    string `json:"amount"`
	}
	postings := []posting{}
	for _, d := range cards {
		for _, row := range d.Edits.ExpenseDeductions {
			if row.Source == "" {
				postings = append(postings, posting{row.ExpenseID, d.ID, row.Amount})
			}
		}
	}
	if len(postings) == 0 {
		return nil
	}
	body, err := json.Marshal(postings)
	if err != nil {
		return err
	}
	var reversed int
	err = tx.QueryRow(ctx, `WITH reviewed AS MATERIALIZED (
 SELECT * FROM jsonb_to_recordset($1::jsonb) r(expense uuid,statement uuid,amount numeric)
 ), locked AS MATERIALIZED (
 SELECT p.expense_id FROM expense_payments p JOIN reviewed r ON r.expense=p.expense_id
 WHERE p.week_start=$2::date AND p.amount=r.amount
 AND ((p.statement_kind=$3 AND p.statement_id=r.statement) OR
      (p.statement_kind IS NULL AND (($3='driver' AND p.investor_truck_id IS NULL) OR ($3='investor' AND p.investor_truck_id=r.statement))))
 ORDER BY p.expense_id FOR UPDATE OF p
 ), removed AS (
 DELETE FROM expense_payments p USING locked l WHERE p.expense_id=l.expense_id AND p.week_start=$2::date RETURNING p.expense_id,p.statement_kind,p.amount
 ), legacy_choices AS (
 SELECT r.statement,jsonb_object_agg(p.expense_id::text,jsonb_build_object('mode',CASE WHEN p.amount=0 THEN 'skip' ELSE 'fixed' END,'amount',p.amount::text)) choices
 FROM removed p JOIN reviewed r ON r.expense=p.expense_id WHERE p.statement_kind IS NULL GROUP BY r.statement
 ), driver_choices AS (
 INSERT INTO driver_pay_weeks(driver_id,week_start,expense_choices)
 SELECT statement,$2::date,choices FROM legacy_choices WHERE $3='driver'
 ON CONFLICT(driver_id,week_start) DO UPDATE SET expense_choices=excluded.expense_choices||driver_pay_weeks.expense_choices,
 version=driver_pay_weeks.version+1,updated_at=now() RETURNING driver_id
 ), investor_choices AS (
 UPDATE investor_pay_weeks w SET expense_choices=l.choices||w.expense_choices,version=w.version+1
 FROM legacy_choices l WHERE $3='investor' AND w.truck_id=l.statement AND w.week_start=$2::date RETURNING w.truck_id
 ), touched AS (
 UPDATE expenses e SET updated_at=now() FROM removed r WHERE e.id=r.expense_id RETURNING e.id
 ) SELECT count(*) FROM touched`, string(body), week, kind).Scan(&reversed)
	if err != nil {
		return err
	}
	if reversed != len(postings) {
		return ErrDriverPayConflict
	}
	return nil
}
