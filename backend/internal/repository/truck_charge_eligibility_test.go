package repository

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestTruckChargeHistoricalEligibilityDatabase(t *testing.T) {
	withTruckChargeDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, actor string) {
		fleet := NewFleetRepository(pool)
		charges := NewDriverChargeRepository(pool)
		week := ChargeCurrentWeek()
		date, _ := chargeWeek(week)
		oldWeek := date.AddDate(0, 0, -14).Format("2006-01-02")
		company := "00000000-0000-0000-0000-000000000001"
		var owner string
		if err := pool.QueryRow(ctx, `INSERT INTO investors(full_name) VALUES('Historical charge owner') RETURNING id::text`).Scan(&owner); err != nil {
			t.Fatal(err)
		}
		transferred, err := fleet.CreateTruck(ctx, TruckInput{UnitNumber: "OLD-INVESTOR", OwnerID: &owner, Active: true, Status: "available", OwnershipEffectiveDate: oldWeek})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = fleet.UpdateTruck(ctx, transferred.ID, TruckInput{UnitNumber: transferred.UnitNumber, OwnerID: &company, Active: true, Status: "available"}); err != nil {
			t.Fatal(err)
		}
		reverse, err := fleet.CreateTruck(ctx, TruckInput{UnitNumber: "OLD-COMPANY", OwnerID: &company, Active: true, Status: "available", OwnershipEffectiveDate: oldWeek})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = fleet.UpdateTruck(ctx, reverse.ID, TruckInput{UnitNumber: reverse.UnitNumber, OwnerID: &owner, Active: true, Status: "available"}); err != nil {
			t.Fatal(err)
		}
		management, err := charges.TruckChargesForWeek(ctx, oldWeek)
		if err != nil {
			t.Fatal(err)
		}
		eligible := map[string]bool{}
		for _, id := range management.EligibleTruckIDs {
			eligible[id] = true
		}
		if !eligible[transferred.ID] || eligible[reverse.ID] {
			t.Fatal("directory did not use historical ownership", management)
		}
		typ, err := charges.SaveType(ctx, ChargeType{Name: "Historical fee", Direction: "charge", Amounts: []string{"50"}}, actor)
		if err != nil {
			t.Fatal(err)
		}
		phase := TruckChargePhase{TruckID: transferred.ID, TypeID: typ.ID, WeekStart: oldWeek, Amount: "50", Included: true, TypeVersion: typ.Version}
		blocked := phase
		blocked.TruckID = reverse.ID
		results, err := charges.SaveTruckCharges(ctx, []TruckChargePhase{blocked, phase}, actor)
		if err != nil || len(results) != 2 || results[0].Error == "" || results[1].Error != "" {
			t.Fatalf("historical bulk eligibility: %+v %v", results, err)
		}
		if err = charges.SaveTruckTerm(ctx, TruckTerm{TruckID: transferred.ID, OwnerID: owner, WeekStart: oldWeek, SharePercent: "88"}, actor); err != nil {
			t.Fatal("historical investor terms blocked by current company ownership", err)
		}
		phase.WeekStart, phase.Version = week, 0
		if err = charges.SaveTruckCharge(ctx, phase, actor); err == nil {
			t.Fatal("current company truck accepted a new fee")
		}
		var saved bool
		if err = pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM truck_charge_phases WHERE truck_id=$1 AND week_start=$3::date AND amount=50 AND included)
 AND NOT EXISTS(SELECT 1 FROM truck_charge_phases WHERE truck_id=$2)`, transferred.ID, reverse.ID, oldWeek).Scan(&saved); err != nil || !saved {
			t.Fatal("historical source writes did not match selected-week eligibility", saved, err)
		}
	})
}
