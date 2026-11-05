package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"mserp/internal/datatruck"
)

func TestDataTruckDriverGrossDatabase(t *testing.T) {
	dsn := os.Getenv("MSERP_GROSS_BOARD_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("local disposable database required")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.HasSuffix(cfg.ConnConfig.Database, "_test") || (cfg.ConnConfig.Host != "127.0.0.1" && cfg.ConnConfig.Host != "localhost") {
		t.Fatal("local _test database required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	for _, migrated := range []bool{false, true} {
		t.Run(fmt.Sprint(migrated), func(t *testing.T) {
			admin, err := pgx.Connect(ctx, dsn)
			if err != nil {
				t.Fatal(err)
			}
			defer admin.Close(context.Background())
			name := fmt.Sprintf("driver_gross_%d", time.Now().UnixNano())
			quoted := pgx.Identifier{name}.Sanitize()
			if _, err = admin.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
				t.Fatal(err)
			}
			defer admin.Exec(context.Background(), "DROP SCHEMA "+quoted+" CASCADE")
			conf := cfg.Copy()
			conf.MaxConns = 1
			conf.ConnConfig.RuntimeParams["search_path"] = name + ",public"
			pool, err := pgxpool.NewWithConfig(ctx, conf)
			if err != nil {
				t.Fatal(err)
			}
			defer pool.Close()
			exec := func(sql string, args ...any) {
				t.Helper()
				if _, e := pool.Exec(ctx, sql, args...); e != nil {
					t.Fatal(e)
				}
			}
			initial, e := os.ReadFile("../../sql/init.sql")
			if e != nil {
				t.Fatal(e)
			}
			migration, e := os.ReadFile("../../sql/195_datatruck_driver_gross.sql")
			if e != nil {
				t.Fatal(e)
			}
			body := strings.ReplaceAll(string(initial), "\r\n", "\n")
			m := strings.ReplaceAll(string(migration), "\r\n", "\n")
			if migrated {
				body = strings.Replace(body, m, "", 1)
			}
			exec(body)
			const driver = "81000000-0000-0000-0000-000000009501"
			exec(`INSERT INTO drivers(id,full_name,normalized_name,pay_type,pay_rate) VALUES($1,'Gross Driver','gross driver','gross_percentage',80)`, driver)
			// Historical amounts and blanks are not migrated into new automatic defaults.
			if migrated {
				exec(`INSERT INTO gross_board_entries(driver_id,service_date,load_number,driver_rate) VALUES($1,'2026-10-12','LEGACY',2800),($1,'2026-10-13','LEGACY-BLANK',NULL)`, driver)
				exec(m)
			}
			exec(`GRANT USAGE ON SCHEMA ` + quoted + ` TO mserp_app; GRANT ALL ON ALL TABLES IN SCHEMA ` + quoted + ` TO mserp_app; GRANT ALL ON ALL SEQUENCES IN SCHEMA ` + quoted + ` TO mserp_app; SET ROLE mserp_app`)
			source := NewLoadRepository(pool)
			board := NewGrossBoardRepository(pool)
			pay := NewDriverPayRepository(pool)
			sync := func(id int, number, gross string, totals ...int) {
				t.Helper()
				total, miles := 2800, 541
				if len(totals) == 2 {
					total, miles = totals[0], totals[1]
				}
				raw := fmt.Sprintf(`{"id":%d,"load_id":%q,"status":"delivered","load_pay":%d,"total_pay":%d,"total_miles":%d,"pickup_time":"2026-10-19T00:01:00Z","trips":[{"id":%d,"trip_id":"T-1","driver__full_name":"Gross Driver","total_load_pay":%s,"mile":%d,"empty_mile":50}]}`, id, number, total, total, miles, id, gross, miles-50)
				var l datatruck.Load
				if e := json.Unmarshal([]byte(raw), &l); e != nil {
					t.Fatal(e)
				}
				p, e := json.Marshal(l)
				if e != nil {
					t.Fatal(e)
				}
				r, e := LoadToRecord(l, p, time.Now())
				if e != nil {
					t.Fatal(e)
				}
				if e = source.UpsertLoads(ctx, []LoadRecord{r}); e != nil {
					t.Fatal(e)
				}
			}
			save := func(e GrossBoardEntry) GrossBoardEntry {
				t.Helper()
				out, err := board.SaveEntries(ctx, []GrossBoardEntry{e})
				if err != nil {
					t.Fatal(err)
				}
				return out[0]
			}
			read := func(date string) GrossBoardEntry {
				t.Helper()
				week, _ := time.Parse(time.DateOnly, "2026-10-19")
				out, err := board.Get(ctx, week)
				if err != nil {
					t.Fatal(err)
				}
				for _, v := range out.Entries {
					if v.Date == date && v.DriverID == driver {
						return v
					}
				}
				t.Fatal("missing row", date)
				return GrossBoardEntry{}
			}
			sync(9501, "KNOWN", "2300")
			lookup, err := board.SearchLoads(ctx, "KNOWN")
			if err != nil || len(lookup) != 1 || lookup[0].DriverRate != "2300" {
				t.Fatal("lookup", lookup, err)
			}
			known := save(GrossBoardEntry{DriverID: driver, Date: "2026-10-19", LoadNumber: "KNOWN"})
			if known.DriverRate != "2300.00" || known.SystemDriverRate != "2300" || known.OriginalRate != "2800.00" {
				t.Fatal("initial trip default", known)
			}
			known.EnteredOriginalRate, known.EnteredMiles = "2800", "541"
			known = save(known)
			sync(9501, "KNOWN", "2200", 2900, 600)
			known = read(known.Date)
			if known.DriverRate != "2300.00" || known.SystemDriverRate != "2200" || known.SystemOriginalRate != "2900.00" || known.SystemMiles != "600.00" || known.OriginalRate != "2800.00" || known.Miles != "541.00" {
				t.Fatal("saved amount overwritten", known)
			}
			input := PaySourceAcceptance{Field: "driverGross", DriverID: driver, Date: known.Date, Version: known.Version, LoadRecordID: 9501, DriverGross: "2300"}
			if _, err = pay.AcceptPaySourceWithUndo(ctx, input, "gross-test"); err == nil {
				t.Fatal("stale source accepted")
			}
			input.DriverGross = "2200"
			receipt, err := pay.AcceptPaySourceWithUndo(ctx, input, "gross-test")
			if err != nil {
				t.Fatal(err)
			}
			accepted := read(known.Date)
			if accepted.DriverRate != "2200.00" || accepted.OriginalRate != known.OriginalRate || accepted.Miles != known.Miles {
				t.Fatal("accept changed other fields", accepted)
			}
			if err = pay.UndoPaySource(ctx, receipt, "gross-test"); err != nil {
				t.Fatal(err)
			}
			if read(known.Date).DriverRate != "2300.00" {
				t.Fatal("undo did not restore driver gross")
			}
			patch := read(known.Date)
			patch.Fields = []string{"driverRate", "acceptDriverGross"}
			patch.DriverRate, patch.AcceptDriverGross = "2200", true
			note := "Concurrent note"
			if _, err = board.SaveChanges(ctx, []GrossBoardEntry{{DriverID: driver, Date: patch.Date, Fields: []string{"note"}, Note: &note}}, "gross-test"); err != nil {
				t.Fatal(err)
			}
			stale := patch
			stale.DriverRate = "2300"
			if _, err = board.SaveChanges(ctx, []GrossBoardEntry{stale}, "gross-test"); err == nil {
				t.Fatal("stale DataTruck amount was accepted")
			}
			stale = patch
			otherID := 9502
			stale.LoadRecordID = &otherID
			if _, err = board.SaveChanges(ctx, []GrossBoardEntry{stale}, "gross-test"); err == nil {
				t.Fatal("acceptance applied to a different load")
			}
			patched, err := board.SaveChanges(ctx, []GrossBoardEntry{patch}, "gross-test")
			if err != nil || len(patched) != 1 || patched[0].DriverRate != "2200.00" || patched[0].OriginalRate != "2800.00" || patched[0].Miles != "541.00" {
				t.Fatal("independent board acceptance", patched, err)
			}
			if patched[0].AcceptDriverGross || patched[0].Note == nil || *patched[0].Note != note {
				t.Fatal("acknowledgement retained an acceptance flag or lost the concurrent note", patched[0])
			}
			updatedNote := "Ordinary note edit"
			stale.Note, stale.Fields = &updatedNote, []string{"note"}
			ordinary, err := board.SaveChanges(ctx, []GrossBoardEntry{stale}, "gross-test")
			if err != nil || ordinary[0].Note == nil || *ordinary[0].Note != updatedNote || ordinary[0].DriverRate != "2200.00" {
				t.Fatal("a flag outside the patch blocked or altered an ordinary edit", ordinary, err)
			}
			known = ordinary[0]
			known.DriverRate = "2300"
			known.AcceptDriverGross = false
			known.Fields = nil
			known = save(known)
			// Both slot tables, manual zero, and explicit amounts survive late imports.
			late := save(GrossBoardEntry{DriverID: driver, Date: "2026-10-20", LoadNumber: "LATE"})
			manual := save(GrossBoardEntry{DriverID: driver, Date: "2026-10-21", LoadNumber: "MANUAL", DriverRate: "2000"})
			zero := save(GrossBoardEntry{DriverID: driver, Date: "2026-10-22", LoadNumber: "ZERO", DriverRate: "0"})
			extra := save(GrossBoardEntry{DriverID: driver, Date: "2026-10-20", Slot: 1, LoadNumber: "EXTRA"})
			sync(9502, "LATE", "2100")
			sync(9503, "MANUAL", "1900")
			sync(9504, "ZERO", "1800")
			sync(9505, "EXTRA", "1700")
			if read(late.Date).DriverRate != "2100.00" || read(manual.Date).DriverRate != "2000.00" || read(zero.Date).DriverRate != "0.00" {
				t.Fatal("late fill/manual preservation")
			}
			var extraGross string
			if err = pool.QueryRow(ctx, `SELECT driver_rate::text FROM gross_board_extra_entries WHERE driver_id=$1 AND service_date=$2::date AND slot=1`, driver, extra.Date).Scan(&extraGross); err != nil || extraGross != "1700.00" {
				t.Fatal(extraGross, err)
			}
			filledVersion := read(late.Date).Version
			revisions := func() string {
				t.Helper()
				var value string
				if err := pool.QueryRow(ctx, `SELECT string_agg(table_name||':'||revision::text,',' ORDER BY table_name) FROM payroll_cache_revisions WHERE table_name IN ('loads','gross_board_entries','gross_board_extra_entries')`).Scan(&value); err != nil {
					t.Fatal(err)
				}
				return value
			}
			beforeRevision := revisions()
			sync(9502, "LATE", "2100")
			if revisions() != beforeRevision {
				t.Fatal("identical refresh invalidated payroll")
			}
			if read(late.Date).Version != filledVersion {
				t.Fatal("identical refresh rewrote the board")
			}
			sync(9502, "LATE", "1600")
			if read(late.Date).DriverRate != "2100.00" {
				t.Fatal("late fill repeated")
			}
			if read(late.Date).Version != filledVersion {
				t.Fatal("changed source rewrote saved gross")
			}
			missing := save(GrossBoardEntry{DriverID: driver, Date: "2026-10-23", LoadNumber: "MISSING"})
			sync(9506, "MISSING", "null")
			if read(missing.Date).DriverRate != "" {
				t.Fatal("missing fell back to revenue")
			}
			sync(9506, "MISSING", "0")
			if read(missing.Date).DriverRate != "0.00" {
				t.Fatal("source zero not used")
			}
			// Normal and normalized preparation expose the same independent comparison.
			week, _ := time.Parse(time.DateOnly, "2026-10-19")
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			report, err := readDriverPaySourceWeek(ctx, tx, week, driver)
			if err == nil {
				err = preparePayrollLoadFacts(ctx, tx)
			}
			if err != nil {
				t.Fatal(err)
			}
			normalized, err := readDriverPaySourceWeek(context.WithValue(ctx, payrollNormalizedFactsKey{}, true), tx, week, driver)
			if err != nil {
				t.Fatal(err)
			}
			for _, d := range normalized.Drivers {
				for _, l := range d.Loads {
					if l.LoadNumber == "KNOWN" && (l.SystemDriverGross != "2200" || l.DriverGross != "2300.00" || l.SystemOriginalRate != "2900.00" || l.SystemMiles != "600.00") {
						t.Fatal("normalized source mismatch", l)
					}
				}
			}
			// Frozen display evidence refreshes without modifying any saved amounts.
			loadID := 9501
			frozenReport := DriverPayWeek{Drivers: []DriverPayDriver{{Settlement: &PayrollSettlement{Finalized: true}, Loads: []DriverPayLoad{{LoadRecordID: &loadID, OriginalRate: "2800.00", DriverGross: "2300.00", TotalMiles: "541.00"}}}}}
			if err = decorateFrozenSourceComparisons(ctx, tx, &frozenReport); err != nil {
				t.Fatal(err)
			}
			fl := frozenReport.Drivers[0].Loads[0]
			if fl.SystemDriverGross != "2200" || fl.SystemOriginalRate != "2900.00" || fl.SystemMiles != "600.00" || fl.DriverGross != "2300.00" || fl.OriginalRate != "2800.00" || fl.TotalMiles != "541.00" {
				t.Fatal("frozen comparison", fl)
			}
			_ = tx.Rollback(ctx)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, d := range report.Drivers {
				for _, l := range d.Loads {
					if l.LoadNumber == "KNOWN" {
						found = true
						if l.DriverGross != "2300.00" || l.SystemDriverGross != "2200" {
							t.Fatal("payroll source", l)
						}
					}
				}
			}
			if !found {
				t.Fatal("missing payroll load")
			}
			frozen := save(GrossBoardEntry{DriverID: driver, Date: "2026-10-24", LoadNumber: "FROZEN"})
			exec(`INSERT INTO payroll_settlements(driver_id,week_start,finalized,report) VALUES($1,'2026-10-19',true,'{}')`, driver)
			sync(9507, "FROZEN", "1500")
			if read(frozen.Date).DriverRate != "" {
				t.Fatal("finalized blank filled")
			}
			input.Version = read(known.Date).Version
			if _, err = pay.AcceptPaySourceWithUndo(ctx, input, "gross-test"); err == nil {
				t.Fatal("finalized driver gross accepted")
			}
			if migrated {
				sync(9508, "LEGACY", "2300")
				sync(9509, "LEGACY-BLANK", "2300")
				var historical, blank string
				err = pool.QueryRow(ctx, `SELECT max(driver_rate::text) FILTER(WHERE load_number='LEGACY'),coalesce(max(driver_rate::text) FILTER(WHERE load_number='LEGACY-BLANK'),'') FROM gross_board_entries WHERE driver_id=$1`, driver).Scan(&historical, &blank)
				if err != nil || historical != "2800.00" || blank != "" {
					t.Fatal("migration changed history", historical, blank, err)
				}
			}
		})
	}
}
