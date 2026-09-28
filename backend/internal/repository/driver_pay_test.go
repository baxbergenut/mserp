package repository

import "testing"

func TestDriverPayFee(t *testing.T) {
	for _, c := range []struct{ kind, rate, basis, want string }{
		{"cpm", "0.7500", "847.34", "635.51"},
		{"cpm", "0.6555", "100.01", "65.56"},
		{"gross_percentage", "87.0000", "3189.00", "2774.43"},
		{"gross_percentage", "30", "0.05", "0.02"},
		{"gross_percentage", "30", "-0.05", "-0.02"},
		{"gross_percentage", "30", "0", "0.00"},
		{"cpm", "0.65", "", ""}, {"cpm", "0", "100", ""},
		{"cpm", "0.65", "-100", ""}, {"other", "3", "100", ""},
	} {
		if got := driverPayFee(c.kind, c.rate, c.basis); got != c.want {
			t.Errorf("%+v: got %s", c, got)
		}
	}
}
