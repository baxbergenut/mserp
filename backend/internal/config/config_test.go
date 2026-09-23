package config

import "testing"

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

func TestParseDailySyncTimeRejectsInvalidValue(t *testing.T) {
	t.Setenv("TEST_DAILY_SYNC_TIME", "25:00")

	if _, err := parseDailySyncTime("TEST_DAILY_SYNC_TIME", "02:00"); err == nil {
		t.Fatal("parseDailySyncTime() error = nil, want an error")
	}
}

func TestParseInt64List(t *testing.T) {
	t.Setenv("TEST_CHAT_IDS", "-1001, 42, -1001")
	values, err := parseInt64List("TEST_CHAT_IDS")
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 2 || values[0] != -1001 || values[1] != 42 {
		t.Fatalf("parseInt64List() = %#v", values)
	}
}
