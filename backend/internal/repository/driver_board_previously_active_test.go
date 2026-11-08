package repository

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestBoardPreviouslyActiveDeparture(t *testing.T) {
	a, b := 74887, 74900
	current := &BoardLoad{LoadID: &a, Number: "4011106820", Stops: []BoardStop{{Key: "pickup", Type: "pickup"}, {Key: "delivery", Type: "delivery"}}}
	replacement := boardLoadState{Current: &BoardLoad{LoadID: &b, Number: "NEXT"}}
	for _, tc := range []struct {
		name           string
		before, after  boardLoadState
		status, number string
		want           bool
	}{
		{"transit then replaced", boardLoadState{Current: current}, replacement, "ENROUTE", current.Number, true},
		{"reserved then replaced", boardLoadState{Current: current}, replacement, "RESERVED", current.Number, true},
		{"delivery then replaced", boardLoadState{Current: current, DestinationSource: true, StopKey: "delivery"}, replacement, "DISPATCHED", current.Number, true},
		{"pickup only", boardLoadState{Current: current, DestinationSource: true, StopKey: "pickup"}, replacement, "DISPATCHED", current.Number, false},
		{"clear is not replacement", boardLoadState{Current: current}, boardLoadState{}, "ENROUTE", current.Number, false},
		{"unmatched replacement", boardLoadState{Current: current}, boardLoadState{Current: &BoardLoad{Number: "NEXT"}}, "ENROUTE", current.Number, false},
		{"same source new plan", boardLoadState{Current: current}, boardLoadState{Current: current}, "ENROUTE", current.Number, false},
		{"mismatched text", boardLoadState{Current: current}, replacement, "ENROUTE", "OTHER", false},
		{"unknown selected stop", boardLoadState{Current: current, DestinationSource: true, StopKey: "missing"}, replacement, "DISPATCHED", current.Number, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := boardPreviouslyActiveDeparture(tc.before, tc.after, tc.status, tc.number); got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

func testBoardPreviouslyActiveQueue(t *testing.T, ctx context.Context, pool *pgxpool.Pool, original *DriverBoardRepository, fleet *FleetRepository) {
	t.Helper()
	repo := *original
	repo.now = func() time.Time { return time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC) }
	d, err := fleet.CreateDriver(ctx, DriverInput{FullName: "Previously Active Driver", PayType: "cpm", PayRate: .6, Active: true})
	if err != nil {
		t.Fatal(err)
	}
	other, err := fleet.CreateDriver(ctx, DriverInput{FullName: "Other Upcoming Driver", PayType: "cpm", PayRate: .6, Active: true})
	if err != nil {
		t.Fatal(err)
	}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	defer func() {
		exec(`DELETE FROM drivers WHERE id=ANY($1::uuid[])`, []string{d.ID, other.ID})
		exec(`DELETE FROM loads WHERE id BETWEEN 88971 AND 88974`)
	}()
	exec(`INSERT INTO loads(id,load_id,status,load_pay,total_pay,raw_payload) VALUES
 (88971,'PREVIOUS-A','Dispatched',0,0,'{"stops":[{"ordering":1,"stop_type":"pickup","location":{"city":"Atlanta","state":"GA"}},{"ordering":2,"stop_type":"delivery","location":{"city":"Dallas","state":"TX"}}]}'),
 (88972,'PREVIOUS-B','Dispatched',0,0,'{}'),(88973,'UPCOMING','Dispatched',0,0,'{}'),(88974,'PREVIOUS-A','Dispatched',0,0,'{}')`)
	exec(`INSERT INTO gross_board_entries(driver_id,service_date,load_number,load_record_id) VALUES
 ($1,'2026-11-04','PREVIOUS-A',88971),($1,'2026-11-08','PREVIOUS-B',88972),($1,'2026-11-09','UPCOMING',88973),($2,'2026-11-09','PREVIOUS-A',88971)`, d.ID, other.ID)
	rows, err := repo.Save(ctx, []DriverBoardEntry{{DriverID: d.ID, CurrentLoad: "PREVIOUS-A"}}, "previous-test")
	if err != nil {
		t.Fatal(err)
	}
	entry := rows[0]
	view := func() BoardLoads {
		t.Helper()
		v, err := repo.Loads(ctx, d.ID, "")
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
	act := func(action, plan string) BoardLoadResult {
		t.Helper()
		v := view()
		r, err := repo.ChangeLoads(ctx, BoardLoadAction{DriverID: d.ID, Version: entry.Version, FromDate: v.FromDate, Revision: v.Revision, Action: action, PlanID: plan}, "previous-test")
		if err != nil {
			t.Fatal(action, err)
		}
		entry = r.Entry
		return r
	}
	b := find(view().Next, "PREVIOUS-B").PlanID
	// Replacement alone is not proof; truly upcoming and other-driver plans survive.
	act("select", b)
	a := find(view().Next, "PREVIOUS-A")
	if a == nil {
		t.Fatal("pickup-only replacement hidden")
	}
	act("select", a.PlanID)
	act("advance", "")
	replaced := act("select", b)
	v := replaced.Loads
	previous := find(v.Earlier, "PREVIOUS-A")
	if previous == nil || !previous.PreviouslyActive || !previous.CanRestoreToQueue || previous.Completed || previous.Overdue || find(v.Next, "PREVIOUS-A") != nil || find(v.Next, "UPCOMING") == nil {
		t.Fatal("previously active classification", v)
	}
	otherView, err := repo.Loads(ctx, other.ID, "")
	if err != nil || find(otherView.Next, "PREVIOUS-A") == nil {
		t.Fatal("cross-driver suppression", otherView, err)
	}
	if _, err := repo.ChangeLoads(ctx, BoardLoadAction{DriverID: d.ID, Version: entry.Version, FromDate: v.FromDate, Revision: v.Revision, Action: "add", Number: "previous-a"}, "previous-test"); !errors.Is(err, ErrBoardLoadSelection) {
		t.Fatal("duplicate manual bypass", err)
	}
	// Read projections must be stable and make no audit writes.
	var count int
	pool.QueryRow(ctx, `SELECT count(*) FROM driver_board_history WHERE driver_id=$1`, d.ID).Scan(&count)
	if view().Revision != v.Revision {
		t.Fatal("read changed revision")
	}
	var afterCount int
	pool.QueryRow(ctx, `SELECT count(*) FROM driver_board_history WHERE driver_id=$1`, d.ID).Scan(&afterCount)
	if afterCount != count {
		t.Fatal("read wrote history")
	}
	restored := act("restore_previous", previous.PlanID)
	if find(restored.Loads.Next, "PREVIOUS-A") == nil || restored.Loads.Current.Number != "PREVIOUS-B" {
		t.Fatal("restore changed current or failed", restored)
	}
	// One correction applies only to this replacement; a new worked cycle retires it again.
	act("select", previous.PlanID)
	act("advance", "")
	act("select", b)
	if find(view().Earlier, "PREVIOUS-A") == nil {
		t.Fatal("new replacement bypassed by old restore")
	}
	// Undo replacement protects the original current selection; undo progress removes proof.
	replacementID := entry.UndoID
	entry, err = repo.Undo(ctx, replacementID, d.ID, entry.Version, "previous-test", true)
	if err != nil {
		t.Fatal(err)
	}
	if v := view(); v.Current == nil || v.Current.Number != "PREVIOUS-A" || v.Current.PreviouslyActive {
		t.Fatal("undo/current protection", v)
	}
	// Return to pickup before replacing again: latest episode must supersede old evidence.
	v = view()
	pickup := v.Current.Stops[0].Key
	result, err := repo.ChangeLoads(ctx, BoardLoadAction{DriverID: d.ID, Version: entry.Version, FromDate: v.FromDate, Revision: v.Revision, Action: "advance", StopKey: pickup}, "previous-test")
	if err != nil {
		t.Fatal(err)
	}
	entry = result.Entry
	act("select", b)
	if find(view().Next, "PREVIOUS-A") == nil {
		t.Fatal("latest corrected episode still suppressed")
	}
	// Native completion/promotion retains the existing one-step completion correction.
	act("select", previous.PlanID)
	act("advance", "")
	act("advance", "")
	completed := find(view().Unavailable, "PREVIOUS-A")
	if completed == nil || !completed.Completed || completed.PreviouslyActive || !completed.CanRestoreToQueue {
		t.Fatal("completion lost precedence", view())
	}
	restored = act("restore_completed", completed.PlanID)
	if find(restored.Loads.Next, "PREVIOUS-A") == nil {
		t.Fatal("completion restore still suppressed by history", restored.Loads)
	}
	// A same-number source replacement changes identity without changing currentLoad
	// text. Its latest pickup-only departure must supersede an older worked cycle.
	act("select", previous.PlanID)
	act("advance", "")
	act("select", b)
	act("select", previous.PlanID)
	exec(`UPDATE driver_board_load_state SET payload=jsonb_set(jsonb_set(payload,'{current,loadId}','88974'::jsonb),'{current,planId}',to_jsonb(gen_random_uuid()::text)) WHERE driver_id=$1`, d.ID)
	if p := find(view().Next, "PREVIOUS-A"); p == nil || p.PreviouslyActive {
		t.Fatal("same-number identity change reused old proof", view())
	}
	act("select", b)
	// Another source record with the same number cannot inherit history.
	exec(`UPDATE gross_board_entries SET load_record_id=88974 WHERE driver_id=$1 AND load_number='PREVIOUS-A'`, d.ID)
	if p := find(view().Next, "PREVIOUS-A"); p == nil || *p.LoadID != 88974 {
		t.Fatal("same-number new source inherited history", view())
	}
	var statuses int
	pool.QueryRow(ctx, `SELECT count(*) FROM loads WHERE id BETWEEN 88971 AND 88974 AND status='Dispatched'`).Scan(&statuses)
	if statuses != 4 {
		t.Fatal("upstream status changed")
	}
}
