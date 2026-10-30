package repository

import (
	"context"
	"sort"

	"github.com/jackc/pgx/v5"
)

// A corrected deduction keeps its original week and amount even if the owner's
// earnings agreement starts later. Expose that allocation as an expense-only
// Investor Pay statement, without inventing a rate or moving driver earnings.
func routeCorrectedInvestorExpenses(ctx context.Context, tx pgx.Tx, investors *DriverPayWeek) error {
	if payrollCostsOnly(ctx) {
		return nil
	}
	rows, err := tx.Query(ctx, `SELECT DISTINCT t.id::text,t.unit_number,e.owner_id::text,coalesce(d.full_name,i.full_name),coalesce(i.driver_id::text,''),NOT t.active
 FROM expense_payments p JOIN expenses e ON e.id=p.expense_id JOIN trucks t ON t.id=p.investor_truck_id
 JOIN investors i ON i.id=e.owner_id LEFT JOIN drivers d ON d.id=i.driver_id
 WHERE p.week_start=$1::date AND e.truck_id=p.investor_truck_id AND lower(btrim(e.covered_by))='truck owner'
 AND (NOT e.driver_settled OR expense_balance_adjustment(e.id)<>0) AND ($2::uuid[] IS NULL OR t.id=ANY($2::uuid[])) ORDER BY t.id::text`, investors.WeekStart, payrollTruckIDs(ctx))
	if err != nil {
		return err
	}
	cards := []DriverPayDriver{}
	for rows.Next() {
		var d DriverPayDriver
		if err = rows.Scan(&d.ID, &d.TruckUnit, &d.InvestorID, &d.FullName, &d.ProfileDriverID, &d.TruckInactive); err != nil {
			rows.Close()
			return err
		}
		cards = append(cards, d)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, d := range cards {
		found := false
		for _, existing := range investors.Drivers {
			if existing.ID == d.ID {
				found = true
				break
			}
		}
		if found {
			continue
		}
		d.TruckID = d.ID
		d.IsOwnerOperator = true
		d.PayType = "gross_percentage"
		d.PayRate = ""
		d.FuelTotal = "0.00"
		d.TollTotal = "0.00"
		d.Loads = []DriverPayLoad{}
		d.Edits = DriverPayEdits{DriverID: d.ID, WeekStart: investors.WeekStart, Comments: map[string]string{}, Adjustments: []DriverPayAdjustment{}}
		d.Edits.ExpenseDeductions, err = investorExpenseDeductions(ctx, tx, d.ID, d.InvestorID, investors.WeekStart, false)
		if err != nil {
			return err
		}
		d.Issues = []string{"This statement contains reassigned expense deductions only; configure the dated investor agreement before finalizing earnings and truck fees"}
		if err = readInvestorEdits(ctx, tx, &d); err != nil {
			return err
		}
		investors.Drivers = append(investors.Drivers, d)
	}
	sort.SliceStable(investors.Drivers, func(i, j int) bool {
		a, b := investors.Drivers[i], investors.Drivers[j]
		if a.FullName != b.FullName {
			return a.FullName < b.FullName
		}
		return a.TruckUnit < b.TruckUnit
	})
	return nil
}
