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
)

func TestPayLoadPlacementsDatabase(t *testing.T) {
	dsn := os.Getenv("MSERP_GROSS_BOARD_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set local disposable MSERP_GROSS_BOARD_TEST_DATABASE_URL")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.HasSuffix(cfg.ConnConfig.Database, "_test") || (cfg.ConnConfig.Host != "localhost" && cfg.ConnConfig.Host != "127.0.0.1") {
		t.Fatal("local _test database required")
	}
	for _, upgrade := range []bool{false, true} {
		t.Run(fmt.Sprint(upgrade), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			admin, e := pgx.Connect(ctx, dsn)
			if e != nil {
				t.Fatal(e)
			}
			defer admin.Close(context.Background())
			schema := fmt.Sprintf("pay_load_placement_%d", time.Now().UnixNano())
			quoted := pgx.Identifier{schema}.Sanitize()
			if _, e = admin.Exec(ctx, "CREATE SCHEMA "+quoted); e != nil {
				t.Fatal(e)
			}
			defer admin.Exec(context.Background(), "DROP SCHEMA "+quoted+" CASCADE")
			conf := cfg.Copy()
			conf.MaxConns = 2
			conf.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
			conf.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
			pool, e := pgxpool.NewWithConfig(ctx, conf)
			if e != nil {
				t.Fatal(e)
			}
			defer pool.Close()
			exec := func(q string, args ...any) {
				t.Helper()
				if _, e := pool.Exec(ctx, q, args...); e != nil {
					t.Fatal(e)
				}
			}
			initial, e := os.ReadFile("../../sql/init.sql")
			if e != nil {
				t.Fatal(e)
			}
			migration, e := os.ReadFile("../../sql/174_payroll_load_placements.sql")
			if e != nil {
				t.Fatal(e)
			}
			body, mig := strings.ReplaceAll(string(initial), "\r\n", "\n"), strings.ReplaceAll(string(migration), "\r\n", "\n")
			exclusionIndex, e := os.ReadFile("../../sql/178_payroll_load_exclusion_weeks.sql")
			if e != nil {
				t.Fatal(e)
			}
			indexSQL := strings.ReplaceAll(string(exclusionIndex), "\r\n", "\n")
			if upgrade {
				if strings.Count(body, mig) != 1 {
					t.Fatal("migration missing from init")
				}
				body = strings.Replace(body, mig, "", 1)
				body = strings.Replace(body, indexSQL, "", 1)
			}
			exec(body)
			if upgrade {
				exec(mig)
				exec(indexSQL)
			}
			const driver = "a1000000-0000-0000-0000-000000000001"
			const truck = "a2000000-0000-0000-0000-000000000001"
			const owner = "a3000000-0000-0000-0000-000000000001"
			const actor = "a4000000-0000-0000-0000-000000000001"
			exec(`INSERT INTO app_users(id,username,password_hash) VALUES($1,'placement-test',crypt('synthetic-password',gen_salt('bf')))`, actor)
			exec(`INSERT INTO drivers(id,full_name,normalized_name,pay_type,pay_rate) VALUES($1,'Placement Driver','placement driver','gross_percentage',25);
 INSERT INTO investors(id,full_name) VALUES($2,'Placement Investor'); INSERT INTO trucks(id,unit_number) VALUES($3,'PLACEMENT');
 UPDATE trucks SET owner_id=$2 WHERE id=$3; UPDATE truck_ownership_history SET assigned_at='2026-01-01' WHERE truck_id=$3;
 INSERT INTO truck_driver_assignments(truck_id,driver_id,assigned_at) VALUES($3,$1,'2026-01-01');
 INSERT INTO truck_settlement_terms(truck_id,owner_id,week_start,share_percent) VALUES($3,$2,'2026-01-05',90)`, driver, owner, truck)
			exec(`INSERT INTO loads(id,load_id,status,load_pay,total_pay,total_miles,pickup_time,delivery_time,raw_payload) VALUES
 (8101,'OLD-DELIVERED','delivered',3000,3000,600,'2026-09-28','2026-09-29','{"trip":{"mile":550,"empty_mile":50},"stops":[{"stop_type":"pickup","ordering":1,"location":{"city":"First","state":"OH"}},{"stop_type":"delivery","ordering":2,"location":{"city":"Last","state":"PA"}}]}'),
 (8102,'NOT-READY','dispatched',1000,1000,100,'2026-10-20','2026-10-21','{}'),
 (8103,'UNPLANNED','invoiced',900,900,200,'2026-10-02','2026-10-03','{}'),
 (8104,'DUPLICATE','delivered',100,100,10,'2026-10-02','2026-10-03','{}'),
 (8105,'DUPLICATE','delivered',100,100,10,'2026-10-02','2026-10-03','{}');
 INSERT INTO gross_board_entries(driver_id,service_date,load_number,load_record_id,driver_rate) VALUES($1,'2026-09-28','OLD-DELIVERED',NULL,2800),($1,'2026-10-20','NOT-READY',8102,900)`, driver)
			exec(`GRANT USAGE ON SCHEMA ` + quoted + ` TO mserp_app; GRANT SELECT,INSERT,UPDATE,DELETE ON ALL TABLES IN SCHEMA ` + quoted + ` TO mserp_app`)
			// Every application connection uses the real restricted runtime role.
			pool.Close()
			conf.ConnConfig.RuntimeParams["role"] = "mserp_app"
			conf.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeCacheStatement
			pool, e = pgxpool.NewWithConfig(ctx, conf)
			if e != nil {
				t.Fatal(e)
			}
			defer pool.Close()
			pay := NewDriverPayRepository(pool)
			board := NewGrossBoardRepository(pool)
			week, _ := time.Parse(time.DateOnly, "2026-10-19")
			old := week.AddDate(0, 0, -21)
			read := func(kind string, w time.Time) DriverPayWeek {
				t.Helper()
				var r DriverPayWeek
				var e error
				if kind == "investor" {
					r, e = pay.InvestorStatement(ctx, w, truck)
				} else {
					r, e = pay.GetDriverWeek(ctx, w, driver)
				}
				if e != nil {
					t.Fatal(e)
				}
				return r
			}
			find := func(r DriverPayWeek, n string) *DriverPayLoad {
				for _, d := range r.Drivers {
					for _, l := range d.Loads {
						if l.LoadNumber == n {
							return &l
						}
					}
				}
				return nil
			}
			before, e := board.Get(ctx, old)
			if e != nil {
				t.Fatal(e)
			}
			beforeJSON, _ := json.Marshal(before)
			// A missed allocation may be caught up even when its original week's
			// frozen paycheck did NOT contain it. That snapshot stays byte-for-byte.
			original := read("driver", old).Drivers[0]
			original.Loads = []DriverPayLoad{}
			originalJSON, _ := json.Marshal(original)
			exec(`INSERT INTO payroll_settlements(driver_id,week_start,report) VALUES($1,$2::date,$3)`, driver, old.Format(time.DateOnly), json.RawMessage(originalJSON))
			add := PayLoadPlacementInput{DriverID: driver, WeekStart: week.Format(time.DateOnly), Action: "add", LoadNumber: "OLD-DELIVERED"}
			if e = pay.PlacePayLoad(ctx, "driver", add, "test"); e != nil {
				t.Fatal(e)
			}
			for _, kind := range []string{"driver", "investor"} {
				l := find(read(kind, week), "OLD-DELIVERED")
				if l == nil || !l.PayrollSupplement || l.SourceDate != "2026-09-28" || l.DriverGross != "2800.00" || l.TotalMiles != "600.00" || len(l.Issues) != 0 {
					t.Fatalf("missing source details %s: %+v", kind, l)
				}
				if kind == "driver" && l.Fee != "700.00" || kind == "investor" && (l.Fee != "2520.00" || l.DriverFee != "700.00") {
					t.Fatalf("incorrect pay %s: %+v", kind, l)
				}
				if find(read(kind, old), "OLD-DELIVERED") != nil {
					t.Fatal("recognized load paid twice", kind)
				}
			}
			after, e := board.Get(ctx, old)
			if e != nil {
				t.Fatal(e)
			}
			afterJSON, _ := json.Marshal(after)
			if string(beforeJSON) != string(afterJSON) {
				t.Fatal("payroll inclusion changed Gross Board")
			}
			var stillFrozen []byte
			if e = pool.QueryRow(ctx, `SELECT report FROM payroll_settlements WHERE driver_id=$1 AND week_start=$2::date`, driver, old.Format(time.DateOnly)).Scan(&stillFrozen); e != nil {
				t.Fatal(e)
			}
			var expected, actual any
			_ = json.Unmarshal(originalJSON, &expected)
			_ = json.Unmarshal(stillFrozen, &actual)
			checkExpected, _ := json.Marshal(expected)
			checkActual, _ := json.Marshal(actual)
			if string(checkExpected) != string(checkActual) {
				t.Fatal("catch-up rewrote original frozen statement")
			}
			// A later duplicate display number cannot revive the captured legacy
			// number-only board slot alongside its explicit payroll recognition.
			exec(`INSERT INTO loads(id,load_id,status,load_pay,total_pay,total_miles,raw_payload) VALUES(8107,'OLD-DELIVERED','delivered',1,1,1,'{}')`)
			if find(read("investor", old), "OLD-DELIVERED") != nil {
				t.Fatal("ambiguous source identity revived an already recognized allocation")
			}
			exec(`DELETE FROM loads WHERE id=8107`)
			if e = pay.PlacePayLoad(ctx, "driver", add, "test"); e == nil {
				t.Fatal("duplicate inclusion accepted")
			}
			add.LoadNumber = "DUPLICATE"
			if e = pay.PlacePayLoad(ctx, "driver", add, "test"); e == nil {
				t.Fatal("ambiguous number accepted")
			}
			add.LoadNumber = "NOT-READY"
			if e = pay.PlacePayLoad(ctx, "driver", add, "test"); e == nil {
				t.Fatal("undelivered load accepted")
			}
			add.LoadNumber = "MISSING"
			if e = pay.PlacePayLoad(ctx, "driver", add, "test"); e == nil {
				t.Fatal("missing load accepted")
			}
			flagged := find(read("investor", week), "NOT-READY")
			if flagged == nil || !flagged.NotReady {
				t.Fatal("missing flag")
			}
			remove := PayLoadPlacementInput{DriverID: truck, WeekStart: add.WeekStart, Action: "remove", LoadRecordID: 8102, CommentKey: flagged.CommentKey, BoardVersion: flagged.BoardVersion}
			if e = pay.PlacePayLoad(ctx, "investor", remove, "test"); e != nil {
				t.Fatal(e)
			}
			for _, kind := range []string{"driver", "investor"} {
				if find(read(kind, week), "NOT-READY") != nil {
					t.Fatal("removed load remains", kind)
				}
			}
			b, e := board.Get(ctx, week)
			if e != nil || len(b.Entries) != 1 || b.Entries[0].LoadNumber != "NOT-READY" {
				t.Fatal("removal changed Gross Board", e)
			}
			if e = pay.PlacePayLoad(ctx, "investor", remove, "test"); e == nil {
				t.Fatal("stale removal accepted")
			}
			// Removing an extra row in another week must not erase the native
			// Monday allocation (the reported cross-week case).
			next := week.AddDate(0, 0, 7)
			exec(`INSERT INTO driver_pay_weeks(driver_id,week_start) VALUES($1,$2::date)`, driver, next.Format(time.DateOnly))
			exec(`INSERT INTO loads(id,load_id,status,load_pay,total_pay,total_miles,pickup_time,raw_payload) VALUES(8109,'MONDAY-RESTORE','invoiced',1000,1000,351,'2026-10-26','{}')`)
			exec(`INSERT INTO gross_board_entries(driver_id,service_date,load_number,load_record_id,driver_rate) VALUES($1,'2026-10-26','MONDAY-RESTORE',8109,1000)`, driver)
			cross := PayLoadPlacementInput{DriverID: driver, WeekStart: week.Format(time.DateOnly), Action: "add", LoadNumber: "MONDAY-RESTORE"}
			if e = pay.PlacePayLoad(ctx, "driver", cross, "test"); e != nil {
				t.Fatal(e)
			}
			full, e := pay.Get(ctx, week)
			if e != nil {
				t.Fatal(e)
			}
			if _, e = pay.Settle(ctx, week, driver, full.Revision, actor, "", false, true); e != nil {
				t.Fatal(e)
			}
			if find(read("driver", next), "MONDAY-RESTORE") != nil {
				t.Fatal("frozen previous-week load was also paid in its native week")
			}
			full, e = pay.Get(ctx, week)
			if e != nil {
				t.Fatal(e)
			}
			if _, e = pay.Settle(ctx, week, driver, full.Revision, actor, "Return this load to its original Monday", true, false); e != nil {
				t.Fatal(e)
			}
			moved := find(read("driver", week), "MONDAY-RESTORE")
			cross.Action = "remove"
			cross.LoadRecordID = 8109
			cross.CommentKey = moved.CommentKey
			cross.PlacementVersion = moved.PayrollPlacementVersion
			if e = pay.PlacePayLoad(ctx, "driver", cross, "test"); e != nil {
				t.Fatal(e)
			}
			for _, kind := range []string{"driver", "investor"} {
				if find(read(kind, week), "MONDAY-RESTORE") != nil {
					t.Fatal("extra row removal did not persist", kind)
				}
				l := find(read(kind, next), "MONDAY-RESTORE")
				if l == nil || l.PayrollSupplement || l.Date != "2026-10-26" || l.TotalMiles != "351.00" {
					t.Fatal("native Monday allocation hidden by another week's removal", kind, l)
				}
			}
			// Preserve a prior explicit removal after adding/removing that same
			// load in a later week; its original week must remain excluded.
			exec(`UPDATE loads SET status='delivered' WHERE id=8102`)
			later := PayLoadPlacementInput{DriverID: driver, WeekStart: next.Format(time.DateOnly), Action: "add", LoadNumber: "NOT-READY"}
			if e = pay.PlacePayLoad(ctx, "driver", later, "test"); e != nil {
				t.Fatal(e)
			}
			moved = find(read("driver", next), "NOT-READY")
			later.Action = "remove"
			later.LoadRecordID = 8102
			later.CommentKey = moved.CommentKey
			later.PlacementVersion = moved.PayrollPlacementVersion
			if e = pay.PlacePayLoad(ctx, "driver", later, "test"); e != nil {
				t.Fatal(e)
			}
			for _, kind := range []string{"driver", "investor"} {
				for _, w := range []time.Time{week, next} {
					if find(read(kind, w), "NOT-READY") != nil {
						t.Fatal("earlier explicit removal lost after a later removal", kind, w)
					}
				}
			}
			add.LoadNumber = "UNPLANNED"
			add.DriverID = truck
			if e = pay.PlacePayLoad(ctx, "investor", add, "test"); e != nil {
				t.Fatal(e)
			}
			l := find(read("driver", week), "UNPLANNED")
			if l == nil || l.DriverGross != "" || l.Fee != "" {
				t.Fatal("unreviewed gross inferred", l)
			}
			gross := PayLoadPlacementInput{DriverID: driver, WeekStart: add.WeekStart, Action: "gross", LoadRecordID: 8103, CommentKey: l.CommentKey, PlacementVersion: l.PayrollPlacementVersion, DriverGross: "800"}
			if e = pay.PlacePayLoad(ctx, "driver", gross, "test"); e != nil {
				t.Fatal(e)
			}
			if e = pay.PlacePayLoad(ctx, "driver", gross, "test"); e != ErrDriverPayConflict {
				t.Fatal("stale gross accepted", e)
			}
			if l = find(read("driver", week), "UNPLANNED"); l == nil || l.Fee != "200.00" {
				t.Fatal("gross edit did not recalculate", l)
			}
			// Reuse actual split allocations; never select a leg by provider name.
			exec(`INSERT INTO loads(id,load_id,status,total_pay,load_pay,total_miles,raw_payload) VALUES(8106,'SPLIT-CATCHUP','delivered',1813,1813,555,$1::jsonb)`, splitFixture)
			exec(`INSERT INTO gross_board_entries(driver_id,service_date,load_number,load_record_id,load_trip_id,driver_rate) VALUES($1,'2026-10-05','SPLIT-CATCHUP',8106,101,450),($1,'2026-10-06','SPLIT-CATCHUP',8106,102,1200)`, driver)
			split := PayLoadPlacementInput{DriverID: driver, WeekStart: add.WeekStart, Action: "add", LoadNumber: "SPLIT-CATCHUP"}
			e = pay.PlacePayLoad(ctx, "driver", split, "test")
			choice, ok := e.(*PayLoadChoiceRequired)
			if !ok || len(choice.Choices) != 2 {
				t.Fatal("ambiguous split not held for explicit selection", e)
			}
			split.LoadTripID = choice.Choices[0].LoadTripID
			split.SourceDriverID = driver
			if e = pay.PlacePayLoad(ctx, "driver", split, "test"); e != nil {
				t.Fatal(e)
			}
			if l = find(read("driver", week), "SPLIT-CATCHUP"); l == nil || l.LoadTripID == nil || *l.LoadTripID != 101 || l.OriginalRate != "500" || l.Fee != "112.50" {
				t.Fatal("wrong split allocation", l)
			}
			// A former solo operator now paired under another main driver cannot
			// acquire a separate member paycheck through historical recognition.
			const member = "a1000000-0000-0000-0000-000000000002"
			exec(`INSERT INTO drivers(id,full_name,normalized_name,pay_type,pay_rate) VALUES($1,'Placement Partner','placement partner','cpm',0.75)`, member)
			exec(`INSERT INTO driver_teams(main_driver_id,team_driver_id,start_week) VALUES($1,$2,'2026-10-19')`, driver, member)
			exec(`INSERT INTO loads(id,load_id,status,load_pay,total_pay,total_miles,raw_payload) VALUES(8108,'FORMER-SOLO','delivered',100,100,10,'{}')`)
			exec(`INSERT INTO gross_board_entries(driver_id,service_date,load_number,load_record_id,driver_rate) VALUES($1,'2026-09-21','FORMER-SOLO',8108,90)`, member)
			teamAdd := PayLoadPlacementInput{DriverID: driver, WeekStart: add.WeekStart, Action: "add", LoadNumber: "FORMER-SOLO"}
			if e = pay.PlacePayLoad(ctx, "driver", teamAdd, "test"); e == nil || !strings.Contains(e.Error(), "team change") {
				t.Fatal("team catch-up was not held for review", e)
			}
			var writes int
			if e = pool.QueryRow(ctx, `SELECT count(*) FROM payroll_load_placements WHERE load_record_id=8108`).Scan(&writes); e != nil || writes != 0 {
				t.Fatal("team review committed a payroll row", writes, e)
			}
			// Frozen counterparts block removal even when the selected driver view is open.
			report := read("investor", week)
			if _, e = pay.SettleInvestor(ctx, week, truck, report.Revision, actor, "", false, true); e != nil {
				t.Fatal(e)
			}
			l = find(read("driver", week), "OLD-DELIVERED")
			remove = PayLoadPlacementInput{DriverID: driver, WeekStart: add.WeekStart, Action: "remove", LoadRecordID: 8101, CommentKey: l.CommentKey, PlacementVersion: l.PayrollPlacementVersion}
			if e = pay.PlacePayLoad(ctx, "driver", remove, "test"); e == nil {
				t.Fatal("frozen investor load removed")
			}
			if _, e = pool.Exec(ctx, `UPDATE payroll_load_placements SET driver_gross=1 WHERE load_record_id=8101`); e == nil {
				t.Fatal("database guard allowed a frozen load change")
			}
			// Global finalization check also prevents a second payment in a different week.
			add.DriverID = driver
			add.WeekStart = "2026-10-26"
			add.LoadNumber = "OLD-DELIVERED"
			// Add a saved row so that the future statement is visible without other work.
			exec(`INSERT INTO driver_pay_weeks(driver_id,week_start) VALUES($1,'2026-10-26') ON CONFLICT DO NOTHING`, driver)
			if e = pay.PlacePayLoad(ctx, "driver", add, "test"); e == nil {
				t.Fatal("finalized load added in another week")
			}
		})
	}
}

func TestPayLoadReady(t *testing.T) {
	for _, s := range []string{"delivered", " INVOICED "} {
		if !payLoadReady(s) {
			t.Fatal(s)
		}
	}
	for _, s := range []string{"", "dispatched", "cancelled", "in_transit"} {
		if payLoadReady(s) {
			t.Fatal(s)
		}
	}
}
