package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"mserp/internal/repository"
)

type sidebarCounterFake struct{ calls []string }

func (f *sidebarCounterFake) TerminatedRelayCount(context.Context) (int, error) {
	f.calls = append(f.calls, "fuel")
	return 3, nil
}
func (f *sidebarCounterFake) TerminatedMembershipCount(context.Context) (int, error) {
	f.calls = append(f.calls, "wmt")
	return 2, nil
}
func (f *sidebarCounterFake) RecentUnlinkedCount(context.Context) (int, string, error) {
	f.calls = append(f.calls, "tolls")
	return 7, "2026-09-01", nil
}

func TestSidebarCountsPermissions(t *testing.T) {
	for _, tc := range []struct {
		name        string
		permissions []string
		expected    repository.SidebarCounts
		calls       int
	}{
		{"none", nil, repository.SidebarCounts{}, 0},
		{"write only", []string{"fuel.drivers.write", "weighmytruck.write", "tolls.sync"}, repository.SidebarCounts{}, 0},
		{"fuel", []string{"fuel.read"}, repository.SidebarCounts{Fuel: 3}, 1},
		{"wmt", []string{"weighmytruck.read"}, repository.SidebarCounts{WeighMyTruck: 2}, 1},
		{"tolls", []string{"tolls.read"}, repository.SidebarCounts{Tolls: 7, TollsFrom: "2026-09-01"}, 1},
		{"all", []string{"fuel.read", "weighmytruck.read", "tolls.read"}, repository.SidebarCounts{Fuel: 3, WeighMyTruck: 2, Tolls: 7, TollsFrom: "2026-09-01"}, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := &sidebarCounterFake{}
			r := chi.NewRouter()
			r.Use(requirePermission)
			registerSidebarRoutes(r, slog.New(slog.NewTextHandler(io.Discard, nil)), fake, fake, fake)
			req := httptest.NewRequest("GET", "/sidebar-counts", nil)
			req = req.WithContext(context.WithValue(req.Context(), authContextKey{}, repository.AuthSession{User: repository.AuthUser{Permissions: tc.permissions}}))
			out := httptest.NewRecorder()
			r.ServeHTTP(out, req)
			var got repository.SidebarCounts
			if err := json.Unmarshal(out.Body.Bytes(), &got); err != nil || out.Code != 200 || got != tc.expected || len(fake.calls) != tc.calls {
				t.Fatalf("status=%d counts=%+v calls=%v err=%v", out.Code, got, fake.calls, err)
			}
			if out.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("counts must not be cached")
			}
		})
	}
	if routePermission("POST", "/sidebar-counts") != "" {
		t.Fatal("sidebar count writes must be denied")
	}
	r := chi.NewRouter()
	fake := &sidebarCounterFake{}
	registerSidebarRoutes(r, slog.New(slog.NewTextHandler(io.Discard, nil)), fake, fake, fake)
	out := httptest.NewRecorder()
	r.ServeHTTP(out, httptest.NewRequest("GET", "/sidebar-counts", nil))
	if out.Code != 401 || len(fake.calls) != 0 {
		t.Fatal("unauthenticated counts exposed")
	}
}
