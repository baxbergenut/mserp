package repository

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestLayoverExclusions(t *testing.T) {
	for _, dates := range [][]string{{"2026-10-04"}, {"2026-10-12"}, {"2026-10-05", "2026-10-05"}, {"bad"}} {
		if validateLayoverExclusions("2026-10-05", dates) == nil {
			t.Fatalf("accepted %v", dates)
		}
	}
	if err := validateLayoverExclusions("2026-10-05", []string{"2026-10-05", "2026-10-11"}); err != nil {
		t.Fatal(err)
	}
}

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
	exec(`INSERT INTO gross_board_entries(driver_id,service_date,day_status) VALUES($1,'2026-10-05','SHOP'),($1,'2026-10-06','SHOP'),($1,'2026-10-07','HOME'),($1,'2026-10-04','SHOP'),($1,'2026-10-19','SHOP')`, driver)
	exec(`INSERT INTO gross_board_extra_entries(driver_id,service_date,slot,day_status,deleted) VALUES($1,'2026-10-05',1,'SHOP',false),($1,'2026-10-08',1,'SHOP',true)`, driver)
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
	if want := []PayDayStatus{{"2026-10-05", "SHOP"}, {"2026-10-06", "SHOP"}, {"2026-10-07", "HOME"}}; !reflect.DeepEqual(report.Drivers[0].DayStatuses, want) {
		t.Fatalf("dated display statuses: got %+v, want %+v", report.Drivers[0].DayStatuses, want)
	}
	if len(report.Drivers) != 1 || len(report.Drivers[0].Loads) != 0 || len(report.Drivers[0].Layovers) != 2 || driverLayoverTotal(report.Drivers[0]) != 20000 {
		t.Fatalf("distinct Shop-only days: %+v", report)
	}
	if total := PaginatePay(report, PayPageQuery{}).Pagination.Payable; total != "200.00" {
		t.Fatal("bonus sign/total", total)
	}
	history, err := repo.History(ctx, driver, Pagination{Page: 1, PageSize: 10})
	if err != nil || history.Total < 1 {
		t.Fatal("Shop-only history", history, err)
	}
	var writes int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM driver_pay_weeks`).Scan(&writes); err != nil || writes != 0 {
		t.Fatal("reads wrote payroll", err)
	}
	settings, err := repo.Settings(ctx)
	if err != nil || settings.LayoverDailyAmount != "100.00" {
		t.Fatal(settings, err)
	}
	stale := settings
	settings.LayoverDailyAmount = "125.25"
	settings, err = repo.SaveSettings(ctx, settings, "")
	if err != nil || driverLayoverTotal(read().Drivers[0]) != 25050 {
		t.Fatal("configurable exact rate", err)
	}
	if _, err = repo.SaveSettings(ctx, stale, ""); !errors.Is(err, ErrDriverPayConflict) {
		t.Fatal("stale settings accepted", err)
	}
	report = read()
	edits := report.Drivers[0].Edits
	edits.LayoverExcludedDates = []string{"2026-10-05"}
	saved, err := repo.Save(ctx, edits)
	if err != nil {
		t.Fatal("remove day", err)
	}
	if _, err = repo.Save(ctx, edits); !errors.Is(err, ErrDriverPayConflict) {
		t.Fatal("stale removal accepted", err)
	}
	if driverLayoverTotal(read().Drivers[0]) != 12525 {
		t.Fatal("removal not saved")
	}
	// Removal survives a legacy save, source removal/re-add, and a default change.
	saved.LayoverExcludedDates = nil
	saved.Notes = "legacy client note"
	if saved, err = repo.Save(ctx, saved); err != nil {
		t.Fatal(err)
	}
	exec(`UPDATE gross_board_entries SET day_status='HOME' WHERE driver_id=$1 AND service_date='2026-10-05'`, driver)
	exec(`UPDATE gross_board_entries SET day_status='SHOP' WHERE driver_id=$1 AND service_date='2026-10-05'`, driver)
	if driverLayoverTotal(read().Drivers[0]) != 12525 {
		t.Fatal("removed bonus resurrected")
	}
	next, err := repo.Get(ctx, week.AddDate(0, 0, 7))
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range next.Drivers {
		if len(d.Layovers) != 0 || driverLayoverTotal(d) != 0 {
			t.Fatal("bonus carried to next week")
		}
	}
	later, err := repo.Get(ctx, week.AddDate(0, 0, 14))
	if err != nil || len(later.Drivers) != 1 || driverLayoverTotal(later.Drivers[0]) != 12525 {
		t.Fatal("later Shop day incorrectly suppressed", err)
	}
	report = read()
	frozen, err := repo.Settle(ctx, week, driver, report.Revision, "", "", false, false)
	if err != nil || driverLayoverTotal(frozen.Drivers[0]) != 12525 {
		t.Fatal("finalize bonus", err)
	}
	settings.LayoverDailyAmount = "50.00"
	settings, err = repo.SaveSettings(ctx, settings, "")
	if err != nil || driverLayoverTotal(read().Drivers[0]) != 12525 {
		t.Fatal("finalized amount changed", err)
	}
	if _, err = repo.Save(ctx, saved); err == nil {
		t.Fatal("finalized removal editable")
	}
	reopened, err := repo.Settle(ctx, week, driver, read().Revision, "", "Review layover", true, false)
	if err != nil || driverLayoverTotal(reopened.Drivers[0]) != 5000 {
		t.Fatal("reopen lost removal or new rate", err)
	}
	settings.LayoverDailyAmount = "0"
	if _, err = repo.SaveSettings(ctx, settings, ""); err != nil {
		t.Fatal(err)
	}
	if driverLayoverTotal(read().Drivers[0]) != 0 {
		t.Fatal("zero rate does not disable bonus")
	}
}
