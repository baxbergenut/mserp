package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestOwnershipFeeCorrectionsDatabase(t *testing.T) {
	withTruckChargeDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, actor string) {
		exec := func(q string, args ...any) {
			t.Helper()
			if _, err := pool.Exec(ctx, q, args...); err != nil {
				t.Fatal(err)
			}
		}
		id := func(q string, args ...any) string {
			t.Helper()
			var v string
			if err := pool.QueryRow(ctx, q, args...).Scan(&v); err != nil {
				t.Fatal(err)
			}
			return v
		}
		oldDriver := id(`INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate) VALUES('Original operator','original operator','cpm',0.7) RETURNING id::text`)
		oldOwner := id(`INSERT INTO investors(full_name,driver_id) VALUES('Original operator',$1) RETURNING id::text`, oldDriver)
		newOwner := id(`INSERT INTO investors(full_name) VALUES('Correct owner') RETURNING id::text`)
		truck := id(`INSERT INTO trucks(unit_number,owner_id) VALUES('CORRECT-FEES',$1) RETURNING id::text`, oldOwner)
		exec(`UPDATE truck_ownership_history SET assigned_at='2026-09-21 04:00+00',start_known=true WHERE truck_id=$1`, truck)
		charges := NewDriverChargeRepository(pool)
		typ, err := charges.SaveType(ctx, ChargeType{Name: "Correction admin", Direction: "charge", Amounts: []string{"100"}, Eligibility: "calendar"}, actor)
		if err != nil {
			t.Fatal(err)
		}
		if err = charges.SaveTruckCharge(ctx, TruckChargePhase{TruckID: truck, TypeID: typ.ID, WeekStart: "2026-09-28", Amount: "100", Included: true, TypeVersion: typ.Version}, actor); err != nil {
			t.Fatal(err)
		}
		exec(`INSERT INTO truck_settlement_terms(truck_id,owner_id,week_start,share_percent) VALUES($1,$2,'2026-09-28',88)`, truck, oldOwner)
		source := "truck_charge:" + truck + ":" + typ.ID
		for _, r := range []struct{ week, base, amount string }{{"2026-09-28", "-100", "-75"}, {"2026-10-05", "-100", "-125"}, {"2026-10-12", "-100", "-100"}} {
			exec(`INSERT INTO pay_source_collections(source,owner_id,week_start,kind,statement_id,truck_id,name,base,amount,statement) VALUES($1,$2,$3::date,'driver',$4,$5,'Admin fee',$6::numeric,$7::numeric,'{}')`, source, oldOwner, r.week, oldDriver, truck, r.base, r.amount)
		}
		fleet := NewFleetRepository(pool)
		in := TruckInput{UnitNumber: "CORRECT-FEES", Active: true, Status: "available", OwnerID: &newOwner, OwnershipEffectiveDate: "2026-10-07", StatusActor: actor}
		changed, err := fleet.UpdateTruck(ctx, truck, in)
		if err != nil {
			t.Fatal(err)
		}
		if v := id(`SELECT base::text||'/'||amount::text FROM pay_source_collections WHERE truck_id=$1 AND week_start='2026-09-28'`, truck); v != "-100.00/-75.00" {
			t.Fatal("earlier fee changed", v)
		}
		if v := id(`SELECT base::text||'/'||amount::text FROM pay_source_collections WHERE truck_id=$1 AND week_start='2026-10-05'`, truck); v != "0.00/-25.00" {
			t.Fatal("earlier debt was lost or new fees retained", v)
		}
		if v := id(`SELECT count(*)::text FROM pay_source_collections WHERE truck_id=$1 AND week_start='2026-10-12'`, truck); v != "0" {
			t.Fatal("invalid fee retained", v)
		}
		if v := id(`SELECT count(*)::text FROM truck_settlement_events WHERE truck_id=$1 AND action='ownership_fee_correction'`, truck); v != "2" {
			t.Fatal("missing correction audits", v)
		}
		weekAfter, _ := chargeWeek("2026-10-12")
		report, err := NewDriverPayRepository(pool).InvestorPay(ctx, weekAfter, truck)
		if err != nil {
			t.Fatal(err)
		}
		for _, card := range report.Drivers {
			for _, fee := range card.AutoCharges {
				if fee.Source == source {
					t.Fatal("stale investor terms generated another fee for the former owner", fee)
				}
			}
		}
		// Re-saving an already corrected date also repairs allocations saved before
		// this release, and stale payroll edits cannot recreate them.
		exec(`INSERT INTO pay_source_collections(source,owner_id,week_start,kind,statement_id,truck_id,name,base,amount,statement) VALUES($1,$2,'2026-10-19','driver',$3,$4,'Admin fee',-100,-100,'{}')`, source, oldOwner, oldDriver, truck)
		in.ExpectedOwnershipVersion = changed.OwnershipVersion
		if _, err = fleet.UpdateTruck(ctx, truck, in); err != nil {
			t.Fatal(err)
		}
		if v := id(`SELECT count(*)::text FROM pay_source_collections WHERE truck_id=$1 AND week_start='2026-10-19'`, truck); v != "0" {
			t.Fatal("same-date repair failed")
		}
		// A later frozen collection depends on earlier debt and blocks the entire
		// correction, including the ownership boundary itself.
		exec(`INSERT INTO pay_source_collections(source,owner_id,week_start,kind,statement_id,truck_id,name,base,amount,statement) VALUES($1,$2,'2026-10-19','driver',$3,$4,'Admin fee',-100,-100,'{}')`, source, oldOwner, oldDriver, truck)
		exec(`INSERT INTO payroll_settlements(driver_id,week_start,report) VALUES($1,'2026-10-26',jsonb_build_object('autoCharges',jsonb_build_array(jsonb_build_object('source',$2::text))))`, oldDriver, source)
		in.OwnershipEffectiveDate = "2026-10-06"
		if _, err = fleet.UpdateTruck(ctx, truck, in); err == nil {
			t.Fatal("frozen future collection allowed correction")
		}
		unchanged, err := fleet.GetTruck(ctx, truck)
		if err != nil || unchanged.OwnerSince == nil || *unchanged.OwnerSince != "2026-10-07" {
			t.Fatal("failed correction changed history", err)
		}
		if v := id(`SELECT amount::text FROM pay_source_collections WHERE truck_id=$1 AND week_start='2026-10-19'`, truck); v != "-100.00" {
			t.Fatal("failed correction changed ledger")
		}
	})
}

func TestExpensePayerCorrectionsDatabase(t *testing.T) {
	withTruckChargeDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, actor string) {
		exec := func(q string, args ...any) {
			t.Helper()
			if _, err := pool.Exec(ctx, q, args...); err != nil {
				t.Fatal(err)
			}
		}
		id := func(q string, args ...any) string {
			t.Helper()
			var v string
			if err := pool.QueryRow(ctx, q, args...).Scan(&v); err != nil {
				t.Fatal(err)
			}
			return v
		}
		driver := id(`INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate) VALUES('Hired operator','hired operator','cpm',0.7) RETURNING id::text`)
		ownerDriver := id(`INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate) VALUES('Actual owner','actual owner','gross_percentage',88) RETURNING id::text`)
		owner := id(`INSERT INTO investors(full_name,driver_id) VALUES('Actual owner',$1) RETURNING id::text`, ownerDriver)
		truck := id(`INSERT INTO trucks(unit_number,owner_id) VALUES('CORRECT-EXPENSE',$1) RETURNING id::text`, owner)
		exec(`UPDATE truck_ownership_history SET assigned_at='2026-09-21 04:00+00',start_known=true WHERE truck_id=$1`, truck)
		exec(`INSERT INTO truck_driver_assignments(truck_id,driver_id,assigned_at) VALUES($1,$2,'2026-09-21 04:00+00')`, truck, driver)
		exec(`INSERT INTO truck_settlement_terms(truck_id,owner_id,week_start,share_percent) VALUES($1,$2,'2026-10-26',88)`, truck, owner)
		category := id(`SELECT id::text FROM expense_settings WHERE kind='category' AND name='Other'`)
		date, _ := time.Parse(time.DateOnly, "2026-10-28")
		repo := NewExpenseRepository(pool)
		pay := NewDriverPayRepository(pool)
		input := ExpenseInput{CategoryID: category, TruckID: &truck, DriverID: &driver, ExpenseDate: date, Amount: "836.01", ExpenseType: payTestString("Brake issue"), CoveredBy: payTestString("Driver"), CreatedBy: actor}
		expense, err := repo.CreateExpense(ctx, input)
		if err != nil {
			t.Fatal(err)
		}
		exec(`INSERT INTO expense_payments(expense_id,week_start,amount,updated_by) VALUES($1,'2026-10-19',100,$2),($1,'2026-10-26',200,$2),($1,'2026-11-02',0,$2)`, expense.ID, actor)
		expense, err = repo.GetExpense(ctx, expense.ID)
		if err != nil {
			t.Fatal(err)
		}
		input.UpdatedAt = &expense.UpdatedAt
		input.CoveredBy = payTestString("Truck Owner")
		input.OwnerID = &owner
		stale := input
		oldWeek, _ := chargeWeek("2026-10-19")
		oldReport, err := pay.GetDriverWeek(ctx, oldWeek, driver)
		if err != nil || len(oldReport.Drivers) != 1 {
			t.Fatal("original statement missing", err)
		}
		stalePay := oldReport.Drivers[0].Edits
		for i := range stalePay.ExpenseDeductions {
			stalePay.ExpenseDeductions[i].Apply = true
		}
		changed, err := repo.UpdateExpense(ctx, expense.ID, input)
		if err != nil {
			t.Fatal(err)
		}
		if changed.ChargeDriverID == nil || *changed.ChargeDriverID != ownerDriver || changed.OwnerID == nil || *changed.OwnerID != owner {
			t.Fatal("incorrect responsible person", changed)
		}
		if v := id(`SELECT count(*)::text||'/'||sum(amount)::text||'/'||bool_and(investor_truck_id=$2)::text FROM expense_payments WHERE expense_id=$1`, expense.ID, truck); v != "3/300.00/true" {
			t.Fatal("partial/zero allocations changed or duplicated", v)
		}
		if v := id(`SELECT count(*)::text FROM access_audit WHERE target_id=$1 AND action='expense.payer_corrected'`, expense.ID); v != "1" {
			t.Fatal("payer audit missing", v)
		}
		if _, err = repo.UpdateExpense(ctx, expense.ID, stale); !errors.Is(err, ErrExpenseSettingConflict) {
			t.Fatal("stale edit accepted", err)
		}
		if _, err = pay.Save(ctx, stalePay); !errors.Is(err, ErrDriverPayConflict) {
			t.Fatal("stale payroll restored old payer", err)
		}
		for _, week := range []string{"2026-10-19", "2026-10-26", "2026-11-02"} {
			w, _ := chargeWeek(week)
			result, err := pay.InvestorPay(ctx, w, truck)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, card := range result.Drivers {
				if card.ID == truck {
					for _, deduction := range card.Edits.ExpenseDeductions {
						if deduction.ExpenseID == expense.ID {
							found = true
							if !deduction.Saved {
								t.Fatal("transferred deduction is not saved")
							}
						}
					}
					if week == "2026-10-19" && (len(card.Loads) != 0 || len(card.Issues) == 0) {
						t.Fatal("missing agreement invented earnings")
					}
				}
			}
			if !found {
				t.Fatal("transferred expense hidden", week)
			}
			for _, setup := range result.SetupRequired {
				if setup.TruckID == truck {
					t.Fatal("transferred expense duplicated as setup row")
				}
			}
			selected, err := pay.InvestorStatement(ctx, w, truck)
			if err != nil || len(selected.Drivers) != 1 {
				t.Fatal("selected statement lost correction", week, err)
			}
			drivers, err := pay.Get(ctx, w)
			if err != nil {
				t.Fatal(err)
			}
			for _, card := range drivers.Drivers {
				for _, deduction := range card.Edits.ExpenseDeductions {
					if deduction.ExpenseID == expense.ID {
						t.Fatal("expense deducted on both pages", week)
					}
				}
			}
		}
		history, err := pay.InvestorHistory(ctx, owner, Pagination{Page: 1, PageSize: 100})
		if err != nil {
			t.Fatal(err)
		}
		foundHistory := false
		for _, entry := range history.Items {
			if entry.WeekStart == "2026-10-19" {
				foundHistory = len(entry.Trucks) > 0
			}
		}
		if !foundHistory {
			t.Fatal("reassigned historical expense missing from investor profile history")
		}
		// The same original allocations can move back to a driver's statement.
		input.UpdatedAt = &changed.UpdatedAt
		input.CoveredBy = payTestString("Driver")
		input.OwnerID = nil
		changed, err = repo.UpdateExpense(ctx, expense.ID, input)
		if err != nil {
			t.Fatal(err)
		}
		if v := id(`SELECT bool_and(investor_truck_id IS NULL)::text||'/'||sum(amount)::text FROM expense_payments WHERE expense_id=$1`, expense.ID); v != "true/300.00" {
			t.Fatal("reverse transfer failed", v)
		}
		// Frozen targets and frozen sources reject the correction without releasing
		// the old reservation, even when another payment week is still open.
		exec(`INSERT INTO investor_pay_weeks(truck_id,owner_id,week_start,edits,report,finalized) VALUES($1,$2,'2026-10-26','{}','{}',true)`, truck, owner)
		input.UpdatedAt = &changed.UpdatedAt
		input.CoveredBy = payTestString("Truck Owner")
		input.OwnerID = &owner
		if _, err = repo.UpdateExpense(ctx, expense.ID, input); !errors.Is(err, ErrExpensePayerCorrection) {
			t.Fatal("frozen target accepted", err)
		}
		exec(`UPDATE investor_pay_weeks SET finalized=false WHERE truck_id=$1`, truck)
		exec(`INSERT INTO payroll_settlements(driver_id,week_start,report) VALUES($1,'2026-10-19','{}')`, driver)
		if _, err = repo.UpdateExpense(ctx, expense.ID, input); !errors.Is(err, ErrExpensePayerCorrection) {
			t.Fatal("frozen source accepted", err)
		}
		exec(`UPDATE payroll_settlements SET finalized=false WHERE driver_id=$1`, driver)
		input.Amount = "900"
		if _, err = repo.UpdateExpense(ctx, expense.ID, input); !errors.Is(err, ErrExpensePayerCorrection) {
			t.Fatal("mixed principal/payer edit accepted", err)
		}
		if v := id(`SELECT bool_and(investor_truck_id IS NULL)::text||'/'||sum(amount)::text FROM expense_payments WHERE expense_id=$1`, expense.ID); v != "true/300.00" {
			t.Fatal("failed correction released principal", v)
		}
		// Legacy EFS balances include an implicit fee. A payer change must not
		// reduce that principal, even if the saved deduction was only partial.
		legacyInput := ExpenseInput{CategoryID: category, TruckID: &truck, DriverID: &driver, ExpenseDate: date, Amount: "100", ExpenseType: payTestString("Legacy EFS repair"), CoveredBy: payTestString("Driver"), PaymentType: payTestString("EFS"), CreatedBy: actor}
		legacy, err := repo.CreateExpense(ctx, legacyInput)
		if err != nil {
			t.Fatal(err)
		}
		exec(`INSERT INTO expense_payments(expense_id,week_start,amount) VALUES($1,'2026-10-19',25)`, legacy.ID)
		legacyInput.CoveredBy = payTestString("Truck Owner")
		legacyInput.OwnerID = &owner
		legacyInput.BaseAmount = payTestString("100")
		if _, err = repo.UpdateExpense(ctx, legacy.ID, legacyInput); !errors.Is(err, ErrExpensePayerCorrection) {
			t.Fatal("payer change dropped the legacy EFS fee", err)
		}
		legacyInput.BaseAmount = nil
		if _, err = repo.UpdateExpense(ctx, legacy.ID, legacyInput); err != nil {
			t.Fatal("legacy EFS payer correction failed", err)
		}
		if v := id(`SELECT amount::text||'/'||(investor_truck_id IS NOT NULL)::text FROM expense_payments WHERE expense_id=$1`, legacy.ID); v != "25.00/true" {
			t.Fatal("legacy EFS allocation changed", v)
		}
	})
}
