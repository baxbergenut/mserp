package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mserp/internal/fleetscope"
	"mserp/internal/groq"
	"mserp/internal/jobs"
	"mserp/internal/openai"
	"mserp/internal/repository"
)

// Long-lived streams stop when HTTP shutdown begins; ordinary requests drain.
type shutdownRouter struct {
	*chi.Mux
	shutdown func()
}

func (r *shutdownRouter) Shutdown() { r.shutdown() }

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
	expenseExtractor openai.ExpenseExtractor,
	fiveELDJob *jobs.SyncFiveELDJob,
	wmtClient repository.WMTProvider,
	authOptions AuthOptions,
	fleetScopeOptions ...fleetscope.Options,
) http.Handler {
	r := chi.NewRouter()
	r.Use(performance(logger))
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
	registerLookupRoutes(protected, fleetRepo)
	wmtRepo := repository.NewWeighMyTruckRepository(pool, wmtClient)
	registerSidebarRoutes(protected, logger, fuelRepo, wmtRepo, tollRepo)
	registerWeighMyTruckRoutes(protected, logger, wmtRepo)
	registerDriverServiceRoutes(protected, logger, repository.NewDriverServiceRepository(pool, fuelRepo, wmtRepo))
	registerDriverIntakeRoutes(protected, logger, fleetRepo, wmtRepo)
	registerFleetSyncRoutes(protected, logger, repository.NewFleetSyncRepository(pool, fleetScopeOption.Sync))
	registerCustomTaskRoutes(protected, logger, customTaskRepo)
	registerTaskBoardRoutes(protected, logger, customTaskRepo)
	events := newTaskEvents(pool, logger)
	protected.Get("/tasks/events", events.serve(auth))
	boardEvents := newDatabaseEvents(pool, logger, "mserp_driver_board", "driver_board.read")
	boardPresence := newDatabaseEvents(pool, logger, "mserp_board_presence", "driver_board.read")
	boardRepo := repository.NewDriverBoardRepository(pool, fiveELDJob != nil)
	boardEvents.snapshotEvent = "board"
	boardEvents.snapshot = func(ctx context.Context) ([]byte, error) {
		week, _ := time.Parse(time.DateOnly, repository.ChargeCurrentWeek())
		value, err := boardRepo.Get(ctx, week)
		if err != nil {
			return nil, err
		}
		return json.Marshal(value)
	}
	boardPresence.snapshotEvent = "peers"
	boardPresence.snapshot = func(ctx context.Context) ([]byte, error) {
		value, err := boardRepo.Presence(ctx)
		if err != nil {
			return nil, err
		}
		return json.Marshal(value)
	}
	protected.Get("/driver-board/events", boardEvents.serve(auth, boardPresence))
	registerGrossBoardRoutes(protected, logger, grossBoardRepo)
	registerDriverBoardRoutes(protected, logger, boardRepo)
	payRepo := repository.NewDriverPayRepository(pool)
	registerDriverPayRoutes(protected, logger, payRepo, job)
	registerDriverChargeRoutes(protected, logger, repository.NewDriverChargeRepository(pool))
	registerInvestorPayRoutes(protected, logger, payRepo, repository.NewDriverChargeRepository(pool))
	registerTollRoutes(protected, logger, tollJob, tollRepo)
	registerFileRoutes(protected, logger, fileRepo, documentExtractor)
	registerFuelRoutes(protected, logger, fuelJob, fuelRepo)
	registerDashboardRoutes(protected, logger, dashboardRepo)
	registerExpenseRoutes(protected, logger, expenseRepo, expenseExtractor)
	registerMaintenanceRoutes(protected, pool, logger, expenseRepo, expenseExtractor, fiveELDJob)
	registerEscrowRoutes(protected, logger, repository.NewEscrowRepository(pool))
	r.Mount("/", protected)

	return &shutdownRouter{Mux: r, shutdown: func() { events.shutdown(); boardEvents.shutdown(); boardPresence.shutdown() }}
}
