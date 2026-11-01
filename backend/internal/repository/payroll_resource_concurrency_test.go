package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Both commands must reach their canonical write before either can commit.
// Holding a weekly job/header also proves those shared rows are absent from
// routine first-save and text-save publication, rather than merely uncontended.
func TestPayrollResourceConcurrencyDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		seedPayrollProjectionTest(t, ctx, pool)
		// Exercise normal saves, text saves and finalization with shared, packed
		// dependency maps, while preserving independent-command concurrency.
		if _, err := pool.Exec(ctx, `INSERT INTO expenses(category_id,category,expense_type,expense_date,amount,driver_id,covered_by)
 SELECT id,name,'Shared dependency fixture','2026-10-05',1,'18200000-0000-0000-0000-000000000001','Company'
 FROM expense_settings CROSS JOIN generate_series(1,180) WHERE kind='category' AND name='Other'`); err != nil {
			t.Fatal(err)
		}
		const independent = "18200000-0000-0000-0000-000000001501"
		const actor = "18200000-0000-0000-0000-000000001500"
		if _, err := pool.Exec(ctx, `INSERT INTO drivers(id,full_name,normalized_name,pay_type,pay_rate,hire_date)
 VALUES('18200000-0000-0000-0000-000000001501','Independent payer','independent payer','cpm',0.65,'2026-01-01');
 INSERT INTO gross_board_entries(driver_id,service_date,load_number,driver_rate,entered_original_rate,entered_miles)
 VALUES('18200000-0000-0000-0000-000000001501','2026-10-05','INDEPENDENT',200,300,120);
 INSERT INTO app_users(id,username,password_hash) VALUES('18200000-0000-0000-0000-000000001500','Resource concurrency',crypt('test-only',gen_salt('bf')));`); err != nil {
			t.Fatal(err)
		}
		repo := NewDriverPayRepository(pool)
		repo.resourceConcurrency = true
		repo.ConfigurePreparedFinalization(true)
		if err := repo.EnablePayrollShadow(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `UPDATE payroll_projection_control SET protocol_mode='resources' WHERE singleton`); err != nil {
			t.Fatal(err)
		}
		week, _ := chargeWeek("2026-10-05")
		if err := repo.RequestPayrollPreparation(ctx, week, false); err != nil {
			t.Fatal(err)
		}
		if _, err := repo.PrepareNextPayrollWeek(ctx); err != nil {
			t.Fatal(err)
		}
		var before, after string
		sharedRows := `SELECT jsonb_build_object('revisions',(SELECT jsonb_agg(to_jsonb(r) ORDER BY table_name) FROM payroll_cache_revisions r),
 'weeks',(SELECT jsonb_agg(to_jsonb(w) ORDER BY week_start) FROM payroll_projection_weeks w),
 'jobs',(SELECT jsonb_agg(to_jsonb(j) ORDER BY week_start) FROM payroll_projection_jobs j))::text`
		if err := pool.QueryRow(ctx, sharedRows).Scan(&before); err != nil {
			t.Fatal(err)
		}
		for pass := 0; pass < 2; pass++ {
			page, err := repo.PreparedPayroll(ctx, "driver", week, PayPageQuery{}, true)
			if err != nil || page.Preparation.State != "ready" {
				t.Fatal("prepared page", err, page.Preparation)
			}
			priorGeneration := page.Preparation.Generation
			inputs := []DriverPayEdits{}
			for _, d := range page.Drivers {
				if d.ID != projectionDriver && d.ID != independent {
					continue
				}
				e := d.Edits
				e.DriverID, e.WeekStart = d.ID, week.Format(time.DateOnly)
				e.ExpectedStatementRevision = d.StatementRevision
				e.RequestID = fmt.Sprintf("18200000-0000-0000-0000-%012d", 2200+pass*10+len(inputs))
				e.Notes = fmt.Sprintf("Concurrent notes %d", pass)
				inputs = append(inputs, e)
			}
			if len(inputs) != 2 {
				t.Fatal("independent fixture statements missing")
			}
			if _, err = admin.Exec(ctx, `CREATE FUNCTION test_parallel_payroll_barrier() RETURNS trigger LANGUAGE plpgsql AS $$
 BEGIN PERFORM pg_advisory_xact_lock_shared(1822200); RETURN NEW; END $$;
 CREATE TRIGGER test_parallel_payroll_barrier BEFORE INSERT OR UPDATE ON driver_pay_weeks FOR EACH ROW EXECUTE FUNCTION test_parallel_payroll_barrier()`); err != nil {
				t.Fatal(err)
			}
			hold, err := admin.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = hold.Exec(ctx, `SELECT pg_advisory_xact_lock(1822200);
 SELECT week_start FROM payroll_projection_jobs FOR UPDATE;
 SELECT week_start FROM payroll_projection_weeks FOR UPDATE`); err != nil {
				t.Fatal(err)
			}
			work, cancel := context.WithTimeout(ctx, 8*time.Second)
			type outcome struct {
				edits DriverPayEdits
				err   error
			}
			done := make(chan outcome, 2)
			for _, e := range inputs {
				go func(e DriverPayEdits) {
					saved, err := repo.Save(WithPayrollReceipt(work), e, actor)
					done <- outcome{saved, err}
				}(e)
			}
			waiting := 0
			deadline := time.Now().Add(4 * time.Second)
			for waiting < 2 && time.Now().Before(deadline) {
				err = pool.QueryRow(ctx, `SELECT count(*) FROM pg_locks WHERE locktype='advisory' AND objid=1822200 AND NOT granted`).Scan(&waiting)
				if err != nil {
					break
				}
				if waiting < 2 {
					time.Sleep(10 * time.Millisecond)
				}
			}
			_ = hold.Rollback(ctx)
			results := []outcome{<-done, <-done}
			cancel()
			if _, e := admin.Exec(ctx, `DROP TRIGGER test_parallel_payroll_barrier ON driver_pay_weeks; DROP FUNCTION test_parallel_payroll_barrier()`); e != nil {
				t.Fatal(e)
			}
			if err != nil || waiting != 2 {
				t.Fatal("independent commands did not enter writes concurrently", waiting, err, results[0].err, results[1].err)
			}
			for _, result := range results {
				if result.err != nil {
					t.Fatal("concurrent save", result.err)
				}
			}
			if err = pool.QueryRow(ctx, sharedRows).Scan(&after); err != nil || before != after {
				t.Fatal("command changed shared revision/job/header rows", err, before, after)
			}
			page, err = repo.PreparedPayroll(ctx, "driver", week, PayPageQuery{}, true)
			if err != nil || page.Preparation.State != "ready" || page.Preparation.Generation == priorGeneration {
				t.Fatal("current composite week revision", err, page.Preparation)
			}
			// Exact retry returns the original bytes after an unrelated commit.
			for _, input := range inputs {
				replayed, e := repo.Save(WithPayrollReceipt(ctx), input, actor)
				if e != nil {
					t.Fatal("receipt replay", e)
				}
				var original DriverPayEdits
				for _, result := range results {
					if result.edits.DriverID == input.DriverID {
						original = result.edits
					}
				}
				a, _ := json.Marshal(original)
				b, _ := json.Marshal(replayed)
				if string(a) != string(b) {
					t.Fatal("receipt replay changed")
				}
			}
		}
	})
}

// Two reviewed weeks can both show the same uncollected invoice. The later
// command must wait for the shared component, then reject its old review after
// the first command publishes the new reservation and every affected week.
func TestPayrollResourceSharedBalanceDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		seedPayrollProjectionTest(t, ctx, pool)
		const actor = "18200000-0000-0000-0000-000000002400"
		if _, err := pool.Exec(ctx, `INSERT INTO app_users(id,username,password_hash) VALUES('18200000-0000-0000-0000-000000002400','Shared balance',crypt('test-only',gen_salt('bf')));
 INSERT INTO gross_board_entries(driver_id,service_date,load_number,driver_rate,entered_original_rate,entered_miles)
 VALUES('18200000-0000-0000-0000-000000000001','2026-10-12','LATER',900,1100,300);
 INSERT INTO expenses(id,category_id,category,expense_type,expense_date,amount,truck_id,owner_id,covered_by)
 SELECT '18200000-0000-0000-0000-000000002401',id,name,'Shared invoice','2026-10-05',120,
 '18200000-0000-0000-0000-000000000002','18200000-0000-0000-0000-000000000003','Truck Owner'
 FROM expense_settings WHERE kind='category' AND name='Other'`); err != nil {
			t.Fatal(err)
		}
		repo := NewDriverPayRepository(pool)
		repo.resourceConcurrency = true
		if err := repo.EnablePayrollShadow(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `UPDATE payroll_projection_control SET protocol_mode='resources' WHERE singleton`); err != nil {
			t.Fatal(err)
		}
		inputs := []DriverPayEdits{}
		for i, date := range []string{"2026-10-05", "2026-10-12"} {
			week, _ := chargeWeek(date)
			if err := repo.RequestPayrollPreparation(ctx, week, false); err != nil {
				t.Fatal(err)
			}
			if _, err := repo.PrepareNextPayrollWeek(ctx); err != nil {
				t.Fatal(err)
			}
			page, err := repo.PreparedPayroll(ctx, "investor", week, PayPageQuery{}, true)
			if err != nil || len(page.Drivers) != 1 || page.Preparation.State != "ready" {
				t.Fatal("shared fixture", err, page.Preparation)
			}
			e := page.Drivers[0].Edits
			e.ExpectedStatementRevision = page.Drivers[0].StatementRevision
			e.RequestID = fmt.Sprintf("18200000-0000-0000-0000-%012d", 2410+i)
			e.Notes = "Shared invoice review"
			if len(e.ExpenseDeductions) != 1 || e.ExpenseDeductions[0].Amount != "120.00" {
				t.Fatal("shared invoice was not available in both reviews", date, e.ExpenseDeductions)
			}
			e.ExpenseDeductions[0].Apply = true
			inputs = append(inputs, e)
		}
		if _, err := admin.Exec(ctx, `CREATE FUNCTION test_shared_payroll_barrier() RETURNS trigger LANGUAGE plpgsql AS $$
 BEGIN IF NEW.week_start='2026-10-05' THEN PERFORM pg_advisory_xact_lock_shared(1822400); END IF; RETURN NEW; END $$;
 CREATE TRIGGER test_shared_payroll_barrier BEFORE INSERT OR UPDATE ON investor_pay_weeks FOR EACH ROW EXECUTE FUNCTION test_shared_payroll_barrier()`); err != nil {
			t.Fatal(err)
		}
		hold, err := admin.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer hold.Rollback(ctx)
		if _, err = hold.Exec(ctx, `SELECT pg_advisory_xact_lock(1822400)`); err != nil {
			t.Fatal(err)
		}
		work, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		first, second := make(chan error, 1), make(chan error, 1)
		go func() { _, e := repo.SaveInvestorPay(WithPayrollReceipt(work), inputs[0], actor); first <- e }()
		waitFor := func(query string) {
			t.Helper()
			deadline := time.Now().Add(3 * time.Second)
			for time.Now().Before(deadline) {
				var ok bool
				if e := hold.QueryRow(ctx, query).Scan(&ok); e != nil {
					t.Fatal(e)
				}
				if ok {
					return
				}
				time.Sleep(10 * time.Millisecond)
			}
			t.Fatal("command did not reach expected lock", query)
		}
		waitFor(`SELECT EXISTS(SELECT 1 FROM pg_locks WHERE locktype='advisory' AND objid=1822400 AND NOT granted)`)
		go func() { _, e := repo.SaveInvestorPay(WithPayrollReceipt(work), inputs[1], actor); second <- e }()
		waitFor(`SELECT EXISTS(SELECT 1 FROM pg_locks WHERE locktype='advisory' AND database=(SELECT oid FROM pg_database WHERE datname=current_database()) AND classid<>0 AND mode='ExclusiveLock' AND NOT granted)`)
		_ = hold.Rollback(ctx)
		if err = <-first; err != nil {
			t.Fatal("first allocation", err)
		}
		if err = <-second; !errors.Is(err, ErrDriverPayConflict) {
			t.Fatal("second command reused stale balances", err)
		}
		var paid string
		var receipts int
		if err = pool.QueryRow(ctx, `SELECT coalesce((SELECT sum(amount) FROM expense_payments WHERE expense_id='18200000-0000-0000-0000-000000002401'),0)::text,(SELECT count(*) FROM payroll_command_receipts)`).Scan(&paid, &receipts); err != nil || paid != "120.00" || receipts != 1 {
			t.Fatal("shared invoice allocations", paid, receipts, err)
		}
		if _, err = admin.Exec(ctx, `DROP TRIGGER test_shared_payroll_barrier ON investor_pay_weeks;DROP FUNCTION test_shared_payroll_barrier()`); err != nil {
			t.Fatal(err)
		}
	})
}
