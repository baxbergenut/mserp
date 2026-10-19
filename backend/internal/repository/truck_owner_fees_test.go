package repository

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestOwnerTruckFeesLoadConflictDatabase(t *testing.T) {
	withTruckChargeDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, actor string) {
		mustID := func(sql string, args ...any) string {
			t.Helper()
			var id string
			if err := pool.QueryRow(ctx, sql, args...).Scan(&id); err != nil {
				t.Fatal(err)
			}
			return id
		}
		exec := func(sql string, args ...any) {
			t.Helper()
			if _, err := pool.Exec(ctx, sql, args...); err != nil {
				t.Fatal(err)
			}
		}
		owner := mustID(`INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate) VALUES('Truck owner','truck owner','gross_percentage',88) RETURNING id`)
		other := mustID(`INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate) VALUES('Other operator','other operator','gross_percentage',25) RETURNING id`)
		investor := mustID(`INSERT INTO investors(full_name,driver_id) VALUES('Truck owner',$1) RETURNING id`, owner)
		truck := mustID(`INSERT INTO trucks(unit_number,owner_id) VALUES('OWNER-001',$1) RETURNING id`, investor)
		exec(`UPDATE truck_ownership_history SET assigned_at='2026-09-01',start_known=true WHERE truck_id=$1`, truck)
		exec(`INSERT INTO truck_driver_assignments(truck_id,driver_id,assigned_at) VALUES($1,$2,'2026-09-01')`, truck, owner)
		const week = "2026-09-28"
		date, _ := time.Parse(time.DateOnly, week)
		charges := NewDriverChargeRepository(pool)
		for _, fee := range []struct{ name, amount, eligibility string }{
			{"Admin", "120", "loads"}, {"Insurance max", "375", "loads"},
			{"Insurance min", "150", "no_loads"}, {"Samsara", "20", "calendar"},
		} {
			typ, err := charges.SaveType(ctx, ChargeType{Name: fee.name, Direction: "charge", Amounts: []string{fee.amount}, Eligibility: fee.eligibility}, actor)
			if err != nil {
				t.Fatal(err)
			}
			if err = charges.SaveTruckCharge(ctx, TruckChargePhase{TruckID: truck, TypeID: typ.ID, WeekStart: week, Included: true, Amount: fee.amount, TypeVersion: typ.Version}, actor); err != nil {
				t.Fatal(err)
			}
		}
		exec(`INSERT INTO loads(id,load_id,status,load_pay,total_pay,total_miles,truck_unit,truck_id,raw_payload) VALUES(91001,'CONFLICT-1','delivered',1000,1000,500,'OWNER-001',$1,'{}')`, truck)
		exec(`INSERT INTO gross_board_entries(driver_id,service_date,load_number,load_record_id,driver_rate) VALUES($1,'2026-09-30','CONFLICT-1',91001,1000)`, owner)
		// The duplicate is in Daily loads, as in the reported production case.
		exec(`INSERT INTO gross_board_extra_entries(driver_id,service_date,slot,load_number,load_record_id,driver_rate) VALUES($1,'2026-09-29',1,'CONFLICT-1',91001,1000)`, other)
		pay := NewDriverPayRepository(pool)
		assertFees := func(report DriverPayWeek, expected map[string]string, warning string) DriverPayDriver {
			t.Helper()
			var ownerStatement DriverPayDriver
			for _, d := range report.Drivers {
				if d.ID != owner {
					if len(d.AutoCharges) != 0 || len(d.Edits.SourceCharges) != 0 {
						t.Fatalf("owner fees moved to another driver: %+v", d)
					}
					continue
				}
				if len(d.AutoCharges) != len(expected) || len(d.Edits.SourceCharges) != len(expected) {
					t.Fatalf("missing or duplicated fees: %+v / %+v", d.AutoCharges, d.Edits.SourceCharges)
				}
				for _, fee := range d.AutoCharges {
					if expected[fee.Name] != fee.Amount {
						t.Fatalf("unexpected fee: %+v", fee)
					}
				}
				for _, fee := range d.Edits.SourceCharges {
					if expected[fee.Name] != fee.Amount {
						t.Fatalf("unexpected collectible fee: %+v", fee)
					}
				}
				if warning == "" && len(d.Issues) > 0 || warning != "" && !strings.Contains(strings.Join(d.Issues, "\n"), warning) {
					t.Fatalf("unexpected warnings: %v", d.Issues)
				}
				ownerStatement = d
			}
			if ownerStatement.ID == "" {
				t.Fatal("owner statement missing")
			}
			return ownerStatement
		}
		workingFees := map[string]string{"Admin": "-120.00", "Insurance max": "-375.00", "Samsara": "-20.00"}
		conflict := "Other operator (CONFLICT-1, 2026-09-29)"
		for _, target := range []string{"", owner} {
			report, err := pay.GetDriverWeek(ctx, date, target)
			if err != nil {
				t.Fatal(err)
			}
			d := assertFees(report, workingFees, conflict)
			if summary := summarizePayStatement(d); summary.Summary.Review == 0 || len(summary.Issues) == 0 {
				t.Fatal("collapsed payroll row lost the conflict warning")
			}
			if target == "" {
				if _, err = pay.Settle(ctx, date, owner, report.Revision, actor, "", false, false); err == nil || !strings.Contains(err.Error(), conflict) {
					t.Fatalf("expected finalization to require conflict review: %v", err)
				}
			}
		}
		var collected int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM pay_source_collections`).Scan(&collected); err != nil || collected != 0 {
			t.Fatalf("reads or rejected finalization collected fees: %d %v", collected, err)
		}
		// Correcting the duplicate clears the warning without changing the fees.
		exec(`DELETE FROM gross_board_extra_entries WHERE driver_id=$1`, other)
		report, err := pay.GetDriverWeek(ctx, date, owner)
		if err != nil {
			t.Fatal(err)
		}
		assertFees(report, workingFees, "")
		// Fees stay with the owner even if all attributed work belongs to the
		// conflicting driver. Work eligibility still follows the truck's loads.
		exec(`INSERT INTO gross_board_extra_entries(driver_id,service_date,slot,load_number,load_record_id,driver_rate) VALUES($1,'2026-09-29',1,'CONFLICT-1',91001,1000)`, other)
		exec(`DELETE FROM gross_board_entries WHERE driver_id=$1`, owner)
		report, err = pay.GetDriverWeek(ctx, date, owner)
		if err != nil {
			t.Fatal(err)
		}
		assertFees(report, workingFees, conflict)
		// No-load insurance remains automatic; both rates must never be charged.
		exec(`DELETE FROM gross_board_extra_entries WHERE driver_id=$1`, other)
		report, err = pay.GetDriverWeek(ctx, date, owner)
		if err != nil {
			t.Fatal(err)
		}
		idleFees := map[string]string{"Insurance min": "-150.00", "Samsara": "-20.00"}
		assertFees(report, idleFees, "")
		// Missing assignment data is also a review warning, not a reason to
		// silently drop a known owner's included calendar fees.
		exec(`DELETE FROM truck_driver_assignments WHERE truck_id=$1`, truck)
		report, err = pay.GetDriverWeek(ctx, date, owner)
		if err != nil {
			t.Fatal(err)
		}
		assertFees(report, idleFees, "no full-week owner-driver assignment")
		investorReport, err := pay.InvestorPay(ctx, date)
		if err != nil || len(investorReport.Drivers) != 0 {
			t.Fatalf("fees duplicated in Investor Pay: %+v %v", investorReport, err)
		}
	})
}
