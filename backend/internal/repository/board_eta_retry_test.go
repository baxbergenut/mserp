package repository

import (
	"mserp/internal/boardroutes"
	"testing"
	"time"
)

func TestBoardTravelManualRetry(t *testing.T) {
	now := time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)
	location := &DriverBoardLocation{Source: "five_eld", Latitude: 40, Longitude: -74, ReportedAt: now}
	travel := BoardTravel{Enabled: true, Context: "selected", Mode: boardroutes.TravelMode, Source: "five_eld", CalculatedAt: now, AttemptedAt: now, Position: &boardRoutePosition{Latitude: 40, Longitude: -74}, RetryRequestedAt: now}
	if !travel.due("selected", location, now) {
		t.Fatal("explicit retry must bypass fresh timer and stationary origin")
	}
	travel.Error = "provider unavailable"
	if travel.due("selected", location, now.Add(time.Minute)) {
		t.Fatal("retry bypassed failed-provider backoff")
	}
	if !travel.due("selected", location, now.Add(5*time.Minute)) {
		t.Fatal("retry never resumes after backoff")
	}
	travel.RetryCompletedAt = travel.RetryRequestedAt
	if travel.retryPending() || travel.due("selected", location, now.Add(5*time.Minute)) {
		t.Fatal("completed retry replayed at stationary origin")
	}
}

func TestBoardTravelPreserveInFlightRetry(t *testing.T) {
	now := time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)
	old := BoardTravel{Context: "selected", Miles: 12, RetryRequestedAt: now.Add(-time.Minute), RetryCompletedAt: now.Add(-time.Minute)}
	current := BoardTravel{Context: "selected", RetryRequestedAt: now}
	old.preserveNewerRetry(current, "selected")
	if !old.retryPending() || !old.RetryRequestedAt.Equal(now) || old.Miles != 12 {
		t.Fatal("in-flight result lost newer retry or discarded good route")
	}
	old.RetryCompletedAt = now
	old.preserveNewerRetry(current, "selected")
	if old.retryPending() {
		t.Fatal("same retry replayed after completion")
	}
	current.Context = "obsolete"
	current.RetryRequestedAt = now.Add(time.Minute)
	old.preserveNewerRetry(current, "selected")
	if old.retryPending() {
		t.Fatal("obsolete-context request leaked into current route")
	}
}

func TestBoardTravelRetryContextReset(t *testing.T) {
	now := time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)
	old := BoardTravel{Context: "old", RetryRequestedAt: now}
	next := old.newRouteContext("new", "five_eld")
	if next.retryPending() || !next.RetryRequestedAt.IsZero() || !next.RetryCompletedAt.IsZero() {
		t.Fatal("retry crossed selection boundary")
	}
	same := old.newRouteContext("old", "five_eld")
	if !same.retryPending() {
		t.Fatal("mode/source renewal lost current retry")
	}
}
