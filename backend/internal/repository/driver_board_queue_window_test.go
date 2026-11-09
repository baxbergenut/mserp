package repository

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestBoardLoadQueueWindow(t *testing.T) {
	loadID := 1
	for _, tc := range []struct{ instant, start string }{
		{"2026-11-09T04:59:59Z", "2026-10-31"}, // Sunday in New York
		{"2026-11-09T05:00:00Z", "2026-11-07"}, // Monday in New York
		{"2026-11-15T23:00:00Z", "2026-11-07"},
		{"2026-11-16T05:00:00Z", "2026-11-14"},
		{"2026-03-09T04:00:00Z", "2026-03-07"}, // DST spring transition
		{"2027-01-01T12:00:00Z", "2026-12-26"},
	} {
		instant, _ := time.Parse(time.RFC3339, tc.instant)
		if got := boardLoadQueueStart(boardLoadToday(instant)); got != tc.start {
			t.Errorf("%s: got %s, want %s", tc.instant, got, tc.start)
		}
	}
	for _, tc := range []struct {
		load BoardLoad
		want bool
	}{
		{BoardLoad{Date: "2026-11-06"}, false},
		{BoardLoad{Date: "2026-11-07"}, true},
		{BoardLoad{Date: "2026-11-08"}, true},
		{BoardLoad{Date: "2026-11-09"}, true},
		{BoardLoad{Date: "2026-11-20"}, true},
		{BoardLoad{Date: "2026-01-01", Manual: true}, true},
		{BoardLoad{Date: "2026-01-01", PlanID: "restored"}, true},
		{BoardLoad{Date: "2026-11-09", LoadID: &loadID, SourceStatus: "in_transit"}, true},
		{BoardLoad{Date: "2026-11-09", LoadID: &loadID, SourceStatus: " In Transit "}, true},
		{BoardLoad{Date: "2026-11-20", LoadID: &loadID, SourceStatus: "IN-TRANSIT"}, true},
		{BoardLoad{Manual: true, LoadID: &loadID, SourceStatus: "in_transit"}, true},
		{BoardLoad{Date: "2026-01-01", PlanID: "restored", LoadID: &loadID, SourceStatus: "in_transit"}, true},
		{BoardLoad{Date: "2026-01-01", LoadID: &loadID, SourceStatus: "in_transit"}, true},
		{BoardLoad{Date: "2026-01-01", SourceStatus: "in_transit"}, false},
		{BoardLoad{Date: "2026-11-09", PreviouslyActive: true, LoadID: &loadID, SourceStatus: "in_transit"}, false},
		{BoardLoad{Date: "2026-11-09", SourceStatus: "in_transit"}, true}, // Unmatched follows the ordinary date window.
		{BoardLoad{Date: "2026-11-09", LoadID: &loadID, SourceStatus: "Dispatched"}, true},
	} {
		state := boardLoadState{RestoredPlans: map[string]bool{"restored": true}}
		if got := boardPlanQueued(tc.load, state, "2026-11-09"); got != tc.want {
			t.Errorf("%+v queued=%t, want %t", tc.load, got, tc.want)
		}
	}
}

func testBoardQueueWindow(t *testing.T, ctx context.Context, pool *pgxpool.Pool, original *DriverBoardRepository, fleet *FleetRepository) {
	t.Helper()
	repo := *original
	day := "2026-11-08"
	repo.now = func() time.Time { value, _ := time.Parse(time.DateOnly, day); return value.Add(12 * time.Hour) }
	d, err := fleet.CreateDriver(ctx, DriverInput{FullName: "Weekend Queue Driver", PayType: "cpm", PayRate: .6, Active: true})
	if err != nil {
		t.Fatal(err)
	}
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	defer func() {
		exec(`DELETE FROM drivers WHERE id=$1`, d.ID)
		exec(`DELETE FROM loads WHERE id BETWEEN 88951 AND 88955`)
	}()
	exec(`INSERT INTO loads(id,load_id,status,load_pay,total_pay,raw_payload) VALUES
 (88951,'WINDOW-DUP','Dispatched',0,0,'{}'),(88952,'WINDOW-DONE','Delivered',0,0,'{}'),
 (88953,'WINDOW-CANCELED','Canceled',0,0,'{}'),(88954,'WINDOW-INVOICED','Invoiced',0,0,'{}'),
 (88955,'WINDOW-COMPLETED','Completed',0,0,'{}')`)
	exec(`INSERT INTO gross_board_entries(driver_id,service_date,load_number,load_record_id) VALUES
 ($1,'2026-10-15','WINDOW-DUP',88951),($1,'2026-11-07','SATURDAY',NULL),
 ($1,'2026-11-08','SUNDAY',NULL),($1,'2026-11-10','WINDOW-DUP',88951),
 ($1,'2026-11-11','WINDOW-DONE',88952),($1,'2026-11-12','WINDOW-CANCELED',88953),
 ($1,'2026-11-13','WINDOW-INVOICED',88954),($1,'2026-11-14','WINDOW-COMPLETED',88955),
 ($1,'2026-11-20','FUTURE',NULL)`, d.ID)
	// The reported case: an unmatched Friday load in the second Gross Board slot.
	exec(`INSERT INTO gross_board_extra_entries(driver_id,service_date,slot,load_number,driver_rate) VALUES
 ($1,'2026-11-06',1,'4011137828',835),($1,'2026-11-09',1,'HIDDEN',0),
 ($1,'2026-11-09',2,'BOARD-COMPLETED',0),($1,'2026-11-09',3,'DELETED',0)`, d.ID)
	exec(`UPDATE gross_board_extra_entries SET deleted=true WHERE driver_id=$1 AND load_number='DELETED'`, d.ID)
	exec(`INSERT INTO driver_board(driver_id,status) VALUES($1,'ENROUTE')`, d.ID)
	exec(`INSERT INTO driver_board_load_state(driver_id,payload) VALUES($1,jsonb_build_object(
 'hidden',jsonb_build_array((SELECT plan_id::text FROM gross_board_extra_entries WHERE driver_id=$1 AND load_number='HIDDEN')),
 'completed',jsonb_build_array('plan:'||(SELECT plan_id::text FROM gross_board_extra_entries WHERE driver_id=$1 AND load_number='BOARD-COMPLETED'))))`, d.ID)
	view := func() BoardLoads {
		t.Helper()
		v, err := repo.Loads(ctx, d.ID, "2026-01-01")
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	find := func(list []BoardLoad, number string) *BoardLoad {
		i := slices.IndexFunc(list, func(p BoardLoad) bool { return p.Number == number })
		if i < 0 {
			return nil
		}
		return &list[i]
	}
	v := view()
	incident := find(v.Next, "4011137828")
	if incident == nil || !incident.Overdue || incident.LoadID != nil || incident.Slot != 1 {
		t.Fatalf("Friday unmatched second slot missing: %+v", v)
	}
	if len(v.Hidden) != 0 || find(v.Next, "HIDDEN") == nil || len(v.Unavailable) != 5 || find(v.Next, "DELETED") != nil {
		t.Fatalf("completion/removal guards changed: %+v", v)
	}
	duplicate := find(v.Next, "WINDOW-DUP")
	if duplicate == nil || duplicate.Date != "2026-11-10" || duplicate.Overdue {
		t.Fatalf("historical duplicate suppressed upcoming plan: %+v", duplicate)
	}
	history, err := repo.History(ctx, []string{d.ID}, 0)
	if err != nil {
		t.Fatal(err)
	}
	_ = view()
	after, err := repo.History(ctx, []string{d.ID}, 0)
	if err != nil || len(after.Items) != len(history.Items) {
		t.Fatal("read created history", err)
	}
	day = "2026-11-09"
	v = view()
	if find(v.Next, "4011137828") != nil || find(v.Earlier, "4011137828") == nil {
		t.Fatal("Friday must move to Earlier at Monday cutoff")
	}
	for _, number := range []string{"SATURDAY", "SUNDAY"} {
		p := find(v.Next, number)
		if p == nil || !p.Overdue {
			t.Fatalf("weekend missing %s: %+v", number, v)
		}
	}
	// Native Gross plans, including legacy hidden entries, cannot be hidden by
	// an old client or a direct API action. Rejections do not mutate the queue.
	var version int
	if err := pool.QueryRow(ctx, `SELECT version FROM driver_board WHERE driver_id=$1`, d.ID).Scan(&version); err != nil {
		t.Fatal(err)
	}
	for _, p := range append(slices.Clone(v.Next), v.Earlier...) {
		if p.CanRemoveFromQueue {
			t.Fatal("native plan marked removable", p)
		}
		_, err := repo.ChangeLoads(ctx, BoardLoadAction{DriverID: d.ID, Version: version, FromDate: v.FromDate, Revision: v.Revision, Action: "hide", PlanID: p.PlanID}, "")
		if !errors.Is(err, ErrBoardGrossPlanRemoval) {
			t.Fatal("Gross hide bypass", p.Number, err)
		}
	}
	if view().Revision != v.Revision {
		t.Fatal("rejected removals mutated the board")
	}
	stale := BoardLoadAction{DriverID: d.ID, Version: version, FromDate: v.FromDate, Revision: v.Revision, Action: "clear"}
	day = "2026-11-15"
	if find(view().Next, "SATURDAY") == nil {
		t.Fatal("weekend must remain through Sunday")
	}
	day = "2026-11-16"
	if _, err := repo.ChangeLoads(ctx, stale, ""); !errors.Is(err, ErrBoardLoadSelection) {
		t.Fatal("stale rollover action accepted", err)
	}
	v = view()
	if find(v.Next, "SATURDAY") != nil || find(v.Earlier, "SATURDAY") == nil || find(v.Next, "FUTURE") == nil {
		t.Fatalf("next Monday window incorrect: %+v", v)
	}
	var gross string
	if err := pool.QueryRow(ctx, `SELECT driver_rate::text FROM gross_board_extra_entries WHERE driver_id=$1 AND load_number='4011137828'`, d.ID).Scan(&gross); err != nil || gross != "835.00" {
		t.Fatal("gross changed", gross, err)
	}
}
