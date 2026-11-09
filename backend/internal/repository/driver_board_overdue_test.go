package repository

import (
	"testing"
	"time"
)

func TestBoardLoadOverduePickupEnd(t *testing.T) {
	id := 1
	now, _ := time.Parse(time.RFC3339, "2026-11-09T18:00:00Z")
	for _, tc := range []struct {
		name, begin, end string
		want             bool
	}{
		{"start passed end future", "2026-11-09T08:00:00-05:00", "2026-11-09T14:00:00-05:00", false},
		{"exact end", "2026-11-09T08:00:00-05:00", "2026-11-09T13:00:00-05:00", false},
		{"after end", "2026-11-09T08:00:00-05:00", "2026-11-09T12:59:59-05:00", true},
		{"overnight not ended", "2026-11-09T23:00:00-08:00", "2026-11-10T02:00:00-08:00", false},
		{"overnight ended", "2026-11-08T23:00:00-08:00", "2026-11-09T02:00:00-08:00", true},
		{"DST repeated hour", "2026-11-01T01:30:00-04:00", "2026-11-01T01:15:00-05:00", true},
		{"missing end", "2026-11-01T12:00:00Z", "", false},
		{"invalid end", "", "bad", false},
		{"local only", "", "2026-11-08T12:00:00", false},
		{"reversed", "2026-11-09T17:00:00Z", "2026-11-09T16:00:00Z", false},
		{"invalid begin", "bad", "2026-11-09T16:00:00Z", false},
		{"explicit end only", "", "2026-11-09T16:00:00Z", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := BoardLoad{LoadID: &id, Date: "2026-11-01", Stops: []BoardStop{{Type: "pickup", Schedule: &BoardSchedule{Kind: "window", BeginAt: tc.begin, EndAt: tc.end}}}}
			if got := boardLoadOverdue(p, "2026-11-09", now); got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
			p.Date = "2026-11-20"
			if got := boardLoadOverdue(p, "2026-11-09", now); got != tc.want {
				t.Fatal("planned date affected system deadline")
			}
		})
	}
	p := BoardLoad{LoadID: &id, PlanID: "plan", Date: "2026-11-01", Stops: []BoardStop{{Key: "pu", Type: "pickup", Schedule: &BoardSchedule{Kind: "window", EndAt: "2026-11-09T16:00:00Z"}}}}
	if boardLoadOverdue(boardStopTimes(p, map[string]string{"load:1/pu": "14:00 MON"}), "2026-11-09", now) {
		t.Fatal("free text override used stale source deadline")
	}
	p.Manual = true
	if boardLoadOverdue(p, "2026-11-09", now) {
		t.Fatal("manual-only entry marked overdue")
	}
	p.Manual = false
	p.Stops[0].Schedule = nil
	p.Stops = append(p.Stops, BoardStop{Type: "pickup", Schedule: &BoardSchedule{Kind: "window", EndAt: "2026-11-09T16:00:00Z"}})
	if boardLoadOverdue(p, "2026-11-09", now) {
		t.Fatal("later pickup substituted for missing first pickup deadline")
	}
	p.Stops = nil
	if boardLoadOverdue(p, "2026-11-09", now) {
		t.Fatal("system load without stops used planned date")
	}
	p.LoadID = nil
	p.Date = "2026-11-08"
	for _, tc := range []struct {
		at   string
		want bool
	}{{"2026-11-09T04:59:59Z", false}, {"2026-11-09T05:00:00Z", true}} {
		at, _ := time.Parse(time.RFC3339, tc.at)
		if got := boardLoadOverdue(p, boardLoadToday(at), at); got != tc.want {
			t.Fatal("unmatched New York date fallback changed", tc)
		}
	}
}

func TestBoardLoadOverdueSourceProjection(t *testing.T) {
	id := 42
	p := BoardLoad{LoadID: &id, Date: "2026-11-01", Stops: boardStops([]byte(`{"stops":[{"ordering":2,"stop_type":"delivery","schedule_type":"window","scheduled_end_at":"2026-11-08T10:00:00Z"},{"ordering":1,"stop_type":"pickup","schedule_type":"window","scheduled_begin_at":"2026-11-09T08:00:00-05:00","scheduled_end_at":"2026-11-09T14:00:00-05:00","scheduled_begin_date":"2026-11-09","scheduled_begin_time":"08:00:00","scheduled_end_date":"2026-11-09","scheduled_end_time":"14:00:00"}]}`))}
	end, _ := time.Parse(time.RFC3339, "2026-11-09T19:00:00Z")
	if boardLoadOverdue(p, "2026-11-09", end) {
		t.Fatal("pickup not yet past end")
	}
	if !boardLoadOverdue(p, "2026-11-09", end.Add(time.Second)) {
		t.Fatal("source end timestamp was lost")
	}
	p.Stops[0].Schedule.Kind = "appointment"
	p.Stops[0].Schedule.EndAt = ""
	if boardLoadOverdue(p, "2026-11-09", end) {
		t.Fatal("appointment start used as pickup end")
	}
}

func TestBoardLoadInTransitNotOverdue(t *testing.T) {
	id := 1
	now := time.Date(2026, 11, 9, 18, 0, 0, 0, time.UTC)
	for _, status := range []string{"in_transit", " In Transit ", "IN-TRANSIT"} {
		p := BoardLoad{LoadID: &id, SourceStatus: status, Date: "2026-01-01", Stops: []BoardStop{{Type: "pickup", Schedule: &BoardSchedule{Kind: "window", EndAt: "2026-11-08T16:00:00Z"}}}}
		if boardLoadOverdue(p, "2026-11-09", now) {
			t.Fatal("in-transit pickup marked overdue", status)
		}
		p.SourceStatus = "Dispatched"
		if !boardLoadOverdue(p, "2026-11-09", now) {
			t.Fatal("pending pickup lost overdue status")
		}
	}
}
