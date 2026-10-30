package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"mserp/internal/repository"
)

func TestPayrollFinalizationBlockedCode(t *testing.T) {
	current, err := time.Parse(time.DateOnly, repository.ChargeCurrentWeek())
	if err != nil {
		t.Fatal(err)
	}
	week := current.AddDate(0, 0, 7).Format(time.DateOnly)
	for _, kind := range []string{"driver", "investor"} {
		t.Run(kind, func(t *testing.T) {
			r := chi.NewRouter()
			pay := &repository.DriverPayRepository{}
			if kind == "driver" {
				registerDriverPayRoutes(r, slog.Default(), pay, nil)
			} else {
				registerInvestorPayRoutes(r, slog.Default(), pay, nil)
			}
			for _, tc := range []struct{ action, extra, code string }{
				{"finalize", "", "finalization_blocked"},
				{"finalize", `,"driverId":"invalid"`, "validation_failed"},
				{"reopen", "", "validation_failed"},
			} {
				request := httptest.NewRequest("POST", "/"+kind+"-pay/"+tc.action, strings.NewReader(`{"weekStart":"`+week+`"`+tc.extra+`}`))
				response := httptest.NewRecorder()
				r.ServeHTTP(response, request)
				var body map[string]string
				if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
					t.Fatal(err)
				}
				if response.Code != 400 || body["code"] != tc.code {
					t.Fatalf("%s%s: status %d, body %v", tc.action, tc.extra, response.Code, body)
				}
			}
		})
	}
}
