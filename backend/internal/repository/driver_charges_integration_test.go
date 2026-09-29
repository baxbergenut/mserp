package repository

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDriverChargesDatabase(t *testing.T) {
	dsn := os.Getenv("MSERP_DRIVER_CHARGES_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set MSERP_DRIVER_CHARGES_TEST_DATABASE_URL to a disposable _test database")
	}
	u, err := url.Parse(dsn)
	if err != nil || !strings.Contains(u.Path, "_test") {
		t.Fatal("only a disposable _test database is allowed")
	}
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal("test database unavailable")
	}
	defer admin.Close(ctx)
	for _, mode := range []string{"fresh", "migration"} {
		t.Run(mode, func(t *testing.T) {
			schema := fmt.Sprintf("charges_test_%d", time.Now().UnixNano())
			quoted := pgx.Identifier{schema}.Sanitize()
			if _, err = admin.Exec(ctx, `CREATE SCHEMA `+quoted+`;SET search_path TO `+quoted+`,public`); err != nil {
				t.Fatal(err)
			}
			defer func() { _, _ = admin.Exec(ctx, `SET search_path TO public;DROP SCHEMA `+quoted+` CASCADE`) }()
			init, err := os.ReadFile("../../sql/init.sql")
			if err != nil {
				t.Fatal(err)
			}
			migration, err := os.ReadFile("../../sql/033_add_driver_charges.sql")
			if err != nil {
				t.Fatal(err)
			}
			matrix, err := os.ReadFile("../../sql/034_driver_charge_matrix.sql")
			if err != nil {
				t.Fatal(err)
			}
			backdated, err := os.ReadFile("../../sql/035_backdated_driver_charges.sql")
			if err != nil {
				t.Fatal(err)
			}
            settlementMigration, err := os.ReadFile("../../sql/038_payroll_settlements.sql")
            if err != nil { t.Fatal(err) }
			source := strings.ReplaceAll(string(init), "\r\n", "\n")
			body := strings.ReplaceAll(string(migration), "\r\n", "\n")
			if mode == "migration" {
 source=strings.Replace(source,strings.ReplaceAll(string(settlementMigration),"\r\n","\n"),"",1)
				source = strings.Replace(source, strings.ReplaceAll(string(matrix), "\r\n", "\n"), "", 1)
				source = strings.Replace(source, strings.ReplaceAll(string(backdated), "\r\n", "\n"), "", 1)
				source = strings.Replace(source, body, "", 1)
				if strings.Contains(source, "CREATE TABLE driver_charge_types") {
					t.Fatal("migration isolation failed")
				}
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
				if _, err = admin.Exec(ctx, `GRANT USAGE ON SCHEMA `+quoted+` TO mserp_app`); err != nil {
					t.Fatal(err)
				}
				// Exercise an actual populated 033 schema, retaining custom amounts and
				// historical load eligibility when moving it onto the charge type.
				_, err = admin.Exec(ctx, `INSERT INTO drivers(id,full_name,normalized_name,pay_type,pay_rate) VALUES('00000000-0000-4000-8000-000000000034','Historical Driver','historical driver','cpm',0.75);
    INSERT INTO driver_charge_types(id,name,direction,amount) VALUES('00000000-0000-4000-8000-000000000035','Historical type','charge',200);
    INSERT INTO driver_charge_schedules(id,driver_id,type_id,kind,name,direction,start_week,end_week,eligibility) VALUES('00000000-0000-4000-8000-000000000036','00000000-0000-4000-8000-000000000034','00000000-0000-4000-8000-000000000035','recurring','Historical type','charge','2001-01-01','2001-01-08','loads');
    INSERT INTO driver_charge_phases(schedule_id,week_start,amount) VALUES('00000000-0000-4000-8000-000000000036','2001-01-01',175);`)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = admin.Exec(ctx, string(matrix)); err != nil {
					t.Fatal(err)
				}
				var options []string
				var eligibility string
				if err = admin.QueryRow(ctx, `SELECT amounts::text[],eligibility FROM driver_charge_types WHERE name='Historical type'`).Scan(&options, &eligibility); err != nil {
					t.Fatal(err)
				}
				if len(options) != 2 || options[0] != "200.00" || options[1] != "175.00" || eligibility != "loads" {
					t.Fatal("migration lost configured amounts or eligibility", options, eligibility)
				}
			}
			if mode == "migration" {
				if _, err = admin.Exec(ctx, string(backdated)); err != nil {
					t.Fatal(err)
				}
			}
			if mode=="migration" {if _,err:=admin.Exec(ctx,string(settlementMigration));err!=nil{t.Fatal(err)}}
            // Do not grant charge tables here: migration ownership must provide access.
			if _, err = admin.Exec(ctx, `GRANT USAGE ON SCHEMA `+quoted+` TO mserp_app;GRANT SELECT ON app_users TO mserp_app; GRANT SELECT,INSERT,UPDATE,DELETE ON drivers,dispatchers,trucks,truck_driver_assignments,gross_board_entries,gross_board_extra_entries,loads,driver_pay_weeks,fuel_transactions,fuel_transaction_items,tolls,app_users,files TO mserp_app`); err != nil {
				t.Fatal(err)
			}
			cfg, err := pgxpool.ParseConfig(dsn)
			if err != nil {
				t.Fatal(err)
			}
			cfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
			cfg.ConnConfig.RuntimeParams["role"] = "mserp_app"
			pool, err := pgxpool.NewWithConfig(ctx, cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer pool.Close()
			repo := NewDriverChargeRepository(pool)
			pay := NewDriverPayRepository(pool)
			week := ChargeCurrentWeek()
			monday, _ := chargeWeek(week)
			next := monday.AddDate(0, 0, 7).Format(time.DateOnly)
			var actor, driver, other string
			if err = admin.QueryRow(ctx, `INSERT INTO app_users(username,password_hash) VALUES('charges',crypt('test-only',gen_salt('bf'))) RETURNING id::text`).Scan(&actor); err != nil {
				t.Fatal(err)
			}
			for i, p := range []*string{&driver, &other} {
				if err = admin.QueryRow(ctx, `INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate) VALUES($1,$1,'cpm',0.75) RETURNING id::text`, fmt.Sprintf("Charge Driver %d", i)).Scan(p); err != nil {
					t.Fatal(err)
				}
			}
			typ, err := repo.SaveType(ctx, ChargeType{Name: "Admin", Direction: "charge", Amount: "50"}, actor)
			if err != nil {
				t.Fatal(err)
			}
			creation := ChargeCreate{DriverIDs: []string{driver}, Kind: "recurring", TypeID: typ.ID, Amount: "50", StartWeek: week, Eligibility: "calendar"}
			ids, err := repo.Create(ctx, creation, actor)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = repo.Create(ctx, creation, actor); err == nil {
				t.Fatal("overlap accepted")
			}
			creation.DriverIDs = []string{other, driver}
			if _, err = repo.Create(ctx, creation, actor); err == nil {
				t.Fatal("bulk duplicate accepted")
			}
			data, err := repo.List(ctx, other)
			if err != nil || len(data.Schedules) != 0 {
				t.Fatal("failed bulk left partial assignments", err)
			}
			report, err := pay.Get(ctx, monday)
			if err != nil {
				t.Fatal(err)
			}
			if len(report.Drivers) != 1 || len(report.Drivers[0].Loads) != 0 || report.Drivers[0].Edits.GeneratedCharges[0].Amount != "-50.00" {
				t.Fatalf("charge-only payroll: %+v", report)
			}
			var count int
			if err = admin.QueryRow(ctx, `SELECT count(*) FROM driver_charge_occurrences`).Scan(&count); err != nil || count != 0 {
				t.Fatal("read persisted occurrences", err)
			}
			renamed := report.Drivers[0].Edits
			renamed.GeneratedCharges = append([]ChargeOccurrence(nil), renamed.GeneratedCharges...)
			renamed.GeneratedCharges[0].Name = "Changed recurring label"
			if _, err = pay.Save(ctx, renamed, actor); err == nil {
				t.Fatal("recurring label was editable")
			}
			edits := report.Drivers[0].Edits
			edits.GeneratedCharges[0].Amount = "0"
			saved, err := pay.Save(ctx, edits, actor)
			if err != nil {
				t.Fatal(err)
			}
			if saved.GeneratedCharges[0].Amount != "0.00" {
				t.Fatal("skip not retained")
			}
			if _, err = pay.Save(ctx, edits, actor); err == nil {
				t.Fatal("stale save accepted")
			}
			// Older clients omit generated rows and cannot erase them.
			legacy := saved
			legacy.GeneratedCharges = nil
			legacy.Notes = "legacy edit"
			if _, err = pay.Save(ctx, legacy, actor); err != nil {
				t.Fatal(err)
			}
			report, err = pay.Get(ctx, monday)
			if err != nil || report.Drivers[0].Edits.GeneratedCharges[0].Amount != "0.00" {
				t.Fatal("legacy save erased skip", err)
			}
			edits = report.Drivers[0].Edits
			edits.GeneratedCharges[0].Reset = true
			saved, err = pay.Save(ctx, edits, actor)
			if err != nil || saved.GeneratedCharges[0].Amount != "-50.00" {
				t.Fatal("reset", err)
			}
			typ.Amounts = []string{"80", "35", "25", "20"}
			typ, err = repo.SaveType(ctx, typ, actor)
			if err != nil {
				t.Fatal(err)
			}
			report, err = pay.Get(ctx, monday)
			if err != nil || report.Drivers[0].Edits.GeneratedCharges[0].Amount != "-50.00" {
				t.Fatal("default altered assignment", err)
			}
			data, err = repo.List(ctx, driver)
			if err != nil {
				t.Fatal(err)
			}
			if err = repo.Bulk(ctx, ChargeBulk{Targets: []ChargeTarget{{ID: ids[0], Version: data.Schedules[0].Version}}, Action: "amount", Amount: "35", WeekStart: next}, actor); err != nil {
				t.Fatal(err)
			}
			nextDate, _ := chargeWeek(next)
			future, err := pay.Get(ctx, nextDate)
			if err != nil || future.Drivers[0].Edits.GeneratedCharges[0].Amount != "-35.00" {
				t.Fatal("effective amount", err)
			}
			report, err = pay.Get(ctx, monday)
			if err != nil || report.Drivers[0].Edits.GeneratedCharges[0].Amount != "-50.00" {
				t.Fatal("history changed", err)
			}
			// An installment is not collected by viewing or saving payroll.
			plans, err := repo.Create(ctx, ChargeCreate{DriverIDs: []string{driver}, Kind: "installment", Name: "Advance", Total: "650", Amount: "100", StartWeek: week, Eligibility: "calendar"}, actor)
			if err != nil {
				t.Fatal(err)
			}
			projection, err := repo.Preview(ctx, plans[0])
			if err != nil || len(projection) != 7 || projection[6].Amount != "-50.00" {
				t.Fatal("installment preview", err)
			}
			selected := projection[0]
			selected.Amount = "-60.00"
			report, err = pay.Get(ctx, monday)
			if err != nil {
				t.Fatal(err)
			}
			edits = report.Drivers[0].Edits
			for i := range edits.GeneratedCharges {
				if edits.GeneratedCharges[i].ScheduleID == plans[0] {
					edits.GeneratedCharges[i].Amount = "-60.00"
				}
			}
			saved, err = pay.Save(ctx, edits, actor)
			if err != nil {
				t.Fatal(err)
			}
			for _, o := range saved.GeneratedCharges {
				if o.ScheduleID == plans[0] {
					selected = o
				}
			}
			confirmation := ChargeConfirm{DriverID: driver, WeekStart: week, Rows: []ChargeOccurrence{selected}}
			var wg sync.WaitGroup
			outcomes := make(chan error, 2)
			for i := 0; i < 2; i++ {
				wg.Add(1)
				go func() { defer wg.Done(); outcomes <- repo.Confirm(ctx, confirmation, actor, false) }()
			}
			wg.Wait()
			close(outcomes)
			for e := range outcomes {
				if e != nil {
					t.Fatal("concurrent confirmation", e)
				}
			}
			if err = repo.Confirm(ctx, confirmation, actor, false); err != nil {
				t.Fatal("confirmation retry", err)
			}
			data, err = repo.List(ctx, driver)
			if err != nil {
				t.Fatal(err)
			}
			for _, s := range data.Schedules {
				if s.ID == plans[0] {
					if s.Confirmed != "60.00" || s.Remaining != "590.00" {
						t.Fatalf("balance %+v", s)
					}
					selected = s.Occurrences[0]
				}
			}
			if err = repo.Confirm(ctx, ChargeConfirm{DriverID: other, WeekStart: week, Rows: []ChargeOccurrence{selected}}, actor, false); err == nil {
				t.Fatal("cross-driver confirmation accepted")
			}
			report, err = pay.Get(ctx, monday)
			if err != nil {
				t.Fatal(err)
			}
			edits = report.Drivers[0].Edits
			for i := range edits.GeneratedCharges {
				if edits.GeneratedCharges[i].ScheduleID == plans[0] {
					edits.GeneratedCharges[i].Amount = "-80.00"
				}
			}
			if _, err = pay.Save(ctx, edits, actor); err == nil {
				t.Fatal("confirmed amount editable")
			}
			confirmation.Rows = []ChargeOccurrence{selected}
			confirmation.Reason = "Correction"
			if err = repo.Confirm(ctx, confirmation, actor, true); err != nil {
				t.Fatal("reopen", err)
			}
			projection, err = repo.Preview(ctx, plans[0])
			if err != nil || len(projection) != 7 || projection[6].Amount != "-90.00" {
				t.Fatal("reopen projection", err)
			}
			data, err = repo.List(ctx, driver)
			if err != nil {
				t.Fatal(err)
			}
			for _, s := range data.Schedules {
				if s.ID == plans[0] && s.Confirmed != "0.00" {
					t.Fatal("reopen did not restore balance")
				}
			}
			// A rejected oversized weekly edit must roll back both the ledger and payroll version.
			report, err = pay.Get(ctx, monday)
			if err != nil {
				t.Fatal(err)
			}
			edits = report.Drivers[0].Edits
			for i := range edits.GeneratedCharges {
				if edits.GeneratedCharges[i].ScheduleID == plans[0] {
					edits.GeneratedCharges[i].Amount = "-700.00"
				}
			}
			if _, err = pay.Save(ctx, edits, actor); err == nil {
				t.Fatal("overallocated edit accepted")
			}
			report, err = pay.Get(ctx, monday)
			if err != nil {
				t.Fatal(err)
			}
			if report.Drivers[0].Edits.Version != edits.Version {
				t.Fatal("failed edit advanced payroll version")
			}
			for _, o := range report.Drivers[0].Edits.GeneratedCharges {
				if o.ScheduleID == plans[0] && o.Amount != "-60.00" {
					t.Fatal("failed edit changed amount")
				}
			}
			// Deletion restriction and atomic effective deactivation.
			fleet := NewFleetRepository(pool)
			if err = fleet.DeleteDriver(ctx, driver); err == nil {
				t.Fatal("deleted financial history")
			}
			input := DriverInput{FullName: "Charge Driver 0", PayType: "cpm", PayRate: 0.75, Active: false, ChargePauseWeek: next, ChargeActor: actor}
			if _, err = fleet.UpdateDriver(ctx, driver, input); err != nil {
				t.Fatal("deactivation", err)
			}
			projection, err = repo.Preview(ctx, plans[0])
			if err != nil || len(projection) != 1 {
				t.Fatal("pause did not stop installments", err, len(projection))
			}
			input.Active = true
			if _, err = fleet.UpdateDriver(ctx, driver, input); err != nil {
				t.Fatal(err)
			}
			projection, _ = repo.Preview(ctx, plans[0])
			if len(projection) != 1 {
				t.Fatal("reactivation resumed charges")
			}
			// Load eligibility belongs to the type, regardless of assignment input.
			typ.Eligibility = "loads"
			typ, err = repo.SaveType(ctx, typ, actor)
			if err != nil {
				t.Fatal(err)
			}
			// Load eligibility: statuses do not qualify, unmatched plans do.
			_, err = repo.Create(ctx, ChargeCreate{DriverIDs: []string{other}, Kind: "recurring", TypeID: typ.ID, Amount: "25", StartWeek: week, Eligibility: "loads"}, actor)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = admin.Exec(ctx, `INSERT INTO gross_board_entries(driver_id,service_date,day_status) VALUES($1,$2::date,'HOME')`, other, week); err != nil {
				t.Fatal(err)
			}
			report, err = pay.Get(ctx, monday)
			if err != nil {
				t.Fatal(err)
			}
			for _, d := range report.Drivers {
				if d.ID == other {
					t.Fatal("status-only driver charged")
				}
			}
			if _, err = admin.Exec(ctx, `UPDATE gross_board_entries SET day_status='',load_number='unmatched' WHERE driver_id=$1`, other); err != nil {
				t.Fatal(err)
			}
			report, err = pay.Get(ctx, monday)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, d := range report.Drivers {
				if d.ID == other {
					found = len(d.Edits.GeneratedCharges) == 1
				}
			}
			if !found {
				t.Fatal("unmatched plan not eligible")
			}
			// Ending an assignment on its first week permits a replacement in that week.
			otherData, e := repo.List(ctx, other)
			if e != nil {
				t.Fatal(e)
			}
			target := otherData.Schedules[0]
			if e = repo.Bulk(ctx, ChargeBulk{Targets: []ChargeTarget{{ID: target.ID, Version: target.Version}}, Action: "end", WeekStart: week}, actor); e != nil {
				t.Fatal(e)
			}
			if _, e = repo.Create(ctx, ChargeCreate{DriverIDs: []string{other}, Kind: "recurring", TypeID: typ.ID, Amount: "20", StartWeek: week, Eligibility: "calendar"}, actor); e != nil {
				t.Fatal("replace ended assignment", e)
			}
			history, err := repo.History(ctx, plans[0])
			if err != nil || len(history) < 4 {
				t.Fatal("audit history missing", err)
			}
			var invalid *ChargeValidationError
			if !errors.As(fleet.DeleteDriver(ctx, driver), &invalid) {
				t.Fatal("deletion did not return useful error")
			}
			testChargeMatrix(t, pool, actor)
			testBackdatedChargeMatrix(t, pool, actor)
		})
	}
}
