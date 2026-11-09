package boardroutes

import (
	"context"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type reverseBudget struct {
	calls int
	err   error
}

func (b *reverseBudget) Reserve(context.Context, Options) (string, error) {
	return "", errors.New("must not reserve routing")
}
func (b *reverseBudget) ReserveReverseGeocode(context.Context, Options) error {
	b.calls++
	return b.err
}

func reverseTestClient() *Client {
	c := NewClient(Options{Enabled: true, TomTomAPIKey: "private-test-key", RequestsPerMinute: 120, ReverseGeocodeMonthlyLimit: 1000, TomTomBillingTimezone: "UTC"})
	c.Budget = &reverseBudget{}
	return c
}

func TestReverseGeocodeResponses(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		want       string
	}{
		{"address", `{"addresses":[{"address":{"freeformAddress":"  123 Main St, Toledo, OH 43604  "}}]}`, 200, "123 Main St, Toledo, OH 43604"},
		{"empty", `{"addresses":[]}`, 200, ""},
		{"missing address", `{"addresses":[{}]}`, 200, ""},
		{"blank", `{"addresses":[{"address":{"freeformAddress":" "}}]}`, 200, ""},
		{"control", `{"addresses":[{"address":{"freeformAddress":"Street\nCity"}}]}`, 200, ""},
		{"invalid", "{", 200, ""},
		{"limit", `{"message":"private-test-key"}`, 429, ""},
		{"denied", `{"message":"private-test-key"}`, 403, ""},
		{"server", `{"message":"private-test-key"}`, 500, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != "GET" || r.URL.Path != "/40.0000000,-74.0000000.json" || r.URL.Query().Get("key") != "private-test-key" || r.URL.Query().Get("language") != "en-US" || !r.Close {
					t.Errorf("unexpected request %s", r.URL.Path)
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			c := reverseTestClient()
			c.TomTomReverseURL = server.URL
			got, err := c.ReverseGeocode(context.Background(), 40, -74)
			if got != tc.want || (err != nil) != (tc.want == "") {
				t.Fatal(got, err)
			}
			if calls != 1 || c.Budget.(*reverseBudget).calls != 1 {
				t.Fatal("request must be counted exactly once", calls)
			}
			if err != nil && strings.Contains(err.Error(), "private-test-key") {
				t.Fatal("secret in error")
			}
			if tc.status == 429 && time.Until(c.blockedUntil) < 4*time.Minute {
				t.Fatal("429 missing shared cooldown")
			}
		})
	}
}

func TestReverseGeocodeSafetyGates(t *testing.T) {
	for _, tc := range []struct {
		name     string
		prepare  func(*Client)
		lat, lng float64
	}{
		{"disabled", func(c *Client) { c.Options.Enabled = false }, 40, -74},
		{"key", func(c *Client) { c.Options.TomTomAPIKey = "" }, 40, -74},
		{"zero limit", func(c *Client) { c.Options.ReverseGeocodeMonthlyLimit = 0 }, 40, -74},
		{"missing accounting", func(c *Client) { c.Budget = nil }, 40, -74},
		{"nan", func(*Client) {}, math.NaN(), -74},
		{"infinite", func(*Client) {}, 40, math.Inf(1)},
		{"latitude", func(*Client) {}, 91, -74},
		{"longitude", func(*Client) {}, 40, -181},
		{"zero island", func(*Client) {}, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := reverseTestClient()
			b := c.Budget.(*reverseBudget)
			tc.prepare(c)
			c.TomTomReverseURL = "http://127.0.0.1:1"
			if _, err := c.ReverseGeocode(context.Background(), tc.lat, tc.lng); err == nil {
				t.Fatal("gate allowed request")
			}
			if b.calls != 0 {
				t.Fatal("invalid request consumed reservation")
			}
		})
	}
	c := reverseTestClient()
	b := c.Budget.(*reverseBudget)
	b.err = ErrReverseGeocodeBudgetExhausted
	if _, err := c.ReverseGeocode(context.Background(), 40, -74); !errors.Is(err, ErrReverseGeocodeBudgetExhausted) {
		t.Fatal(err)
	}
	if b.calls != 1 {
		t.Fatal("budget not consulted")
	}
}

func TestReverseGeocodeRedirectAndCancellation(t *testing.T) {
	targetCalls := 0
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { targetCalls++ }))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, http.StatusFound) }))
	defer server.Close()
	c := reverseTestClient()
	c.TomTomReverseURL = server.URL
	c.HTTP = server.Client()
	if _, err := c.ReverseGeocode(context.Background(), 40, -74); err == nil {
		t.Fatal("redirect accepted")
	}
	if targetCalls != 0 {
		t.Fatal("redirect followed")
	}
	c = reverseTestClient()
	c.blockedUntil = time.Now().Add(time.Minute)
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	if _, err := c.ReverseGeocode(ctx, 40, -74); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if c.Budget.(*reverseBudget).calls != 0 {
		t.Fatal("cancelled wait counted")
	}
}

func TestReverseGeocodeLimitConfiguration(t *testing.T) {
	for _, name := range []string{"BOARD_ROUTES_ENABLED", "BOARD_ROUTES_ACCESS_VERIFIED", "BOARD_ROUTES_OPERATIONS_VERIFIED", "FIVE_ELD_SPEED_VERIFIED"} {
		t.Setenv(name, "true")
	}
	t.Setenv("FIVE_ELD_SPEED_UNIT", "mph")
	t.Setenv("TOMTOM_ROUTING_API_KEY", "test")
	t.Setenv("BOARD_ROUTES_PAID_PROVIDER", "tomtom")
	for _, v := range []string{"-1", "1001", "bad"} {
		t.Setenv("BOARD_REVERSE_GEOCODE_MONTHLY_LIMIT", v)
		if _, err := LoadOptions(); err == nil {
			t.Fatal("invalid limit", v)
		}
	}
	for _, v := range []struct {
		text string
		want int
	}{{"", 1000}, {"0", 0}, {"17", 17}} {
		t.Setenv("BOARD_REVERSE_GEOCODE_MONTHLY_LIMIT", v.text)
		o, err := LoadOptions()
		if err != nil || o.ReverseGeocodeMonthlyLimit != v.want {
			t.Fatal(o, err)
		}
	}
}
