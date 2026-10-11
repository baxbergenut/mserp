package repository

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Runs in the Relay fixture's isolated fresh/migrated schemas as mserp_app.
func testSidebarCounts(t *testing.T, ctx context.Context, pool *pgxpool.Pool, terminatedID, activeID string) {
	t.Helper()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	wmt := NewWeighMyTruckRepository(pool, nil)
	exec(`INSERT INTO weighmytruck_memberships(driver_id,email,first_name,last_name,enrolled) VALUES
 ($1,'terminated@example.test','Terminated','Driver',true),($2,'active@example.test','Active','Driver',true),
 (NULL,'unlinked@example.test','Unlinked','Driver',true)`, terminatedID, activeID)
	checkWMT := func(want int) {
		t.Helper()
		got, err := wmt.TerminatedMembershipCount(ctx)
		if err != nil || got != want {
			t.Fatalf("WMT count=%d want=%d err=%v", got, want, err)
		}
	}
	checkWMT(1)
	exec(`UPDATE weighmytruck_memberships SET state='pending' WHERE driver_id=$1`, terminatedID)
	checkWMT(1)
	exec(`UPDATE weighmytruck_memberships SET enrolled=false,state='review' WHERE driver_id=$1`, terminatedID)
	checkWMT(1)
	exec(`UPDATE weighmytruck_memberships SET state='confirmed' WHERE driver_id=$1`, terminatedID)
	checkWMT(0)
	tolls := NewTollRepository(pool)
	_, from, err := tolls.RecentUnlinkedCount(ctx)
	if err != nil {
		t.Fatal(err)
	}
	zone, _ := time.LoadLocation("America/New_York")
	now := time.Now().In(zone)
	wantFrom := time.Date(now.Year(), now.Month()-1, 1, 0, 0, 0, 0, zone).Format("2006-01-02")
	if from != wantFrom {
		t.Fatalf("cutoff=%s want=%s", from, wantFrom)
	}
	exec(`INSERT INTO tolls(posting_date,invoice_date,customer_id,source,read_type,transponder_or_plate,
 equipment_unit,agency,exit_plaza,exit_date,exit_time,toll_class,amount,row_fingerprint)
 SELECT $1::date+n,$1::date,'test','test','test','test','test','test','test',$1::date,'12:00','test',1,repeat(md5(n::text),2)
 FROM generate_series(-1,1) n`, from)
	checkTolls := func(want int) {
		t.Helper()
		got, _, err := tolls.RecentUnlinkedCount(ctx)
		if err != nil || got != want {
			t.Fatalf("toll count=%d want=%d err=%v", got, want, err)
		}
	}
	checkTolls(2) // Exact cutoff and next day count; the preceding day does not.
	exec(`INSERT INTO trucks(unit_number) VALUES('sidebar-test')`)
	exec(`UPDATE tolls SET truck_id=(SELECT id FROM trucks WHERE unit_number='sidebar-test') WHERE posting_date=$1::date`, from)
	checkTolls(1)
}
