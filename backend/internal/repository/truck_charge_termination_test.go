package repository

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestTruckChargeTerminationDatabase(t *testing.T) {
	withTruckChargeDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, actor string) {
		fleet := NewFleetRepository(pool)
		charges := NewDriverChargeRepository(pool)
		pay := NewDriverPayRepository(pool)
		week := ChargeCurrentWeek()
		monday, _ := chargeWeek(week)
		prior := monday.AddDate(0, 0, -7).Format(time.DateOnly)
		future := monday.AddDate(0, 0, 7).Format(time.DateOnly)
		typ, err := charges.SaveType(ctx, ChargeType{Name: "Termination insurance", Direction: "charge", Amounts: []string{"50"}}, actor)
		if err != nil {
			t.Fatal(err)
		}
		makeTruck := func(name string, termination string, assigned bool) (Driver, Truck) {
			t.Helper()
			driver, err := fleet.CreateDriver(ctx, DriverInput{FullName: name, Active: true, PayType: "gross_percentage", PayRate: 88, AssignmentWeek: prior})
			if err != nil {
				t.Fatal(err)
			}
			input := TruckInput{UnitNumber: name, OwnerDriverID: &driver.ID, Active: true, Status: "available", AssignmentWeek: prior, OwnershipEffectiveDate: prior}
			if assigned {
				input.DriverID = &driver.ID
			}
			truck, err := fleet.CreateTruck(ctx, input)
			if err != nil {
				t.Fatal(err)
			}
			if err = charges.SaveTruckCharge(ctx, TruckChargePhase{TruckID: truck.ID, TypeID: typ.ID, WeekStart: prior, Amount: "50", Included: true, TypeVersion: typ.Version}, actor); err != nil {
				t.Fatal(err)
			}
			if err = charges.SaveTruckTerm(ctx, TruckTerm{TruckID: truck.ID, OwnerID: truck.OwnerID, WeekStart: prior, SharePercent: "88"}, actor); err != nil {
				t.Fatal(err)
			}
			if termination != "" {
				if _, err = pool.Exec(ctx, `UPDATE drivers SET status='terminated',termination_date=$2::date WHERE id=$1`, driver.ID, termination); err != nil {
					t.Fatal(err)
				}
			}
			return driver, truck
		}
		assertEligible := func(truck Truck, selected string, want bool) {
			t.Helper()
			data, err := charges.TruckChargesForWeek(ctx, selected)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, id := range data.EligibleTruckIDs {
				found = found || id == truck.ID
			}
			if found != want {
				t.Fatalf("%s eligible in %s = %v, want %v", truck.UnitNumber, selected, found, want)
			}
			for _, phase := range data.Phases {
				if !want && phase.TruckID == truck.ID {
					t.Fatal("hidden truck leaked phases into page", phase)
				}
			}
			config, err := truckChargeData(ctx, pool)
			if err != nil {
				t.Fatal(err)
			}
			config, err = truckChargesForEmploymentWeek(ctx, pool, config, selected)
			if err != nil {
				t.Fatal(err)
			}
			if got := len(truckRecurringCharges(config, []ChargeType{typ}, truck.ID, selected, false)) > 0; got != want {
				t.Fatalf("%s payroll fees in %s = %v, want %v", truck.UnitNumber, selected, got, want)
			}
		}
		// Monday, midweek, and Sunday all retain their own week and stop next Monday.
		var stopped Truck
		for _, offset := range []int{-7, -4, -1} {
			_, truck := makeTruck("STOP"+monday.AddDate(0, 0, offset).Format("0102"), monday.AddDate(0, 0, offset).Format(time.DateOnly), true)
			assertEligible(truck, prior, true)
			assertEligible(truck, week, false)
			assertEligible(truck, future, false)
			stopped = truck
		}
		_, mondayTruck := makeTruck("MONDAY", week, true)
		assertEligible(mondayTruck, week, true)
		assertEligible(mondayTruck, future, false)
		// An active investor's unassigned fleet does not follow their employment.
		_, investorTruck := makeTruck("INVESTOR", monday.AddDate(0, 0, -1).Format(time.DateOnly), false)
		assertEligible(investorTruck, week, true)
		// A continuing employed co-driver also preserves fees on a retained link.
		owner, teamTruck := makeTruck("TEAM", monday.AddDate(0, 0, -1).Format(time.DateOnly), true)
		person, err := fleet.CreateDriver(ctx, DriverInput{FullName: "Continuing crew", Active: true, PayType: "cpm", PayRate: 0.5})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `INSERT INTO driver_teams(main_driver_id,team_driver_id,start_week) VALUES($1,$2,$3::date)`, owner.ID, person.ID, week); err != nil {
			t.Fatal(err)
		}
		assertEligible(teamTruck, week, true)
		// A future hired assignment must not erase the intervening stopped week.
		_, reassigned := makeTruck("REASSIGNED", monday.AddDate(0, 0, -1).Format(time.DateOnly), true)
		hired, err := fleet.CreateDriver(ctx, DriverInput{FullName: "Replacement driver", Active: true, PayType: "cpm", PayRate: 0.5})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `UPDATE truck_driver_assignments SET unassigned_at=$2::date::timestamp AT TIME ZONE 'America/New_York' WHERE truck_id=$1`, reassigned.ID, future); err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `INSERT INTO truck_driver_assignments(truck_id,driver_id,assigned_at) VALUES($1,$2,$3::date::timestamp AT TIME ZONE 'America/New_York')`, reassigned.ID, hired.ID, future); err != nil {
			t.Fatal(err)
		}
		assertEligible(reassigned, week, false)
		assertEligible(reassigned, future, true)
		available, homeTruck := makeTruck("HOME", "", true)
		if _, err = pool.Exec(ctx, `UPDATE drivers SET status='home' WHERE id=$1`, available.ID); err != nil {
			t.Fatal(err)
		}
		assertEligible(homeTruck, week, true)
		// Both single and bulk APIs reject attempts to put stopped fees back.
		update := TruckChargePhase{TruckID: stopped.ID, TypeID: typ.ID, WeekStart: week, Amount: "50", Included: true, TypeVersion: typ.Version}
		if err = charges.SaveTruckCharge(ctx, update, actor); err == nil || !strings.Contains(err.Error(), "termination") {
			t.Fatal("terminated truck charge was accepted", err)
		}
		allowed := update
		allowed.TruckID = investorTruck.ID
		results, err := charges.SaveTruckCharges(ctx, []TruckChargePhase{update, allowed}, actor)
		if err != nil || len(results) != 2 || results[0].Error == "" || results[1].Error != "" {
			t.Fatal("bulk did not isolate terminated truck", results, err)
		}
		// Real Investor Pay uses the cutoff even though the old inclusion remains stored.
		for _, selected := range []string{prior, week} {
			date, _ := chargeWeek(selected)
			report, err := pay.InvestorPay(ctx, date)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, row := range report.Drivers {
				for _, fee := range row.AutoCharges {
					found = found || fee.Source == "truck_charge:"+stopped.ID+":"+typ.ID
				}
			}
			if found != (selected == prior) {
				t.Fatalf("investor fee in %s = %v", selected, found)
			}
		}
		var preserved bool
		if err = pool.QueryRow(ctx, `SELECT included AND amount=50 FROM truck_charge_phases WHERE truck_id=$1 AND week_start=$2::date`, stopped.ID, prior).Scan(&preserved); err != nil || !preserved {
			t.Fatal("historical configuration changed", err)
		}
		for _, frozen := range []bool{false, true} {
			name := "DRIVER-LIVE"
			if frozen {
				name = "DRIVER-FROZEN"
			}
			driver, truck := makeTruck(name, "", true)
			if _, err = pool.Exec(ctx, `DELETE FROM truck_settlement_terms WHERE truck_id=$1`, truck.ID); err != nil {
				t.Fatal(err)
			}
			// A late weekly entry must not make a departed driver's truck accrue fees.
			if _, err = pool.Exec(ctx, `INSERT INTO gross_board_entries(driver_id,service_date,load_number,driver_rate) VALUES($1,$2::date,'LATE-WORK',1000)`, driver.ID, week); err != nil {
				t.Fatal(err)
			}
			report, err := pay.Get(ctx, monday)
			if err != nil {
				t.Fatal(err)
			}
			if frozen {
				// Explicit override permits this synthetic unmatched load, while still
				// exercising the real freeze and source-collection transaction.
				if _, err = pay.Settle(ctx, monday, driver.ID, report.Revision, actor, "Synthetic unmatched load reviewed", false, true); err != nil {
					t.Fatal(err)
				}
			}
			if _, err = pool.Exec(ctx, `UPDATE drivers SET status='terminated',termination_date=$2::date WHERE id=$1`, driver.ID, monday.AddDate(0, 0, -1).Format(time.DateOnly)); err != nil {
				t.Fatal(err)
			}
			report, err = pay.Get(ctx, monday)
			if err != nil {
				t.Fatal(err)
			}
			foundDriver, foundFee := false, false
			for _, row := range report.Drivers {
				if row.ID != driver.ID {
					continue
				}
				foundDriver = true
				for _, fee := range row.AutoCharges {
					foundFee = foundFee || fee.Source == "truck_charge:"+truck.ID+":"+typ.ID
				}
			}
			if !foundDriver || foundFee != frozen {
				t.Fatalf("Driver Pay frozen=%v: driver=%v fee=%v", frozen, foundDriver, foundFee)
			}
		}
	})
}
