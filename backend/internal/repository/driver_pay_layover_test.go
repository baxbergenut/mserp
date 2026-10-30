package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestDriverPayLayoverDatabase(t *testing.T) {
	dsn := os.Getenv("MSERP_DRIVER_PAY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set MSERP_DRIVER_PAY_TEST_DATABASE_URL to a disposable local _test database")
	}
	u, err := url.Parse(dsn)
	if err != nil || !strings.Contains(u.Path, "_test") || (u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1") {
		t.Fatal("use a disposable local _test database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal("test database unavailable")
	}
	defer admin.Close(ctx)
	init, err := os.ReadFile("../../sql/init.sql")
	if err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile("../../sql/086_driver_pay_layover.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"fresh", "migration"} {
		t.Run(mode, func(t *testing.T) {
			schema := fmt.Sprintf("layover_test_%d", time.Now().UnixNano())
			quoted := pgx.Identifier{schema}.Sanitize()
			if _, err = admin.Exec(ctx, `CREATE SCHEMA `+quoted+`; SET search_path TO `+quoted+`,public`); err != nil {
				t.Fatal(err)
			}
			defer func() {
				_, _ = admin.Exec(context.Background(), `SET search_path TO public;DROP SCHEMA `+quoted+` CASCADE`)
			}()
			source := strings.ReplaceAll(string(init), "\r\n", "\n")
			body := strings.ReplaceAll(string(migration), "\r\n", "\n")
			if !strings.Contains(source, body) {
				t.Fatal("init must contain exact layover migration")
			}
			if mode == "migration" {
				source = strings.Replace(source, body, "", 1)
			}
			if _, err = admin.Exec(ctx, source); err != nil {
				t.Fatal(err)
			}
			if mode == "migration" {
				if _, err = admin.Exec(ctx, body); err != nil {
					t.Fatal(err)
				}
			}
			var owner string
			if err = admin.QueryRow(ctx, `SELECT tableowner FROM pg_tables WHERE schemaname=$1 AND tablename='driver_pay_settings'`, schema).Scan(&owner); err != nil || owner != "mserp_app" {
				t.Fatal("settings ownership", owner, err)
			}
			if _, err = admin.Exec(ctx, `GRANT USAGE ON SCHEMA `+quoted+` TO mserp_app; GRANT SELECT,INSERT,UPDATE,DELETE ON ALL TABLES IN SCHEMA `+quoted+` TO mserp_app; GRANT USAGE,SELECT ON ALL SEQUENCES IN SCHEMA `+quoted+` TO mserp_app`); err != nil {
				t.Fatal(err)
			}
			config, err := pgxpool.ParseConfig(dsn)
			if err != nil {
				t.Fatal("invalid test config")
			}
			config.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
			config.ConnConfig.RuntimeParams["role"] = "mserp_app"
			pool, err := pgxpool.NewWithConfig(ctx, config)
			if err != nil {
				t.Fatal(err)
			}
			defer pool.Close()
			verifyDriverPayLayovers(t, ctx, pool)
			testPayrollDayStatusRouting(t, ctx, pool)
			testPayrollAssignmentLabels(t, ctx, pool)
		})
	}
}

func verifyDriverPayLayovers(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	var driver string
	if err := pool.QueryRow(ctx, `INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate,active) VALUES('Shop Example','shop example','cpm',0.75,false) RETURNING id`).Scan(&driver); err != nil {
		t.Fatal(err)
	}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO gross_board_entries(driver_id,service_date,day_status) VALUES($1,'2026-10-05','SHOP'),($1,'2026-10-06','SHOP'),($1,'2026-10-07','HOME'),($1,'2026-10-19','SHOP')`, driver)
	exec(`INSERT INTO gross_board_extra_entries(driver_id,service_date,slot,day_status) VALUES($1,'2026-10-05',1,'SHOP')`, driver)
	// A retained nonzero setting must never enable automatic additions.
	exec(`UPDATE driver_pay_settings SET layover_daily_amount=125.25`)
	repo := NewDriverPayRepository(pool)
	week, _ := time.Parse(time.DateOnly, "2026-10-05")
	read := func() DriverPayWeek {
		t.Helper()
		r, err := repo.Get(ctx, week)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	report := read()
	if len(report.Drivers) != 0 {
		t.Fatalf("Shop-only inactive driver created a payable statement: %+v", report)
	}
	history, err := repo.History(ctx, driver, Pagination{Page: 1, PageSize: 10})
	if err != nil || history.Total != 0 {
		t.Fatal("Shop-only history", history, err)
	}
	var writes int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM driver_pay_weeks`).Scan(&writes); err != nil || writes != 0 {
		t.Fatal("reads wrote payroll", err)
	}
	// Keep an ordinary manually edited statement, including legacy exclusion data.
	exec(`INSERT INTO driver_pay_weeks(driver_id,week_start,adjustments,layover_excluded_dates) VALUES($1,'2026-10-05','[{"id":"manual","name":"Manual bonus","amount":"25.00"}]','{2026-10-05}')`, driver)
	report = read()
	if len(report.Drivers) != 1 || len(report.Drivers[0].Layovers) != 0 || driverLayoverTotal(report.Drivers[0]) != 0 {
		t.Fatalf("open statement generated layovers: %+v", report)
	}
	if got := PaginatePay(report, PayPageQuery{}).Pagination.Payable; got != "25.00" {
		t.Fatal("manual adjustment", got)
	}
	if len(report.Drivers[0].DayStatuses) != 3 {
		t.Fatal("Shop/Home display metadata lost", report.Drivers[0].DayStatuses)
	}
	edits := report.Drivers[0].Edits
	edits.LayoverExcludedDates = []string{"2026-10-06"}
	edits.Notes = "Ordinary note edit"
	if _, err = repo.Save(ctx, edits); err != nil {
		t.Fatal(err)
	}
	var exclusions []string
	if err = pool.QueryRow(ctx, `SELECT layover_excluded_dates FROM driver_pay_weeks WHERE driver_id=$1`, driver).Scan(&exclusions); err != nil || len(exclusions) != 1 || exclusions[0] != "2026-10-05" {
		t.Fatal("retired exclusions mutated", exclusions, err)
	}
	// Simulate a pre-retirement finalized snapshot with one excluded historical day.
	legacy := read().Drivers[0]
	legacy.Layovers = []DriverPayLayover{{Date: "2026-10-05", Amount: "125.25"}, {Date: "2026-10-06", Amount: "125.25"}}
	body, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	exec(`INSERT INTO payroll_settlements(driver_id,week_start,report) VALUES($1,'2026-10-05',$2::jsonb)`, driver, string(body))
	frozen := read()
	if len(frozen.Drivers) != 1 || driverLayoverTotal(frozen.Drivers[0]) != 12525 {
		t.Fatal("historical snapshot lost", frozen)
	}
	if got := PaginatePay(frozen, PayPageQuery{}).Pagination.Payable; got != "150.25" {
		t.Fatal("historical total changed", got)
	}
	exec(`UPDATE driver_pay_settings SET layover_daily_amount=999`)
	if driverLayoverTotal(read().Drivers[0]) != 12525 {
		t.Fatal("retired rate changed historical amount")
	}
	var unchanged bool
	if err = pool.QueryRow(ctx, `SELECT report=$2::jsonb FROM payroll_settlements WHERE driver_id=$1`, driver, string(body)).Scan(&unchanged); err != nil || !unchanged {
		t.Fatal("historical snapshot rewritten", err)
	}
	if _, err = repo.Save(ctx, read().Drivers[0].Edits); err == nil {
		t.Fatal("finalized statement editable")
	}
	reopened, err := repo.Settle(ctx, week, driver, read().Revision, "", "Review historical bonus", true, false)
	if err != nil || len(reopened.Drivers) != 1 || len(reopened.Drivers[0].Layovers) != 0 {
		t.Fatal("reopened statement regenerated layovers", reopened, err)
	}
	if got := PaginatePay(reopened, PayPageQuery{}).Pagination.Payable; got != "25.00" {
		t.Fatal("reopened manual adjustment", got)
	}
	finalized, err := repo.Settle(ctx, week, driver, reopened.Revision, "", "", false, false)
	if err != nil || len(finalized.Drivers[0].Layovers) != 0 {
		t.Fatal("new finalization generated layovers", err)
	}
	next, err := repo.Get(ctx, week.AddDate(0, 0, 14))
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range next.Drivers {
		if len(d.Layovers) != 0 {
			t.Fatal("later Shop day generated layovers")
		}
	}
}
