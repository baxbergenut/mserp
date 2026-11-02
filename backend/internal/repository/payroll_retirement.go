package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type PayrollRetirement struct {
	FirstWeek     string `json:"firstWeek"`
	DriverWeeks   int64  `json:"driverWeeks"`
	InvestorWeeks int64  `json:"investorWeeks"`
	PinnedCosts   int64  `json:"pinnedCosts"`
	VerifiedWeeks int    `json:"verifiedWeeks"`
}

// Operator-only, with all writers quiesced. Source invoices, payment allocations,
// financial audit history and compact references survive. A current report difference
// aborts the entire retirement, including any pinned pre-ledger cost evidence.
func (r *DriverPayRepository) RetirePayrollBefore(ctx context.Context, first string) (PayrollRetirement, error) {
	result := PayrollRetirement{FirstWeek: first}
	start, err := chargeWeek(first)
	if err != nil || first > ChargeCurrentWeek() {
		return result, chargeInvalid("Invalid first payroll week")
	}
	tx, closeWrite, err := r.beginPayrollWrite(ctx)
	if err != nil {
		return result, err
	}
	defer closeWrite()
	var blocked bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM payroll_settlements WHERE week_start<$1 AND finalized) OR EXISTS(SELECT 1 FROM investor_pay_weeks WHERE week_start<$1 AND finalized)`, start).Scan(&blocked); err != nil {
		return result, err
	}
	if blocked {
		return result, chargeInvalid("Finalized historical statements require separate review before retirement")
	}
	var previous *time.Time
	if err = tx.QueryRow(ctx, `SELECT first_week FROM payroll_projection_control WHERE singleton FOR UPDATE`).Scan(&previous); err != nil {
		return result, err
	}
	if previous != nil && start.Before(*previous) {
		return result, chargeInvalid("Retired payroll weeks cannot be reopened by moving the boundary back")
	}
	weeks := []time.Time{}
	before := []string{}
	current, _ := chargeWeek(ChargeCurrentWeek())
	for week := start; !week.After(current); week = week.AddDate(0, 0, 7) {
		if len(weeks) >= 104 {
			return result, chargeInvalid("Retirement validation is limited to two years of retained payroll")
		}
		reports, e := readPayrollProjectionReference(ctx, tx, week, true)
		if e != nil {
			return result, e
		}
		weeks = append(weeks, week)
		before = append(before, payrollRetirementValues(reports))
	}
	// Preserve precisely the pre-ledger cost values the calculator already uses.
	records, err := readPayCostRecords(context.WithValue(ctx, payrollCostDetailKey{}, true), tx)
	if err != nil {
		return result, err
	}
	for _, record := range records {
		if record.Week == "" || record.Week >= first {
			continue
		}
		changed, e := tx.Exec(ctx, `INSERT INTO driver_pay_cost_collections(driver_id,week_start,fuel_base,toll_base,fuel_amount,toll_amount)
 SELECT $1::uuid,$2::date,$3::numeric,$4::numeric,$5::numeric,$6::numeric
 WHERE EXISTS(SELECT 1 FROM driver_pay_weeks WHERE driver_id=$1::uuid AND week_start=$2::date AND (fuel_override IS NOT NULL OR toll_override IS NOT NULL))
 ON CONFLICT DO NOTHING`, record.Driver, record.Week, record.Fuel, record.Toll, record.FuelAmount, record.TollAmount)
		if e != nil {
			return result, e
		}
		result.PinnedCosts += changed.RowsAffected()
	}
	changed, err := tx.Exec(ctx, `DELETE FROM driver_pay_weeks WHERE week_start<$1`, start)
	if err != nil {
		return result, err
	}
	result.DriverWeeks = changed.RowsAffected()
	changed, err = tx.Exec(ctx, `DELETE FROM investor_pay_weeks WHERE week_start<$1`, start)
	if err != nil {
		return result, err
	}
	result.InvestorWeeks = changed.RowsAffected()
	if _, err = tx.Exec(ctx, `DELETE FROM payroll_settlements WHERE week_start<$1`, start); err != nil {
		return result, err
	}
	for i, week := range weeks {
		reports, e := readPayrollProjectionReference(ctx, tx, week, true)
		if e != nil {
			return result, e
		}
		if before[i] != payrollRetirementValues(reports) {
			return result, fmt.Errorf("retirement would change retained payroll for %s; all changes rolled back", week.Format(time.DateOnly))
		}
	}
	result.VerifiedWeeks = len(weeks)
	// Retain compact audit identities for retirement, not statement copies.
	if _, err = tx.Exec(ctx, `SELECT payroll_retain_statement_reference(id) FROM payroll_statement_current WHERE week_start<$1`, start); err != nil {
		return result, err
	}
	for _, query := range []string{`UPDATE payroll_projection_control SET first_week=$1 WHERE singleton`,
		`DELETE FROM payroll_statement_state WHERE week_start<$1`, `DELETE FROM payroll_projection_weeks WHERE week_start<$1`,
		`DELETE FROM payroll_projection_jobs WHERE week_start<$1`} {
		if _, err = tx.Exec(ctx, query, start); err != nil {
			return result, err
		}
	}
	if _, err = tx.Exec(ctx, `INSERT INTO payroll_retirement_receipts(first_week,driver_weeks,investor_weeks,pinned_costs,verified_weeks) VALUES($1,$2,$3,$4,$5)`, start, result.DriverWeeks, result.InvestorWeeks, result.PinnedCosts, result.VerifiedWeeks); err != nil {
		return result, err
	}
	if err = tx.Commit(ctx); err != nil {
		return result, err
	}
	r.firstWeek = first
	return result, nil
}

// Revision tokens necessarily change when old saved rows are removed. Every
// financial and display value on retained reports must remain exactly equal.
func payrollRetirementValues(reports [2]DriverPayWeek) string {
	body, _ := json.Marshal(reports)
	var value any
	_ = json.Unmarshal(body, &value)
	var strip func(any)
	strip = func(v any) {
		switch row := v.(type) {
		case map[string]any:
			for key, item := range row {
				if strings.HasSuffix(strings.ToLower(key), "revision") {
					delete(row, key)
				} else {
					strip(item)
				}
			}
		case []any:
			for _, item := range row {
				strip(item)
			}
		}
	}
	strip(value)
	body, _ = json.Marshal(value)
	return string(body)
}
