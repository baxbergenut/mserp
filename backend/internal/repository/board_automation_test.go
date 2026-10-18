package repository

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"mserp/internal/datatruck"
	"mserp/internal/fiveeld"
)

func TestBoardSchedules(t *testing.T) {
	var stop datatruck.LoadStop
	raw := `{"id":101555,"stop_type":"delivery","schedule_type":"window","scheduled_begin_date":"2026-10-16","scheduled_begin_time":"08:00:00","scheduled_end_date":"2026-10-16","scheduled_end_time":"23:59:00","scheduled_begin_at":"2026-10-16T12:00:00Z","location":{"latitude":"40.46211","longitude":"-74.3909193"}}`
	if err := json.Unmarshal([]byte(raw), &stop); err != nil {
		t.Fatal(err)
	}
	if got := boardSchedule(stop); got.Label != "DEL 24/7 FRI" || !strings.Contains(got.Detail, "08:00") {
		t.Fatal(got)
	}
	encoded, _ := json.Marshal(datatruck.Load{Stops: []datatruck.LoadStop{stop}})
	projected := boardStops(encoded)
	if len(projected) != 1 || projected[0].Schedule.Label != "DEL 24/7 FRI" || projected[0].Latitude == nil || projected[0].ProviderID != 101555 {
		t.Fatal("schedule fields lost in import", string(encoded))
	}
	stop.ScheduleType = "appointment"
	stop.ScheduledEndDate = ""
	stop.ScheduledEndTime = ""
	stop.ScheduledBeginTime = "15:00:00"
	if boardSchedule(stop).Label != "DEL 15:00 FRI" {
		t.Fatal(boardSchedule(stop))
	}
	stop.ScheduledBeginTime = "00:00:00"
	if boardSchedule(stop).Label != "DEL 00:00 FRI" {
		t.Fatal("midnight is a real appointment")
	}
	stop.ScheduleType = ""
	if boardSchedule(stop).Label != "" {
		t.Fatal("unknown type inferred")
	}
	stop.ScheduleType = "window"
	stop.ScheduledEndDate = "2026-10-17"
	stop.ScheduledEndTime = "02:00:00"
	if boardSchedule(stop).Label != "DEL 24/7 FRI" {
		t.Fatal("overnight window lost")
	}
	stop.ScheduledEndDate = "2026-10-15"
	if boardSchedule(stop).Label != "" {
		t.Fatal("reversed window accepted")
	}
	stop.ScheduledBeginDate = "2026-02-30"
	if boardSchedule(stop).Label != "" {
		t.Fatal("invalid date accepted")
	}
}

func TestArrivalEvidence(t *testing.T) {
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	lat, lng := 40.0, -74.0
	stop := BoardStop{Latitude: &lat, Longitude: &lng}
	point := func(at time.Time, lat float64) *DriverBoardLocation {
		return &DriverBoardLocation{Latitude: lat, Longitude: lng, ReportedAt: at}
	}
	a := evaluateArrival(ArrivalEvidence{}, "one", point(now.Add(-4*time.Minute), lat), stop, now)
	if a.Arrived {
		t.Fatal("one sample establishes arrival")
	}
	same := evaluateArrival(a, "one", point(a.LastAt, lat), stop, now)
	if same != a {
		t.Fatal("cached sample advances state")
	}
	a = evaluateArrival(a, "one", point(now, lat), stop, now)
	if !a.Arrived {
		t.Fatal("dwell failed")
	}
	stale := evaluateArrival(a, "one", point(now.Add(-time.Hour), lat+1), stop, now)
	if stale != a {
		t.Fatal("stale sample changed arrival")
	}
	a = evaluateArrival(a, "one", point(now.Add(time.Minute), lat+0.01), stop, now.Add(time.Minute))
	if !a.Arrived {
		t.Fatal("one outside sample cleared arrival")
	}
	a = evaluateArrival(a, "one", point(now.Add(2*time.Minute), lat+0.01), stop, now.Add(2*time.Minute))
	if a.Arrived {
		t.Fatal("departure failed")
	}
	a = evaluateArrival(a, "two", nil, stop, now)
	if a.Context != "two" || !a.LastAt.IsZero() {
		t.Fatal("new context reused evidence")
	}
}

func TestTrackingMotion(t *testing.T) {
	now := time.Now()
	slow, fast := 0.0, 8.04672
	points := []fiveeld.TrackingPoint{{Date: now.Add(-4 * time.Minute), Speed: &slow}, {Date: now, Speed: &slow}}
	motion, _ := trackingMotion(points, "mph", "")
	if motion != "stopped" {
		t.Fatal(motion)
	}
	points = append(points, fiveeld.TrackingPoint{Date: now.Add(time.Minute), Speed: &fast})
	motion, _ = trackingMotion(points, "kph", motion)
	if motion != "moving" {
		t.Fatal("kph threshold", motion)
	}
	points = append(points, points[len(points)-1])
	motion, _ = trackingMotion(points, "kph", motion)
	if motion != "moving" {
		t.Fatal("duplicate changed movement")
	}
	estimate := BoardTravel{Enabled: true, Context: "one", Motion: "moving", CalculatedAt: now, ReportedAt: now, ArrivalAt: time.Date(2026, 10, 17, 19, 0, 0, 0, time.UTC)}
	if value, _ := estimate.display("one", now); value != "ETA 15:00" {
		t.Fatal(value)
	}
	if value, _ := estimate.display("two", now); value != "" {
		t.Fatal("cross-load ETA")
	}
	if value, _ := estimate.display("one", now.Add(11*time.Minute)); value != "" {
		t.Fatal("stale ETA")
	}
}

func testBoardAutomationDatabase(t *testing.T, ctx context.Context, pool *pgxpool.Pool, repo *DriverBoardRepository, fleet *FleetRepository) {
	t.Helper()
	d, err := fleet.CreateDriver(ctx, DriverInput{FullName: "Automation Driver", PayType: "cpm", PayRate: 0.6, Active: true})
	if err != nil {
		t.Fatal(err)
	}
	vin := "1M8GDM9AXKP042789"
	truck, err := fleet.CreateTruck(ctx, TruckInput{UnitNumber: "AUTO-115", VIN: &vin, DriverID: &d.ID, Status: "available", Active: true, IsCompanyOwned: true})
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO loads(id,load_id,status,load_pay,total_pay,raw_payload) VALUES(99115,'AUTO-LOAD','dispatched',0,0,'{"stops":[{"id":101555,"ordering":1,"stop_type":"delivery","schedule_type":"window","scheduled_begin_date":"2026-10-16","scheduled_begin_time":"08:00:00","scheduled_end_date":"2026-10-16","scheduled_end_time":"23:59:00","location":{"city":"South River","state":"NJ","latitude":"40.46211","longitude":"-74.3909193"}}]}')`)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := repo.Save(ctx, []DriverBoardEntry{{DriverID: d.ID, CurrentLoad: "AUTO-LOAD", Status: "ENROUTE", StatusEdited: true, Notes: "Keep human notes", ETA: "15:00"}})
	if err != nil {
		t.Fatal(err)
	}
	view, err := repo.Loads(ctx, d.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if view.Current == nil || view.Current.PlanID != "" || view.Automation == nil || view.Automation.Appointment == nil || view.Automation.Appointment.Label != "DEL 24/7 FRI" {
		t.Fatalf("unplanned resolution: %+v", view)
	}
	result, err := repo.ChangeLoads(ctx, BoardLoadAction{DriverID: d.ID, Version: saved[0].Version, Revision: view.Revision, Action: "arrival_yes"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Loads.Automation.Arrived || result.Entry.Notes != "Keep human notes" {
		t.Fatal("arrival correction lost", result)
	}
	var count int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM gross_board_entries WHERE driver_id=$1`, d.ID).Scan(&count); err != nil || count != 0 {
		t.Fatal("synthetic plan created", err)
	}
	// Repeated GETs must never establish arrival or add audit entries.
	if err = repo.ReconcileAutomation(ctx, nil, nil, ""); err != nil {
		t.Fatal(err)
	}
	view, err = repo.Loads(ctx, d.ID, "")
	if err != nil || !view.Automation.Arrived {
		t.Fatal("manual arrival did not survive cache refresh", err)
	}
	_, err = pool.Exec(ctx, `UPDATE loads SET driver_id=(SELECT id FROM drivers WHERE id<>$1 LIMIT 1) WHERE id=99115`, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	view, err = repo.Loads(ctx, d.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if view.Automation.RequiresConfirmation || view.Automation.Appointment == nil {
		t.Fatal("source driver change blocked the operational selection")
	}
	result, err = repo.ChangeLoads(ctx, BoardLoadAction{DriverID: d.ID, Version: result.Entry.Version, Revision: view.Revision, Action: "arrival_yes"}, "")
	if err != nil || result.Loads.Automation.RequiresConfirmation {
		t.Fatal("arrival correction required driver confirmation", err)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM driver_board_automation WHERE driver_id=$1`, d.ID).Scan(&count); err != nil || count != 1 {
		t.Fatal("missing cached evidence", err)
	}
	result, err = repo.ChangeLoads(ctx, BoardLoadAction{DriverID: d.ID, Version: result.Entry.Version, Revision: result.Loads.Revision, Action: "arrival_auto"}, "")
	if err != nil {
		t.Fatal(err)
	}
	var historyBefore, historyAfter int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM driver_board_history WHERE driver_id=$1`, d.ID).Scan(&historyBefore); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	_, err = pool.Exec(ctx, `INSERT INTO five_eld_locations(vin,latitude,longitude,reported_at,fetched_at) VALUES($1,40.46211,-74.3909193,$2,now())`, vin, now.Add(-4*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if err = repo.ReconcileAutomation(ctx, nil, nil, ""); err != nil {
		t.Fatal(err)
	}
	if err = repo.ReconcileAutomation(ctx, nil, nil, ""); err != nil {
		t.Fatal(err)
	}
	view, err = repo.Loads(ctx, d.ID, "")
	if err != nil || view.Automation.Arrived {
		t.Fatal("duplicate cached point established GPS arrival", err)
	}
	_, err = pool.Exec(ctx, `UPDATE five_eld_locations SET reported_at=$2 WHERE vin=$1`, vin, now)
	if err != nil {
		t.Fatal(err)
	}
	if err = repo.ReconcileAutomation(ctx, nil, nil, ""); err != nil {
		t.Fatal(err)
	}
	view, err = repo.Loads(ctx, d.ID, "")
	if err != nil || !view.Automation.Arrived {
		t.Fatal("GPS dwell not projected", err)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM driver_board_history WHERE driver_id=$1`, d.ID).Scan(&historyAfter); err != nil || historyAfter != historyBefore {
		t.Fatal("polls polluted personal history", err)
	}
	// An asynchronous estimate from before a human edit must be discarded.
	entry := result.Entry
	entry.Notes = "Reviewed notes"
	saved, err = repo.Save(ctx, []DriverBoardEntry{entry})
	if err != nil {
		t.Fatal(err)
	}
	travel := BoardTravel{Enabled: true, Context: view.Automation.Context, CalculatedAt: now, ReportedAt: now, ArrivalAt: now.Add(time.Hour), Motion: "moving", Miles: 133}
	if err = repo.storeBoardTravel(ctx, d.ID, truck.ID, view.Revision, view.Automation.Context, travel); err != nil {
		t.Fatal(err)
	}
	var cached BoardTravel
	if err = pool.QueryRow(ctx, `SELECT travel FROM driver_board_automation WHERE driver_id=$1`, d.ID).Scan(&cached); err != nil || cached.Context != "" {
		t.Fatal("stale estimate crossed version fence", err)
	}
	view, err = repo.Loads(ctx, d.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if err = repo.storeBoardTravel(ctx, d.ID, truck.ID, view.Revision, view.Automation.Context, travel); err != nil {
		t.Fatal(err)
	}
	view, err = repo.Loads(ctx, d.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	result, err = repo.ChangeLoads(ctx, BoardLoadAction{DriverID: d.ID, Version: saved[0].Version, Revision: view.Revision, Action: "travel_auto"}, "")
	if err != nil || !strings.HasPrefix(result.Loads.Automation.TravelValue, "ETA ") {
		t.Fatal("automatic estimate projection", err)
	}
	_, _ = pool.Exec(ctx, `DELETE FROM five_eld_locations WHERE vin=$1`, vin)
	_, _ = pool.Exec(ctx, `DELETE FROM drivers WHERE id=$1`, d.ID)
	_, _ = pool.Exec(ctx, `DELETE FROM trucks WHERE id=$1`, truck.ID)
	_, _ = pool.Exec(ctx, `DELETE FROM loads WHERE id=99115`)
}
