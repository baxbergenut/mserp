package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mserp/internal/groq"
	"mserp/internal/jobs"
	"mserp/internal/repository"
)

func NewRouter(
	logger *slog.Logger,
	job *jobs.SyncLoadsJob,
	fuelJob *jobs.SyncFuelJob,
	pool *pgxpool.Pool,
	loadRepo *repository.LoadRepository,
	fleetRepo *repository.FleetRepository,
	tollRepo *repository.TollRepository,
	fileRepo *repository.FileRepository,
	fuelRepo *repository.FuelRepository,
	authRepo *repository.AuthRepository,
	documentExtractor groq.DocumentExtractor,
	authOptions AuthOptions,
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

	protected := chi.NewRouter()
	protected.Use(auth.requireSession)
	protected.Use(auth.requireCSRF)
	protected.Get("/auth/session", auth.session)
	protected.Post("/auth/logout", auth.logout)

	protected.Post("/jobs/sync-loads", func(w http.ResponseWriter, r *http.Request) {
		result, err := job.Run(r.Context())
		if err != nil {
			logger.Error("sync loads failed", "error", err)
			writeAPIError(w, http.StatusBadGateway, "DataTruck load sync failed: "+err.Error())
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
	registerTollRoutes(protected, logger, tollRepo)
	registerFileRoutes(protected, logger, fileRepo, documentExtractor)
	registerFuelRoutes(protected, logger, fuelJob, fuelRepo)
	r.Mount("/", protected)

	return r
}
