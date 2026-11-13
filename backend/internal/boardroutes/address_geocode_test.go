package boardroutes

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

type geocodeFixtureBudget struct {
	budgetFunc
	reserve func() error
}

func (b geocodeFixtureBudget) ReserveAddressGeocode(context.Context, Options) error {
	return b.reserve()
}

func TestGeocodeAddress(t *testing.T) {
	const match = `{"type":"Point Address","matchConfidence":{"score":1},"position":{"lat":40.1,"lon":-74.2}}`
	for _, tc := range []struct {
		name, results string
		fail          bool
	}{
		{"exact match", match, false},
		{"no match", "", true},
		{"street only", `{"type":"Street","matchConfidence":{"score":1}}`, true},
		{"partial match", `{"type":"Point Address","matchConfidence":{"score":0.88}}`, true},
		{"ambiguous", match + `,{"type":"Point Address","matchConfidence":{"score":1},"position":{"lat":40.9,"lon":-74.2}}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const address = "123 US-1 / 9, Newark, NJ 07102"
			reserved, calls := 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if reserved != calls || r.Method != http.MethodGet || !r.Close || r.URL.Path != "/"+address+".json" || r.URL.Query().Get("limit") != "2" || r.URL.Query().Get("key") != "test" {
					t.Error("unexpected or unmetered geocoding request")
				}
				_, _ = w.Write([]byte(`{"results":[` + tc.results + `]}`))
			}))
			defer server.Close()
			client := NewClient(Options{TomTomAPIKey: "test"})
			client.TomTomGeocodeURL = server.URL
			client.Budget = geocodeFixtureBudget{reserve: func() error { reserved++; return nil }}
			got, err := client.GeocodeAddress(context.Background(), address)
			if calls != 1 {
				t.Fatal("expected one geocoding request", calls)
			}
			if tc.fail {
				if !errors.Is(err, ErrAddressUnmatched) {
					t.Fatal(got, err)
				}
			} else if err != nil || got != (Waypoint{40.1, -74.2}) {
				t.Fatal(got, err)
			}
		})
	}
}
