package repository

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestPayrollExpenseDraftCutoverDatabase(t *testing.T) {
	dsn := os.Getenv("MSERP_DRIVER_PAY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("local disposable payroll test database required")
	}
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil || !strings.HasSuffix(cfg.Database, "_test") || (cfg.Host != "127.0.0.1" && cfg.Host != "localhost") {
		t.Fatal("local _test database required")
	}
	ctx := context.Background()
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	name := fmt.Sprintf("expense_cutover_%d", time.Now().UnixNano())
	schema := pgx.Identifier{name}.Sanitize()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := conn.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`CREATE EXTENSION IF NOT EXISTS pgcrypto WITH SCHEMA public; CREATE SCHEMA ` + schema + `; SET search_path TO ` + schema + `,public`)
	defer conn.Exec(ctx, `ROLLBACK; SET search_path TO public; DROP SCHEMA `+schema+` CASCADE`)
	init, err := os.ReadFile("../../sql/init.sql")
	if err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile("../../sql/198_payroll_expense_drafts.sql")
	if err != nil {
		t.Fatal(err)
	}
	body, upgrade := strings.ReplaceAll(string(init), "\r\n", "\n"), strings.ReplaceAll(string(migration), "\r\n", "\n")
	if strings.Count(body, upgrade) != 1 {
		t.Fatal("fresh schema must contain exact migration")
	}
	exec(strings.Replace(body, upgrade, "", 1))
	exec(`INSERT INTO drivers(id,full_name,normalized_name,pay_type,pay_rate) VALUES
 ('19800000-0000-0000-0000-000000000001','Cutover Driver','cutover driver','cpm',0.75),
 ('19800000-0000-0000-0000-000000000002','Ambiguous Driver','ambiguous driver','cpm',0.75);
 INSERT INTO investors(id,full_name) VALUES('19800000-0000-0000-0000-000000000003','Cutover Owner');
 INSERT INTO trucks(id,unit_number,owner_id) VALUES('19800000-0000-0000-0000-000000000004','CUTOVER','19800000-0000-0000-0000-000000000003');
 INSERT INTO investor_pay_weeks(truck_id,owner_id,week_start,edits) VALUES('19800000-0000-0000-0000-000000000004','19800000-0000-0000-0000-000000000003','2026-10-05','{}');
 INSERT INTO expenses(id,category_id,category,expense_type,expense_date,amount,driver_id,charge_driver_id,covered_by,truck_id,owner_id)
 SELECT ('19800000-0000-0000-0000-'||lpad(n::text,12,'0'))::uuid,s.id,s.name,'Cutover invoice','2026-10-01',100,
 CASE WHEN n<>15 THEN '19800000-0000-0000-0000-000000000001'::uuid END,
 CASE WHEN n<>15 THEN '19800000-0000-0000-0000-000000000001'::uuid END,
 CASE WHEN n=15 THEN 'Truck Owner' ELSE 'Driver' END,
 CASE WHEN n=15 THEN '19800000-0000-0000-0000-000000000004'::uuid END,
 CASE WHEN n=15 THEN '19800000-0000-0000-0000-000000000003'::uuid END
 FROM generate_series(11,15) n CROSS JOIN expense_settings s WHERE s.kind='category' AND s.name='Other';
 INSERT INTO expense_payments(expense_id,week_start,amount,investor_truck_id)
 SELECT id,'2026-10-05',CASE right(id::text,2) WHEN '11' THEN 60 WHEN '12' THEN 40 WHEN '13' THEN 20 WHEN '14' THEN 0 ELSE 35 END,
 CASE WHEN right(id::text,2)='15' THEN '19800000-0000-0000-0000-000000000004'::uuid END FROM expenses;
 INSERT INTO payroll_projection_generations(week_start,requested_generation,input_snapshot,calendar_week,calculator_version,schema_version,dependencies,driver_header,investor_header)
 VALUES('2026-10-05',1,'fixture','2026-11-02','payroll-197',10,'{}','{}','{}');`)
	// Use separate frozen identity/week so canonical protection remains active.
	exec(`UPDATE expense_payments SET week_start='2026-09-28' WHERE expense_id='19800000-0000-0000-0000-000000000012';
 INSERT INTO payroll_projection_generations(week_start,requested_generation,input_snapshot,calendar_week,calculator_version,schema_version,dependencies,driver_header,investor_header)
 VALUES('2026-09-28',1,'fixture','2026-11-02','payroll-197',10,'{}','{}','{}');
 INSERT INTO payroll_statement_current(generation_id,kind,statement_id,week_start,detail,cost_evidence,resource_dependencies,summary,fee_cents,payable_cents,finalized,edit_version,settlement_version)
 SELECT g.id,v.kind,v.statement_id::uuid,g.week_start,jsonb_build_object('edits',jsonb_build_object('expenseDeductions',v.items::jsonb)),
 '{}','{}','{}',0,0,v.finalized,1,1 FROM (VALUES
 ('driver','19800000-0000-0000-0000-000000000001','2026-10-05',false,'[{"expenseId":"19800000-0000-0000-0000-000000000011"},{"expenseId":"19800000-0000-0000-0000-000000000014"}]'),
 ('driver','19800000-0000-0000-0000-000000000001','2026-09-28',true,'[{"expenseId":"19800000-0000-0000-0000-000000000012"}]'),
 ('investor','19800000-0000-0000-0000-000000000004','2026-10-05',false,'[{"expenseId":"19800000-0000-0000-0000-000000000015"}]'),
 ('driver','19800000-0000-0000-0000-000000000002','2026-10-05',false,'[{"expenseId":"19800000-0000-0000-0000-000000000011"}]')
 ) v(kind,statement_id,week,finalized,items) JOIN payroll_projection_generations g ON g.week_start=v.week::date;
 INSERT INTO payroll_settlements(driver_id,week_start,report)
 SELECT statement_id,week_start,detail FROM payroll_statement_current WHERE finalized;`)
	if _, err = conn.Exec(ctx, upgrade); err == nil {
		t.Fatal("ambiguous draft ownership was migrated")
	}
	exec(`ROLLBACK`)
	var unchanged bool
	if err = conn.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='driver_pay_weeks' AND column_name='expense_choices') AND (SELECT count(*) FROM expense_payments)=5`).Scan(&unchanged); err != nil || !unchanged {
		t.Fatal("failed cutover was not atomic", err)
	}
	exec(`DELETE FROM payroll_statement_current WHERE statement_id='19800000-0000-0000-0000-000000000002'`)
	var retainedBefore string
	const retained = `SELECT jsonb_agg(to_jsonb(p)-'statement_kind'-'statement_id' ORDER BY expense_id)::text FROM expense_payments p WHERE right(expense_id::text,2) IN ('12','13')`
	if err = conn.QueryRow(ctx, retained).Scan(&retainedBefore); err != nil {
		t.Fatal(err)
	}
	exec(upgrade)
	var retainedAfter string
	if err = conn.QueryRow(ctx, retained).Scan(&retainedAfter); err != nil || retainedBefore != retainedAfter {
		t.Fatal("frozen or unreferenced payment changed", err)
	}
	var count int
	var fixed, skip, investor string
	if err = conn.QueryRow(ctx, `SELECT (SELECT count(*) FROM expense_payments),
 expense_choices#>>'{19800000-0000-0000-0000-000000000011,amount}',expense_choices#>>'{19800000-0000-0000-0000-000000000014,mode}',
 (SELECT expense_choices#>>'{19800000-0000-0000-0000-000000000015,amount}' FROM investor_pay_weeks)
 FROM driver_pay_weeks WHERE driver_id='19800000-0000-0000-0000-000000000001' AND week_start='2026-10-05'`).Scan(&count, &fixed, &skip, &investor); err != nil || count != 2 || fixed != "60.00" || skip != "skip" || investor != "35.00" {
		t.Fatal("draft conversion", count, fixed, skip, investor, err)
	}
	_, err = conn.Exec(ctx, `UPDATE expense_payments SET statement_id='19800000-0000-0000-0000-000000000001'
 WHERE expense_id='19800000-0000-0000-0000-000000000013'`)
	if constraint, ok := err.(*pgconn.PgError); !ok || constraint.Code != "23514" || constraint.ConstraintName != "expense_payment_statement" {
		t.Fatal("partial statement identity was not rejected by the pairing constraint", err)
	}
}
