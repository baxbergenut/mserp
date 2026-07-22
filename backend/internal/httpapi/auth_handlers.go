package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	"mserp/internal/repository"
)

const sessionCookieName = "mserp_session"

type authStore interface {
	FindUserByUsername(context.Context, string) (repository.AuthUser, error)
	CreateSession(context.Context, string, string, string, time.Time) error
	FindSessionByTokenHash(context.Context, string) (repository.AuthSession, error)
	DeleteSessionByTokenHash(context.Context, string) error
}

type AuthOptions struct {
	CookieSecure bool
	SessionTTL   time.Duration
}

type authHandler struct {
	logger            *slog.Logger
	store             authStore
	options           AuthOptions
	limiter           *loginLimiter
	now               func() time.Time
	dummyPasswordHash string
}

type authContextKey struct{}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type authUserResponse struct {
	ID       string `json:"id"`
	Username string `json:"username"`
}

type sessionResponse struct {
	User      authUserResponse `json:"user"`
	CSRFToken string           `json:"csrfToken"`
	ExpiresAt time.Time        `json:"expiresAt"`
}

func newAuthHandler(logger *slog.Logger, store authStore, options AuthOptions) *authHandler {
	dummyPasswordHash, err := bcrypt.GenerateFromPassword([]byte("dummy-password"), 12)
	if err != nil {
		panic("generate dummy bcrypt hash: " + err.Error())
	}
	return &authHandler{
		logger:            logger,
		store:             store,
		options:           options,
		limiter:           newLoginLimiter(5, 15*time.Minute),
		now:               time.Now,
		dummyPasswordHash: string(dummyPasswordHash),
	}
}

func (h *authHandler) login(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	var request loginRequest
	if err := decodeJSON(r, &request); err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	request.Username = strings.TrimSpace(request.Username)
	if request.Username == "" || request.Password == "" || len(request.Username) > 200 || len(request.Password) > 72 {
		writeAPIError(w, http.StatusUnauthorized, "invalid username or password")
		return
	}

	limiterKey := clientAddress(r) + "\x00" + strings.ToLower(request.Username)
	if !h.limiter.allow(limiterKey, h.now()) {
		w.Header().Set("Retry-After", "900")
		writeAPIError(w, http.StatusTooManyRequests, "too many login attempts; try again later")
		return
	}

	user, err := h.store.FindUserByUsername(r.Context(), request.Username)
	if err != nil && !errors.Is(err, repository.ErrAuthRecordNotFound) {
		h.logger.Error("find login user", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "login could not be completed")
		return
	}

	passwordHash := user.PasswordHash
	if passwordHash == "" {
		// Keep missing-user requests on the same expensive bcrypt path.
		passwordHash = h.dummyPasswordHash
	}
	if bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(request.Password)) != nil || user.ID == "" {
		h.limiter.fail(limiterKey, h.now())
		writeAPIError(w, http.StatusUnauthorized, "invalid username or password")
		return
	}

	token, err := randomToken()
	if err != nil {
		h.logger.Error("generate session token", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "login could not be completed")
		return
	}
	csrfToken, err := randomToken()
	if err != nil {
		h.logger.Error("generate CSRF token", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "login could not be completed")
		return
	}

	expiresAt := h.now().Add(h.options.SessionTTL)
	if err := h.store.CreateSession(r.Context(), user.ID, hashToken(token), csrfToken, expiresAt); err != nil {
		h.logger.Error("create login session", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "login could not be completed")
		return
	}

	h.limiter.succeed(limiterKey)
	h.setSessionCookie(w, token, expiresAt)
	writeJSON(w, http.StatusOK, makeSessionResponse(user, csrfToken, expiresAt))
}

func (h *authHandler) session(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	session, ok := authSessionFromContext(r.Context())
	if !ok {
		writeAPIError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	writeJSON(w, http.StatusOK, makeSessionResponse(session.User, session.CSRFToken, session.ExpiresAt))
}

func (h *authHandler) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		if err := h.store.DeleteSessionByTokenHash(r.Context(), hashToken(cookie.Value)); err != nil {
			h.logger.Error("delete login session", "error", err)
			writeAPIError(w, http.StatusInternalServerError, "logout could not be completed")
			return
		}
	}
	h.clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

func (h *authHandler) requireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookieName)
		if err != nil || cookie.Value == "" {
			writeAPIError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		session, err := h.store.FindSessionByTokenHash(r.Context(), hashToken(cookie.Value))
		if errors.Is(err, repository.ErrAuthRecordNotFound) {
			h.clearSessionCookie(w)
			writeAPIError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		if err != nil {
			h.logger.Error("validate login session", "error", err)
			writeAPIError(w, http.StatusInternalServerError, "authentication could not be verified")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), authContextKey{}, session)))
	})
}

func (h *authHandler) requireCSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}
		session, ok := authSessionFromContext(r.Context())
		provided := r.Header.Get("X-CSRF-Token")
		if !ok || provided == "" || subtle.ConstantTimeCompare([]byte(provided), []byte(session.CSRFToken)) != 1 {
			writeAPIError(w, http.StatusForbidden, "invalid CSRF token")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (h *authHandler) setSessionCookie(w http.ResponseWriter, token string, expiresAt time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookieName, Value: token, Path: "/", HttpOnly: true,
		Secure: h.options.CookieSecure, SameSite: http.SameSiteStrictMode,
		Expires: expiresAt, MaxAge: int(expiresAt.Sub(h.now()).Seconds()),
	})
}

func (h *authHandler) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookieName, Value: "", Path: "/", HttpOnly: true,
		Secure: h.options.CookieSecure, SameSite: http.SameSiteStrictMode,
		Expires: time.Unix(1, 0), MaxAge: -1,
	})
}

func authSessionFromContext(ctx context.Context) (repository.AuthSession, bool) {
	session, ok := ctx.Value(authContextKey{}).(repository.AuthSession)
	return session, ok
}

func makeSessionResponse(user repository.AuthUser, csrfToken string, expiresAt time.Time) sessionResponse {
	return sessionResponse{
		User:      authUserResponse{ID: user.ID, Username: user.Username},
		CSRFToken: csrfToken,
		ExpiresAt: expiresAt,
	}
}

func randomToken() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func clientAddress(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

type loginAttempt struct {
	failures int
	blocked  time.Time
	updated  time.Time
}

type loginLimiter struct {
	mu       sync.Mutex
	attempts map[string]loginAttempt
	limit    int
	window   time.Duration
}

func newLoginLimiter(limit int, window time.Duration) *loginLimiter {
	return &loginLimiter{attempts: make(map[string]loginAttempt), limit: limit, window: window}
}

func (l *loginLimiter) allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	attempt, ok := l.attempts[key]
	if !ok {
		return true
	}
	if !attempt.blocked.IsZero() && now.Before(attempt.blocked) {
		return false
	}
	if now.Sub(attempt.updated) >= l.window {
		delete(l.attempts, key)
	}
	return true
}

func (l *loginLimiter) fail(key string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	attempt := l.attempts[key]
	if now.Sub(attempt.updated) >= l.window {
		attempt.failures = 0
	}
	attempt.failures++
	attempt.updated = now
	if attempt.failures >= l.limit {
		attempt.blocked = now.Add(l.window)
	}
	l.attempts[key] = attempt
}

func (l *loginLimiter) succeed(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, key)
}
