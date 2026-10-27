package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/go-chi/chi/v5"
	"io"
	"log/slog"
	"mserp/internal/repository"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

type dashboardFake struct {
	mu         sync.Mutex
	calls      []string
	categories []string
	fail       string
}

func (f *dashboardFake) call(key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, key)
	if key == f.fail {
		return errors.New("synthetic database failure")
	}
	return nil
}
func (f *dashboardFake) Performance(context.Context, repository.DashboardPeriod) (repository.DashboardPerformance, error) {
	return repository.DashboardPerformance{Current: repository.DashboardMetric{Gross: "123.45"}}, f.call("performance")
}
func (f *dashboardFake) DashboardCounts(_ context.Context, key string) ([]repository.DashboardItem, error) {
	return []repository.DashboardItem{{ID: key, Count: 3}}, f.call(key)
}
func (f *dashboardFake) DashboardCost(_ context.Context, key string, _ repository.DashboardPeriod, categories []string) ([]repository.DashboardItem, error) {
	f.mu.Lock()
	f.categories = categories
	f.mu.Unlock()
	return []repository.DashboardItem{{ID: key, Count: 1}}, f.call(key)
}
func (f *dashboardFake) DashboardLoads(context.Context, repository.DashboardPeriod, string, string, repository.Pagination) (repository.Page[repository.DashboardLoad], error) {
	return repository.Page[repository.DashboardLoad]{Items: []repository.DashboardLoad{}}, f.call("loads")
}
func (f *dashboardFake) CategoryID(context.Context) (string, error) {
	return "maintenance", f.call("category")
}
func (f *dashboardFake) ProgramTrucks(context.Context, string, string) ([]repository.ProgramTruck, error) {
	return nil, f.call("service")
}
func (f *dashboardFake) DashboardTasks(context.Context, bool, bool) ([]repository.DashboardItem, error) {
	return nil, f.call("tasks")
}
func (f *dashboardFake) TerminatedRelayCount(context.Context) (int, error) {
	return 2, f.call("relayAccess")
}
func (f *dashboardFake) TerminatedMembershipCount(context.Context) (int, error) {
	return 2, f.call("scaleAccess")
}
func (f *dashboardFake) RecentUnlinkedCount(context.Context) (int, string, error) {
	return 2, "2026-09-01", f.call("unlinkedTolls")
}

func TestCompanyDashboardAccess(t *testing.T) {
	for _, tc := range []struct {
		name        string
		permissions []string
		sections    int
	}{
		{"none", nil, 0}, {"board", []string{"board.read"}, 1}, {"trucks", []string{"trucks.read"}, 2}, {"expenses without categories", []string{"expenses.read"}, 0}, {"costs", []string{"fuel.read", "tolls.read", "weighmytruck.read"}, 6}, {"tasks", []string{"tasks.read"}, 1},
		{"trailers are omitted", []string{"trailers.read"}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := &dashboardFake{}
			r := chi.NewRouter()
			r.Use(requirePermission)
			registerCompanyDashboardRoutes(r, slog.New(slog.NewTextHandler(io.Discard, nil)), fake, fake, fake, fake, fake, fake)
			req := httptest.NewRequest("GET", "/dashboard/summary?preset=lastWeek", nil)
			req = req.WithContext(context.WithValue(req.Context(), authContextKey{}, repository.AuthSession{User: repository.AuthUser{Administrator: true, Permissions: tc.permissions}}))
			out := httptest.NewRecorder()
			r.ServeHTTP(out, req)
			var got repository.CompanyDashboard
			if err := json.Unmarshal(out.Body.Bytes(), &got); err != nil || out.Code != 200 || len(got.Sections) != tc.sections || len(fake.calls) != tc.sections {
				t.Fatalf("code=%d result=%s calls=%v", out.Code, out.Body.String(), fake.calls)
			}
			if out.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("cacheable response")
			}
		})
	}
	fake := &dashboardFake{fail: "performance"}
	r := chi.NewRouter()
	r.Use(requirePermission)
	registerCompanyDashboardRoutes(r, slog.New(slog.NewTextHandler(io.Discard, nil)), fake, fake, fake, fake, fake, fake)
	user := repository.AuthUser{Administrator: true, Permissions: []string{"board.read", "trucks.read", "expenses.read"}, ExpenseCategoryAccess: []repository.ExpenseCategoryAccess{{CategoryID: "visible", CanView: true}, {CategoryID: "hidden", CanCreate: true}}}
	request := func(path string, user repository.AuthUser) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", path, nil)
		req = req.WithContext(context.WithValue(req.Context(), authContextKey{}, repository.AuthSession{User: user}))
		out := httptest.NewRecorder()
		r.ServeHTTP(out, req)
		return out
	}
	out := request("/dashboard/summary", user)
	var got repository.CompanyDashboard
	_ = json.Unmarshal(out.Body.Bytes(), &got)
	if got.Sections["performance"].Error == "" || got.Sections["performance"].Performance != nil || got.Sections["trucks"].Error != "" || len(fake.categories) != 1 || fake.categories[0] != "visible" {
		t.Fatalf("partial failure/category projection %s", out.Body.String())
	}
	for _, path := range []string{"/dashboard/loads", "/dashboard/loads?inclusion=included"} {
		if out := request(path, repository.AuthUser{}); out.Code != 403 {
			t.Fatal("load details exposed")
		}
	}
	for _, path := range []string{"/dashboard/summary?preset=bad", "/dashboard/summary?dateFrom=2026-02-30&dateTo=2026-03-01", "/dashboard/loads?dispatcherId=bad", "/dashboard/loads?inclusion=anything"} {
		if out := request(path, user); out.Code != 400 {
			t.Fatalf("invalid query accepted %s %d", path, out.Code)
		}
	}
	if out := request("/dashboard/loads?page=1&pageSize=25", user); out.Code != 200 {
		t.Fatalf("authorized loads rejected %s", out.Body.String())
	}
}

func TestCompanyDashboardRequiresAdministrator(t *testing.T) {
	fake := &dashboardFake{}
	r := chi.NewRouter()
	r.Use(requirePermission)
	registerCompanyDashboardRoutes(r, slog.New(slog.NewTextHandler(io.Discard, nil)), fake, fake, fake, fake, fake, fake)
	permissions := []string{"board.read", "trucks.read", "drivers.read", "trailers.read", "expenses.read", "fuel.read", "tolls.read", "weighmytruck.read", "tasks.read", "access.manage"}
	for _, path := range []string{"/dashboard/summary", "/dashboard/loads", "/dashboard/summary?preset=bad"} {
		req := httptest.NewRequest("GET", path, nil)
		req = req.WithContext(context.WithValue(req.Context(), authContextKey{}, repository.AuthSession{User: repository.AuthUser{Permissions: permissions}}))
		out := httptest.NewRecorder()
		r.ServeHTTP(out, req)
		if out.Code != 403 || len(fake.calls) != 0 {
			t.Fatalf("non-admin reached dashboard: %s %d %v", path, out.Code, fake.calls)
		}
	}
	// An explicit false flag cannot be replaced by a role name or broad permissions.
	for _, admin := range []bool{false, true} {
		h := &authHandler{}
		result := h.makeSessionResponse(context.Background(), repository.AuthUser{Administrator: admin, RoleName: "Administrator"}, "", time.Time{})
		if result.User.Administrator != admin {
			t.Fatal("session response guessed administrator status")
		}
	}
}
