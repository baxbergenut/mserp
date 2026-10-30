package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"strings"
	"testing"
	"time"
)

func TestLoadWeekTransfersDatabase(t *testing.T) {
	dsn := os.Getenv("MSERP_GROSS_BOARD_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set local disposable MSERP_GROSS_BOARD_TEST_DATABASE_URL")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.HasSuffix(cfg.ConnConfig.Database, "_test") || (cfg.ConnConfig.Host != "localhost" && cfg.ConnConfig.Host != "127.0.0.1") {
		t.Fatal("local _test database required")
	}
	for _, migrated := range []bool{false, true} {
		t.Run(fmt.Sprint(migrated), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			admin, err := pgx.Connect(ctx, dsn)
			if err != nil {
				t.Fatal(err)
			}
			defer admin.Close(context.Background())
			schema := fmt.Sprintf("load_transfer_%d", time.Now().UnixNano())
			quoted := pgx.Identifier{schema}.Sanitize()
			if _, err = admin.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
				t.Fatal(err)
			}
			defer admin.Exec(context.Background(), "DROP SCHEMA "+quoted+" CASCADE")
			conf := cfg.Copy()
			conf.MaxConns = 1
			conf.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
			conf.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
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
			initial, _ := os.ReadFile("../../sql/init.sql")
			migration, _ := os.ReadFile("../../sql/159_load_week_transfers.sql")
			body := strings.ReplaceAll(string(initial), "\r\n", "\n")
			mig := strings.ReplaceAll(string(migration), "\r\n", "\n")
			if migrated {
				if strings.Count(body, mig) != 1 {
					t.Fatal("migration missing from init")
				}
				body = withoutAccountingCutoverMigration(t, strings.Replace(body, mig, "", 1))
			}
			exec(body)
			const driver = "91000000-0000-0000-0000-000000000001"
			const truck = "92000000-0000-0000-0000-000000000001"
			const owner = "93000000-0000-0000-0000-000000000001"
			const oldDispatcher = "94000000-0000-0000-0000-000000000001"
			exec(`INSERT INTO drivers(id,full_name,normalized_name,pay_type,pay_rate) VALUES($1,'Transfer Driver','transfer driver','gross_percentage',25)`, driver)
			exec(`INSERT INTO investors(id,full_name) VALUES($1,'Transfer Investor'); INSERT INTO trucks(id,unit_number) VALUES('`+truck+`','MOVE-1')`, owner)
			exec(`UPDATE trucks SET owner_id=$1 WHERE id=$2; UPDATE truck_ownership_history SET assigned_at='2026-01-01' WHERE truck_id=$2 AND owner_id=$1`, owner, truck)
			exec(`INSERT INTO truck_driver_assignments(truck_id,driver_id,assigned_at) VALUES($1,$2,'2026-01-01');INSERT INTO truck_settlement_terms(truck_id,owner_id,week_start,share_percent) VALUES($1,$3,'2026-01-05',90)`, truck, driver, owner)
			exec(`INSERT INTO dispatchers(id,full_name,normalized_name) VALUES($1,'Original Dispatcher','original dispatcher'),('94000000-0000-0000-0000-000000000002','New Dispatcher','new dispatcher')`, oldDispatcher)
			exec(`DELETE FROM driver_dispatcher_assignments WHERE driver_id=$1;INSERT INTO driver_dispatcher_assignments(driver_id,dispatcher_id,dispatcher_name,assigned_at,unassigned_at) VALUES($1,$2,'Original Dispatcher','2026-01-01','2026-10-26 00:00-04'),($1,'94000000-0000-0000-0000-000000000002','New Dispatcher','2026-10-26 00:00-04',NULL)`, driver, oldDispatcher)
			exec(`INSERT INTO loads(id,load_id,status,total_pay,load_pay,total_miles,truck_id,raw_payload) VALUES(9101,'MOVE-SUN','delivered',3000,3000,600,$1,'{}'),(9102,'MONDAY','dispatched',1000,1000,100,$1,'{}');INSERT INTO gross_board_entries(driver_id,service_date,load_number,load_record_id,driver_rate) VALUES($2,'2026-10-25','MOVE-SUN',9101,2800),($2,'2026-10-26','MONDAY',9102,900)`, truck, driver)
			if migrated {
				exec(mig)
				cutover, e := os.ReadFile("../../sql/160_accounting_cutover.sql")
				if e != nil {
					t.Fatal(e)
				}
				exec(string(cutover))
				costStart, e := os.ReadFile("../../sql/173_payroll_cost_start.sql")
				if e != nil {
					t.Fatal(e)
				}
				exec(string(costStart))
			}
			exec(`GRANT USAGE ON SCHEMA ` + quoted + ` TO mserp_app;GRANT SELECT,INSERT,UPDATE,DELETE ON ALL TABLES IN SCHEMA ` + quoted + ` TO mserp_app;SET ROLE mserp_app`)
			pay := NewDriverPayRepository(pool)
			board := NewGrossBoardRepository(pool)
			week, _ := time.Parse(time.DateOnly, "2026-10-19")
			next := week.AddDate(0, 0, 7)
			input := LoadWeekTransferInput{DriverID: driver, Date: "2026-10-25", Slot: 0, BoardVersion: 1, Mode: "dispatch_only", Reason: "Driver requested original settlement week"}
			transfer := func() {
				t.Helper()
				if e := pay.TransferLoadWeek(ctx, input, "accountant"); e != nil {
					t.Fatal(e)
				}
			}
			find := func(report DriverPayWeek, number string) *DriverPayLoad {
				for _, d := range report.Drivers {
					for _, l := range d.Loads {
						if l.LoadNumber == number {
							return &l
						}
					}
				}
				return nil
			}
			read := func(investor bool, w time.Time) DriverPayWeek {
				t.Helper()
				var out DriverPayWeek
				var e error
				if investor {
					out, e = pay.InvestorStatement(ctx, w, "")
				} else {
					out, e = pay.Get(ctx, w)
				}
				if e != nil {
					t.Fatal(e)
				}
				return out
			}
			listener, listenErr := pgx.Connect(ctx, dsn)
			if listenErr != nil {
				t.Fatal(listenErr)
			}
			defer listener.Close(context.Background())
			if _, listenErr = listener.Exec(ctx, `LISTEN mserp_gross_board`); listenErr != nil {
				t.Fatal(listenErr)
			}
			var publisher uint32
			if listenErr = pool.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&publisher); listenErr != nil {
				t.Fatal(listenErr)
			}
			transfer()
			notificationCtx, stopNotifications := context.WithTimeout(ctx, 3*time.Second)
			for {
				notice, waitErr := listener.WaitForNotification(notificationCtx)
				if waitErr != nil {
					stopNotifications()
					t.Fatal("transfer did not announce its committed Gross Board change", waitErr)
				}
				if notice.PID == publisher {
					break
				}
			}
			stopNotifications()
			for _, investor := range []bool{false, true} {
				if l := find(read(investor, week), "MOVE-SUN"); l == nil || l.Date != "2026-10-25" || l.TransferVersion != 1 {
					t.Fatal("settlement should stay", investor, l)
				}
				if l := find(read(investor, next), "MOVE-SUN"); l != nil {
					t.Fatal("double counted", investor)
				}
			}
			previous, e := board.Get(ctx, week)
			if e != nil {
				t.Fatal(e)
			}
			if len(previous.Entries) != 1 || !previous.Entries[0].TransferOut || !previous.Entries[0].Duplicate || previous.Entries[0].OriginalRate != "" {
				t.Fatal("source marker must not count", previous)
			}
			following, e := board.Get(ctx, next)
			if e != nil {
				t.Fatal(e)
			}
			if len(following.Entries) != 2 {
				t.Fatal("Monday collision", following)
			}
			for _, entry := range following.Entries {
				if entry.LoadNumber == "MOVE-SUN" && (entry.Slot != 1 || entry.Date != "2026-10-26" || entry.CreditedDispatcherID != oldDispatcher || entry.SourceDate != "2026-10-25") {
					t.Fatal("lost original identity/credit", entry)
				}
			}
			if e = pay.TransferLoadWeek(ctx, input, "accountant"); e != ErrDriverPayConflict {
				t.Fatal("stale transfer accepted", e)
			}
			if e = board.Save(ctx, []GrossBoardEntry{{DriverID: driver, Date: "2026-10-26", Slot: 1, Version: 0, LoadNumber: "OTHER"}}); e == nil {
				t.Fatal("reserved destination overwritten")
			}
			input.TransferVersion = 1
			input.Mode = "everyone"
			transfer()
			for _, investor := range []bool{false, true} {
				if l := find(read(investor, week), "MOVE-SUN"); l != nil {
					t.Fatal("original settlement still contains transferred load", investor)
				}
				if l := find(read(investor, next), "MOVE-SUN"); l == nil || l.Date != "2026-10-26" || l.SourceDate != "2026-10-25" {
					t.Fatal("settlement not moved", investor, l)
				}
			}
			nextInvestor := read(true, next)
			for _, d := range nextInvestor.Drivers {
				if d.ID == truck {
					for _, c := range d.AutoCharges {
						if c.Source == "driver:"+driver && c.Amount != "-925.00" {
							t.Fatal("hired labor out of sync", c)
						}
					}
				}
			}
			// A finalized destination prevents restoring financial placement, but allows
			// a dispatcher-only decision when monetary placement stays unchanged.
			frozen := read(false, next).Drivers[0]
			frozenJSON, _ := json.Marshal(frozen)
			exec(`INSERT INTO payroll_settlements(driver_id,week_start,report) VALUES($1,'2026-10-26',$2)`, driver, string(frozenJSON))
			input.TransferVersion = 2
			input.Mode = "dispatch_only"
			if e = pay.TransferLoadWeek(ctx, input, "accountant"); e == nil {
				t.Fatal("finalized destination moved")
			}
			input.Mode = "everyone"
			transfer()
			decorated := find(read(false, next), "MOVE-SUN")
			if decorated == nil || decorated.TransferVersion != 3 {
				t.Fatal("frozen placement metadata stale", decorated)
			}
			exec(`UPDATE payroll_settlements SET finalized=false WHERE driver_id=$1`, driver)
			input.TransferVersion = 3
			input.Mode = "restore"
			transfer()
			for _, investor := range []bool{false, true} {
				if find(read(investor, week), "MOVE-SUN") == nil || find(read(investor, next), "MOVE-SUN") != nil {
					t.Fatal("restore out of sync", investor)
				}
			}
			var history int
			if e = pool.QueryRow(ctx, `SELECT count(*) FROM gross_board_transfer_history`).Scan(&history); e != nil || history != 4 {
				t.Fatal("missing audit", history, e)
			}
			input.TransferVersion = 0
			input.Mode = "dispatch_only"
			if e = pay.TransferLoadWeek(ctx, input, "accountant"); e != ErrDriverPayConflict {
				t.Fatal("restored placement lost its concurrency version", e)
			}
			// Saturday extra slots retain their source identity while using a
			// different, unoccupied Monday slot. Investor finalization fences
			// the same financial move as driver finalization.
			exec(`INSERT INTO loads(id,load_id,status,total_pay,load_pay,total_miles,truck_id,raw_payload) VALUES(9103,'SAT-EXTRA','delivered',500,500,100,$1,'{}');INSERT INTO gross_board_extra_entries(driver_id,service_date,slot,load_number,load_record_id,driver_rate) VALUES($2,'2026-10-24',7,'SAT-EXTRA',9103,400)`, truck, driver)
			extra := LoadWeekTransferInput{DriverID: driver, Date: "2026-10-24", Slot: 7, BoardVersion: 1, Mode: "everyone", Reason: "Saturday delivery after cutoff"}
			investorSnapshot := read(true, next).Drivers[0]
			investorJSON, _ := json.Marshal(investorSnapshot)
			exec(`INSERT INTO investor_pay_weeks(truck_id,week_start,owner_id,edits,report,finalized) VALUES($1,'2026-10-26',$2,'{}',$3,true)`, truck, owner, string(investorJSON))
			if e = pay.TransferLoadWeek(ctx, extra, "accountant"); e == nil {
				t.Fatal("finalized investor destination moved")
			}
			exec(`UPDATE investor_pay_weeks SET finalized=false WHERE truck_id=$1 AND week_start='2026-10-26'`, truck)
			if e = pay.TransferLoadWeek(ctx, extra, "accountant"); e != nil {
				t.Fatal(e)
			}
			for _, investor := range []bool{false, true} {
				l := find(read(investor, next), "SAT-EXTRA")
				if l == nil || l.SourceSlot != 7 || l.Slot != 1 || l.SourceDate != "2026-10-24" {
					t.Fatal("extra source identity lost", investor, l)
				}
			}
			if _, e = pool.Exec(ctx, `UPDATE gross_board_extra_entries SET driver_rate=99 WHERE driver_id=$1 AND service_date='2026-10-24' AND slot=7`, driver); e == nil {
				t.Fatal("database source fence bypassed")
			}
			extra.TransferVersion = 1
			extra.Mode = "restore"
			if e = pay.TransferLoadWeek(ctx, extra, "accountant"); e != nil {
				t.Fatal(e)
			}
			// Reusing an empty/cleared destination must produce one projected
			// entry while retaining its raw version for a later restore.
			for i, kind := range []string{"cleared base", "deleted extra", "empty extra"} {
				sourceDay := next.AddDate(0, 0, 6+7*i)
				mondayDay := sourceDay.AddDate(0, 0, 1)
				targetSlot := 0
				number := fmt.Sprintf("EMPTY-DEST-%d", i)
				recordID := 9201 + i
				exec(`INSERT INTO loads(id,load_id,status,total_pay,load_pay,total_miles,truck_id,raw_payload) VALUES($1,$2,'delivered',500,500,100,$3,'{}');INSERT INTO gross_board_entries(driver_id,service_date,load_number,load_record_id,driver_rate) VALUES($4,$5,$2,$1,400)`, recordID, number, truck, driver, sourceDay)
				if i == 0 {
					exec(`INSERT INTO gross_board_entries(driver_id,service_date,load_number,version,note) VALUES($1,$2,'',25,'Cleared draft')`, driver, mondayDay)
				} else {
					targetSlot = 1
					exec(`INSERT INTO gross_board_entries(driver_id,service_date,day_status) VALUES($1,$2,'SHOP');INSERT INTO gross_board_extra_entries(driver_id,service_date,slot,load_number,deleted,version,note) VALUES($1,$2,1,'',$3,25,'Cleared draft')`, driver, mondayDay, i == 1)
				}
				move := LoadWeekTransferInput{DriverID: driver, Date: sourceDay.Format(time.DateOnly), Slot: 0, BoardVersion: 1, Mode: "everyone", Reason: kind}
				if e = pay.TransferLoadWeek(ctx, move, "accountant"); e != nil {
					t.Fatal(kind, e)
				}
				projected, readErr := board.Get(ctx, mondayDay)
				if readErr != nil {
					t.Fatal(kind, readErr)
				}
				count := 0
				for _, entry := range projected.Entries {
					if entry.Date == mondayDay.Format(time.DateOnly) && entry.Slot == targetSlot {
						count++
						if entry.LoadNumber != number || entry.Deleted || entry.TransferVersion == 0 {
							t.Fatal(kind, "placeholder hid transfer", entry)
						}
					}
				}
				if count != 1 {
					t.Fatal(kind, "duplicate projected destination", count)
				}
				for _, investor := range []bool{false, true} {
					l := find(read(investor, mondayDay), number)
					if l == nil || l.Date != mondayDay.Format(time.DateOnly) {
						t.Fatal(kind, "settlement missing", investor, l)
					}
				}
				additional := GrossBoardEntry{DriverID: driver, Date: mondayDay.Format(time.DateOnly), Slot: targetSlot + 1, LoadNumber: "ADDED-LOAD", OriginalRate: "100", DriverRate: "90", Miles: "10"}
				if e = board.Save(ctx, []GrossBoardEntry{additional}); e != nil {
					t.Fatal(kind, "cannot add beside transfer", e)
				}
				move.Mode = "restore"
				move.TransferVersion = 1
				if e = pay.TransferLoadWeek(ctx, move, "accountant"); e != nil {
					t.Fatal(kind, e)
				}
				restored, readErr := board.Get(ctx, mondayDay)
				if readErr != nil {
					t.Fatal(kind, readErr)
				}
				count = 0
				for _, entry := range restored.Entries {
					if entry.Date == mondayDay.Format(time.DateOnly) && entry.Slot == targetSlot {
						count++
						if entry.LoadNumber != "" || entry.Version != 25 || entry.TransferVersion != 0 || entry.Note == nil || *entry.Note != "Cleared draft" || entry.Deleted != (i == 1) {
							t.Fatal(kind, "restore lost raw placeholder", entry)
						}
					}
				}
				if count != 1 {
					t.Fatal(kind, "restore destination count", count)
				}
			}
			// A pre-existing frozen original statement can receive dispatcher-only
			// placement without rewriting its stored report.
			original := read(false, week).Drivers[0]
			originalJSON, _ := json.Marshal(original)
			exec(`INSERT INTO payroll_settlements(driver_id,week_start,report) VALUES($1,'2026-10-19',$2)`, driver, string(originalJSON))
			input.TransferVersion = 4
			input.Mode = "dispatch_only"
			transfer()
			var stored []byte
			if e = pool.QueryRow(ctx, `SELECT report FROM payroll_settlements WHERE driver_id=$1 AND week_start='2026-10-19'`, driver).Scan(&stored); e != nil {
				t.Fatal(e)
			}
			var recorded DriverPayDriver
			if json.Unmarshal(stored, &recorded) != nil || recorded.Loads[0].TransferVersion != 0 {
				t.Fatal("frozen report rewritten")
			}
			if l := find(read(false, week), "MOVE-SUN"); l == nil || l.TransferVersion == 0 {
				t.Fatal("frozen statement cannot manage dispatch transfer", l)
			}
		})
	}
}
