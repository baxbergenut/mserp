package relay

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Explicit opt-in: this creates and removes one synthetic driver in Relay staging.
// The host is fixed so a production URL cannot accidentally be supplied.
func TestLiveStagingDriverPolicyLifecycle(t *testing.T) {
	key := os.Getenv("MSERP_RELAY_LIVE_STAGING_API_KEY")
	if key == "" {
		t.Skip("requires explicit Relay staging mutation-test credentials")
	}
	c := NewClient("https://staging.relaypayments.com/api", key)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	drivers, err := c.Drivers(ctx)
	if err != nil {
		t.Fatal("list drivers:", err)
	}
	policies, err := c.Policies(ctx)
	if err != nil {
		t.Fatal("list policies:", err)
	}
	var policy DriverPolicy
	for _, p := range policies {
		if p.Name == "Company" && !p.ELDEnabled {
			policy = p
			break
		}
	}
	if policy.ID == "" {
		t.Fatal("designated staging Company policy not found")
	}
	phone := ""
	for n := 199; n >= 100; n-- {
		candidate := fmt.Sprintf("+1202555%04d", n)
		used := false
		for _, d := range drivers {
			if strings.TrimPrefix(d.Phone, "+") == strings.TrimPrefix(candidate, "+") {
				used = true
			}
		}
		if !used {
			phone = candidate
			break
		}
	}
	if phone == "" {
		t.Fatal("no unused reserved fictional number")
	}
	in := DriverInput{FirstName: "MSERP API TEST", LastName: fmt.Sprintf("%d", time.Now().UnixNano()), Phone: phone, DataFields: []DriverDataField{}}
	d, err := c.CreateDriver(ctx, in)
	if err != nil {
		// A timeout can occur after creation. Discover only our exact unique fixture.
		checkCtx, stop := context.WithTimeout(context.Background(), time.Minute)
		defer stop()
		if roster, readErr := c.Drivers(checkCtx); readErr == nil {
			for _, candidate := range roster {
				if candidate.FirstName == in.FirstName && candidate.LastName == in.LastName && candidate.Phone == in.Phone {
					if cleanupErr := c.RemoveDriver(checkCtx, candidate.ID); cleanupErr != nil {
						t.Errorf("uncertain-create cleanup %s: %v", candidate.ID, cleanupErr)
					}
				}
			}
		}
		t.Fatal("create driver:", err)
	}
	t.Log("created isolated staging driver", d.ID)
	t.Cleanup(func() {
		cleanupCtx, stop := context.WithTimeout(context.Background(), time.Minute)
		defer stop()
		if err := c.RemoveDriver(cleanupCtx, d.ID); err != nil {
			t.Errorf("remove fixture %s: %v", d.ID, err)
			return
		}
		roster, err := c.Drivers(cleanupCtx)
		if err != nil {
			t.Errorf("fixture removal roster read failed: %v", err)
			return
		}
		for _, candidate := range roster {
			if candidate.ID == d.ID {
				t.Errorf("removed fixture %s is still in roster", d.ID)
				return
			}
		}
		t.Log("removed staging fixture; full roster confirms absence")
	})
	in.FirstName = "MSERP API TEST EDITED"
	if err := c.UpdateDriver(ctx, d.ID, in); err != nil {
		t.Fatal("edit driver:", err)
	}
	if got, err := c.Driver(ctx, d.ID); err != nil || got.FirstName != in.FirstName {
		t.Fatalf("edit readback failed: %v", err)
	}
	t.Log("edit verified")
	if a, err := c.Assignment(ctx, d.ID); err != nil || a != nil {
		t.Fatalf("new driver should have no assignment: %v", err)
	}
	if err := c.AssignPolicy(ctx, d.ID, policy.ID); err != nil {
		t.Fatal("assign policy:", err)
	}
	a, err := c.Assignment(ctx, d.ID)
	if err != nil || a == nil || a.PolicyID != policy.ID || !a.Enabled {
		t.Fatalf("assignment readback failed: %v", err)
	}
	t.Log("assignment verified", a.ID)
	for _, enabled := range []bool{false, true} {
		if err := c.EnablePolicy(ctx, d.ID, enabled); err != nil {
			t.Fatalf("set enabled=%t: %v", enabled, err)
		}
		got, err := c.Assignment(ctx, d.ID)
		if err != nil || got == nil || got.ID != a.ID || got.Enabled != enabled || !reflect.DeepEqual(got.Limits, a.Limits) {
			t.Fatalf("enabled=%t readback/limits failed: %v", enabled, err)
		}
		t.Logf("enabled=%t verified; same assignment and limits", enabled)
	}
	if err := c.UnassignPolicy(ctx, d.ID); err != nil {
		t.Fatal("unassign policy:", err)
	}
	if got, err := c.Assignment(ctx, d.ID); err != nil || got != nil {
		t.Fatalf("unassignment readback failed: %v", err)
	}
	t.Log("unassignment verified")
}
