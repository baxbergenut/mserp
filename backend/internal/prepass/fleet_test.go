package prepass

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func validFleetInput() VehicleInput {
	return VehicleInput{"003", Plate{false, "ABC123", "OH"}, 80000, Registration{"12345", "OH"}, Registration{"67890", "OH"}, "791", false}
}
func TestFleetValidation(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*VehicleInput)
		valid  bool
	}{
		{"valid", func(*VehicleInput) {}, true},
		{"sheet alias", func(v *VehicleInput) { v.VehicleNumber = "777T (022)" }, false},
		{"temporary", func(v *VehicleInput) { v.LicensePlate = Plate{true, "", "OH"} }, true},
		{"invalid state", func(v *VehicleInput) { v.IFTA.State = "ZZ" }, false},
		{"empty IRP", func(v *VehicleInput) { v.IRP.AccountNumber = "" }, false},
		{"negative weight", func(v *VehicleInput) { v.DeclaredWeight = -1 }, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			v := validFleetInput()
			test.mutate(&v)
			if (v.Validate() == nil) != test.valid {
				t.Fatal("validation mismatch")
			}
		})
	}
}
func TestFleetWireContract(t *testing.T) {
	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/auth/v1/token":
			if r.Header.Get("client_id") != "test-id" || r.Header.Get("client_secret") != "test-secret" {
				t.Error("credentials missing")
			}
			fmt.Fprint(w, `{"access_token":"test-token","expires_in":3600}`)
		case "/accounts/v1/accounts":
			fmt.Fprint(w, `{"accounts":[{"accountNumber":"123","accountStatus":"Active"}]}`)
		default:
			if r.Header.Get("Authorization") != "Bearer test-token" || r.URL.Query().Get("accountNumber") != "123" {
				t.Error("auth/account missing")
			}
			calls = append(calls, r.Method+" "+r.URL.Path)
			switch r.URL.Path {
			case "/fleetmanagement/v2/vehicles":
				fmt.Fprint(w, `[{"accountNumber":"123","vin":"1XKADP9X8DJ364740","vehicleNumber":"003"}]`)
			case "/fleetmanagement/v2/transponders":
				fmt.Fprint(w, `[{"accountNumber":"123","transponderNumber":"777111222"}]`)
			case "/fleetmanagement/v2/vehicle":
				if r.Method != http.MethodPost && r.URL.Query().Get("vin") != "1XKADP9X8DJ364740" {
					t.Error("VIN query missing")
				}
				if r.Method == http.MethodDelete {
					break
				}
				var body map[string]any
				if json.NewDecoder(r.Body).Decode(&body) != nil {
					t.Error("invalid JSON")
				}
				if r.Method == http.MethodPost && body["vin"] != "1XKADP9X8DJ364740" {
					t.Error("create VIN missing")
				}
				if body["transponderNumber"] != nil {
					if len(body) != 1 {
						t.Error("assign overwrites other fields")
					}
				} else if body["vehicleNumber"] != "003" || body["irp"] == nil {
					t.Error("bad vehicle body")
				}
			default:
				t.Error("unexpected path")
			}
		}
	}))
	defer server.Close()
	c := NewClient(server.URL, "test-id", "test-secret")
	s, e := c.Fleet(context.Background())
	if e != nil || len(s.Vehicles) != 1 || len(s.Transponders) != 1 {
		t.Fatalf("fleet: %+v %v", s, e)
	}
	for _, action := range []string{"add", "edit", "remove", "assign"} {
		if e = c.ChangeVehicle(context.Background(), action, "123", "1XKADP9X8DJ364740", validFleetInput(), "777111222"); e != nil {
			t.Fatal(e)
		}
	}
	if len(calls) != 6 {
		t.Fatal(calls)
	}
}
func TestFleetFailures(t *testing.T) {
	t.Run("empty roster response", func(t *testing.T) {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }))
		defer s.Close()
		var rows []Vehicle
		if err := NewClient(s.URL, "", "").fleetRequest(context.Background(), http.MethodGet, "/vehicles", "", nil, &rows); err == nil {
			t.Fatal("empty response accepted as a complete roster")
		}
	})
	for _, code := range []int{400, 403, 429, 500, 502} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(code); fmt.Fprint(w, "private payload") }))
			defer s.Close()
			c := NewClient(s.URL, "", "")
			e := c.fleetRequest(context.Background(), http.MethodPatch, "/vehicle", "secret", map[string]string{"transponderNumber": "777111222"}, nil)
			api, ok := e.(*FleetAPIError)
			if !ok || api.Uncertain != (code >= 429) || strings.Contains(e.Error(), "private") {
				t.Fatal(e)
			}
		})
	}
	for _, body := range []string{"null", "{broken", "{\"vehicles\":[]}"} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) }))
		var rows []Vehicle
		e := NewClient(s.URL, "", "").fleetRequest(context.Background(), http.MethodGet, "/vehicles", "", nil, &rows)
		s.Close()
		if e == nil {
			t.Fatalf("accepted %s", body)
		}
	}
}
