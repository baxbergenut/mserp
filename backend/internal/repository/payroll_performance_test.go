package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"mserp/internal/db"
)

type payrollPlanKey struct{}
type payrollPlanQuery struct {
	sql     string
	args    []any
	start   time.Time
	elapsed time.Duration
}
type payrollPlanTracer struct {
	db.Tracer
	mu   sync.Mutex
	slow payrollPlanQuery
}

func (p *payrollPlanTracer) TraceQueryStart(ctx context.Context, c *pgx.Conn, d pgx.TraceQueryStartData) context.Context {
	ctx = p.Tracer.TraceQueryStart(ctx, c, d)
	return context.WithValue(ctx, payrollPlanKey{}, payrollPlanQuery{sql: d.SQL, args: d.Args, start: time.Now()})
}
func (p *payrollPlanTracer) TraceQueryEnd(ctx context.Context, c *pgx.Conn, d pgx.TraceQueryEndData) {
	p.Tracer.TraceQueryEnd(ctx, c, d)
	q := ctx.Value(payrollPlanKey{}).(payrollPlanQuery)
	q.elapsed = time.Since(q.start)
	p.mu.Lock()
	defer p.mu.Unlock()
	if !strings.HasPrefix(q.sql, "EXPLAIN") && (os.Getenv("MSERP_PAYROLL_EXPLAIN_MATCH") == "" || strings.Contains(q.sql, os.Getenv("MSERP_PAYROLL_EXPLAIN_MATCH"))) && q.elapsed > p.slow.elapsed {
		p.slow = q
	}
}

// Opt-in benchmark uses only synthetic records in a disposable local database.
// Keep the fixture large enough that full-fleet work cannot hide behind tiny tests.
func TestPayrollPerformance(t *testing.T) {
	if os.Getenv("MSERP_PAYROLL_PERF") != "1" {
		t.Skip("set MSERP_PAYROLL_PERF=1")
	}
	dsn := os.Getenv("MSERP_DRIVER_PAY_TEST_DATABASE_URL")
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.HasSuffix(cfg.ConnConfig.Database, "_test") || (cfg.ConnConfig.Host != "127.0.0.1" && cfg.ConnConfig.Host != "localhost" && cfg.ConnConfig.Host != "/var/run/postgresql") {
		t.Fatal("local _test database required")
	}
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(ctx)
	schema := fmt.Sprintf("payroll_perf_%d", time.Now().UnixNano())
	exec := func(q string) {
		t.Helper()
		if _, e := admin.Exec(ctx, q); e != nil {
			t.Fatal(e)
		}
	}
	exec("CREATE SCHEMA " + schema + "; SET search_path TO " + schema + ",public")
	defer func() {
		if os.Getenv("MSERP_PAYROLL_KEEP_FIXTURE") != "1" {
			_, _ = admin.Exec(ctx, "SET search_path TO public; DROP SCHEMA "+schema+" CASCADE")
		}
	}()
	source, err := os.ReadFile("../../sql/init.sql")
	if err != nil {
		t.Fatal(err)
	}
	exec(string(source))
	exec(`INSERT INTO drivers(id,full_name,normalized_name,pay_type,pay_rate,hire_date)
 SELECT md5('driver'||n)::uuid,'Perf Driver '||lpad(n::text,3,'0'),'perf driver '||n,CASE WHEN n%4=0 THEN 'gross_percentage' ELSE 'cpm' END,CASE WHEN n%4=0 THEN 88 ELSE 0.75 END,'2026-01-01' FROM generate_series(1,176)n;
 INSERT INTO investors(id,full_name,driver_id) SELECT md5('owner'||n)::uuid,'Perf Owner '||n,CASE WHEN n%4=0 THEN md5('driver'||n)::uuid END FROM generate_series(1,176)n;
 INSERT INTO trucks(id,unit_number,owner_id) SELECT md5('truck'||n)::uuid,'PERF-'||n,md5('owner'||n)::uuid FROM generate_series(1,176)n;
 UPDATE truck_ownership_history SET start_known=false;
 INSERT INTO truck_driver_assignments(truck_id,driver_id,assigned_at) SELECT md5('truck'||n)::uuid,md5('driver'||n)::uuid,'2026-01-01' FROM generate_series(1,176)n;
 INSERT INTO truck_settlement_terms(truck_id,owner_id,week_start,share_percent) SELECT md5('truck'||n)::uuid,md5('owner'||n)::uuid,'2026-01-05',88 FROM generate_series(1,176)n;
 INSERT INTO gross_board_entries(driver_id,service_date,load_number,driver_rate,entered_original_rate,entered_miles)
 SELECT md5('driver'||n)::uuid,'2026-09-21'::date+d,'PERF-'||n||'-'||d,1000,1200,400 FROM generate_series(1,176)n CROSS JOIN generate_series(0,27)d;
 INSERT INTO loads(id,load_id,status,load_pay,total_pay,total_miles,pickup_time,truck_id,truck_unit,raw_payload)
 SELECT n*100+d,'PERF-'||n||'-'||d,'delivered',1000,1200,400,'2026-09-21 12:00Z'::timestamptz+d*interval '1 day',md5('truck'||n)::uuid,'PERF-'||n,'{}'::jsonb FROM generate_series(1,176)n CROSS JOIN generate_series(0,27)d;
 INSERT INTO tolls(truck_id,posting_date,invoice_date,customer_id,source,read_type,transponder_or_plate,equipment_unit,agency,exit_plaza,exit_date,exit_time,toll_class,amount,row_fingerprint)
 SELECT md5('truck'||(1+n%176))::uuid,'2026-09-21'::date+n%28,'2026-10-01','test','test','test','test','PERF-'||(1+n%176),'test','test','2026-09-21'::date+n%28,'12:00','test',10,md5('toll'||n)||md5('toll'||n) FROM generate_series(1,6000)n;
 INSERT INTO expenses(category_id,category,expense_date,amount,charge_driver_id,driver_settled,expense_type)
 SELECT (SELECT id FROM expense_settings WHERE kind='category' AND name='Other'),'Other','2026-09-21',100,md5('driver'||(1+(n%176)))::uuid,n>176,'Perf charge' FROM generate_series(1,6000)n;
 INSERT INTO driver_escrows(id,driver_id,driver_name,start_date,amount,opening_paid)
 SELECT md5('escrow'||n)::uuid,md5('driver'||n)::uuid,'Perf Driver '||n,'2026-01-01',2500,500 FROM generate_series(1,176)n WHERE n%4<>0;
 INSERT INTO driver_escrow_payments(escrow_id,week_start,amount)
 SELECT id,'2026-09-21'::date+7*w,100 FROM driver_escrows CROSS JOIN generate_series(0,1)w;
 -- Only fixture creation may import an already historical release. Restore the
 -- ordinary date/version/funding guard before every measured read and save.
 ALTER TABLE driver_escrow_releases DISABLE TRIGGER escrow_release_guard;
 INSERT INTO driver_escrow_releases(escrow_id,week_start,amount)
 SELECT id,'2026-09-28',50 FROM driver_escrows;
 ALTER TABLE driver_escrow_releases ENABLE TRIGGER escrow_release_guard;
 ALTER TABLE fuel_transactions DISABLE TRIGGER fuel_reporting_fields_before;
 WITH inserted AS (
 INSERT INTO fuel_transactions(relay_environment,relay_transaction_id,driver_id,relay_driver_id,purchased_at,total_amount_paid,total_retail_price,total_amount_saved,is_direct_bill,currency_code,merchant_id,merchant_name,merchant_number,location_id,location_name,merchant_location_id,address,city,state,postal_code,latitude,longitude,timezone,raw_payload,prompts,purchased_on,reporting_timezone,reporting_timezone_valid,reported_truck_units)
 SELECT 'production','perf-'||n,md5('driver'||(1+(n%176)))::uuid,'perf-'||(1+(n%176)),'2026-09-21 12:00Z'::timestamptz+(n%28)*interval '1 day',100,100,0,false,'USD','','','','','','','','','','',0,0,'US/Eastern','{}',jsonb_build_array(jsonb_build_object('name','Truck #','value','PERF-'||(1+(n%176)))),'2026-09-21'::date+(n%28),'US/Eastern',true,ARRAY['PERF-'||(1+(n%176))] FROM generate_series(1,11449)n RETURNING id
 ) INSERT INTO fuel_transaction_items(fuel_transaction_id,line_number,item_kind,category,total_amount_paid) SELECT id,0,'fuel','diesel',100 FROM inserted;
 ALTER TABLE fuel_transactions ENABLE TRIGGER fuel_reporting_fields_before;
 INSERT INTO driver_pay_weeks(driver_id,week_start,fuel_override) VALUES(md5('driver4')::uuid,'2026-09-21',-50),(md5('driver8')::uuid,'2026-09-28',-50);
 ANALYZE;
 `)
	exec("GRANT USAGE ON SCHEMA " + schema + " TO mserp_app; GRANT ALL ON ALL TABLES IN SCHEMA " + schema + " TO mserp_app; GRANT USAGE,SELECT ON ALL SEQUENCES IN SCHEMA " + schema + " TO mserp_app")
	if os.Getenv("MSERP_PAYROLL_PERF_CUTOVER") == "1" {
		// Synthetic fixture activation only. Product activation is covered by
		// TestAccountingCutoverDatabase and the browser test, never bypassed live.
		exec(`INSERT INTO accounting_legacy_sources(kind,source_id) SELECT 'escrow',id FROM driver_escrows;
 INSERT INTO accounting_rate_openings(driver_id,amount,reference) SELECT id,0,'Synthetic reviewed zero' FROM drivers;
 UPDATE accounting_cutover SET active=true,week_start='2026-10-05',version=version+1,activated_at=now(),receipt='{"synthetic":true}';`)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
	cfg.ConnConfig.RuntimeParams["role"] = "mserp_app"
	cfg.ConnConfig.RuntimeParams["jit"] = "off"
	tracer := &payrollPlanTracer{}
	cfg.ConnConfig.Tracer = tracer
	cfg.MaxConns = 5
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if os.Getenv("MSERP_PAYROLL_KEEP_FIXTURE") == "1" {
		t.Logf("retained synthetic schema=%s", schema)
		return
	}
	week, _ := chargeWeek("2026-10-05")
	repo := NewDriverPayRepository(pool)
	if os.Getenv("MSERP_PAYROLL_PERF_SKIP_PARITY") != "1" {
		// Compare every selected statement with the same full-week calculation.
		for _, kind := range []string{"driver", "investor"} {
			var full DriverPayWeek
			var e error
			if kind == "driver" {
				full, e = repo.Get(ctx, week)
			} else {
				full, e = repo.InvestorPay(ctx, week)
			}
			if e != nil {
				t.Fatal(e)
			}
			// The compact calculation must preserve totals, search, metadata and
			// the whole-input revision before any detailed statement is requested.
			for _, q := range []PayPageQuery{{Pagination: Pagination{Page: 1, PageSize: 25}, Summary: true}, {Pagination: Pagination{Page: 2, PageSize: 25}, Summary: true}, {Pagination: Pagination{Page: 1, PageSize: 25}, Summary: true, Search: "Owner"}} {
				want, _ := json.Marshal(PaginatePay(full, q))
				page, err := repo.PayrollPage(ctx, kind, week, q)
				if err != nil {
					t.Fatal(err)
				}
				got, _ := json.Marshal(page)
				if string(want) != string(got) {
					t.Fatalf("%s compact calculation differs\n%s\n%s", kind, want, got)
				}
			}
			for _, d := range full.Drivers {
				var selected DriverPayWeek
				if kind == "driver" {
					selected, e = repo.GetDriverWeek(ctx, week, d.ID)
				} else {
					selected, e = repo.InvestorStatement(ctx, week, d.ID)
				}
				if e != nil {
					t.Fatal(e)
				}
				if len(selected.Drivers) != 1 {
					t.Fatalf("%s selection returned %d statements", kind, len(selected.Drivers))
				}
				want, _ := json.Marshal(d)
				got, _ := json.Marshal(selected.Drivers[0])
				if string(got) != string(want) {
					t.Fatalf("%s selected calculation differs for %s\nfull=%s\nselected=%s", kind, d.ID, want, got)
				}
			}
		}
		// The dedicated historical cost projection must agree with the original full source path.
		for _, date := range []string{"2026-09-21", "2026-09-28"} {
			w, _ := chargeWeek(date)
			tx, e := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
			if e != nil {
				t.Fatal(e)
			}
			if e = payrollQueryPlans(ctx, tx); e != nil {
				t.Fatal(e)
			}
			full, e := readDriverPaySourceWeek(ctx, tx, w, "")
			if e != nil {
				t.Fatal(e)
			}
			full, _, e = routeTruckPaySources(ctx, tx, full)
			if e != nil {
				t.Fatal(e)
			}
			thinCtx := context.WithValue(ctx, payrollCostsOnlyKey{}, true)
			thin, e := readDriverPayCostWeek(thinCtx, tx, w)
			if e != nil {
				t.Fatal(e)
			}
			thin, _, e = routeTruckPaySources(thinCtx, tx, thin)
			if e != nil {
				t.Fatal(e)
			}
			for _, d := range full.Drivers {
				for _, v := range thin.Drivers {
					if d.ID == v.ID {
						wf, wt := driverCostBase(d)
						gf, gt := driverCostBase(v)
						if wf != gf || wt != gt {
							t.Fatal("historical cost projection differs")
						}
					}
				}
			}
			_ = tx.Rollback(ctx)
		}
	}
	for _, kind := range []string{"driver", "investor"} {
		read := func(c context.Context, r *DriverPayRepository) (DriverPayWeek, error) {
			if kind == "driver" {
				return r.Get(c, week)
			}
			return r.InvestorPay(c, week)
		}
		for _, mode := range []string{"cold", "warm", "changed", "expand", "save"} {
			t.Run(kind+"/"+mode, func(t *testing.T) {
				report, e := read(ctx, repo)
				if e != nil {
					t.Fatal(e)
				}
				if len(report.Drivers) == 0 {
					t.Fatal("missing fixture statements")
				}
				edits := report.Drivers[0].Edits
				var samples []float64
				runs := 100
				if os.Getenv("MSERP_PAYROLL_PERF_QUICK") == "1" {
					runs = 3
				}
				if n, _ := strconv.Atoi(os.Getenv("MSERP_PAYROLL_PERF_RUNS")); n > 0 {
					runs = n
				}
				errors := 0
				for n := 0; n < runs; n++ {
					if mode == "changed" {
						exec(`UPDATE drivers SET pay_rate=pay_rate WHERE id=md5('driver1')::uuid`)
					}
					c, m := db.WithQueryMetrics(ctx)
					start := time.Now()
					if mode == "save" {
						edits.Notes = fmt.Sprintf("Perf %d", n)
						if kind == "driver" {
							edits, e = repo.Save(c, edits)
						} else {
							edits, e = repo.SaveInvestorPay(c, edits, "")
						}
					} else if mode == "expand" {
						r := NewDriverPayRepository(pool)
						if kind == "driver" {
							_, e = r.GetDriverWeek(c, week, edits.DriverID)
						} else {
							_, e = r.InvestorStatement(c, week, edits.DriverID)
						}
					} else {
						r := repo
						if mode == "cold" {
							r = NewDriverPayRepository(pool)
						}
						if os.Getenv("MSERP_PAYROLL_PERF_SUMMARY") == "1" {
							_, e = r.PayrollPage(c, kind, week, PayPageQuery{Pagination: Pagination{Page: 1, PageSize: 25}, Summary: true})
						} else {
							_, e = read(c, r)
						}
					}
					samples = append(samples, float64(time.Since(start).Microseconds())/1000)
					if e != nil {
						errors++
						t.Logf("sample %d failed: %v", n, e)
					}
					if n == 0 {
						phases, queries := m.Details()
						body, _ := json.Marshal(map[string]any{"phases": phases, "queries": queries, "calls": m.Queries.Load()})
						t.Log(string(body))
						if mode == "cold" && os.Getenv("MSERP_PAYROLL_EXPLAIN") == "1" {
							tracer.mu.Lock()
							q := tracer.slow
							tracer.mu.Unlock()
							tx, e := pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
							if e != nil {
								t.Fatal(e)
							}
							rows, e := tx.Query(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT TEXT) "+q.sql, q.args...)
							if e != nil {
								t.Fatal(e)
							}
							for rows.Next() {
								var line string
								_ = rows.Scan(&line)
								t.Log(line)
							}
							rows.Close()
							_ = tx.Rollback(ctx)
						}
					}
				}
				payrollPerfSummary(t, samples, errors)
			})
		}
		t.Run(kind+"/save5", func(t *testing.T) {
			report, e := read(ctx, repo)
			if e != nil {
				t.Fatal(e)
			}
			runs := 100
			if os.Getenv("MSERP_PAYROLL_PERF_QUICK") == "1" {
				runs = 3
			}
			if n, _ := strconv.Atoi(os.Getenv("MSERP_PAYROLL_PERF_RUNS")); n > 0 {
				runs = n
			}
			var mu sync.Mutex
			var wg sync.WaitGroup
			var samples []float64
			errors := 0
			start := make(chan struct{})
			for worker := 0; worker < 5; worker++ {
				edits := report.Drivers[worker].Edits
				wg.Add(1)
				go func() {
					defer wg.Done()
					<-start
					for n := 0; n < runs; n++ {
						edits.Notes = fmt.Sprintf("Concurrent %d", n)
						before := time.Now()
						var e error
						if kind == "driver" {
							edits, e = repo.Save(ctx, edits)
						} else {
							edits, e = repo.SaveInvestorPay(ctx, edits, "")
						}
						elapsed := float64(time.Since(before).Microseconds()) / 1000
						mu.Lock()
						samples = append(samples, elapsed)
						if e != nil {
							errors++
							t.Logf("save error: %v", e)
						}
						mu.Unlock()
					}
				}()
			}
			close(start)
			wg.Wait()
			payrollPerfSummary(t, samples, errors)
		})
		t.Run(kind+"/summary5sync", func(t *testing.T) {
			var wg sync.WaitGroup
			var mu sync.Mutex
			var samples []float64
			failures := 0
			for worker := 0; worker < 5; worker++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for n := 0; n < 20; n++ {
						start := time.Now()
						_, e := repo.PayrollPage(ctx, kind, week, PayPageQuery{Pagination: Pagination{Page: 1, PageSize: 25}, Summary: true})
						mu.Lock()
						samples = append(samples, float64(time.Since(start).Microseconds())/1000)
						if e != nil {
							failures++
							t.Log(e)
						}
						mu.Unlock()
					}
				}()
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				for n := 0; n < 10; n++ {
					if _, e := pool.Exec(ctx, `UPDATE loads SET total_pay=total_pay+0.01 WHERE id=100`); e != nil {
						mu.Lock()
						failures++
						mu.Unlock()
					}
					time.Sleep(30 * time.Millisecond)
				}
			}()
			wg.Wait()
			payrollPerfSummary(t, samples, failures)
		})

	}
}

func payrollPerfSummary(t *testing.T, samples []float64, errors int) {
	t.Helper()
	sort.Float64s(samples)
	p95 := samples[(len(samples)*95+99)/100-1]
	t.Logf("samples=%d errors=%d p50=%.2fms p95=%.2fms p99=%.2fms max=%.2fms", len(samples), errors, samples[(len(samples)-1)/2], p95, samples[(len(samples)*99+99)/100-1], samples[len(samples)-1])
	if errors > 0 {
		t.Errorf("%d failed actions (included in samples)", errors)
	}
	if raw := os.Getenv("MSERP_PAYROLL_API_BUDGET_MS"); raw != "" {
		budget, e := strconv.ParseFloat(raw, 64)
		if e != nil || budget <= 0 {
			t.Fatal("invalid API budget")
		}
		if p95 >= budget {
			t.Errorf("API p95 %.2fms exceeds %.2fms budget; this is not a browser latency measurement", p95, budget)
		}
	}
}
