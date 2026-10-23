package repository

import (
	"bytes"
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

func TestExpenseDocumentsDatabase(t *testing.T) {
	dsn := os.Getenv("MSERP_EXPENSE_DOCUMENT_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set MSERP_EXPENSE_DOCUMENT_TEST_DATABASE_URL to a local disposable _test database")
	}
	u, err := url.Parse(dsn)
	if err != nil || (u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost") || !strings.HasSuffix(u.Path, "_test") {
		t.Fatal("local disposable _test database required")
	}
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(ctx)
	init, err := os.ReadFile("../../sql/init.sql")
	if err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile("../../sql/127_expense_documents.sql")
	if err != nil {
		t.Fatal(err)
	}
	expenseMigration, err := os.ReadFile("../../sql/147_maintenance_expenses.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"fresh", "migrated"} {
		t.Run(mode, func(t *testing.T) {
			schema := fmt.Sprintf("expense_documents_%d", time.Now().UnixNano())
			quoted := pgx.Identifier{schema}.Sanitize()
			exec := func(sql string) {
				t.Helper()
				if _, err := admin.Exec(ctx, sql); err != nil {
					t.Fatal(err)
				}
			}
			exec("CREATE SCHEMA " + quoted + ";SET search_path TO " + quoted + ",public;GRANT USAGE ON SCHEMA " + quoted + " TO mserp_app")
			defer func() { _, _ = admin.Exec(ctx, "SET search_path TO public;DROP SCHEMA "+quoted+" CASCADE") }()
			source := strings.ReplaceAll(string(init), "\r\n", "\n")
			if mode == "migrated" {
				source = strings.Replace(source, strings.ReplaceAll(string(expenseMigration), "\r\n", "\n"), "", 1)
				source = strings.Replace(source, strings.ReplaceAll(string(migration), "\r\n", "\n"), "", 1)
			}
			exec(source)
			if mode == "migrated" {
				exec(string(migration))
				exec(string(expenseMigration))
			}
			var category, expense string
			if err := admin.QueryRow(ctx, `SELECT id FROM expense_settings WHERE kind='category' AND active ORDER BY name LIMIT 1`).Scan(&category); err != nil {
				t.Fatal(err)
			}
			if err := admin.QueryRow(ctx, `INSERT INTO expenses(category_id,category,expense_date,amount) VALUES($1,'Synthetic','2026-10-20',10) RETURNING id`, category).Scan(&expense); err != nil {
				t.Fatal(err)
			}
			config, err := pgxpool.ParseConfig(dsn)
			if err != nil {
				t.Fatal(err)
			}
			config.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
			config.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
				_, err := conn.Exec(ctx, "SET ROLE mserp_app")
				return err
			}
			pool, err := pgxpool.NewWithConfig(ctx, config)
			if err != nil {
				t.Fatal(err)
			}
			defer pool.Close()
			repo := NewExpenseRepository(pool)
			data := []byte("Synthetic receipt, no AI extraction")
			first, err := repo.CreateExpenseDocument(ctx, expense, category, "receipt.txt", "text/plain", "Uploader", data)
			if err != nil {
				t.Fatal(err)
			}
			retry, err := repo.CreateExpenseDocument(ctx, expense, category, "receipt.txt", "text/plain", "Uploader", data)
			if err != nil || first.ID != retry.ID {
				t.Fatalf("retry duplicated receipt: %v", err)
			}
			docs, err := repo.ExpenseDocuments(ctx, expense)
			if err != nil || len(docs) != 1 {
				t.Fatalf("list: %v %v", docs, err)
			}
			got, err := repo.ExpenseDocumentCategory(ctx, first.ID)
			if err != nil || got != category {
				t.Fatalf("category: %s %v", got, err)
			}
			_, stored, err := repo.ExpenseDocument(ctx, first.ID, category)
			if err != nil || !bytes.Equal(stored, data) {
				t.Fatalf("stored content mismatch: %v", err)
			}
			const other = "00000000-0000-0000-0000-000000000001"
			if _, _, err := repo.ExpenseDocument(ctx, first.ID, other); !errors.Is(err, ErrNotFound) {
				t.Fatalf("category fence failed: %v", err)
			}
			if _, err := repo.CreateExpenseDocument(ctx, expense, other, "receipt.txt", "text/plain", "Uploader", data); !errors.Is(err, ErrNotFound) {
				t.Fatalf("upload category fence failed: %v", err)
			}
			if _, err := repo.CreateExpenseDocument(ctx, expense, category, "empty.txt", "text/plain", "Uploader", nil); err == nil {
				t.Fatal("empty receipt accepted")
			}
		})
	}
}
