package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"mserp/internal/repository"
)

func expenseVerificationRole(user repository.AuthUser) string {
	if user.Administrator {
		return "admin"
	}
	if strings.EqualFold(strings.TrimSpace(user.RoleName), "Accountant") {
		return "accountant"
	}
	return ""
}

func (handler expenseHandler) verifyExpense(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	session, _ := authSessionFromContext(r.Context())
	role := expenseVerificationRole(session.User)
	if role == "" {
		writeAPIError(w, http.StatusForbidden, "only Accountants and Administrators can verify expenses")
		return
	}
	var request struct {
		UpdatedAt time.Time `json:"updatedAt"`
	}
	if err := decodeJSON(r, &request); err != nil || request.UpdatedAt.IsZero() {
		writeAPIError(w, http.StatusBadRequest, "the displayed expense version is required")
		return
	}
	existing, err := handler.repo.GetExpense(r.Context(), id)
	if err != nil {
		handler.writeError(w, err)
		return
	}
	if !hasExpenseCategoryAction(session.User.ExpenseCategoryAccess, existing.CategoryID, "view") {
		writeAPIError(w, http.StatusForbidden, "you do not have permission to verify entries in that category")
		return
	}
	value, err := handler.repo.VerifyExpense(r.Context(), id, existing.CategoryID, role, request.UpdatedAt, session.User)
	if errors.Is(err, repository.ErrAccessConflict) {
		writeAPIError(w, http.StatusConflict, "This entry changed. Refresh and review it before verifying.")
		return
	}
	if err != nil {
		handler.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func (handler expenseHandler) unverifyExpense(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var request struct {
		VerificationID string `json:"verificationId"`
	}
	if err := decodeJSON(r, &request); err != nil || !isUUID(request.VerificationID) {
		writeAPIError(w, http.StatusBadRequest, "the verification ID is required")
		return
	}
	session, _ := authSessionFromContext(r.Context())
	existing, err := handler.repo.GetExpense(r.Context(), id)
	if err != nil {
		handler.writeError(w, err)
		return
	}
	if !hasExpenseCategoryAction(session.User.ExpenseCategoryAccess, existing.CategoryID, "view") {
		writeAPIError(w, http.StatusForbidden, "you do not have permission to view entries in that category")
		return
	}
	value, err := handler.repo.UnverifyExpense(r.Context(), id, existing.CategoryID, request.VerificationID, session.User)
	if errors.Is(err, repository.ErrExpenseVerificationForbidden) {
		writeAPIError(w, http.StatusForbidden, err.Error())
		return
	}
	if errors.Is(err, repository.ErrAccessConflict) {
		writeAPIError(w, http.StatusConflict, "This verification changed or was already removed. Refresh to see the latest checks.")
		return
	}
	if err != nil {
		handler.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
}
