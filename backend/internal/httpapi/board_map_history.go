package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"mserp/internal/fiveeld"
	"mserp/internal/repository"
)

type boardTrackingHistory func(context.Context, string, time.Time) ([]fiveeld.TrackingPoint, error)

type boardHistoryPoint struct {
	Latitude   float64   `json:"latitude"`
	Longitude  float64   `json:"longitude"`
	ReportedAt time.Time `json:"reportedAt"`
}

func registerBoardMapHistory(r chi.Router, logger *slog.Logger, repo *repository.DriverBoardRepository, history boardTrackingHistory) {
	r.Get("/driver-board/loads/{driverId}/map-history", func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "driverId")
		if !isUUID(id) {
			writeAPIError(w, http.StatusBadRequest, "valid driver id is required")
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		result := struct {
			TruckID string              `json:"truckId"`
			Points  []boardHistoryPoint `json:"points"`
		}{Points: []boardHistoryPoint{}}
		if history != nil {
			truckID, vin, err := repo.MapTruckIdentity(r.Context(), id)
			if err != nil {
				logger.Error("map truck identity lookup failed", "error", err)
				writeAPIError(w, http.StatusInternalServerError, "truck history could not be loaded")
				return
			}
			result.TruckID = truckID
			if vin != "" {
				ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
				defer cancel()
				points, err := history(ctx, vin, time.Now().UTC())
				if err != nil {
					writeAPIError(w, http.StatusBadGateway, "Five ELD truck history is unavailable")
					return
				}
				for _, point := range points {
					result.Points = append(result.Points, boardHistoryPoint{
						Latitude: point.Coordinates.Lat, Longitude: point.Coordinates.Lng, ReportedAt: point.Date,
					})
				}
			}
		}
		writeJSON(w, http.StatusOK, result)
	})
}
