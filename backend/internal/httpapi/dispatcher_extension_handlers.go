package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"mserp/internal/repository"
)

type dispatcherExtensionStore interface {
	DispatcherExtensionAccess(context.Context, string) (repository.DispatcherExtensionAccess, error)
	SetDispatcherExtensionAccess(context.Context, string, string, string, string, int, bool) error
	UpdaterExtensionAccess(context.Context, string) (repository.DispatcherExtensionAccess, error)
	SetUpdaterExtensionAccess(context.Context, string, string, string, string, int, bool) error
	ActivateDispatcherExtension(context.Context, string, string) (string, error)
	CheckDispatcherExtension(context.Context, string) (string, error)
}

func registerDispatcherExtensionRoutes(public, protected chi.Router, repo dispatcherExtensionStore) {
	registerExtensionAdminRoutes(protected, "/settings/dispatcher-extension/{id}", repo.DispatcherExtensionAccess, repo.SetDispatcherExtensionAccess)
	registerExtensionAdminRoutes(protected, "/settings/updater-extension/{id}", repo.UpdaterExtensionAccess, repo.SetUpdaterExtensionAccess)
	registerExtensionPublicRoutes(public, repo)
}

func registerExtensionAdminRoutes(protected chi.Router, path string,
	read func(context.Context, string) (repository.DispatcherExtensionAccess, error),
	save func(context.Context, string, string, string, string, int, bool) error,
) {
	protected.Get(path, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		id := chi.URLParam(r, "id")
		if !isUUID(id) {
			writeAPIError(w, 400, "invalid profile")
			return
		}
		a, err := read(r.Context(), id)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeAPIError(w, 404, "profile not found")
			} else {
				slog.Error("read dispatcher extension access", "error", err)
				writeAPIError(w, 503, "extension access could not be loaded")
			}
			return
		}
		writeJSON(w, 200, a)
	})
	protected.Put(path, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		var in struct {
			Enabled      bool   `json:"enabled"`
			AccountLabel string `json:"accountLabel"`
			Version      int    `json:"version"`
		}
		id := chi.URLParam(r, "id")
		if decodeJSON(r, &in) != nil || !isUUID(id) || in.Version < 0 || len(in.AccountLabel) > 254 {
			writeAPIError(w, 400, "invalid extension access settings")
			return
		}
		code, err := randomToken()
		if err != nil {
			writeAPIError(w, 500, "could not generate activation code")
			return
		}
		hash := ""
		if in.Enabled {
			hash = hashToken(code)
		} else {
			code = ""
		}
		session, _ := authSessionFromContext(r.Context())
		err = save(r.Context(), id, session.User.ID, strings.TrimSpace(in.AccountLabel), hash, in.Version, in.Enabled)
		if errors.Is(err, repository.ErrAccessConflict) || errors.Is(err, pgx.ErrNoRows) {
			writeAPIError(w, 409, "Profile is unavailable or access changed. Reload before trying again.")
			return
		}
		if err != nil {
			slog.Error("update dispatcher extension access", "error", err)
			writeAPIError(w, 500, "could not update extension access")
			return
		}
		writeJSON(w, 200, map[string]string{"activationCode": code})
	})
}

func registerExtensionPublicRoutes(public chi.Router, repo dispatcherExtensionStore) {
	limiter := newLoginLimiter(20, 15*time.Minute)
	public.Post("/dispatcher-extension/activate", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		key := clientAddress(r)
		now := time.Now()
		if !limiter.allow(key, now) {
			w.Header().Set("Retry-After", "900")
			writeAPIError(w, 429, "Too many attempts. Try again later.")
			return
		}
		limiter.fail(key, now)
		var in struct {
			Code string `json:"code"`
		}
		if decodeJSON(r, &in) != nil || len(in.Code) != 43 {
			writeAPIError(w, 401, "Invalid or expired activation code")
			return
		}
		token, err := randomToken()
		if err != nil {
			writeAPIError(w, 500, "Activation unavailable")
			return
		}
		name, err := repo.ActivateDispatcherExtension(r.Context(), hashToken(in.Code), hashToken(token))
		if errors.Is(err, pgx.ErrNoRows) {
			writeAPIError(w, 401, "Invalid or expired activation code")
			return
		}
		if err != nil {
			slog.Error("activate dispatcher extension", "error", err)
			writeAPIError(w, 503, "Activation unavailable")
			return
		}
		limiter.succeed(key)
		writeJSON(w, 200, map[string]string{"token": token, "dispatcherName": name})
	})
	public.Post("/dispatcher-extension/check", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		header := r.Header.Get("Authorization")
		if !strings.HasPrefix(header, "Bearer ") || len(header) != 50 {
			writeAPIError(w, 401, "Extension activation required")
			return
		}
		name, err := repo.CheckDispatcherExtension(r.Context(), hashToken(strings.TrimPrefix(header, "Bearer ")))
		if errors.Is(err, pgx.ErrNoRows) {
			writeAPIError(w, 401, "Extension access expired or revoked")
			return
		}
		if err != nil {
			slog.Error("check dispatcher extension access", "error", err)
			writeAPIError(w, 503, "Could not verify extension access")
			return
		}
		writeJSON(w, 200, map[string]any{"dispatcherName": name, "minimumIntervalSeconds": 4})
	})
}
