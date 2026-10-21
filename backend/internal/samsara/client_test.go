package samsara

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSnapshotDuplicateVINUsesOnlyUnambiguousAttachedGateway(t *testing.T) {
	for _, tc := range []struct {
		name, roster, stats, wantID string
	}{
		{"unpaired history", `[{"id":"old","vin":"1M8GDM9AXKP042788","name":"003"},{"id":"current","vin":"1M8GDM9AXKP042788","gateway":{"serial":"GW-003"}}]`, `[{"id":"old"},{"id":"current"}]`, "current"},
		{"multiple attached gateways", `[{"id":"old","vin":"1M8GDM9AXKP042788","name":"Inactive","gateway":{"serial":"GW-OLD"}},{"id":"current","vin":"1M8GDM9AXKP042788","gateway":{"serial":"GW-003"}}]`, `[{"id":"old"},{"id":"current"}]`, ""},
		{"no attached gateway", `[{"id":"old","vin":"1M8GDM9AXKP042788"},{"id":"current","vin":"1M8GDM9AXKP042788","gateway":{"serial":" "}}]`, `[{"id":"current"}]`, ""},
		{"duplicate stats stay ambiguous", `[{"id":"old","vin":"1M8GDM9AXKP042788"},{"id":"current","vin":"1M8GDM9AXKP042788","gateway":{"serial":"GW-003"}}]`, `[{"id":"current"},{"id":"current"}]`, ""},
		{"duplicate provider IDs stay ambiguous", `[{"id":"current","vin":"1M8GDM9AXKP042788","gateway":{"serial":"GW-003"}},{"id":"current","vin":"1M8GDM9AXKP042789"}]`, `[{"id":"current"}]`, ""},
		{"gateway attached to conflicting VINs", `[{"id":"one","vin":"1M8GDM9AXKP042788","gateway":{"serial":"GW-003"}},{"id":"two","vin":"1M8GDM9AXKP042789","gateway":{"serial":"GW-003"}}]`, `[{"id":"one"},{"id":"two"}]`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				data := tc.roster
				if r.URL.Path == "/fleet/vehicles/stats" {
					data = tc.stats
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"data": json.RawMessage(data)})
			}))
			defer server.Close()
			s, err := NewClient(server.URL, "fixture", "").Snapshot(context.Background(), time.Now())
			if err != nil {
				t.Fatal(err)
			}
			if tc.wantID == "" {
				if len(s.Ambiguous) == 0 {
					t.Fatal("ambiguous identity was selected", s)
				}
				for _, v := range s.Vehicles {
					if v.VIN == "1M8GDM9AXKP042788" {
						t.Fatal("ambiguous VIN included", v)
					}
				}
			} else if len(s.Vehicles) != 1 || s.Vehicles[0].ID != tc.wantID || len(s.Ambiguous) != 0 {
				t.Fatal("attached gateway was not selected", s)
			}
		})
	}
}

func TestSnapshotIdentityPaginationAndUnits(t *testing.T) {
	now := time.Date(2026, 10, 21, 16, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.Header.Get("Authorization") != "Bearer fixture-token" || r.Header.Get("Samsara-Version") != "fixture-version" {
			t.Error("request headers or method incorrect")
		}
		switch r.URL.Path {
		case "/fleet/vehicles":
			if r.URL.Query().Get("after") == "" {
				fmt.Fprint(w, `{"data":[{"id":"one","vin":"1M8GDM9AXKP042788","name":"001"}],"pagination":{"hasNextPage":true,"endCursor":"next"}}`)
			} else {
				fmt.Fprint(w, `{"data":[{"id":"two","vin":"1M8GDM9AXKP042789"},{"id":"dup-a","vin":"1M8GDM9AXKP042790"},{"id":"dup-b","vin":"1M8GDM9AXKP042790"},{"id":"bad","vin":"unknown"}],"pagination":{"hasNextPage":false}}`)
			}
		case "/fleet/vehicles/stats":
			if r.URL.Query().Get("types") != "gps,obdOdometerMeters,gpsOdometerMeters" {
				t.Error("incorrect stats")
			}
			fmt.Fprint(w, `{"data":[{"id":"one","gps":{"time":"2026-10-21T15:59:00Z","latitude":40,"longitude":-75,"headingDegrees":90,"speedMilesPerHour":55},"obdOdometerMeters":{"time":"2026-10-21T15:59:00Z","value":1609344},"gpsOdometerMeters":{"time":"2026-10-21T15:59:00Z","value":3218688}},{"id":"two","gps":{"time":"2026-10-22T00:00:00Z","latitude":40,"longitude":-75},"gpsOdometerMeters":{"time":"2026-10-21T15:59:00Z","value":0}}],"pagination":{"hasNextPage":false}}`)
		default:
			t.Error("unexpected path")
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	snapshot, err := NewClient(server.URL, "fixture-token", "fixture-version").Snapshot(context.Background(), now)
	if err != nil || len(snapshot.Vehicles) != 2 || len(snapshot.Ambiguous) != 1 || snapshot.Invalid != 1 {
		t.Fatal(snapshot, err)
	}
	v := snapshot.Vehicles[0]
	if v.VIN != "1M8GDM9AXKP042788" || v.Odometer == nil || v.Odometer.Miles != 1000 || v.GPS == nil || v.GPS.SpeedMPH == nil || *v.GPS.SpeedMPH != 55 {
		t.Fatal("OBD priority, units or GPS incorrect", v)
	}
	v = snapshot.Vehicles[1]
	if v.GPS != nil || v.Odometer == nil || v.Odometer.Miles != 0 {
		t.Fatal("future GPS or zero odometer handled incorrectly", v)
	}
}

func TestSnapshotFailureAndRedirectDoNotExposeCredentials(t *testing.T) {
	for _, status := range []int{401, 302} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Location", "https://example.invalid/leak")
				w.WriteHeader(status)
				fmt.Fprint(w, "fixture-secret-token")
			}))
			defer server.Close()
			_, err := NewClient(server.URL, "fixture-secret-token", "").Snapshot(context.Background(), time.Now())
			if err == nil || strings.Contains(err.Error(), "fixture-secret-token") || strings.Contains(err.Error(), "example.invalid") {
				t.Fatal("unsafe provider error", err)
			}
		})
	}
}
func TestSnapshotRejectsRepeatedCursor(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":[],"pagination":{"hasNextPage":true,"endCursor":"same"}}`)
	}))
	defer server.Close()
	if _, err := NewClient(server.URL, "fixture", "").Snapshot(context.Background(), time.Now()); err == nil {
		t.Fatal("repeated cursor accepted")
	}
}
func TestOptions(t *testing.T) {
	t.Setenv("SAMSARA_API_TOKEN", "fixture")
	t.Setenv("SAMSARA_API_URL", "")
	t.Setenv("SAMSARA_SYNC_INTERVAL", "")
	t.Setenv("SAMSARA_API_VERSION", "")
	o, err := LoadOptions()
	if err != nil || o.URL != "https://api.samsara.com" || o.Interval != time.Minute {
		t.Fatal(o, err)
	}
	for _, url := range []string{"http://api.samsara.com", "https://user:secret@api.samsara.com", "https://api.samsara.com/path", "https://api.samsara.com?token=bad"} {
		t.Setenv("SAMSARA_API_URL", url)
		if _, err = LoadOptions(); err == nil {
			t.Fatal("unsafe API origin accepted")
		}
	}
	t.Setenv("SAMSARA_API_URL", "")
	t.Setenv("SAMSARA_SYNC_INTERVAL", "1s")
	if _, err = LoadOptions(); err == nil {
		t.Fatal("unsafe polling interval")
	}
}
