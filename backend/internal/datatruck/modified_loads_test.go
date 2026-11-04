package datatruck

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

func TestModifiedLoadsKeysetAndMetadata(t *testing.T) {
	since := time.Date(2026, 11, 4, 1, 0, 0, 0, time.UTC)
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		var filters []map[string]string
		if err := json.Unmarshal([]byte(r.URL.Query().Get("filter")), &filters); err != nil {
			t.Error(err)
		}
		wantID := "1"
		if requests == 2 {
			wantID = "26"
		}
		if len(filters) != 2 || filters[0]["column"] != "modified_datetime" || filters[0]["contains"] != "after" || filters[0]["value"] != since.Format(time.RFC3339Nano) || filters[1]["contains"] != "greater_than" || filters[1]["value"] != wantID || r.URL.Query().Get("ordering") != "id" || r.URL.Query().Get("page_size") != "25" || r.URL.Query().Get("page") != "" {
			t.Errorf("unexpected query: %s", r.URL.RawQuery)
		}
		if requests == 2 {
			fmt.Fprint(w, `{"results":[],"count":0}`)
			return
		}
		rows := []map[string]any{}
		for id := 1; id <= 25; id++ {
			rows = append(rows, map[string]any{"id": id, "modified_datetime": since.Format(time.RFC3339), "load_id": "example", "total_pay": "2800", "trips": []map[string]any{{"id": 42, "total_load_pay": "2300", "mile": "400", "empty_mile": "20"}}})
		}
		// Must ignore offset/foreign Next links; subsequent calls construct the keyset URL.
		json.NewEncoder(w).Encode(map[string]any{"results": rows, "next": "https://untrusted.invalid/page=2", "count": 25})
	}))
	defer server.Close()
	client := &Client{httpClient: server.Client(), baseURL: server.URL, apiKey: "test"}
	rows, err := client.FetchModifiedLoadsPage(context.Background(), since, 0)
	if err != nil || len(rows) != 25 {
		t.Fatalf("%d %v", len(rows), err)
	}
	if rows[0].ModifiedDatetime == nil || rows[0].Trips[0].TotalLoadPay.String() != "2300" || rows[0].TotalPay.String() != "2800" {
		t.Fatal(rows[0])
	}
	payload, _ := json.Marshal(rows[0])
	if strings.Contains(string(payload), "modified_datetime") {
		t.Fatal("transport timestamp changes canonical payload")
	}
	rows, err = client.FetchModifiedLoadsPage(context.Background(), since, 25)
	if err != nil || len(rows) != 0 || requests != 2 {
		t.Fatalf("%d %v requests %d", len(rows), err, requests)
	}
}

func TestModifiedLoadsRejectInvalidFeed(t *testing.T) {
	for _, body := range []string{
		`{"results":[{"id":5}]}`,
		`{"results":[{"id":5,"modified_datetime":"2020-01-01T00:00:00Z"}]}`,
		`{"results":[{"id":4,"modified_datetime":"2026-11-04T01:00:00Z"}]}`,
		`{"results":[{"id":6,"modified_datetime":"2026-11-04T01:00:00Z"},{"id":5,"modified_datetime":"2026-11-04T01:00:00Z"}]}`,
		`{"results":[{"id":5,"modified_datetime":"2026-11-04T01:00:00Z"},{"id":5,"modified_datetime":"2026-11-04T01:00:00Z"}]}`,
		`{"results":[],"next":"?page=2"}`,
		`{"results":[],"count":1}`,
	} {
		t.Run(body, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) }))
			defer server.Close()
			client := &Client{httpClient: server.Client(), baseURL: server.URL}
			if _, err := client.FetchModifiedLoadsPage(context.Background(), time.Date(2026, 11, 4, 0, 0, 0, 0, time.UTC), 4); err == nil {
				t.Fatal("accepted invalid filter/page")
			}
		})
	}
}
