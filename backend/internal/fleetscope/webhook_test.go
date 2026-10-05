package fleetscope

import (
	"strings"
	"testing"
	"time"
)

func TestSignatureAndReplayWindow(t *testing.T) {
	now := time.Unix(1800000000, 0)
	body := []byte(`{"version":1}`)
	secret := strings.Repeat("s", 32)
	signature := Sign(secret, "1800000000", body)
	if !Verify(secret, "1800000000", signature, body, now) {
		t.Fatal("valid signature rejected")
	}
	for _, tc := range []struct {
		name, timestamp, signature string
		body                       []byte
		now                        time.Time
	}{
		{"tampered", "1800000000", signature, []byte(`{"version":2}`), now},
		{"stale", "1800000000", signature, body, now.Add(301 * time.Second)},
		{"future", "1800000000", signature, body, now.Add(-301 * time.Second)},
		{"changed timestamp", "1800000001", signature, body, now},
		{"invalid hex", "1800000000", "v1=zz", body, now},
		{"missing version", "1800000000", signature[3:], body, now},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if Verify(secret, tc.timestamp, tc.signature, tc.body, tc.now) {
				t.Fatal("invalid request accepted")
			}
		})
	}
}

func TestOptionsFailClosed(t *testing.T) {
	for _, options := range []Options{{CompanyID: "bad", Secret: strings.Repeat("s", 32)}, {CompanyID: "00000000-0000-0000-0000-000000000001"}, {Secret: strings.Repeat("s", 32)}} {
		if options.Validate() == nil {
			t.Fatal("partial configuration accepted")
		}
	}
	if (Options{}).Validate() != nil {
		t.Fatal("disabled integration must be allowed")
	}
}

func TestEventValidation(t *testing.T) {
	valid := Event{Version: 1, Type: "driver.hired", EventID: "00000000-0000-0000-0000-000000000001", CompanyID: "00000000-0000-0000-0000-000000000002", OccurredAt: time.Now(), Driver: Driver{ID: "00000000-0000-0000-0000-000000000003", FullName: "Test Driver", DriverType: "company", HireDate: "2026-09-28"}}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*Event){
		func(e *Event) { e.Version = 2 }, func(e *Event) { e.Type = "driver.updated" },
		func(e *Event) { e.Driver.HireDate = "2026-02-30" }, func(e *Event) { e.Driver.FullName = " " },
		func(e *Event) { e.Driver.Email = "not-email" }, func(e *Event) { e.Driver.LicenseExpires = "tomorrow" },
		func(e *Event) { e.Driver.DriverType = "fleet_owner" }, func(e *Event) { e.Driver.ID = "invalid" },
		func(e *Event) { e.Driver.Phone = strings.Repeat("x", 51) }, func(e *Event) { e.OccurredAt = time.Time{} },
	} {
		event := valid
		mutate(&event)
		if event.Validate() == nil {
			t.Fatal("invalid event accepted")
		}
	}
}

func TestTerminationValidation(t *testing.T) {
	valid := Event{Version: 1, Type: "driver.terminated", EventID: "00000000-0000-0000-0000-000000000001", CompanyID: "00000000-0000-0000-0000-000000000002", OccurredAt: time.Now(), TerminationDate: "2026-01-01", Driver: Driver{ID: "00000000-0000-0000-0000-000000000003", FullName: "Test Driver"}}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*Event){
		func(e *Event) { e.TerminationDate = "" },
		func(e *Event) { e.TerminationDate = "2026-02-30" },
		func(e *Event) { e.TerminationDate = "2999-01-01" },
		func(e *Event) { e.Driver.FullName = " " },
		func(e *Event) { e.Driver.ID = "invalid" },
		func(e *Event) { e.Driver.Email = "extra@example.test" },
		func(e *Event) { e.Driver.HireDate = "2026-01-01" },
	} {
		event := valid
		mutate(&event)
		if event.Validate() == nil {
			t.Fatal("invalid termination accepted")
		}
	}
}
