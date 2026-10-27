package httpapi

import (
	"errors"
	"github.com/go-chi/chi/v5"
	"mserp/internal/repository"
	"net/http"
)

func (h tollHandler) registerTruckLinks(r chi.Router) {
	fail := func(w http.ResponseWriter, err error) {
		var invalid *repository.TollLinkError
		if errors.As(err, &invalid) {
			writeAPIError(w, 409, err.Error())
			return
		}
		h.logger.Error("PrePass truck link failed", "error", err)
		writeAPIError(w, 500, "Could not update PrePass truck links.")
	}
	r.Get("/tolls/truck-links", func(w http.ResponseWriter, r *http.Request) {
		out, err := h.repo.PrePassTruckLinks(r.Context(), r.URL.Query().Get("from"), r.URL.Query().Get("includeReviewed") == "true")
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, out)
	})
	r.Post("/tolls/{id}/review", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ID       string `json:"id"`
			Revision string `json:"revision"`
			Version  string `json:"version"`
			Reviewed *bool  `json:"reviewed"`
		}
		id := chi.URLParam(r, "id")
		if !isUUID(id) || decodeJSON(r, &body) != nil || !isUUID(body.ID) || len(body.Revision) != 64 || body.Reviewed == nil || body.Version != "" && !isUUID(body.Version) {
			writeAPIError(w, 400, "Provide the current flagged transaction and review status.")
			return
		}
		session, _ := authSessionFromContext(r.Context())
		if err := h.repo.ReviewTollFlag(r.Context(), id, repository.TollFlagReviewInput{ID: body.ID, Revision: body.Revision, Version: body.Version, Reviewed: *body.Reviewed}, session.User.ID); err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, map[string]bool{"saved": true})
	})
	read := func(w http.ResponseWriter, r *http.Request) (repository.PrePassTruckLinkInput, bool) {
		var in repository.PrePassTruckLinkInput
		if decodeJSON(r, &in) != nil || !isUUID(in.TruckID) {
			writeAPIError(w, 400, "Select a truck and valid mapping details.")
			return in, false
		}
		return in, true
	}
	r.Post("/tolls/truck-links/preview", func(w http.ResponseWriter, r *http.Request) {
		in, ok := read(w, r)
		if !ok {
			return
		}
		out, err := h.repo.PreviewPrePassTruckLink(r.Context(), in)
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, out)
	})
	r.Post("/tolls/truck-links", func(w http.ResponseWriter, r *http.Request) {
		in, ok := read(w, r)
		if !ok {
			return
		}
		if !isUUID(in.ID) || len(in.Revision) != 64 {
			writeAPIError(w, 400, "Preview the mapping before saving.")
			return
		}
		session, _ := authSessionFromContext(r.Context())
		if err := h.repo.LinkPrePassTruck(r.Context(), in, session.User.ID); err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, map[string]bool{"saved": true})
	})
	r.Post("/tolls/truck-links/{id}/undo", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Version int `json:"version"`
		}
		id := chi.URLParam(r, "id")
		if !isUUID(id) || decodeJSON(r, &in) != nil || in.Version < 1 {
			writeAPIError(w, 400, "Provide the current mapping version.")
			return
		}
		session, _ := authSessionFromContext(r.Context())
		if err := h.repo.UndoPrePassTruckLink(r.Context(), id, in.Version, session.User.ID); err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, map[string]bool{"saved": true})
	})
}
