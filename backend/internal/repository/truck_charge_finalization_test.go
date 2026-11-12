package repository

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestTruckRecurringFormerOwnerFinalizationDatabase(t *testing.T) {
	withTruckChargeDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, actor string) {
		fleet := NewFleetRepository(pool)
		charges := NewDriverChargeRepository(pool)
		const prior = "2026-09-21"
		const week = "2026-11-02"
		driver, err := fleet.CreateDriver(ctx, DriverInput{FullName: "Former owner now hired driver", PayType: "cpm", Active: true, AssignmentWeek: prior})
		if err != nil {
			t.Fatal(err)
		}
		owner, err := fleet.SaveInvestor(ctx, "", InvestorInput{FullName: "Current investor", Active: true})
		if err != nil {
			t.Fatal(err)
		}
		typ, err := charges.SaveType(ctx, ChargeType{Name: "Recurring insurance", Direction: "charge", Amounts: []string{"50", "75"}}, actor)
		if err != nil {
			t.Fatal(err)
		}
		var trucks []string
		for _, transfer := range []string{prior, week} {
			truck, err := fleet.CreateTruck(ctx, TruckInput{UnitNumber: "TRANSFER-" + transfer, OwnerDriverID: &driver.ID, DriverID: &driver.ID, Active: true, Status: "assigned", AssignmentWeek: prior, OwnershipEffectiveDate: prior})
			if err != nil {
				t.Fatal(err)
			}
			// Stale agreements and zero-duration ownership corrections must not
			// make the former owner's later wages own the investor's charges.
			if err = charges.SaveTruckTerm(ctx, TruckTerm{TruckID: truck.ID, OwnerID: truck.OwnerID, WeekStart: prior, SharePercent: "88"}, actor); err != nil {
				t.Fatal(err)
			}
			if _, err = fleet.UpdateTruck(ctx, truck.ID, TruckInput{UnitNumber: truck.UnitNumber, OwnerID: &owner.ID, DriverID: &driver.ID, Active: true, Status: "assigned", OwnershipEffectiveDate: transfer}); err != nil {
				t.Fatal(err)
			}
			trucks = append(trucks, truck.ID)
		}
		if _, err = pool.Exec(ctx, `INSERT INTO payroll_settlements(driver_id,week_start,report) VALUES($1,$2,'{"frozen":"hired driver"}')`, driver.ID, week); err != nil {
			t.Fatal(err)
		}
		checkLocks := func(want bool) {
			t.Helper()
			data, err := charges.TruckChargesForWeek(ctx, week)
			if err != nil {
				t.Fatal(err)
			}
			for _, truck := range trucks {
				locked := false
				for _, cell := range data.LockedCharges {
					locked = locked || cell.TruckID == truck && cell.TypeID == typ.ID
				}
				if locked != want {
					t.Fatalf("truck %s locked=%v, want %v", truck, locked, want)
				}
			}
		}
		checkLocks(false)
		// A real prior owner-driver week is still protected. An unknown start
		// is historical evidence, even if its observation time equals its end.
		if _, err = pool.Exec(ctx, `INSERT INTO payroll_settlements(driver_id,week_start,report) VALUES($1,$2,'{"frozen":"prior owner"}')`, driver.ID, prior); err != nil {
			t.Fatal(err)
		}
		for _, unknown := range []bool{false, true} {
			if unknown {
				if _, err = pool.Exec(ctx, `UPDATE truck_ownership_history SET start_known=false,assigned_at=unassigned_at WHERE truck_id=$1 AND unassigned_at IS NOT NULL`, trucks[1]); err != nil {
					t.Fatal(err)
				}
			}
			data, err := charges.TruckChargesForWeek(ctx, prior)
			if err != nil {
				t.Fatal(err)
			}
			locked := map[string]bool{}
			for _, cell := range data.LockedCharges {
				if cell.TypeID == typ.ID {
					locked[cell.TruckID] = true
				}
			}
			if locked[trucks[0]] || !locked[trucks[1]] {
				t.Fatalf("wrong historical ownership locks (unknown=%v): %+v", unknown, locked)
			}
		}
		checkLocks(false)
		var updates []TruckChargePhase
		for _, truck := range trucks {
			phase := TruckChargePhase{TruckID: truck, TypeID: typ.ID, WeekStart: week, Amount: "50", Included: true, TypeVersion: typ.Version}
			if err = charges.SaveTruckCharge(ctx, phase); err != nil {
				t.Fatal("hired driver's finalized wages blocked investor fee", err)
			}
			phase.Version, phase.Amount = 1, "75"
			updates = append(updates, phase)
		}
		results, err := charges.SaveTruckCharges(ctx, updates)
		if err != nil || len(results) != len(trucks) {
			t.Fatal(results, err)
		}
		for _, result := range results {
			if result.Error != "" {
				t.Fatal(result.Error)
			}
		}
		var unchanged bool
		if err = pool.QueryRow(ctx, `SELECT finalized AND version=1 AND report='{"frozen":"hired driver"}'::jsonb FROM payroll_settlements WHERE driver_id=$1 AND week_start=$2`, driver.ID, week).Scan(&unchanged); err != nil || !unchanged {
			t.Fatal("driver statement changed", unchanged, err)
		}
		for _, truck := range trucks {
			if _, err = pool.Exec(ctx, `INSERT INTO investor_pay_weeks(truck_id,week_start,owner_id,edits,report,finalized) VALUES($1,$2,$3,'{}','{"frozen":"investor"}',true)`, truck, week, owner.ID); err != nil {
				t.Fatal(err)
			}
		}
		checkLocks(true)
		for _, update := range updates {
			update.Version = 2
			update.Amount = "50"
			if err = charges.SaveTruckCharge(ctx, update); err == nil {
				t.Fatal("finalized investor fee was editable")
			}
		}
	})
}
