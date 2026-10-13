package httpapi

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPayrollDetailIDs(t *testing.T) {
	id := "12345678-1234-4123-8123-123456789ABC"
	for _, test := range []struct {
		raw string
		bad bool
	}{
		{"", false}, {id, false}, {id + "," + id, false}, {"not-an-id", true},
		{id + ",", true}, {strings.Repeat(id+",", 100) + id, true},
	} {
		q, err := payPageQuery(httptest.NewRequest("GET", "/driver-pay?summary=1&page=1&pageSize=25&detailIds="+test.raw, nil))
		if (err != nil) != test.bad {
			t.Fatalf("detailIds %q: %v", test.raw, err)
		}
		if err == nil && test.raw != "" && !q.DetailIDs[strings.ToLower(id)] {
			t.Fatal("missing normalized identity")
		}
	}
}
