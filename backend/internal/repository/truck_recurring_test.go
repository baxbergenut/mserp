package repository

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"strings"
	"testing"
	"time"
)

func withoutTruckRecurringMigration(t *testing.T, source string) string {
	t.Helper()
	b, e := os.ReadFile("../../sql/081_truck_recurring_charges.sql")
	if e != nil {
		t.Fatal(e)
	}
	return strings.Replace(source, strings.ReplaceAll(string(b), "\r\n", "\n"), "", 1)
}
func applyTruckRecurringMigration(t *testing.T, ctx context.Context, conn *pgx.Conn) {
	t.Helper()
	b, e := os.ReadFile("../../sql/081_truck_recurring_charges.sql")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = conn.Exec(ctx, string(b)); e != nil {
		t.Fatal(e)
	}
}
func withTruckChargeDatabase(t *testing.T, check func(*testing.T, context.Context, *pgxpool.Pool, string)) {
	t.Helper()
	dsn := os.Getenv("MSERP_DRIVER_CHARGES_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set MSERP_DRIVER_CHARGES_TEST_DATABASE_URL to a local _test database")
	}
	cfg, e := pgxpool.ParseConfig(dsn)
	if e != nil || !strings.HasSuffix(cfg.ConnConfig.Database, "_test") || (cfg.ConnConfig.Host != "127.0.0.1" && cfg.ConnConfig.Host != "localhost") {
		t.Fatal("local _test database required")
	}
	ctx := context.Background()
	admin, e := pgx.Connect(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer admin.Close(ctx)
	source, e := os.ReadFile("../../sql/init.sql")
	if e != nil {
		t.Fatal(e)
	}
	for _, mode := range []string{"fresh", "migration"} {
		t.Run(mode, func(t *testing.T) {
			schema := fmt.Sprintf("truck_recurring_%d", time.Now().UnixNano())
			q := pgx.Identifier{schema}.Sanitize()
			exec := func(sql string) {
				t.Helper()
				if _, e := admin.Exec(ctx, sql); e != nil {
					t.Fatal(e)
				}
			}
			exec("CREATE SCHEMA " + q + "; SET search_path TO " + q + ",public")
			defer admin.Exec(ctx, "SET search_path TO public; DROP SCHEMA "+q+" CASCADE")
			if mode == "fresh" {
				exec(string(source))
			} else {
				sourceMigration, sourceErr := os.ReadFile("../../sql/093_pay_source_collections.sql")
				if sourceErr != nil {
					t.Fatal(sourceErr)
				}
				prior := strings.ReplaceAll(string(source), "\r\n", "\n")
				prior = strings.Replace(prior, strings.ReplaceAll(string(sourceMigration), "\r\n", "\n"), "", 1)
				exec(withoutAccountingCutoverMigration(t, withoutTruckRecurringMigration(t, prior)))
				exec(`INSERT INTO drivers(id,full_name,normalized_name,pay_type,pay_rate,active,status) VALUES('81000000-0000-0000-0000-000000000001','Old recurring','old recurring','cpm',0,false,'terminated');
    INSERT INTO driver_charge_types(id,name,direction,amount,amounts) VALUES('81000000-0000-0000-0000-000000000002','charge','charge',25,ARRAY[25]::numeric[]);
    INSERT INTO driver_charge_schedules(id,driver_id,type_id,kind,name,direction,start_week,eligibility) VALUES('81000000-0000-0000-0000-000000000003','81000000-0000-0000-0000-000000000001','81000000-0000-0000-0000-000000000002','recurring','charge','charge','2026-10-05','calendar');
    INSERT INTO driver_charge_phases VALUES('81000000-0000-0000-0000-000000000003','2026-10-05',25,false);
    INSERT INTO driver_charge_occurrences(schedule_id,week_start,name,scheduled_amount,amount,confirmed_at) VALUES('81000000-0000-0000-0000-000000000003','2026-10-05','charge',-25,-25,now());
    INSERT INTO trucks(id,unit_number) VALUES('81000000-0000-0000-0000-000000000004','OLD-CHARGE');
    INSERT INTO truck_charge_phases(truck_id,type_id,week_start,amount) VALUES('81000000-0000-0000-0000-000000000004','81000000-0000-0000-0000-000000000002','2026-10-05',25);`)
				exec(`INSERT INTO driver_charge_schedules(id,driver_id,kind,name,direction,start_week,eligibility,total)
 VALUES('81000000-0000-0000-0000-000000000005','81000000-0000-0000-0000-000000000001','installment','Retained advance','charge','2026-10-05','calendar',100);
 INSERT INTO driver_charge_phases(schedule_id,week_start,amount) VALUES('81000000-0000-0000-0000-000000000005','2026-10-05',20);
 INSERT INTO payroll_settlements(driver_id,week_start,report,confirmed_schedules) VALUES('81000000-0000-0000-0000-000000000001','2026-10-05','{"frozen":"unchanged"}',ARRAY['81000000-0000-0000-0000-000000000003','81000000-0000-0000-0000-000000000005']::uuid[]);`)
				applyTruckRecurringMigration(t, ctx, admin)
				exec(string(sourceMigration))
				var preserved bool
				if e = admin.QueryRow(ctx, `SELECT finalized AND report='{"frozen":"unchanged"}'::jsonb
 AND confirmed_schedules=ARRAY['81000000-0000-0000-0000-000000000005']::uuid[]
 AND EXISTS(SELECT 1 FROM driver_charge_schedules WHERE kind='installment' AND total=100)
 AND EXISTS(SELECT 1 FROM pg_trigger WHERE tgrelid='driver_charge_occurrences'::regclass AND tgname='payroll_charge_guard' AND tgenabled='O')
 FROM payroll_settlements WHERE driver_id='81000000-0000-0000-0000-000000000001'`).Scan(&preserved); e != nil || !preserved {
					t.Fatal("frozen statement, installment or financial guard changed", e)
				}
				var count int
				if e = admin.QueryRow(ctx, `SELECT (SELECT count(*) FROM driver_charge_schedules WHERE kind='recurring')+(SELECT count(*) FROM driver_charge_types WHERE name='charge')+(SELECT count(*) FROM truck_charge_phases)`).Scan(&count); e != nil || count != 0 {
					t.Fatal("retired recurring rows remain", count, e)
				}
				if e = admin.QueryRow(ctx, `SELECT count(*) FROM driver_charge_events WHERE action IN ('retired_driver_recurring','type_deleted') AND type_id IS NULL AND schedule_id IS NULL`).Scan(&count); e != nil || count != 2 {
					t.Fatal("retirement audit missing", count, e)
				}
			}
			applyAccountingCutoverMigration(t, ctx, admin)
			exec("GRANT USAGE ON SCHEMA " + q + " TO mserp_app; GRANT SELECT,INSERT,UPDATE,DELETE ON ALL TABLES IN SCHEMA " + q + " TO mserp_app; GRANT USAGE,SELECT ON ALL SEQUENCES IN SCHEMA " + q + " TO mserp_app")
			app := cfg.Copy()
			app.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
			app.ConnConfig.RuntimeParams["role"] = "mserp_app"
			pool, e := pgxpool.NewWithConfig(ctx, app)
			if e != nil {
				t.Fatal(e)
			}
			defer pool.Close()
			var actor string
			if e = pool.QueryRow(ctx, `INSERT INTO app_users(username,password_hash) VALUES('Truck charges test',crypt('test-only',gen_salt('bf')) ) RETURNING id::text`).Scan(&actor); e != nil {
				t.Fatal(e)
			}
			check(t, ctx, pool, actor)
		})
	}
}

func TestTruckRecurringDatabase(t *testing.T) {
	withTruckChargeDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, actor string) {
		fleet := NewFleetRepository(pool)
		charges := NewDriverChargeRepository(pool)
		pay := NewDriverPayRepository(pool)
		week := ChargeCurrentWeek()
		date, _ := chargeWeek(week)
		driver, err := fleet.CreateDriver(ctx, DriverInput{FullName: "Automatic owner", PayType: "gross_percentage", PayRate: 88, Active: true, AssignmentWeek: week, IsOwnerOperator: true})
		if err != nil || driver.EmploymentType != "unassigned" || driver.IsOwnerOperator {
			t.Fatalf("unassigned/manual flag: %+v %v", driver, err)
		}
		truck, err := fleet.CreateTruck(ctx, TruckInput{UnitNumber: "AUTO-01", OwnerDriverID: &driver.ID, DriverID: &driver.ID, Active: true, Status: "assigned", AssignmentWeek: week})
		if err != nil {
			t.Fatal(err)
		}
		got, err := fleet.GetDriver(ctx, driver.ID)
		if err != nil || got.EmploymentType != "owner_operator" || !got.IsOwnerOperator {
			t.Fatalf("automatic owner: %+v %v", got, err)
		}
		second, err := fleet.CreateTruck(ctx, TruckInput{UnitNumber: "AUTO-02", OwnerDriverID: &driver.ID, Active: true, Status: "available"})
		if err != nil || second.OwnerID != truck.OwnerID {
			t.Fatal("owner identity not reused", err)
		}
		typ, err := charges.SaveType(ctx, ChargeType{Name: "Insurance", Direction: "charge", Amounts: []string{"50", "75"}}, actor)
		if err != nil {
			t.Fatal(err)
		}
		phase := TruckChargePhase{TruckID: truck.ID, TypeID: typ.ID, WeekStart: week, Included: true, Amount: "50", TypeVersion: typ.Version}
		if err = charges.SaveTruckCharge(ctx, phase, actor); err != nil {
			t.Fatal("owner truck fee without investor terms", err)
		}
		if err = charges.SaveTruckCharge(ctx, phase, actor); err != ErrChargeConflict {
			t.Fatal("stale fee accepted", err)
		}
		report, err := pay.Get(ctx, date)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, d := range report.Drivers {
			if d.ID == driver.ID {
				found = true
				if len(d.AutoCharges) != 1 || d.AutoCharges[0].Amount != "-50.00" {
					t.Fatalf("owner fee: %+v", d)
				}
			}
		}
		if !found {
			t.Fatal("owner fee-only statement missing")
		}
		// Reproduce a terminated owner whose active truck still retains an open
		// historical assignment. Truck calendar fees alone must not add a driver.
		if _, err = pool.Exec(ctx, `UPDATE drivers SET active=false,status='terminated' WHERE id=$1`, driver.ID); err != nil {
			t.Fatal(err)
		}
		inactive, err := pay.Get(ctx, date)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range inactive.Drivers {
			if d.ID == driver.ID {
				t.Fatal("truck fees reintroduced a terminated driver without weekly work")
			}
		}
		// With explicit investor terms, the same non-driving owner remains in
		// Investor Pay rather than being treated as an active owner-driver.
		if err = charges.SaveTruckTerm(ctx, TruckTerm{TruckID: truck.ID, OwnerID: truck.OwnerID, WeekStart: week, SharePercent: "88"}, actor); err != nil {
			t.Fatal(err)
		}
		inactive, err = pay.Get(ctx, date)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range inactive.Drivers {
			if d.ID == driver.ID {
				t.Fatal("truck terms reintroduced a terminated driver")
			}
		}
		ownerReport, err := pay.InvestorPay(ctx, date)
		if err != nil || len(ownerReport.Drivers) != 1 || ownerReport.Drivers[0].ID != truck.ID || len(ownerReport.Drivers[0].AutoCharges) != 1 {
			t.Fatal("terminated driver's investor truck fees were lost", ownerReport, err)
		}
		if _, err = pool.Exec(ctx, `UPDATE drivers SET active=true,status='active' WHERE id=$1`, driver.ID); err != nil {
			t.Fatal(err)
		}
		report, err = pay.Get(ctx, date)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = pay.Settle(ctx, date, driver.ID, report.Revision, actor, "", false, false); err != nil {
			t.Fatal("finalize owner fee", err)
		}
		if _, err = pool.Exec(ctx, `UPDATE drivers SET active=false,status='terminated' WHERE id=$1`, driver.ID); err != nil {
			t.Fatal(err)
		}
		inactive, err = pay.Get(ctx, date)
		if err != nil {
			t.Fatal(err)
		}
		found = false
		for _, d := range inactive.Drivers {
			if d.ID == driver.ID {
				found = true
				if d.Settlement == nil || !d.Settlement.Finalized || len(d.AutoCharges) != 1 || d.AutoCharges[0].Amount != "-50.00" {
					t.Fatal("terminated driver's frozen statement changed", d)
				}
			}
		}
		if !found {
			t.Fatal("terminated driver's saved statement disappeared")
		}
		if _, err = pool.Exec(ctx, `UPDATE drivers SET active=true,status='active' WHERE id=$1`, driver.ID); err != nil {
			t.Fatal(err)
		}
		closedChange := phase
		closedChange.Version, closedChange.Amount = 1, "75"
		if err = charges.SaveTruckCharge(ctx, closedChange, actor); err == nil {
			t.Fatal("truck fee changed a finalized owner statement")
		}
		var independentBulkOwner string
		if err = pool.QueryRow(ctx, `INSERT INTO investors(full_name) VALUES('Bulk fee owner') RETURNING id::text`).Scan(&independentBulkOwner); err != nil {
			t.Fatal(err)
		}
		bulkTruck, err := fleet.CreateTruck(ctx, TruckInput{UnitNumber: "BULK-OK", OwnerID: &independentBulkOwner, Active: true, Status: "available"})
		if err != nil {
			t.Fatal(err)
		}
		good := TruckChargePhase{TruckID: bulkTruck.ID, TypeID: typ.ID, WeekStart: week, Included: true, Amount: "50", TypeVersion: typ.Version}
		badAmount := good
		badAmount.TruckID, badAmount.Amount = second.ID, "-1"
		results, err := charges.SaveTruckCharges(ctx, []TruckChargePhase{closedChange, badAmount, good}, actor)
		if err != nil || len(results) != 3 || results[0].Error == "" || results[1].Error == "" || results[2].Error != "" {
			t.Fatalf("bulk did not continue after finalized/invalid rows: %+v %v", results, err)
		}
		var savedAmount string
		var auditCount int
		if err = pool.QueryRow(ctx, `SELECT amount::text FROM truck_charge_phases WHERE truck_id=$1`, bulkTruck.ID).Scan(&savedAmount); err != nil || savedAmount != "50.00" {
			t.Fatal("later truck fee was not saved", savedAmount, err)
		}
		if err = pool.QueryRow(ctx, `SELECT count(*) FROM truck_settlement_events WHERE truck_id=$1 AND action='charge'`, bulkTruck.ID).Scan(&auditCount); err != nil || auditCount != 1 {
			t.Fatal("bulk audit missing", auditCount, err)
		}
		// A stale INSERT triggers a database-level no-rows conflict. Its
		// savepoint must recover so a later valid update can still commit.
		nextTruck, err := fleet.CreateTruck(ctx, TruckInput{UnitNumber: "BULK-NEXT", OwnerID: &independentBulkOwner, Active: true, Status: "available"})
		if err != nil {
			t.Fatal(err)
		}
		valid := badAmount
		valid.TruckID, valid.Amount = nextTruck.ID, "75"
		results, err = charges.SaveTruckCharges(ctx, []TruckChargePhase{good, valid}, actor)
		if err != nil || len(results) != 2 || results[0].Error != ErrChargeConflict.Error() || results[1].Error != "" {
			t.Fatalf("bulk stale row stopped later save: %+v %v", results, err)
		}
		if _, err = charges.SaveTruckCharges(ctx, []TruckChargePhase{good, good}, actor); err == nil {
			t.Fatal("duplicate bulk cell accepted")
		}
		if _, err = charges.SaveTruckCharges(ctx, nil, actor); err == nil {
			t.Fatal("empty bulk accepted")
		}
		// An unexpected database error rolls back successes and their audit.
		changed := good
		changed.Version, changed.Amount = 1, "75"
		broken := valid
		broken.TruckID = "invalid-uuid"
		if _, err = charges.SaveTruckCharges(ctx, []TruckChargePhase{changed, broken}, actor); err == nil {
			t.Fatal("unexpected database error hidden")
		}
		if err = pool.QueryRow(ctx, `SELECT amount::text FROM truck_charge_phases WHERE truck_id=$1`, bulkTruck.ID).Scan(&savedAmount); err != nil || savedAmount != "50.00" {
			t.Fatal("failed bulk committed earlier update", savedAmount, err)
		}
		if err = pool.QueryRow(ctx, `SELECT count(*) FROM truck_settlement_events WHERE truck_id=$1 AND action='charge'`, bulkTruck.ID).Scan(&auditCount); err != nil || auditCount != 1 {
			t.Fatal("failed bulk committed audit", auditCount, err)
		}
		report, err = pay.Get(ctx, date)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = pay.Settle(ctx, date, driver.ID, report.Revision, actor, "Verify ownership changes", true, false); err != nil {
			t.Fatal(err)
		}
		if err = charges.SaveCell(ctx, ChargeCell{DriverID: driver.ID}, actor); err == nil {
			t.Fatal("driver recurring write accepted")
		}
		if _, err = charges.Create(ctx, ChargeCreate{Kind: "recurring"}, actor); err == nil {
			t.Fatal("driver recurring creation accepted")
		}
		company := "00000000-0000-0000-0000-000000000001"
		input := TruckInput{UnitNumber: truck.UnitNumber, OwnerID: &company, DriverID: &driver.ID, Status: "assigned", Active: true, AssignmentWeek: week}
		if _, err = fleet.UpdateTruck(ctx, truck.ID, input); err != nil {
			t.Fatal(err)
		}
		got, err = fleet.GetDriver(ctx, driver.ID)
		if err != nil || got.EmploymentType != "company" || got.IsOwnerOperator {
			t.Fatal("company ownership did not change type", got, err)
		}
		phase.Version = 1
		if err = charges.SaveTruckCharge(ctx, phase, actor); err == nil {
			t.Fatal("company fee accepted")
		}
		var independent string
		if err = pool.QueryRow(ctx, `INSERT INTO investors(full_name) VALUES('Independent owner') RETURNING id::text`).Scan(&independent); err != nil {
			t.Fatal(err)
		}
		input.OwnerID = &independent
		if _, err = fleet.UpdateTruck(ctx, truck.ID, input); err != nil {
			t.Fatal(err)
		}
		got, err = fleet.GetDriver(ctx, driver.ID)
		if err != nil || got.EmploymentType != "investor_driver" || got.IsOwnerOperator {
			t.Fatal("investor driver type", got, err)
		}
		management, err := charges.TruckCharges(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(management.EligibleTruckIDs) != 4 {
			t.Fatal("all non-company trucks not included", management)
		}
		// Exercise a fleet-sized selection through the same financial guards and
		// audit writes, rather than only verifying a handful of checkbox saves.
		rows, err := pool.Query(ctx, `INSERT INTO trucks(unit_number,owner_id) SELECT 'BULK-FLEET-'||n,$1::uuid FROM generate_series(1,250) n RETURNING id::text`, independentBulkOwner)
		if err != nil {
			t.Fatal(err)
		}
		updates := make([]TruckChargePhase, 0, 250)
		for rows.Next() {
			v := good
			if err = rows.Scan(&v.TruckID); err != nil {
				t.Fatal(err)
			}
			updates = append(updates, v)
		}
		rows.Close()
		if err = rows.Err(); err != nil {
			t.Fatal(err)
		}
		started := time.Now()
		results, err = charges.SaveTruckCharges(ctx, updates, actor)
		if err != nil || len(results) != 250 {
			t.Fatal("fleet-sized save failed", len(results), err)
		}
		for i, result := range results {
			if result.Error != "" || result.TruckID != updates[i].TruckID {
				t.Fatalf("fleet-sized save result %d: %+v", i, result)
			}
		}
		t.Logf("250 truck charges saved in one batch in %s", time.Since(started))
		// Owner creation rolls back together with a rejected duplicate truck.
		other, err := fleet.CreateDriver(ctx, DriverInput{FullName: "Rollback owner", PayType: "cpm", Active: true})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = fleet.CreateTruck(ctx, TruckInput{UnitNumber: truck.UnitNumber, OwnerDriverID: &other.ID, Active: true, Status: "available"}); err == nil {
			t.Fatal("duplicate truck accepted")
		}
		var count int
		if err = pool.QueryRow(ctx, `SELECT count(*) FROM investors WHERE driver_id=$1`, other.ID).Scan(&count); err != nil || count != 0 {
			t.Fatal("failed truck leaked investor", count, err)
		}
	})
}

func TestTruckRecurringFinalizedSelectionDatabase(t *testing.T) {
	withTruckChargeDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, actor string) {
		charges := NewDriverChargeRepository(pool)
		fleet := NewFleetRepository(pool)
		week := ChargeCurrentWeek()
		date, _ := chargeWeek(week)
		nextWeek := date.AddDate(0, 0, 7).Format("2006-01-02")
		previousWeek := date.AddDate(0, 0, -7).Format("2006-01-02")
		driver, err := fleet.CreateDriver(ctx, DriverInput{FullName: "Frozen owner", PayType: "cpm", Active: true, AssignmentWeek: week})
		if err != nil {
			t.Fatal(err)
		}
		driverTruck, err := fleet.CreateTruck(ctx, TruckInput{UnitNumber: "FROZEN-DRIVER", OwnerDriverID: &driver.ID, Active: true, Status: "available"})
		if err != nil {
			t.Fatal(err)
		}
		var owner string
		if err = pool.QueryRow(ctx, `INSERT INTO investors(full_name) VALUES('Selection owner') RETURNING id::text`).Scan(&owner); err != nil {
			t.Fatal(err)
		}
		investorTruck, err := fleet.CreateTruck(ctx, TruckInput{UnitNumber: "FROZEN-INVESTOR", OwnerID: &owner, Active: true, Status: "available", OwnershipEffectiveDate: previousWeek})
		if err != nil {
			t.Fatal(err)
		}
		openTruck, err := fleet.CreateTruck(ctx, TruckInput{UnitNumber: "OPEN-TRUCK", OwnerID: &owner, Active: true, Status: "available"})
		if err != nil {
			t.Fatal(err)
		}
		typ, err := charges.SaveType(ctx, ChargeType{Name: "Selection fee", Direction: "charge", Amounts: []string{"50"}}, actor)
		if err != nil {
			t.Fatal(err)
		}
		// A later phase bounds the affected investor period, independently per type.
		if err = charges.SaveTruckCharge(ctx, TruckChargePhase{TruckID: investorTruck.ID, TypeID: typ.ID, WeekStart: nextWeek, Amount: "50", TypeVersion: typ.Version}, actor); err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `INSERT INTO payroll_settlements(driver_id,week_start,report) VALUES($1,$2,'{"frozen":"driver"}')`, driver.ID, week); err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `INSERT INTO investor_pay_weeks(truck_id,week_start,owner_id,edits,report,finalized) VALUES($1,$2,$3,'{}','{"frozen":"investor"}',true)`, investorTruck.ID, week, owner); err != nil {
			t.Fatal(err)
		}
		management, err := charges.TruckChargesForWeek(ctx, week)
		if err != nil {
			t.Fatal(err)
		}
		locked := map[string]bool{}
		for _, v := range management.LockedCharges {
			if v.TypeID == typ.ID {
				locked[v.TruckID] = true
			}
		}
		if !locked[driverTruck.ID] || !locked[investorTruck.ID] || locked[openTruck.ID] {
			t.Fatalf("wrong finalized selection locks: %+v", management.LockedCharges)
		}
		updates := []TruckChargePhase{}
		for _, truck := range []string{driverTruck.ID, openTruck.ID, investorTruck.ID} {
			updates = append(updates, TruckChargePhase{TruckID: truck, TypeID: typ.ID, WeekStart: week, Amount: "50", Included: true, TypeVersion: typ.Version})
		}
		// Even a stale browser that sends locked trucks cannot change them or
		// prevent the open truck in the middle of the selection from saving.
		results, err := charges.SaveTruckCharges(ctx, updates, actor)
		if err != nil || len(results) != 3 || results[0].Error == "" || results[1].Error != "" || results[2].Error == "" {
			t.Fatalf("finalized selection results: %+v %v", results, err)
		}
		var unchanged bool
		if err = pool.QueryRow(ctx, `SELECT
 (SELECT report='{"frozen":"driver"}'::jsonb AND finalized AND version=1 FROM payroll_settlements WHERE driver_id=$1 AND week_start=$4::date)
 AND (SELECT report='{"frozen":"investor"}'::jsonb AND finalized AND version=1 FROM investor_pay_weeks WHERE truck_id=$2 AND week_start=$4::date)
 AND NOT EXISTS(SELECT 1 FROM truck_charge_phases WHERE truck_id=ANY($3::uuid[]) AND week_start=$4::date)
 AND NOT EXISTS(SELECT 1 FROM truck_settlement_events WHERE truck_id=ANY($3::uuid[]) AND week_start=$4::date)`, driver.ID, investorTruck.ID, []string{driverTruck.ID, investorTruck.ID}, week).Scan(&unchanged); err != nil || !unchanged {
			t.Fatal("finalized statements or charge sources changed", unchanged, err)
		}
		// A week before the frozen investor week remains protected until a
		// subsequent phase cuts off its effect. Later weeks are editable.
		for _, selected := range []string{previousWeek, nextWeek} {
			management, err = charges.TruckChargesForWeek(ctx, selected)
			if err != nil {
				t.Fatal(err)
			}
			isLocked := false
			for _, v := range management.LockedCharges {
				if v.TruckID == investorTruck.ID && v.TypeID == typ.ID {
					isLocked = true
				}
			}
			if isLocked != (selected == previousWeek) {
				t.Fatalf("wrong period lock for %s: %+v", selected, management.LockedCharges)
			}
		}
	})
}
