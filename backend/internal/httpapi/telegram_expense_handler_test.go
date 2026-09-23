package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

type telegramExpenseAcceptorStub struct{ calls int }

func (s *telegramExpenseAcceptorStub) AcceptUpdate(context.Context, []byte) (bool, error) {
	s.calls++
	return true, nil
}

func TestTelegramExpenseWebhookRequiresSecret(t *testing.T) {
	service := &telegramExpenseAcceptorStub{}
	router := chi.NewRouter()
	registerTelegramExpenseWebhook(router, slog.New(slog.NewTextHandler(io.Discard, nil)), TelegramExpenseOptions{
		Enabled: true, WebhookSecret: "expected", Service: service,
	})

	request := httptest.NewRequest(http.MethodPost, "/telegram/expenses/webhook", strings.NewReader(`{"update_id":1}`))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || service.calls != 0 {
		t.Fatalf("unauthorized response = %d, calls = %d", response.Code, service.calls)
	}

	request = httptest.NewRequest(http.MethodPost, "/telegram/expenses/webhook", strings.NewReader(`{"update_id":1}`))
	request.Header.Set("X-Telegram-Bot-Api-Secret-Token", "expected")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || service.calls != 1 {
		t.Fatalf("authorized response = %d, calls = %d", response.Code, service.calls)
	}
}
