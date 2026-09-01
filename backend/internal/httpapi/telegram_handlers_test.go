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

type fakeTelegramAcceptor struct{ called bool }

func (f *fakeTelegramAcceptor) AcceptUpdate(context.Context, []byte) (bool, error) {
	f.called = true
	return true, nil
}
func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestTelegramWebhookRequiresSecret(t *testing.T) {
	acceptor := &fakeTelegramAcceptor{}
	router := chi.NewRouter()
	registerTelegramWebhook(router, discardLogger(), TelegramOptions{Enabled: true, WebhookSecret: "expected", Service: acceptor})
	request := httptest.NewRequest(http.MethodPost, "/telegram/webhook", strings.NewReader(`{"update_id":1}`))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status=%d", response.Code)
	}
	if acceptor.called {
		t.Fatal("acceptor called without valid secret")
	}
	request = httptest.NewRequest(http.MethodPost, "/telegram/webhook", strings.NewReader(`{"update_id":1}`))
	request.Header.Set("X-Telegram-Bot-Api-Secret-Token", "expected")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("valid secret status=%d body=%s", response.Code, response.Body.String())
	}
	if !acceptor.called {
		t.Fatal("acceptor not called")
	}
}
