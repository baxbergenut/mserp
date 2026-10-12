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
				exec(withoutTruckRecurringMigration(t, strings.ReplaceAll(string(source), "\r\n", "\n")))
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
		if _, err = pay.Settle(ctx, date, driver.ID, report.Revision, actor, "", false); err != nil {
			t.Fatal("finalize owner fee", err)
		}
		closedChange := phase
		closedChange.Version, closedChange.Amount = 1, "75"
		if err = charges.SaveTruckCharge(ctx, closedChange, actor); err == nil {
			t.Fatal("truck fee changed a finalized owner statement")
		}
		report, err = pay.Get(ctx, date)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = pay.Settle(ctx, date, driver.ID, report.Revision, actor, "Verify ownership changes", true); err != nil {
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
		if len(management.EligibleTruckIDs) != 2 {
			t.Fatal("all non-company trucks not included", management)
		}
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
