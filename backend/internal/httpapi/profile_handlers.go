package httpapi

import (
	"net/http"
	"strings"
	"unicode/utf8"
)

func (h fleetHandler) profileNotes(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	truck := strings.HasPrefix(r.URL.Path, "/trucks/")
	if r.Method == http.MethodGet {
		rows, err := h.repo.ProfileNotes(r.Context(), id, truck)
		if err != nil {
			h.writeError(w, err)
			return
		}
		writeJSON(w, 200, rows)
		return
	}
	var v struct {
		ID   string `json:"id"`
		Body string `json:"body"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 32*1024)
	if err := decodeJSON(r, &v); err != nil {
		writeAPIError(w, 400, "Invalid note")
		return
	}
	v.Body = strings.TrimSpace(v.Body)
	if !isUUID(v.ID) || v.Body == "" || utf8.RuneCountInString(v.Body) > 5000 || strings.ContainsRune(v.Body, 0) {
		writeAPIError(w, 400, "A note requires an ID and 1–5000 characters")
		return
	}
	session, _ := authSessionFromContext(r.Context())
	note, err := h.repo.AddProfileNote(r.Context(), id, truck, v.ID, v.Body, session.User.ID)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, note)
}

func (h fleetHandler) getTruckAssignments(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	rows, err := h.repo.TruckAssignmentHistory(r.Context(), id)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, 200, rows)
}
func (h fleetHandler) getInvestor(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	v, err := h.repo.GetInvestor(r.Context(), id)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, 200, v)
}
func (h fleetHandler) getDispatcher(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	v, err := h.repo.GetDispatcher(r.Context(), id)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, 200, v)
}
