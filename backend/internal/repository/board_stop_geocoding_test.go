package repository

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"mserp/internal/boardroutes"
)

func TestBoardStopGeocodeLabel(t *testing.T) {
	address := boardroutes.ReverseAddress{
		Municipality: "Berlin", LocalName: "Newport", State: "MI", StateName: "Michigan", PostalCode: "48166",
	}
	for _, tc := range []struct{ state, postal, want string }{
		{"Michigan", "48166-9275", "Newport, MI"},
		{"MI", "48166", "Newport, MI"},
		{"", "", "Newport, MI"},
		{"OH", "48166", ""},
		{"Michigan", "48167", ""},
	} {
		if got := (boardStopGeocode{state: tc.state, postalCode: tc.postal}).label(address); got != tc.want {
			t.Errorf("state %q ZIP %q: got %q, want %q", tc.state, tc.postal, got, tc.want)
		}
	}
	address.LocalName = ""
	if got := (boardStopGeocode{state: "MI", postalCode: "48166"}).label(address); got != "Berlin, MI" {
		t.Errorf("municipality fallback = %q", got)
	}
}

func testBoardStopGeocoding(t *testing.T, ctx context.Context, pool *pgxpool.Pool, repo *DriverBoardRepository, fleet *FleetRepository) {
	t.Helper()
	clocked := *repo
	repo = &clocked
	repo.now = func() time.Time { return time.Date(2026, 11, 13, 15, 0, 0, 0, time.UTC) }
	d, err := fleet.CreateDriver(ctx, DriverInput{FullName: "Stop Geocoding Test", PayType: "cpm", PayRate: 0.6, Active: true})
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
		exec(`DELETE FROM loads WHERE id=88216`)
	}()
	exec(`INSERT INTO loads(id,load_id,status,load_pay,total_pay,raw_payload) VALUES(88216,'STOP-GEOCODE','in_transit',100,100,
 '{"stops":[{"id":1,"ordering":1,"stop_type":"pickup","location":{"city":"Chester","state":"Pennsylvania","latitude":"39.8506982","longitude":"-75.360781"}},
 {"id":2,"ordering":2,"stop_type":"delivery","location":{"state":"Michigan","zip_code":"48166","address1":"8857 SWAN CREEK RD","latitude":"41.9930662","longitude":"-83.3198394"}}]}')`)
	exec(`INSERT INTO gross_board_entries(driver_id,service_date,load_number,load_record_id,original_rate,driver_rate,miles)
 VALUES($1,'2026-11-13','STOP-GEOCODE',88216,100,100,20)`, d.ID)
	view := func() BoardLoads {
		t.Helper()
		loads, err := repo.Loads(ctx, d.ID, "")
		if err != nil {
			t.Fatal(err)
		}
		return loads
	}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if !strings.HasSuffix(r.URL.Path, "/41.9930662,-83.3198394.json") {
			t.Errorf("unexpected stop coordinates %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"addresses":[{"address":{"municipality":"Berlin","localName":"Newport","countrySubdivisionCode":"MI","countrySubdivisionName":"Michigan","postalCode":"48166"}}]}`))
	}))
	defer server.Close()
	routes := boardroutes.NewClient(boardroutes.Options{Enabled: true, TomTomAPIKey: "fixture", RequestsPerMinute: 120, ReverseGeocodeMonthlyLimit: 1000, TomTomBillingTimezone: "UTC"})
	routes.Budget, routes.TomTomReverseURL = boardroutes.NewPostgresBudget(pool), server.URL

	initial := view()
	if len(initial.Next) != 1 || initial.Next[0].Stops[1].Location != "Michigan, 48166" || calls.Load() != 0 {
		t.Fatalf("unexpected initial view %+v, calls %d", initial, calls.Load())
	}
	if err := repo.ReconcileStopCities(ctx, routes); err != nil {
		t.Fatal(err)
	}
	queued := view()
	stop := queued.Next[0].Stops[1]
	if calls.Load() != 1 || queued.Revision != initial.Revision || stop.Location != "Newport, MI" ||
		stop.Key != initial.Next[0].Stops[1].Key || stop.StreetAddress != "8857 SWAN CREEK RD" || queued.Next[0].Stops[0].Location != "Chester, Pennsylvania" {
		t.Fatalf("unexpected queued stop %+v, calls %d", stop, calls.Load())
	}
	selected, err := repo.ChangeLoads(ctx, BoardLoadAction{DriverID: d.ID, Action: "select", PlanID: queued.Next[0].PlanID, FromDate: queued.FromDate, Revision: queued.Revision}, "board-test")
	if err != nil {
		t.Fatal(err)
	}
	current, err := repo.ChangeLoads(ctx, BoardLoadAction{DriverID: d.ID, Action: "advance", StopKey: stop.Key, FromDate: selected.Loads.FromDate, Revision: selected.Loads.Revision, Version: selected.Entry.Version}, "board-test")
	if err != nil {
		t.Fatal(err)
	}
	if current.Loads.StopKey != stop.Key || current.Loads.SourceDestination != "Newport, MI" || !current.Loads.DestinationSource {
		t.Fatalf("current destination %+v", current.Loads)
	}
	var saved string
	if err := pool.QueryRow(ctx, `SELECT payload->'current'->'stops'->1->>'location' FROM driver_board_load_state WHERE driver_id=$1`, d.ID).Scan(&saved); err != nil || saved != "Michigan, 48166" {
		t.Fatalf("saved source location %q, err %v", saved, err)
	}
	if err := repo.ReconcileStopCities(ctx, routes); err != nil || calls.Load() != 1 {
		t.Fatalf("cached stop retried: calls %d, err %v", calls.Load(), err)
	}
	if loads := view(); loads.StopKey != stop.Key || loads.SourceDestination != "Newport, MI" {
		t.Fatalf("reload lost destination: %+v", loads)
	}
	exec(`UPDATE board_stop_geocodes SET address=jsonb_set(address,'{postalCode}','"48167"')
 WHERE coordinate='41.9930662,-83.3198394'`)
	if loads := view(); loads.StopKey != stop.Key || loads.SourceDestination != "Michigan, 48166" {
		t.Fatalf("conflicting ZIP replaced the stop label: %+v", loads)
	}
}
