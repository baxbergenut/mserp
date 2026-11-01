package repository

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"mserp/internal/db"
)

func TestPreparedPayrollReadsDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		seedPayrollProjectionTest(t, ctx, pool)
		if _, err := pool.Exec(ctx, `INSERT INTO tolls(truck_id,posting_date,invoice_date,customer_id,source,read_type,transponder_or_plate,equipment_unit,agency,exit_plaza,exit_date,exit_time,toll_class,amount,row_fingerprint)
 VALUES($1,'2026-10-06','2026-10-06','','','','','PROJECTION','','','2026-10-06','12:00','',40,repeat('p',64))`, projectionTruck); err != nil {
			t.Fatal(err)
		}
		repo := NewDriverPayRepository(pool)
		if err := repo.EnablePayrollShadow(ctx); err != nil {
			t.Fatal(err)
		}
		week, _ := chargeWeek("2026-10-05")
		missing, err := repo.PreparedPayroll(ctx, "driver", week, PayPageQuery{}, true)
		if err != nil || missing.Preparation.State != "preparing" {
			t.Fatal(missing, err)
		}
		if _, err = repo.PreparedPayroll(ctx, "driver", week, PayPageQuery{}, false); !errors.Is(err, ErrPayrollPreparing) {
			t.Fatal("legacy client received missing payroll", err)
		}
		if _, err = repo.PrepareNextPayrollWeek(ctx); err != nil {
			t.Fatal(err)
		}
		for _, kind := range []string{"driver", "investor"} {
			id := projectionDriver
			if kind == "investor" {
				id = projectionTruck
			}
			summary, err := repo.PreparedPayroll(ctx, kind, week, PayPageQuery{Summary: true}, true)
			if err != nil {
				t.Fatal(err)
			}
			version := summary.Drivers[0].StatementRevision
			if version == "" || version == summary.Revision {
				t.Fatal("weekly and statement revisions conflated")
			}
			detail, err := repo.PreparedPayroll(ctx, kind, week, PayPageQuery{ID: id, StatementRevision: version}, true)
			if err != nil || detail.Drivers[0].Summary != nil || detail.Drivers[0].StatementRevision != version {
				t.Fatal("detail version mismatch", err)
			}
			originalEvidence := map[string]PayCostTransactions{}
			for _, cost := range []string{"fuel", "toll"} {
				want, err := repo.CostTransactions(ctx, kind, id, week, cost)
				if err != nil {
					t.Fatal(err)
				}
				originalEvidence[cost] = want
				measured, metrics := db.WithQueryMetrics(ctx)
				got, err := repo.PreparedCostTransactions(measured, kind, id, week, cost)
				if err != nil {
					t.Fatal(err)
				}
				if got.Preparation.State != "ready" {
					t.Fatal("fresh evidence stale")
				}
				got.Preparation = nil
				a, _ := json.Marshal(want)
				b, _ := json.Marshal(got)
				if string(a) != string(b) {
					t.Fatalf("%s %s evidence mismatch\n%s\n%s", kind, cost, a, b)
				}
				phases, _ := metrics.Details()
				for phase := range phases {
					if strings.Contains(phase, "calculation") {
						t.Fatal("evidence reconstructed payroll", phases)
					}
				}
			}
			if _, err = pool.Exec(ctx, `UPDATE gross_board_entries SET entered_miles=entered_miles+10,version=version+1 WHERE driver_id=$1::uuid`, projectionDriver); err != nil {
				t.Fatal(err)
			}
			if _, err = pool.Exec(ctx, `UPDATE tolls SET amount=amount+10 WHERE truck_id=$1`, projectionTruck); err != nil {
				t.Fatal(err)
			}
			stale, err := repo.PreparedPayroll(ctx, kind, week, PayPageQuery{Summary: true}, true)
			if err != nil || stale.Preparation.State != "updating" || stale.Drivers[0].StatementRevision != version {
				t.Fatal("stale values not retained", err)
			}
			if _, err = repo.PreparedPayroll(ctx, kind, week, PayPageQuery{}, false); !errors.Is(err, ErrPayrollPreparing) {
				t.Fatal("legacy stale values presented current", err)
			}
			if _, err = repo.DispatchPayrollChanges(ctx); err != nil {
				t.Fatal(err)
			}
			if _, err = repo.PrepareNextPayrollWeek(ctx); err != nil {
				t.Fatal(err)
			}
			retained, err := NewDriverPayRepository(pool).PreparedPayroll(ctx, kind, week, PayPageQuery{ID: id, StatementRevision: version}, true)
			if err != nil || retained.Drivers[0].StatementRevision != version || retained.Drivers[0].Loads[0].TotalMiles != detail.Drivers[0].Loads[0].TotalMiles {
				t.Fatal("retained detail silently advanced", err)
			}
			if _, err = repo.PreparedPayroll(ctx, kind, week, PayPageQuery{ID: id, StatementRevision: "18200000-0000-0000-0000-000000000099"}, true); !errors.Is(err, ErrPayrollVersionExpired) {
				t.Fatal("expired revision did not require review", err)
			}
			for _, cost := range []string{"fuel", "toll"} {
				got, err := repo.PreparedCostTransactions(ctx, kind, id, week, cost, version)
				if err != nil || got.Preparation.State != "updating" {
					t.Fatal("retained cost revision", err)
				}
				got.Preparation = nil
				a, _ := json.Marshal(originalEvidence[cost])
				b, _ := json.Marshal(got)
				if string(a) != string(b) {
					t.Fatal("cost evidence silently advanced", cost)
				}
			}
			if _, err = repo.PreparedCostTransactions(ctx, kind, id, week, "toll", "18200000-0000-0000-0000-000000000099"); !errors.Is(err, ErrPayrollVersionExpired) {
				t.Fatal("expired cost evidence", err)
			}
		}
	})
}
