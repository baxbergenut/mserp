package httpapi

import (
	"strings"
	"testing"
)

func TestDriverNameParts(t *testing.T) {
	ptr := func(s string) *string { return &s }
	for _, tc := range []struct {
		name        string
		first, last *string
		full, want  string
		invalid     bool
	}{
		{name: "legacy", full: "Legacy Unsplit Name", want: "Legacy Unsplit Name"},
		{name: "compound names", first: ptr("  Ana   María "), last: ptr("de la Cruz"), full: "ignored", want: "Ana María de la Cruz"},
		{name: "missing family", first: ptr("Jane"), invalid: true},
		{name: "missing given", last: ptr("Doe"), invalid: true},
		{name: "blank", first: ptr(" "), last: ptr("Doe"), invalid: true},
		{name: "too long", first: ptr(strings.Repeat("a", 51)), last: ptr("Doe"), invalid: true},
		{name: "unicode limit", first: ptr(strings.Repeat("é", 50)), last: ptr("Doe"), want: strings.Repeat("é", 50) + " Doe"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := (driverRequest{FullName: tc.full, FirstName: tc.first, LastName: tc.last, PayType: "cpm"}).validate()
			if (err != nil) != tc.invalid {
				t.Fatalf("validation: %v", err)
			}
			if !tc.invalid && got.FullName != tc.want {
				t.Fatalf("name: %q", got.FullName)
			}
		})
	}
}
