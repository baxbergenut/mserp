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
			if mode == "migration" {
				if !strings.Contains(source, body) {
					t.Fatal("migration 057 must match the fresh schema")
				}
				source = strings.Replace(source, body, "", 1)
			}
			exec(source)
			var legacyDriver string
			if mode == "migration" {
				if err = admin.QueryRow(ctx, `INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate,hire_date) VALUES('Legacy escrow driver','legacy escrow driver','cpm',0.75,'2026-09-28') RETURNING id`).Scan(&legacyDriver); err != nil {
					t.Fatal(err)
				}
				exec(body)
			}
			var owner string
			if err = admin.QueryRow(ctx, `SELECT tableowner FROM pg_tables WHERE schemaname=$1 AND tablename='driver_escrow_settings'`, schema).Scan(&owner); err != nil || owner != "mserp_app" {
				t.Fatalf("driver escrow settings owner=%q err=%v", owner, err)
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

			expenses := NewExpenseRepository(pool)
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
				assertEscrowExpense(t, ctx, pool, legacyDriver, "2500.00", "2026-09-28")
			}

			hireDate := time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC)
			fleet := NewFleetRepository(pool)
			driver, err := fleet.CreateDriver(ctx, DriverInput{FullName: "Custom Escrow", PayType: "cpm", PayRate: .75, Active: true, HireDate: &hireDate, EscrowAmount: "3100"})
			if err != nil {
				t.Fatal(err)
			}
			assertEscrowExpense(t, ctx, pool, driver.ID, "3100.00", "2026-09-28")
			report, err := NewDriverPayRepository(pool).Get(ctx, hireDate)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, card := range report.Drivers {
				if card.ID == driver.ID && len(card.Edits.ExpenseDeductions) == 1 && card.Edits.ExpenseDeductions[0].Name == "Escrow payment" && card.Edits.ExpenseDeductions[0].Amount == "3100.00" {
					found = true
				}
			}
			if !found {
				t.Fatalf("escrow missing from Driver Pay: %+v", report.Drivers)
			}

			if _, err = fleet.UpdateDriver(ctx, driver.ID, DriverInput{FullName: driver.FullName, PayType: driver.PayType, PayRate: driver.PayRate, Active: true, HireDate: driver.HireDate, EscrowAmount: "9999"}); err != nil {
				t.Fatal(err)
			}
			assertEscrowExpense(t, ctx, pool, driver.ID, "3100.00", "2026-09-28")
			defaulted, err := fleet.CreateDriver(ctx, DriverInput{FullName: "Default Escrow", PayType: "cpm", PayRate: .75, Active: true, HireDate: &hireDate})
			if err != nil {
				t.Fatal(err)
			}
			assertEscrowExpense(t, ctx, pool, defaulted.ID, "2750.00", "2026-09-28")
		})
	}
}

func assertEscrowExpense(t *testing.T, ctx context.Context, pool *pgxpool.Pool, driver, amount, date string) {
	t.Helper()
	var count int
	var gotAmount, gotDate, category, name, coveredBy, chargeDriver string
	err := pool.QueryRow(ctx, `SELECT count(*)::int,min(amount)::text,min(expense_date)::text,min(category),min(expense_type),min(covered_by),min(charge_driver_id::text) FROM expenses WHERE driver_id=$1 AND system_kind='driver_escrow'`, driver).Scan(&count, &gotAmount, &gotDate, &category, &name, &coveredBy, &chargeDriver)
	if err != nil || count != 1 || gotAmount != amount || gotDate != date || category != "Safety" || name != "Escrow payment" || coveredBy != "Driver" || chargeDriver != driver {
		t.Fatalf("escrow count=%d amount=%q date=%q category=%q name=%q coveredBy=%q chargeDriver=%q err=%v", count, gotAmount, gotDate, category, name, coveredBy, chargeDriver, err)
	}
}
