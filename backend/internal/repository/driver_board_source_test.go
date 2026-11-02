package repository

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func testBoardSourceAvailability(t *testing.T, ctx context.Context, pool *pgxpool.Pool, board *DriverBoardRepository, fleet *FleetRepository) {
	t.Helper()
	d, err := fleet.CreateDriver(ctx, DriverInput{FullName: "Source Availability Driver", PayType: "cpm", PayRate: .6, Active: true})
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, `DELETE FROM drivers WHERE id=$1`, d.ID)
	defer pool.Exec(ctx, `DELETE FROM loads WHERE id BETWEEN 88120 AND 88122`)
	_, err = pool.Exec(ctx, `INSERT INTO loads(id,load_id,status,load_pay,total_pay,raw_payload) VALUES
 (88120,'SOURCE-DUPLICATE','dispatched',100,100,'{"stops":[{"id":1,"stop_type":"pickup","location":{"city":"Old","state":"TX"}}]}'),
 (88121,'SOURCE-DUPLICATE','in_transit',200,200,'{"stops":[{"id":2,"stop_type":"pickup","location":{"city":"New","state":"TX"}},{"id":3,"stop_type":"delivery","location":{"city":"Dallas","state":"TX"}}]}'),
 (88122,'SOURCE-GHOST','dispatched',300,300,'{}')`)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := board.Save(ctx, []DriverBoardEntry{{DriverID: d.ID, CurrentLoad: "SOURCE-DUPLICATE"}})
	if err != nil {
		t.Fatal(err)
	}
	view, err := board.Loads(ctx, d.ID, "")
	if err != nil || view.Current != nil {
		t.Fatal("duplicate source must remain ambiguous", view, err)
	}
	_, err = board.ChangeLoads(ctx, BoardLoadAction{DriverID: d.ID, Action: "add", Number: "SOURCE-GHOST", Version: entries[0].Version, Revision: view.Revision}, "")
	if err != nil {
		t.Fatal(err)
	}
	var before string
	if err = pool.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(l) ORDER BY id)::text FROM loads l WHERE id BETWEEN 88120 AND 88122`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	source := NewLoadRepository(pool)
	if err = source.RecordLoadSourceLookup(ctx, []int{88120, 88121, 88122}, []int{88121}); err != nil {
		t.Fatal(err)
	}
	view, err = board.Loads(ctx, d.ID, "")
	if err != nil || view.Current == nil || view.Current.LoadID == nil || *view.Current.LoadID != 88121 || len(view.Current.Stops) != 2 || view.SourceDestination != "New, TX" {
		t.Fatal("remaining live source did not resolve", view, err)
	}
	if len(view.Next) != 0 || len(view.Unavailable) != 1 || !view.Unavailable[0].SourceMissing {
		t.Fatal("missing source remained in active queue", view)
	}
	var after string
	if err = pool.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(l) ORDER BY id)::text FROM loads l WHERE id BETWEEN 88120 AND 88122`).Scan(&after); err != nil || before != after {
		t.Fatal("presence evidence changed financial source", err)
	}
	// Pin the exact ID, then remove it and restore the older same-number record.
	var version int
	if err = pool.QueryRow(ctx, `SELECT version FROM driver_board WHERE driver_id=$1`, d.ID).Scan(&version); err != nil {
		t.Fatal(err)
	}
	_, err = board.Save(ctx, []DriverBoardEntry{{DriverID: d.ID, CurrentLoad: "SOURCE-DUPLICATE", Version: version, ResolveCurrentLoad: true}})
	if err != nil {
		t.Fatal(err)
	}
	if err = source.RecordLoadSourceLookup(ctx, []int{88120, 88121}, []int{88120}); err != nil {
		t.Fatal(err)
	}
	view, err = board.Loads(ctx, d.ID, "")
	if err != nil || view.Current == nil || view.Current.LoadID == nil || *view.Current.LoadID != 88121 || !view.Current.SourceMissing || len(view.Current.Stops) != 0 || view.DestinationSource {
		t.Fatal("missing pinned identity was replaced or exposed stale stops", view, err)
	}
	// Reappearance is explicit evidence and restores the same selected identity.
	if err = source.RecordLoadSourceLookup(ctx, []int{88120, 88121}, []int{88121}); err != nil {
		t.Fatal(err)
	}
	view, err = board.Loads(ctx, d.ID, "")
	if err != nil || view.Current == nil || view.Current.SourceMissing || len(view.Current.Stops) != 2 {
		t.Fatal("source reappearance not recovered", view, err)
	}
	if err = source.RecordLoadSourceLookup(ctx, []int{88121}, []int{}); err != nil {
		t.Fatal(err)
	}
	view, err = board.Loads(ctx, d.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT version FROM driver_board WHERE driver_id=$1`, d.ID).Scan(&version); err != nil {
		t.Fatal(err)
	}
	cleared, err := board.ChangeLoads(ctx, BoardLoadAction{DriverID: d.ID, Action: "clear", Version: version, Revision: view.Revision}, "")
	if err != nil || cleared.Loads.Current != nil {
		t.Fatal("missing current source could not be cleared", cleared, err)
	}
}

func testBoardCurrentEntryParity(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	var driver string
	if err = tx.QueryRow(ctx, `INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate) VALUES('Board Query Parity','board query parity','cpm',.6) RETURNING id::text`).Scan(&driver); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO loads(id,load_id,status,load_pay,total_pay,total_miles,raw_payload) VALUES
 (88130,'PARITY-REPEAT','delivered',500,500,200,'{}'),(88131,'PARITY-OTHER','dispatched',600,600,300,'{}');
 `); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO gross_board_entries(driver_id,service_date,load_number,driver_rate,entered_original_rate,entered_miles) VALUES
 ($1,'2026-09-28','PARITY-REPEAT',100,NULL,NULL),($1,'2026-10-05','PARITY-REPEAT',100,NULL,NULL),
 ($1,'2026-10-06','PARITY-OTHER',200,700,400),($1,'2026-10-12','UNMATCHED-PARITY',200,800,500)`, driver); err != nil {
		t.Fatal(err)
	}
	// Compare the complete API rows, including duplicate flags, with the original
	// financial projection. Existing board fixtures include historical repeats,
	// manual values, unresolved numbers and split/trip selections.
	for _, date := range []string{"2026-09-28", "2026-10-05", "2026-10-12"} {
		week, _ := time.Parse(time.DateOnly, date)
		read := func(query string) []GrossBoardEntry {
			rows, e := tx.Query(ctx, query+"SELECT "+grossBoardEntryColumns+` FROM effective WHERE service_date >= $1::date ORDER BY driver_id,service_date,slot`, week)
			if e != nil {
				t.Fatal(e)
			}
			defer rows.Close()
			result := []GrossBoardEntry{}
			for rows.Next() {
				var entry GrossBoardEntry
				if e = scanGrossBoardEntry(rows, &entry); e != nil {
					t.Fatal(e)
				}
				result = append(result, entry)
			}
			if e = rows.Err(); e != nil {
				t.Fatal(e)
			}
			return result
		}
		before, after := read(grossBoardEffective), read(driverBoardCurrentEntries)
		if !reflect.DeepEqual(before, after) {
			a, _ := json.Marshal(before)
			b, _ := json.Marshal(after)
			t.Fatalf("current projection differs for %s: %s != %s", date, a, b)
		}
	}
}
