package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"slices"

	"github.com/go-chi/chi/v5"
	"mserp/internal/repository"
)

func driverServicePermission(service string) string {
	if service == "datatruck" {
		return "drivers.create"
	}
	if service == "relay" {
		return "fuel.drivers.create"
	}
	return "weighmytruck.create"
}
func serviceIntentContext(w http.ResponseWriter, r *http.Request, items []repository.DriverServiceIntent) (*http.Request, bool) {
	if err := repository.ValidateDriverServiceIntents(items); err != nil {
		writeAPIError(w, 400, err.Error())
		return r, false
	}
	session, _ := authSessionFromContext(r.Context())
	for _, in := range items {
		if !slices.Contains(session.User.Permissions, driverServicePermission(in.Service)) {
			writeAPIError(w, 403, "Service write permission is required for this enrollment choice.")
			return r, false
		}
	}
	return r.WithContext(repository.WithDriverServiceIntents(r.Context(), items, session.User.ID)), true
}
func registerDriverServiceRoutes(router chi.Router, logger *slog.Logger, repo *repository.DriverServiceRepository) {
	fail := func(w http.ResponseWriter, err error) {
		var invalid *repository.DriverServiceError
		if errors.As(err, &invalid) {
			writeAPIError(w, 409, err.Error())
			return
		}
		if errors.Is(err, repository.ErrNotFound) {
			writeAPIError(w, 404, "Driver not found.")
			return
		}
		logger.Error("driver service setup failed", "error", err)
		writeAPIError(w, 500, "Could not load or update driver service setup.")
	}
	router.Get("/driver-services/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		out, err := repo.Status(r.Context(), id)
		if err != nil {
			fail(w, err)
			return
		}
		session, _ := authSessionFromContext(r.Context())
		items := out.Items[:0]
		for _, item := range out.Items {
			read := "weighmytruck.read"
			if item.Service == "datatruck" {
				read = "drivers.read"
			}
			if item.Service == "relay" {
				read = "fuel.read"
			}
			if slices.Contains(session.User.Permissions, read) || slices.Contains(session.User.Permissions, driverServicePermission(item.Service)) {
				items = append(items, item)
			}
		}
		out.Items = items
		if !slices.Contains(session.User.Permissions, "fuel.read") && !slices.Contains(session.User.Permissions, "fuel.drivers.create") {
			out.Policies = nil
			out.CanLinkRelay = false
		}
		out.CanLinkRelay = out.CanLinkRelay && slices.Contains(session.User.Permissions, "tasks.review") && slices.Contains(session.User.Permissions, "fuel.read")
		if !out.CanLinkRelay {
			for i := range out.Items {
				out.Items[i].Candidates = []repository.RelayRosterEntry{}
			}
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, 200, out)
	})
	router.Post("/driver-services/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		var in repository.DriverServiceIntent
		if decodeJSON(r, &in) != nil {
			writeAPIError(w, 400, "Invalid service setup request.")
			return
		}
		if _, ok = serviceIntentContext(w, r, []repository.DriverServiceIntent{in}); !ok {
			return
		}
		session, _ := authSessionFromContext(r.Context())
		if err := repo.Request(r.Context(), id, session.User.ID, in); err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, map[string]bool{"saved": true})
	})
}
