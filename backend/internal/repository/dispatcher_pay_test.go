package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func workbookCommissionRules() CommissionRules {
	return CommissionRules{HomeDayCredit: 1100, MaxTierPromotion: 1, Tiers: []CommissionTier{
		{7500, []CommissionBracket{{2.2, 1}, {2.31, 1.25}, {2.7, 1.5}}},
		{9000, []CommissionBracket{{2.4, 1}, {2.61, 1.5}, {2.9, 1.75}}},
		{11000, []CommissionBracket{{2.5, 1.25}, {2.71, 1.75}, {2.9, 2}}},
	}}
}
func TestDispatcherCommissionWorkbook(t *testing.T) {
	rules := workbookCommissionRules()
	for _, tc := range []struct {
		name                   string
		gross, miles           int64
		home, actual, selected int
		rate                   int64
	}{
		{"below gross", 749999, 250000, 0, 0, 0, 0},
		{"first tier first RPM", 770000, 350000, 0, 1, 1, 100},
		{"first tier second RPM exact", 808500, 350000, 0, 1, 1, 125},
		{"first tier third RPM exact", 810000, 300000, 0, 1, 1, 150},
		{"second tier first RPM exact", 960000, 400000, 0, 2, 2, 100},
		{"second tier second RPM exact", 913500, 350000, 0, 2, 2, 150},
		{"second tier third RPM exact", 1015000, 350000, 0, 2, 2, 175},
		{"third tier first RPM exact", 1100000, 440000, 0, 3, 3, 125},
		{"third tier second RPM exact", 1355000, 500000, 0, 3, 3, 175},
		{"third tier third RPM exact", 1160000, 400000, 0, 3, 3, 200},
		{"below first RPM", 750000, 350000, 0, 1, 1, 0},
		{"below second bracket", 808499, 350000, 0, 1, 1, 100},
		{"one tier cap", 500000, 200000, 4, 0, 1, 125},
		{"no home protection", 850000, 370000, 0, 1, 1, 100},
		{"home may remove commission", 850000, 370000, 1, 1, 2, 0},
		{"no miles", 1100000, 0, 0, 3, 3, 0},
		{"zero gross", 0, 0, 7, 0, 1, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			adjusted, actual, selected, rate := commissionCalculation(tc.gross, tc.miles, tc.home, rules)
			if adjusted != tc.gross+int64(tc.home)*110000 || actual != tc.actual || selected != tc.selected || rate != tc.rate {
				t.Fatalf("got adjusted=%d tier=%d/%d rate=%d", adjusted, actual, selected, rate)
			}
		})
	}
}
func TestDispatcherCommissionBoardAttribution(t *testing.T) {
	board := GrossBoard{WeekStart: "2026-10-26", Drivers: []GrossBoardDriver{{ID: "driver", FullName: "Driver", Assignments: []DailyAssignment{{Date: "2026-10-26", DispatcherID: "a", DispatcherName: "First", TruckUnit: "01"}, {Date: "2026-10-27", DispatcherID: "b", DispatcherName: "Second", TruckUnit: "02"}}}}, Entries: []GrossBoardEntry{
		{DriverID: "driver", Date: "2026-10-26", LoadNumber: "1", OriginalRate: "5000", Miles: "1000"},
		{DriverID: "driver", Date: "2026-10-27", LoadNumber: "2", OriginalRate: "5000", Miles: "3000"},
		{DriverID: "driver", Date: "2026-10-27", LoadNumber: "1", OriginalRate: "5000", Miles: "1000", Duplicate: true},
		{DriverID: "driver", Date: "2026-10-27", LoadNumber: "3", OriginalRate: "9999", Miles: "1", Deleted: true},
		{DriverID: "driver", Date: "2026-10-27", DayStatus: "HOME"},
		{DriverID: "driver", Date: "2026-10-27", TransferOut: true, LoadNumber: "4", OriginalRate: "9999", Miles: "1"},
	}}
	rule := &CommissionRuleVersion{Rules: workbookCommissionRules()}
	out := buildDispatcherCommissions(board, map[string]DispatcherPayInput{"driver": {HomeDays: 0}}, rule)
	if len(out) != 2 {
		t.Fatal(out)
	}
	for _, s := range out {
		row := s.Rows[0]
		miles := float64(1000)
		if s.DispatcherID == "b" {
			miles = 3000
		}
		if row.CreditedMiles == nil || *row.CreditedMiles != miles {
			t.Fatal("dispatcher mileage must use only its own loads", row)
		}
		if s.Pay != 50 || s.Gross != 5000 || row.Gross != 10000 || row.Miles != 4000 || row.RPM != 2.5 || row.Rate != 1 || row.LoadCount != 1 {
			t.Fatalf("driver-week weighted RPM and credited gross differ: %+v", s)
		}
	}
	board.Entries[0].TransferVersion = 1
	board.Entries[0].CreditedDispatcherID = "b"
	board.Entries[0].CreditedDispatcherName = "Second"
	out = buildDispatcherCommissions(board, map[string]DispatcherPayInput{"driver": {HomeDays: 0}}, rule)
	if len(out) != 1 || out[0].Pay != 100 || out[0].LoadCount != 2 {
		t.Fatal("transfer lost original credit", out)
	}
	board.Entries[0].TripReview = true
	out = buildDispatcherCommissions(board, map[string]DispatcherPayInput{"driver": {HomeDays: 0}}, rule)
	if out[0].Pay != 0 || len(out[0].Rows[0].Issues) == 0 {
		t.Fatal("review split was paid", out)
	}
}
func TestDispatcherCommissionRuleValidation(t *testing.T) {
	if err := workbookCommissionRules().Validate(); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*CommissionRules){func(r *CommissionRules) { r.Tiers[1].MinGross = 7000 }, func(r *CommissionRules) { r.Tiers[0].Brackets[1].MinRPM = 2.2 }, func(r *CommissionRules) { r.Tiers[0].Brackets[0].Percent = 101 }, func(r *CommissionRules) { r.HomeDayCredit = -1 }, func(r *CommissionRules) { r.MaxTierPromotion = 4 }} {
		r := workbookCommissionRules()
		change(&r)
		if r.Validate() == nil {
			t.Fatal("invalid rule accepted", r)
		}
	}
	if _, err := dispatcherWeek("2026-10-27"); err == nil {
		t.Fatal("non-Monday accepted")
	}
}

func TestDispatcherCommissionSignedCorrections(t *testing.T) {
	if roundedCommission(-50050, 125) != -626 || roundedCommission(50050, 125) != 626 {
		t.Fatal("positive and negative commissions must round symmetrically to cents")
	}
	board := GrossBoard{WeekStart: "2026-10-26", Drivers: []GrossBoardDriver{{ID: "d", Assignments: []DailyAssignment{{Date: "2026-10-26", DispatcherID: "a"}, {Date: "2026-10-27", DispatcherID: "b"}}}}, Entries: []GrossBoardEntry{
		{DriverID: "d", Date: "2026-10-26", LoadNumber: "1", OriginalRate: "5000", Miles: "1000"},
		{DriverID: "d", Date: "2026-10-27", LoadNumber: "2", OriginalRate: "-500", Miles: "1000"},
	}}
	out := buildDispatcherCommissions(board, map[string]DispatcherPayInput{"d": {HomeDays: 3}}, &CommissionRuleVersion{Rules: workbookCommissionRules()})
	var pay float64
	for _, s := range out {
		pay += s.Pay
		if s.Rows[0].Gross != 4500 || len(s.Rows[0].Issues) != 0 {
			t.Fatal("Gross Board corrections must stay in weekly totals", s)
		}
	}
	if pay != 45 {
		t.Fatal("corrections must reduce commission", out)
	}
}

func TestDispatcherHomeDaysFromGrossBoard(t *testing.T) {
	board := GrossBoard{WeekStart: "2026-10-26", Drivers: []GrossBoardDriver{{ID: "d", Assignments: []DailyAssignment{{Date: "2026-10-26", DispatcherID: "a"}}}}, Entries: []GrossBoardEntry{
		{DriverID: "d", Date: "2026-10-26", LoadNumber: "1", OriginalRate: "8000", Miles: "3200"},
		{DriverID: "d", Date: "2026-10-27", DayStatus: "HOME"},
		{DriverID: "d", Date: "2026-10-27", Slot: 1, DayStatus: "HOME"},
		{DriverID: "d", Date: "2026-10-28", DayStatus: "HOME", Deleted: true},
		{DriverID: "d", Date: "2026-10-29", DayStatus: "SHOP"},
		{DriverID: "d", Date: "2026-11-02", DayStatus: "HOME"},
	}}
	rule := &CommissionRuleVersion{Rules: workbookCommissionRules()}
	auto := buildDispatcherCommissions(board, nil, rule)[0]
	if !auto.Rows[0].AutoHomeDays || auto.Rows[0].HomeDays != 1 || auto.Rows[0].BoardHomeDays != 1 || auto.Pay != 80 {
		t.Fatal("unique HOME dates should automatically qualify the tier", auto)
	}
	manual := buildDispatcherCommissions(board, map[string]DispatcherPayInput{"d": {HomeDays: 0}}, rule)[0]
	if manual.Rows[0].AutoHomeDays || manual.Rows[0].HomeDays != 0 || manual.Rows[0].BoardHomeDays != 1 || manual.Pay != 100 {
		t.Fatal("explicit manual overrides, including zero, must be preserved", manual)
	}
	again := buildDispatcherCommissions(board, map[string]DispatcherPayInput{"d": {HomeDays: 7, AutoHomeDays: true}}, rule)[0]
	if again.Rows[0].HomeDays != 1 {
		t.Fatal("automatic days must use live board evidence", again)
	}
}

func TestDispatcherPayDatabase(t *testing.T) {
	dsn := os.Getenv("MSERP_DISPATCHER_PAY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set local disposable MSERP_DISPATCHER_PAY_TEST_DATABASE_URL")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.HasSuffix(cfg.ConnConfig.Database, "_test") || (cfg.ConnConfig.Host != "localhost" && cfg.ConnConfig.Host != "127.0.0.1") {
		t.Fatal("local _test database required")
	}
	source, err := os.ReadFile("../../sql/init.sql")
	if err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile("../../sql/163_dispatcher_commissions.sql")
	if err != nil {
		t.Fatal(err)
	}
	initial := strings.ReplaceAll(string(source), "\r\n", "\n")
	body := strings.TrimSpace(strings.ReplaceAll(string(migration), "\r\n", "\n"))
	homeMigration, err := os.ReadFile("../../sql/164_dispatcher_home_days.sql")
	if err != nil {
		t.Fatal(err)
	}
	homeBody := strings.TrimSpace(strings.ReplaceAll(string(homeMigration), "\r\n", "\n"))
	if strings.Count(initial, homeBody) != 1 {
		t.Fatal("home-day init and migration must agree")
	}
	if strings.Count(initial, body) != 1 {
		t.Fatal("init and migration must agree")
	}
	for _, upgrade := range []bool{false, true} {
		t.Run(fmt.Sprintf("upgrade=%v", upgrade), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			admin, err := pgx.Connect(ctx, dsn)
			if err != nil {
				t.Fatal(err)
			}
			defer admin.Close(context.Background())
			schema := fmt.Sprintf("dispatcher_pay_%d", time.Now().UnixNano())
			quoted := pgx.Identifier{schema}.Sanitize()
			if _, err = admin.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
				t.Fatal(err)
			}
			defer admin.Exec(context.Background(), "DROP SCHEMA "+quoted+" CASCADE")
			conf := cfg.Copy()
			conf.MaxConns = 1
			conf.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
			pool, err := pgxpool.NewWithConfig(ctx, conf)
			if err != nil {
				t.Fatal(err)
			}
			defer pool.Close()
			exec := func(sql string, args ...any) {
				t.Helper()
				if _, err := pool.Exec(ctx, sql, args...); err != nil {
					t.Fatal(err)
				}
			}
			if upgrade {
				exec(strings.Replace(strings.Replace(initial, body, "", 1), homeBody, "", 1))
				exec(string(migration))
				exec(`INSERT INTO drivers(id,full_name,normalized_name,pay_type,pay_rate) VALUES('93000000-0000-0000-0000-000000000003','Legacy Home Driver','legacy home driver','cpm',1); INSERT INTO dispatcher_pay_inputs(week_start,driver_id,home_days) VALUES('2099-01-05','93000000-0000-0000-0000-000000000003',2)`)
				exec(string(homeMigration))
				var automatic bool
				var days int
				if err = pool.QueryRow(ctx, `SELECT auto_home_days,home_days FROM dispatcher_pay_inputs WHERE driver_id='93000000-0000-0000-0000-000000000003'`).Scan(&automatic, &days); err != nil || automatic || days != 2 {
					t.Fatal("migration changed an existing manual home-day decision", err, automatic, days)
				}
			} else {
				exec(initial)
			}
			var owned bool
			if err = pool.QueryRow(ctx, `SELECT count(*)=4 AND bool_and(tableowner='mserp_app') FROM pg_tables WHERE schemaname=current_schema() AND tablename IN ('dispatcher_commission_rules','dispatcher_pay_inputs','dispatcher_pay_statements','dispatcher_pay_history')`).Scan(&owned); err != nil || !owned {
				t.Fatal("new runtime tables must belong to mserp_app", err)
			}
			// Legacy base tables need grants in isolated schemas; new tables are owned above.
			exec("GRANT USAGE ON SCHEMA " + quoted + " TO mserp_app; GRANT SELECT,INSERT,UPDATE,DELETE ON ALL TABLES IN SCHEMA " + quoted + " TO mserp_app; SET ROLE mserp_app")
			const driver = "93000000-0000-0000-0000-000000000001"
			const dispatcher = "93000000-0000-0000-0000-000000000002"
			const finalizer = "93000000-0000-0000-0000-000000000004"
			exec(`INSERT INTO app_users(id,username,password_hash) VALUES($1,'Payroll Reviewer','$2test')`, finalizer)
			exec(`INSERT INTO dispatchers(id,full_name,normalized_name) VALUES($1,'Commission Dispatcher','commission dispatcher')`, dispatcher)
			exec(`INSERT INTO drivers(id,full_name,normalized_name,pay_type,pay_rate,dispatcher_id) VALUES($1,'Commission Driver','commission driver','cpm',1,$2)`, driver, dispatcher)
			exec(`UPDATE driver_dispatcher_assignments SET assigned_at='2099-01-05'::date::timestamp AT TIME ZONE 'America/New_York',start_known=true WHERE driver_id=$1`, driver)
			exec(`INSERT INTO loads(id,load_id,status,total_pay,load_pay,total_miles) VALUES(9301,'COMMISSION-1','delivered',123,123,456)`)
			exec(`INSERT INTO gross_board_entries(driver_id,service_date,load_number,load_record_id,entered_original_rate,entered_miles,driver_rate) VALUES($1,'2099-01-05','COMMISSION-1',9301,8500,3700,1)`, driver)
			repo := NewDispatcherPayRepository(pool)
			versions, err := repo.Rules(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if len(versions) != 1 {
				t.Fatal("missing workbook defaults", versions)
			}
			in := CommissionRuleChange{ExpectedRevision: versions[0].ID, EffectiveWeek: "2099-01-05", Rules: workbookCommissionRules()}
			if err = repo.SaveRules(ctx, in, "test"); err != nil {
				t.Fatal(err)
			}
			if err = repo.SaveRules(ctx, in, "test"); !errors.Is(err, ErrDispatcherPayConflict) {
				t.Fatal("stale rules accepted", err)
			}
			get := func() DispatcherCommissionReport {
				t.Helper()
				v, e := repo.Get(ctx, "2099-01-05")
				if e != nil {
					t.Fatal(e)
				}
				return v
			}
			report := get()
			if len(report.Statements) != 1 || report.Statements[0].Pay != 85 || report.Statements[0].Rows[0].Gross != 8500 {
				t.Fatalf("not Gross Board source: %+v", report)
			}
			exec(`UPDATE dispatchers SET pay_percentage=99 WHERE id=$1`, dispatcher)
			fleet := NewFleetRepository(pool)
			created, err := fleet.CreateDispatcher(ctx, DispatcherInput{FullName: "Profile Without Commission", Active: true})
			if err != nil {
				t.Fatal("create dispatcher without retired commission", err)
			}
			updated, err := fleet.UpdateDispatcher(ctx, created.ID, DispatcherInput{FullName: "Updated Profile Without Commission", Active: true})
			if err != nil || updated.FullName != "Updated Profile Without Commission" {
				t.Fatal("update dispatcher without retired commission", err, updated.FullName)
			}
			profileJSON, _ := json.Marshal(updated)
			if strings.Contains(string(profileJSON), "payPercentage") {
				t.Fatal("dispatcher profiles must not expose retired commission")
			}
			period, _ := dispatcherWeek("2099-01-05")
			legacy := FinancialDashboard{Dispatchers: []DispatcherFinancialBreakdown{}}
			if err = NewDashboardRepository(pool).loadDispatcherFinancials(ctx, FinancialDashboardQuery{DateFrom: &period}, &legacy); err != nil || len(legacy.Dispatchers) != 1 || legacy.Dispatchers[0].Pay == nil || *legacy.Dispatchers[0].Pay != 85 {
				t.Fatal("financial report must share Gross Board commissions", err, legacy.Dispatchers)
			}
			input := DispatcherPayInput{DriverID: driver, HomeDays: 1, Note: "Reviewed home day"}
			if err = repo.SaveInput(ctx, "2099-01-05", input, "test"); err != nil {
				t.Fatal(err)
			}
			if err = repo.SaveInput(ctx, "2099-01-05", input, "test"); !errors.Is(err, ErrDispatcherPayConflict) {
				t.Fatal("stale home days accepted", err)
			}
			report = get()
			if report.Statements[0].Pay != 0 || report.Statements[0].Rows[0].SelectedTier != 9000 {
				t.Fatal("home-day reduction not preserved", report)
			}
			s := report.Statements[0]
			change := DispatcherStatementChange{DispatcherID: dispatcher, Revision: s.Revision, Version: s.Version}
			exec(`UPDATE gross_board_entries SET entered_original_rate=10000,entered_miles=4000 WHERE driver_id=$1`, driver)
			if err = repo.ChangeStatement(ctx, "2099-01-05", change, "test", false); !errors.Is(err, ErrDispatcherPayConflict) {
				t.Fatal("stale board preview finalized", err)
			}
			report = get()
			s = report.Statements[0]
			change.Revision = s.Revision
			if err = repo.ChangeStatement(ctx, "2099-01-05", change, finalizer, false); err != nil {
				t.Fatal(err)
			}
			frozen := get()
			if !frozen.Statements[0].Finalized || frozen.Statements[0].SourceChanged {
				t.Fatal("invalid snapshot", frozen)
			}
			if frozen.Statements[0].FinalizedBy != "Payroll Reviewer" {
				t.Fatal("missing finalizer attribution", frozen.Statements[0].FinalizedBy)
			}
			// Simulate a snapshot saved before credited mileage was introduced.
			exec(`UPDATE dispatcher_pay_statements SET report=jsonb_set(report,'{rows,0}',(report->'rows'->0)-'creditedMiles')`)
			if get().Statements[0].SourceChanged {
				t.Fatal("display-only mileage must not invalidate an older financial snapshot")
			}
			input.Version = 1
			if err = repo.SaveInput(ctx, "2099-01-05", input, "test"); err == nil {
				t.Fatal("finalized home days changed")
			}
			exec(`UPDATE gross_board_entries SET entered_original_rate=11000,entered_miles=4400 WHERE driver_id=$1`, driver)
			frozen = get()
			if !frozen.Statements[0].SourceChanged || frozen.Statements[0].Pay != s.Pay {
				t.Fatal("snapshot did not freeze", frozen)
			}
			versions, err = repo.Rules(ctx)
			if err != nil {
				t.Fatal(err)
			}
			latest := int64(0)
			for _, v := range versions {
				latest = max(latest, v.ID)
			}
			future := workbookCommissionRules()
			future.Tiers[2].Brackets[0].Percent = 3
			if err = repo.SaveRules(ctx, CommissionRuleChange{ExpectedRevision: latest, EffectiveWeek: "2099-01-12", Rules: future}, "test"); err != nil {
				t.Fatal(err)
			}
			next, err := repo.Get(ctx, "2099-01-12")
			if err != nil || next.Rule == nil || next.Rule.Rules.Tiers[2].Brackets[0].Percent != 3 {
				t.Fatal("future rules missing", err, next)
			}
			if get().Rule.Rules.Tiers[2].Brackets[0].Percent != 1.25 {
				t.Fatal("future rules affected earlier week")
			}
			change.Version = frozen.Statements[0].Version
			change.Reason = "Review corrected board"
			if err = repo.ChangeStatement(ctx, "2099-01-05", change, "test", true); err != nil {
				t.Fatal(err)
			}
			reopened := get()
			if reopened.Statements[0].Finalized || reopened.Statements[0].Pay != 137.5 {
				t.Fatal("reopen did not calculate", reopened)
			}
			if reopened.Statements[0].FinalizedBy != "" || reopened.Statements[0].FinalizedAt != nil {
				t.Fatal("reopened statement retained finalization metadata")
			}
			exec(`INSERT INTO gross_board_entries(driver_id,service_date,day_status) VALUES($1,'2099-01-06','HOME')`, driver)
			exec(`INSERT INTO gross_board_extra_entries(driver_id,service_date,slot,day_status) VALUES($1,'2099-01-06',1,'HOME')`, driver)
			if err = repo.SaveInput(ctx, "2099-01-05", DispatcherPayInput{DriverID: driver, Version: 1, HomeDays: 7, AutoHomeDays: true}, "test"); err != nil {
				t.Fatal(err)
			}
			automaticRow := get().Statements[0].Rows[0]
			if !automaticRow.AutoHomeDays || automaticRow.HomeDays != 1 || automaticRow.BoardHomeDays != 1 {
				t.Fatal("reset must derive unique board days, not caller-supplied days", automaticRow)
			}
			exec(`DELETE FROM gross_board_entries WHERE driver_id=$1 AND day_status='HOME'`, driver)
			exec(`DELETE FROM gross_board_extra_entries WHERE driver_id=$1 AND day_status='HOME'`, driver)
			if get().Statements[0].Rows[0].HomeDays != 0 {
				t.Fatal("automatic days did not follow later Gross Board changes")
			}
			// Audit and rules are immutable even as the application owner.
			if _, err = pool.Exec(ctx, `UPDATE dispatcher_commission_rules SET actor_id='changed'`); err == nil {
				t.Fatal("rules mutable")
			}
			if _, err = pool.Exec(ctx, `DELETE FROM dispatcher_pay_history`); err == nil {
				t.Fatal("audit mutable")
			}
			var count int
			if err = pool.QueryRow(ctx, `SELECT count(*) FROM payroll_cache_revisions WHERE table_name LIKE 'dispatcher_%'`).Scan(&count); err != nil || count < 4 {
				t.Fatal("cache registration missing", count, err)
			}
			raw, _ := json.Marshal(reopened)
			if !strings.Contains(string(raw), "creditedGross") {
				t.Fatal("missing public contract")
			}
		})
	}
}
