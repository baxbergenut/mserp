package repository

import "testing"

func TestFormatPersonName(t *testing.T) {
	tests := map[string]string{
		"JOHN DOE":         "John Doe",
		"  jane   DOE  ":   "Jane Doe",
		"mary-jane o'NEIL": "Mary-Jane O'Neil",
		"":                 "",
	}
	for input, expected := range tests {
		if actual := formatPersonName(input); actual != expected {
			t.Errorf("formatPersonName(%q) = %q, want %q", input, actual, expected)
		}
	}
}

func TestNormalizeName(t *testing.T) {
	if actual := normalizeName("  JOHN   Doe "); actual != "john doe" {
		t.Fatalf("normalizeName returned %q", actual)
	}
}

func TestNormalizeTruckUnit(t *testing.T) {
	if actual := normalizeTruckUnit("  ab  123 "); actual != "AB 123" {
		t.Fatalf("normalizeTruckUnit returned %q", actual)
	}
}
