package httpapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mserp/internal/fleetscope"
	"mserp/internal/gemini"
	"mserp/internal/groq"
	"mserp/internal/jobs"
	"mserp/internal/repository"
)

func NewRouter(
	logger *slog.Logger,
	job *jobs.SyncLoadsJob,
	fuelJob *jobs.SyncFuelJob,
	tollJob *jobs.SyncTollsJob,
	pool *pgxpool.Pool,
	loadRepo *repository.LoadRepository,
	fleetRepo *repository.FleetRepository,
	tollRepo *repository.TollRepository,
	fileRepo *repository.FileRepository,
	fuelRepo *repository.FuelRepository,
	dashboardRepo *repository.DashboardRepository,
	expenseRepo *repository.ExpenseRepository,
	grossBoardRepo *repository.GrossBoardRepository,
	authRepo *repository.AuthRepository,
	customTaskRepo *repository.CustomTaskRepository,
	documentExtractor groq.DocumentExtractor,
	expenseExtractor gemini.ExpenseExtractor,
	fiveELDJob *jobs.SyncFiveELDJob,
	authOptions AuthOptions,
	fleetScopeOptions ...fleetscope.Options,
) http.Handler {
	r := chi.NewRouter()
	auth := newAuthHandler(logger, authRepo, authOptions)

	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	r.Get("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := pool.Ping(r.Context()); err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ready"})
	})
	r.Post("/auth/login", auth.login)
	var fleetScopeOption fleetscope.Options
	if len(fleetScopeOptions) > 0 {
		fleetScopeOption = fleetScopeOptions[0]
	}
	r.Post("/integrations/fleetscope/driver-hired", fleetScopeWebhook(logger, fleetRepo, fleetScopeOption))
	r.Post("/integrations/fleetscope/driver-terminated", fleetScopeWebhook(logger, fleetRepo, fleetScopeOption, "driver.terminated"))

	protected := chi.NewRouter()
	protected.Use(auth.requireSession)
	protected.Use(auth.requireCSRF)
	protected.Use(requirePermission)
	registerAccessRoutes(protected, auth, authRepo)
	protected.Get("/auth/session", auth.session)
	protected.Post("/auth/logout", auth.logout)
	protected.Post("/auth/password", auth.changePassword)
	protected.Put("/auth/theme", auth.setTheme)

	protected.Post("/jobs/sync-loads", func(w http.ResponseWriter, r *http.Request) {
		result, err := job.Run(r.Context())
		if err != nil {
			logger.Error("sync loads failed", "error", err)
			writeAPIError(w, http.StatusBadGateway, "DataTruck load sync failed: "+err.Error())
			return
		}

		writeJSON(w, http.StatusOK, result)
	})
	protected.Post("/jobs/sync-eld", func(w http.ResponseWriter, r *http.Request) {
		if fiveELDJob == nil {
			writeAPIError(w, http.StatusServiceUnavailable, "Five ELD integration is not configured")
			return
		}
		result, err := fiveELDJob.Run(r.Context())
		if errors.Is(err, jobs.ErrFiveELDSyncInProgress) {
			writeAPIError(w, http.StatusConflict, err.Error())
			return
		}
		if err != nil {
			logger.Error("sync Five ELD failed", "error", err)
			writeAPIError(w, http.StatusBadGateway, "Five ELD location refresh failed")
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
	protected.Get("/loads", func(w http.ResponseWriter, r *http.Request) {
		if wantsPagination(r) {
			pagination, err := parsePagination(r)
			if err != nil {
				writeAPIError(w, http.StatusBadRequest, err.Error())
				return
			}
			pickupFrom, err := parseOptionalDate(r.URL.Query().Get("pickupFrom"), "pickupFrom")
			if err != nil {
				writeAPIError(w, http.StatusBadRequest, err.Error())
				return
			}
			pickupTo, err := parseOptionalDate(r.URL.Query().Get("pickupTo"), "pickupTo")
			if err != nil {
				writeAPIError(w, http.StatusBadRequest, err.Error())
				return
			}
			loads, err := loadRepo.GetLoadsPage(r.Context(), repository.LoadPageQuery{
				Pagination: pagination,
				Search:     r.URL.Query().Get("search"), Status: r.URL.Query().Get("status"),
				Customer: r.URL.Query().Get("customer"), Dispatcher: r.URL.Query().Get("dispatcher"),
				Driver: r.URL.Query().Get("driver"), PickupFrom: pickupFrom, PickupTo: pickupTo,
				Sort: r.URL.Query().Get("sort"), Direction: r.URL.Query().Get("direction"),
			})
			if err != nil {
				logger.Error("get paginated loads failed", "error", err)
				writeAPIError(w, http.StatusInternalServerError, "the loads could not be loaded")
				return
			}
			writeJSON(w, http.StatusOK, loads)
			return
		}
		loads, err := loadRepo.GetLoads(r.Context())
		if err != nil {
			logger.Error("get loads failed", "error", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(loads)
	})

	registerFleetRoutes(protected, logger, fleetRepo)
	registerDriverIntakeRoutes(protected, logger, fleetRepo)
	registerCustomTaskRoutes(protected, logger, customTaskRepo)
	registerGrossBoardRoutes(protected, logger, grossBoardRepo)
	registerDriverBoardRoutes(protected, logger, repository.NewDriverBoardRepository(pool, fiveELDJob != nil))
	registerDriverPayRoutes(protected, logger, repository.NewDriverPayRepository(pool), job)
	registerDriverChargeRoutes(protected, logger, repository.NewDriverChargeRepository(pool))
	registerInvestorPayRoutes(protected, logger, repository.NewDriverPayRepository(pool), repository.NewDriverChargeRepository(pool))
	registerTollRoutes(protected, logger, tollJob, tollRepo)
	registerFileRoutes(protected, logger, fileRepo, documentExtractor)
	registerFuelRoutes(protected, logger, fuelJob, fuelRepo)
	registerDashboardRoutes(protected, logger, dashboardRepo)
	registerExpenseRoutes(protected, logger, expenseRepo, expenseExtractor)
	registerEscrowRoutes(protected, logger, repository.NewEscrowRepository(pool))
	r.Mount("/", protected)

	return r
}
