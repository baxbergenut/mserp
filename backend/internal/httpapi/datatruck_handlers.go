package httpapi

import (
	"github.com/go-chi/chi/v5"
	"mserp/internal/repository"
	"net/http"
)

func registerTruckDataTruckRoutes(router chi.Router, fleet *repository.FleetRepository) {
	router.Get("/trucks/{id}/datatruck", func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		if _, err := fleet.GetTruck(r.Context(), id); err != nil {
			writeAPIError(w, 404, "Truck not found.")
			return
		}
		repo := fleet.DataTruckServices()
		if repo == nil {
			writeJSON(w, 200, repository.DataTruckEnrollment{State: "not_requested"})
			return
		}
		out, err := repo.Status(r.Context(), "trucks", id)
		if err != nil {
			writeAPIError(w, 500, "Could not read DataTruck enrollment.")
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, 200, out)
	})
	router.Post("/trucks/{id}/datatruck", func(w http.ResponseWriter, r *http.Request) {
		if !requireCapabilities(w, r, "trucks.create") {
			return
		}
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		if _, err := fleet.GetTruck(r.Context(), id); err != nil {
			writeAPIError(w, 404, "Truck not found.")
			return
		}
		var in struct {
			Version int `json:"version"`
		}
		if decodeJSON(r, &in) != nil {
			writeAPIError(w, 400, "Invalid enrollment request.")
			return
		}
		repo := fleet.DataTruckServices()
		if repo == nil {
			writeAPIError(w, 409, "DataTruck enrollment is not configured.")
			return
		}
		if err := repo.RetryTruck(r.Context(), id, in.Version); err != nil {
			writeAPIError(w, 409, "DataTruck setup changed or requires provider review. Refresh before retrying.")
			return
		}
		writeJSON(w, 200, map[string]bool{"saved": true})
	})
}
