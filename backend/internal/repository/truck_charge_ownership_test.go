package repository

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestTruckChargeOwnershipDatabase(t *testing.T) {
	withTruckChargeDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, actor string) {
		fleet := NewFleetRepository(pool)
		charges := NewDriverChargeRepository(pool)
		first, err := fleet.SaveInvestor(ctx, "", InvestorInput{FullName: "Former owner", Active: true})
		if err != nil {
			t.Fatal(err)
		}
		next, err := fleet.SaveInvestor(ctx, "", InvestorInput{FullName: "New owner", Active: true})
		if err != nil {
			t.Fatal(err)
		}
		truck, err := fleet.CreateTruck(ctx, TruckInput{UnitNumber: "OWNERSHIP", OwnerID: &first.ID, Active: true, Status: "available", OwnershipEffectiveDate: "2026-09-21"})
		if err != nil {
			t.Fatal(err)
		}
		if err = charges.SaveTruckTerm(ctx, TruckTerm{TruckID: truck.ID, OwnerID: first.ID, WeekStart: "2026-09-21", SharePercent: "88"}, actor); err != nil {
			t.Fatal(err)
		}
		input := TruckInput{UnitNumber: truck.UnitNumber, OwnerID: &next.ID, Active: true, Status: "available", OwnershipEffectiveDate: "2026-09-30"}
		truck, err = fleet.UpdateTruck(ctx, truck.ID, input)
		if err != nil {
			t.Fatal(err)
		}
		check := func(week, ownerID, ownerName string) {
			t.Helper()
			data, err := charges.TruckChargesForWeek(ctx, week)
			if err != nil {
				t.Fatal(err)
			}
			if ownerID == "" {
				if len(data.EligibleTruckIDs) != 0 || len(data.Owners) != 0 {
					t.Fatalf("company or future acquisition included in %s: %+v", week, data)
				}
				return
			}
			if len(data.Owners) != 1 || data.Owners[0] != (TruckChargeOwner{TruckID: truck.ID, OwnerID: ownerID, OwnerName: ownerName}) || len(data.EligibleTruckIDs) != 1 || data.EligibleTruckIDs[0] != truck.ID {
				t.Fatalf("wrong dated owner for %s: %+v", week, data)
			}
			if len(data.Terms) != 1 || data.Terms[0].OwnerID != first.ID || data.Terms[0].SharePercent != "88.0000" || data.Terms[0].Version != 1 {
				t.Fatalf("ownership read changed the separate agreement: %+v", data.Terms)
			}
		}
		check("2026-09-14", "", "")
		check("2026-09-21", first.ID, first.FullName)
		check("2026-09-28", next.ID, next.FullName)
		// A migrated unknown start is an observation date, even when an earlier
		// backdated transfer moved its stored timestamp to the closing boundary.
		if _, err = pool.Exec(ctx, `UPDATE truck_ownership_history SET start_known=false,assigned_at=unassigned_at WHERE truck_id=$1 AND owner_id=$2`, truck.ID, first.ID); err != nil {
			t.Fatal(err)
		}
		check("2026-09-14", first.ID, first.FullName)
		// Correcting the same owner's date also updates historical charge views.
		input.ExpectedOwnershipVersion = truck.OwnershipVersion
		input.OwnershipEffectiveDate = "2026-10-05"
		if _, err = fleet.UpdateTruck(ctx, truck.ID, input); err != nil {
			t.Fatal(err)
		}
		check("2026-09-28", first.ID, first.FullName)
		check("2026-10-05", next.ID, next.FullName)
		// Historical non-company trucks remain visible after transfer to company.
		company := CompanyOwnerID
		input.OwnerID, input.ExpectedOwnershipVersion, input.OwnershipEffectiveDate = &company, "", "2026-10-12"
		if _, err = fleet.UpdateTruck(ctx, truck.ID, input); err != nil {
			t.Fatal(err)
		}
		check("2026-10-05", next.ID, next.FullName)
		check("2026-10-12", "", "")
		if _, err = charges.TruckChargesForWeek(ctx, "2026-10-06"); err == nil {
			t.Fatal("non-Monday accepted")
		}
	})
}
