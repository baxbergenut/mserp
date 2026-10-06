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
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestExpenseSettingsDatabase(t *testing.T) {
	dsn := os.Getenv("MSERP_DRIVER_PAY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set MSERP_DRIVER_PAY_TEST_DATABASE_URL to a disposable _test database")
	}
	u, err := url.Parse(dsn)
	if err != nil || !strings.Contains(u.Path, "_test") {
		t.Fatal("only disposable _test databases allowed")
	}
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal("test database unavailable")
	}
	defer admin.Close(ctx)
	source, err := os.ReadFile("../../sql/init.sql")
	if err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile("../../sql/040_expense_settings.sql")
	if err != nil {
		t.Fatal(err)
	}
	accessMigration, err := os.ReadFile("../../sql/056_expense_category_access.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"fresh", "migration"} {
		t.Run(mode, func(t *testing.T) {
			schema := fmt.Sprintf("expense_settings_test_%d", time.Now().UnixNano())
			quoted := pgx.Identifier{schema}.Sanitize()
			exec := func(sql string) {
				t.Helper()
				if _, err := admin.Exec(ctx, sql); err != nil {
					t.Fatal(err)
				}
			}
			exec(`CREATE SCHEMA ` + quoted + `;SET search_path TO ` + quoted + `,public`)
			defer func() { _, _ = admin.Exec(ctx, `SET search_path TO public;DROP SCHEMA `+quoted+` CASCADE`) }()
			sql := strings.ReplaceAll(string(source), "\r\n", "\n")
			if mode == "migration" {
				sql = strings.Replace(sql, strings.ReplaceAll(string(migration), "\r\n", "\n"), "", 1)
				var found bool
				sql, _, found = strings.Cut(sql, "BEGIN;\n\n-- Durable expense categories")
				if !found {
					t.Fatal("missing migration 056 boundary")
				}
			}
			exec(sql)
			if mode == "migration" {
				exec(`INSERT INTO expenses(company,category,expense_type,payment_type,paid_by,expense_date,amount) VALUES('MS Express','Maintenance','Old custom repair','Legacy card','Legacy payer','2026-09-28',10)`)
				exec(string(migration))
				exec(`INSERT INTO expense_settings(kind,name) VALUES('category','Legacy customs')`)
				exec(`INSERT INTO expenses(company,category,expense_type,expense_date,amount) VALUES('MS Express','Legacy customs','Historical unmatched entry','2026-09-21',20)`)
				exec(`UPDATE expense_settings SET name='Modern customs' WHERE kind='category' AND name='Legacy customs'`)
				exec(string(accessMigration))
			} else {
				exec(`INSERT INTO expenses(company,category_id,category,expense_type,payment_type,paid_by,expense_date,amount) SELECT 'MS Express',id,name,'Old custom repair','Legacy card','Legacy payer','2026-09-28',10 FROM expense_settings WHERE kind='category' AND name='Maintenance'`)
			}
			exec(`GRANT USAGE ON SCHEMA ` + quoted + ` TO mserp_app;GRANT SELECT ON trucks,drivers,truck_driver_assignments,app_users TO mserp_app`)
			cfg, err := pgxpool.ParseConfig(dsn)
			if err != nil {
				t.Fatal(err)
			}
			cfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
			cfg.ConnConfig.RuntimeParams["role"] = "mserp_app"
			pool, err := pgxpool.NewWithConfig(ctx, cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer pool.Close()
			repo := NewExpenseRepository(pool)
			save := func(id string, v ExpenseSetting) ExpenseSetting {
				t.Helper()
				r, e := repo.SaveExpenseSetting(ctx, id, v)
				if e != nil {
					t.Fatal(e)
				}
				return r
			}
			items, err := repo.ListExpenseSettings(ctx)
			if err != nil {
				t.Fatal(err)
			}
			categories := map[string]string{}
			legacyArchived := false
			for _, item := range items {
				if item.Kind == "category" {
					categories[item.Name] = item.ID
					if item.Name == "Legacy customs" && !item.Active {
						legacyArchived = true
					}
				}
				if item.Kind == "name" && item.Name == "Old custom repair" {
					t.Fatal("custom expense became a default")
				}
			}
			if mode == "migration" && !legacyArchived {
				t.Fatal("unmatched historical category was not archived")
			}
			for _, item := range items {
				if item.Name == "Tire replacement" && (item.CategoryID == nil || *item.CategoryID != categories["Maintenance"]) {
					t.Fatal("default not linked to its category")
				}
			}
			cat := save("", ExpenseSetting{Kind: "category", Name: "  Travel  costs ", Active: true})
			if cat.Name != "Travel costs" {
				t.Fatal("name not normalized")
			}
			name := save("", ExpenseSetting{Kind: "name", CategoryID: &cat.ID, Name: "Hotel", Active: true})
			other := save("", ExpenseSetting{Kind: "name", CategoryID: payTestString(categories["HR"]), Name: "Hotel", Active: true})
			if name.ID == other.ID {
				t.Fatal("names not scoped by category")
			}
			_, err = repo.SaveExpenseSetting(ctx, "", ExpenseSetting{Kind: "name", CategoryID: &cat.ID, Name: "hotel", Active: true})
			var pg *pgconn.PgError
			if !errors.As(err, &pg) || pg.Code != "23505" {
				t.Fatalf("duplicate accepted: %v", err)
			}
			input := ExpenseInput{Company: "MS Express", CategoryID: cat.ID, ExpenseDate: time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC), Amount: "100", ExpenseType: payTestString("One-off travel")}
			expense, err := repo.CreateExpense(ctx, input)
			if err != nil {
				t.Fatal(err)
			}
			old := cat
			cat.Name = "Travel"
			cat = save(cat.ID, cat)
			if _, err = repo.SaveExpenseSetting(ctx, old.ID, old); !errors.Is(err, ErrExpenseSettingConflict) {
				t.Fatalf("stale update accepted: %v", err)
			}
			// Saved text survives a category rename, including unrelated expense edits.
			input.Description = payTestString("Changed description")
			expense, err = repo.UpdateExpense(ctx, expense.ID, input)
			if err != nil || expense.Category != "Travel costs" {
				t.Fatalf("history changed: %+v %v", expense, err)
			}
			renamed, err := repo.CreateExpense(ctx, input)
			if err != nil || renamed.Category != "Travel" || renamed.CategoryID != cat.ID {
				t.Fatalf("renamed category identity was not used: %+v %v", renamed, err)
			}
			cat.Active = false
			cat = save(cat.ID, cat)
			if _, err = repo.CreateExpense(ctx, input); err == nil {
				t.Fatal("archived category accepted")
			}
			if _, err = repo.SaveExpenseSetting(ctx, "", ExpenseSetting{Kind: "name", CategoryID: &cat.ID, Name: "Flight", Active: true}); err == nil {
				t.Fatal("active name added to archived category")
			}
			cat.Active = true
			cat = save(cat.ID, cat)
			if _, err = repo.CreateExpense(ctx, input); err != nil {
				t.Fatal(err)
			}
			method := save("", ExpenseSetting{Kind: "payment_method", Name: "Card 1456", Active: true})
			method.Active = false
			save(method.ID, method)
			page, err := repo.ListExpensesPage(ctx, ExpensePageQuery{VisibleCategoryIDs: []string{cat.ID, categories["Maintenance"]}, AccessibleCategoryIDs: []string{cat.ID, categories["Maintenance"]}})
			if err != nil {
				t.Fatal(err)
			}
			for _, v := range page.Options.PaymentTypes {
				if v == "Card 1456" {
					t.Fatal("archived method still suggested")
				}
			}
			found := false
			for _, v := range page.Options.Categories {
				if v.ID == cat.ID {
					found = true
				}
			}
			if !found {
				t.Fatal("historical category missing from filters")
			}
		})
	}
}
