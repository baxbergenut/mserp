package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mserp/internal/jobs"
	"mserp/internal/repository"
)

func NewRouter(
	logger *slog.Logger,
	job *jobs.SyncLoadsJob,
	pool *pgxpool.Pool,
	loadRepo *repository.LoadRepository,
	fleetRepo *repository.FleetRepository,
) http.Handler {
	r := chi.NewRouter()

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

	r.Post("/jobs/sync-loads", func(w http.ResponseWriter, r *http.Request) {
		result, err := job.Run(r.Context())
		if err != nil {
			logger.Error("sync loads failed", "error", err)
			writeAPIError(w, http.StatusBadGateway, "DataTruck load sync failed: "+err.Error())
			return
		}

		writeJSON(w, http.StatusOK, result)
	})
	r.Get("/loads", func(w http.ResponseWriter, r *http.Request) {
		loads, err := loadRepo.GetLoads(r.Context())
		if err != nil {
			logger.Error("get loads failed", "error", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(loads)
	})

	registerFleetRoutes(r, logger, fleetRepo)

	return r
}
