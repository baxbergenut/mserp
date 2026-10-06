package httpapi

import (
	"errors"
	"net/http"
	"net/mail"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/crypto/bcrypt"
	"mserp/internal/repository"
)

func validEmail(value string) bool {
	a, err := mail.ParseAddress(value)
	return err == nil && a.Address == value && len(value) <= 254 && !strings.ContainsAny(value, " \t\r\n") && strings.Contains(strings.SplitN(value, "@", 2)[1], ".")
}

func registerAccessRoutes(r chi.Router, h *authHandler, repo *repository.AuthRepository) {
	fail := func(w http.ResponseWriter, err error) {
		var pgerr *pgconn.PgError
		if errors.Is(err, repository.ErrAccessConflict) || errors.Is(err, pgx.ErrNoRows) || (errors.As(err, &pgerr) && (pgerr.Code == "23505" || pgerr.Code == "23503")) {
			writeAPIError(w, http.StatusConflict, repository.ErrAccessConflict.Error())
			return
		}
		h.logger.Error("access administration failed", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "access settings could not be saved")
	}
	r.Get("/settings/access", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		data, err := repo.AccessData(r.Context())
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, http.StatusOK, data)
	})
	r.Get("/tasks/users", func(w http.ResponseWriter, r *http.Request) {
		users, err := repo.TaskUsers(r.Context())
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, http.StatusOK, users)
	})
	r.Get("/settings/system-tasks", func(w http.ResponseWriter, r *http.Request) {
		data, err := repo.SystemTaskAssignments(r.Context())
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, http.StatusOK, data)
	})
	r.Put("/settings/system-tasks/{kind}", func(w http.ResponseWriter, r *http.Request) {
		var input repository.SystemTaskAssignment
		if err := decodeJSON(r, &input); err != nil {
			writeAPIError(w, 400, "Provide an assignee and version")
			return
		}
		input.Kind = chi.URLParam(r, "kind")
		if (input.Kind != "driver_onboarding" && input.Kind != "driver_offboarding" && input.Kind != "relay_review") || input.Version < 1 || (input.AssigneeID != nil && !isUUID(*input.AssigneeID)) {
			writeAPIError(w, 400, "Provide a valid system task, assignee and version")
			return
		}
		session, _ := authSessionFromContext(r.Context())
		if err := repo.SaveSystemTaskAssignment(r.Context(), session.User.ID, input); err != nil {
			fail(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	saveUser := func(w http.ResponseWriter, r *http.Request) {
		var in repository.ManagedUser
		if err := decodeJSON(r, &in); err != nil {
			writeAPIError(w, http.StatusBadRequest, err.Error())
			return
		}
		in.ID = chi.URLParam(r, "id")
		in.Username = strings.TrimSpace(in.Username)
		in.Email = strings.ToLower(strings.TrimSpace(in.Email))
		if in.Username == "" || len(in.Username) > 200 || !validEmail(in.Email) || !isUUID(in.RoleID) || (in.ID != "" && !isUUID(in.ID)) {
			writeAPIError(w, http.StatusBadRequest, "name, valid email and role are required")
			return
		}
		hash := ""
		if in.ID == "" || in.Password != "" {
			if len(in.Password) < 12 || len(in.Password) > 72 {
				writeAPIError(w, http.StatusBadRequest, "password must contain 12–72 bytes")
				return
			}
			value, err := bcrypt.GenerateFromPassword([]byte(in.Password), 12)
			if err != nil {
				fail(w, err)
				return
			}
			hash = string(value)
		}
		in.Password = "" // Never put plaintext passwords into audit records.
		session, _ := authSessionFromContext(r.Context())
		id, err := repo.SaveUser(r.Context(), session.User.ID, in, hash)
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"id": id})
	}
	r.Post("/settings/users", saveUser)
	r.Put("/settings/users/{id}", saveUser)
	saveRole := func(w http.ResponseWriter, r *http.Request) {
		var in repository.AccessRole
		if err := decodeJSON(r, &in); err != nil {
			writeAPIError(w, http.StatusBadRequest, err.Error())
			return
		}
		in.ID = chi.URLParam(r, "id")
		in.Name = strings.TrimSpace(in.Name)
		if in.Name == "" || len(in.Name) > 80 || !repository.ValidPermissions(in.Permissions) || (in.ID != "" && !isUUID(in.ID)) {
			writeAPIError(w, http.StatusBadRequest, "valid name and permissions are required")
			return
		}
		session, _ := authSessionFromContext(r.Context())
		if err := repo.SaveRole(r.Context(), session.User.ID, in); err != nil {
			fail(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
	r.Post("/settings/roles", saveRole)
	r.Put("/settings/roles/{id}", saveRole)
	r.Post("/settings/users/{id}/revoke", func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		if !isUUID(id) {
			writeAPIError(w, http.StatusBadRequest, "invalid user")
			return
		}
		session, _ := authSessionFromContext(r.Context())
		if err := repo.RevokeAccess(r.Context(), session.User.ID, id); err != nil {
			fail(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}
