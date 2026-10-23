package httpapi

import (
	"golang.org/x/crypto/bcrypt"
	"mserp/internal/repository"
	"net/http"
	"strings"
)

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func (h fleetHandler) prepareDispatcherAccount(w http.ResponseWriter, r *http.Request, request dispatcherRequest, input *repository.DispatcherInput) bool {
	session, _ := authSessionFromContext(r.Context())
	if !repository.HasPermission(session.User, "access.manage") {
		writeAPIError(w, 403, "Creating a dispatcher login requires user-management permission")
		return false
	}
	email := strings.ToLower(strings.TrimSpace(request.Email))
	if !validEmail(email) || len(request.Password) < 12 || len(request.Password) > 72 {
		writeAPIError(w, 400, "A valid login email and password of 12–72 bytes are required")
		return false
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(request.Password), 12)
	if err != nil {
		h.writeError(w, err)
		return false
	}
	input.Email = &email
	input.AccountPasswordHash = string(hash)
	input.AccountActor = session.User.ID
	return true
}
