package httpapi

import (
	"context"
	"encoding/csv"
	"encoding/json"
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
	return repository.ReportsResult{Revision: "fixture", Period: q.Period, Current: []repository.ReportRow{
		{ID: "company", Name: "=SpreadsheetFormula", Metrics: map[string]float64{"gross": 1250, "earnedPay": 500}, Estimated: map[string]bool{"earnedPay": true}, Sources: []repository.ReportEvidence{
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
	for _, path := range []string{"/reports/company", "/reports/owner-operator", "/reports/company/sources", "/reports/owner-operator/sources", "/reports/company/export.csv", "/reports/owner-operator/export.csv"} {
		if out := request(path, "reports.performance"); out.Code != 403 {
			t.Fatalf("%s exposed financial data: %d", path, out.Code)
		}
	}
	for _, path := range []string{"/reports/driver", "/reports/driver/sources", "/reports/dispatcher/sources", "/reports/dispatcher/export.csv"} {
		if out := request(path, "reports.read"); out.Code != 403 {
			t.Fatalf("%s exposed performance data: %d", path, out.Code)
		}
	}
	for _, suffix := range []string{"", "/sources", "/export.csv"} {
		for _, permission := range []string{"reports.read", "investors.read"} {
			if out := request("/reports/investor"+suffix, permission); out.Code != 403 {
				t.Fatalf("investor report exposed with %s: %d", permission, out.Code)
			}
		}
	}
	if fixture.calls != 0 {
		t.Fatal("denied request reached data source")
	}
	query := "?week=2026-10-19"
	for _, invalid := range []string{"2026-10-12", "2026-10-20", "2099-01-05", "invalid"} {
		for _, path := range []string{"/reports/company", "/reports/company/sources", "/reports/company/export.csv"} {
			if out := request(path+"?week="+invalid, "reports.read"); out.Code != 400 {
				t.Fatalf("%s accepted invalid week %s: %d", path, invalid, out.Code)
			}
		}
	}
	for _, path := range []string{"/reports/company/sources", "/reports/company/export.csv"} {
		if out := request(path+query+"&revision=obsolete", "reports.read"); out.Code != 409 {
			t.Fatalf("%s accepted changed source records: %d", path, out.Code)
		}
	}
	for _, tc := range []struct {
		group, revision string
		wantReport      bool
		wantSources     int
	}{
		{"company", "fixture", false, 1},
		{"company", "obsolete", true, 1},
		{"removed", "obsolete", true, 0},
	} {
		calls := fixture.calls
		out := request("/reports/company/sources"+query+"&refreshSummary=true&group="+tc.group+"&revision="+tc.revision, "reports.read")
		var body struct {
			Sources []repository.ReportEvidence
			Report  *repository.ReportsResult
		}
		if err := json.Unmarshal(out.Body.Bytes(), &body); err != nil || out.Code != 200 || fixture.calls != calls+1 || body.Sources == nil || len(body.Sources) != tc.wantSources || (body.Report != nil) != tc.wantReport {
			t.Fatalf("drilldown %+v: status %d body %s", tc, out.Code, out.Body)
		}
		if tc.wantReport && (body.Report.Revision != "fixture" || len(body.Report.Current) != 1 || body.Report.Current[0].Sources != nil) {
			t.Fatalf("expected a compact current summary: %+v", body.Report)
		}
	}
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
	if records[1][slices.Index(records[0], "Driver fee (USD)")] != "5" || records[1][slices.Index(records[0], "Note")] != "Estimated: earnedPay" {
		t.Fatal("CSV lost driver fee label or estimate status", records)
	}
	for _, excluded := range []string{"equipment (USD)", "unassignedCost (USD)", "carrierCosts (USD)", "contribution (USD)", "revenuePerMile (USD)"} {
		if slices.Contains(records[0], excluded) {
			t.Fatalf("removed column in CSV: %s", excluded)
		}
	}
	if !slices.Contains(records[0], "Profit (USD)") || !slices.Contains(records[0], "RPM (USD)") {
		t.Fatal("CSV labels differ from report", records[0])
	}
	if records[0][len(records[0])-1] != "Profit (USD)" || slices.Index(records[0], "CPM (USD)") != slices.Index(records[0], "RPM (USD)")+1 || slices.Index(records[0], "miles") != slices.Index(records[0], "gross (USD)")+1 {
		t.Fatal("CSV column order differs from report", records[0])
	}
}
