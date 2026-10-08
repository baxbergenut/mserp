package httpapi

import (
	"mserp/internal/repository"
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

func TestInvestorDirectory(t *testing.T) {
	id := "driver"
	values := []repository.Investor{
		{ID: "company", IsCompany: true}, {ID: "independent"},
		{ID: "owner", DriverID: &id, Trucks: []repository.InvestorTruck{{ID: "a"}}},
		{ID: "investor", DriverID: &id, Trucks: []repository.InvestorTruck{{ID: "a"}, {ID: "b"}}},
	}
	visible := investorDirectory(values, false)
	if len(visible) != 3 || visible[0].ID != "independent" || visible[1].ID != "owner" || visible[2].ID != "investor" {
		t.Fatalf("directory: %+v", visible)
	}
	if len(investorDirectory(values, true)) != 4 {
		t.Fatal("company toggle must retain all owners")
	}
	if len(values) != 4 {
		t.Fatal("owner lookups must retain all identities")
	}
}
