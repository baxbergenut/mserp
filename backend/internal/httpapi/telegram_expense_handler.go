package httpapi

import (
	"context"
	"crypto/subtle"
	"io"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
)

type TelegramExpenseOptions struct {
	Enabled       bool
	WebhookSecret string
	Service       TelegramExpenseAcceptor
}

type TelegramExpenseAcceptor interface {
	AcceptUpdate(context.Context, []byte) (bool, error)
}

func registerTelegramExpenseWebhook(r chi.Router, logger *slog.Logger, options TelegramExpenseOptions) {
	r.Post("/telegram/expenses/webhook", func(w http.ResponseWriter, request *http.Request) {
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
		payload, err := io.ReadAll(request.Body)
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid Telegram update")
			return
		}
		accepted, err := options.Service.AcceptUpdate(request.Context(), payload)
		if err != nil {
			logger.Error("accept Telegram expense update", "error", err)
			writeAPIError(w, http.StatusInternalServerError, "Telegram update could not be queued")
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"accepted": accepted})
	})
}
