package expensesheet

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestOutboxDatabase(t *testing.T) {
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
	schemaSource, err := os.ReadFile("../../sql/init.sql")
	if err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile("../../sql/105_expense_sheet_export.sql")
	if err != nil {
		t.Fatal(err)
	}
	notifyMigration, err := os.ReadFile("../../sql/107_expense_sheet_notify.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, upgrade := range []bool{false, true} {
		t.Run(fmt.Sprint(upgrade), func(t *testing.T) {
			schema := fmt.Sprintf("expense_sheet_%d", time.Now().UnixNano())
			exec := func(sql string) {
				t.Helper()
				if _, err := conn.Exec(ctx, sql); err != nil {
					t.Fatal(err)
				}
			}
			exec("CREATE SCHEMA " + schema + " AUTHORIZATION mserp_app; SET search_path TO " + schema + ",public")
			defer func() { conn.Exec(ctx, "RESET ROLE; SET search_path TO public; DROP SCHEMA "+schema+" CASCADE") }()
			source := strings.ReplaceAll(string(schemaSource), "\r\n", "\n")
			if upgrade {
				source = strings.Replace(source, strings.ReplaceAll(string(notifyMigration), "\r\n", "\n"), "", 1)
				source = strings.Replace(source, strings.ReplaceAll(string(migration), "\r\n", "\n"), "", 1)
			}
			exec(source)
			if upgrade {
				exec(string(migration))
				exec(string(notifyMigration))
			}
			exec("SET ROLE mserp_app")
			count := func(want int) {
				t.Helper()
				var got int
				if err := conn.QueryRow(ctx, "SELECT count(*) FROM expense_sheet_exports").Scan(&got); err != nil || got != want {
					t.Fatalf("count=%d want=%d err=%v", got, want, err)
				}
			}
			insert := `INSERT INTO expenses(id,category_id,category,expense_date,amount,expense_type,covered_by) SELECT '11111111-1111-4111-8111-111111111111',id,name,'2026-10-16',12.34,'Test','Company' FROM expense_settings WHERE kind='category' AND name='HR'`
			exec(insert)
			count(0) // Deployment is inert, and enabling never backfills.
			exec("UPDATE expense_sheet_export_config SET enabled=true")
			count(0)
			exec(strings.ReplaceAll(insert, "11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222"))
			count(1)
			exec("BEGIN; " + strings.ReplaceAll(insert, "11111111-1111-4111-8111-111111111111", "33333333-3333-4333-8333-333333333333") + "; ROLLBACK")
			count(1)
			exec(`INSERT INTO expenses(category_id,category,expense_date,amount,expense_type,source_spreadsheet_id,source_sheet,source_row) SELECT id,name,'2026-10-16',1,'Import','test','HR expenses',2 FROM expense_settings WHERE kind='category' AND name='HR'`)
			count(1)
			// Native work-order capture runs at commit, after the linked service is updated.
			exec(`BEGIN; INSERT INTO expenses(id,category_id,category,expense_date,amount,expense_type,covered_by) SELECT '44444444-4444-4444-8444-444444444444',s.id,s.name,'2026-10-16',55,'Repair','Company' FROM expense_settings s JOIN maintenance_configuration c ON c.category_id=s.id; UPDATE maintenance_records SET vendor_name='Final shop',notes='Final service details' WHERE expense_id='44444444-4444-4444-8444-444444444444'; COMMIT`)
			count(2)
			var vendor string
			if err := conn.QueryRow(ctx, `SELECT payload->'maintenance'->>'vendor_name' FROM expense_sheet_exports WHERE expense_id='44444444-4444-4444-8444-444444444444'`).Scan(&vendor); err != nil || vendor != "Final shop" {
				t.Fatalf("vendor=%s err=%v", vendor, err)
			}
			exec("DELETE FROM expenses WHERE id='22222222-2222-4222-8222-222222222222'")
			count(2)
			if _, err := conn.Exec(ctx, "UPDATE expense_sheet_exports SET payload='{}'"); err == nil {
				t.Fatal("immutable snapshot changed")
			}
			exec("UPDATE expense_sheet_exports SET attempts=attempts+1,last_attempt_at=now(),delivered_at=now()")
			if _, err := conn.Exec(ctx, "UPDATE expense_sheet_exports SET delivered_at=NULL"); err == nil {
				t.Fatal("delivery receipt cleared")
			}
			// The fixed cutoff settles only dates strictly before September 28.
			exec(`INSERT INTO expenses(id,category_id,category,expense_date,amount,expense_type,covered_by) SELECT '66666666-6666-4666-8666-666666666666',id,name,'2026-09-27',15,'Permit','Company' FROM expense_settings WHERE kind='category' AND name='Safety'`)
			exec(`INSERT INTO expenses(id,category_id,category,expense_date,amount,expense_type,covered_by) SELECT '77777777-7777-4777-8777-777777777777',id,name,'2026-09-28',20,'Permit','Company' FROM expense_settings WHERE kind='category' AND name='Safety'`)
			count(4)
			var oldPaid, boundaryPaid, verified bool
			var target int
			if err := conn.QueryRow(ctx, `SELECT (payload->'expense'->>'driver_settled')::boolean,(payload->'expense'->>'manager_verified')::boolean,sheet_id FROM expense_sheet_exports WHERE expense_id='66666666-6666-4666-8666-666666666666'`).Scan(&oldPaid, &verified, &target); err != nil || !oldPaid || verified || target != 752928518 {
				t.Fatalf("oldPaid=%t verified=%t target=%d err=%v", oldPaid, verified, target, err)
			}
			if err := conn.QueryRow(ctx, `SELECT driver_settled FROM expenses WHERE id='77777777-7777-4777-8777-777777777777'`).Scan(&boundaryPaid); err != nil || boundaryPaid {
				t.Fatalf("cutoff boundary was automatically paid: %t %v", boundaryPaid, err)
			}
			exec(`INSERT INTO expenses(id,category_id,category,expense_date,amount,expense_type,covered_by) SELECT '88888888-8888-4888-8888-888888888888',id,name,'2026-10-16',25,'Ticket','Company' FROM expense_settings WHERE kind='category' AND name='Penalties'`)
			if err := conn.QueryRow(ctx, `SELECT sheet_id FROM expense_sheet_exports WHERE expense_id='88888888-8888-4888-8888-888888888888'`).Scan(&target); err != nil || target != 752928518 {
				t.Fatalf("penalties target=%d err=%v", target, err)
			}
			exec("UPDATE expense_sheet_export_config SET enabled=false")
			exec(strings.ReplaceAll(insert, "11111111-1111-4111-8111-111111111111", "55555555-5555-4555-8555-555555555555"))
			count(5)
		})
	}
}
