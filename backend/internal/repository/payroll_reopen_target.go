package repository

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"time"
)

// Reopening removes a freeze; it does not approve a newly calculated amount.
// After validating the committed-input revision under the financial lock, the
// immutable stored statement is sufficient to identify this single operation.
// Its live balances are calculated once, after reopening, for the receipt.
func readPayrollReopenTarget(ctx context.Context, tx pgx.Tx, kind string, week time.Time, id, revision string) (DriverPayWeek, error) {
	r := DriverPayWeek{WeekStart: week.Format(time.DateOnly), Revision: revision, Drivers: []DriverPayDriver{}}
	query := `SELECT report FROM payroll_settlements WHERE driver_id=$1 AND week_start=$2::date AND finalized`
	if kind == "investor" {
		query = `SELECT report FROM investor_pay_weeks WHERE truck_id=$1 AND week_start=$2::date AND finalized`
	}
	var body []byte
	if err := tx.QueryRow(ctx, query, id, r.WeekStart).Scan(&body); err != nil {
		if err == pgx.ErrNoRows {
			return r, chargeInvalid("No eligible settlements selected")
		}
		return r, err
	}
	var d DriverPayDriver
	if err := json.Unmarshal(body, &d); err != nil {
		return r, err
	}
	if d.Settlement == nil {
		d.Settlement = &PayrollSettlement{}
	}
	d.Settlement.Finalized = true
	r.Drivers = append(r.Drivers, d)
	return r, nil
}
