package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"mserp/internal/repository"
)

type companyDashboardSource interface {
	Performance(context.Context, repository.DashboardPeriod) (repository.DashboardPerformance, error)
	DashboardCounts(context.Context, string) ([]repository.DashboardItem, error)
	DashboardCost(context.Context, string, repository.DashboardPeriod, []string) ([]repository.DashboardItem, error)
	DashboardLoads(context.Context, repository.DashboardPeriod, string, string, repository.Pagination) (repository.Page[repository.DashboardLoad], error)
}

type dashboardTaskSource interface {
	DashboardTasks(context.Context, bool, bool) ([]repository.DashboardItem, error)
}
type dashboardMaintenanceSource interface {
	CategoryID(context.Context) (string, error)
	ProgramTrucks(context.Context, string, string) ([]repository.ProgramTruck, error)
}

func registerCompanyDashboardRoutes(r chi.Router, logger *slog.Logger, repo companyDashboardSource, tasks dashboardTaskSource, maintenance dashboardMaintenanceSource, fuel sidebarFuelCounter, wmt sidebarWMTCounter, tolls sidebarTollCounter) {
	administrator := func(w http.ResponseWriter, r *http.Request) bool {
		session, ok := authSessionFromContext(r.Context())
		if !ok || !session.User.Administrator {
			permissionDenied(w)
			return false
		}
		return true
	}
	period := func(w http.ResponseWriter, r *http.Request) (repository.DashboardPeriod, bool) {
		q := r.URL.Query()
		p, err := repository.ResolveDashboardPeriod(q.Get("preset"), q.Get("dateFrom"), q.Get("dateTo"), time.Now())
		if err != nil {
			writeAPIError(w, 400, err.Error())
			return p, false
		}
		return p, true
	}
	r.Get("/dashboard/summary", func(w http.ResponseWriter, r *http.Request) {
		if !administrator(w, r) {
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		r = r.WithContext(ctx)
		p, ok := period(w, r)
		if !ok {
			return
		}
		session, ok := authSessionFromContext(r.Context())
		if !ok {
			writeAPIError(w, 401, "Sign in to view the dashboard")
			return
		}
		user := session.User
		has := func(key string) bool { return repository.HasPermission(user, key) }
		result := repository.CompanyDashboard{Period: p, Sections: map[string]repository.DashboardSection{}}
		visible, _ := expenseCategoryIDs(user.ExpenseCategoryAccess)
		type job struct {
			key, kind, title string
			run              func(context.Context) (repository.DashboardSection, error)
		}
		jobs := []job{}
		add := func(key, kind, title string, run func(context.Context) ([]repository.DashboardItem, error)) {
			jobs = append(jobs, job{key, kind, title, func(ctx context.Context) (repository.DashboardSection, error) {
				items, err := run(ctx)
				return repository.DashboardSection{Items: items}, err
			}})
		}
		if has("board.read") {
			jobs = append(jobs, job{"performance", "performance", "Gross Board performance", func(ctx context.Context) (repository.DashboardSection, error) {
				v, err := repo.Performance(ctx, p)
				return repository.DashboardSection{Performance: &v}, err
			}})
		}
		for _, v := range []struct{ key, permission, title string }{{"trucks", "trucks.read", "Trucks"}, {"drivers", "drivers.read", "Drivers"}, {"gps", "trucks.read", "GPS attention"}} {
			if has(v.permission) {
				kind := "fleet"
				if v.key == "gps" {
					kind = "attention"
				}
				add(v.key, kind, v.title, func(ctx context.Context) ([]repository.DashboardItem, error) { return repo.DashboardCounts(ctx, v.key) })
			}
		}
		for _, v := range []struct{ key, permission, title string }{{"diesel", "fuel.read", "Diesel"}, {"tolls", "tolls.read", "Tolls"}, {"scale", "weighmytruck.read", "Scale"}, {"expenses", "expenses.read", "Expenses by category"}} {
			if has(v.permission) && (v.key != "expenses" || len(visible) > 0) {
				add(v.key, "cost", v.title, func(ctx context.Context) ([]repository.DashboardItem, error) {
					return repo.DashboardCost(ctx, v.key, p, visible)
				})
			}
		}
		if has("tasks.read") {
			add("tasks", "attention", "Your tasks", func(ctx context.Context) ([]repository.DashboardItem, error) {
				return tasks.DashboardTasks(ctx, has("drivers.read"), has("trucks.read"))
			})
		}
		if has("fuel.read") {
			add("relayAccess", "attention", "Relay access", func(ctx context.Context) ([]repository.DashboardItem, error) {
				count, err := fuel.TerminatedRelayCount(ctx)
				return []repository.DashboardItem{{ID: "relay", Label: "Terminated drivers in Relay", Count: count, Href: "/fuel?dashboard=1&tab=drivers"}}, err
			})
		}
		if has("weighmytruck.read") {
			add("scaleAccess", "attention", "Scale access", func(ctx context.Context) ([]repository.DashboardItem, error) {
				count, err := wmt.TerminatedMembershipCount(ctx)
				return []repository.DashboardItem{{ID: "scale", Label: "Terminated drivers with scale access to review", Count: count, Href: "/weighmytruck?dashboard=1&tab=drivers"}}, err
			})
		}
		if has("tolls.read") {
			add("unlinkedTolls", "attention", "Toll links", func(ctx context.Context) ([]repository.DashboardItem, error) {
				count, from, err := tolls.RecentUnlinkedCount(ctx)
				return []repository.DashboardItem{{ID: "unlinked", Label: "Recent tolls without a truck", Count: count, Href: "/tolls?dashboard=1&tab=transactions&unlinked=true&dateFrom=" + from, Note: "Posted since " + from}}, err
			})
		}
		// Read category identity only when the user has at least one viewable category.
		if has("expenses.read") && len(visible) > 0 {
			category, err := maintenance.CategoryID(r.Context())
			if err == nil && hasExpenseCategoryAction(user.ExpenseCategoryAccess, category, "view") {
				add("service", "attention", "Service programs", func(ctx context.Context) ([]repository.DashboardItem, error) {
					rows, err := maintenance.ProgramTrucks(ctx, "", "")
					if err != nil {
						return nil, err
					}
					counts := map[string]*repository.DashboardItem{}
					order := []string{}
					for _, v := range rows {
						if !v.Enrolled || !v.Active || (v.Status != "due" && v.Status != "overdue") {
							continue
						}
						if counts[v.ProgramID] == nil {
							order = append(order, v.ProgramID)
							counts[v.ProgramID] = &repository.DashboardItem{ID: v.ProgramID, Label: v.ProgramName + " due / overdue", Href: "/fleet/service-programs?dashboard=1&program=" + v.ProgramID + "&status=due"}
						}
						counts[v.ProgramID].Count++
					}
					items := []repository.DashboardItem{}
					for _, id := range order {
						items = append(items, *counts[id])
					}
					return items, nil
				})
			}
		}
		var mu sync.Mutex
		var wg sync.WaitGroup
		gate := make(chan struct{}, 3)
		for _, j := range jobs {
			wg.Add(1)
			go func() {
				defer wg.Done()
				var section repository.DashboardSection
				var err error
				select {
				case gate <- struct{}{}:
					defer func() { <-gate }()
					ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
					defer cancel()
					section, err = j.run(ctx)
				case <-r.Context().Done():
					err = r.Context().Err()
				}
				section.Kind = j.kind
				section.Title = j.title
				section.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
				if section.Items == nil {
					section.Items = []repository.DashboardItem{}
				}
				if err != nil {
					logger.Error("dashboard section failed", "section", j.key, "error", err)
					section.Error = "This section could not be loaded"
					section.Items = []repository.DashboardItem{}
					section.Performance = nil
				}
				mu.Lock()
				result.Sections[j.key] = section
				mu.Unlock()
			}()
		}
		wg.Wait()
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, 200, result)
	})
	r.Get("/dashboard/loads", func(w http.ResponseWriter, r *http.Request) {
		if !administrator(w, r) {
			return
		}
		if !requireCapabilities(w, r, "board.read") {
			return
		}
		p, ok := period(w, r)
		if !ok {
			return
		}
		q := r.URL.Query()
		dispatcher, inclusion := q.Get("dispatcherId"), q.Get("inclusion")
		if dispatcher != "" && dispatcher != "unassigned" && !isUUID(dispatcher) {
			writeAPIError(w, 400, "choose a valid dispatcher")
			return
		}
		switch inclusion {
		case "", "included", "provisional", "conflict", "repeat":
		default:
			writeAPIError(w, 400, "choose a valid inclusion filter")
			return
		}
		pagination, err := parsePagination(r)
		if err != nil {
			writeAPIError(w, 400, err.Error())
			return
		}
		if pagination.PageSize == 0 {
			pagination.PageSize = 25
		}
		if pagination.Page < 1 {
			pagination.Page = 1
		}
		value, err := repo.DashboardLoads(r.Context(), p, dispatcher, inclusion, pagination)
		if err != nil {
			logger.Error("dashboard loads failed", "error", err)
			writeAPIError(w, 500, "Load details could not be loaded")
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, 200, value)
	})
}
