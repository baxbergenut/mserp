package expensesheet

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
)

func TestScaleOutboxDatabase(t *testing.T) {
	dsn := os.Getenv("MSERP_EXPENSE_SHEET_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set disposable local MSERP_EXPENSE_SHEET_TEST_DATABASE_URL")
	}
	u, err := url.Parse(dsn)
	if err != nil || !strings.HasSuffix(u.Path, "_test") || (u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost") {
		t.Fatal("local _test database required")
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	source, err := os.ReadFile("../../sql/init.sql")
	if err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile("../../sql/138_scale_sheet_export.sql")
	if err != nil {
		t.Fatal(err)
	}
	retirement, err := os.ReadFile("../../sql/151_retire_expense_manager_verification.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, upgrade := range []bool{false, true} {
		t.Run(fmt.Sprint(upgrade), func(t *testing.T) {
			schema := fmt.Sprintf("scale_sheet_%d", time.Now().UnixNano())
			exec := func(sql string) {
				t.Helper()
				if _, err := conn.Exec(ctx, sql); err != nil {
					t.Fatal(err)
				}
			}
			exec("CREATE SCHEMA " + schema + " AUTHORIZATION mserp_app; SET search_path TO " + schema + ",public")
			defer conn.Exec(ctx, "RESET ROLE; SET search_path TO public; DROP SCHEMA "+schema+" CASCADE")
			s := strings.ReplaceAll(string(source), "\r\n", "\n")
			if upgrade {
				s = strings.Replace(s, strings.ReplaceAll(string(retirement), "\r\n", "\n"), "", 1)
				s = strings.Replace(s, strings.ReplaceAll(string(migration), "\r\n", "\n"), "", 1)
			}
			exec(s)
			if upgrade {
				exec(string(migration))
				exec(string(retirement))
			}
			exec("GRANT SELECT,INSERT,UPDATE,DELETE ON ALL TABLES IN SCHEMA " + schema + " TO mserp_app; GRANT USAGE,SELECT ON ALL SEQUENCES IN SCHEMA " + schema + " TO mserp_app")
			exec("SET ROLE mserp_app")
			insert := func(number, date, amount string) {
				t.Helper()
				exec(fmt.Sprintf(`INSERT INTO weighmytruck_transactions(site_number,ticket_number,transaction_date,amount,tractor_number,raw_payload) VALUES('7','%s','%s',%s,'001','{}') ON CONFLICT(site_number,ticket_number) DO UPDATE SET amount=excluded.amount`, number, date, amount))
			}
			count := func(want int) {
				t.Helper()
				var got int
				if err := conn.QueryRow(ctx, "SELECT count(*) FROM expense_sheet_exports").Scan(&got); err != nil || got != want {
					t.Fatalf("count=%d want=%d err=%v", got, want, err)
				}
			}
			insert("disabled", "2026-10-22", "15.25")
			count(0)
			exec("UPDATE expense_sheet_export_config SET enabled=true")
			insert("scale-disabled", "2026-10-22", "15.25")
			count(0)
			exec("UPDATE expense_sheet_export_config SET scale_enabled=true,scale_from_date='2026-10-22'")
			count(0)
			insert("old", "2026-10-21", "15.25")
			count(0)
			exec("BEGIN")
			insert("rollback", "2026-10-22", "15.25")
			exec("ROLLBACK")
			count(0)
			// Final identity is assigned after insertion by settlement reconciliation.
			exec(`INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate) VALUES('Scale Driver','scale driver','cpm',0.75)`)
			exec(`INSERT INTO trucks(unit_number,owner_id) SELECT '001',id FROM investors WHERE is_company`)
			exec(`UPDATE truck_ownership_history SET assigned_at='2026-10-01',start_known=true`)
			exec("BEGIN")
			insert("ticket", "2026-10-22", "15.25")
			exec(`UPDATE weighmytruck_transactions SET driver_id=(SELECT id FROM drivers WHERE normalized_name='scale driver'),truck_id=(SELECT id FROM trucks WHERE unit_number='001') WHERE ticket_number='ticket'`)
			exec("COMMIT")
			count(1)
			var payload []byte
			var id string
			var sheet int
			if err := conn.QueryRow(ctx, `SELECT expense_id::text,sheet_id,payload FROM expense_sheet_exports`).Scan(&id, &sheet, &payload); err != nil {
				t.Fatal(err)
			}
			if sheet != 377431143 {
				t.Fatal("wrong scale tab", sheet)
			}
			e := Entry{ID: id, Payload: payload}
			cells, err := entryCells(e, Tab{IDColumn: 19})
			if err != nil {
				t.Fatal(err)
			}
			for i, want := range map[int]string{1: "Other", 4: "001", 5: "Scale Driver", 7: "WeighMyTruck", 8: "Scale", 9: "7/ticket", 11: "Company", 19: id} {
				if got := cells[i].(map[string]any)["userEnteredValue"].(map[string]any)["stringValue"]; got != want {
					t.Fatalf("column %d=%v want %s", i, got, want)
				}
			}
			// Repeated imports and corrections must not change the delivered snapshot.
			insert("ticket", "2026-10-22", "20.50")
			count(1)
			var after []byte
			conn.QueryRow(ctx, "SELECT payload FROM expense_sheet_exports").Scan(&after)
			if string(after) != string(payload) {
				t.Fatal("source update rewrote snapshot")
			}
			insert("credit", "2026-10-22", "-5.25")
			count(2)
			var credit json.RawMessage
			if err := conn.QueryRow(ctx, `SELECT payload FROM expense_sheet_exports WHERE payload->'scale_ticket'->>'ticket_number'='credit'`).Scan(&credit); err != nil {
				t.Fatal(err)
			}
			var snapshot struct {
				Expense struct {
					Amount    json.Number
					CoveredBy string `json:"covered_by"`
				}
			}
			if err := json.Unmarshal(credit, &snapshot); err != nil || snapshot.Expense.Amount.String() != "-5.25" || snapshot.Expense.CoveredBy != "Needs review" {
				t.Fatalf("credit/review snapshot %s %v", credit, err)
			}
			// Snapshot dated ownership, even when the truck's current owner differs.
			exec(`INSERT INTO investors(full_name) VALUES('Scale Investor'); UPDATE truck_ownership_history SET owner_id=(SELECT id FROM investors WHERE full_name='Scale Investor')`)
			for _, payer := range []string{"Truck Owner", "Driver", "Needs review"} {
				if payer == "Driver" {
					exec(`UPDATE investors SET driver_id=(SELECT id FROM drivers WHERE normalized_name='scale driver') WHERE full_name='Scale Investor'`)
				}
				exec("BEGIN")
				insert(payer, "2026-10-22", "15.25")
				exec(`UPDATE weighmytruck_transactions SET driver_id=(SELECT id FROM drivers WHERE normalized_name='scale driver'),truck_id=(SELECT id FROM trucks WHERE unit_number='001') WHERE ticket_number='` + payer + `'`)
				if payer == "Needs review" {
					exec(`UPDATE weighmytruck_transactions SET match_warning='Source identity conflicts with saved ticket; review link.' WHERE ticket_number='Needs review'`)
				}
				exec("COMMIT")
				var got string
				if err := conn.QueryRow(ctx, `SELECT payload->'expense'->>'covered_by' FROM expense_sheet_exports WHERE payload->'scale_ticket'->>'ticket_number'=$1`, payer).Scan(&got); err != nil || got != payer {
					t.Fatalf("payer=%s want=%s err=%v", got, payer, err)
				}
			}
			count(5)
			var expenses int
			conn.QueryRow(ctx, "SELECT count(*) FROM expenses").Scan(&expenses)
			if expenses != 0 {
				t.Fatal("scale export created duplicate expenses")
			}
			exec("UPDATE expense_sheet_export_config SET enabled=false")
			insert("paused", "2026-10-22", "15.25")
			count(5)
		})
	}
}
