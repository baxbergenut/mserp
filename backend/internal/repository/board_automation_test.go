package repository

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"mserp/internal/boardroutes"
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
	// Just inside and outside five straight-line miles, without a second sample.
	inside, outside := lat+0.072, lat+0.073
	if distanceMeters(lat, lng, inside, lng) >= boardArrivalRadiusMeters || distanceMeters(lat, lng, outside, lng) <= boardArrivalRadiusMeters {
		t.Fatal("test coordinates do not straddle five miles")
	}
	a := evaluateArrival(ArrivalEvidence{}, "one", point(now.Add(-4*time.Minute), outside), stop, now)
	if a.Arrived {
		t.Fatal("outside point established proximity")
	}
	same := evaluateArrival(a, "one", point(a.LastAt, outside), stop, now)
	if same != a {
		t.Fatal("unchanged cached sample changed state")
	}
	legacy := ArrivalEvidence{Context: "one", LastAt: now.Add(-4 * time.Minute)}
	if updated := evaluateArrival(legacy, "one", point(legacy.LastAt, inside), stop, now); !updated.Arrived {
		t.Fatal("cached point was not re-evaluated after proximity rule change")
	}
	if lastKnown := evaluateArrival(ArrivalEvidence{}, "one", point(now.Add(-30*time.Minute), inside), stop, now); !lastKnown.Arrived || !lastKnown.LastAt.Equal(now.Add(-30*time.Minute)) {
		t.Fatal("last-known proximity was discarded")
	}
	a = evaluateArrival(a, "one", point(now, inside), stop, now)
	if !a.Arrived {
		t.Fatal("first fresh point inside five miles did not establish proximity")
	}
	outOfOrder := evaluateArrival(a, "one", point(now.Add(-time.Hour), lat+1), stop, now)
	if outOfOrder != a {
		t.Fatal("out-of-order sample changed proximity")
	}
	a = evaluateArrival(a, "one", point(now.Add(time.Minute), outside), stop, now.Add(time.Minute))
	if a.Arrived {
		t.Fatal("first fresh point outside five miles did not clear proximity")
	}
	a = evaluateArrival(a, "one", point(now.Add(2*time.Minute), inside), stop, now.Add(2*time.Minute))
	if !a.Arrived {
		t.Fatal("returning within five miles did not restore proximity")
	}
	a = evaluateArrival(a, "two", nil, stop, now)
	if a.Context != "two" || !a.LastAt.IsZero() {
		t.Fatal("new context reused evidence")
	}
	if a.Arrived {
		t.Fatal("new stop reused proximity")
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
	if value, _ := estimate.display("one", now.Add(11*time.Minute)); value != "0mi" {
		t.Fatal("stale ETA did not become last known miles", value)
	}
	estimate.Motion = "stopped"
	estimate.Miles = 133
	if value, reason := estimate.display("one", now.Add(12*time.Hour)); value != "133mi" || !strings.Contains(reason, "GPS data stale") || !strings.Contains(reason, "last observed") {
		t.Fatal("resting truck lost last known miles", value, reason)
	}
	if value, _ := estimate.display("two", now.Add(12*time.Hour)); value != "" {
		t.Fatal("stale miles crossed load/stop/truck context")
	}
	motion, _ = trackingMotion([]fiveeld.TrackingPoint{{Date: now.Add(time.Hour), Speed: &slow}}, "mph", "stopped")
	if motion != "stopped" {
		t.Fatal("resting truck lost last stopped state", motion)
	}
	motion, _ = trackingMotion([]fiveeld.TrackingPoint{{Date: now.Add(time.Hour), Speed: &fast}}, "mph", "stopped")
	if motion != "moving" {
		t.Fatal("fresh movement did not replace last stopped state", motion)
	}
	estimate.CalculatedAt = time.Time{}
	if value, _ := estimate.display("one", now.Add(12*time.Hour)); value != "" {
		t.Fatal("invalidated route retained stopped miles")
	}
}

func TestBoardTravelDistanceDisplay(t *testing.T) {
	now := time.Date(2026, 10, 17, 18, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name   string
		motion string
		miles  float64
		age    time.Duration
		want   string
	}{
		{"below threshold", "moving", 599.99, 0, "ETA 15:00"},
		{"at threshold", "moving", 600, 0, "ETA 15:00"},
		{"above threshold", "moving", 600.01, 0, "601mi"},
		{"long haul", "moving", 1000, 0, "1000mi"},
		{"stopped nearby", "stopped", 133, 0, "133mi"},
		{"unknown movement", "", 1000, 0, ""},
		{"stale moving miles", "moving", 1000, 11 * time.Minute, "1000mi"},
		{"stale nearby moving", "moving", 133, 11 * time.Minute, "133mi"},
		{"stale unknown movement", "", 133, 12 * time.Hour, "133mi"},
		{"resting long haul", "stopped", 1000, 12 * time.Hour, "1000mi"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			estimate := BoardTravel{Enabled: true, Context: "one", Motion: tc.motion, Miles: tc.miles, Assumptions: boardroutes.StandardSemiAssumptions,
				CalculatedAt: now, ReportedAt: now, ArrivalAt: now.Add(time.Hour)}
			got, reason := estimate.display("one", now.Add(tc.age))
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
			if got != "" && !strings.Contains(reason, boardroutes.StandardSemiAssumptions) {
				t.Fatal("profile assumptions missing from hover", reason)
			}
		})
	}
}

func TestBoardTravelETARounding(t *testing.T) {
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct{ arrival, want string }{
		{"2026-10-20T14:07:29Z", "ETA 10:00"},
		{"2026-10-20T14:07:30Z", "ETA 10:15"},
		{"2026-10-20T14:22:30Z", "ETA 10:30"},
		{"2026-10-20T14:37:30Z", "ETA 10:45"},
		{"2026-10-20T14:52:30Z", "ETA 11:00"},
		{"2026-10-21T03:59:00Z", "ETA 00:00"},
	} {
		arrival, _ := time.Parse(time.RFC3339, tc.arrival)
		travel := BoardTravel{Enabled: true, Context: "one", Motion: "moving", CalculatedAt: now, ReportedAt: now, ArrivalAt: arrival}
		if got, _ := travel.display("one", now); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.arrival, got, tc.want)
		}
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
	if view.Current == nil || view.Current.PlanID != "" || view.Automation == nil || view.Automation.Appointment == nil || view.Automation.Appointment.Label != "DEL 24/7 FRI" || view.Automation.StopType != "delivery" {
		t.Fatalf("unplanned resolution: %+v", view)
	}
	result, err := repo.ChangeLoads(ctx, BoardLoadAction{DriverID: d.ID, Version: saved[0].Version, Revision: view.Revision, Action: "stop_time", ManualStopTime: "14:00 SUN"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if result.Loads.Automation.ManualStopTime != "14:00 SUN" || result.Loads.Automation.Appointment.Label != "DEL 14:00 SUN" || result.Entry.Notes != "Keep human notes" {
		t.Fatal("manual stop time lost", result)
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
	if err != nil || view.Automation.ManualStopTime != "14:00 SUN" || view.Automation.Arrived {
		t.Fatal("manual stop time did not survive cache refresh or arrival changed", err)
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
	result, err = repo.ChangeLoads(ctx, BoardLoadAction{DriverID: d.ID, Version: result.Entry.Version, Revision: view.Revision, Action: "stop_time", ManualStopTime: "15:00 SUN"}, "")
	if err != nil || result.Loads.Automation.RequiresConfirmation || result.Loads.Automation.Appointment.Label != "DEL 15:00 SUN" {
		t.Fatal("manual stop time required driver confirmation", err)
	}
	_, err = pool.Exec(ctx, `UPDATE loads SET raw_payload=jsonb_set(jsonb_set(raw_payload,'{stops,0,scheduled_begin_date}','"2026-10-17"'),'{stops,0,scheduled_end_date}','"2026-10-17"') WHERE id=99115`)
	if err != nil {
		t.Fatal(err)
	}
	view, err = repo.Loads(ctx, d.ID, "")
	if err != nil || view.Automation.Appointment.Label != "DEL 15:00 SUN" {
		t.Fatal("DataTruck schedule update replaced manual stop time", err, view.Automation)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM driver_board_automation WHERE driver_id=$1`, d.ID).Scan(&count); err != nil || count != 1 {
		t.Fatal("missing cached evidence", err)
	}
	result, err = repo.ChangeLoads(ctx, BoardLoadAction{DriverID: d.ID, Version: result.Entry.Version, Revision: view.Revision, Action: "stop_time", ManualStopTime: ""}, "")
	if err != nil {
		t.Fatal(err)
	}
	if result.Loads.Automation.ManualStopTime != "" || result.Loads.Automation.Appointment.Label != "DEL 24/7 SAT" {
		t.Fatal("clearing manual stop time did not restore DataTruck schedule", result.Loads.Automation)
	}
	var historyBefore, historyAfter int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM driver_board_history WHERE driver_id=$1`, d.ID).Scan(&historyBefore); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	_, err = pool.Exec(ctx, `INSERT INTO five_eld_locations(vin,latitude,longitude,reported_at,fetched_at) VALUES($1,40.55211,-74.3909193,$2,now())`, vin, now.Add(-4*time.Minute))
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
		t.Fatal("outside point established GPS proximity", err)
	}
	_, err = pool.Exec(ctx, `UPDATE five_eld_locations SET latitude=40.46211,reported_at=$2 WHERE vin=$1`, vin, now)
	if err != nil {
		t.Fatal(err)
	}
	if err = repo.ReconcileAutomation(ctx, nil, nil, ""); err != nil {
		t.Fatal(err)
	}
	view, err = repo.Loads(ctx, d.ID, "")
	if err != nil || !view.Automation.Arrived {
		t.Fatal("GPS proximity not projected", err)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM driver_board_history WHERE driver_id=$1`, d.ID).Scan(&historyAfter); err != nil || historyAfter != historyBefore {
		t.Fatal("polls polluted personal history", err)
	}
	// An asynchronous estimate from before a human edit must be discarded.
	entry := result.Entry
	entry.Notes = "Reviewed notes"
	entry.ETA = ""
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
	if view.Automation.TravelMode != "auto" || !strings.HasPrefix(view.Automation.TravelValue, "ETA ") {
		t.Fatal("automatic estimate projection", view.Automation)
	}
	// Stale GPS can bootstrap distance without inventing movement or querying ELD.
	_, err = pool.Exec(ctx, `UPDATE five_eld_locations SET reported_at=$2 WHERE vin=$1`, vin, now.Add(-11*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	var routeCalls atomic.Int32
	routeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		routeCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"routes":[{"duration":"3600s","distanceMeters":160934,"travelAdvisory":{"routeRestrictionsPartiallyIgnored":false}}]}`))
	}))
	defer routeServer.Close()
	routes := boardroutes.NewClient(boardroutes.Options{Enabled: true, StandardSemi: true, SpeedUnit: "mph", RequestsPerMinute: 120})
	routes.URL = routeServer.URL
	// No existing route: first enable while the truck is off must still show miles.
	_, err = pool.Exec(ctx, `UPDATE driver_board_automation SET travel='{}' WHERE driver_id=$1`, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = repo.ReconcileAutomation(ctx, routes, nil, ""); err != nil {
		t.Fatal(err)
	}
	view, err = repo.Loads(ctx, d.ID, "")
	if err != nil || !view.Automation.RoutingEnabled || view.Automation.TravelValue != "100mi" || !strings.Contains(view.Automation.Reason, "GPS data stale") {
		t.Fatal("stale GPS did not bootstrap last known miles", err, view.Automation)
	}
	if err = pool.QueryRow(ctx, `SELECT travel FROM driver_board_automation WHERE driver_id=$1`, d.ID).Scan(&cached); err != nil || !cached.LastKnownPosition || !cached.ArrivalAt.IsZero() || cached.Motion != "" {
		t.Fatal("stale position invented ETA or movement", err, cached)
	}
	calls := routeCalls.Load()
	if calls == 0 {
		t.Fatal("last known distance was not routed")
	}
	if err = repo.ReconcileAutomation(ctx, routes, nil, ""); err != nil || routeCalls.Load() != calls {
		t.Fatal("unchanged resting position caused another route request", err)
	}
	// Fresh moving telemetry replaces the distance immediately, bypassing retry delay.
	_, err = pool.Exec(ctx, `UPDATE five_eld_locations SET reported_at=$2 WHERE vin=$1`, vin, now)
	if err != nil {
		t.Fatal(err)
	}
	eldServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if strings.Contains(req.URL.Path, "/active-units/") {
			_ = json.NewEncoder(w).Encode([]map[string]string{{"id": "fixture-truck", "vin": vin}})
			return
		}
		speed := 10.0
		p := fiveeld.TrackingPoint{Date: now, Speed: &speed}
		p.Coordinates.Lat, p.Coordinates.Lng = 40.46211, -74.3909193
		_ = json.NewEncoder(w).Encode([]fiveeld.TrackingPoint{p})
	}))
	defer eldServer.Close()
	eld := fiveeld.NewClientWithHTTPClient(eldServer.URL, "fixture", "fixture", eldServer.Client())
	if err = repo.ReconcileAutomation(ctx, routes, eld, "fixture"); err != nil {
		t.Fatal(err)
	}
	view, err = repo.Loads(ctx, d.ID, "")
	if err != nil || !strings.HasPrefix(view.Automation.TravelValue, "ETA ") || routeCalls.Load() <= calls {
		t.Fatal("fresh movement did not promptly replace last known miles", err, view.Automation)
	}
	// An older Samsara observation has priority, invalidates Five ELD evidence
	// immediately, and routes from cached mph samples without calling Five ELD.
	_, err = pool.Exec(ctx, `INSERT INTO samsara_telemetry(vin,vehicle_id,unit_number,latitude,longitude,speed_mph,gps_at,fetched_at) VALUES($1,'automation-samsara','Samsara',40.55211,-74.3909193,6,$2,$3)`, vin, now.Add(-time.Minute), now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO samsara_tracking(vin,reported_at,latitude,longitude,speed_mph) VALUES($1,$2,40.55211,-74.3909193,6)`, vin, now.Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	view, err = repo.Loads(ctx, d.ID, "")
	if err != nil || view.Automation.Arrived || view.Automation.TravelValue != "" {
		t.Fatal("previous provider evidence remained visible", view.Automation, err)
	}
	routes.Options.SpeedUnit = "kph"
	calls = routeCalls.Load()
	if err = repo.ReconcileAutomation(ctx, routes, nil, ""); err != nil {
		t.Fatal(err)
	}
	view, err = repo.Loads(ctx, d.ID, "")
	if err != nil || view.Automation.Arrived || !strings.HasPrefix(view.Automation.TravelValue, "ETA ") || routeCalls.Load() <= calls {
		t.Fatal("Samsara priority/mph routing failed", view.Automation, err)
	}
	if err = pool.QueryRow(ctx, `SELECT travel FROM driver_board_automation WHERE driver_id=$1`, d.ID).Scan(&cached); err != nil || cached.Source != "samsara" {
		t.Fatal("routing used another provider", cached, err)
	}
	_, _ = pool.Exec(ctx, `DELETE FROM samsara_telemetry WHERE vin=$1`, vin)
	_, _ = pool.Exec(ctx, `DELETE FROM five_eld_locations WHERE vin=$1`, vin)
	_, _ = pool.Exec(ctx, `DELETE FROM drivers WHERE id=$1`, d.ID)
	_, _ = pool.Exec(ctx, `DELETE FROM trucks WHERE id=$1`, truck.ID)
	_, _ = pool.Exec(ctx, `DELETE FROM loads WHERE id=99115`)
}
