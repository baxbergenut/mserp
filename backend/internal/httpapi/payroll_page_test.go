package httpapi

import (
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"mserp/internal/repository"
)

func TestPayrollWeekAvailability(t *testing.T) {
	current, _ := time.Parse(time.DateOnly, repository.ChargeCurrentWeek())
	for _, offset := range []int{-7, 0, 7, 14} {
		_, err := availablePayrollWeek(current.AddDate(0, 0, offset).Format(time.DateOnly))
		if (err != nil) != (offset > 0) {
			t.Fatalf("week offset %d: %v", offset, err)
		}
	}
	// Reject at the HTTP boundary, before touching a repository or issuing work.
	router := chi.NewRouter()
	registerDriverPayRoutes(router, slog.Default(), nil, nil)
	registerInvestorPayRoutes(router, slog.Default(), nil, nil)
	future := current.AddDate(0, 0, 7).Format(time.DateOnly)
	for _, kind := range []string{"driver", "investor"} {
		for _, method := range []string{"GET", "PUT"} {
			body := `{"driverId":"18200000-0000-0000-0000-000000000001","weekStart":"` + future + `","version":0}`
			r := httptest.NewRequest(method, "/"+kind+"-pay?weekStart="+future, strings.NewReader(body))
			w := httptest.NewRecorder()
			router.ServeHTTP(w, r)
			if w.Code != 400 || !strings.Contains(w.Body.String(), "when the week starts") {
				t.Fatal(kind, method, w.Code, w.Body.String())
			}
		}
	}
}

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
