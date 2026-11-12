package repository

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestExpenseSearchAmount(t *testing.T) {
	for input, want := range map[string]string{
		"1234": "1234.00", "1234.5": "1234.50", "1234.50": "1234.50",
		"1,234.50": "1234.50", " $ 1,234.50 ": "1234.50", "$0.00": "0.00",
		".50": "0.50", "-.50": "-0.50", "+25": "25.00", "00025": "25.00",
		"-0": "0.00", "999,999,999,999.99": "999999999999.99",
		"": "", " ": "", "$": "", "+": "", "-": "", ".": "",
		"1,23": "", "1234,567": "", "1.234,50": "", "12.345": "",
		"1e3": "", "NaN": "", "Infinity": "", "25 repair": "", "INV-25": "",
		"25%": "", "1_000": "", "1000000000000": "", "25 OR 1=1": "",
	} {
		t.Run(input, func(t *testing.T) {
			if got := normalizeExpenseSearchAmount(input); got != want {
				t.Fatalf("normalizeExpenseSearchAmount(%q)=%q; want %q", input, got, want)
			}
		})
	}
}

func TestExpenseEFSReferenceDatabase(t *testing.T) {
	withTruckChargeDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, _ string) {
		repo := NewExpenseRepository(pool)
		category := func(name string) string {
			t.Helper()
			var id string
			if err := pool.QueryRow(ctx, `SELECT id FROM expense_settings WHERE kind='category' AND name=$1`, name).Scan(&id); err != nil {
				t.Fatal(err)
			}
			return id
		}
		visible, hidden := category("Other"), category("Safety")
		day, _ := time.Parse(time.DateOnly, "2026-10-14")
		input := ExpenseInput{CategoryID: visible, ExpenseDate: day, Amount: "25", CoveredBy: payTestString("Company"), ExpenseType: payTestString("Repair"), PaymentType: payTestString("EFS"), ReferenceNumber: payTestString("INV-817"), EFSReferenceNumber: payTestString("00042-ALPHA")}
		saved, err := repo.CreateExpense(ctx, input)
		if err != nil || saved.EFSReferenceNumber == nil || *saved.EFSReferenceNumber != "00042-ALPHA" || saved.ReferenceNumber == nil || *saved.ReferenceNumber != "INV-817" {
			t.Fatalf("create: %+v %v", saved, err)
		}
		hiddenInput := input
		hiddenInput.CategoryID = hidden
		if _, err := repo.CreateExpense(ctx, hiddenInput); err != nil {
			t.Fatal(err)
		}
		search := func(term string, total int) {
			t.Helper()
			page, err := repo.ListExpensesPage(ctx, ExpensePageQuery{VisibleCategoryIDs: []string{visible}, AccessibleCategoryIDs: []string{visible}, History: true, Search: term, Pagination: Pagination{Page: 1, PageSize: 10}})
			if err != nil || page.Total != total || len(page.Items) != total || (total == 1 && page.Items[0].ID != saved.ID) {
				t.Fatalf("search %q: total=%d items=%v error=%v", term, page.Total, page.Items, err)
			}
		}
		search("42-alpha", 1)
		search("00042", 1)
		search("INV-817", 1)
		input.EFSReferenceNumber = payTestString("REPLACEMENT-702")
		updated, err := repo.UpdateExpense(ctx, saved.ID, input)
		if err != nil || updated.EFSReferenceNumber == nil || *updated.EFSReferenceNumber != "REPLACEMENT-702" {
			t.Fatalf("update: %+v %v", updated, err)
		}
		search("42-alpha", 0)
		search("replacement-702", 1)
		input.PaymentType, input.EFSReferenceNumber = payTestString("Cash"), nil
		if _, err := repo.UpdateExpense(ctx, saved.ID, input); err != nil {
			t.Fatal(err)
		}
		search("replacement-702", 0)
		search("INV-817", 1)

		// Confirm that the same predicate used by the directory can use the index.
		conn, err := pool.Acquire(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Release()
		if _, err := conn.Exec(ctx, `SET enable_seqscan=off`); err != nil {
			t.Fatal(err)
		}
		defer conn.Exec(ctx, `RESET enable_seqscan`)
		var plan string
		err = conn.QueryRow(ctx, `EXPLAIN (FORMAT JSON) SELECT id FROM expenses
			WHERE efs_reference_number IS NOT NULL AND lower(btrim(payment_type))='efs'
			AND efs_reference_number ILIKE '%alpha%'`).Scan(&plan)
		if err != nil || !strings.Contains(plan, "expenses_efs_reference_trgm_idx") {
			t.Fatalf("EFS index unavailable: %v\n%s", err, plan)
		}
	})
}

func TestExpenseAmountSearchDatabase(t *testing.T) {
	withTruckChargeDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, _ string) {
		repo := NewExpenseRepository(pool)
		var category, hidden string
		if err := pool.QueryRow(ctx, `SELECT id::text FROM expense_settings WHERE kind='category' AND name='Other'`).Scan(&category); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `SELECT id::text FROM expense_settings WHERE kind='category' AND name='Safety'`).Scan(&hidden); err != nil {
			t.Fatal(err)
		}
		insert := func(category, amount, reference, date string) string {
			t.Helper()
			var id string
			err := pool.QueryRow(ctx, `INSERT INTO expenses(category_id,category,amount,reference_number,expense_date,expense_type,covered_by)
				SELECT id,name,NULLIF($2,'')::numeric,$3,$4::date,'Amount fixture','Company' FROM expense_settings WHERE id=$1
				RETURNING id::text`, category, amount, reference, date).Scan(&id)
			if err != nil {
				t.Fatal(err)
			}
			return id
		}
		amountID := insert(category, "1234.50", "invoice-a", "2026-10-12")
		bothID := insert(category, "1234.50", "ref-1234.50", "2026-10-13")
		textID := insert(category, "10", "ref-1234.50", "2026-10-14")
		insert(category, "12345", "invoice-b", "2026-10-15")
		insert(category, "", "incomplete", "2026-10-16")
		zeroID := insert(category, "0", "zero", "2026-10-17")
		insert(hidden, "1234.50", "hidden", "2026-10-18")
		query := ExpensePageQuery{VisibleCategoryIDs: []string{category}, AccessibleCategoryIDs: []string{category}, History: true, Pagination: Pagination{Page: 1, PageSize: 10}}
		for _, tc := range []struct {
			search string
			total  int
			amount string
		}{
			{"1234.50", 3, "2479.00"}, {"1234.5", 3, "2479.00"},
			{"$1,234.50", 2, "2469.00"}, {"1,234.50", 2, "2469.00"},
			{"$1234", 0, "0"}, {"$12.34", 0, "0"}, {"$0", 1, "0.00"},
			{"invoice-a", 1, "1234.50"}, {"1,23", 0, "0"}, {"", 6, "14824.00"},
		} {
			query.Search = tc.search
			page, err := repo.ListExpensesPage(ctx, query)
			if err != nil || page.Total != tc.total || page.Summary.Amount != tc.amount {
				t.Fatalf("search %q: total=%d sum=%s error=%v", tc.search, page.Total, page.Summary.Amount, err)
			}
			if tc.search == "$0" && page.Items[0].ID != zeroID {
				t.Fatal("zero search matched a missing amount")
			}
		}
		query.Search, query.Pagination = "1234.50", Pagination{Page: 2, PageSize: 1}
		page, err := repo.ListExpensesPage(ctx, query)
		if err != nil || page.Total != 3 || page.TotalPages != 3 || len(page.Items) != 1 || page.Items[0].ID != bothID || page.Summary.Amount != "2479.00" {
			t.Fatalf("pagination/deduplication: %+v %v", page, err)
		}
		query.AmountMax = "20"
		page, err = repo.ListExpensesPage(ctx, query)
		if err != nil || page.Total != 1 || page.Items[0].ID != textID || page.Summary.Amount != "10.00" {
			t.Fatalf("combined amount filter: %+v %v", page, err)
		}
		query.AmountMax, query.Search, query.CategoryID = "", "$1,234.50", hidden
		page, err = repo.ListExpensesPage(ctx, query)
		if err != nil || page.Total != 0 {
			t.Fatal("amount search bypassed category permissions", err)
		}
		query.CategoryID, query.Search = "", "$12.34"
		if _, err := pool.Exec(ctx, `UPDATE expenses SET amount=12.34 WHERE id=$1`, amountID); err != nil {
			t.Fatal(err)
		}
		page, err = repo.ListExpensesPage(ctx, query)
		if err != nil || page.Total != 1 || page.Items[0].ID != amountID {
			t.Fatal("amount index did not reflect edit", err)
		}
		migration, err := os.ReadFile("../../sql/205_expense_amount_search.sql")
		if err != nil {
			t.Fatal(err)
		}
		// Exercise upgrade and repeated migration execution without changing invoices.
		adminConfig := pool.Config().ConnConfig.Copy()
		delete(adminConfig.RuntimeParams, "role")
		admin, err := pgx.ConnectConfig(ctx, adminConfig)
		if err != nil {
			t.Fatal(err)
		}
		defer admin.Close(ctx)
		if _, err := admin.Exec(ctx, `DROP INDEX expenses_amount_category_idx`); err != nil {
			t.Fatal(err)
		}
		for range 2 {
			if _, err := admin.Exec(ctx, string(migration)); err != nil {
				t.Fatal(err)
			}
		}
		conn, err := pool.Acquire(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Release()
		if _, err := conn.Exec(ctx, `SET enable_seqscan=off`); err != nil {
			t.Fatal(err)
		}
		defer conn.Exec(ctx, `RESET enable_seqscan`)
		rows, err := conn.Query(ctx, `EXPLAIN SELECT id FROM expenses WHERE amount=$1::numeric AND category_id=ANY($2::uuid[])`, "1234.50", []string{category})
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var plan strings.Builder
		for rows.Next() {
			var line string
			if err := rows.Scan(&line); err != nil {
				t.Fatal(err)
			}
			plan.WriteString(line)
		}
		if rows.Err() != nil || !strings.Contains(plan.String(), "expenses_amount_category_idx") {
			t.Fatal("numeric lookup cannot use covering index", plan.String(), rows.Err())
		}
	})
}
