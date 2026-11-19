package repository

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestReportsClosedWeekDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		fixture, err := os.ReadFile("testdata/reports_closed_week.sql")
		if err != nil {
			t.Fatal(err)
		}
		_, err = admin.Exec(ctx, string(fixture))
		if err != nil {
			t.Fatal(err)
		}
		repo := NewReportsRepository(pool)
		read := func(view, from, to string) ReportsResult {
			t.Helper()
			period, err := ResolveReportsPeriod(from, to, "", "", time.Now())
			if err != nil {
				t.Fatal(err)
			}
			result, err := repo.Read(ctx, ReportsQuery{View: view, Period: period})
			if err != nil {
				t.Fatal(err)
			}
			return result
		}
		check := func(row ReportRow, want map[string]float64) {
			t.Helper()
			for key, value := range want {
				if got, ok := row.Metrics[key]; !ok || got != value {
					t.Errorf("%s %s got %v (%v), want %v; all %v", row.Name, key, got, ok, value, row.Metrics)
				}
			}
		}
		company := read(ReportCompany, "2026-10-12", "2026-10-18")
		if len(company.Current) != 1 {
			t.Fatalf("company: %+v", company.Current)
		}
		check(company.Current[0], map[string]float64{"gross": 119900, "miles": 400, "maintenance": 8000, "fuel": 20000, "tolls": 4000, "scales": 1400, "equipment": 6000, "completedLoads": 2, "earnedPay": 37500, "contribution": 43000})
		if crew := company.Current[0].AssignedDrivers; len(crew) != 1 || crew[0].Name != "Company" || crew[0].ID != "61000000-0000-0000-0000-000000000001" {
			t.Fatalf("missing dated driver identity: %+v", crew)
		}
		operator := read(ReportOperator, "2026-10-12", "2026-10-18")
		if len(operator.Current) != 1 {
			t.Fatalf("operator: %+v", operator.Current)
		}
		check(operator.Current[0], map[string]float64{"gross": 199900, "carrierFees": 34000, "carrierCosts": 5000, "contribution": 29000})
		performance := read(ReportDriver, "2026-10-12", "2026-10-18")
		if len(performance.Current) != 2 {
			t.Fatalf("driver: %+v", performance.Current)
		}
		check(performance.Current[0], map[string]float64{"pickupOnTime": 100, "pickupCoverage": 100, "deliveryOnTime": 0, "deliveryCoverage": 100})
		check(performance.Current[1], map[string]float64{"pickupCoverage": 0, "deliveryCoverage": 0})
		if _, ok := performance.Current[1].Metrics["pickupOnTime"]; ok {
			t.Error("unknown on-time rate displayed")
		}
		period := performance.Period
		filtered, err := repo.Read(ctx, ReportsQuery{View: ReportDriver, Period: period, Truck: "61000000-0000-0000-0000-000000000004", Dispatcher: "historical:Former Dispatcher"})
		if err != nil || len(filtered.Current) != 1 || filtered.Current[0].Name != "Company" {
			t.Fatal("dated filters", filtered, err)
		}
		dispatch := read(ReportDispatcher, "2026-10-12", "2026-10-18")
		if len(dispatch.Current) != 2 || dispatch.Current[1].Name != "Original Dispatcher" {
			t.Fatalf("transferred dispatch: %+v", dispatch.Current)
		}
		check(dispatch.Current[1], map[string]float64{"gross": 199900, "truckWeeks": 1, "grossPerTruckWeek": 199900})
		if _, ok := operator.Current[0].Metrics["netPayable"]; ok {
			t.Fatal("whole statement in P&L")
		}
		if _, err = admin.Exec(ctx, `INSERT INTO expenses(truck_id,driver_id,category,category_id,expense_date,amount,expense_type,payment_type,covered_by)
 SELECT '61000000-0000-0000-0000-000000000004'::uuid,'61000000-0000-0000-0000-000000000001'::uuid,name,id,'2026-10-13'::date,500,'Driver repair','Card','Driver' FROM expense_settings WHERE kind='category' AND name='Maintenance'
 UNION ALL SELECT NULL,'61000000-0000-0000-0000-000000000001'::uuid,name,id,'2026-10-13'::date,700,'Unassigned driver repair','Card','Driver' FROM expense_settings WHERE kind='category' AND name='Maintenance'`); err != nil {
			t.Fatal(err)
		}
		costs := read(ReportCompany, "2026-10-12", "2026-10-18")
		if len(costs.Current) != 1 {
			t.Fatal("excluded expense created a report group", costs.Current)
		}
		check(costs.Current[0], map[string]float64{"maintenance": 8000, "carrierCosts": 39400})
		for _, source := range costs.Current[0].Sources {
			if source.Label == "Driver repair" || source.Label == "Unassigned driver repair" {
				t.Fatal("driver charge exposed as company cost", source)
			}
		}
		company.Revision = costs.Revision
		// Repeat reads must reuse evidence without sharing mutable result objects.
		company.Current[0].Metrics["gross"] = -1
		check(read(ReportCompany, "2026-10-12", "2026-10-18").Current[0], map[string]float64{"gross": 119900})
		// Collaboration heartbeats must not evict unchanged financial snapshots.
		if _, err = admin.Exec(ctx, `UPDATE payroll_cache_revisions SET revision=revision+1 WHERE table_name IN ('board_presence','fleetscope_sync_state','maintenance_odometer_readings','payroll_statement_current')`); err != nil {
			t.Fatal(err)
		}
		if presence := read(ReportCompany, "2026-10-12", "2026-10-18"); presence.Revision != company.Revision {
			t.Fatal("board presence invalidated financial report")
		}
		if _, err = admin.Exec(ctx, `UPDATE payroll_cache_revisions SET revision=revision+1 WHERE table_name='maintenance_configuration'`); err != nil {
			t.Fatal(err)
		}
		if classification := read(ReportCompany, "2026-10-12", "2026-10-18"); classification.Revision == company.Revision {
			t.Fatal("maintenance classification did not invalidate report")
		}
		if _, err = admin.Exec(ctx, `UPDATE loads SET total_pay=1200 WHERE id=61001`); err != nil {
			t.Fatal(err)
		}
		check(read(ReportCompany, "2026-10-12", "2026-10-18").Current[0], map[string]float64{"gross": 119900})
		// Provider availability is intentionally outside payroll revision capture.
		if _, err = admin.Exec(ctx, `INSERT INTO load_source_availability(load_id,present) VALUES(61001,false)`); err != nil {
			t.Fatal(err)
		}
		check(read(ReportCompany, "2026-10-12", "2026-10-18").Current[0], map[string]float64{"gross": 119900, "miles": 400})
		if _, err = admin.Exec(ctx, `UPDATE load_source_availability SET present=true WHERE load_id=61001`); err != nil {
			t.Fatal(err)
		}
		check(read(ReportCompany, "2026-10-12", "2026-10-18").Current[0], map[string]float64{"gross": 119900})
		if _, err = admin.Exec(ctx, `DELETE FROM gross_board_entries WHERE load_number='MANUAL'`); err != nil {
			t.Fatal(err)
		}
		check(read(ReportCompany, "2026-10-12", "2026-10-18").Current[0], map[string]float64{"gross": 99900, "miles": 300, "earnedPay": 30000, "contribution": 30500})
		if _, err = admin.Exec(ctx, `UPDATE payroll_settlements SET finalized=false WHERE driver_id='61000000-0000-0000-0000-000000000001'`); err != nil {
			t.Fatal(err)
		}
		unfinalized := read(ReportCompany, "2026-10-12", "2026-10-18").Current[0]
		check(unfinalized, map[string]float64{"earnedPay": 22500, "contribution": 38000})
		if _, err = admin.Exec(ctx, `INSERT INTO loads(id,load_id,status,load_pay,total_pay,total_miles,driver_id,truck_id,delivery_time,raw_payload)
 VALUES(61999,'UNPLANNED','delivered',500,500,100,'61000000-0000-0000-0000-000000000001','61000000-0000-0000-0000-000000000004','2026-10-15 15:00Z','{}')`); err != nil {
			t.Fatal(err)
		}
		unplanned := read(ReportDriver, "2026-10-12", "2026-10-18")
		if len(unplanned.Current) != 2 {
			t.Fatal("load outside Gross Board entered report", unplanned.Current)
		}
		for _, view := range []string{ReportCompany, ReportOperator, ReportDispatcher, ReportDriver} {
			for _, row := range read(view, "2026-10-12", "2026-10-18").Current {
				for _, source := range row.Sources {
					if source.Kind == "Loads" && source.ID == "61999" {
						t.Fatalf("%s included a load absent from Gross Board", view)
					}
				}
			}
		}
	})
}

func TestReportsTariffProfitDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		fixture, err := os.ReadFile("testdata/reports_closed_week.sql")
		if err != nil {
			t.Fatal(err)
		}
		if _, err = admin.Exec(ctx, string(fixture)); err != nil {
			t.Fatal(err)
		}
		exec := func(sql string) {
			t.Helper()
			if _, err := admin.Exec(ctx, sql); err != nil {
				t.Fatal(err)
			}
		}
		repo := NewReportsRepository(pool)
		period, _ := ResolveReportsPeriod("2026-10-12", "2026-10-18", "", "", time.Now())
		read := func(view string, pay, profit float64) ReportRow {
			t.Helper()
			result, err := repo.Read(ctx, ReportsQuery{View: view, Period: period, CurrentOnly: true})
			if err != nil || len(result.Current) != 1 {
				t.Fatalf("report: %+v %v", result, err)
			}
			row := result.Current[0]
			if row.Metrics["earnedPay"] != pay || row.Metrics["contribution"] != profit {
				t.Fatalf("pay/profit: %+v", row.Metrics)
			}
			return row
		}
		exec(`UPDATE payroll_settlements SET finalized=false`)
		// Gross Board mileage overrides + manual entries, not provider miles.
		read(ReportCompany, 30000, 50500)
		owner := read(ReportOperator, 136000, 29000)
		if owner.Metrics["carrierFees"] != 34000 {
			t.Fatal(owner.Metrics)
		}
		if _, exists := owner.Metrics["netPayable"]; exists {
			t.Fatal("tariff estimate created a finalized settlement")
		}
		// Tariff and Gross Board edits invalidate the cache without finalization.
		exec(`UPDATE drivers SET pay_rate=1.25 WHERE full_name='Company Driver'; UPDATE drivers SET pay_rate=83.3333 WHERE full_name='Owner Driver'`)
		read(ReportCompany, 50000, 30500)
		read(ReportOperator, 141667, 23333)
		exec(`UPDATE gross_board_entries SET entered_miles=301.01 WHERE load_number='REPORT-1'`)
		read(ReportCompany, 50126, 30374)
		// Percentage payroll uses driver gross, not the higher displayed load gross.
		exec(`UPDATE drivers SET pay_type='gross_percentage',pay_rate=25 WHERE full_name='Company Driver'`)
		read(ReportCompany, 27500, 53000)
		// Finalized load fees override the current tariff and aren't added twice.
		exec(`UPDATE payroll_settlements SET finalized=true`)
		read(ReportCompany, 32500, 48000)
		read(ReportOperator, 136000, 29000)
		// Frozen manual rows match their native board identity as well.
		exec(`UPDATE payroll_settlements SET report=jsonb_set(report,'{loads}',report->'loads'||'[{"loadNumber":"MANUAL","sourceDate":"2026-10-14","sourceSlot":0,"fee":"90.00"}]'::jsonb) WHERE driver_id='61000000-0000-0000-0000-000000000001'`)
		read(ReportCompany, 39000, 41500)
		exec(`UPDATE payroll_settlements SET finalized=false WHERE driver_id='61000000-0000-0000-0000-000000000001'`)
		read(ReportCompany, 27500, 53000)
		// Genuine missing input stays unavailable, never silently becomes zero pay.
		exec(`UPDATE drivers SET pay_rate=0 WHERE full_name='Company Driver'`)
		result, err := repo.Read(ctx, ReportsQuery{View: ReportCompany, Period: period, CurrentOnly: true})
		if err != nil {
			t.Fatal(err)
		}
		if _, exists := result.Current[0].Metrics["contribution"]; exists {
			t.Fatal("missing tariff became zero pay")
		}
	})
}

func TestReportsWeeklyDriversDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		_, err := admin.Exec(ctx, `INSERT INTO drivers(id,full_name,normalized_name,hire_date,pay_type,pay_rate) VALUES
 ('62000000-0000-0000-0000-000000000001','First Driver','first driver','2026-01-01','cpm',0.75),
 ('62000000-0000-0000-0000-000000000002','Second Driver','second driver','2026-01-01','cpm',0.75),
 ('62000000-0000-0000-0000-000000000003','Future Driver','future driver','2026-01-01','cpm',0.75),
 ('62000000-0000-0000-0000-000000000004','Team Driver','team driver','2026-01-01','cpm',0.75);
 INSERT INTO trucks(id,unit_number) VALUES ('62000000-0000-0000-0000-000000000005','WEEK-CREW');
 INSERT INTO truck_driver_assignments(truck_id,driver_id,assigned_at,unassigned_at) VALUES
 ('62000000-0000-0000-0000-000000000005','62000000-0000-0000-0000-000000000001','2026-10-12 04:00Z','2026-10-15 04:00Z'),
 ('62000000-0000-0000-0000-000000000005','62000000-0000-0000-0000-000000000002','2026-10-15 04:00Z','2026-10-19 04:00Z'),
 ('62000000-0000-0000-0000-000000000005','62000000-0000-0000-0000-000000000003','2026-10-19 04:00Z',NULL);
 INSERT INTO driver_teams(main_driver_id,team_driver_id,start_week,end_week) VALUES
 ('62000000-0000-0000-0000-000000000001','62000000-0000-0000-0000-000000000004','2026-10-12','2026-10-19');`)
		if err != nil {
			t.Fatal(err)
		}
		tx, err := pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		s := reportRead{tx: tx}
		for _, week := range []struct {
			from, to string
			names    []string
		}{
			{"2026-10-12", "2026-10-18", []string{"First Driver", "Second Driver", "Team Driver"}},
			{"2026-10-19", "2026-10-25", []string{"Future Driver"}},
		} {
			groups := reportGroups{"62000000-0000-0000-0000-000000000005": &reportGroup{}}
			if err := s.addWeeklyDrivers(ctx, groups, week.from, week.to); err != nil {
				t.Fatal(err)
			}
			crew := groups["62000000-0000-0000-0000-000000000005"].AssignedDrivers
			if len(crew) != len(week.names) {
				t.Fatalf("%s crew: %+v", week.from, crew)
			}
			for i, name := range week.names {
				if crew[i].Name != name {
					t.Fatalf("%s crew: %+v", week.from, crew)
				}
			}
		}
	})
}

func TestReportsRecurringChargesDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		fixture, err := os.ReadFile("testdata/reports_closed_week.sql")
		if err != nil {
			t.Fatal(err)
		}
		if _, err = admin.Exec(ctx, string(fixture)); err != nil {
			t.Fatal(err)
		}
		if _, err = admin.Exec(ctx, `UPDATE payroll_settlements SET report=jsonb_set(jsonb_set(jsonb_set(report,
 '{autoCharges}',report->'autoCharges'||'[
 {"name":"Insurance","source":"truck_charge:61000000-0000-0000-0000-000000000005:insurance","amount":"-125.00"},
 {"name":"Waived","source":"truck_charge:61000000-0000-0000-0000-000000000005:waived","amount":"0.00"},
 {"name":"Credit","source":"truck_charge:61000000-0000-0000-0000-000000000005:credit","amount":"50.00"}]'::jsonb),
 '{edits,sourceCharges}','[
 {"source":"truck_charge:61000000-0000-0000-0000-000000000005:insurance","base":"-200.00","due":"-450.00","amount":"-125.00"},
 {"source":"truck_charge:61000000-0000-0000-0000-000000000005:waived","amount":"0.00"}]'),
 '{edits,generatedCharges}','[
 {"kind":"installment","scheduleId":"loan","amount":"-40.00"},
 {"kind":"recurring","scheduleId":"legacy-fee","name":"Legacy fee","amount":"-25.00"}]')
 WHERE driver_id='61000000-0000-0000-0000-000000000002'`); err != nil {
			t.Fatal(err)
		}
		period, _ := ResolveReportsPeriod("2026-10-12", "2026-10-18", "", "", time.Now())
		repo := NewReportsRepository(pool)
		result, err := repo.Read(ctx, ReportsQuery{View: ReportOperator, Period: period, CurrentOnly: true})
		if err != nil || len(result.Current) != 1 {
			t.Fatal(result, err)
		}
		row := result.Current[0]
		if row.Metrics["recurringCharges"] != 15000 || row.Metrics["contribution"] != 44000 {
			t.Fatal("recurring deductions or profit do not reconcile", row.Metrics)
		}
		var evidence float64
		for _, source := range row.Sources {
			evidence += source.Values["recurringCharges"]
		}
		if evidence != row.Metrics["recurringCharges"] {
			t.Fatal("recurring source evidence does not reconcile", evidence)
		}
		if _, err = admin.Exec(ctx, `UPDATE payroll_settlements SET finalized=false WHERE driver_id='61000000-0000-0000-0000-000000000002'`); err != nil {
			t.Fatal(err)
		}
		result, err = repo.Read(ctx, ReportsQuery{View: ReportOperator, Period: period, CurrentOnly: true})
		if err != nil || result.Current[0].Metrics["recurringCharges"] != 0 {
			t.Fatal("unfinalized recurring charges counted as income", result, err)
		}
	})
}

func TestReportsGrossBoardCohortDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		fixture, err := os.ReadFile("testdata/reports_closed_week.sql")
		if err != nil {
			t.Fatal(err)
		}
		exec := func(sql string) {
			t.Helper()
			if _, err := admin.Exec(ctx, sql); err != nil {
				t.Fatal(err)
			}
		}
		exec(string(fixture))
		exec(`INSERT INTO loads(id,load_id,status,load_pay,total_pay,total_miles,delivery_time,raw_payload)
 VALUES(61999,'BOARD-ONLY','delivered',600,600,150,'2026-10-15 15:00Z','{}')`)
		read := func() []reportLoad {
			t.Helper()
			tx, err := pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx)
			loads, err := readReportLoads(ctx, tx, ReportDriver, "2026-10-12", "2026-10-18")
			if err != nil {
				t.Fatal(err)
			}
			var found []reportLoad
			for _, l := range loads {
				if l.parent == "61999" {
					found = append(found, l)
				}
			}
			return found
		}
		if rows := read(); len(rows) != 0 {
			t.Fatal("unlinked source included", rows)
		}
		// Board service date may be outside the revenue's delivery week. Entered
		// amounts are deliberately different: linked Loads remains authoritative.
		exec(`INSERT INTO gross_board_entries(driver_id,service_date,load_number,load_record_id,entered_original_rate,entered_miles)
 VALUES('61000000-0000-0000-0000-000000000001','2026-10-05','BOARD-ONLY',61999,999,999)`)
		rows := read()
		if len(rows) != 1 || rows[0].gross == nil || *rows[0].gross != 99900 || rows[0].miles == nil || *rows[0].miles != 999 || rows[0].serviceDate != "2026-10-05" {
			t.Fatal("linked source/date mismatch", rows)
		}
		// Number-only entries still resolve only a unique source.
		exec(`UPDATE gross_board_entries SET load_record_id=NULL WHERE load_record_id=61999`)
		if rows := read(); len(rows) != 1 {
			t.Fatal("unique number link lost", rows)
		}
		exec(`UPDATE gross_board_entries SET load_record_id=61999,load_trip_id=51,entered_original_rate=NULL,entered_miles=NULL WHERE load_number='BOARD-ONLY';
 UPDATE loads SET split_expected=true,raw_payload='{"trips":[{"id":51,"driver__full_name":"Company Driver","total_load_pay":200,"mile":40,"empty_mile":10},{"id":52,"driver__full_name":"Owner Driver","total_load_pay":400,"mile":80,"empty_mile":20}]}' WHERE id=61999;
 INSERT INTO gross_board_extra_entries(driver_id,service_date,slot,load_number,load_record_id,load_trip_id)
 VALUES('61000000-0000-0000-0000-000000000001','2026-10-05',1,'BOARD-ONLY',61999,51)`)
		rows = read()
		if len(rows) != 1 || rows[0].trip != "51" || rows[0].state != "confirmed" || rows[0].gross == nil || *rows[0].gross != 20000 || rows[0].miles == nil || *rows[0].miles != 50 {
			t.Fatal("unselected sibling or duplicate trip counted", rows)
		}
		exec(`UPDATE gross_board_extra_entries SET deleted=true,load_trip_id=NULL WHERE load_record_id=61999`)
		for _, trip := range []string{"999", "NULL", "51"} {
			exec(`UPDATE gross_board_entries SET load_trip_id=` + trip + ` WHERE load_record_id=61999`)
			if trip == "51" {
				exec(`UPDATE loads SET total_pay=601 WHERE id=61999`)
			}
			rows = read()
			if len(rows) != 1 || rows[0].state != "review" || rows[0].gross != nil || rows[0].miles != nil {
				t.Fatal("unverified selected allocation lost or given amounts", trip, rows)
			}
		}
		exec(`DELETE FROM gross_board_entries WHERE load_record_id=61999`)
		if rows := read(); len(rows) != 0 {
			t.Fatal("deleted board links included", rows)
		}
	})
}

func TestReportsCutoff(t *testing.T) {
	now := time.Date(2026, 11, 9, 4, 59, 59, 0, time.UTC)
	period, err := ResolveReportsPeriod("", "", "", "", now)
	if err != nil || period.DateFrom != "2026-10-26" || period.DateTo != "2026-11-01" {
		t.Fatal(period, err)
	}
	period, err = ResolveReportsPeriod("", "", "", "", now.Add(time.Second))
	if err != nil || period.DateFrom != "2026-11-02" || period.CompareFrom != "2026-10-26" {
		t.Fatal(period, err)
	}
}

func TestReportsFleetScaleDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		for _, file := range []string{"reports_closed_week.sql", "reports_scale.sql"} {
			body, err := os.ReadFile("testdata/" + file)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = admin.Exec(ctx, string(body)); err != nil {
				t.Fatal(err)
			}
		}
		period, err := ResolveReportsPeriod("2026-10-12", "2026-10-18", "", "", time.Now())
		if err != nil {
			t.Fatal(err)
		}
		repo := NewReportsRepository(pool)
		for _, view := range []string{ReportCompany, ReportOperator, ReportDispatcher, ReportDriver} {
			q := ReportsQuery{View: view, Period: period, CurrentOnly: true}
			started := time.Now()
			cold, err := repo.Read(ctx, q)
			if err != nil {
				t.Fatal(err)
			}
			coldTime := time.Since(started)
			started = time.Now()
			warm, err := repo.Read(ctx, q)
			if err != nil {
				t.Fatal(err)
			}
			warmTime := time.Since(started)
			if len(cold.Current) != len(warm.Current) || len(cold.Comparison) != 0 {
				t.Fatal("cache changed row coverage")
			}
			t.Logf("%s: %d rows; cold %s; cached %s", view, len(cold.Current), coldTime, warmTime)
			if view == ReportCompany && len(cold.Current) != 121 {
				t.Fatal("scale fixture lost trucks")
			}
		}
	})
}
