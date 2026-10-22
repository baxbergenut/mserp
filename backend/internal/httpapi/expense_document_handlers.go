package httpapi

import (
	"mime"
	"net/http"
	"strconv"

	"mserp/internal/repository"
)

func (h expenseHandler) listDocuments(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	category, _, err := h.repo.ExpenseDocumentAccess(r.Context(), id)
	if err != nil {
		h.writeError(w, err)
		return
	}
	session, _ := authSessionFromContext(r.Context())
	if !hasExpenseCategoryAction(session.User.ExpenseCategoryAccess, category, "view") {
		writeAPIError(w, 403, "your role cannot view documents in this category")
		return
	}
	values, err := h.repo.ExpenseDocuments(r.Context(), id)
	if err != nil {
		h.writeError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	writeJSON(w, 200, values)
}

func (h expenseHandler) uploadDocument(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	existing, err := h.repo.GetExpense(r.Context(), id)
	if err != nil {
		h.writeError(w, err)
		return
	}
	session, _ := authSessionFromContext(r.Context())
	category := existing.CategoryID
	if !repository.CanEditExpenseRecord(session.User, category, existing.CreatorRoleID) {
		writeAPIError(w, 403, "your role cannot attach documents to this expense")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 22<<20)
	if err := r.ParseMultipartForm(2 << 20); err != nil {
		writeAPIError(w, 400, "invalid expense attachment")
		return
	}
	defer r.MultipartForm.RemoveAll()
	header, err := firstMultipartFile(r, "file")
	if err != nil {
		writeAPIError(w, 400, err.Error())
		return
	}
	data, err := readMultipartFile(header, 20<<20)
	if err != nil {
		writeAPIError(w, 400, "attachment must be nonempty and no larger than 20 MB")
		return
	}
	kind := detectUploadContentType(header.Header.Get("Content-Type"), data)
	if !isSupportedDocumentType(kind) && kind != "text/plain" {
		writeAPIError(w, 400, "use PDF, TXT, PNG, JPEG or WEBP")
		return
	}
	value, err := h.repo.CreateExpenseDocument(r.Context(), id, category, safeUploadFileName(header.Filename, "expense-document"), kind, session.User.Username, data)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, 200, value)
}

func (h expenseHandler) downloadDocument(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	category, err := h.repo.ExpenseDocumentCategory(r.Context(), id)
	if err != nil {
		h.writeError(w, err)
		return
	}
	session, _ := authSessionFromContext(r.Context())
	if !hasExpenseCategoryAction(session.User.ExpenseCategoryAccess, category, "view") {
		writeAPIError(w, 403, "your role cannot view documents in this category")
		return
	}
	file, data, err := h.repo.ExpenseDocument(r.Context(), id, category)
	if err != nil {
		h.writeError(w, err)
		return
	}
	w.Header().Set("Content-Type", file.ContentType)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": file.FileName}))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(200)
	_, _ = w.Write(data)
}
