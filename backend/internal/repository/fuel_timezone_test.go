package repository

import (
	"strings"
	"testing"
)

func TestNormalizeFuelTimezone(t *testing.T) {
	tests := map[string]string{
		"US/Central":       "America/Chicago",
		" US/Eastern ":     "America/New_York",
		"America/Denver":   "America/Denver",
		"Pacific/Honolulu": "Pacific/Honolulu",
		"":                 "",
	}

	for input, expected := range tests {
		if actual := normalizeFuelTimezone(input); actual != expected {
			t.Errorf("normalizeFuelTimezone(%q) = %q, want %q", input, actual, expected)
		}
	}
}

func TestFuelTimezoneExpressionFallsBackSafely(t *testing.T) {
	expression := fuelTimezoneExpression("t.timezone")
	for _, expected := range []string{"US/Central", "America/Chicago", "pg_timezone_names", "America/New_York"} {
		if !strings.Contains(expression, expected) {
			t.Errorf("timezone expression does not contain %q", expected)
		}
	}
}
