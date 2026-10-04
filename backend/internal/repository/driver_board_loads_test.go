package repository

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func testDriverBoardLoads(t *testing.T, ctx context.Context, pool *pgxpool.Pool, repo *DriverBoardRepository, fleet *FleetRepository) {
	t.Helper()
	clocked := *repo
	repo = &clocked
	repo.now = func() time.Time { return time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC) }
	d, err := fleet.CreateDriver(ctx, DriverInput{FullName: "Queue Driver", PayType: "cpm", PayRate: 0.6, Active: true})
	if err != nil {
		t.Fatal(err)
	}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	defer func() {
		exec(`DELETE FROM drivers WHERE id=$1`, d.ID)
		exec(`DELETE FROM loads WHERE id IN (88001,88002,88003)`)
	}()
	exec(`INSERT INTO loads(id,load_id,status,load_pay,total_pay,total_miles,raw_payload) VALUES
 (88001,'QUEUE-A','Dispatched',1000,1000,500,'{"pickup_appointment_time":"2026-09-28T12:00:00Z","delivery_appointment_time":"2026-10-01T17:00:00Z","stops":[{"ordering":3,"stop_type":"delivery","location":{"city":"Boston","state":"MA"}},{"ordering":1,"stop_type":"pickup","location":{"city":"Atlanta","state":"GA"}},{"ordering":2,"stop_type":"delivery","location":{"city":"Richmond","state":"VA"}}]}'),
 (88002,'DUP','Dispatched',2000,2000,600,'{}'),(88003,'DUP','Dispatched',3000,3000,700,'{}')`)
	exec(`INSERT INTO gross_board_entries(driver_id,service_date,load_number,load_record_id,original_rate,driver_rate,miles) VALUES
 ($1,'2026-09-28','QUEUE-A',88001,1000,900,500),($1,'2026-10-05','QUEUE-A',88001,1000,900,500),
 ($1,'2026-10-06','PLAN-B',NULL,2000,1800,700),($1,'2026-10-07','DUP',88002,2000,1800,600),($1,'2026-10-08','DUP',88003,3000,2800,700)`, d.ID)
	exec(`INSERT INTO gross_board_entries(driver_id,service_date,day_status) VALUES($1,'2026-10-09','HOME')`, d.ID)
	exec(`INSERT INTO gross_board_extra_entries(driver_id,service_date,slot,load_number,deleted) VALUES($1,'2026-10-09',1,'DELETED',true)`, d.ID)
	view := func() BoardLoads {
		t.Helper()
		v, err := repo.Loads(ctx, d.ID, "2026-09-21")
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	v := view()
	if v.Current != nil || len(v.Next) != 4 || v.Next[1].Number != "PLAN-B" || v.Next[2].LoadID == v.Next[3].LoadID {
		t.Fatalf("initial plan queue: %+v", v)
	}
	if v.Next[0].Stops[0].Location != "Atlanta, GA" || v.Next[0].Stops[1].Appointment != "" || v.Next[0].Stops[2].Appointment == "" {
		t.Fatal("multi-stop order/appointments", v.Next[0].Stops)
	}
	h, err := repo.History(ctx, []string{d.ID}, 0)
	if err != nil || len(h.Items) != 0 {
		t.Fatal("read mutated audit", err)
	}
	entries, err := repo.Save(ctx, []DriverBoardEntry{{DriverID: d.ID, CurrentLoad: "Free text", Status: "RESERVED", Destination: "Manual city", ETA: "Friday", Notes: "Keep my notes"}})
	if err != nil {
		t.Fatal(err)
	}
	e := entries[0]
	action := func(a BoardLoadAction) BoardLoadResult {
		t.Helper()
		v = view()
		a.DriverID = d.ID
		a.Version = e.Version
		a.HomeVersion = e.HomeVersion
		a.FromDate = v.FromDate
		a.Revision = v.Revision
		r, err := repo.ChangeLoads(ctx, a, "")
		if err != nil {
			t.Fatal(err)
		}
		e = r.Entry
		return r
	}
	currentID := v.Next[0].PlanID
	r := action(BoardLoadAction{Action: "select", PlanID: currentID, StopKey: v.Next[0].Stops[1].Key})
	if r.Loads.SourceDestination != "Atlanta, GA" || len(r.Loads.Next) != 3 || e.Status != "DISPATCHED" || e.ETA != "Friday" || e.Notes != "Keep my notes" || e.CurrentLoad != "QUEUE-A" {
		t.Fatalf("explicit selection damaged fields: %+v", r)
	}
	r = action(BoardLoadAction{Action: "source", StopKey: r.Loads.Current.Stops[1].Key})
	stale := BoardLoadAction{DriverID: d.ID, Version: e.Version - 1, HomeVersion: e.HomeVersion, FromDate: r.Loads.FromDate, Revision: r.Loads.Revision, Action: "clear"}
	if _, err = repo.ChangeLoads(ctx, stale, ""); !errors.Is(err, ErrDriverBoardConflict) {
		t.Fatal("repeated click accepted", err)
	}
	r = action(BoardLoadAction{Action: "order", Order: []string{r.Loads.Next[2].PlanID, r.Loads.Next[1].PlanID, r.Loads.Next[0].PlanID}})
	if *r.Loads.Next[0].LoadID != 88003 {
		t.Fatal("duplicate numbers conflated")
	}
	h, _ = repo.History(ctx, []string{d.ID}, 0)
	e, err = repo.Undo(ctx, h.Items[0].ID, d.ID, e.Version, e.HomeVersion, "")
	if err != nil {
		t.Fatal("order undo", err)
	}
	if view().CustomOrder || view().Next[0].Number != "PLAN-B" {
		t.Fatal("undo failed to restore planned order")
	}
	r = action(BoardLoadAction{Action: "hide", PlanID: view().Next[0].PlanID})
	if len(r.Loads.Hidden) != 1 || len(r.Loads.Next) != 2 {
		t.Fatal("hide queue")
	}
	action(BoardLoadAction{Action: "restore", PlanID: r.Loads.Hidden[0].PlanID})
	// Source revision prevents acting on plans edited after the drawer was opened.
	v = view()
	exec(`UPDATE gross_board_entries SET load_number='PLAN-C' WHERE driver_id=$1 AND service_date='2026-10-06'`, d.ID)
	if _, err = repo.ChangeLoads(ctx, BoardLoadAction{DriverID: d.ID, Version: e.Version, HomeVersion: e.HomeVersion, FromDate: v.FromDate, Revision: v.Revision, Action: "clear"}, ""); !errors.Is(err, ErrBoardLoadSelection) {
		t.Fatal("stale source revision accepted", err)
	}
	r = action(BoardLoadAction{Action: "manual"})
	if e.Destination != "Richmond, VA" || r.Loads.DestinationSource {
		t.Fatal("manual override failed")
	}
	r = action(BoardLoadAction{Action: "source", StopKey: r.Loads.Current.Stops[2].Key})
	if r.Loads.SourceDestination != "Boston, MA" || e.Destination != "Richmond, VA" {
		t.Fatal("return to source overwrote manual text")
	}
	// Gross edits do not erase the current snapshot or silently pick another load.
	exec(`UPDATE gross_board_entries SET load_number='' WHERE driver_id=$1 AND service_date='2026-09-28'`, d.ID)
	v = view()
	if v.Current == nil || v.Current.PlanID != currentID || !strings.Contains(v.Current.Warning, "removed") {
		t.Fatalf("removed current lost: %+v", v.Current)
	}
	// A free-text correction explicitly detaches the selection; undo restores it.
	e.CurrentLoad = "Manual correction"
	entries, err = repo.Save(ctx, []DriverBoardEntry{e})
	if err != nil {
		t.Fatal(err)
	}
	e = entries[0]
	if view().Current != nil {
		t.Fatal("manual current text retained linked identity")
	}
	h, _ = repo.History(ctx, []string{d.ID}, 0)
	e, err = repo.Undo(ctx, h.Items[0].ID, d.ID, e.Version, e.HomeVersion, "")
	if err != nil || view().Current == nil {
		t.Fatal("selection undo", err)
	}
	// Gross Board owns planned assignment; lagging DataTruck assignment is visible for review.
	other, err := fleet.CreateDriver(ctx, DriverInput{FullName: "Queue Other", PayType: "cpm", PayRate: 0.6, Active: true})
	if err != nil {
		t.Fatal(err)
	}
	defer exec(`DELETE FROM drivers WHERE id=$1`, other.ID)
	exec(`UPDATE loads SET driver_id=$1 WHERE id=88002`, other.ID)
	exec(`UPDATE loads SET status='Delivered' WHERE id=88003`)
	v = view()
	if len(v.Unavailable) != 1 {
		t.Fatal("delivered/reassigned queue candidates", v)
	}
	foundMismatch := false
	for _, p := range v.Next {
		if p.LoadID != nil && *p.LoadID == 88002 && strings.Contains(p.Warning, "another driver") {
			foundMismatch = true
		}
	}
	if !foundMismatch {
		t.Fatal("Gross Board plan disappeared due to stale DataTruck assignment")
	}
	var sum string
	if err = pool.QueryRow(ctx, `SELECT sum(original_rate)::text FROM gross_board_entries WHERE driver_id=$1`, d.ID).Scan(&sum); err != nil || sum != "9000.00" {
		t.Fatal("operational actions changed accounting", sum, err)
	}
	// Current-week defaults must not pull in the previous week, even when an
	// older current load is retained for review across the week boundary.
	exec(`INSERT INTO gross_board_entries(driver_id,service_date,load_number) VALUES
 ($1,'2026-09-21','OLD-WEEK'),($1,'2026-09-29','OLD-OTHER')`, d.ID)
	for _, week := range []string{"2026-09-28", "2026-10-05"} {
		monday, _ := time.Parse(time.DateOnly, week)
		repo.now = func() time.Time { return monday.Add(12 * time.Hour) }
		board, err := repo.Get(ctx, monday)
		if err != nil {
			t.Fatal(err)
		}
		loads := board.Loads[d.ID]
		if loads.FromDate != week || loads.Current == nil || loads.Current.PlanID != currentID {
			t.Fatalf("week window or retained current changed: %+v", loads)
		}
		for _, p := range loads.Next {
			if p.Date < week {
				t.Fatalf("previous-week plan appeared in next loads: %+v", p)
			}
		}
	}
	// Midnight changes queue eligibility, even for an explicitly ordered plan.
	v = view()
	order := []string{}
	for _, p := range v.Next {
		order = append(order, p.PlanID)
	}
	r = action(BoardLoadAction{Action: "order", Order: order})
	beforeMidnight := BoardLoadAction{DriverID: d.ID, Version: e.Version, HomeVersion: e.HomeVersion, FromDate: r.Loads.FromDate, Revision: r.Loads.Revision, Action: "clear"}
	repo.now = func() time.Time { return time.Date(2026, 10, 7, 4, 0, 0, 0, time.UTC) }
	if _, err := repo.ChangeLoads(ctx, beforeMidnight, ""); !errors.Is(err, ErrBoardLoadSelection) {
		t.Fatal("pre-midnight action accepted", err)
	}
	v = view()
	if v.FromDate != "2026-10-07" || v.Current == nil || v.Current.PlanID != currentID {
		t.Fatal("rollover lost current", v)
	}
	for _, p := range v.Next {
		if p.Date < v.FromDate {
			t.Fatal("old ordered plan still upcoming", p)
		}
	}
	var earlier *BoardLoad
	for _, p := range v.Earlier {
		if p.Number == "PLAN-C" {
			copy := p
			earlier = &copy
		}
	}
	if earlier == nil {
		t.Fatal("older unfinished plan unavailable")
	}
	r = action(BoardLoadAction{Action: "select", PlanID: earlier.PlanID})
	if r.Loads.Current == nil || r.Loads.Current.PlanID != earlier.PlanID {
		t.Fatal("cannot select earlier current")
	}
}

func TestBoardLoadToday(t *testing.T) {
	for _, tc := range []struct{ instant, date string }{
		{"2026-10-04T03:59:59Z", "2026-10-03"},
		{"2026-10-04T04:00:00Z", "2026-10-04"},
		{"2026-10-05T04:00:00Z", "2026-10-05"},
		{"2026-11-02T04:59:59Z", "2026-11-01"},
		{"2026-11-02T05:00:00Z", "2026-11-02"},
	} {
		instant, _ := time.Parse(time.RFC3339, tc.instant)
		if got := boardLoadToday(instant); got != tc.date {
			t.Errorf("%s: got %s, want %s", tc.instant, got, tc.date)
		}
	}
}

func testCurrentLoadAutofill(t *testing.T, ctx context.Context, pool *pgxpool.Pool, original *DriverBoardRepository, fleet *FleetRepository) {
	t.Helper()
	repo := *original
	repo.now = func() time.Time { return time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC) }
	d, err := fleet.CreateDriver(ctx, DriverInput{FullName: "Autofill Driver", PayType: "cpm", PayRate: 0.6, Active: true})
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
		exec(`DELETE FROM loads WHERE id IN (88021,88022)`)
	}()
	exec(`INSERT INTO loads(id,load_id,status,load_pay,total_pay,total_miles,raw_payload) VALUES
 (88021,'AUTO-A','Delivered',1000,1000,500,'{"stops":[{"ordering":1,"stop_type":"pickup","location":{"city":"Atlanta","state":"Georgia","zip_code":"30303"}},{"ordering":2,"stop_type":"delivery","location":{"city":"Dallas","state":"Texas","zip_code":"75236"}}]}'),
 (88022,'AUTO-B','Dispatched',1000,1000,500,'{}')`)
	exec(`INSERT INTO gross_board_entries(driver_id,service_date,load_number,load_record_id) VALUES
 ($1,'2026-09-28','AUTO-A',88021),($1,'2026-09-29','AUTO-A',88021),
 ($1,'2026-09-21','PREVIOUS',88021),($1,'2026-10-05','FUTURE',88021)`, d.ID)
	e := DriverBoardEntry{DriverID: d.ID, Status: "DISPATCHED", ETA: "2026-10-01T15:00", Notes: "Keep notes"}
	view := func() BoardLoads {
		t.Helper()
		v, err := repo.Loads(ctx, d.ID, "")
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	save := func() {
		t.Helper()
		rows, err := repo.Save(ctx, []DriverBoardEntry{e})
		if err != nil {
			t.Fatal(err)
		}
		e = rows[0]
	}
	e.CurrentLoad = "  auto-a  "
	save()
	v := view()
	if v.Current == nil || !v.DestinationSource || v.SourceDestination != "Atlanta, Georgia, 30303" || len(v.Next) != 0 {
		t.Fatalf("earlier delivered weekly match/repeated slots: %+v", v)
	}
	if e.Status != "DISPATCHED" || e.ETA != "2026-10-01T15:00" || e.Notes != "Keep notes" {
		t.Fatal("autofill changed unrelated fields")
	}
	e.CurrentLoad = ""
	e.Destination = "stale manual destination"
	save()
	if e.Destination != "" || view().Current != nil || view().DestinationSource {
		t.Fatal("clear retained location or source")
	}
	h, err := repo.History(ctx, []string{d.ID}, 0)
	if err != nil {
		t.Fatal(err)
	}
	e, err = repo.Undo(ctx, h.Items[0].ID, d.ID, e.Version, e.HomeVersion, "")
	if err != nil || !view().DestinationSource || view().SourceDestination != "Atlanta, Georgia, 30303" {
		t.Fatal("clear undo did not restore source", err)
	}
	for _, number := range []string{"PREVIOUS", "FUTURE", "AUTO", "UNKNOWN"} {
		e.CurrentLoad = number
		save()
		if view().Current != nil || view().DestinationSource {
			t.Fatal("out-of-week/partial/free text matched", number)
		}
	}
	e.CurrentLoad = "AUTO-A"
	save()
	v = view()
	cleared, err := repo.ChangeLoads(ctx, BoardLoadAction{DriverID: d.ID, Version: e.Version, HomeVersion: e.HomeVersion, Revision: v.Revision, FromDate: v.FromDate, Action: "clear"}, "")
	if err != nil || cleared.Entry.Destination != "" || cleared.Loads.Current != nil {
		t.Fatal("drawer clear retained location", err)
	}
	e = cleared.Entry
	// Distinct imported loads sharing a plan number must not be guessed.
	exec(`INSERT INTO gross_board_entries(driver_id,service_date,load_number,load_record_id) VALUES($1,'2026-09-30','AUTO-A',88022)`, d.ID)
	e.CurrentLoad = "AUTO-A"
	save()
	if view().Current != nil {
		t.Fatal("ambiguous number auto-linked")
	}
	exec(`UPDATE gross_board_entries SET load_number='' WHERE driver_id=$1 AND service_date='2026-09-30'`, d.ID)
	// Retyping an existing unlinked number also links it on autosave.
	e.ResolveCurrentLoad = true
	save()
	if !view().DestinationSource {
		t.Fatal("existing unlinked exact number not resolved")
	}
	stale := e
	e.Notes = "newer"
	save()
	stale.CurrentLoad = ""
	if _, err = repo.Save(ctx, []DriverBoardEntry{stale}); !errors.Is(err, ErrDriverBoardConflict) {
		t.Fatal("stale clear accepted", err)
	}
	if !view().DestinationSource {
		t.Fatal("failed save changed source")
	}
}
