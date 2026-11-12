package httpapi

import (
	"strings"
	"testing"
)

func TestInvestorValidation(t *testing.T) {
	for _, v := range []investorRequest{{}, {FullName: strings.Repeat("a", 201)}, {FullName: "A", Notes: strings.Repeat("n", 5001)}, {FullName: "A", DriverID: new(string)}} {
		if _, err := v.validate(); err == nil {
			t.Fatalf("accepted invalid investor: %+v", v)
		}
	}
	id := "00000000-0000-0000-0000-000000000002"
	if _, err := (investorRequest{DriverID: &id, Active: true}).validate(); err != nil {
		t.Fatal(err)
	}
}
