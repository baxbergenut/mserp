package httpapi

import (
	"strings"
	"testing"
)

func TestTruckColorValidation(t *testing.T) {
	for _, value := range []string{"", " White ", strings.Repeat("é", 30), strings.Repeat("a", 31), "Red\nWhite"} {
		input, err := (truckRequest{UnitNumber: "100", Status: "available", Color: &value}).validate()
		invalid := len([]rune(strings.TrimSpace(value))) > 30 || strings.Contains(value, "\n")
		if (err != nil) != invalid {
			t.Fatalf("color %q: %v", value, err)
		}
		if !invalid && (input.Color == nil || *input.Color != strings.TrimSpace(value)) {
			t.Fatal("color must be trimmed while preserving an explicit clear")
		}
	}
	input, err := (truckRequest{UnitNumber: "100", Status: "available"}).validate()
	if err != nil || input.Color != nil {
		t.Fatal("omitted color must preserve the existing value", err)
	}
}
