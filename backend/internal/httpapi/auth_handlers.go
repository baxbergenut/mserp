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
	FindUserByEmail(context.Context, string) (repository.AuthUser, error)
	CreatePasswordSession(context.Context, repository.AuthUser, string, string, time.Time) error
	ChangePassword(context.Context, string, string, string) error
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
	networkLimiter    *loginLimiter
	now               func() time.Time
	dummyPasswordHash string
}

type authContextKey struct{}

type loginRequest struct {
	Email       string `json:"email"`
	Password    string `json:"password"`
	TrustDevice bool   `json:"trustDevice"`
}

type authUserResponse struct {
	ID                    string                             `json:"id"`
	Username              string                             `json:"username"`
	Email                 string                             `json:"email"`
	RoleID                string                             `json:"roleId"`
	Permissions           []string                           `json:"permissions"`
	ExpenseCategoryAccess []repository.ExpenseCategoryAccess `json:"expenseCategoryAccess"`
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
		networkLimiter:    newLoginLimiter(30, 15*time.Minute),
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
	request.Email = strings.TrimSpace(request.Email)
	if request.Email == "" || request.Password == "" || len(request.Email) > 254 || len(request.Password) > 72 {
		writeAPIError(w, http.StatusUnauthorized, "invalid email or password")
		return
	}

	limiterKey := strings.ToLower(request.Email)
	if !h.limiter.allow(limiterKey, h.now()) || !h.networkLimiter.allow(clientAddress(r), h.now()) {
		w.Header().Set("Retry-After", "900")
		writeAPIError(w, http.StatusTooManyRequests, "too many login attempts; try again later")
		return
	}
	h.networkLimiter.fail(clientAddress(r), h.now())
	h.limiter.fail(limiterKey, h.now())

	user, err := h.store.FindUserByEmail(r.Context(), request.Email)
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
		writeAPIError(w, http.StatusUnauthorized, "invalid email or password")
		return
	}

	token, err := randomToken()
	if err != nil {
		h.authFailure(w)
		return
	}
	csrf, err := randomToken()
	if err != nil {
		h.authFailure(w)
		return
	}
	ttl := h.options.SessionTTL
	if request.TrustDevice {
		ttl = 30 * 24 * time.Hour
	}
	expiry := h.now().Add(ttl)
	if err = h.store.CreatePasswordSession(r.Context(), user, hashToken(token), csrf, expiry); err != nil {
		h.authFailure(w)
		return
	}
	session, err := h.store.FindSessionByTokenHash(r.Context(), hashToken(token))
	if err != nil {
		h.authFailure(w)
		return
	}
	h.limiter.succeed(limiterKey)
	h.setSessionCookie(w, token, expiry)
	writeJSON(w, http.StatusOK, h.makeSessionResponse(r.Context(), session.User, csrf, expiry))
}

func (h *authHandler) authFailure(w http.ResponseWriter) {
	writeAPIError(w, http.StatusInternalServerError, "login could not be completed")
}

func (h *authHandler) changePassword(w http.ResponseWriter, r *http.Request) {
	var in struct {
		CurrentPassword string `json:"currentPassword"`
		NewPassword     string `json:"newPassword"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(in.NewPassword) < 12 || len(in.NewPassword) > 72 {
		writeAPIError(w, http.StatusBadRequest, "new password must contain 12–72 bytes")
		return
	}
	session, _ := authSessionFromContext(r.Context())
	key := "password:" + session.User.ID
	if !h.limiter.allow(key, h.now()) {
		writeAPIError(w, http.StatusTooManyRequests, "too many attempts; try again later")
		return
	}
	h.limiter.fail(key, h.now())
	login := session.User.Email
	if login == "" {
		login = session.User.Username
	}
	u, err := h.store.FindUserByEmail(r.Context(), login)
	if err != nil || u.ID != session.User.ID || bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(in.CurrentPassword)) != nil {
		writeAPIError(w, http.StatusUnauthorized, "current password is incorrect")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(in.NewPassword), 12)
	if err != nil {
		h.authFailure(w)
		return
	}
	if err = h.store.ChangePassword(r.Context(), u.ID, u.PasswordHash, string(hash)); err != nil {
		writeAPIError(w, http.StatusConflict, "password changed elsewhere; sign in again")
		return
	}
	h.clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

func (h *authHandler) session(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	session, ok := authSessionFromContext(r.Context())
	if !ok {
		writeAPIError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	writeJSON(w, http.StatusOK, h.makeSessionResponse(r.Context(), session.User, session.CSRFToken, session.ExpiresAt))
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
		ctx := repository.WithTaskViewer(r.Context(), session.User.ID, session.User.Administrator)
		next.ServeHTTP(w, r.WithContext(context.WithValue(ctx, authContextKey{}, session)))
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

func (h *authHandler) makeSessionResponse(ctx context.Context, user repository.AuthUser, csrfToken string, expiresAt time.Time) sessionResponse {
	response := sessionResponse{
		User:      authUserResponse{ID: user.ID, Username: user.Username, Email: user.Email, RoleID: user.RoleID, Permissions: user.Permissions, ExpenseCategoryAccess: user.ExpenseCategoryAccess},
		CSRFToken: csrfToken,
		ExpiresAt: expiresAt,
	}
	return response
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
		// Production Nginx is local and overwrites X-Real-IP. Never accept a
		// forwarded address from an untrusted remote peer.
		if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
			if real := net.ParseIP(r.Header.Get("X-Real-IP")); real != nil {
				return real.String()
			}
		}
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
	if len(l.attempts) > 10000 {
		for k, v := range l.attempts {
			if now.Sub(v.updated) >= l.window {
				delete(l.attempts, k)
			}
		}
	}
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
