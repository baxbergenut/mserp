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
			wantCalls := 2
			if tc.name == "exact match" || tc.name == "ambiguous" {
				wantCalls = 1
			}
			if calls != wantCalls {
				t.Fatal("unexpected geocoding requests", calls, wantCalls)
			}
			if tc.fail {
				if !errors.Is(err, ErrAddressUnmatched) {
					t.Fatal(got, err)
				}
			} else if err != nil || got.Waypoint != (Waypoint{40.1, -74.2}) || got.City {
				t.Fatal(got, err)
			}
		})
	}
}

func TestGeocodeCityDestinations(t *testing.T) {
	const city = `{"type":"Geography","entityType":"Municipality","matchConfidence":{"score":1},"position":{"lat":39.8,"lon":-75.3}}`
	const township = `{"type":"Geography","entityType":"Municipality","matchConfidence":{"score":0.716},"address":{"municipality":"Logan","countrySubdivision":"NJ","countrySubdivisionName":"New Jersey"},"position":{"lat":39.8,"lon":-75.3}}`
	const street = `{"type":"Street","matchConfidence":{"score":0.959},"position":{"lat":40.3,"lon":-74.4}}`
	for _, tc := range []struct {
		name, query, initial, filtered string
		fail, exact                    bool
	}{
		{name: "city and state", query: "Perryville, Maryland", initial: city},
		{name: "township hidden by streets", query: "Logan Township, NJ", initial: street, filtered: township},
		{name: "township full state name", query: "Logan Township, New Jersey", initial: street, filtered: township},
		{name: "wrong state", query: "Logan Township, OH", initial: street, filtered: township, fail: true},
		{name: "wrong city", query: "Other Township, NJ", initial: street, filtered: township, fail: true},
		{name: "ambiguous city", query: "Springfield", initial: city + `,{"type":"Geography","entityType":"Municipality","matchConfidence":{"score":1},"position":{"lat":42,"lon":-72}}`, fail: true},
		{name: "state alone", query: "New Jersey", initial: `{"type":"Geography","entityType":"CountrySubdivision","matchConfidence":{"score":1},"position":{"lat":40,"lon":-74}}`, fail: true},
		{name: "street address preferred", query: "123 Main St, Logan, NJ", initial: city + `,{"type":"Point Address","matchConfidence":{"score":1},"position":{"lat":39.8,"lon":-75.3}}`, exact: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls, reservations := 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if calls != reservations {
					t.Error("unmetered geocoding request")
				}
				results := tc.initial
				if calls == 2 {
					if r.URL.Query().Get("entityTypeSet") != "Municipality,MunicipalitySubdivision" {
						t.Error("fallback includes street matches")
					}
					results = tc.filtered
				}
				_, _ = w.Write([]byte(`{"results":[` + results + `]}`))
			}))
			defer server.Close()
			client := NewClient(Options{TomTomAPIKey: "test", RequestsPerMinute: 120})
			client.TomTomGeocodeURL = server.URL
			client.Budget = geocodeFixtureBudget{reserve: func() error { reservations++; return nil }}
			got, err := client.GeocodeAddress(context.Background(), tc.query)
			if (err != nil) != tc.fail {
				t.Fatal(got, err)
			}
			if !tc.fail && (got.Waypoint != (Waypoint{39.8, -75.3}) || got.City == tc.exact) {
				t.Fatal(got)
			}
			wantCalls := 1
			if tc.filtered != "" || tc.name == "state alone" {
				wantCalls = 2
			}
			if calls != wantCalls {
				t.Fatal("unexpected calls", calls, wantCalls)
			}
		})
	}
}

func TestCityFallbackBudget(t *testing.T) {
	calls, reservations := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; _, _ = w.Write([]byte(`{"results":[]}`)) }))
	defer server.Close()
	client := NewClient(Options{TomTomAPIKey: "test", RequestsPerMinute: 120})
	client.TomTomGeocodeURL = server.URL
	client.Budget = geocodeFixtureBudget{reserve: func() error {
		reservations++
		if reservations == 2 {
			return ErrAddressGeocodeBudgetExhausted
		}
		return nil
	}}
	if _, err := client.GeocodeAddress(context.Background(), "Logan Township, NJ"); !errors.Is(err, ErrAddressGeocodeBudgetExhausted) || calls != 1 || reservations != 2 {
		t.Fatal(err, calls, reservations)
	}
}
