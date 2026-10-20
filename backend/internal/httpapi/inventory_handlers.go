package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"mserp/internal/repository"
)

type inventoryHandler struct {
	repo   *repository.InventoryRepository
	logger *slog.Logger
}

func registerInventoryRoutes(router chi.Router, logger *slog.Logger, repo *repository.InventoryRepository) {
	h := inventoryHandler{repo, logger}
	router.Get("/inventory", h.list)
	router.Put("/inventory/{collection}/{id}", h.save)
	router.Get("/inventory/history", h.history)
	router.Post("/inventory/movements", h.move)
	router.Get("/trailers", h.trailers)
	router.Put("/trailers/{id}", h.saveTrailer)
	router.Get("/trailers/{id}/history", h.trailerHistory)
	router.Post("/inventory/imports/preview", h.previewImport)
	router.Get("/inventory/imports", h.listImports)
	router.Put("/inventory/imports/{id}", h.reviewImport)
	router.Post("/inventory/imports/{id}/commit", h.commitImport)
}
func inventoryActor(r *http.Request) repository.InventoryActor {
	session, _ := authSessionFromContext(r.Context())
	return repository.InventoryActor{ID: session.User.ID, Name: session.User.Username}
}
func (h inventoryHandler) fail(w http.ResponseWriter, err error) {
	var invalid *repository.InventoryValidationError
	var database *pgconn.PgError
	switch {
	case errors.As(err, &invalid):
		writeAPIError(w, 400, err.Error())
	case errors.Is(err, repository.ErrInventoryConflict):
		writeAPIError(w, 409, err.Error())
	case errors.Is(err, pgx.ErrNoRows):
		writeAPIError(w, 404, "Inventory record not found")
	case errors.As(err, &database) && (database.Code == "23505" || database.Code == "23503" || database.Code == "23514"):
		writeAPIError(w, 409, "A linked record, unique identifier, or assignment conflicts with existing inventory. Reload and review.")
	default:
		h.logger.Error("inventory request failed", "error", err)
		writeAPIError(w, 500, "Inventory could not be loaded or saved")
	}
}
func (h inventoryHandler) list(w http.ResponseWriter, r *http.Request) {
	result := map[string]any{}
	collections := []string{"assets", "lines", "locations", "supplies", "equipment"}
	session, _ := authSessionFromContext(r.Context())
	if repository.HasPermission(session.User, "tolls.read") {
		collections = append(collections, "transponder-notes")
	}
	for _, collection := range collections {
		records, err := h.repo.List(r.Context(), collection)
		if err != nil {
			h.fail(w, err)
			return
		}
		result[collection] = records
	}
	balances, err := h.repo.Stock(r.Context())
	if err != nil {
		h.fail(w, err)
		return
	}
	result["balances"] = balances
	movements, err := h.repo.Movements(r.Context())
	if err != nil {
		h.fail(w, err)
		return
	}
	result["movements"] = movements
	writeJSON(w, 200, result)
}
func (h inventoryHandler) save(w http.ResponseWriter, r *http.Request) {
	collection := chi.URLParam(r, "collection")
	if collection == "trailers" {
		permissionDenied(w)
		return
	}
	if collection == "transponder-notes" && !requireCapabilities(w, r, "tolls.read") {
		return
	}
	h.saveCollection(w, r, collection)
}
func (h inventoryHandler) saveTrailer(w http.ResponseWriter, r *http.Request) {
	h.saveCollection(w, r, "trailers")
}
func (h inventoryHandler) saveCollection(w http.ResponseWriter, r *http.Request, collection string) {
	var input repository.InventorySave
	if err := decodeJSON(r, &input); err != nil || input.Record.ID != chi.URLParam(r, "id") {
		writeAPIError(w, 400, "Invalid inventory request")
		return
	}
	if input.Record.Data.TruckID != "" && !requireCapabilities(w, r, "trucks.read") {
		return
	}
	saved, err := h.repo.Save(r.Context(), collection, input, inventoryActor(r))
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, 200, saved)
}
func (h inventoryHandler) trailers(w http.ResponseWriter, r *http.Request) {
	records, err := h.repo.List(r.Context(), "trailers")
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, 200, records)
}
func (h inventoryHandler) history(w http.ResponseWriter, r *http.Request) {
	collection := r.URL.Query().Get("collection")
	if collection == "trailers" {
		permissionDenied(w)
		return
	}
	if collection == "transponder-notes" && !requireCapabilities(w, r, "tolls.read") {
		return
	}
	events, err := h.repo.History(r.Context(), collection, r.URL.Query().Get("id"))
	if err != nil {
		h.fail(w, err)
		return
	}
	// A combined history must not reveal trailers or restricted provider notes.
	filtered := []repository.InventoryEvent{}
	session, _ := authSessionFromContext(r.Context())
	for _, event := range events {
		if event.Collection != "trailers" && (event.Collection != "transponder-notes" || repository.HasPermission(session.User, "tolls.read")) {
			filtered = append(filtered, event)
		}
	}
	writeJSON(w, 200, filtered)
}
func (h inventoryHandler) trailerHistory(w http.ResponseWriter, r *http.Request) {
	events, err := h.repo.History(r.Context(), "trailers", chi.URLParam(r, "id"))
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, 200, events)
}
func (h inventoryHandler) move(w http.ResponseWriter, r *http.Request) {
	var input repository.InventoryMovement
	if err := decodeJSON(r, &input); err != nil {
		writeAPIError(w, 400, "Invalid stock movement")
		return
	}
	// Truck identities are permission protected even when supplied as ledger holders.
	if (len(input.From) > 6 && input.From[:6] == "truck:" || len(input.To) > 6 && input.To[:6] == "truck:") && !requireCapabilities(w, r, "trucks.read") {
		return
	}
	if err := h.repo.Move(r.Context(), input, inventoryActor(r)); err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"saved": true})
}
func inventoryImportAllowed(w http.ResponseWriter, r *http.Request) bool {
	session, ok := authSessionFromContext(r.Context())
	if !ok || !session.User.Administrator {
		permissionDenied(w)
		return false
	}
	return requireCapabilities(w, r, "inventory.import")
}
func decodeInventoryImport(w http.ResponseWriter, r *http.Request, destination any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 10<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("expected one JSON object")
	}
	return nil
}
func (h inventoryHandler) previewImport(w http.ResponseWriter, r *http.Request) {
	if !inventoryImportAllowed(w, r) {
		return
	}
	var input repository.InventorySnapshot
	if err := decodeInventoryImport(w, r, &input); err != nil {
		writeAPIError(w, 400, "Upload a valid fleet snapshot JSON file (maximum 10 MB)")
		return
	}
	result, err := h.repo.PreviewImport(r.Context(), input, inventoryActor(r))
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, 200, result)
}
func (h inventoryHandler) listImports(w http.ResponseWriter, r *http.Request) {
	if !inventoryImportAllowed(w, r) {
		return
	}
	result, err := h.repo.ListImports(r.Context())
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, 200, result)
}
func (h inventoryHandler) reviewImport(w http.ResponseWriter, r *http.Request) {
	if !inventoryImportAllowed(w, r) {
		return
	}
	var input repository.InventoryImportReview
	if err := decodeInventoryImport(w, r, &input); err != nil {
		writeAPIError(w, 400, "Invalid import review")
		return
	}
	result, err := h.repo.ReviewImport(r.Context(), chi.URLParam(r, "id"), input)
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, 200, result)
}
func (h inventoryHandler) commitImport(w http.ResponseWriter, r *http.Request) {
	if !inventoryImportAllowed(w, r) {
		return
	}
	var input struct {
		Version int64 `json:"version"`
	}
	if err := decodeJSON(r, &input); err != nil {
		writeAPIError(w, 400, "Invalid import version")
		return
	}
	result, err := h.repo.CommitImport(r.Context(), chi.URLParam(r, "id"), input.Version, inventoryActor(r))
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, 200, result)
}
