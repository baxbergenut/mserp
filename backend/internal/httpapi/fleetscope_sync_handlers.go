package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"slices"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"mserp/internal/repository"
)

func registerFleetSyncRoutes(r chi.Router, logger *slog.Logger, repo *repository.FleetSyncRepository) {
	fail := func(w http.ResponseWriter, e error) {
		status := 400
		message := e.Error()
		var pgerr *pgconn.PgError
		if errors.As(e, &pgerr) {
			status = 500
			message = "FleetScope synchronization data could not be saved"
			if pgerr.Code == "23505" || pgerr.Code == "40001" || pgerr.Code == "40P01" {
				status = 409
				message = "The record changed or already has a pending operation; refresh the comparison"
			}
			logger.Error("FleetScope sync persistence failed", "code", pgerr.Code)
		} else if errors.Is(e, repository.ErrFleetSyncConflict) || errors.Is(e, pgx.ErrNoRows) {
			status = 409
		} else if errors.Is(e, repository.ErrNotFound) {
			status = 404
		} else if errors.Is(e, context.Canceled) || errors.Is(e, context.DeadlineExceeded) {
			status = 503
			message = "FleetScope request did not complete; refresh before retrying"
		}
		writeAPIError(w, status, message)
	}
	allowed := func(w http.ResponseWriter, r *http.Request, admin bool) (repository.AuthSession, bool) {
		s, ok := authSessionFromContext(r.Context())
		if !ok || !slices.Contains(s.User.Permissions, "fleet.read") || (admin && !s.User.Administrator) {
			writeAPIError(w, 403, "Fleet read permission is required; reconciliation is restricted to Administrators")
			return s, false
		}
		return s, true
	}
	r.Get("/fleetscope-sync", func(w http.ResponseWriter, r *http.Request) {
		s, ok := allowed(w, r, false)
		if !ok {
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		v, e := repo.View(r.Context())
		v.CanReview = s.User.Administrator && slices.Contains(s.User.Permissions, "fleetscope.reconcile")
		v.CanManage = slices.Contains(s.User.Permissions, "fleetscope.manage")
		if e != nil {
			fail(w, e)
			return
		}
		writeJSON(w, 200, v)
	})
	r.Post("/fleetscope-sync/snapshot", func(w http.ResponseWriter, r *http.Request) {
		s, ok := allowed(w, r, true)
		if !ok {
			return
		}
		if e := repo.CaptureReview(r.Context(), s.User.ID); e != nil {
			fail(w, e)
			return
		}
		writeJSON(w, 200, map[string]bool{"saved": true})
	})
	r.Post("/fleetscope-sync/review", func(w http.ResponseWriter, r *http.Request) {
		s, ok := allowed(w, r, true)
		if !ok {
			return
		}
		var in repository.FleetSyncReviewInput
		if e := decodeJSON(r, &in); e != nil {
			writeAPIError(w, 400, "invalid reconciliation review")
			return
		}
		if e := repo.Review(r.Context(), in, s.User.ID); e != nil {
			fail(w, e)
			return
		}
		writeJSON(w, 200, map[string]bool{"queued": true})
	})
	type stateInput struct {
		SnapshotID     string `json:"snapshotId"`
		SourceRevision int64  `json:"sourceRevision"`
		Version        int64  `json:"version"`
	}
	for _, action := range []string{"assignments", "activate", "pause", "resume"} {
		r.Post("/fleetscope-sync/"+action, func(w http.ResponseWriter, r *http.Request) {
			s, ok := allowed(w, r, action != "pause")
			if !ok {
				return
			}
			var in stateInput
			if e := decodeJSON(r, &in); e != nil {
				writeAPIError(w, 400, "invalid synchronization action")
				return
			}
			var e error
			switch action {
			case "assignments":
				e = repo.PrepareAssignments(r.Context(), in.SnapshotID, in.SourceRevision, s.User.ID)
			case "activate":
				e = repo.Activate(r.Context(), in.SnapshotID, in.SourceRevision, in.Version, s.User.ID)
			case "pause":
				e = repo.Pause(r.Context(), in.Version, s.User.ID)
			case "resume":
				e = repo.ResumeReview(r.Context(), in.Version, s.User.ID)
			}
			if e != nil {
				fail(w, e)
				return
			}
			writeJSON(w, 200, map[string]bool{"saved": true})
		})
	}
	for _, action := range []string{"retry", "supersede"} {
		r.Post("/fleetscope-sync/operations/{id}/"+action, func(w http.ResponseWriter, r *http.Request) {
			s, ok := allowed(w, r, action == "supersede")
			if !ok {
				return
			}
			id, ok := pathID(w, r)
			if !ok {
				return
			}
			var e error
			if action == "retry" {
				e = repo.Retry(r.Context(), id, s.User.ID)
			} else {
				e = repo.Supersede(r.Context(), id, s.User.ID)
			}
			if e != nil {
				fail(w, e)
				return
			}
			writeJSON(w, 200, map[string]bool{"saved": true})
		})
	}
}
