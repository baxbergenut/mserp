package repository

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"mserp/internal/db"
)

func TestPayrollDayFactsDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		seedPayrollProjectionTest(t, ctx, pool)
		changes := []string{
			`INSERT INTO gross_board_extra_entries(driver_id,service_date,slot,day_status) VALUES('18200000-0000-0000-0000-000000000001','2026-10-06',1,'HOME')`,
			`UPDATE truck_driver_assignments SET unassigned_at='2026-10-05'`,
			`UPDATE drivers SET active=false,termination_date='2026-10-06' WHERE id='18200000-0000-0000-0000-000000000001'`,
			`UPDATE gross_board_extra_entries SET deleted=true`,
		}
		for phase := 0; phase <= len(changes); phase++ {
			tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
			if err != nil {
				t.Fatal(err)
			}
			snapshot, err := payrollProjectionFence(ctx, tx)
			if err != nil {
				t.Fatal(err)
			}
			read := payrollRoutingFactsContext(context.WithValue(ctx, payrollNormalizedFactsKey{}, true), snapshot)
			read = context.WithValue(read, payrollSelectionKey{}, &payrollSelection{drivers: []string{projectionDriver}, trucks: []string{projectionTruck}})
			for _, kind := range []string{"driver", "investor"} {
				want, err := readPayDayStatuses(ctx, tx, kind, "2026-10-05", nil)
				if err != nil {
					t.Fatal(err)
				}
				got, err := readPreparedPayDayStatuses(read, tx, kind, "2026-10-05", []string{projectionDriver, projectionTruck})
				a, _ := json.Marshal(want)
				b, _ := json.Marshal(got)
				if err != nil || string(a) != string(b) {
					t.Fatal("day fact routing differs", phase, kind, err, string(a), string(b))
				}
				if phase == 0 && len(got) != 0 || phase == 1 && len(got) != 1 || phase >= 2 && kind == "investor" && len(got) != 0 {
					t.Fatal("day fact fixture did not exercise boundary", phase, kind, got)
				}
				measured, metrics := db.WithQueryMetrics(read)
				if _, err := readPreparedPayDayStatuses(measured, tx, kind, "2026-10-05", nil); err != nil || metrics.Queries.Load() != 0 {
					t.Fatal("same-phase day facts reloaded", err, metrics.Queries.Load())
				}
				id := projectionDriver
				if kind == "investor" {
					id = projectionTruck
				}
				frozen := DriverPayWeek{WeekStart: "2026-10-05", Drivers: []DriverPayDriver{{ID: id, Settlement: &PayrollSettlement{Finalized: true}, DayStatuses: []PayDayStatus{{Date: "2026-10-07", Status: "SHOP"}}}}}
				if err := decoratePayDayStatuses(read, tx, kind, &frozen); err != nil || len(frozen.Drivers[0].DayStatuses) != 1 || frozen.Drivers[0].DayStatuses[0].Status != "SHOP" {
					t.Fatal("day facts changed frozen labels", err)
				}
			}
			if err := tx.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			if phase < len(changes) {
				if _, err := pool.Exec(ctx, changes[phase]); err != nil {
					t.Fatal(err)
				}
			}
		}
	})
}
