package repository

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestDriverEscrowDatabase(t *testing.T) {
	dsn := os.Getenv("MSERP_DRIVER_PAY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set MSERP_DRIVER_PAY_TEST_DATABASE_URL to a disposable _test database")
	}
	u, err := url.Parse(dsn)
	if err != nil || !strings.Contains(u.Path, "_test") {
		t.Fatal("only a disposable _test database is allowed")
	}
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal("test database unavailable")
	}
	defer admin.Close(ctx)
	initSQL, err := os.ReadFile("../../sql/init.sql")
	if err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile("../../sql/057_driver_escrow.sql")
	if err != nil {
		t.Fatal(err)
	}
	renameMigration, err := os.ReadFile("../../sql/058_rename_driver_escrow.sql")
	if err != nil {
		t.Fatal(err)
	}

	standalone, err := os.ReadFile("../../sql/059_standalone_escrow.sql")
	if err != nil {
		t.Fatal(err)
	}

	releaseMigration, err := os.ReadFile("../../sql/060_escrow_releases.sql")
	if err != nil {
		t.Fatal(err)
	}
	replenishment, err := os.ReadFile("../../sql/061_escrow_replenishment.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"fresh", "migration"} {
		t.Run(mode, func(t *testing.T) {
			schema := fmt.Sprintf("driver_escrow_test_%d", time.Now().UnixNano())
			quoted := pgx.Identifier{schema}.Sanitize()
			exec := func(sql string, args ...any) {
				t.Helper()
				if _, execErr := admin.Exec(ctx, sql, args...); execErr != nil {
					t.Fatal(execErr)
				}
			}
			exec(`CREATE SCHEMA ` + quoted + `;SET search_path TO ` + quoted + `,public;GRANT USAGE ON SCHEMA ` + quoted + ` TO mserp_app`)
			defer func() { _, _ = admin.Exec(ctx, `SET search_path TO public;DROP SCHEMA `+quoted+` CASCADE`) }()
			source := strings.ReplaceAll(string(initSQL), "\r\n", "\n")
			body := strings.ReplaceAll(string(migration), "\r\n", "\n")
			standaloneBody := strings.ReplaceAll(string(standalone), "\r\n", "\n")
			renameBody := strings.ReplaceAll(string(renameMigration), "\r\n", "\n")
			if mode == "migration" {
				replenishmentBody := strings.ReplaceAll(string(replenishment), "\r\n", "\n")
				if !strings.Contains(source, replenishmentBody) {
					t.Fatal("replenishment migration must match the fresh schema")
				}
				source = strings.Replace(source, replenishmentBody, "", 1)
				if !strings.Contains(source, body) || !strings.Contains(source, renameBody) {
					t.Fatal("driver escrow migrations must match the fresh schema")
				}
				source = strings.Replace(source, strings.ReplaceAll(string(releaseMigration), "\r\n", "\n"), "", 1)
				source = strings.Replace(source, standaloneBody, "", 1)
				source = strings.Replace(source, body, "", 1)
				source = strings.Replace(source, renameBody, "", 1)
			}
			exec(source)
			var legacyDriver string
			if mode == "migration" {
				if err = admin.QueryRow(ctx, `INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate,hire_date) VALUES('Legacy escrow driver','legacy escrow driver','cpm',0.75,'2026-09-28') RETURNING id`).Scan(&legacyDriver); err != nil {
					t.Fatal(err)
				}
				exec(body)
				exec(renameBody)
				exec(`INSERT INTO expense_payments(expense_id,week_start,amount) SELECT id,'2026-01-05',500.25 FROM expenses WHERE driver_id=$1`, legacyDriver)
				exec(standaloneBody)
				exec(string(releaseMigration))
				exec(string(replenishment))
			}
			for _, table := range []string{"driver_escrow_settings", "driver_escrows", "driver_escrow_payments"} {
				var owner string
				if err = admin.QueryRow(ctx, `SELECT tableowner FROM pg_tables WHERE schemaname=$1 AND tablename=$2`, schema, table).Scan(&owner); err != nil || owner != "mserp_app" {
					t.Fatalf("%s owner=%q err=%v", table, owner, err)
				}
			}
			exec(`GRANT USAGE ON SCHEMA ` + quoted + ` TO mserp_app;GRANT SELECT,INSERT,UPDATE,DELETE ON ALL TABLES IN SCHEMA ` + quoted + ` TO mserp_app`)

			cfg, parseErr := pgxpool.ParseConfig(dsn)
			if parseErr != nil {
				t.Fatal(parseErr)
			}
			cfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
			cfg.ConnConfig.RuntimeParams["role"] = "mserp_app"
			pool, poolErr := pgxpool.NewWithConfig(ctx, cfg)
			if poolErr != nil {
				t.Fatal(poolErr)
			}
			defer pool.Close()

			expenses := NewEscrowRepository(pool)
			setting, err := expenses.GetDriverEscrowSetting(ctx)
			if err != nil || setting.DefaultAmount != "2500.00" || setting.Version != 1 {
				t.Fatalf("initial setting=%+v err=%v", setting, err)
			}
			saved, err := expenses.SaveDriverEscrowSetting(ctx, DriverEscrowSetting{DefaultAmount: "2750", Version: setting.Version})
			if err != nil || saved.DefaultAmount != "2750.00" || saved.Version != 2 {
				t.Fatalf("saved setting=%+v err=%v", saved, err)
			}
			if _, err = expenses.SaveDriverEscrowSetting(ctx, setting); !errors.Is(err, ErrDriverEscrowSettingConflict) {
				t.Fatalf("stale setting accepted: %v", err)
			}
			if legacyDriver != "" {
				assertEscrowAccount(t, ctx, pool, legacyDriver, "2500.00", "2026-09-28")
				balances, e := expenses.List(ctx, EscrowQuery{DriverID: legacyDriver, Status: "partial"})
				if e != nil || balances.Total != 1 || balances.Items[0].PaidAmount != "500.25" || balances.Items[0].RemainingAmount != "1999.75" || len(balances.Items[0].Payments) != 1 {
					t.Fatalf("migration lost paid balances: %+v %v", balances, e)
				}
			}

			hireDate := time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC)
			fleet := NewFleetRepository(pool)
			driver, err := fleet.CreateDriver(ctx, DriverInput{FullName: "Custom Escrow", PayType: "cpm", PayRate: .75, Active: true, HireDate: &hireDate, EscrowAmount: "3100", AssignmentWeek: hireDate.Format(time.DateOnly)})
			if err != nil {
				t.Fatal(err)
			}
			assertEscrowAccount(t, ctx, pool, driver.ID, "3100.00", "2026-09-28")
			report, err := NewDriverPayRepository(pool).Get(ctx, hireDate)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, card := range report.Drivers {
				if card.ID == driver.ID && len(card.Edits.ExpenseDeductions) == 1 && card.Edits.ExpenseDeductions[0].Name == "Escrow" && card.Edits.ExpenseDeductions[0].Amount == "3100.00" {
					found = true
				}
			}
			if !found {
				t.Fatalf("escrow missing from Driver Pay: %+v", report.Drivers)
			}

			if _, err = fleet.UpdateDriver(ctx, driver.ID, DriverInput{FullName: driver.FullName, PayType: driver.PayType, PayRate: driver.PayRate, Active: true, HireDate: driver.HireDate, EscrowAmount: "9999"}); err != nil {
				t.Fatal(err)
			}
			assertEscrowAccount(t, ctx, pool, driver.ID, "3100.00", "2026-09-28")
			defaulted, err := fleet.CreateDriver(ctx, DriverInput{FullName: "Default Escrow", PayType: "cpm", PayRate: .75, Active: true, HireDate: &hireDate, AssignmentWeek: hireDate.Format(time.DateOnly)})
			if err != nil {
				t.Fatal(err)
			}
			assertEscrowAccount(t, ctx, pool, defaulted.ID, "2750.00", "2026-09-28")
			verifyEscrowCollections(t, ctx, pool, driver.ID, hireDate)
			verifyEscrowSummaryAndRoster(t, ctx, pool, hireDate)
			verifyEscrowReleases(t, ctx, pool)
		})
	}
}

func verifyEscrowSummaryAndRoster(t *testing.T, ctx context.Context, pool *pgxpool.Pool, week time.Time) {
	t.Helper()
	ids := map[string]string{}
	for _, seed := range []struct {
		name   string
		active bool
		paid   string
	}{{"Summary Fully", true, "2500"}, {"Summary Partial", true, "100.25"}, {"Summary Unpaid", true, "0"}, {"Summary Inactive", false, "500"}} {
		d, err := NewFleetRepository(pool).CreateDriver(ctx, DriverInput{FullName: seed.name, PayType: "cpm", PayRate: .75, Active: seed.active, HireDate: &week, EscrowAmount: "2500", AssignmentWeek: week.Format(time.DateOnly)})
		if err != nil {
			t.Fatal(err)
		}
		ids[seed.name] = d.ID
		if seed.paid != "0" {
			if _, err = pool.Exec(ctx, `INSERT INTO driver_escrow_payments(escrow_id,week_start,amount) SELECT id,$2,$3::numeric FROM driver_escrows WHERE driver_id=$1`, d.ID, week, seed.paid); err != nil {
				t.Fatal(err)
			}
		}
	}
	repo := NewEscrowRepository(pool)
	q := EscrowQuery{Search: "Summary", Pagination: Pagination{Page: 1, PageSize: 1}}
	result, err := repo.List(ctx, q)
	want := EscrowSummary{Held: "2600.25", Released: "0", Target: "7500.00", Paid: "2600.25", Remaining: "4899.75", Drivers: 3, PaidDrivers: 1, PartialDrivers: 1, UnpaidDrivers: 1}
	if err != nil || result.Total != 3 || len(result.Items) != 1 || result.Summary != want {
		t.Fatalf("filtered summary across pages: %+v %v", result, err)
	}
	q.IncludeInactive = true
	result, err = repo.List(ctx, q)
	if err != nil || result.Total != 4 || result.Summary.Paid != "3100.25" || result.Summary.Remaining != "6899.75" || result.Summary.PartialDrivers != 1 || result.Summary.Drivers != 3 || result.Summary.PaidDrivers != 1 {
		t.Fatalf("include inactive summary: %+v %v", result, err)
	}
	q.Status = "unpaid"
	result, err = repo.List(ctx, q)
	if err != nil || result.Total != 1 || result.Summary.Paid != "0.00" || result.Summary.UnpaidDrivers != 1 {
		t.Fatalf("status summary: %+v %v", result, err)
	}
	q.Search = "does not exist"
	result, err = repo.List(ctx, q)
	if err != nil || result.Total != 0 || result.Summary.Drivers != 0 || result.Summary.Paid != "0" {
		t.Fatalf("empty summary: %+v %v", result, err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO driver_pay_cost_collections(driver_id,week_start,fuel_base,toll_base,fuel_amount,toll_amount) VALUES($1,$2,100,0,0,0)`, ids["Summary Inactive"], week); err != nil {
		t.Fatal(err)
	}
	pay := NewDriverPayRepository(pool)
	next := week.AddDate(0, 0, 7)
	report, err := pay.Get(ctx, next)
	if err != nil {
		t.Fatal(err)
	}
	foundUnpaid := false
	for _, d := range report.Drivers {
		if d.ID == ids["Summary Inactive"] || d.ID == ids["Summary Fully"] {
			t.Fatalf("escrow created unwanted payroll entry: %s", d.FullName)
		}
		foundUnpaid = foundUnpaid || d.ID == ids["Summary Unpaid"]
	}
	if !foundUnpaid {
		t.Fatal("active unpaid escrow missing from payroll")
	}
	if _, err = pool.Exec(ctx, `INSERT INTO driver_pay_weeks(driver_id,week_start,notes) VALUES($1,$2,'Saved payroll history')`, ids["Summary Inactive"], week); err != nil {
		t.Fatal(err)
	}
	historical, err := pay.GetDriverWeek(ctx, week, ids["Summary Inactive"])
	if err != nil || len(historical.Drivers) != 1 || historical.Drivers[0].Edits.Notes != "Saved payroll history" {
		t.Fatalf("inactive saved history hidden: %+v %v", historical, err)
	}
}

func verifyEscrowCollections(t *testing.T, ctx context.Context, pool *pgxpool.Pool, driver string, week time.Time) {
	t.Helper()
	pay := NewDriverPayRepository(pool)
	escrows := NewEscrowRepository(pool)
	read := func(w time.Time) DriverPayEdits {
		t.Helper()
		report, err := pay.GetDriverWeek(ctx, w, driver)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range report.Drivers {
			if d.ID == driver {
				return d.Edits
			}
		}
		return DriverPayEdits{}
	}
	balance := func(status, paid, remaining string) {
		t.Helper()
		result, err := escrows.List(ctx, EscrowQuery{DriverID: driver, Status: status})
		if err != nil || result.Total != 1 || result.Items[0].PaidAmount != paid || result.Items[0].RemainingAmount != remaining {
			t.Fatalf("balance: %+v %v", result, err)
		}
	}
	if len(read(week.AddDate(0, 0, -7)).ExpenseDeductions) != 0 {
		t.Fatal("escrow suggested before September 28")
	}
	first := read(week)
	stale := read(week.AddDate(0, 0, 7))
	balance("unpaid", "0.00", "3100.00")
	first.ExpenseDeductions[0].Amount = "100.25"
	first.ExpenseDeductions[0].Apply = true
	if _, err := pay.Save(ctx, first); err != nil {
		t.Fatal(err)
	}
	balance("partial", "100.25", "2999.75")
	stale.ExpenseDeductions[0].Apply = true
	if _, err := pay.Save(ctx, stale); !errors.Is(err, ErrDriverPayConflict) {
		t.Fatalf("stale cross-week payment accepted: %v", err)
	}
	next := read(week.AddDate(0, 0, 7))
	if next.ExpenseDeductions[0].Amount != "2999.75" || next.ExpenseDeductions[0].Source != "escrow" {
		t.Fatalf("bad carry: %+v", next)
	}
	next.ExpenseDeductions[0].Amount = "3000"
	next.ExpenseDeductions[0].Apply = true
	if _, err := pay.Save(ctx, next); err == nil {
		t.Fatal("overpayment accepted")
	}
	next.ExpenseDeductions[0].Amount = "0"
	if _, err := pay.Save(ctx, next); err != nil {
		t.Fatal(err)
	}
	balance("partial", "100.25", "2999.75")
	// Reserve a future payment. An earlier week may only use unallocated principal.
	later := read(week.AddDate(0, 0, 14))
	later.ExpenseDeductions[0].Amount = "50.50"
	later.ExpenseDeductions[0].Apply = true
	if _, err := pay.Save(ctx, later); err != nil {
		t.Fatal(err)
	}
	first = read(week)
	if first.ExpenseDeductions[0].Available != "3049.50" {
		t.Fatal("future payment not reserved", first)
	}
	// Finalization locks escrow collections; reopening retains them for correction.
	report, err := pay.Get(ctx, week)
	if err != nil {
		t.Fatal(err)
	}
	final, err := pay.Settle(ctx, week, driver, report.Revision, "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `UPDATE driver_escrow_payments SET amount=0 WHERE escrow_id=$1 AND week_start=$2`, first.ExpenseDeductions[0].ExpenseID, week); err == nil {
		t.Fatal("database allowed finalized escrow edit")
	}
	if _, err = pay.Settle(ctx, week, driver, final.Revision, "", "Correction", true); err != nil {
		t.Fatal(err)
	}
	balance("partial", "150.75", "2949.25")
	first = read(week)
	first.ExpenseDeductions[0].Amount = first.ExpenseDeductions[0].Available
	first.ExpenseDeductions[0].Apply = true
	if _, err = pay.Save(ctx, first); err != nil {
		t.Fatal(err)
	}
	balance("paid", "3100.00", "0.00")
	if len(read(week.AddDate(0, 0, 21)).ExpenseDeductions) > 0 {
		t.Fatal("paid escrow suggested again")
	}
	history, err := pay.History(ctx, driver, Pagination{PageSize: 100})
	if err != nil || history.Total == 0 {
		t.Fatalf("missing payroll history: %+v %v", history, err)
	}
}

func assertEscrowAccount(t *testing.T, ctx context.Context, pool *pgxpool.Pool, driver, amount, date string) {
	t.Helper()
	var count int
	var gotAmount, gotDate string
	err := pool.QueryRow(ctx, `SELECT count(*)::int,min(amount)::text,min(start_date)::text FROM driver_escrows WHERE driver_id=$1`, driver).Scan(&count, &gotAmount, &gotDate)
	if err != nil || count != 1 || gotAmount != amount || gotDate != date {
		t.Fatalf("escrow count=%d amount=%s date=%s err=%v", count, gotAmount, gotDate, err)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM expenses WHERE driver_id=$1`, driver).Scan(&count); err != nil || count != 0 {
		t.Fatalf("escrow leaked into expenses: %d %v", count, err)
	}
}
