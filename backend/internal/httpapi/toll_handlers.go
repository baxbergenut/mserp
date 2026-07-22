package httpapi

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	"mserp/internal/repository"
)

const maxTollCSVBytes = 10 << 20

type tollHandler struct {
	logger *slog.Logger
	repo   *repository.TollRepository
}

func registerTollRoutes(r chi.Router, logger *slog.Logger, repo *repository.TollRepository) {
	handler := tollHandler{logger: logger, repo: repo}
	r.Get("/tolls", handler.listTolls)
	r.Post("/toll-reports", handler.uploadReport)
}

func (handler tollHandler) listTolls(w http.ResponseWriter, r *http.Request) {
	if wantsPagination(r) {
		pagination, err := parsePagination(r)
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, err.Error())
			return
		}
		postFrom, err := parseOptionalDate(r.URL.Query().Get("postFrom"), "postFrom")
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, err.Error())
			return
		}
		postTo, err := parseOptionalDate(r.URL.Query().Get("postTo"), "postTo")
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, err.Error())
			return
		}
		value, err := handler.repo.ListTollsPage(r.Context(), repository.TollPageQuery{
			Pagination: pagination, Search: strings.TrimSpace(r.URL.Query().Get("search")),
			Unit: r.URL.Query().Get("unit"), Agency: r.URL.Query().Get("agency"),
			PostFrom: postFrom, PostTo: postTo,
		})
		if err != nil {
			handler.logger.Error("list paginated tolls failed", "error", err)
			writeAPIError(w, http.StatusInternalServerError, "the tolls could not be loaded")
			return
		}
		writeJSON(w, http.StatusOK, value)
		return
	}
	values, err := handler.repo.ListTolls(r.Context())
	if err != nil {
		handler.logger.Error("list tolls failed", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "the tolls could not be loaded")
		return
	}
	writeJSON(w, http.StatusOK, values)
}

func (handler tollHandler) uploadReport(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxTollCSVBytes+(1<<20))
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			writeAPIError(w, http.StatusRequestEntityTooLarge, "the CSV file must be 10 MB or smaller")
			return
		}
		writeAPIError(w, http.StatusBadRequest, "invalid report upload: "+err.Error())
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "a CSV report file is required")
		return
	}
	defer file.Close()

	fileName := filepath.Base(strings.TrimSpace(header.Filename))
	if !strings.EqualFold(filepath.Ext(fileName), ".csv") {
		writeAPIError(w, http.StatusBadRequest, "the report must be a .csv file")
		return
	}
	content, err := io.ReadAll(io.LimitReader(file, maxTollCSVBytes+1))
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "the CSV file could not be read")
		return
	}
	if len(content) > maxTollCSVBytes {
		writeAPIError(w, http.StatusRequestEntityTooLarge, "the CSV file must be 10 MB or smaller")
		return
	}
	if !utf8.Valid(content) {
		writeAPIError(w, http.StatusBadRequest, "the CSV file must use UTF-8 text encoding")
		return
	}

	rows, totalAmountCents, err := parseTollCSV(bytes.NewReader(content))
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	fileHash := sha256.Sum256(content)
	result, err := handler.repo.ImportTolls(
		r.Context(), fileName, fmt.Sprintf("%x", fileHash), rows, totalAmountCents,
	)
	if err != nil {
		handler.logger.Error("import toll report failed", "file", fileName, "error", err)
		writeAPIError(w, http.StatusInternalServerError, "the toll report could not be imported")
		return
	}
	writeJSON(w, http.StatusCreated, result)
}
