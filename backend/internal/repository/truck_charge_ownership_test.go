package repository

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestTruckOwnershipTransferPayrollDatabase(t *testing.T) {
	withTruckChargeDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, actor string) {
		const priorWeek = "2026-10-19"
		const transferWeek = "2026-10-26"
		fleet := NewFleetRepository(pool)
		charges := NewDriverChargeRepository(pool)
		pay := NewDriverPayRepository(pool)
		former, err := fleet.SaveInvestor(ctx, "", InvestorInput{FullName: "Former investor", Active: true})
		if err != nil {
			t.Fatal(err)
		}
		driver, err := fleet.CreateDriver(ctx, DriverInput{FullName: "Buying driver", PayType: "gross_percentage", PayRate: 87, Active: true, AssignmentWeek: priorWeek})
		if err != nil {
			t.Fatal(err)
		}
		truck, err := fleet.CreateTruck(ctx, TruckInput{UnitNumber: "OWNER-TRANSFER", OwnerID: &former.ID, DriverID: &driver.ID, Active: true, Status: "assigned", AssignmentWeek: priorWeek, OwnershipEffectiveDate: priorWeek})
		if err != nil {
			t.Fatal(err)
		}
		term := TruckTerm{TruckID: truck.ID, OwnerID: former.ID, WeekStart: priorWeek, SharePercent: "88"}
		if err := charges.SaveTruckTerm(ctx, term, actor); err != nil {
			t.Fatal(err)
		}
		insurance, err := charges.SaveType(ctx, ChargeType{Name: "Insurance", Direction: "charge", Amounts: []string{"375"}, Eligibility: "loads"}, actor)
		if err != nil {
			t.Fatal(err)
		}
		if err := charges.SaveTruckCharge(ctx, TruckChargePhase{TruckID: truck.ID, TypeID: insurance.ID, WeekStart: priorWeek, Included: true, Amount: "375", TypeVersion: insurance.Version}, actor); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO gross_board_entries(driver_id,service_date,load_number,original_rate,miles,driver_rate)
 VALUES($1,'2026-10-20','BEFORE-PURCHASE',1000,100,1000),($1,'2026-10-27','AFTER-PURCHASE',1000,100,1000)`, driver.ID); err != nil {
			t.Fatal(err)
		}
		updated, err := fleet.UpdateTruck(ctx, truck.ID, TruckInput{UnitNumber: truck.UnitNumber, OwnerDriverID: &driver.ID, DriverID: &driver.ID, Active: true, Status: "assigned", OwnershipEffectiveDate: transferWeek})
		if err != nil || updated.OwnerID == former.ID {
			t.Fatal("transfer to the operating driver", updated.OwnerID, err)
		}

		prior, _ := time.Parse(time.DateOnly, priorWeek)
		current, _ := time.Parse(time.DateOnly, transferWeek)
		investor, err := pay.InvestorPay(ctx, prior)
		if err != nil || len(investor.Drivers) != 1 || investor.Drivers[0].InvestorID != former.ID ||
			len(investor.Drivers[0].Loads) != 1 || investor.Drivers[0].Loads[0].Fee != "880.00" {
			t.Fatalf("prior owner lost their agreement: %+v %v", investor, err)
		}
		investor, err = pay.InvestorPay(ctx, current)
		if err != nil || len(investor.Drivers) != 0 || len(investor.SetupRequired) != 0 {
			t.Fatalf("owner-driver week appeared in Investor Pay: %+v %v", investor, err)
		}
		driverPay, err := pay.Get(ctx, current)
		if err != nil || len(driverPay.Drivers) != 1 {
			t.Fatalf("missing owner-driver statement: %+v %v", driverPay, err)
		}
		card := driverPay.Drivers[0]
		if card.ID != driver.ID || card.PayRate != "87.0000" || len(card.Loads) != 1 || card.Loads[0].Fee != "870.00" ||
			len(card.AutoCharges) != 1 || card.AutoCharges[0].Amount != "-375.00" {
			t.Fatalf("owner-driver tariff or recurring charge: %+v", card)
		}
		management, err := charges.TruckChargesForWeek(ctx, transferWeek)
		if err != nil || len(management.Terms) != 1 || management.Terms[0].EndsBefore != transferWeek {
			t.Fatalf("recurring charges did not expose the agreement end: %+v %v", management.Terms, err)
		}
		term.WeekStart = transferWeek
		if err := charges.SaveTruckTerm(ctx, term, actor); err == nil {
			t.Fatal("saved terms for the former owner after the transfer")
		}

		if _, err := pool.Exec(ctx, `INSERT INTO investor_pay_weeks(truck_id,week_start,owner_id,edits)
 VALUES($1,$2::date,$3,'{}')`, truck.ID, transferWeek, former.ID); err != nil {
			t.Fatal(err)
		}
		investor, err = pay.InvestorPay(ctx, current)
		if err != nil || len(investor.Drivers) != 1 || investor.Drivers[0].InvestorID != former.ID ||
			len(investor.Drivers[0].Loads) != 0 || len(investor.Drivers[0].AutoCharges) != 0 || len(investor.Drivers[0].Issues) == 0 {
			t.Fatalf("saved former-owner draft should be visible for review without new earnings: %+v %v", investor, err)
		}
	})
}

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
			if week < "2026-09-21" {
				if len(data.Terms) != 0 {
					t.Fatalf("returned a future agreement for %s: %+v", week, data.Terms)
				}
				return
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
		input.OwnerID, input.OwnershipEffectiveDate = &first.ID, "2026-10-19"
		if _, err = fleet.UpdateTruck(ctx, truck.ID, input); err != nil {
			t.Fatal(err)
		}
		data, err := charges.TruckChargesForWeek(ctx, "2026-10-19")
		if err != nil || len(applicableTruckTerms(data, "2026-10-19")) != 0 {
			t.Fatalf("reacquisition revived an expired agreement: %+v %v", data.Terms, err)
		}
		if _, err = charges.TruckChargesForWeek(ctx, "2026-10-06"); err == nil {
			t.Fatal("non-Monday accepted")
		}
	})
}
