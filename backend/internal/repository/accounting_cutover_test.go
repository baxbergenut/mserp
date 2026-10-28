package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"strings"
	"testing"
	"time"
)

func withoutAccountingCutoverMigration(t *testing.T, source string) string {
	t.Helper()
	body, e := os.ReadFile("../../sql/160_accounting_cutover.sql")
	if e != nil {
		t.Fatal(e)
	}
	return strings.Replace(source, strings.ReplaceAll(string(body), "\r\n", "\n"), "", 1)
}
func applyAccountingCutoverMigration(t *testing.T, ctx context.Context, conn *pgx.Conn) {
	t.Helper()
	var exists bool
	if e := conn.QueryRow(ctx, `SELECT to_regclass('accounting_cutover') IS NOT NULL`).Scan(&exists); e != nil {
		t.Fatal(e)
	}
	if exists {
		return
	}
	if e := conn.QueryRow(ctx, `SELECT to_regclass('expense_reimbursement_payments') IS NOT NULL`).Scan(&exists); e != nil {
		t.Fatal(e)
	}
	if !exists {
		body, e := os.ReadFile("../../sql/157_expense_reimbursement_payments.sql")
		if e != nil {
			t.Fatal(e)
		}
		if _, e = conn.Exec(ctx, string(body)); e != nil {
			t.Fatal(e)
		}
	}
	body, e := os.ReadFile("../../sql/160_accounting_cutover.sql")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = conn.Exec(ctx, string(body)); e != nil {
		t.Fatal(e)
	}
}

func TestAccountingCutoverDatabase(t *testing.T) {
	dsn := os.Getenv("MSERP_CUTOVER_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("local disposable database required")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.HasSuffix(cfg.ConnConfig.Database, "_test") || (cfg.ConnConfig.Host != "localhost" && cfg.ConnConfig.Host != "127.0.0.1") {
		t.Fatal("local _test database required")
	}
	for _, migration := range []bool{false, true} {
		t.Run(fmt.Sprint("migration=", migration), func(t *testing.T) {
			ctx := context.Background()
			admin, err := pgx.Connect(ctx, dsn)
			if err != nil {
				t.Fatal(err)
			}
			defer admin.Close(ctx)
			schema := fmt.Sprintf("cutover_%d", time.Now().UnixNano())
			quoted := pgx.Identifier{schema}.Sanitize()
			exec := func(sql string, args ...any) {
				t.Helper()
				if _, e := admin.Exec(ctx, sql, args...); e != nil {
					t.Fatal(e)
				}
			}
			exec(`CREATE SCHEMA ` + quoted + `;SET search_path TO ` + quoted + `,public`)
			defer admin.Exec(ctx, `SET search_path TO public;DROP SCHEMA `+quoted+` CASCADE`)
			source, e := os.ReadFile("../../sql/init.sql")
			if e != nil {
				t.Fatal(e)
			}
			m, e := os.ReadFile("../../sql/160_accounting_cutover.sql")
			if e != nil {
				t.Fatal(e)
			}
			if migration {
				base := strings.ReplaceAll(string(source), "\r\n", "\n")
				old := strings.ReplaceAll(string(m), "\r\n", "\n")
				if !strings.Contains(base, old) {
					t.Fatal("migration missing from init")
				}
				exec(strings.Replace(base, old, "", 1))
				exec(string(m))
			} else {
				exec(string(source))
			}
			exec(`GRANT USAGE ON SCHEMA ` + quoted + ` TO mserp_app;GRANT ALL ON ALL TABLES IN SCHEMA ` + quoted + ` TO mserp_app`)
			cfg2 := cfg.Copy()
			cfg2.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
			cfg2.ConnConfig.RuntimeParams["role"] = "mserp_app"
			pool, e := pgxpool.NewWithConfig(ctx, cfg2)
			if e != nil {
				t.Fatal(e)
			}
			defer pool.Close()
			scalar := func(sql string, args ...any) string {
				t.Helper()
				var s string
				if e := admin.QueryRow(ctx, sql, args...).Scan(&s); e != nil {
					t.Fatal(e)
				}
				return s
			}
			actor := scalar(`INSERT INTO app_users(username,email,password_hash,role_id) SELECT 'Cutover Test','cutover@example.test',crypt('Synthetic-cutover-test',gen_salt('bf')),id FROM app_roles WHERE system_role RETURNING id::text`)
			driver := scalar(`INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate,hire_date) VALUES('Opening Driver','opening driver','cpm',0.75,'2026-09-01') RETURNING id::text`)
			oldExpense := scalar(`INSERT INTO expenses(category,category_id,driver_id,charge_driver_id,expense_date,amount,covered_by) VALUES('Other',(SELECT id FROM expense_settings WHERE kind='category' AND name='Other'),$1,$1,'2026-10-25',100,'Driver') RETURNING id::text`, driver)
			currentExpense := scalar(`INSERT INTO expenses(category,category_id,driver_id,charge_driver_id,expense_date,amount,covered_by) VALUES('Other',(SELECT id FROM expense_settings WHERE kind='category' AND name='Other'),$1,$1,'2026-10-26',200,'Driver') RETURNING id::text`, driver)
			exec(`INSERT INTO gross_board_entries(driver_id,service_date,load_number,original_rate,driver_rate,miles) VALUES($1,'2026-10-20','OLD',1000,900,100),($1,'2026-10-26','NEW',500,450,100)`, driver)
			exec(`INSERT INTO driver_escrows(driver_id,driver_name,start_date,amount,opening_paid) VALUES($1,'Opening Driver','2026-10-26',1000,100)`, driver)
			repo := NewDriverPayRepository(pool)
			expenses := NewExpenseRepository(pool)
			draft, e := repo.AccountingCutover(ctx)
			if e != nil {
				t.Fatal(e)
			}
			if draft.Active {
				t.Fatal("migration activated cutoff")
			}
			draft.BackupReference = "test backup"
			draft.SheetsReference = "dated test Sheets copy"
			draft.Openings = []AccountingOpening{{DriverID: driver, Amount: "350.00", Reference: "Driver Board opening"}}
			draft, e = repo.SaveAccountingCutover(ctx, draft.AccountingCutoverDraft)
			if e != nil {
				t.Fatal(e)
			}
			preview, e := repo.PreviewAccountingCutover(ctx, actor)
			if e != nil {
				t.Fatal(e)
			}
			exec(`UPDATE expenses SET description='changed during preview' WHERE id=$1`, currentExpense)
			if _, e = repo.ActivateAccountingCutover(ctx, preview.ID, actor); e == nil {
				t.Fatal("accepted stale preview")
			}
			exec(`INSERT INTO expense_payments(expense_id,week_start,amount) VALUES($1,'2026-10-26',25)`, oldExpense)
			preview, e = repo.PreviewAccountingCutover(ctx, actor)
			if e != nil {
				t.Fatal(e)
			}
			blocked := false
			for _, i := range preview.Items {
				blocked = blocked || i.Blocking
			}
			if !blocked {
				t.Fatal("legacy payment did not block")
			}
			if _, e = repo.ActivateAccountingCutover(ctx, preview.ID, actor); e == nil {
				t.Fatal("activated blocked preview")
			}
			exec(`UPDATE expense_payments SET amount=0 WHERE expense_id=$1`, oldExpense)
			exec(`INSERT INTO gross_board_transfers(driver_id,source_date,source_slot,dispatch_date,dispatch_slot,settlement_date,dispatcher_id,dispatcher_name) VALUES($1,'2026-10-25',0,'2026-10-26',0,'2026-10-25','','')`, driver)
			preview, e = repo.PreviewAccountingCutover(ctx, actor)
			if e != nil {
				t.Fatal(e)
			}
			crossing := false
			for _, item := range preview.Items {
				if item.Kind == "transfer" && item.Blocking {
					crossing = true
				}
			}
			if !crossing {
				t.Fatal("dispatch-only crossing was not reviewed")
			}
			exec(`DELETE FROM gross_board_transfers WHERE driver_id=$1`, driver)
			preview, e = repo.PreviewAccountingCutover(ctx, actor)
			if e != nil {
				t.Fatal(e)
			}
			activated, e := repo.ActivateAccountingCutover(ctx, preview.ID, actor)
			if e != nil {
				t.Fatal(e)
			}
			if !activated.Active {
				t.Fatal("not active")
			}
			if _, e = pool.Exec(ctx, `INSERT INTO gross_board_transfers(driver_id,source_date,source_slot,dispatch_date,dispatch_slot,settlement_date,dispatcher_id,dispatcher_name) VALUES($1,'2026-10-25',0,'2026-10-26',0,'2026-10-26','','')`, driver); e == nil {
				t.Fatal("post-activation historical transfer allowed")
			}
			if scalar(`SELECT count(*)::text FROM expenses`) != "2" {
				t.Fatal("history was deleted")
			}
			if _, e = pool.Exec(ctx, `UPDATE gross_board_entries SET driver_rate=0 WHERE service_date='2026-10-20'`); e == nil {
				t.Fatal("historical board write allowed")
			}
			if _, e = pool.Exec(ctx, `UPDATE expenses SET description='rewrite' WHERE id=$1`, oldExpense); e == nil {
				t.Fatal("historical expense write allowed")
			}
			week, _ := time.Parse(time.DateOnly, "2026-10-26")
			report, e := repo.Get(ctx, week)
			if e != nil {
				t.Fatal(e)
			}
			if len(report.Drivers) != 1 {
				t.Fatalf("unexpected report %+v", report)
			}
			for _, d := range report.Drivers[0].Edits.ExpenseDeductions {
				if d.ExpenseID == oldExpense || d.Source == "escrow" {
					t.Fatal("legacy obligation leaked", d)
				}
			}
			archived, e := repo.Get(ctx, week.AddDate(0, 0, -7))
			if e != nil || !archived.ManagedInSheets {
				t.Fatal("archive unavailable", e)
			}
			board, e := NewGrossBoardRepository(pool).Get(ctx, week)
			if e != nil {
				t.Fatal(e)
			}
			if len(board.Balances) != 1 || board.Balances[0].OpeningBalance != "350.00" {
				t.Fatal("incorrect rate opening", board.Balances)
			}
			invoice, e := expenses.GetExpense(ctx, currentExpense)
			if e != nil {
				t.Fatal(e)
			}
			receipt := ExternalExpenseSettlement{ID: scalar(`SELECT gen_random_uuid()::text`), Amount: "50.00", SettledOn: "2026-10-26", Reference: "External receipt", Version: invoice.BalanceVersion}
			if e = expenses.RecordExternalSettlement(ctx, currentExpense, actor, receipt); e != nil {
				t.Fatal(e)
			}
			if e = expenses.RecordExternalSettlement(ctx, currentExpense, actor, receipt); e != nil {
				t.Fatal("retry not idempotent", e)
			}
			invoice, e = expenses.GetExpense(ctx, currentExpense)
			if e != nil || invoice.RemainingAmount == nil || *invoice.RemainingAmount != "150.00" {
				t.Fatal("incorrect external balance", invoice, e)
			}
			if _, e = pool.Exec(ctx, `INSERT INTO expense_payments(expense_id,week_start,amount) VALUES($1,'2026-10-26',151)`, currentExpense); e == nil {
				t.Fatal("external recovery could be charged twice")
			}
			receipt.ReversesID = receipt.ID
			receipt.ID = scalar(`SELECT gen_random_uuid()::text`)
			receipt.Version = invoice.BalanceVersion
			receipt.Reference = "Reversal reason"
			if e = expenses.RecordExternalSettlement(ctx, currentExpense, actor, receipt); e != nil {
				t.Fatal(e)
			}
			invoice, e = expenses.GetExpense(ctx, currentExpense)
			if e != nil || *invoice.RemainingAmount != "200.00" {
				t.Fatal("reversal balance", e)
			}
			full, e := repo.Get(ctx, week)
			if e != nil {
				t.Fatal(e)
			}
			// Unrelated committed transactions must not invalidate a financial
			// revision (the old cache fallback used the global MVCC snapshot).
			exec(`SELECT pg_current_xact_id()`)
			stable, e := repo.Get(ctx, week)
			if e != nil || stable.Revision != full.Revision {
				t.Fatal("unrelated transaction changed financial revision", e)
			}
			query := PayPageQuery{Pagination: Pagination{Page: 1, PageSize: 25}, Summary: true}
			want := PaginatePay(full, query)
			got, e := repo.PayrollPage(ctx, "driver", week, query)
			if e != nil {
				t.Fatal(e)
			}
			a, _ := json.Marshal(want)
			b, _ := json.Marshal(got)
			if string(a) != string(b) {
				t.Fatalf("summary differs\n%s\n%s", a, b)
			}
			query.DetailIDs = map[string]bool{driver: true}
			want = PaginatePay(full, query)
			got, e = repo.PayrollPage(ctx, "driver", week, query)
			if e != nil {
				t.Fatal(e)
			}
			a, _ = json.Marshal(want)
			b, _ = json.Marshal(got)
			if string(a) != string(b) {
				t.Fatalf("detail differs\n%s\n%s", a, b)
			}
			correction := RateOpeningCorrection{ID: scalar(`SELECT gen_random_uuid()::text`), DriverID: driver, Amount: "-25.00", Reference: "Reviewed Sheets correction", ExpectedAmount: payTestString("350.00")}
			if _, e = repo.CorrectRateOpening(ctx, correction, actor); e != nil {
				t.Fatal(e)
			}
			if _, e = repo.CorrectRateOpening(ctx, correction, actor); e != nil {
				t.Fatal("opening retry", e)
			}
			board, e = NewGrossBoardRepository(pool).Get(ctx, week)
			if e != nil || board.Balances[0].OpeningBalance != "325.00" {
				t.Fatal("opening correction", board, e)
			}
			correction.ID = scalar(`SELECT gen_random_uuid()::text`)
			if _, e = repo.CorrectRateOpening(ctx, correction, actor); e == nil {
				t.Fatal("accepted stale opening")
			}
			corrected, e := repo.Get(ctx, week)
			if e != nil {
				t.Fatal(e)
			}
			if summarizePayStatement(corrected.Drivers[0]).Summary.Payable != summarizePayStatement(full.Drivers[0]).Summary.Payable {
				t.Fatal("rate correction changed payroll")
			}
			if _, e = repo.Settle(ctx, week, driver, got.Revision, actor, "", false, true); e != ErrDriverPayConflict {
				t.Fatal("stale compact token accepted", e)
			}
			// New obligations continue carrying after the fixed boundary.
			exec(`INSERT INTO expense_payments(expense_id,week_start,amount) VALUES($1,'2026-10-26',75)`, currentExpense)
			next, e := repo.Get(ctx, week.AddDate(0, 0, 7))
			if e != nil {
				t.Fatal(e)
			}
			found := false
			for _, d := range next.Drivers {
				for _, x := range d.Edits.ExpenseDeductions {
					if x.ExpenseID == currentExpense {
						found = true
						if x.Amount != "125.00" {
							t.Fatal("new partial payment did not carry", x)
						}
					}
				}
			}
			if !found {
				t.Fatal("new debt disappeared at next week")
			}
			invoice, e = expenses.GetExpense(ctx, currentExpense)
			if e != nil {
				t.Fatal(e)
			}
			overspend := ExternalExpenseSettlement{ID: scalar(`SELECT gen_random_uuid()::text`), Amount: "126.00", SettledOn: "2026-10-26", Reference: "Over reserved", Version: invoice.BalanceVersion}
			if e = expenses.RecordExternalSettlement(ctx, currentExpense, actor, overspend); e == nil {
				t.Fatal("external settlement ignored saved reservation")
			}
			if _, e = pool.Exec(ctx, `UPDATE driver_escrows SET amount=5000 WHERE driver_id=$1`, driver); e == nil {
				t.Fatal("managed escrow mutated")
			}
			if _, e = pool.Exec(ctx, `UPDATE accounting_cutover SET week_start='2026-11-02'`); e == nil {
				t.Fatal("active cutoff moved")
			}
			// A late historical source remains visible as a cutover review notice.
			exec(`INSERT INTO tolls(posting_date,invoice_date,customer_id,source,read_type,transponder_or_plate,equipment_unit,agency,exit_plaza,exit_date,exit_time,toll_class,amount,row_fingerprint) VALUES('2026-10-27','2026-10-27','test','test','test','test','test','test','test','2026-10-25','12:00','test',15,repeat('z',64))`)
			late, e := repo.Get(ctx, week)
			if e != nil || len(late.Notices) == 0 {
				t.Fatal("late historical toll lacks review notice", e)
			}
			// Finalized source statements reject subsequent external edits.
			finalPage, e := repo.PayrollPage(ctx, "driver", week, PayPageQuery{Summary: true})
			if e != nil {
				t.Fatal(e)
			}
			if _, e = repo.Settle(ctx, week, driver, finalPage.Revision, actor, "", false, true); e != nil {
				t.Fatal("compact token could not finalize full statement", e)
			}
			overspend.Amount = "1.00"
			if e = expenses.RecordExternalSettlement(ctx, currentExpense, actor, overspend); e == nil {
				t.Fatal("external settlement bypassed finalized statement")
			}
		})
	}
}

// Old expense-specific upgrade fixtures install only the actual new query
// dependencies; the complete migration and its guards are tested above.
func applyAccountingExpenseTestSchema(t *testing.T, ctx context.Context, conn *pgx.Conn) {
	t.Helper()
	var exists bool
	if e := conn.QueryRow(ctx, "SELECT to_regclass('accounting_cutover') IS NOT NULL").Scan(&exists); e != nil {
		t.Fatal(e)
	}
	if exists {
		return
	}
	b, e := os.ReadFile("../../sql/160_accounting_cutover.sql")
	if e != nil {
		t.Fatal(e)
	}
	s := strings.ReplaceAll(string(b), "\r\n", "\n")
	prefix, _, ok := strings.Cut(s, "CREATE FUNCTION protect_accounting_cutover()")
	if !ok {
		t.Fatal("cutover dependency boundary")
	}
	_, external, ok := strings.Cut(s, "CREATE TABLE expense_external_settlements (")
	if !ok {
		t.Fatal("external boundary")
	}
	external, _, ok = strings.Cut(external, "CREATE TRIGGER expense_external_immutable")
	if !ok {
		t.Fatal("external function boundary")
	}
	if _, e = conn.Exec(ctx, prefix+"CREATE TABLE expense_external_settlements ("+external+"COMMIT;"); e != nil {
		t.Fatal(e)
	}
	if _, e = conn.Exec(ctx, "GRANT SELECT ON accounting_cutover,accounting_legacy_sources,expense_external_settlements TO mserp_app"); e != nil {
		t.Fatal(e)
	}
}
