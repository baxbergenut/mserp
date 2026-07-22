package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"golang.org/x/crypto/bcrypt"

	"mserp/internal/repository"
)

type fakeAuthStore struct {
	user     repository.AuthUser
	sessions map[string]repository.AuthSession
}

func (s *fakeAuthStore) FindUserByUsername(_ context.Context, username string) (repository.AuthUser, error) {
	if !strings.EqualFold(username, s.user.Username) {
		return repository.AuthUser{}, repository.ErrAuthRecordNotFound
	}
	return s.user, nil
}

func (s *fakeAuthStore) CreateSession(
	_ context.Context,
	_ string,
	tokenHash string,
	csrfToken string,
	expiresAt time.Time,
) error {
	s.sessions[tokenHash] = repository.AuthSession{
		User: s.user, CSRFToken: csrfToken, ExpiresAt: expiresAt,
	}
	return nil
}

func (s *fakeAuthStore) FindSessionByTokenHash(_ context.Context, tokenHash string) (repository.AuthSession, error) {
	session, ok := s.sessions[tokenHash]
	if !ok {
		return repository.AuthSession{}, repository.ErrAuthRecordNotFound
	}
	return session, nil
}

func (s *fakeAuthStore) DeleteSessionByTokenHash(_ context.Context, tokenHash string) error {
	delete(s.sessions, tokenHash)
	return nil
}

func TestAuthLoginSessionAndCSRF(t *testing.T) {
	passwordHash, err := bcrypt.GenerateFromPassword([]byte("correct horse battery staple"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	store := &fakeAuthStore{
		user: repository.AuthUser{
			ID: "ef4f62e2-4582-47aa-9af5-e8015ce6d32f", Username: "admin",
			PasswordHash: string(passwordHash),
		},
		sessions: make(map[string]repository.AuthSession),
	}
	handler := newAuthHandler(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		store,
		AuthOptions{SessionTTL: 12 * time.Hour},
	)

	router := chi.NewRouter()
	router.Post("/auth/login", handler.login)
	router.Group(func(r chi.Router) {
		r.Use(handler.requireSession)
		r.Use(handler.requireCSRF)
		r.Get("/auth/session", handler.session)
		r.Post("/protected", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		})
	})

	loginRequest := httptest.NewRequest(
		http.MethodPost,
		"/auth/login",
		strings.NewReader(`{"username":"ADMIN","password":"correct horse battery staple"}`),
	)
	loginRequest.Header.Set("Content-Type", "application/json")
	loginResponse := httptest.NewRecorder()
	router.ServeHTTP(loginResponse, loginRequest)
	if loginResponse.Code != http.StatusOK {
		t.Fatalf("login status = %d, body = %s", loginResponse.Code, loginResponse.Body.String())
	}

	var response sessionResponse
	if err := json.NewDecoder(loginResponse.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.User.Username != "admin" || response.CSRFToken == "" {
		t.Fatalf("unexpected login response: %+v", response)
	}
	cookies := loginResponse.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatalf("unexpected session cookie: %+v", cookies)
	}

	sessionRequest := httptest.NewRequest(http.MethodGet, "/auth/session", nil)
	sessionRequest.AddCookie(cookies[0])
	sessionResponseRecorder := httptest.NewRecorder()
	router.ServeHTTP(sessionResponseRecorder, sessionRequest)
	if sessionResponseRecorder.Code != http.StatusOK {
		t.Fatalf("session status = %d", sessionResponseRecorder.Code)
	}

	missingCSRFRequest := httptest.NewRequest(http.MethodPost, "/protected", nil)
	missingCSRFRequest.AddCookie(cookies[0])
	missingCSRFResponse := httptest.NewRecorder()
	router.ServeHTTP(missingCSRFResponse, missingCSRFRequest)
	if missingCSRFResponse.Code != http.StatusForbidden {
		t.Fatalf("missing CSRF status = %d", missingCSRFResponse.Code)
	}

	protectedRequest := httptest.NewRequest(http.MethodPost, "/protected", nil)
	protectedRequest.AddCookie(cookies[0])
	protectedRequest.Header.Set("X-CSRF-Token", response.CSRFToken)
	protectedResponse := httptest.NewRecorder()
	router.ServeHTTP(protectedResponse, protectedRequest)
	if protectedResponse.Code != http.StatusNoContent {
		t.Fatalf("protected status = %d", protectedResponse.Code)
	}
}

func TestAuthLoginRejectsInvalidCredentials(t *testing.T) {
	store := &fakeAuthStore{sessions: make(map[string]repository.AuthSession)}
	handler := newAuthHandler(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		store,
		AuthOptions{SessionTTL: time.Hour},
	)
	request := httptest.NewRequest(
		http.MethodPost,
		"/auth/login",
		strings.NewReader(`{"username":"missing","password":"wrong"}`),
	)
	response := httptest.NewRecorder()
	handler.login(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
	if !strings.Contains(response.Body.String(), "invalid username or password") {
		t.Fatalf("unexpected body: %s", response.Body.String())
	}
}
