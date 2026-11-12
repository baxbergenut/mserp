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
			for _, cost := range []string{"fuel", "toll"} {
				want, err := repo.CostTransactions(ctx, kind, id, week, cost)
				if err != nil {
					t.Fatal(err)
				}
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
			if _, err = NewDriverPayRepository(pool).PreparedPayroll(ctx, kind, week, PayPageQuery{ID: id, StatementRevision: version}, true); !errors.Is(err, ErrPayrollVersionExpired) {
				t.Fatal("superseded detail must expire without substituting current values", err)
			}
			if _, err = repo.PreparedPayroll(ctx, kind, week, PayPageQuery{ID: id, StatementRevision: "18200000-0000-0000-0000-000000000099"}, true); !errors.Is(err, ErrPayrollVersionExpired) {
				t.Fatal("expired revision did not require review", err)
			}
			for _, cost := range []string{"fuel", "toll"} {
				if _, err = repo.PreparedCostTransactions(ctx, kind, id, week, cost, version); !errors.Is(err, ErrPayrollVersionExpired) {
					t.Fatal("superseded cost evidence must expire", cost, err)
				}
			}
			if _, err = repo.PreparedCostTransactions(ctx, kind, id, week, "toll", "18200000-0000-0000-0000-000000000099"); !errors.Is(err, ErrPayrollVersionExpired) {
				t.Fatal("expired cost evidence", err)
			}
		}
	})
}

// Sharing a validation must preserve the expanded map's exact semantics, even
// for a stale shared key overridden locally or a missing/empty shared map.
func TestPreparedPayrollSharedFreshnessDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		seedPayrollProjectionTest(t, ctx, pool)
		repo := NewDriverPayRepository(pool)
		if err := repo.EnablePayrollShadow(ctx); err != nil {
			t.Fatal(err)
		}
		week, _ := chargeWeek("2026-10-05")
		if err := repo.RequestPayrollPreparation(ctx, week, false); err != nil {
			t.Fatal(err)
		}
		if worked, err := repo.PrepareNextPayrollWeek(ctx); err != nil || !worked {
			t.Fatal(worked, err)
		}
		var packed []byte
		if err := pool.QueryRow(ctx, `SELECT payroll_pack_resource_snapshot(
 ARRAY(SELECT 'expense:freshness-'||n FROM generate_series(1,5600)n),'')`).Scan(&packed); err != nil {
			t.Fatal(err)
		}
		check := func(name string, value []byte, want bool) {
			t.Helper()
			if _, err := pool.Exec(ctx, `UPDATE payroll_statement_current SET resource_dependencies=$1 WHERE kind='driver'`, value); err != nil {
				t.Fatal(err)
			}
			var exact bool
			if err := pool.QueryRow(ctx, `SELECT bool_and(payroll_resources_current(resource_dependencies)) FROM payroll_statement_current WHERE kind='driver'`).Scan(&exact); err != nil || exact != want {
				t.Fatal(name, "reference", exact, want, err)
			}
			page, err := repo.ReadPreparedPayrollPage(ctx, "driver", week, PayPageQuery{Summary: true})
			if err != nil || (page.State == "ready") != want {
				t.Fatal(name, "page freshness differs", page.State, want, err)
			}
		}
		check("shared current", packed, true)
		if _, err := pool.Exec(ctx, `INSERT INTO payroll_resource_revisions(resource_key,revision) VALUES('expense:freshness-1',1)`); err != nil {
			t.Fatal(err)
		}
		check("shared stale", packed, false)
		var overridden []byte
		if err := pool.QueryRow(ctx, `SELECT $1::jsonb||'{"expense:freshness-1":1}'::jsonb`, packed).Scan(&overridden); err != nil {
			t.Fatal(err)
		}
		check("local override of shared key", overridden, true)
		if _, err := pool.Exec(ctx, `UPDATE payroll_resource_revisions SET revision=2 WHERE resource_key='expense:freshness-1'`); err != nil {
			t.Fatal(err)
		}
		check("local override stale", overridden, false)
		check("missing shared", []byte(`{"$shared":"missing"}`), false)
		check("inline current", []byte(`{"expense:freshness-1":2}`), true)
		check("inline stale", []byte(`{"expense:freshness-1":1}`), false)
		var emptyShared []byte
		if err := pool.QueryRow(ctx, `INSERT INTO payroll_resource_dependency_sets(fingerprint,dependencies)
 VALUES(encode(sha256(convert_to('{}','UTF8')),'hex'),'{}') RETURNING jsonb_build_object('$shared',fingerprint)`).Scan(&emptyShared); err != nil {
			t.Fatal(err)
		}
		check("empty shared global protocol", emptyShared, true)
		if _, err := pool.Exec(ctx, `UPDATE payroll_projection_control SET protocol_mode='resources' WHERE singleton`); err != nil {
			t.Fatal(err)
		}
		check("empty shared resource protocol", emptyShared, false)
		if err := pool.QueryRow(ctx, `SELECT $1::jsonb||'{"expense:freshness-1":2}'::jsonb`, emptyShared).Scan(&overridden); err != nil {
			t.Fatal(err)
		}
		check("empty shared with current local", overridden, true)
	})
}

func TestDriverPayrollExportDuringPreparationDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, _ *pgx.Conn) {
		seedPayrollProjectionTest(t, ctx, pool)
		const draft = "18200000-0000-0000-0000-000000006001"
		_, err := pool.Exec(ctx, `INSERT INTO drivers(id,full_name,normalized_name,pay_type,pay_rate,hire_date)
 VALUES($1,'Export draft','export draft','cpm',0.50,'2026-01-01')`, draft)
		if err != nil {
			t.Fatal(err)
		}
		_, err = pool.Exec(ctx, `INSERT INTO gross_board_entries(driver_id,service_date,load_number,driver_rate,entered_original_rate,entered_miles)
 VALUES($1,'2026-10-05','EXPORT-DRAFT',200,200,100)`, draft)
		if err != nil {
			t.Fatal(err)
		}
		repo := NewDriverPayRepository(pool)
		repo.ConfigurePreparedReads(true)
		week, _ := chargeWeek("2026-10-05")
		var actor string
		if err = pool.QueryRow(ctx, `INSERT INTO app_users(username,password_hash) VALUES('Export tester',crypt('test-only',gen_salt('bf'))) RETURNING id::text`).Scan(&actor); err != nil {
			t.Fatal(err)
		}
		if _, err := settlePayrollFixture(t, ctx, repo, "driver", week, projectionDriver, "", actor, "", false, true, true); err != nil {
			t.Fatal(err)
		}
		rowsByID := func(report DriverPayWeek) map[string]DriverPayDriver {
			t.Helper()
			if len(report.Drivers) != 2 {
				t.Fatalf("expected both statements, got %d", len(report.Drivers))
			}
			rows := make(map[string]DriverPayDriver, len(report.Drivers))
			for _, row := range report.Drivers {
				if len(row.Loads) != 1 {
					t.Fatalf("expected an expanded statement for %s", row.ID)
				}
				rows[row.ID] = row
			}
			return rows
		}
		before, err := repo.PayrollExportPage(ctx, "driver", week, PayPageQuery{})
		if err != nil || before.Preparation == nil || before.Preparation.State != "ready" {
			t.Fatal("expected the prepared export before the update", err)
		}
		saved := rowsByID(before)
		if _, err = pool.Exec(ctx, `UPDATE loads SET total_miles=total_miles+100 WHERE id=18201`); err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `UPDATE gross_board_entries SET entered_miles=140 WHERE driver_id=$1`, draft); err != nil {
			t.Fatal(err)
		}
		if _, err = repo.PreparedPayroll(ctx, "driver", week, PayPageQuery{}, false); !errors.Is(err, ErrPayrollPreparing) {
			t.Fatal("fixture must leave the prepared export stale", err)
		}
		after, err := repo.PayrollExportPage(ctx, "driver", week, PayPageQuery{})
		if err != nil {
			t.Fatal(err)
		}
		current := rowsByID(after)
		frozen := current[projectionDriver]
		if frozen.Settlement == nil || !frozen.Settlement.Finalized || frozen.Loads[0].Fee != saved[projectionDriver].Loads[0].Fee ||
			frozen.Loads[0].TotalMiles != saved[projectionDriver].Loads[0].TotalMiles {
			t.Fatal("export replaced frozen financial values")
		}
		if current[draft].Loads[0].Fee != "70.00" || saved[draft].Loads[0].Fee != "50.00" {
			t.Fatalf("export used stale draft pay: before=%q after=%q", saved[draft].Loads[0].Fee, current[draft].Loads[0].Fee)
		}
	})
}
