package repository

import (
	"context"
	"math"
	"mserp/internal/boardroutes"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestReadyLocationDue(t *testing.T) {
	now := time.Date(2026, 11, 9, 15, 0, 0, 0, time.UTC)
	base := BoardReadyLocation{Context: "ready", Source: "five_eld", CalculatedAt: now.Add(-90 * time.Minute), AttemptedAt: now.Add(-90 * time.Minute), ReportedAt: now.Add(-90 * time.Minute), Position: &boardRoutePosition{Latitude: 40, Longitude: -74}}
	p := DriverBoardLocation{Source: "five_eld", Latitude: 40.02, Longitude: -74, ReportedAt: now}
	for _, tc := range []struct {
		name   string
		change func(*BoardReadyLocation, *DriverBoardLocation)
		key    string
		want   bool
	}{
		{"ninety minutes and mile", func(*BoardReadyLocation, *DriverBoardLocation) {}, "ready", true},
		{"movement alone", func(c *BoardReadyLocation, p *DriverBoardLocation) { c.CalculatedAt = now.Add(-89 * time.Minute) }, "ready", false},
		{"time alone", func(c *BoardReadyLocation, p *DriverBoardLocation) { p.Latitude = 40.001 }, "ready", false},
		{"new transition bypasses gates", func(c *BoardReadyLocation, p *DriverBoardLocation) { c.AttemptedAt = now; p.Latitude = 40 }, "new", true},
		{"failed attempt backoff", func(c *BoardReadyLocation, p *DriverBoardLocation) { c.AttemptedAt = now.Add(-4 * time.Minute) }, "ready", false},
		{"backoff expires", func(c *BoardReadyLocation, p *DriverBoardLocation) { c.AttemptedAt = now.Add(-5 * time.Minute) }, "ready", true},
		{"same GPS observation", func(c *BoardReadyLocation, p *DriverBoardLocation) { p.ReportedAt = c.ReportedAt }, "ready", false},
		{"older GPS", func(c *BoardReadyLocation, p *DriverBoardLocation) { p.ReportedAt = c.ReportedAt.Add(-time.Minute) }, "ready", false},
		{"future GPS", func(c *BoardReadyLocation, p *DriverBoardLocation) { p.ReportedAt = now.Add(time.Second) }, "new", false},
		{"missing GPS time", func(c *BoardReadyLocation, p *DriverBoardLocation) { p.ReportedAt = time.Time{} }, "new", false},
		{"Samsara forbidden", func(c *BoardReadyLocation, p *DriverBoardLocation) { p.Source = "samsara" }, "new", false},
		{"NaN forbidden", func(c *BoardReadyLocation, p *DriverBoardLocation) { p.Latitude = math.NaN() }, "new", false},
		{"infinity forbidden", func(c *BoardReadyLocation, p *DriverBoardLocation) { p.Longitude = math.Inf(1) }, "new", false},
		{"latitude range", func(c *BoardReadyLocation, p *DriverBoardLocation) { p.Latitude = 91 }, "new", false},
		{"zero coordinate", func(c *BoardReadyLocation, p *DriverBoardLocation) { p.Latitude = 0; p.Longitude = 0 }, "new", false},
		{"last known initial lookup", func(c *BoardReadyLocation, p *DriverBoardLocation) { p.ReportedAt = now.Add(-24 * time.Hour) }, "new", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, q := base, p
			tc.change(&c, &q)
			if got := c.due(tc.key, &q, now); got != tc.want {
				t.Fatalf("due = %v, want %v", got, tc.want)
			}
		})
	}
	if base.due("new", nil, now) {
		t.Fatal("missing GPS became eligible")
	}
}

func TestReadyLocationContextAndStaleness(t *testing.T) {
	now := time.Date(2026, 11, 9, 15, 0, 0, 0, time.UTC)
	next := now.Add(time.Nanosecond)
	a := readyLocationContext("truck/assignment/vin", &now)
	if a != readyLocationContext("truck/assignment/vin", &now) || a == readyLocationContext("other/assignment/vin", &now) || a == readyLocationContext("truck/assignment/vin", &next) {
		t.Fatal("context must fence truck and every transition")
	}
	c := BoardReadyLocation{ReportedAt: now.Add(-90 * time.Minute)}
	if c.stale(now, &now) {
		t.Fatal("fresh GPS and within cadence should not be stale")
	}
	old := now.Add(-11 * time.Minute)
	future := now.Add(time.Second)
	for _, at := range []*time.Time{nil, &old, &future} {
		if !c.stale(now, at) {
			t.Fatal("missing/stale/future GPS must label last-known address")
		}
	}
	c.ReportedAt = now.Add(-96 * time.Minute)
	if !c.stale(now, &now) {
		t.Fatal("aged address must be stale")
	}
}

// Runs inside the existing fresh-and-migrated disposable local schema harness.
func testReadyLocationDatabase(t *testing.T, ctx context.Context, pool *pgxpool.Pool, repo *DriverBoardRepository, fleet *FleetRepository) {
	t.Helper()
	d, err := fleet.CreateDriver(ctx, DriverInput{FullName: "Ready Location Test", PayType: "cpm", PayRate: 0.6, Active: true})
	if err != nil {
		t.Fatal(err)
	}
	vin := "1M8GDM9AXKP047207"
	truck, err := fleet.CreateTruck(ctx, TruckInput{UnitNumber: "READY-207", VIN: &vin, DriverID: &d.ID, Status: "available", Active: true, IsCompanyOwned: true})
	if err != nil {
		t.Fatal(err)
	}
	listener, err := pgx.ConnectConfig(ctx, pool.Config().ConnConfig.Copy())
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close(ctx)
	if _, err = listener.Exec(ctx, "LISTEN board_ready_location"); err != nil {
		t.Fatal(err)
	}
	saved, err := repo.Save(ctx, []DriverBoardEntry{{DriverID: d.ID, Status: "READY", StatusEdited: true, Destination: "Keep saved override", ETA: "123mi", Notes: "Keep notes"}})
	if err != nil {
		t.Fatal(err)
	}
	noticeCtx, cancelNotice := context.WithTimeout(ctx, 2*time.Second)
	notice, noticeErr := listener.WaitForNotification(noticeCtx)
	cancelNotice()
	if noticeErr != nil || notice.Payload != d.ID {
		t.Fatal("READY transition did not notify", notice, noticeErr)
	}
	var requested time.Time
	if err = pool.QueryRow(ctx, `SELECT ready_location_requested_at FROM driver_board_automation WHERE driver_id=$1`, d.ID).Scan(&requested); err != nil {
		t.Fatal("READY transition must durably request immediate lookup", err)
	}
	view, err := repo.Loads(ctx, d.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	a := view.Automation
	if a == nil || !a.ready || a.TravelValue != "" || a.TravelMode != "none" || a.ManualETAValid || a.RoutingEnabled || a.Appointment != nil {
		t.Fatalf("READY exposes ETA routing: %+v", a)
	}
	before := a.Context
	now := time.Now().UTC()
	if _, err = pool.Exec(ctx, `INSERT INTO five_eld_locations(vin,latitude,longitude,reported_at,fetched_at) VALUES($1,40,-74,$2,now())`, vin, now); err != nil {
		t.Fatal(err)
	}
	cache := BoardReadyLocation{Context: before, Source: "five_eld", Address: "123 Main St, Test City", CalculatedAt: now, ReportedAt: now, Position: &boardRoutePosition{Latitude: 40, Longitude: -74}}
	accepted, err := repo.storeReadyLocation(ctx, d.ID, truck.ID, before, cache)
	if err != nil || !accepted {
		t.Fatal("current READY cache rejected", err)
	}
	view, err = repo.Loads(ctx, d.ID, "")
	if err != nil || view.Automation.ReadyLocationAddress != cache.Address || view.Automation.ReadyLocationStale {
		t.Fatal("fresh cached address not projected", err)
	}
	var preserved, preservedETA string
	if err = pool.QueryRow(ctx, `SELECT destination,eta FROM driver_board WHERE driver_id=$1`, d.ID).Scan(&preserved, &preservedETA); err != nil || preserved != "Keep saved override" || preservedETA != "123mi" {
		t.Fatal("automatic address overwrote manual destination", err)
	}
	entry := saved[0]
	entry.Notes = "Changed notes"
	saved, err = repo.Save(ctx, []DriverBoardEntry{entry})
	if err != nil {
		t.Fatal(err)
	}
	view, err = repo.Loads(ctx, d.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if view.Automation.Context != before {
		t.Fatal("unrelated edit reset READY lookup")
	}
	entry = saved[0]
	entry.Destination = ""
	_, err = repo.Save(ctx, []DriverBoardEntry{entry})
	if err != nil {
		t.Fatal(err)
	}
	view, err = repo.Loads(ctx, d.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if view.Automation.Context == before {
		t.Fatal("clearing override did not request immediate lookup")
	}
	accepted, err = repo.storeReadyLocation(ctx, d.ID, truck.ID, before, cache)
	if err != nil || accepted {
		t.Fatal("obsolete lookup crossed READY transition fence", err)
	}
	accepted, err = repo.storeReadyLocation(ctx, d.ID, "other-truck", view.Automation.Context, cache)
	if err != nil || accepted {
		t.Fatal("obsolete truck lookup crossed fence", err)
	}
	var destination, eta, notes string
	if err = pool.QueryRow(ctx, `SELECT destination,eta,notes FROM driver_board WHERE driver_id=$1`, d.ID).Scan(&destination, &eta, &notes); err != nil || destination != "" || notes != "Changed notes" {
		t.Fatal("READY projection destroyed saved data", destination, eta, notes, err)
	}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		if request.URL.Path != "/40.0000000,-74.0000000.json" {
			t.Errorf("unexpected geocode path %s", request.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"addresses":[{"address":{"freeformAddress":"456 Current St, Test City"}}]}`))
	}))
	defer server.Close()
	routes := boardroutes.NewClient(boardroutes.Options{Enabled: true, TomTomAPIKey: "local-test-only", TomTomBillingTimezone: "UTC", RequestsPerMinute: 120, ReverseGeocodeMonthlyLimit: 1000})
	routes.TomTomReverseURL = server.URL
	routes.Budget = boardroutes.NewPostgresBudget(pool)
	if err = repo.RefreshReadyLocation(ctx, routes, d.ID); err != nil {
		t.Fatal(err)
	}
	view, err = repo.Loads(ctx, d.ID, "")
	if err != nil || view.Automation.ReadyLocationAddress != "456 Current St, Test City" || calls.Load() != 1 {
		t.Fatal("worker did not publish reverse-geocoded address", calls.Load(), view.Automation, err)
	}
	if err = repo.RefreshReadyLocation(ctx, routes, d.ID); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatal("duplicate immediate refresh bypassed cache", calls.Load())
	}
	var usage int
	if err = pool.QueryRow(ctx, `SELECT requests FROM board_route_usage WHERE provider='tomtom_reverse' AND month_start=date_trunc('month',now() AT TIME ZONE 'UTC')::date`).Scan(&usage); err != nil || usage != 1 {
		t.Fatal("reverse lookup not independently counted", usage, err)
	}
	if err = pool.QueryRow(ctx, `SELECT destination FROM driver_board WHERE driver_id=$1`, d.ID).Scan(&preserved); err != nil || preserved != "" {
		t.Fatal("worker persisted automatic destination as manual", preserved, err)
	}
}
