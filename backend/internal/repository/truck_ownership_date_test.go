package repository

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestTruckOwnershipDateDatabase(t *testing.T) {
	withTruckChargeDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, actor string) {
		fleet := NewFleetRepository(pool)
		driver, err := fleet.CreateDriver(ctx, DriverInput{FullName: "Dated owner", PayType: "gross_percentage", PayRate: 88, Active: true})
		if err != nil {
			t.Fatal(err)
		}
		truck, err := fleet.CreateTruck(ctx, TruckInput{UnitNumber: "DATED", Active: true, Status: "assigned", DriverID: &driver.ID, AssignmentWeek: "2026-09-21"})
		if err != nil {
			t.Fatal(err)
		}
		// Match the real migrated baseline: timestamp is observation, not acquisition.
		if _, err = pool.Exec(ctx, `UPDATE truck_ownership_history SET start_known=false WHERE truck_id=$1`, truck.ID); err != nil {
			t.Fatal(err)
		}
		input := TruckInput{UnitNumber: truck.UnitNumber, Active: true, Status: "assigned", DriverID: &driver.ID, OwnerDriverID: &driver.ID, OwnershipEffectiveDate: "2026-10-05"}
		truck, err = fleet.UpdateTruck(ctx, truck.ID, input)
		if err != nil {
			t.Fatal(err)
		}
		readType := func(day string) string {
			t.Helper()
			var value string
			if err := pool.QueryRow(ctx, `SELECT driver_employment_type($1,$2::date)`, driver.ID, day).Scan(&value); err != nil {
				t.Fatal(err)
			}
			return value
		}
		if readType("2026-10-04") != "company" || readType("2026-10-11") != "owner_operator" {
			t.Fatal("dated transfer ignored")
		}
		input.OwnerDriverID = nil
		input.OwnerID = &truck.OwnerID
		input.ExpectedOwnershipVersion = truck.OwnershipVersion
		input.OwnershipEffectiveDate = "2026-09-27"
		corrected, err := fleet.UpdateTruck(ctx, truck.ID, input)
		if err != nil || corrected.OwnerSince == nil || *corrected.OwnerSince != "2026-09-27" {
			t.Fatal("correct owner start", corrected, err)
		}
		if readType("2026-09-26") != "company" || readType("2026-09-27") != "owner_operator" || readType("2026-10-04") != "owner_operator" {
			t.Fatal("corrected boundary ignored")
		}
		if _, err = fleet.UpdateTruck(ctx, truck.ID, input); err == nil {
			t.Fatal("stale correction accepted")
		}
		input.ExpectedOwnershipVersion = corrected.OwnershipVersion
		if _, err = fleet.UpdateTruck(ctx, truck.ID, input); err != nil {
			t.Fatal("unchanged save failed", err)
		}
		var overlaps int
		if err = pool.QueryRow(ctx, `SELECT count(*) FROM truck_ownership_history a JOIN truck_ownership_history b ON a.truck_id=b.truck_id AND a.id<>b.id WHERE a.truck_id=$1 AND a.assigned_at<coalesce(b.unassigned_at,'infinity') AND b.assigned_at<coalesce(a.unassigned_at,'infinity')`, truck.ID).Scan(&overlaps); err != nil || overlaps != 0 {
			t.Fatal("overlapping history", err)
		}
		if _, err = pool.Exec(ctx, `INSERT INTO payroll_settlements(driver_id,week_start,report) VALUES($1,'2026-09-28','{}')`, driver.ID); err != nil {
			t.Fatal(err)
		}
		input.OwnershipEffectiveDate = "2026-10-01"
		if _, err = fleet.UpdateTruck(ctx, truck.ID, input); err == nil {
			t.Fatal("finalized period changed")
		}
		if _, err = pool.Exec(ctx, `UPDATE payroll_settlements SET finalized=false WHERE driver_id=$1`, driver.ID); err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `INSERT INTO investor_pay_weeks(truck_id,week_start,owner_id,edits,finalized,report)
 SELECT id,'2026-09-28',owner_id,'{}',true,'{}' FROM trucks WHERE id=$1`, truck.ID); err != nil {
			t.Fatal(err)
		}
		if _, err = fleet.UpdateTruck(ctx, truck.ID, input); err == nil {
			t.Fatal("finalized investor period changed")
		}
		unchanged, err := fleet.GetTruck(ctx, truck.ID)
		if err != nil || unchanged.OwnerSince == nil || *unchanged.OwnerSince != "2026-09-27" || unchanged.OwnershipVersion != corrected.OwnershipVersion {
			t.Fatal("failed correction changed ownership", err)
		}
		input.OwnershipEffectiveDate = "2099-01-01"
		if _, err = fleet.UpdateTruck(ctx, truck.ID, input); err == nil {
			t.Fatal("future date accepted")
		}
		input.OwnershipEffectiveDate = "2026-02-30"
		if _, err = fleet.UpdateTruck(ctx, truck.ID, input); err == nil {
			t.Fatal("invalid date accepted")
		}
		// A genuine later transfer cannot consume an earlier known ownership period.
		company := "00000000-0000-0000-0000-000000000001"
		input.OwnerID = &company
		input.OwnershipEffectiveDate = "2026-09-20"
		if _, err = fleet.UpdateTruck(ctx, truck.ID, input); err == nil {
			t.Fatal("earlier known transfer overwritten")
		}
	})
}
