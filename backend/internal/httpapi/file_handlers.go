package httpapi

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"mserp/internal/groq"
	"mserp/internal/repository"
)

const (
	maxStoredFileSize    = 10 << 20
	maxExtractionImage   = 6 << 20
	maxExtractionPayload = 15 << 20
	maxMultipartBody     = 30 << 20
)

type fileHandler struct {
	logger    *slog.Logger
	repo      *repository.FileRepository
	extractor groq.DocumentExtractor
}

type irpUploadResponse struct {
	File   repository.StoredFileMetadata `json:"file"`
	Fields groq.CabCardFields            `json:"fields"`
}

type cdlUploadResponse struct {
	File   repository.StoredFileMetadata `json:"file"`
	Fields groq.CDLFields                `json:"fields"`
}

type uploadedDocument struct {
	fileName    string
	contentType string
	data        []byte
	images      []groq.Image
}

func registerFileRoutes(
	r chi.Router,
	logger *slog.Logger,
	repo *repository.FileRepository,
	extractor groq.DocumentExtractor,
) {
	handler := fileHandler{logger: logger, repo: repo, extractor: extractor}
	r.Post("/irp-files", handler.uploadIRPFile)
	r.Post("/cdl-files", handler.uploadCDLFile)
	r.Get("/files/{id}", handler.downloadFile)
}

func (handler fileHandler) uploadIRPFile(w http.ResponseWriter, r *http.Request) {
	upload, ok := handler.readDocumentUpload(w, r, "cab card")
	if !ok {
		return
	}
	fields, err := handler.extractor.ExtractCabCard(r.Context(), upload.images)
	if err != nil {
		handler.writeExtractionError(w, "IRP cab card", err)
		return
	}
	storedFile, ok := handler.storeDocument(w, r, "IRP cab card", upload)
	if !ok {
		return
	}
	writeJSON(w, http.StatusCreated, irpUploadResponse{File: storedFile, Fields: fields})
}

func (handler fileHandler) uploadCDLFile(w http.ResponseWriter, r *http.Request) {
	upload, ok := handler.readDocumentUpload(w, r, "CDL")
	if !ok {
		return
	}
	fields, err := handler.extractor.ExtractCDL(r.Context(), upload.images)
	if err != nil {
		handler.writeExtractionError(w, "CDL", err)
		return
	}
	storedFile, ok := handler.storeDocument(w, r, "CDL", upload)
	if !ok {
		return
	}
	writeJSON(w, http.StatusCreated, cdlUploadResponse{File: storedFile, Fields: fields})
}

func (handler fileHandler) readDocumentUpload(
	w http.ResponseWriter,
	r *http.Request,
	label string,
) (uploadedDocument, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxMultipartBody)
	if err := r.ParseMultipartForm(2 << 20); err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			writeAPIError(w, http.StatusRequestEntityTooLarge, "the "+label+" upload is too large")
			return uploadedDocument{}, false
		}
		writeAPIError(w, http.StatusBadRequest, "invalid "+label+" upload")
		return uploadedDocument{}, false
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}

	originalHeader, err := firstMultipartFile(r, "file")
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "a "+label+" file is required")
		return uploadedDocument{}, false
	}
	originalData, err := readMultipartFile(originalHeader, maxStoredFileSize)
	if err != nil {
		writeAPIError(w, http.StatusRequestEntityTooLarge, "the original "+label+" file must be 10 MB or smaller")
		return uploadedDocument{}, false
	}
	originalType := detectUploadContentType(originalHeader.Header.Get("Content-Type"), originalData)
	if !isSupportedDocumentType(originalType) {
		writeAPIError(w, http.StatusUnsupportedMediaType, label+" files must be PDF, PNG, JPEG, or WEBP")
		return uploadedDocument{}, false
	}

	images := make([]groq.Image, 0, 3)
	if strings.HasPrefix(originalType, "image/") {
		images = append(images, groq.Image{ContentType: originalType, Data: originalData})
	} else {
		pageHeaders := r.MultipartForm.File["page"]
		if len(pageHeaders) == 0 {
			writeAPIError(w, http.StatusBadRequest, "PDF "+label+" files require rendered page images")
			return uploadedDocument{}, false
		}
		if len(pageHeaders) > 3 {
			writeAPIError(w, http.StatusBadRequest, "at most three PDF pages can be extracted")
			return uploadedDocument{}, false
		}
		total := 0
		for _, pageHeader := range pageHeaders {
			pageData, readErr := readMultipartFile(pageHeader, maxExtractionImage)
			if readErr != nil {
				writeAPIError(w, http.StatusRequestEntityTooLarge, "each rendered PDF page must be 6 MB or smaller")
				return uploadedDocument{}, false
			}
			pageType := detectUploadContentType(pageHeader.Header.Get("Content-Type"), pageData)
			if !isSupportedImageType(pageType) {
				writeAPIError(w, http.StatusUnsupportedMediaType, "rendered PDF pages must be PNG, JPEG, or WEBP images")
				return uploadedDocument{}, false
			}
			total += len(pageData)
			if total > maxExtractionPayload {
				writeAPIError(w, http.StatusRequestEntityTooLarge, "rendered PDF pages are too large")
				return uploadedDocument{}, false
			}
			images = append(images, groq.Image{ContentType: pageType, Data: pageData})
		}
	}
	return uploadedDocument{
		fileName:    safeUploadFileName(originalHeader.Filename, label),
		contentType: originalType,
		data:        originalData,
		images:      images,
	}, true
}

func (handler fileHandler) writeExtractionError(w http.ResponseWriter, label string, err error) {
	if err != nil {
		if errors.Is(err, groq.ErrNotConfigured) {
			writeAPIError(w, http.StatusServiceUnavailable, "GROQ_API_KEY is not configured on the server")
			return
		}
		handler.logger.Error("extract document", "document", label, "error", err)
		writeAPIError(w, http.StatusBadGateway, "the "+label+" could not be read: "+err.Error())
	}
}

func (handler fileHandler) storeDocument(
	w http.ResponseWriter,
	r *http.Request,
	label string,
	upload uploadedDocument,
) (repository.StoredFileMetadata, bool) {
	storedFile, err := handler.repo.CreateFile(r.Context(), upload.fileName, upload.contentType, upload.data)
	if err != nil {
		handler.logger.Error("store document", "document", label, "error", err)
		writeAPIError(w, http.StatusInternalServerError, "the "+label+" could not be stored")
		return repository.StoredFileMetadata{}, false
	}
	return storedFile, true
}

func (handler fileHandler) downloadFile(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	file, err := handler.repo.GetFile(r.Context(), id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			writeAPIError(w, http.StatusNotFound, "file not found")
			return
		}
		handler.logger.Error("download stored file", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "the file could not be loaded")
		return
	}

	disposition := mime.FormatMediaType("attachment", map[string]string{"filename": file.FileName})
	w.Header().Set("Content-Type", file.ContentType)
	w.Header().Set("Content-Disposition", disposition)
	w.Header().Set("Content-Length", strconv.FormatInt(file.SizeBytes, 10))
	w.Header().Set("ETag", `"sha256-`+file.SHA256+`"`)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(file.Data)
}

func firstMultipartFile(r *http.Request, field string) (*multipart.FileHeader, error) {
	headers := r.MultipartForm.File[field]
	if len(headers) == 0 {
		return nil, errors.New("missing multipart file")
	}
	return headers[0], nil
}

func readMultipartFile(header *multipart.FileHeader, limit int64) ([]byte, error) {
	file, err := header.Open()
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("file exceeds %d bytes", limit)
	}
	if len(data) == 0 {
		return nil, errors.New("file is empty")
	}
	return data, nil
}

func detectUploadContentType(declared string, data []byte) string {
	detected := http.DetectContentType(data)
	if detected != "application/octet-stream" {
		return strings.ToLower(strings.TrimSpace(strings.Split(detected, ";")[0]))
	}
	return strings.ToLower(strings.TrimSpace(strings.Split(declared, ";")[0]))
}

func isSupportedDocumentType(contentType string) bool {
	return contentType == "application/pdf" || isSupportedImageType(contentType)
}

func isSupportedImageType(contentType string) bool {
	return contentType == "image/jpeg" || contentType == "image/png" || contentType == "image/webp"
}

func safeUploadFileName(name, fallback string) string {
	cleaned := filepath.Base(strings.ReplaceAll(strings.TrimSpace(name), "\\", "/"))
	if cleaned == "." || cleaned == "" {
		return strings.ToLower(strings.ReplaceAll(fallback, " ", "-"))
	}
	return cleaned
}
