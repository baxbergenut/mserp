package httpapi

import (
	"context"
	"crypto/subtle"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"mserp/internal/assistant"
	"mserp/internal/repository"
)

type TelegramOptions struct {
	Enabled       bool
	WebhookSecret string
	BotUsername   string
	Service       TelegramUpdateAcceptor
	Repository    *repository.AssistantRepository
}

type TelegramUpdateAcceptor interface {
	AcceptUpdate(context.Context, []byte) (bool, error)
}

var _ TelegramUpdateAcceptor = (*assistant.Service)(nil)

func registerTelegramWebhook(r chi.Router, logger *slog.Logger, options TelegramOptions) {
	r.Post("/telegram/webhook", func(w http.ResponseWriter, request *http.Request) {
		if !options.Enabled || options.Service == nil {
			http.NotFound(w, request)
			return
		}
		provided := request.Header.Get("X-Telegram-Bot-Api-Secret-Token")
		if provided == "" || subtle.ConstantTimeCompare([]byte(provided), []byte(options.WebhookSecret)) != 1 {
			writeAPIError(w, http.StatusForbidden, "invalid Telegram webhook secret")
			return
		}
		request.Body = http.MaxBytesReader(w, request.Body, 1<<20)
		body, err := io.ReadAll(request.Body)
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid Telegram update")
			return
		}
		inserted, err := options.Service.AcceptUpdate(request.Context(), body)
		if err != nil {
			if strings.Contains(err.Error(), "rate limit") || strings.Contains(err.Error(), "queue is full") {
				w.Header().Set("Retry-After", "10")
				writeAPIError(w, http.StatusTooManyRequests, err.Error())
				return
			}
			logger.Error("accept Telegram update", "error", err)
			writeAPIError(w, http.StatusBadRequest, "invalid Telegram update")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"accepted": inserted})
	})
}

func registerTelegramSettings(r chi.Router, logger *slog.Logger, options TelegramOptions) {
	if options.Repository == nil {
		return
	}
	requireManager := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
			session, ok := authSessionFromContext(request.Context())
			if !ok {
				writeAPIError(w, http.StatusUnauthorized, "authentication required")
				return
			}
			status, err := options.Repository.ManagerStatus(request.Context(), session.User.ID)
			if err != nil || !status.Approved {
				writeAPIError(w, http.StatusForbidden, "Telegram manager approval required")
				return
			}
			next.ServeHTTP(w, request)
		})
	}
	r.Group(func(manager chi.Router) {
		manager.Use(requireManager)
		manager.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Cache-Control", "no-store")
				next.ServeHTTP(w, r)
			})
		})
		manager.Get("/telegram/managers", func(w http.ResponseWriter, request *http.Request) {
			values, err := options.Repository.ListManagers(request.Context())
			if err != nil {
				logger.Error("list Telegram managers", "error", err)
				writeAPIError(w, 500, "Telegram managers could not be loaded")
				return
			}
			writeJSON(w, 200, values)
		})
		manager.Put("/telegram/managers/{id}", func(w http.ResponseWriter, request *http.Request) {
			var body struct {
				Approved bool `json:"approved"`
			}
			if err := decodeJSON(request, &body); err != nil {
				writeAPIError(w, 400, err.Error())
				return
			}
			session, _ := authSessionFromContext(request.Context())
			err := options.Repository.SetManagerApproved(request.Context(), session.User.ID, chi.URLParam(request, "id"), body.Approved)
			if errors.Is(err, repository.ErrLastTelegramManager) {
				writeAPIError(w, 409, err.Error())
				return
			}
			if errors.Is(err, repository.ErrNotFound) {
				writeAPIError(w, 404, "user not found")
				return
			}
			if err != nil {
				logger.Error("update Telegram manager", "error", err)
				writeAPIError(w, 500, "Telegram manager could not be updated")
				return
			}
			w.WriteHeader(http.StatusNoContent)
		})
		manager.Delete("/telegram/managers/{id}/link", func(w http.ResponseWriter, request *http.Request) {
			if err := options.Repository.RevokeIdentity(request.Context(), chi.URLParam(request, "id")); err != nil {
				writeAPIError(w, 500, "Telegram link could not be revoked")
				return
			}
			w.WriteHeader(204)
		})
		manager.Post("/telegram/link", func(w http.ResponseWriter, request *http.Request) {
			if !options.Enabled || options.BotUsername == "" {
				writeAPIError(w, 503, "Telegram bot is not configured")
				return
			}
			session, _ := authSessionFromContext(request.Context())
			token, err := randomToken()
			if err != nil {
				writeAPIError(w, 500, "link could not be generated")
				return
			}
			expires := time.Now().Add(10 * time.Minute)
			if err := options.Repository.CreateLinkToken(request.Context(), session.User.ID, hashToken(token), expires); err != nil {
				writeAPIError(w, 403, "Telegram manager approval required")
				return
			}
			link := "https://t.me/" + url.PathEscape(options.BotUsername) + "?start=" + url.QueryEscape(token)
			writeJSON(w, 201, map[string]any{"url": link, "expiresAt": expires})
		})
		manager.Delete("/telegram/link", func(w http.ResponseWriter, request *http.Request) {
			session, _ := authSessionFromContext(request.Context())
			if err := options.Repository.RevokeIdentity(request.Context(), session.User.ID); err != nil {
				writeAPIError(w, 500, "Telegram link could not be revoked")
				return
			}
			w.WriteHeader(204)
		})
		manager.Get("/telegram/audit", func(w http.ResponseWriter, request *http.Request) {
			limit, _ := strconv.Atoi(request.URL.Query().Get("limit"))
			values, err := options.Repository.ListAudit(request.Context(), limit)
			if err != nil {
				logger.Error("list assistant audit", "error", err)
				writeAPIError(w, 500, "audit log could not be loaded")
				return
			}
			writeJSON(w, 200, values)
		})
	})
}
