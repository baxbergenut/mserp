package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"mime"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"mserp/internal/repository"
)

type feedbackHandler struct {
	repo   *repository.FeedbackRepository
	logger *slog.Logger
}

func registerFeedbackRoutes(r chi.Router, logger *slog.Logger, repo *repository.FeedbackRepository) {
	h := feedbackHandler{repo, logger}
	r.Get("/feedback", h.list)
	r.Post("/feedback", h.create)
	r.Get("/feedback/tags", h.tags)
	r.Get("/feedback/attachments/{id}", h.attachment)
	r.Get("/feedback/{id}", h.detail)
	r.Put("/feedback/{id}", h.update)
	r.Put("/feedback/{id}/vote", h.vote)
	r.Post("/feedback/{id}/comments", h.comment)
}
func validFeedbackStatus(s string) bool {
	return slices.Contains([]string{"open", "in_progress", "completed", "discarded"}, s)
}
func normalizeFeedback(in *repository.FeedbackInput) error {
	in.Title = strings.TrimSpace(in.Title)
	in.Body = strings.TrimSpace(in.Body)
	if in.Title == "" || utf8.RuneCountInString(in.Title) > 200 || in.Body == "" || utf8.RuneCountInString(in.Body) > 10000 || strings.ContainsRune(in.Title+in.Body, '\x00') {
		return errors.New("add a title (up to 200 characters) and details (up to 10,000 characters)")
	}
	if len(in.Tags) > 8 {
		return errors.New("choose up to 8 tags")
	}
	tags := []string{}
	for _, tag := range in.Tags {
		tag = strings.ToLower(strings.TrimSpace(tag))
		if tag == "" || utf8.RuneCountInString(tag) > 24 || strings.IndexFunc(tag, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '-' && r != ' ' }) >= 0 {
			return errors.New("tags must contain 1–24 letters, numbers, spaces or hyphens")
		}
		if !slices.Contains(tags, tag) {
			tags = append(tags, tag)
		}
	}
	slices.Sort(tags)
	in.Tags = tags
	return nil
}
func (h feedbackHandler) fail(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, repository.ErrNotFound):
		writeAPIError(w, 404, "Feedback was not found")
	case errors.Is(err, repository.ErrFeedbackForbidden):
		writeAPIError(w, 403, err.Error())
	case errors.Is(err, repository.ErrFeedbackConflict):
		writeAPIError(w, 409, err.Error())
	default:
		h.logger.Error("feedback request failed", "error", err)
		writeAPIError(w, 500, "Feedback could not be saved or loaded. Please try again.")
	}
}
func feedbackID(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := chi.URLParam(r, "id")
	if !isUUID(id) {
		writeAPIError(w, 400, "Provide a valid feedback ID")
		return "", false
	}
	return id, true
}
func (h feedbackHandler) list(w http.ResponseWriter, r *http.Request) {
	p, err := parsePagination(r)
	if err != nil {
		writeAPIError(w, 400, err.Error())
		return
	}
	q := r.URL.Query()
	status := q.Get("status")
	sort := q.Get("sort")
	if (status != "" && !validFeedbackStatus(status)) || (sort != "" && sort != "newest" && sort != "top") || len(q.Get("search")) > 500 || len(q.Get("tag")) > 96 {
		writeAPIError(w, 400, "Choose a valid status, tag, search or sort")
		return
	}
	session, _ := authSessionFromContext(r.Context())
	result, err := h.repo.List(r.Context(), session.User.ID, repository.FeedbackQuery{Pagination: p, Search: q.Get("search"), Status: status, Tag: strings.ToLower(q.Get("tag")), Sort: sort})
	if err != nil {
		h.fail(w, err)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	writeJSON(w, 200, result)
}
func (h feedbackHandler) detail(w http.ResponseWriter, r *http.Request) {
	id, ok := feedbackID(w, r)
	if !ok {
		return
	}
	session, _ := authSessionFromContext(r.Context())
	result, err := h.repo.Detail(r.Context(), id, session.User.ID)
	if err != nil {
		h.fail(w, err)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	writeJSON(w, 200, result)
}
func (h feedbackHandler) tags(w http.ResponseWriter, r *http.Request) {
	result, err := h.repo.Tags(r.Context())
	if err != nil {
		h.fail(w, err)
		return
	}
	for _, tag := range []string{"bug", "feature", "ui", "ux", "improvement", "question"} {
		if !slices.Contains(result, tag) {
			result = append(result, tag)
		}
	}
	slices.Sort(result)
	writeJSON(w, 200, result)
}
func (h feedbackHandler) create(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 26<<20)
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		writeAPIError(w, 400, "Use at most 5 files, 10 MB each and 20 MB total")
		return
	}
	defer r.MultipartForm.RemoveAll()
	var in repository.FeedbackInput
	if err := json.Unmarshal([]byte(r.FormValue("post")), &in); err != nil || !isUUID(in.ID) {
		writeAPIError(w, 400, "Provide a valid feedback submission")
		return
	}
	if err := normalizeFeedback(&in); err != nil {
		writeAPIError(w, 400, err.Error())
		return
	}
	if in.Status != "" && in.Status != "open" {
		writeAPIError(w, 400, "New feedback starts as Open")
		return
	}
	headers := r.MultipartForm.File["files"]
	if len(headers) > 5 {
		writeAPIError(w, 400, "Attach up to 5 files")
		return
	}
	files := []repository.FeedbackAttachment{}
	total := 0
	// The digest binds retries to all normalized content and exact attachment bytes.
	digest := sha256.New()
	normalized, _ := json.Marshal(struct {
		Title, Body string
		Tags        []string
	}{in.Title, in.Body, in.Tags})
	digest.Write(normalized)
	for _, header := range headers {
		data, err := readMultipartFile(header, 10<<20)
		if err != nil {
			writeAPIError(w, 400, "Each file must be nonempty and no larger than 10 MB")
			return
		}
		total += len(data)
		if total > 20<<20 {
			writeAPIError(w, 400, "Attachments must total 20 MB or less")
			return
		}
		name := safeUploadFileName(header.Filename, "attachment")
		kind := http.DetectContentType(data)
		files = append(files, repository.FeedbackAttachment{FileName: name, ContentType: kind, Data: data})
		metadata, _ := json.Marshal([]any{name, kind, len(data)})
		digest.Write(metadata)
		digest.Write(data)
	}
	session, _ := authSessionFromContext(r.Context())
	id, err := h.repo.Create(r.Context(), in, session.User, hex.EncodeToString(digest.Sum(nil)), files)
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, 201, map[string]string{"id": id})
}
func (h feedbackHandler) update(w http.ResponseWriter, r *http.Request) {
	id, ok := feedbackID(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	var in repository.FeedbackInput
	if err := decodeJSON(r, &in); err != nil || !validFeedbackStatus(in.Status) || in.Version < 1 {
		writeAPIError(w, 400, "Provide feedback content, a valid status and its current version")
		return
	}
	if err := normalizeFeedback(&in); err != nil {
		writeAPIError(w, 400, err.Error())
		return
	}
	session, _ := authSessionFromContext(r.Context())
	if err := h.repo.Update(r.Context(), id, in, session.User); err != nil {
		h.fail(w, err)
		return
	}
	w.WriteHeader(204)
}
func (h feedbackHandler) vote(w http.ResponseWriter, r *http.Request) {
	id, ok := feedbackID(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	var in struct {
		Voted *bool `json:"voted"`
	}
	if err := decodeJSON(r, &in); err != nil || in.Voted == nil {
		writeAPIError(w, 400, "Provide voted as true or false")
		return
	}
	session, _ := authSessionFromContext(r.Context())
	if err := h.repo.Vote(r.Context(), id, session.User.ID, *in.Voted); err != nil {
		h.fail(w, err)
		return
	}
	w.WriteHeader(204)
}
func (h feedbackHandler) comment(w http.ResponseWriter, r *http.Request) {
	id, ok := feedbackID(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 32<<10)
	var in struct {
		ID   string `json:"id"`
		Body string `json:"body"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeAPIError(w, 400, "Provide a comment")
		return
	}
	in.Body = strings.TrimSpace(in.Body)
	if !isUUID(in.ID) || in.Body == "" || utf8.RuneCountInString(in.Body) > 5000 || strings.ContainsRune(in.Body, '\x00') {
		writeAPIError(w, 400, "Add a comment of 1–5,000 characters")
		return
	}
	session, _ := authSessionFromContext(r.Context())
	if err := h.repo.Comment(r.Context(), id, in.ID, in.Body, session.User); err != nil {
		h.fail(w, err)
		return
	}
	w.WriteHeader(204)
}
func (h feedbackHandler) attachment(w http.ResponseWriter, r *http.Request) {
	id, ok := feedbackID(w, r)
	if !ok {
		return
	}
	a, err := h.repo.Attachment(r.Context(), id)
	if err != nil {
		h.fail(w, err)
		return
	}
	disposition := "attachment"
	if slices.Contains([]string{"image/png", "image/jpeg", "image/webp", "image/gif"}, a.ContentType) && r.URL.Query().Get("download") != "1" {
		disposition = "inline"
	}
	w.Header().Set("Content-Type", a.ContentType)
	w.Header().Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": a.FileName}))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'")
	w.Header().Set("Content-Length", strconv.Itoa(len(a.Data)))
	w.WriteHeader(200)
	_, _ = w.Write(a.Data)
}
