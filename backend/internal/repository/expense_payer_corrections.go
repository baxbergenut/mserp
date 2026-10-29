package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

var ErrExpensePayerCorrection = errors.New("expense payer correction")

type expensePayerPayment struct {
	Week      string    `json:"weekStart"`
	Amount    string    `json:"amount"`
	Truck     *string   `json:"investorTruckId"`
	UpdatedBy *string   `json:"updatedBy"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type expensePayerCorrection struct {
	Previous        Expense
	Payments        []expensePayerPayment
	CoveredBy       string
	SettlementTotal string
	Driver, Truck   *string
}

func sameExpenseID(a, b *string) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}

// Cancel the old allocation before changing responsibility, allowing the normal
// identity guards to keep protecting every other edit. The caller holds the
// financial lock; failures anywhere roll back both sides and the invoice edit.
func prepareExpensePayerCorrection(ctx context.Context, tx pgx.Tx, id string, in ExpenseInput) (*expensePayerCorrection, error) {
	old, err := scanExpense(tx.QueryRow(ctx, selectExpensesSQL+" WHERE e.id=$1 FOR UPDATE OF e", id))
	if err != nil {
		return nil, mapNotFound(err)
	}
	if in.UpdatedAt != nil && !old.UpdatedAt.Equal(*in.UpdatedAt) {
		return nil, ErrExpenseSettingConflict
	}
	covered := normalizedExpenseText(in.CoveredBy)
	owner, driver := in.OwnerID, in.DriverID
	if covered == "truck owner" && owner != nil {
		var company bool
		if err = tx.QueryRow(ctx, `SELECT driver_id::text,is_company FROM investors WHERE id=$1`, *owner).Scan(&driver, &company); err != nil {
			return nil, err
		}
		if company {
			covered, owner, driver = "company", nil, nil
		}
	} else if covered != "driver" {
		owner, driver = nil, nil
	} else {
		owner = nil
	}
	if normalizedExpenseText(old.CoveredBy) == covered && sameExpenseID(old.OwnerID, owner) && sameExpenseID(old.ChargeDriverID, driver) {
		return nil, nil
	}
	fail := func(message string) (*expensePayerCorrection, error) {
		return nil, fmt.Errorf("%w: %s", ErrExpensePayerCorrection, message)
	}
	var external bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM expense_external_settlements WHERE expense_id=$1)`, id).Scan(&external); err != nil {
		return nil, err
	}
	if old.DriverSettled || external {
		return fail("Previously settled expenses retain their responsible person; reverse the settlement before correcting the payer")
	}
	if normalizedExpenseText(old.PaymentType) == "reimburse" || normalizedExpenseText(in.PaymentType) == "reimburse" {
		return fail("Reimbursements retain their payout recipient; correct the reimbursement separately")
	}
	rows, err := tx.Query(ctx, `SELECT week_start::text,amount::text,investor_truck_id::text,updated_by::text,updated_at FROM expense_payments WHERE expense_id=$1 ORDER BY week_start FOR UPDATE`, id)
	if err != nil {
		return nil, err
	}
	c := &expensePayerCorrection{Previous: old, CoveredBy: covered, Driver: driver}
	if covered == "truck owner" {
		if in.TruckID == nil || owner == nil {
			rows.Close()
			return fail("Select the linked truck and responsible truck owner to transfer its deductions")
		}
		c.Truck = in.TruckID
	}
	for rows.Next() {
		var p expensePayerPayment
		if err = rows.Scan(&p.Week, &p.Amount, &p.Truck, &p.UpdatedBy, &p.UpdatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		c.Payments = append(c.Payments, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if len(c.Payments) == 0 {
		return nil, nil
	}
	if c.Truck != nil {
		var conflict bool
		err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM expense_payments p
 JOIN LATERAL (SELECT owner_id FROM truck_settlement_terms WHERE truck_id=$2 AND week_start<=p.week_start ORDER BY week_start DESC LIMIT 1) t ON true
 WHERE p.expense_id=$1 AND t.owner_id<>$3::uuid)
 OR EXISTS(SELECT 1 FROM investor_pay_weeks w JOIN expense_payments p ON p.week_start=w.week_start
 WHERE p.expense_id=$1 AND w.truck_id=$2 AND w.owner_id<>$3::uuid)`, id, c.Truck, owner).Scan(&conflict)
		if err != nil {
			return nil, err
		}
		if conflict {
			return fail("Review the dated investor agreement and existing truck statement; they name a different owner")
		}
	}
	if old.Amount == nil {
		return fail("The saved deduction has no invoice total; review it before correcting the responsible person")
	}
	beforeAmount, _ := chargeCents(*old.Amount)
	afterAmount, amountErr := chargeCents(in.Amount)
	if amountErr != nil || beforeAmount != afterAmount || old.ExpenseDate == nil || *old.ExpenseDate != in.ExpenseDate.Format("2006-01-02") ||
		old.CategoryID != in.CategoryID || !sameExpenseID(old.TruckID, in.TruckID) || normalizedExpenseText(old.PaymentType) != normalizedExpenseText(in.PaymentType) {
		return fail("Save the responsible person correction separately; a saved deduction must retain its truck, date, category, payment method and total")
	}
	var closed bool
	// Frozen snapshots may contain the expense even when a legacy allocation is
	// absent. Check both identities, their dated main drivers, and both destinations.
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM payroll_settlements s WHERE s.finalized AND (
 s.report @> jsonb_build_object('edits',jsonb_build_object('expenseDeductions',jsonb_build_array(jsonb_build_object('expenseId',$1::text))))
 OR (s.week_start IN (SELECT week_start FROM expense_payments WHERE expense_id=$1::uuid)
 AND (s.driver_id=payroll_driver_at($2::uuid,s.week_start) OR s.driver_id=payroll_driver_at($3::uuid,s.week_start)))))
 OR EXISTS(SELECT 1 FROM investor_pay_weeks w WHERE w.finalized AND (
 w.report @> jsonb_build_object('edits',jsonb_build_object('expenseDeductions',jsonb_build_array(jsonb_build_object('expenseId',$1::text))))
 OR (w.week_start IN (SELECT week_start FROM expense_payments WHERE expense_id=$1::uuid)
 AND (w.truck_id=$4::uuid OR w.truck_id IN (SELECT investor_truck_id FROM expense_payments WHERE expense_id=$1::uuid)))))`, id, old.ChargeDriverID, driver, c.Truck).Scan(&closed)
	if err != nil {
		return nil, err
	}
	if closed {
		return fail("Reopen the affected driver and investor statements before changing the responsible person")
	}
	if err = tx.QueryRow(ctx, `SELECT `+expenseSettlementTotalSQL+`::text FROM expenses e WHERE e.id=$1`, id).Scan(&c.SettlementTotal); err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM expense_payments WHERE expense_id=$1`, id); err != nil {
		return nil, err
	}
	return c, nil
}

func finishExpensePayerCorrection(ctx context.Context, tx pgx.Tx, id string, in ExpenseInput, c *expensePayerCorrection) error {
	if c == nil {
		return nil
	}
	var total string
	if err := tx.QueryRow(ctx, `SELECT `+expenseSettlementTotalSQL+`::text FROM expenses e WHERE e.id=$1`, id).Scan(&total); err != nil {
		return err
	}
	if total != c.SettlementTotal {
		return fmt.Errorf("%w: Retain the original settlement total including payment fees when correcting the responsible person", ErrExpensePayerCorrection)
	}
	if c.CoveredBy == "driver" || c.CoveredBy == "truck owner" {
		for _, p := range c.Payments {
			if _, err := tx.Exec(ctx, `INSERT INTO expense_payments(expense_id,week_start,amount,updated_by,investor_truck_id)
 VALUES($1,$2::date,$3::numeric,nullif($4,'')::uuid,$5)`, id, p.Week, p.Amount, in.CreatedBy, c.Truck); err != nil {
				return err
			}
		}
	}
	details, err := json.Marshal(map[string]any{
		"previousCoveredBy": c.Previous.CoveredBy, "previousOwnerId": c.Previous.OwnerID, "previousChargeDriverId": c.Previous.ChargeDriverID,
		"coveredBy": c.CoveredBy, "ownerId": in.OwnerID, "chargeDriverId": c.Driver, "investorTruckId": c.Truck, "previousPayments": c.Payments,
	})
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO access_audit(actor_id,action,target_id,details) VALUES(nullif($1,'')::uuid,'expense.payer_corrected',$2,$3)`, in.CreatedBy, id, details)
	return err
}

func normalizedExpenseText(value *string) string {
	if value == nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(*value))
}
