package httpapi

import (
	"github.com/go-chi/chi/v5"
	"mserp/internal/repository"
	"net/http"
	"slices"
)

func (h escrowHandler) taskDetail(w http.ResponseWriter, r *http.Request) {
	session, _ := authSessionFromContext(r.Context())
	if !slices.Contains(session.User.Permissions, "escrow.read") {
		writeAPIError(w, 403, "Escrow read access is required")
		return
	}
	if !isUUID(chi.URLParam(r, "id")) {
		writeAPIError(w, 400, "Invalid task ID")
		return
	}
	result, err := h.repo.TaskDetail(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, 200, result)
}
func (h escrowHandler) completeTask(w http.ResponseWriter, r *http.Request) {
	session, _ := authSessionFromContext(r.Context())
	if !slices.Contains(session.User.Permissions, "escrow.write") {
		writeAPIError(w, 403, "Escrow write access is required")
		return
	}
	var input repository.EscrowTaskDecision
	r.Body = http.MaxBytesReader(w, r.Body, 32768)
	if !isUUID(chi.URLParam(r, "id")) {
		writeAPIError(w, 400, "Invalid task ID")
		return
	}
	if err := decodeJSON(r, &input); err != nil {
		writeAPIError(w, 400, "Invalid escrow decision")
		return
	}
	if err := h.repo.CompleteTask(r.Context(), chi.URLParam(r, "id"), session.User.ID, input); err != nil {
		h.writeError(w, err)
		return
	}
	w.WriteHeader(204)
}
func (h escrowHandler) saveOpening(w http.ResponseWriter, r *http.Request) {
	var input repository.EscrowOpeningInput
	if !isUUID(chi.URLParam(r, "id")) {
		writeAPIError(w, 400, "Invalid escrow ID")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	if err := decodeJSON(r, &input); err != nil {
		writeAPIError(w, 400, "Invalid opening balance")
		return
	}
	session, _ := authSessionFromContext(r.Context())
	if err := h.repo.SaveOpening(r.Context(), chi.URLParam(r, "id"), session.User.ID, input); err != nil {
		h.writeError(w, err)
		return
	}
	w.WriteHeader(204)
}
