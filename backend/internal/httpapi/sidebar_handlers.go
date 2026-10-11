package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"slices"

	"github.com/go-chi/chi/v5"
	"mserp/internal/repository"
)

type sidebarFuelCounter interface {
	TerminatedRelayCount(context.Context) (int, error)
}
type sidebarWMTCounter interface {
	TerminatedMembershipCount(context.Context) (int, error)
}
type sidebarTollCounter interface {
	RecentUnlinkedCount(context.Context) (int, string, error)
}

func registerSidebarRoutes(router chi.Router, logger *slog.Logger, fuel sidebarFuelCounter, wmt sidebarWMTCounter, tolls sidebarTollCounter) {
	router.Get("/sidebar-counts", func(w http.ResponseWriter, r *http.Request) {
		session, ok := authSessionFromContext(r.Context())
		if !ok {
			writeAPIError(w, http.StatusUnauthorized, "Sign in to view sidebar counts")
			return
		}
		counts := repository.SidebarCounts{}
		var err error
		if slices.Contains(session.User.Permissions, "fuel.read") {
			counts.Fuel, err = fuel.TerminatedRelayCount(r.Context())
		}
		if err == nil && slices.Contains(session.User.Permissions, "weighmytruck.read") {
			counts.WeighMyTruck, err = wmt.TerminatedMembershipCount(r.Context())
		}
		if err == nil && slices.Contains(session.User.Permissions, "tolls.read") {
			counts.Tolls, counts.TollsFrom, err = tolls.RecentUnlinkedCount(r.Context())
		}
		if err != nil {
			logger.Error("read sidebar counts failed", "error", err)
			writeAPIError(w, http.StatusInternalServerError, "Sidebar counts could not be loaded")
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, counts)
	})
}
