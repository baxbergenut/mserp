package repository

import (
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

func TestDriverTeamsDatabase(t *testing.T) {
	dsn := os.Getenv("MSERP_DRIVER_PAY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set MSERP_DRIVER_PAY_TEST_DATABASE_URL to a disposable local _test database")
	}
	u, err := url.Parse(dsn)
	if err != nil || !strings.Contains(u.Path, "_test") || (u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1") {
		t.Fatal("use a disposable local _test database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal("test database unavailable")
	}
	defer admin.Close(ctx)
	init, err := os.ReadFile("../../sql/init.sql")
	if err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile("../../sql/078_driver_teams.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"fresh", "migration"} {
		t.Run(mode, func(t *testing.T) {
			schema := fmt.Sprintf("driver_teams_test_%d", time.Now().UnixNano())
			quoted := pgx.Identifier{schema}.Sanitize()
			if _, err = admin.Exec(ctx, `CREATE SCHEMA `+quoted+`;SET search_path TO `+quoted+`,public`); err != nil {
				t.Fatal(err)
			}
			defer func() {
				_, _ = admin.Exec(context.Background(), `SET search_path TO public;DROP SCHEMA `+quoted+` CASCADE`)
			}()
			source := strings.ReplaceAll(string(init), "\r\n", "\n")
			if mode == "migration" {
				body := strings.ReplaceAll(string(migration), "\r\n", "\n")
				if !strings.Contains(source, body) {
					t.Fatal("init must contain the exact team migration")
				}
				source = withoutBoardLiveMigration(t, withoutFleetScopeSyncMigration(t, withoutTruckRecurringMigration(t, strings.Replace(source, body, "", 1))))
			}
			if _, err = admin.Exec(ctx, source); err != nil {
				t.Fatal(err)
			}
			if mode == "migration" {
				if _, err = admin.Exec(ctx, string(migration)); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "migration" {
				applyTruckRecurringMigration(t, ctx, admin)
				applyFleetScopeSyncMigration(t, ctx, admin)
				applyBoardLiveMigration(t, ctx, admin)
			}
			for _, table := range []string{"driver_teams", "driver_team_events"} {
				var owner string
				if err = admin.QueryRow(ctx, `SELECT tableowner FROM pg_tables WHERE schemaname=$1 AND tablename=$2`, schema, table).Scan(&owner); err != nil || owner != "mserp_app" {
					t.Fatal("team table runtime ownership", table, owner, err)
				}
			}
			if _, err = admin.Exec(ctx, `GRANT USAGE ON SCHEMA `+quoted+` TO mserp_app;GRANT SELECT,INSERT,UPDATE,DELETE ON ALL TABLES IN SCHEMA `+quoted+` TO mserp_app;GRANT USAGE,SELECT ON ALL SEQUENCES IN SCHEMA `+quoted+` TO mserp_app`); err != nil {
				t.Fatal(err)
			}
			cfg, err := pgxpool.ParseConfig(dsn)
			if err != nil {
				t.Fatal("invalid test config")
			}
			cfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
			cfg.ConnConfig.RuntimeParams["role"] = "mserp_app"
			pool, err := pgxpool.NewWithConfig(ctx, cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer pool.Close()
			verifyDriverTeams(t, ctx, pool)
			verifyTeamDriverSetup(t, ctx, pool)
		})
	}
}

// Legacy fixtures remove older tables/migrations from init.sql. Defer the team
// migration until those prerequisite tables have been rebuilt.
func withoutDriverTeams(t *testing.T, source string) string {
	t.Helper()
	body, err := os.ReadFile("../../sql/078_driver_teams.sql")
	if err != nil {
		t.Fatal(err)
	}
	source = withoutFleetScopeSyncMigration(t, withoutTruckRecurringMigration(t, source))
	return strings.Replace(source, strings.ReplaceAll(string(body), "\r\n", "\n"), "", 1)
}

// Migration 103 installs capture triggers on driver_teams. Legacy fixtures must
// defer it until migration 078 has recreated that table, just as production does.
func withoutFleetScopeSyncMigration(t *testing.T, source string) string {
	t.Helper()
	body, err := os.ReadFile("../../sql/103_fleetscope_fleet_sync.sql")
	if err != nil {
		t.Fatal(err)
	}
	normalized := strings.ReplaceAll(string(body), "\r\n", "\n")
	if !strings.Contains(source, "CREATE TABLE fleetscope_sync_state") {
		return source // Some older fixtures end before the integration existed.
	}
	if !strings.Contains(source, normalized) {
		t.Fatal("init must contain the exact FleetScope migration")
	}
	applicant, err := os.ReadFile("../../sql/134_fleetscope_applicants.sql")
	if err != nil {
		t.Fatal(err)
	}
	source = strings.Replace(source, strings.ReplaceAll(string(applicant), "\r\n", "\n"), "", 1)
	return strings.Replace(source, normalized, "", 1)
}

func applyFleetScopeSyncMigration(t *testing.T, ctx context.Context, conn *pgx.Conn) {
	defer applyExpenseApplicantTestSchema(t, ctx, conn)
	t.Helper()
	body, err := os.ReadFile("../../sql/103_fleetscope_fleet_sync.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = conn.Exec(ctx, string(body)); err != nil {
		t.Fatal(err)
	}
}
func applyDriverTeams(t *testing.T, ctx context.Context, conn *pgx.Conn) {
	defer applyTruckRecurringMigration(t, ctx, conn)
	t.Helper()
	body, err := os.ReadFile("../../sql/078_driver_teams.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = conn.Exec(ctx, string(body)); err != nil {
		t.Fatal(err)
	}
}

func verifyDriverTeams(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	id := func(sql string, args ...any) string {
		t.Helper()
		var out string
		if err := pool.QueryRow(ctx, sql, args...).Scan(&out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	fleet := NewFleetRepository(pool)
	pay := NewDriverPayRepository(pool)
	actor := id(`INSERT INTO app_users(username,password_hash) VALUES('Team reviewer',crypt('test-only',gen_salt('bf'))) RETURNING id::text`)
	main, err := fleet.CreateDriver(ctx, DriverInput{FullName: "Main Example", PayType: "gross_percentage", PayRate: 90, IsOwnerOperator: true, Active: true, EscrowAmount: "100", AssignmentWeek: "2026-09-21"})
	if err != nil {
		t.Fatal(err)
	}
	team, err := fleet.CreateDriver(ctx, DriverInput{FullName: "Team Example", PayType: "cpm", PayRate: 0.5, Active: true, EscrowAmount: "2500", AssignmentWeek: "2026-09-21"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := fleet.CreateDriver(ctx, DriverInput{FullName: "Other Main", PayType: "cpm", PayRate: 0.7, Active: true, EscrowAmount: "0", AssignmentWeek: "2026-09-21"})
	if err != nil {
		t.Fatal(err)
	}
	// Existing prior solo work must retain its separate statement.
	exec(`INSERT INTO gross_board_entries(driver_id,service_date,load_number,driver_rate,entered_original_rate,entered_miles) VALUES($1,'2026-09-21','SOLO',100,100,100)`, team.ID)
	week, _ := time.Parse(time.DateOnly, "2026-09-28")
	if _, err = pay.Save(ctx, DriverPayEdits{DriverID: main.ID, WeekStart: "2026-09-28", Comments: map[string]string{}, Adjustments: []DriverPayAdjustment{}}); err != nil {
		t.Fatal("save open main statement", err)
	}
	pairing := DriverTeamInput{TeamDriverID: team.ID, WeekStart: "2026-09-28", Version: 0}
	value, err := fleet.SetDriverTeam(ctx, main.ID, pairing, "")
	if err != nil {
		t.Fatal(err)
	}
	if value.Version != 1 || len(value.Periods) != 1 {
		t.Fatal("missing versioned team", value)
	}
	if _, err = fleet.SetDriverTeam(ctx, main.ID, pairing, ""); !errors.Is(err, ErrDriverTeamConflict) {
		t.Fatal("stale team accepted", err)
	}
	if _, err = fleet.SetDriverTeam(ctx, other.ID, DriverTeamInput{TeamDriverID: team.ID, WeekStart: "2026-09-28"}, ""); err == nil {
		t.Fatal("duplicate membership accepted")
	}
	if _, err = fleet.SetDriverTeam(ctx, team.ID, DriverTeamInput{TeamDriverID: main.ID, WeekStart: "2026-09-28", Version: 1}, ""); err == nil {
		t.Fatal("cyclic team accepted")
	}
	got, err := fleet.GetDriver(ctx, team.ID)
	if err != nil || got.TeamRole != "team" || got.TeamPartnerID == nil || *got.TeamPartnerID != main.ID {
		t.Fatal("directory team identity", got, err)
	}
	// Guard direct/older clients too: a team driver cannot be independently dispatched.
	if _, err = pool.Exec(ctx, `INSERT INTO gross_board_entries(driver_id,service_date,load_number) VALUES($1,'2026-09-28','DUPLICATE')`, team.ID); err == nil {
		t.Fatal("team load accepted")
	}
	owner := id(`INSERT INTO investors(full_name,driver_id) VALUES('Main owner',$1) RETURNING id`, main.ID)
	truck := id(`INSERT INTO trucks(unit_number,owner_id) VALUES('TEAM-01',$1) RETURNING id`, owner)
	exec(`UPDATE truck_ownership_history SET start_known=false WHERE truck_id=$1`, truck)
	if _, err = pool.Exec(ctx, `INSERT INTO truck_driver_assignments(truck_id,driver_id,assigned_at) VALUES($1,$2,'2026-09-28 04:00+00')`, truck, team.ID); err == nil {
		t.Fatal("team truck accepted")
	}
	exec(`INSERT INTO truck_driver_assignments(truck_id,driver_id,assigned_at) VALUES($1,$2,'2026-09-28 04:00+00')`, truck, main.ID)
	// Confirmed identity and merchant-local boundary fixtures, including DEF,
	// credits, staging, unlinked rows and nonfuel products.
	exec(`WITH fixtures(label,stamp,amount,env,linked,category,kind) AS (VALUES
 ('diesel','2026-09-29 03:00+00',100.10,'production',true,'diesel','fuel'),
 ('main','2026-09-29 03:00+00',25.00,'production',true,'diesel','fuel'),
 ('credit','2026-09-30 16:00+00',-10.00,'production',true,'diesel','fuel'),
 ('def','2026-09-29 12:00+00',20.00,'production',true,'def','fuel'),
 ('sunday','2026-10-05 03:59+00',0.20,'production',true,'diesel','fuel'),
 ('before','2026-09-28 03:59+00',999.00,'production',true,'diesel','fuel'),
 ('after','2026-10-05 04:00+00',999.00,'production',true,'diesel','fuel'),
 ('staging','2026-09-29 12:00+00',999.00,'staging',true,'diesel','fuel'),
 ('unlinked','2026-09-29 12:00+00',999.00,'production',false,'diesel','fuel'),
 ('product','2026-09-29 12:00+00',999.00,'production',true,'other','product')
 ), inserted AS (
 INSERT INTO fuel_transactions(relay_environment,relay_transaction_id,driver_id,relay_driver_id,purchased_at,total_amount_paid,total_retail_price,total_amount_saved,is_direct_bill,currency_code,merchant_id,merchant_name,merchant_number,location_id,location_name,merchant_location_id,address,city,state,postal_code,latitude,longitude,timezone,raw_payload)
 SELECT env,label,CASE WHEN linked THEN CASE WHEN label='main' THEN $2::uuid ELSE $1::uuid END END,CASE WHEN label='main' THEN 'main-relay' ELSE 'team-relay' END,stamp::timestamptz,amount,amount,0,false,'USD','','','','','','','','','','',0,0,'US/Eastern','{}' FROM fixtures
 RETURNING id,relay_transaction_id
 ) INSERT INTO fuel_transaction_items(fuel_transaction_id,line_number,item_kind,category,total_amount_paid)
 SELECT i.id,0,f.kind,f.category,f.amount FROM inserted i JOIN fixtures f ON f.label=i.relay_transaction_id`, team.ID, main.ID)
	exec(`UPDATE fuel_transactions SET prompts='[{"label":"Truck #","value":"TEAM-01"}]'`)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	allocations, err := readTruckCostAllocations(ctx, tx, "2026-09-28")
	if err != nil {
		t.Fatal(err)
	}
	if allocations[truck] == nil || allocations[truck].driverFuel[main.ID] != 13530 || allocations[truck].driverFuel[team.ID] != 0 {
		t.Fatal("investor allocation lost team recipient")
	}
	costReport := DriverPayWeek{Drivers: []DriverPayDriver{{ID: main.ID, FuelTotal: "135.30", TollTotal: "0.00"}}}
	routeTruckDriverCosts(&costReport, map[string]*truckPayGroup{truck: {}}, allocations)
	if costReport.Drivers[0].FuelTotal != "0.00" {
		t.Fatal("investor fuel charged twice")
	}
	_ = tx.Rollback(ctx)
	expense := id(`INSERT INTO expenses(category,category_id,expense_type,expense_date,amount,covered_by,driver_id,charge_driver_id)
 VALUES('Other',(SELECT id FROM expense_settings WHERE kind='category' AND name='Other'),'Team expense','2026-09-29',50,'Driver',$1,$1) RETURNING id`, team.ID)
	schedule := id(`INSERT INTO driver_charge_schedules(driver_id,kind,name,direction,start_week,eligibility,total,installment_count)
 VALUES($1,'installment','Prior personal balance','charge','2026-09-28','calendar',60,2) RETURNING id`, team.ID)
	exec(`INSERT INTO driver_charge_phases(schedule_id,week_start,amount) VALUES($1,'2026-09-28',30)`, schedule)
	chargeType := id(`INSERT INTO driver_charge_types(name,direction,amount,amounts,eligibility) VALUES('Team test fee','charge',10,ARRAY[10]::numeric[],'calendar') RETURNING id`)
	if _, err = pool.Exec(ctx, `INSERT INTO driver_charge_schedules(driver_id,type_id,kind,name,direction,start_week,eligibility) VALUES($1,$2,'recurring','Not allowed','charge','2026-09-28','calendar')`, team.ID, chargeType); err == nil {
		t.Fatal("team recurring accepted")
	}
	report, err := pay.Get(ctx, week)
	if err != nil {
		t.Fatal(err)
	}
	var card DriverPayDriver
	for _, d := range report.Drivers {
		if d.ID == team.ID {
			t.Fatal("separate team statement")
		}
		if d.ID == main.ID {
			card = d
		}
	}
	if card.ID == "" || card.FuelTotal != "135.30" || len(card.TeamDrivers) != 1 {
		t.Fatal("wrong combined fuel", card)
	}
	if len(card.Edits.GeneratedCharges) != 1 || card.Edits.GeneratedCharges[0].ScheduleID != schedule {
		t.Fatal("lost installment")
	}
	for _, x := range card.Edits.ExpenseDeductions {
		if x.Source == "escrow" && x.Total != "100.00" {
			t.Fatal("double escrow collected")
		}
	}
	edits := card.Edits
	fuel := "-50.00"
	edits.FuelOverride = &fuel
	for i := range edits.ExpenseDeductions {
		if edits.ExpenseDeductions[i].ExpenseID == expense {
			edits.ExpenseDeductions[i].Apply = true
			edits.ExpenseDeductions[i].Amount = "20.00"
		}
	}
	edits.GeneratedCharges[0].Amount = "-15.00"
	saved, err := pay.Save(ctx, edits)
	if err != nil {
		t.Fatal("main save", err)
	}
	if id(`SELECT amount::text FROM expense_payments WHERE expense_id=$1 AND week_start='2026-09-28'`, expense) != "20.00" {
		t.Fatal("expense not saved on its original ledger")
	}
	if id(`SELECT count(*)::text FROM driver_escrow_payments p JOIN driver_escrows e ON e.id=p.escrow_id WHERE e.driver_id=$1`, team.ID) != "0" {
		t.Fatal("team escrow collected")
	}
	if _, err = pay.Save(ctx, DriverPayEdits{DriverID: team.ID, WeekStart: "2026-09-28"}); err == nil {
		t.Fatal("separate team payroll saved")
	}
	// A midweek termination is a review notice, not a reason to lose that
	// week's combined deductions or prevent its legitimate finalization.
	exec(`UPDATE drivers SET active=false,termination_date='2026-10-01' WHERE id=$1`, team.ID)
	report, err = pay.Get(ctx, week)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range report.Drivers {
		if d.ID == main.ID && (d.TeamNotice == "" || len(d.Issues) > 0) {
			t.Fatal("termination should retain a payable shared statement", d)
		}
	}
	finalized, err := pay.Settle(ctx, week, main.ID, report.Revision, actor, "", false, false)
	if err != nil {
		t.Fatal("finalization", err)
	}
	var frozen DriverPayDriver
	for _, d := range finalized.Drivers {
		if d.ID == main.ID {
			frozen = d
		}
	}
	if frozen.Settlement == nil || !frozen.Settlement.Finalized {
		t.Fatal("main not frozen")
	}
	if _, err = pool.Exec(ctx, `UPDATE expense_payments SET amount=21 WHERE expense_id=$1`, expense); err == nil {
		t.Fatal("finalized team expense changed")
	}
	if _, err = fleet.SetDriverTeam(ctx, main.ID, DriverTeamInput{WeekStart: "2026-09-28", Version: value.Version}, ""); err == nil {
		t.Fatal("finalized team changed")
	}
	exec(`UPDATE fuel_transaction_items SET total_amount_paid=120.10 WHERE fuel_transaction_id IN(SELECT id FROM fuel_transactions WHERE relay_transaction_id='diesel')`)
	report, err = pay.Get(ctx, week)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range report.Drivers {
		if d.ID == main.ID && d.FuelTotal != frozen.FuelTotal {
			t.Fatal("frozen fuel changed")
		}
	}
	if _, err = pay.Settle(ctx, week, main.ID, report.Revision, actor, "Reviewed team deduction", true, false); err != nil {
		t.Fatal("reopen", err)
	}
	if _, err = pay.Save(ctx, saved); err == nil {
		t.Fatal("stale cross-source save accepted")
	}
	// End on the next Monday. Earlier weeks and unpaid main fuel retain their destination.
	if _, err = fleet.SetDriverTeam(ctx, main.ID, DriverTeamInput{WeekStart: "2026-10-05", Version: value.Version}, ""); err != nil {
		t.Fatal("end team", err)
	}
	next, _ := time.Parse(time.DateOnly, "2026-10-05")
	report, err = pay.Get(ctx, next)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range report.Drivers {
		if d.ID == main.ID && (len(d.TeamDrivers) != 0 || d.FuelTotal != "0.00" || d.Edits.Costs.FuelCarry != "85.30") {
			t.Fatal("ended team/carry", d)
		}
	}
	prior, _ := time.Parse(time.DateOnly, "2026-09-21")
	historical, err := pay.Get(ctx, prior)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, d := range historical.Drivers {
		found = found || d.ID == team.ID
	}
	if !found {
		t.Fatal("prior solo history disappeared")
	}
	if id(`SELECT count(*)::text FROM driver_team_events`) != "2" {
		t.Fatal("missing team audit")
	}
	if id(`SELECT driver_id::text FROM fuel_transactions WHERE relay_transaction_id='diesel'`) != team.ID {
		t.Fatal("Relay identity rewritten")
	}
}
