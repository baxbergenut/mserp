package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPayrollOverrideDatabase(t *testing.T) {
	withTruckChargeDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, actor string) {
		pay := NewDriverPayRepository(pool)
		charges := NewDriverChargeRepository(pool)
		for index, kind := range []string{"driver", "investor"} {
			t.Run(kind, func(t *testing.T) {
				id := func(query string, args ...any) string {
					t.Helper()
					var value string
					if err := pool.QueryRow(ctx, query, args...).Scan(&value); err != nil {
						t.Fatal(err)
					}
					return value
				}
				exec := func(query string, args ...any) {
					t.Helper()
					if _, err := pool.Exec(ctx, query, args...); err != nil {
						t.Fatal(err)
					}
				}
				driver := id(`INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate) VALUES($1,$1,'gross_percentage',25) RETURNING id`, kind+" override driver")
				owner := id(`INSERT INTO investors(full_name) VALUES($1) RETURNING id`, kind+" override owner")
				unit := kind + "-OVERRIDE"
				truck := id(`INSERT INTO trucks(unit_number,owner_id) VALUES($1,$2) RETURNING id`, unit, owner)
				exec(`UPDATE truck_ownership_history SET start_known=false WHERE truck_id=$1`, truck)
				exec(`INSERT INTO truck_driver_assignments(truck_id,driver_id,assigned_at) VALUES($1,$2,'2026-01-01')`, truck, driver)
				week, _ := chargeWeek("2026-09-28")
				if err := charges.SaveTruckTerm(ctx, TruckTerm{TruckID: truck, OwnerID: owner, WeekStart: "2026-01-05", SharePercent: "88"}, actor); err != nil {
					t.Fatal(err)
				}
				loadID := 8800 + index
				exec(`INSERT INTO loads(id,load_id,status,load_pay,total_pay,total_miles,truck_unit,pickup_time,raw_payload) VALUES($1,$2,'delivered',1000,1000,100,$3,'2026-09-28 12:00+00','{"trip":{"mile":90,"empty_mile":10},"stops":[{"stop_type":"pickup","ordering":1,"location":{"city":"Dallas","state":"TX"}}]}')`, loadID, unit, unit)
				exec(`INSERT INTO gross_board_entries(driver_id,service_date,load_number,load_record_id,driver_rate) VALUES($1,$2::date,$3,$4,1000)`, driver, week.Format(time.DateOnly), unit, loadID)
				read := pay.Get
				settle := pay.Settle
				target := driver
				if kind == "investor" {
					read = func(ctx context.Context, week time.Time) (DriverPayWeek, error) { return pay.InvestorPay(ctx, week) }
					settle = pay.SettleInvestor
					target = truck
				}
				get := func(week time.Time) DriverPayWeek {
					t.Helper()
					report, err := read(ctx, week)
					if err != nil {
						t.Fatal(err)
					}
					return report
				}
				card := func(report DriverPayWeek) DriverPayDriver {
					t.Helper()
					for _, d := range report.Drivers {
						if d.ID == target {
							return d
						}
					}
					t.Fatal("statement missing")
					return DriverPayDriver{}
				}
				report := get(week)
				before := card(report)
				if len(before.Loads) != 1 || before.Loads[0].DeliveryLocation != "" || len(before.Loads[0].Issues) == 0 || before.Loads[0].Fee == "" {
					t.Fatalf("missing-delivery fixture is incorrect: %+v", before)
				}
				if _, err := settle(ctx, week, target, report.Revision, actor, "", false, false); err == nil {
					t.Fatal("ordinary finalization accepted missing delivery")
				}
				if _, err := settle(ctx, week, target, "stale", actor, "", false, true); !errors.Is(err, ErrDriverPayConflict) {
					t.Fatal("override bypassed revision check", err)
				}
				final, err := settle(ctx, week, target, report.Revision, actor, "", false, true)
				if err != nil {
					t.Fatal(err)
				}
				frozen := card(final)
				if frozen.Settlement == nil || !frozen.Settlement.Finalized || !frozen.Settlement.Override || frozen.Settlement.Reason != "Finalized with override" {
					t.Fatalf("override metadata missing: %+v", frozen.Settlement)
				}
				if frozen.Loads[0].Fee != before.Loads[0].Fee || len(frozen.Loads[0].Issues) == 0 {
					t.Fatal("override changed earnings or hid source issues")
				}
				exec(`UPDATE loads SET raw_payload='{}' WHERE id=$1`, loadID)
				if !card(get(week)).Settlement.Override || card(get(week)).Loads[0].PickupLocation != before.Loads[0].PickupLocation {
					t.Fatal("override snapshot changed after source refresh")
				}
				if kind == "driver" {
					events, err := pay.SettlementHistory(ctx, driver, week.Format(time.DateOnly))
					if err != nil || len(events) != 1 || events[0].Reason != "Finalized with override" || !events[0].Report.Settlement.Override {
						t.Fatal("override event missing", events, err)
					}
					history, err := pay.History(ctx, driver, Pagination{PageSize: 100})
					if err != nil {
						t.Fatal(err)
					}
					found := false
					for _, item := range history.Items {
						if item.WeekStart == week.Format(time.DateOnly) {
							found = item.Driver.Settlement.Override
						}
					}
					if !found {
						t.Fatal("profile history lost override")
					}
				} else {
					var audit []byte
					if err := pool.QueryRow(ctx, `SELECT details FROM truck_settlement_events WHERE truck_id=$1 AND action='finalized' ORDER BY created_at DESC LIMIT 1`, truck).Scan(&audit); err != nil {
						t.Fatal(err)
					}
					var event struct {
						Override bool `json:"override"`
					}
					if err := json.Unmarshal(audit, &event); err != nil || !event.Override {
						t.Fatal("investor override audit missing", err)
					}
				}
				if _, err := settle(ctx, week, target, get(week).Revision, actor, "", true, true); err == nil {
					t.Fatal("override removed the reopen reason requirement")
				}
				if _, err := settle(ctx, week, target, get(week).Revision, actor, "Review source", true, false); err != nil {
					t.Fatal(err)
				}
				// An unmatched extra load has no fee; whole-week override still freezes
				// the current values and retains every review issue.
				exec(`INSERT INTO gross_board_extra_entries(driver_id,service_date,slot,load_number) VALUES($1,$2::date,1,'UNMATCHED')`, driver, week.Format(time.DateOnly))
				report = get(week)
				if _, err := settle(ctx, week, "", report.Revision, actor, "", false, true); err != nil {
					t.Fatal("whole-week override failed", err)
				}
				if _, err := settle(ctx, week, "", get(week).Revision, actor, "Review whole week", true, false); err != nil {
					t.Fatal(err)
				}
				future, _ := chargeWeek(ChargeCurrentWeek())
				future = future.AddDate(0, 0, 7)
				exec(`INSERT INTO gross_board_entries(driver_id,service_date,load_number,driver_rate) VALUES($1,$2::date,$3,1000)`, driver, future.Format(time.DateOnly), fmt.Sprintf("FUTURE-%s", kind))
				report = get(future)
				if _, err := settle(ctx, future, target, report.Revision, actor, "", false, false); err == nil {
					t.Fatal("ordinary finalization accepted future week")
				}
				if _, err := settle(ctx, future, target, report.Revision, actor, "", false, true); err != nil {
					t.Fatal("future override failed", err)
				}
				if _, err := settle(ctx, future, target, get(future).Revision, actor, "Correct future override", true, false); err != nil {
					t.Fatal("future override could not be reopened", err)
				}
			})
		}
	})
}
