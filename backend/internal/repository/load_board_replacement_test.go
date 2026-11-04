package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"mserp/internal/datatruck"
)

func TestMissingBoardReplacementDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		seedPayrollProjectionTest(t, ctx, pool)
		exec := func(sql string, args ...any) {
			t.Helper()
			if _, err := pool.Exec(ctx, sql, args...); err != nil {
				t.Fatal(err)
			}
		}
		exec(`INSERT INTO loads(id,load_id,status,customer_name,load_pay,total_pay,total_miles,pickup_time,delivery_time,raw_payload)
 SELECT id,number,status,'Broker',amount,amount,miles,pickup::timestamptz,'2026-10-06T12:00Z',
 '{"stops":[{"stop_type":"pickup","location":{"city":"Austin","state":"TX"}},{"stop_type":"delivery","location":{"city":"Dallas","state":"TX"}}]}'::jsonb
 FROM (VALUES (88301,'REPLACE-1','canceled',0,0,'2026-10-04T12:00Z'),(88302,'REPLACE-1','booked',1044,198,'2026-10-05T12:00Z'),(88303,'REPLACE-1','invoiced',1044,203,'2026-10-05T12:00Z'),
 (88310,'MANUAL-REPLACE','booked',900,190,'2026-10-04T12:00Z'),(88311,'MANUAL-REPLACE','invoiced',1044,203,'2026-10-05T12:00Z'),
 (88320,'AMBIGUOUS','booked',900,190,'2026-10-05T12:00Z'),(88321,'AMBIGUOUS','invoiced',1044,203,'2026-10-05T12:00Z'),(88322,'AMBIGUOUS','invoiced',1044,203,'2026-10-05T12:00Z'),
 (88330,'GONE','booked',900,190,'2026-10-05T12:00Z')) v(id,number,status,amount,miles,pickup)`)
		exec(`UPDATE gross_board_entries SET load_number='REPLACE-1',load_record_id=88301,driver_rate=0,original_rate=0,miles=0,entered_original_rate=NULL,entered_miles=NULL WHERE driver_id=$1`, projectionDriver)
		exec(`INSERT INTO gross_board_extra_entries(driver_id,service_date,slot,load_number,load_record_id,driver_rate,original_rate,miles) VALUES
 ($1,'2026-10-05',1,'REPLACE-1',88302,674.27,1044,198),($1,'2026-10-05',2,'REPLACE-1',88303,674.27,1044,203)`, projectionDriver)
		exec(`INSERT INTO gross_board_entries(driver_id,service_date,load_number,load_record_id,driver_rate,entered_original_rate,entered_miles,note) VALUES
 ($1,'2026-10-07','MANUAL-REPLACE',88310,900,1200,222,'Keep this correction'),
 ($1,'2026-10-08','AMBIGUOUS',88320,900,NULL,NULL,''),($1,'2026-10-09','GONE',88330,900,NULL,NULL,'')`, projectionDriver)
		r := NewLoadRepository(pool)
		ids := []int{88301, 88302, 88303, 88310, 88311, 88320, 88321, 88322}
		present := []int{88303, 88311, 88321, 88322}
		listener, err := pool.Acquire(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer listener.Release()
		if _, err = listener.Exec(ctx, `LISTEN mserp_gross_board`); err != nil {
			t.Fatal(err)
		}
		if err := r.RecordLoadSourceLookup(ctx, append(append([]int{}, ids...), 88330), present); err != nil {
			t.Fatal(err)
		}
		waitCtx, cancel := context.WithTimeout(ctx, time.Second)
		_, err = listener.Conn().WaitForNotification(waitCtx)
		cancel()
		if err != nil {
			t.Fatal("missing source did not notify the board", err)
		}
		if err = r.RecordLoadSourceLookup(ctx, ids, present); err != nil {
			t.Fatal(err)
		}
		waitCtx, cancel = context.WithTimeout(ctx, 100*time.Millisecond)
		_, err = listener.Conn().WaitForNotification(waitCtx)
		cancel()
		if err == nil {
			t.Fatal("unchanged presence unnecessarily refreshed the board")
		}
		if _, err = listener.Exec(ctx, `UNLISTEN mserp_gross_board`); err != nil {
			t.Fatal(err)
		}
		var live []datatruck.Load
		for _, s := range []struct {
			id     int
			number string
		}{{88303, "REPLACE-1"}, {88311, "MANUAL-REPLACE"}, {88321, "AMBIGUOUS"}, {88322, "AMBIGUOUS"}} {
			var l datatruck.Load
			body := fmt.Sprintf(`{"id":%d,"load_id":%q,"status":"invoiced","customer__company_name":"Broker","total_pay":1044,"total_miles":203,"pickup_time":"2026-10-05T12:00:00Z","delivery_time":"2026-10-06T12:00:00Z","stops":[{"stop_type":"pickup","location":{"city":"Austin","state":"TX"}},{"stop_type":"delivery","location":{"city":"Dallas","state":"TX"}}]}`, s.id, s.number)
			if err := json.Unmarshal([]byte(body), &l); err != nil {
				t.Fatal(err)
			}
			live = append(live, l)
		}
		candidates, err := r.MissingBoardReplacementIDs(ctx)
		if err != nil || !reflect.DeepEqual(candidates, ids) {
			t.Fatal(candidates, err)
		}
		// The accountant's finalized week must never be modified, even by a
		// candidate list fetched before finalization occurred.
		exec(`INSERT INTO payroll_settlements(driver_id,week_start,report) VALUES($1,'2026-10-05','{"accountant":"adjusted"}')`, projectionDriver)
		if candidates, err = r.MissingBoardReplacementIDs(ctx); err != nil || len(candidates) != 0 {
			t.Fatal("finalized week fetched", candidates, err)
		}
		if count, err := r.ReplaceMissingBoardLoads(ctx, ids, live); err != nil || count != 0 {
			t.Fatal("finalized week changed", count, err)
		}
		exec(`UPDATE payroll_settlements SET finalized=false WHERE driver_id=$1`, projectionDriver)
		// A finalized investor source also holds replacement.
		exec(`INSERT INTO investor_pay_weeks(truck_id,owner_id,week_start,edits,finalized,report) VALUES($1,'18200000-0000-0000-0000-000000000003','2026-10-05','{}',true,jsonb_build_object('loads',jsonb_build_array(jsonb_build_object('sourceDriverId',$2::text,'sourceDate','2026-10-05','sourceSlot',0))))`, projectionTruck, projectionDriver)
		if count, err := r.ReplaceMissingBoardLoads(ctx, ids[:3], live[:1]); err != nil || count != 0 {
			t.Fatal("finalized investor changed", count, err)
		}
		exec(`UPDATE investor_pay_weeks SET finalized=false WHERE truck_id=$1`, projectionTruck)
		// Concurrent provider reappearance invalidates the older absence proof.
		if err = r.RecordLoadSourceLookup(ctx, []int{88301}, []int{88301}); err != nil {
			t.Fatal(err)
		}
		if _, err = r.ReplaceMissingBoardLoads(ctx, ids, live); err == nil {
			t.Fatal("stale absence accepted")
		}
		if err = r.RecordLoadSourceLookup(ctx, []int{88301}, nil); err != nil {
			t.Fatal(err)
		}
		pay := NewDriverPayRepository(pool)
		if err = pay.EnablePayrollShadow(ctx); err != nil {
			t.Fatal(err)
		}
		week, _ := time.Parse(time.DateOnly, "2026-10-05")
		if err = pay.RequestPayrollPreparation(ctx, week, false); err != nil {
			t.Fatal(err)
		}
		if _, err = pay.PrepareNextPayrollWeek(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err = admin.Exec(ctx, `CREATE FUNCTION fail_replacement_publication() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'replacement publication failed'; END $$;
 CREATE TRIGGER fail_replacement_publication BEFORE INSERT OR UPDATE ON payroll_statement_current FOR EACH ROW EXECUTE FUNCTION fail_replacement_publication()`); err != nil {
			t.Fatal(err)
		}
		if _, err = r.ReplaceMissingBoardLoads(ctx, ids, live); err == nil {
			t.Fatal("failed publication committed")
		}
		var audits, oldID int
		if err = pool.QueryRow(ctx, `SELECT count(*) FROM gross_board_trip_history WHERE actor_id='datatruck_replacement'`).Scan(&audits); err != nil || audits != 0 {
			t.Fatal("audit escaped rollback", audits, err)
		}
		if err = pool.QueryRow(ctx, `SELECT load_record_id FROM gross_board_entries WHERE driver_id=$1 AND service_date='2026-10-05'`, projectionDriver).Scan(&oldID); err != nil || oldID != 88301 {
			t.Fatal("source escaped rollback", oldID, err)
		}
		if _, err = admin.Exec(ctx, `DROP TRIGGER fail_replacement_publication ON payroll_statement_current; DROP FUNCTION fail_replacement_publication()`); err != nil {
			t.Fatal(err)
		}
		if count, err := r.ReplaceMissingBoardLoads(ctx, ids, live); err != nil || count != 2 {
			t.Fatal("replacement failed", count, err)
		}
		board, err := NewGrossBoardRepository(pool).Get(ctx, week)
		if err != nil {
			t.Fatal(err)
		}
		found := map[string]int{}
		for _, e := range board.Entries {
			if e.Deleted || e.LoadNumber == "" {
				continue
			}
			found[e.LoadNumber]++
			switch e.LoadNumber {
			case "REPLACE-1":
				if e.LoadRecordID == nil || *e.LoadRecordID != 88303 || e.DriverRate != "1348.54" || e.OriginalRate != "1044.00" || e.Miles != "203.00" {
					t.Fatal(e)
				}
			case "MANUAL-REPLACE":
				if *e.LoadRecordID != 88311 || e.DriverRate != "900.00" || e.OriginalRate != "1200.00" || e.Miles != "222.00" || e.SystemOriginalRate != "1044.00" || e.Note == nil || *e.Note != "Keep this correction" {
					t.Fatal(e)
				}
			case "GONE", "AMBIGUOUS":
				if !e.SourceMissing || e.DriverRate != "900.00" {
					t.Fatal("missing/ambiguous evidence lost", e)
				}
			}
		}
		for _, n := range []string{"REPLACE-1", "MANUAL-REPLACE", "AMBIGUOUS", "GONE"} {
			if found[n] != 1 {
				t.Fatal(found)
			}
		}
		if err = pool.QueryRow(ctx, `SELECT count(*) FROM gross_board_trip_history WHERE actor_id='datatruck_replacement'`).Scan(&audits); err != nil || audits != 4 {
			t.Fatal(audits, err)
		}
		if count, err := r.ReplaceMissingBoardLoads(ctx, ids, live); err != nil || count != 0 {
			t.Fatal("replacement replayed", count, err)
		}
		var sources int
		if err = pool.QueryRow(ctx, `SELECT count(*) FROM loads WHERE id=ANY($1::int[])`, ids).Scan(&sources); err != nil || sources != len(ids) {
			t.Fatal("source history removed", sources, err)
		}
	})
}
