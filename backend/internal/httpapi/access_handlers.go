package httpapi

import (
	"errors"
	"net/http"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

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
		var invalid *repository.ChargeValidationError
		if errors.As(err, &invalid) {
			writeAPIError(w, 400, err.Error())
			return
		}
		var pgerr *pgconn.PgError
		if errors.Is(err, repository.ErrAccessConflict) || errors.Is(err, pgx.ErrNoRows) || (errors.As(err, &pgerr) && (pgerr.Code == "23505" || pgerr.Code == "23503" || pgerr.ConstraintName == "dispatcher_account_role")) {
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
		session, _ := authSessionFromContext(r.Context())
		if !session.User.Administrator {
			for i := range data.Users {
				data.Users[i].DateOfBirth, data.Users[i].Phone = nil, nil
			}
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
		if (input.Kind != "driver_onboarding" && input.Kind != "driver_offboarding" && input.Kind != "relay_review" && input.Kind != "escrow_release" && input.Kind != "relay_setup" && input.Kind != "weighmytruck_setup" && input.Kind != "datatruck_setup" && input.Kind != "five_eld_truck" && input.Kind != "maintenance_pm") || input.Version < 1 || (input.AssigneeID != nil && !isUUID(*input.AssigneeID)) {
			writeAPIError(w, 400, "Provide a valid system task, assignee and version")
			return
		}
		if len(input.AssigneeIDs) > 100 || len(input.AssigneeRoleIDs) > 100 {
			writeAPIError(w, 400, "Too many assignees")
			return
		}
		for _, id := range append(append([]string{}, input.AssigneeIDs...), input.AssigneeRoleIDs...) {
			if !isUUID(id) {
				writeAPIError(w, 400, "Invalid assignee")
				return
			}
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
		if err := normalizeUserProfile(&in, h.now()); err != nil {
			writeAPIError(w, http.StatusBadRequest, err.Error())
			return
		}
		session, _ := authSessionFromContext(r.Context())
		if !session.User.Administrator && (in.DateOfBirth != nil || in.Phone != nil) {
			writeAPIError(w, http.StatusForbidden, "Only Administrators can edit DOB and phone")
			return
		}
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
		for _, a := range in.ExpenseCategoryAccess {
			if !a.CanView && (a.CanCreate || a.CanEdit || a.CanDelete) {
				writeAPIError(w, 400, "Category actions require View access")
				return
			}
		}
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

func normalizeUserProfile(in *repository.ManagedUser, now time.Time) error {
	in.FirstName, in.LastName = strings.TrimSpace(in.FirstName), strings.TrimSpace(in.LastName)
	if utf8.RuneCountInString(in.FirstName) > 50 || utf8.RuneCountInString(in.LastName) > 50 || (in.FirstName == "") != (in.LastName == "") || (in.ID == "" && in.FirstName == "") {
		return errors.New("Provide both first and last name, up to 50 characters each")
	}
	if in.FirstName != "" {
		in.Username = in.FirstName + " " + in.LastName
	}
	if in.Phone != nil {
		phone := strings.TrimSpace(*in.Phone)
		if utf8.RuneCountInString(phone) > 40 || strings.ContainsAny(phone, "\r\n\t") {
			return errors.New("Provide a phone number up to 40 characters")
		}
		in.Phone = &phone
	}
	if in.DateOfBirth != nil && *in.DateOfBirth != "" {
		date, err := time.Parse("2006-01-02", *in.DateOfBirth)
		if err != nil || date.Year() < 1900 || date.Format("2006-01-02") > now.In(birthdayLocation).Format("2006-01-02") {
			return errors.New("Provide a valid DOB between 1900 and today")
		}
	}
	return nil
}
