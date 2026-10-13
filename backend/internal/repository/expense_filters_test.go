package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestExpenseFiltersAndRetiredCompanyDatabase(t *testing.T) {
	dsn := os.Getenv("MSERP_ACCESS_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set MSERP_ACCESS_TEST_DATABASE_URL to a disposable local _test database")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.HasSuffix(cfg.ConnConfig.Database, "_test") || (cfg.ConnConfig.Host != "127.0.0.1" && cfg.ConnConfig.Host != "localhost") {
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
	for _, mode := range []string{"fresh", "upgrade"} {
		t.Run(mode, func(t *testing.T) {
			schema := fmt.Sprintf("expense_filters_%d", time.Now().UnixNano())
			quoted := pgx.Identifier{schema}.Sanitize()
			exec := func(q string, args ...any) {
				t.Helper()
				if _, err := admin.Exec(ctx, q, args...); err != nil {
					t.Fatal(err)
				}
			}
			exec(`CREATE SCHEMA ` + quoted + `; SET search_path TO ` + quoted + `,public`)
			defer func() { _, _ = admin.Exec(ctx, `SET search_path TO public; DROP SCHEMA `+quoted+` CASCADE`) }()
			if mode == "upgrade" {
				before, after, ok := strings.Cut(string(source), "-- Retire expense company (compatibility bridge, migration 087).")
				if !ok {
					t.Fatal("missing migration boundary")
				}
				exec(before)
				exec(`INSERT INTO expenses(company,category_id,category,expense_type,amount,expense_date) SELECT 'Historical company',id,name,'Before migration',7,'2026-10-12' FROM expense_settings WHERE kind='category' AND name='Other'`)
				exec(after)
				var count int
				if err := admin.QueryRow(ctx, `SELECT count(*) FROM expenses WHERE expense_type='Before migration' AND amount=7`).Scan(&count); err != nil || count != 1 {
					t.Fatal("migration lost expense", err)
				}
			} else {
				exec(string(source))
			}
			exec(`GRANT USAGE ON SCHEMA ` + quoted + ` TO mserp_app; GRANT SELECT,INSERT,UPDATE,DELETE ON ALL TABLES IN SCHEMA ` + quoted + ` TO mserp_app`)
			config := cfg.Copy()
			config.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
			config.ConnConfig.RuntimeParams["role"] = "mserp_app"
			pool, err := pgxpool.NewWithConfig(ctx, config)
			if err != nil {
				t.Fatal(err)
			}
			defer pool.Close()
			var category, hidden, driver, truck string
			id := func(q string, args ...any) string {
				t.Helper()
				var v string
				if err := admin.QueryRow(ctx, q, args...).Scan(&v); err != nil {
					t.Fatal(err)
				}
				return v
			}
			category = id(`SELECT id::text FROM expense_settings WHERE kind='category' AND name='Other'`)
			hidden = id(`SELECT id::text FROM expense_settings WHERE kind='category' AND name='Maintenance'`)
			driver = id(`INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate) VALUES('Filter Driver','filter driver','cpm',0.75) RETURNING id::text`)
			truck = id(`INSERT INTO trucks(unit_number) VALUES('FILTER-01') RETURNING id::text`)
			repo := NewExpenseRepository(pool)
			date, _ := time.Parse(time.DateOnly, "2026-10-12")
			input := ExpenseInput{CategoryID: category, ExpenseDate: date, Amount: "10.25", ExpenseType: payTestString("Filter purchase"), DriverID: &driver, TruckID: &truck, PaymentType: payTestString("Cash"), PaidBy: payTestString("Former Payer"), CoveredBy: payTestString("Company")}
			created, err := repo.CreateExpense(ctx, input)
			if err != nil {
				t.Fatal("create without company", err)
			}
			input.Amount = "20.25"
			updated, err := repo.UpdateExpense(ctx, created.ID, input)
			if err != nil {
				t.Fatal("edit without company", err)
			}
			encoded, _ := json.Marshal(updated)
			if strings.Contains(string(encoded), `"company"`) {
				t.Fatal("company still exposed")
			}
			input.TruckID = nil
			input.DriverID = nil
			input.UnitNumber = payTestString("LEGACY-02")
			input.DriverName = payTestString("Legacy Driver")
			input.PaidBy = payTestString("Other Payer")
			input.PaymentType = payTestString("Retired card")
			input.CoveredBy = payTestString("Broker")
			input.Amount = "40"
			if _, err = repo.CreateExpense(ctx, input); err != nil {
				t.Fatal(err)
			}
			input.CategoryID = hidden
			input.DriverName = payTestString("Hidden Driver")
			input.UnitNumber = payTestString("HIDDEN")
			input.PaidBy = payTestString("Hidden Payer")
			input.PaymentType = payTestString("Hidden card")
			input.CoveredBy = payTestString("Hidden responsibility")
			if _, err = repo.CreateExpense(ctx, input); err != nil {
				t.Fatal(err)
			}
			base := ExpensePageQuery{Search: "Filter purchase", VisibleCategoryIDs: []string{category}, AccessibleCategoryIDs: []string{category}, Pagination: Pagination{Page: 1, PageSize: 1}}
			checks := []ExpensePageQuery{base, base, base, base, base, base, base, base}
			checks[1].UnitNumber = "FILTER-01"
			checks[2].DriverName = "Filter Driver"
			checks[3].PaymentType = "Cash"
			checks[4].PaidBy = "Former Payer"
			checks[5].CoveredBy = "Company"
			checks[6].UnitNumber = "LEGACY-02"
			checks[6].DriverName = "Legacy Driver"
			checks[6].PaymentType = "Retired card"
			checks[6].PaidBy = "Other Payer"
			checks[6].CoveredBy = "Broker"
			checks[7].PaidBy = "Former Payer"
			checks[7].CoveredBy = "Broker"
			for i, q := range checks {
				page, err := repo.ListExpensesPage(ctx, q)
				if err != nil {
					t.Fatal(err)
				}
				wantTotal, wantAmount := 1, "20.25"
				if i == 0 {
					wantTotal, wantAmount = 2, "60.25"
				}
				if i == 6 {
					wantAmount = "40.00"
				}
				if i == 7 {
					wantTotal, wantAmount = 0, "0"
				}
				if page.Total != wantTotal || page.Summary.Amount != wantAmount {
					t.Fatalf("filter %d: %+v", i, page)
				}
				if !reflect.DeepEqual(page.Options.Filters.PaidBy, []string{"Former Payer", "Other Payer"}) || strings.Contains(fmt.Sprint(page.Options.Filters), "Hidden") {
					t.Fatalf("options leak inaccessible data: %+v", page.Options.Filters)
				}
				if len(page.Options.Filters.Drivers) != 2 || len(page.Options.Filters.Trucks) != 2 || len(page.Options.Filters.PaymentTypes) != 2 {
					t.Fatal("filter choices lost on filtered/empty result")
				}
			}
		})
	}
}
