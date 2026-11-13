package httpapi

import (
	"context"
	"encoding/csv"
	"io"
	"log/slog"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"mserp/internal/repository"
)

type reportsFixture struct{ calls int }

func (f *reportsFixture) Read(_ context.Context, q repository.ReportsQuery) (repository.ReportsResult, error) {
	f.calls++
	return repository.ReportsResult{Period: q.Period, Current: []repository.ReportRow{
		{ID: "company", Name: "=SpreadsheetFormula", Metrics: map[string]float64{"gross": 1250}, Sources: []repository.ReportEvidence{
			{Kind: "Loads", ID: "11", Label: "@Load", Date: q.Period.DateFrom, Values: map[string]float64{"gross": 1250}},
		}},
	}}, nil
}

func TestReportsPermissionsAndCSV(t *testing.T) {
	fixture := &reportsFixture{}
	router := chi.NewRouter()
	router.Use(requirePermission)
	registerReportsRoutes(router, slog.New(slog.NewTextHandler(io.Discard, nil)), fixture)
	request := func(path, permission string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", path, nil)
		req = req.WithContext(context.WithValue(req.Context(), authContextKey{}, repository.AuthSession{User: repository.AuthUser{Permissions: []string{permission}}}))
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, req)
		return recorder
	}
	for _, path := range []string{"/reports/company", "/reports/owner-operator", "/reports/company/export.csv", "/reports/owner-operator/export.csv"} {
		if out := request(path, "reports.performance"); out.Code != 403 {
			t.Fatalf("%s exposed financial data: %d", path, out.Code)
		}
	}
	for _, path := range []string{"/reports/driver", "/reports/dispatcher/export.csv"} {
		if out := request(path, "reports.read"); out.Code != 403 {
			t.Fatalf("%s exposed performance data: %d", path, out.Code)
		}
	}
	if fixture.calls != 0 {
		t.Fatal("denied request reached data source")
	}
	query := "?from=2026-10-12&to=2026-10-18&compareFrom=2026-09-01&compareTo=2026-09-07"
	if out := request("/reports/company"+query, "reports.read"); out.Code != 200 {
		t.Fatalf("report status %d: %s", out.Code, out.Body)
	}
	out := request("/reports/company/export.csv"+query, "reports.read")
	if out.Code != 200 || !strings.HasPrefix(out.Header().Get("Content-Type"), "text/csv") {
		t.Fatalf("CSV status %d: %s", out.Code, out.Body)
	}
	records, err := csv.NewReader(out.Body).ReadAll()
	if err != nil || len(records) != 3 {
		t.Fatalf("CSV: %v, %v", records, err)
	}
	if records[1][3] != "'=SpreadsheetFormula" || records[2][7] != "'@Load" || records[2][slices.Index(records[0], "gross (USD)")] != "12.5" {
		t.Fatalf("CSV lost values or formula escaping: %v", records)
	}
}
