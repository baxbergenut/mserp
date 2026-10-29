package repository

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPayrollVacationsDatabase(t *testing.T) {
	withTruckChargeDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, actor string) {
		exec := func(sql string, args ...any) {
			t.Helper()
			if _, err := pool.Exec(ctx, sql, args...); err != nil {
				t.Fatal(err)
			}
		}
		id := func(sql string, args ...any) string {
			t.Helper()
			var v string
			if err := pool.QueryRow(ctx, sql, args...).Scan(&v); err != nil {
				t.Fatal(err)
			}
			return v
		}
		driver := id(`INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate,hire_date) VALUES('Vacation owner','vacation owner','gross_percentage',88,'2026-09-01') RETURNING id::text`)
		owner := id(`INSERT INTO investors(full_name,driver_id) VALUES('Vacation owner',$1) RETURNING id::text`, driver)
		truck := id(`INSERT INTO trucks(unit_number,owner_id) VALUES('VACATION-1',$1) RETURNING id::text`, owner)
		exec(`UPDATE truck_ownership_history SET assigned_at='2026-09-01',start_known=true WHERE truck_id=$1`, truck)
		exec(`INSERT INTO truck_driver_assignments(truck_id,driver_id,assigned_at) VALUES($1,$2,'2026-09-01')`, truck, driver)
		// Current status has already returned to Active: past vacation weeks must
		// remain exempt. Profile and Status Board writes record this same history.
		history := func(person, status, date string) {
			exec(`INSERT INTO fleet_status_history(entity_kind,entity_id,entity_name,track,previous_status,status,effective_at,actor_name,source)
 VALUES('driver',$1,'Vacation fixture','availability','',$2,($3::date::timestamp AT TIME ZONE 'America/New_York'),'Test','profile')`, person, status, date)
		}
		history(driver, "active", "2026-09-01")
		history(driver, "vacation", "2026-09-28")
		history(driver, "active", "2026-10-12")
		charges := NewDriverChargeRepository(pool)
		typ, err := charges.SaveType(ctx, ChargeType{Name: "Vacation insurance", Direction: "charge", Amounts: []string{"100.00"}, Eligibility: "calendar"}, actor)
		if err != nil {
			t.Fatal(err)
		}
		if err = charges.SaveTruckCharge(ctx, TruckChargePhase{TruckID: truck, TypeID: typ.ID, WeekStart: "2026-09-21", Amount: "100.00", Included: true, TypeVersion: typ.Version}, actor); err != nil {
			t.Fatal(err)
		}
		pay := NewDriverPayRepository(pool)
		board := NewGrossBoardRepository(pool)
		week := func(date string) time.Time { v, _ := time.Parse(time.DateOnly, date); return v }
		get := func(date string) DriverPayWeek {
			t.Helper()
			r, e := pay.GetDriverWeek(ctx, week(date), driver)
			if e != nil {
				t.Fatal(e)
			}
			return r
		}
		fee := func(report DriverPayWeek) string {
			for _, d := range report.Drivers {
				for _, c := range d.AutoCharges {
					if c.Source == "truck_charge:"+truck+":"+typ.ID {
						return c.Amount
					}
				}
			}
			return ""
		}
		if got := fee(get("2026-09-21")); got != "-100.00" {
			t.Fatalf("pre-vacation fee: %s", got)
		}
		// A saved open row, recurring carry and operational vacation marks cannot
		// independently put an idle vacationing driver back on any weekly page.
		exec(`INSERT INTO driver_pay_weeks(driver_id,week_start) VALUES($1,'2026-09-28')`, driver)
		exec(`INSERT INTO gross_board_entries(driver_id,service_date,day_status) VALUES($1,'2026-09-28','HOME')`, driver)
		snapshot, _ := json.Marshal(DriverPayDriver{ID: driver, FullName: "Vacation owner", TruckID: truck, InvestorID: owner, PayType: "gross_percentage", PayRate: "88", IsOwnerOperator: true})
		exec(`INSERT INTO pay_source_collections(source,owner_id,week_start,kind,statement_id,truck_id,name,base,amount,statement,updated_by)
 VALUES($1,$2,'2026-09-21','driver',$3,$4,'Insurance',-100,0,$5,$6)`, "truck_charge:"+truck+":"+typ.ID, owner, driver, truck, snapshot, actor)
		for _, date := range []string{"2026-09-28", "2026-10-05"} {
			if r := get(date); len(r.Drivers) != 0 {
				t.Fatalf("vacation reappeared on Driver Pay: %+v", r)
			}
			b, e := board.Get(ctx, week(date))
			if e != nil || len(b.Drivers) != 0 {
				t.Fatalf("vacation Gross Board: %+v %v", b, e)
			}
			for _, kind := range []string{"driver", "investor"} {
				rowID := driver
				if kind == "investor" {
					rowID = truck
				}
				page, e := pay.PayrollPage(ctx, kind, week(date), PayPageQuery{ID: rowID, Summary: true, Pagination: Pagination{Page: 1, PageSize: 100}})
				if e != nil || page.Pagination.Total != 0 || page.Pagination.Payable != "0.00" {
					t.Fatalf("%s vacation summary: %+v %v", kind, page, e)
				}
			}
		}
		if _, err = pay.Save(ctx, DriverPayEdits{DriverID: driver, WeekStart: "2026-10-05", Notes: "Stale vacation autosave"}, actor); err == nil {
			t.Fatal("stale vacation autosave was accepted")
		}
		if got := fee(get("2026-10-12")); got != "-200.00" {
			t.Fatalf("return should collect one new fee plus pre-leave carry, no vacation accrual: %s", got)
		}
		// The fee configuration survives leave; the normal matrix remains editable.
		matrix, err := charges.TruckChargesForWeek(ctx, "2026-09-28")
		if err != nil || len(matrix.Phases) != 1 || !matrix.Phases[0].Included {
			t.Fatalf("vacation changed recurring configuration: %+v %v", matrix, err)
		}
		// With explicit investor terms the truck statement is also absent, even
		// after an earlier autosave. This covers owner and hired-driver routing.
		exec(`INSERT INTO truck_settlement_terms(truck_id,owner_id,week_start,share_percent) VALUES($1,$2,'2026-09-21',88)`, truck, owner)
		exec(`INSERT INTO investor_pay_weeks(truck_id,owner_id,week_start,edits) VALUES($1,$2,'2026-09-28','{}')`, truck, owner)
		investor, err := pay.InvestorPay(ctx, week("2026-09-28"))
		if err != nil || len(investor.Drivers) != 0 {
			t.Fatalf("vacation investor statement: %+v %v", investor, err)
		}
		// Recorded work stays reviewable; recurring fees still do not apply.
		exec(`INSERT INTO gross_board_entries(driver_id,service_date,load_number,original_rate,driver_rate,miles) VALUES($1,'2026-09-29','ACTUAL-WORK',1000,900,500)`, driver)
		b, err := board.Get(ctx, week("2026-09-28"))
		if err != nil || len(b.Drivers) != 1 {
			t.Fatalf("lost recorded Gross Board work: %+v %v", b, err)
		}
		investor, err = pay.InvestorPay(ctx, week("2026-09-28"))
		if err != nil || len(investor.Drivers) != 1 || len(investor.Drivers[0].Loads) != 1 || fee(investor) != "" {
			t.Fatalf("vacation work/fee routing: %+v %v", investor, err)
		}
		// A working replacement prevents a driver's leave from suspending fees
		// on an investor's independently operating truck.
		relief := id(`INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate,hire_date) VALUES('Relief','relief','cpm',0.7,'2026-09-01') RETURNING id::text`)
		exec(`UPDATE truck_driver_assignments SET unassigned_at='2026-10-05 00:00-04' WHERE truck_id=$1`, truck)
		exec(`INSERT INTO truck_driver_assignments(truck_id,driver_id,assigned_at) VALUES($1,$2,'2026-10-05 00:00-04')`, truck, relief)
		investor, err = pay.InvestorPay(ctx, week("2026-10-05"))
		if err != nil || fee(investor) == "" {
			t.Fatalf("relief crew incorrectly exempted: %+v %v", investor, err)
		}
		history(relief, "active", "2026-09-01")
		history(relief, "vacation", "2026-10-07")
		history(relief, "active", "2026-10-12")
		investor, err = pay.InvestorPay(ctx, week("2026-10-05"))
		if err != nil || fee(investor) == "" {
			t.Fatalf("partial week should retain fee: %+v %v", investor, err)
		}
		// Frozen snapshots survive even an idle vacation week and retain amounts.
		frozen := DriverPayDriver{ID: driver, FullName: "Frozen vacation statement", Loads: []DriverPayLoad{}, Edits: DriverPayEdits{DriverID: driver, WeekStart: "2026-10-05"}, AutoCharges: []PayAutoCharge{{Name: "Frozen fee", Amount: "-50.00", Source: "frozen"}}}
		body, _ := json.Marshal(frozen)
		exec(`INSERT INTO payroll_settlements(driver_id,week_start,report,finalized) VALUES($1,'2026-10-05',$2,true)`, driver, body)
		r := get("2026-10-05")
		if len(r.Drivers) != 1 || r.Drivers[0].Settlement == nil || !r.Drivers[0].Settlement.Finalized || r.Drivers[0].AutoCharges[0].Amount != "-50.00" {
			t.Fatalf("frozen vacation history changed: %+v", r)
		}
		frozen.ID, frozen.TruckID, frozen.InvestorID = truck, truck, owner
		frozen.Edits = DriverPayEdits{DriverID: truck, WeekStart: "2026-09-28"}
		body, _ = json.Marshal(frozen)
		exec(`UPDATE investor_pay_weeks SET finalized=true,report=$2 WHERE truck_id=$1 AND week_start='2026-09-28'`, truck, body)
		investor, err = pay.InvestorPay(ctx, week("2026-09-28"))
		if err != nil || len(investor.Drivers) != 1 || investor.Drivers[0].Settlement == nil || !investor.Drivers[0].Settlement.Finalized || investor.Drivers[0].AutoCharges[0].Amount != "-50.00" {
			t.Fatalf("frozen investor vacation history changed: %+v %v", investor, err)
		}
		// End dates and same-day transitions use New York calendar dates.
		for n, tc := range []struct {
			start, end string
			away       bool
		}{
			{"2026-09-28", "2026-10-05", true},  // Monday return ends the following week.
			{"2026-09-28", "2026-10-04", false}, // Sunday return is a partial week.
			{"2026-09-30", "2026-10-05", false}, // Wednesday departure is partial.
		} {
			person := id(`INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate) VALUES('Boundary '||$1::integer,'boundary '||$1::integer,'cpm',0.7) RETURNING id::text`, n)
			history(person, "active", "2026-09-01")
			history(person, "vacation", tc.start)
			history(person, "active", tc.end)
			v, e := readVacationWeek(ctx, pool, "2026-09-28")
			if e != nil || v.drivers[person] != tc.away {
				t.Fatalf("boundary %+v: away=%v %v", tc, v.drivers[person], e)
			}
		}
		// A team member who remains available keeps the physical truck eligible.
		teamTruck := id(`INSERT INTO trucks(unit_number,owner_id) VALUES('VACATION-TEAM',$1) RETURNING id::text`, owner)
		exec(`UPDATE truck_ownership_history SET assigned_at='2026-09-01',start_known=true WHERE truck_id=$1`, teamTruck)
		main := id(`INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate) VALUES('Team vacation','team vacation','cpm',0.7) RETURNING id::text`)
		member := id(`INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate) VALUES('Team available','team available','cpm',0.7) RETURNING id::text`)
		exec(`INSERT INTO truck_driver_assignments(truck_id,driver_id,assigned_at) VALUES($1,$2,'2026-09-01')`, teamTruck, main)
		exec(`INSERT INTO driver_teams(main_driver_id,team_driver_id,start_week) VALUES($1,$2,'2026-09-21')`, main, member)
		history(main, "vacation", "2026-09-28")
		v, err := readVacationWeek(ctx, pool, "2026-09-28")
		if err != nil || v.trucks[teamTruck] {
			t.Fatal("available teammate was ignored", err)
		}
		history(member, "vacation", "2026-09-28")
		v, err = readVacationWeek(ctx, pool, "2026-09-28")
		if err != nil || !v.trucks[teamTruck] {
			t.Fatal("fully vacationing team retained fees", err)
		}
		legacy := id(`INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate,status) VALUES('Legacy vacation','legacy vacation','cpm',0.7,'vacation') RETURNING id::text`)
		v, err = readVacationWeek(ctx, pool, "2026-09-28")
		if err != nil || v.drivers[legacy] {
			t.Fatal("unknown legacy leave start rewrote earlier weeks", err)
		}
		v, err = readVacationWeek(ctx, pool, ChargeCurrentWeek())
		if err != nil || !v.drivers[legacy] {
			t.Fatal("legacy current vacation ignored", err)
		}
	})
}
