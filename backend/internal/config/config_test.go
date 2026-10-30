package config

import (
	"testing"
	"time"
)

func TestAuthSessionTTL(t *testing.T) {
	for key, value := range map[string]string{
		"DATABASE_URL":           "postgres://localhost/mserp_test",
		"DATATRUCK_API_KEY":      "test-key",
		"DATATRUCK_COMPANY_NAME": "Test company",
		"RELAY_API_KEY":          "test-key",
		"PREPASS_CLIENT_ID":      "test-client",
		"PREPASS_CLIENT_SECRET":  "test-secret",
	} {
		t.Setenv(key, value)
	}
	for _, tc := range []struct {
		name, value string
		want        time.Duration
	}{
		{name: "default", want: 8 * time.Hour},
		{name: "explicit eight hours", value: "8h", want: 8 * time.Hour},
		{name: "configured override", value: "2h", want: 2 * time.Hour},
		{name: "legacy override capped", value: "12h", want: 8 * time.Hour},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("AUTH_SESSION_TTL", tc.value)
			cfg, err := Load()
			if err != nil {
				t.Fatal(err)
			}
			if cfg.AuthSessionTTL != tc.want {
				t.Fatalf("AuthSessionTTL = %v, want %v", cfg.AuthSessionTTL, tc.want)
			}
		})
	}
}

func TestParseDailySyncTime(t *testing.T) {
	t.Setenv("TEST_DAILY_SYNC_TIME", "23:45")

	got, err := parseDailySyncTime("TEST_DAILY_SYNC_TIME", "02:00")
	if err != nil {
		t.Fatal(err)
	}
	if got.Hour != 23 || got.Minute != 45 {
		t.Fatalf("parseDailySyncTime() = %+v, want hour 23 minute 45", got)
	}
}

func TestDataTruckIntervals(t *testing.T) {
	for _, value := range []string{"0", "30s", "-1m", "bad", "25h"} {
		t.Setenv("TEST_DATATRUCK_INTERVAL", value)
		if _, err := parseDataTruckInterval("TEST_DATATRUCK_INTERVAL", "1m"); err == nil {
			t.Errorf("accepted %s", value)
		}
	}
	t.Setenv("TEST_DATATRUCK_INTERVAL", "")
	if got, err := parseDataTruckInterval("TEST_DATATRUCK_INTERVAL", "1m"); err != nil || got != time.Minute {
		t.Fatal(got, err)
	}
	t.Setenv("TEST_DATATRUCK_INTERVAL", "5m")
	if got, err := parseDataTruckInterval("TEST_DATATRUCK_INTERVAL", "1m"); err != nil || got != 5*time.Minute {
		t.Fatal(got, err)
	}
}

func TestParseDailySyncTimeRejectsInvalidValue(t *testing.T) {
	t.Setenv("TEST_DAILY_SYNC_TIME", "25:00")

	if _, err := parseDailySyncTime("TEST_DAILY_SYNC_TIME", "02:00"); err == nil {
		t.Fatal("parseDailySyncTime() error = nil, want an error")
	}
}
