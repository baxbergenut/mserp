package repository

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"mserp/internal/datatruck"
)

func TestUnlinkedBoardReconciliationDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		seedPayrollProjectionTest(t, ctx, pool)
		exec := func(sql string, args ...any) {
			t.Helper()
			if _, err := pool.Exec(ctx, sql, args...); err != nil {
				t.Fatal(err)
			}
		}
		exec(`INSERT INTO loads(id,load_id,status,customer_name,load_pay,total_pay,total_miles,pickup_time,delivery_time,raw_payload)
   SELECT id,'UNLINKED','invoiced','Broker',1044,1044,203,'2026-10-05T12:00Z','2026-10-06T12:00Z', '{"stops":[{"stop_type":"pickup","location":{"city":"Austin","state":"TX"}},{"stop_type":"delivery","location":{"city":"Dallas","state":"TX"}}]}'::jsonb FROM (VALUES(88401),(88402)) v(id)`)
		exec(`UPDATE gross_board_entries SET load_number='UNLINKED',load_record_id=NULL,driver_rate=900,original_rate=1200,miles=222,entered_original_rate=NULL,entered_miles=NULL,note='Keep manual details' WHERE driver_id=$1`, projectionDriver)
		r := NewLoadRepository(pool)
		ids := []int{88401, 88402}
		var source datatruck.Load
		if err := json.Unmarshal([]byte(`{"id":88402,"load_id":"UNLINKED","status":"invoiced","customer__company_name":"Broker","total_pay":1044,"total_miles":203,"pickup_time":"2026-10-05T12:00:00Z","delivery_time":"2026-10-06T12:00:00Z","stops":[{"stop_type":"pickup","location":{"city":"Austin","state":"TX"}},{"stop_type":"delivery","location":{"city":"Dallas","state":"TX"}}]}`), &source); err != nil {
			t.Fatal(err)
		}
		live := []datatruck.Load{source}
		if err := r.RecordLoadSourceLookup(ctx, ids, []int{88402}); err != nil {
			t.Fatal(err)
		}
		if got, err := r.MissingBoardReplacementIDs(ctx); err != nil || !reflect.DeepEqual(got, ids) {
			t.Fatal(got, err)
		}
		held := func() {
			t.Helper()
			if n, err := r.ReplaceMissingBoardLoads(ctx, ids, live); err != nil || n != 0 {
				t.Fatal("protected unlinked slot changed", n, err)
			}
		}
		exec(`INSERT INTO payroll_settlements(driver_id,week_start,report) VALUES($1,'2026-10-05','{}')`, projectionDriver)
		held()
		if got, err := r.MissingBoardReplacementIDs(ctx); err != nil || len(got) != 0 {
			t.Fatal("finalized candidates", got, err)
		}
		exec(`UPDATE payroll_settlements SET finalized=false WHERE driver_id=$1`, projectionDriver)
		exec(`INSERT INTO investor_pay_weeks(truck_id,owner_id,week_start,edits,finalized,report) VALUES($1,'18200000-0000-0000-0000-000000000003','2026-10-05','{}',true,jsonb_build_object('loads',jsonb_build_array(jsonb_build_object('sourceDriverId',$2::text,'sourceDate','2026-10-05','sourceSlot',0))))`, projectionTruck, projectionDriver)
		held()
		exec(`UPDATE investor_pay_weeks SET finalized=false WHERE truck_id=$1`, projectionTruck)
		// Cross-date duplicate manual claims must not be silently combined.
		exec(`INSERT INTO gross_board_extra_entries(driver_id,service_date,slot,load_number,driver_rate) VALUES($1,'2026-10-06',1,'UNLINKED',1)`, projectionDriver)
		held()
		exec(`DELETE FROM gross_board_extra_entries WHERE driver_id=$1`, projectionDriver)
		// Two live records remain ambiguous, even though both have identical values.
		other := source
		other.ID = 88401
		if err := r.RecordLoadSourceLookup(ctx, ids, ids); err != nil {
			t.Fatal(err)
		}
		if n, err := r.ReplaceMissingBoardLoads(ctx, ids, []datatruck.Load{other, source}); err != nil || n != 0 {
			t.Fatal("ambiguous source linked", n, err)
		}
		if err := r.RecordLoadSourceLookup(ctx, ids, []int{88402}); err != nil {
			t.Fatal(err)
		}
		if n, err := r.ReplaceMissingBoardLoads(ctx, ids, live); err != nil || n != 1 {
			t.Fatal("unlinked source not reconciled", n, err)
		}
		var id int
		var gross, original, miles, note string
		if err := pool.QueryRow(ctx, `SELECT load_record_id,driver_rate::text,entered_original_rate::text,entered_miles::text,note FROM gross_board_entries WHERE driver_id=$1`, projectionDriver).Scan(&id, &gross, &original, &miles, &note); err != nil || id != 88402 || gross != "900.00" || original != "1200.00" || miles != "222.00" || note != "Keep manual details" {
			t.Fatal(id, gross, original, miles, note, err)
		}
		if n, err := r.ReplaceMissingBoardLoads(ctx, ids, live); err != nil || n != 0 {
			t.Fatal("link replayed", n, err)
		}
		if got, err := r.MissingBoardReplacementIDs(ctx); err != nil || len(got) != 0 {
			t.Fatal("linked slot still candidate", got, err)
		}
		var audits int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM gross_board_trip_history WHERE actor_id='datatruck_replacement'`).Scan(&audits); err != nil || audits != 1 {
			t.Fatal("audit", audits, err)
		}
	})
}

func TestPrepareUnlinkedBoardSource(t *testing.T) {
	for _, tc := range []struct {
		name    string
		sources []LoadReconciliationSource
		entries []GrossBoardEntry
		linked  bool
	}{
		{"single live without deleted predecessor", []LoadReconciliationSource{{ID: 1, Present: true}}, []GrossBoardEntry{{LoadNumber: "1"}}, true},
		{"no live", []LoadReconciliationSource{{ID: 1}}, []GrossBoardEntry{{LoadNumber: "1"}}, false},
		{"split", []LoadReconciliationSource{{ID: 1, Present: true, Split: true}}, []GrossBoardEntry{{LoadNumber: "1"}}, false},
		{"multiple slots", []LoadReconciliationSource{{ID: 1, Present: true}}, []GrossBoardEntry{{LoadNumber: "1"}, {LoadNumber: "1"}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := LoadReconciliationGroup{Sources: tc.sources, Before: tc.entries}
			prepareMissingLoadReplacement(&g)
			if (len(g.After) > 0) != tc.linked {
				t.Fatal(g)
			}
		})
	}
}
