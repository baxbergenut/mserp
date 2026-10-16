package repository

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPenaltyCategoryDatabase(t *testing.T) {
	dsn := os.Getenv("MSERP_DRIVER_PAY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set disposable local MSERP_DRIVER_PAY_TEST_DATABASE_URL")
	}
	u, err := url.Parse(dsn)
	if err != nil || !strings.HasSuffix(u.Path, "_test") || (u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1") {
		t.Fatal("local _test database required")
	}
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(ctx)
	source, err := os.ReadFile("../../sql/init.sql")
	if err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile("../../sql/106_driver_only_penalties.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, upgrade := range []bool{false, true} {
		t.Run(fmt.Sprint(upgrade), func(t *testing.T) {
			schema := fmt.Sprintf("penalties_%d", time.Now().UnixNano())
			exec := func(sql string) {
				t.Helper()
				if _, err := admin.Exec(ctx, sql); err != nil {
					t.Fatal(err)
				}
			}
			exec("CREATE SCHEMA " + schema + " AUTHORIZATION mserp_app; SET search_path TO " + schema + ",public")
			defer func() { admin.Exec(ctx, "RESET ROLE; SET search_path TO public; DROP SCHEMA "+schema+" CASCADE") }()
			sql := string(source)
			if upgrade {
				sql = strings.Replace(sql, string(migration), "", 1)
			}
			exec(sql)
			if upgrade {
				exec(`INSERT INTO expenses(category_id,category,expense_date,amount,expense_type,covered_by,payment_type,paid_by,driver_settled) SELECT id,name,'2026-09-01',125,'Old penalty','Company','EFS','Historical payer',true FROM expense_settings WHERE kind='category' AND name='Penalties'`)
				exec(string(migration))
				var amount, payer, covered string
				if err := admin.QueryRow(ctx, `SELECT amount::text,paid_by,covered_by FROM expenses WHERE expense_type='Old penalty'`).Scan(&amount, &payer, &covered); err != nil || amount != "125.00" || payer != "Historical payer" || covered != "Company" {
					t.Fatalf("legacy row changed: %s %s %s %v", amount, payer, covered, err)
				}
			}
			cfg, err := pgxpool.ParseConfig(dsn)
			if err != nil {
				t.Fatal(err)
			}
			cfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
			cfg.AfterConnect = func(ctx context.Context, c *pgx.Conn) error { _, err := c.Exec(ctx, "SET ROLE mserp_app"); return err }
			pool, err := pgxpool.NewWithConfig(ctx, cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer pool.Close()
			repo := NewExpenseRepository(pool)
			settings, err := repo.ListExpenseSettings(ctx)
			if err != nil {
				t.Fatal(err)
			}
			var penalty ExpenseSetting
			for _, s := range settings {
				if s.Name == "Penalties" {
					penalty = s
				}
				if s.Kind != "category" && s.DriverOnly {
					t.Fatal("name/payment setting acquired category rules")
				}
			}
			if !penalty.DriverOnly {
				t.Fatal("Penalties flag missing")
			}
			penalty.Name = "Driver deductions"
			renamed, err := repo.SaveExpenseSetting(ctx, penalty.ID, penalty)
			if err != nil || !renamed.DriverOnly {
				t.Fatalf("rename lost rule: %+v %v", renamed, err)
			}
			only, err := repo.ExpenseCategoryDriverOnly(ctx, penalty.ID)
			if err != nil || !only {
				t.Fatalf("lookup after rename: %t %v", only, err)
			}
		})
	}
}
