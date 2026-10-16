package weighmytruck

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const ticketCSV = "Group Id,Date,Approx Time,Ticket Number,Original Ticket Number,Fee,Tractor Number,Trailer Number,Site Number,T/S Name,City,St,Driver Code,\nfixture,10-08-26,13:16,123456,,15.25,074,TR1,3199,LOVES,LAKE VILLAGE,AR,007,\nfixture,10-08-26,14:00,123457,123456,5.00,074,TR1,3199,LOVES,LAKE VILLAGE,AR,007,\n"

func TestSettlementCSV(t *testing.T) {
	rows, e := ParseTickets("\ufeff" + ticketCSV)
	if e != nil || len(rows) != 2 {
		t.Fatalf("parse %v %v", rows, e)
	}
	if rows[0].Date != "2026-10-08" || rows[0].Fee != "15.25" || rows[0].Tractor != "074" || rows[0].DriverCode != "007" || rows[1].OriginalNumber != "123456" {
		t.Fatalf("mapping %+v", rows)
	}
	for _, invalid := range []string{strings.Replace(ticketCSV, "15.25", "15.255", 1), strings.Replace(ticketCSV, "10-08-26", "not a date", 1), strings.Replace(ticketCSV, "Fee,", "Cost,", 1), strings.Replace(ticketCSV, "123457", "123456", 1)} {
		if _, e = ParseTickets(invalid); e == nil {
			t.Fatal("invalid CSV accepted")
		}
	}
	credit, e := ParseTickets(strings.Replace(ticketCSV, "15.25", "-15.25", 1))
	if e != nil || credit[0].Fee != "-15.25" {
		t.Fatal("credit lost")
	}
}

func TestSettlementCSVDatePadding(t *testing.T) {
	for _, date := range []string{"9-7-26", "09-07-26", "9-07-26", "09-7-26", "9/7/2026", "09/07/2026", "2026-09-07", "9-7-2026", "09-07-2026"} {
		t.Run(date, func(t *testing.T) {
			rows, err := ParseTickets(strings.ReplaceAll(ticketCSV, "10-08-26", date))
			if err != nil || len(rows) != 2 {
				t.Fatalf("parse %q: rows=%d err=%v", date, len(rows), err)
			}
			for _, row := range rows {
				if row.Date != "2026-09-07" {
					t.Fatalf("date = %q, want 2026-09-07", row.Date)
				}
			}
		})
	}
	for _, date := range []string{"9-31-26", "13-7-26", "9-0-26"} {
		if _, err := ParseTickets(strings.ReplaceAll(ticketCSV, "10-08-26", date)); err == nil {
			t.Errorf("invalid calendar date %q accepted", date)
		}
	}
}

func TestFetchSettlements(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			w.Write([]byte(`{"access_token":"fixture","expires_in":3600}`))
			return
		}
		if r.URL.Path != "/Settlement/GetSettlement" || r.Header.Get("Authorization") != "Bearer fixture" {
			t.Error("wrong settlement authentication/path")
		}
		var in struct {
			Types             []int  `json:"types"`
			Date              string `json:"date"`
			IncludeDriverCode bool   `json:"includeDriverCode"`
		}
		if json.NewDecoder(r.Body).Decode(&in) != nil || len(in.Types) != 1 || in.Types[0] != 3 || in.Date != "2026-10-08" || !in.IncludeDriverCode {
			t.Error("wrong request")
		}
		json.NewEncoder(w).Encode(map[string]any{"csv": []byte(ticketCSV), "pdfs": nil, "tifs": nil})
	}))
	defer server.Close()
	c := NewClient(Options{ClientID: "fixture", ClientSecret: "fixture", TokenURL: server.URL + "/token", APIURL: server.URL})
	rows, e := c.FetchSettlements(context.Background(), "2026-10-08")
	if e != nil || len(rows) != 2 {
		t.Fatalf("fetch %v %v", rows, e)
	}
}
