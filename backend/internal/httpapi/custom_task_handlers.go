package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"mserp/internal/repository"
)

type customTaskStore interface {
	List(context.Context, repository.Pagination, string, string) (repository.Page[repository.CustomTask], error)
	Create(context.Context, repository.CustomTaskInput, string) (repository.CustomTask, error)
	Update(context.Context, string, repository.CustomTaskInput) (repository.CustomTask, error)
	SetCompleted(context.Context, string, bool) (repository.CustomTask, error)
	Delete(context.Context, string) error
}

func registerCustomTaskRoutes(r chi.Router, logger *slog.Logger, repo customTaskStore) {
	failure := func(w http.ResponseWriter, err error) {
		if errors.Is(err, repository.ErrNotFound) {
			writeAPIError(w, 404, "This task no longer exists")
			return
		}
		logger.Error("custom task operation failed", "error", err)
		writeAPIError(w, 500, "The custom task could not be loaded or saved")
	}
	r.Get("/tasks/custom", func(w http.ResponseWriter, r *http.Request) {
		pagination, err := parsePagination(r)
		if err != nil {
			writeAPIError(w, 400, err.Error())
			return
		}
		status := r.URL.Query().Get("status")
		if status == "" {
			status = "open"
		}
		if status != "open" && status != "completed" && status != "all" {
			writeAPIError(w, 400, "status must be open, completed or all")
			return
		}
		result, err := repo.List(r.Context(), pagination, strings.TrimSpace(r.URL.Query().Get("search")), status)
		if err != nil {
			failure(w, err)
			return
		}
		writeJSON(w, 200, result)
	})
	readInput := func(w http.ResponseWriter, r *http.Request) (repository.CustomTaskInput, bool) {
		var input repository.CustomTaskInput
		r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
		if err := decodeJSON(r, &input); err != nil {
			writeAPIError(w, 400, "Provide a task title and optional notes")
			return input, false
		}
		input.Title = strings.TrimSpace(input.Title)
		input.Notes = strings.TrimSpace(input.Notes)
		if input.AssignedTo != nil && *input.AssignedTo != "" && !isUUID(*input.AssignedTo) {
			writeAPIError(w, 400, "Select a valid assignee")
			return input, false
		}
		if input.Title == "" || utf8.RuneCountInString(input.Title) > 200 || utf8.RuneCountInString(input.Notes) > 5000 || strings.ContainsRune(input.Title+input.Notes, '\x00') {
			writeAPIError(w, 400, "Title must be 1–200 characters and notes at most 5,000 characters, without null characters")
			return input, false
		}
		return input, true
	}
	r.Post("/tasks/custom", func(w http.ResponseWriter, r *http.Request) {
		session, ok := authSessionFromContext(r.Context())
		if !ok {
			writeAPIError(w, 401, "Sign in to create a task")
			return
		}
		input, ok := readInput(w, r)
		if !ok {
			return
		}
		task, err := repo.Create(r.Context(), input, session.User.ID)
		if err != nil {
			failure(w, err)
			return
		}
		writeJSON(w, 201, task)
	})
	r.Route("/tasks/custom/{id}", func(r chi.Router) {
		r.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !isUUID(chi.URLParam(r, "id")) {
					writeAPIError(w, 400, "Provide a valid task ID")
					return
				}
				next.ServeHTTP(w, r)
			})
		})
		r.Put("/", func(w http.ResponseWriter, r *http.Request) {
			input, ok := readInput(w, r)
			if !ok {
				return
			}
			task, err := repo.Update(r.Context(), chi.URLParam(r, "id"), input)
			if err != nil {
				failure(w, err)
				return
			}
			writeJSON(w, 200, task)
		})
		r.Patch("/", func(w http.ResponseWriter, r *http.Request) {
			var input struct {
				Completed *bool `json:"completed"`
			}
			r.Body = http.MaxBytesReader(w, r.Body, 1024)
			if err := decodeJSON(r, &input); err != nil || input.Completed == nil {
				writeAPIError(w, 400, "Provide completed as true or false")
				return
			}
			task, err := repo.SetCompleted(r.Context(), chi.URLParam(r, "id"), *input.Completed)
			if err != nil {
				failure(w, err)
				return
			}
			writeJSON(w, 200, task)
		})
		r.Delete("/", func(w http.ResponseWriter, r *http.Request) {
			if err := repo.Delete(r.Context(), chi.URLParam(r, "id")); err != nil {
				failure(w, err)
				return
			}
			w.WriteHeader(204)
		})
	})
}
