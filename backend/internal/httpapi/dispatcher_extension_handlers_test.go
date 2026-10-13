package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"mserp/internal/repository"
)

type extensionStoreFake struct {
	code, token string
	enabled     bool
	version     int
}

func (s *extensionStoreFake) DispatcherExtensionAccess(context.Context, string) (repository.DispatcherExtensionAccess, error) {
	return repository.DispatcherExtensionAccess{Enabled: s.enabled, Version: s.version}, nil
}
func (s *extensionStoreFake) UpdaterExtensionAccess(ctx context.Context, id string) (repository.DispatcherExtensionAccess, error) {
	return s.DispatcherExtensionAccess(ctx, id)
}
func (s *extensionStoreFake) SetUpdaterExtensionAccess(ctx context.Context, id, actor, label, hash string, v int, enabled bool) error {
	return s.SetDispatcherExtensionAccess(ctx, id, actor, label, hash, v, enabled)
}
func (s *extensionStoreFake) SetDispatcherExtensionAccess(_ context.Context, _, _, _, hash string, v int, enabled bool) error {
	if v != s.version {
		return repository.ErrAccessConflict
	}
	s.code = hash
	s.token = ""
	s.enabled = enabled
	s.version++
	return nil
}
func (s *extensionStoreFake) ActivateDispatcherExtension(_ context.Context, c, t string) (string, error) {
	if !s.enabled || c != s.code || s.code == "" {
		return "", pgx.ErrNoRows
	}
	s.code = ""
	s.token = t
	return "Dispatcher", nil
}
func (s *extensionStoreFake) CheckDispatcherExtension(_ context.Context, t string) (string, error) {
	if !s.enabled || t != s.token || s.token == "" {
		return "", pgx.ErrNoRows
	}
	return "Dispatcher", nil
}

func TestDispatcherExtensionBoundaries(t *testing.T) {
	for _, kind := range []string{"dispatcher", "updater"} {
		t.Run(kind, func(t *testing.T) { testExtensionBoundaries(t, kind) })
	}
}

func testExtensionBoundaries(t *testing.T, kind string) {
	store := &extensionStoreFake{}
	r := chi.NewRouter()
	p := chi.NewRouter()
	permissions := []string{"access.manage"}
	p.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), authContextKey{}, repository.AuthSession{User: repository.AuthUser{ID: "admin", Permissions: permissions}, CSRFToken: "csrf"})))
		})
	})
	h := &authHandler{}
	p.Use(h.requireCSRF, requirePermission)
	registerDispatcherExtensionRoutes(r, p, store)
	r.Mount("/", p)
	call := func(path, method, body, token, csrf string) *httptest.ResponseRecorder {
		q := httptest.NewRequest(method, path, strings.NewReader(body))
		q.Header.Set("Authorization", token)
		q.Header.Set("X-CSRF-Token", csrf)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, q)
		return w
	}
	adminPath := "/settings/" + kind + "-extension/11111111-1111-4111-8111-111111111111"
	if w := call(adminPath, "PUT", `{"enabled":true,"version":0}`, "", ""); w.Code != 403 {
		t.Fatal("missing CSRF accepted")
	}
	permissions = []string{"fleet.write"}
	if w := call(adminPath, "PUT", `{"enabled":true,"version":0}`, "", "csrf"); w.Code != 403 {
		t.Fatal("fleet editor can grant access")
	}
	permissions = []string{"access.manage"}
	w := call(adminPath, "PUT", `{"enabled":true,"version":0}`, "", "csrf")
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var activation map[string]string
	json.Unmarshal(w.Body.Bytes(), &activation)
	code := activation["activationCode"]
	if len(code) != 43 || store.code == code || store.code != hashToken(code) {
		t.Fatal("activation not hashed")
	}
	w = call("/dispatcher-extension/activate", "POST", `{"code":"`+code+`"}`, "", "")
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var session map[string]string
	json.Unmarshal(w.Body.Bytes(), &session)
	token := session["token"]
	if store.token != hashToken(token) {
		t.Fatal("token not hashed")
	}
	if w = call("/dispatcher-extension/activate", "POST", `{"code":"`+code+`"}`, "", ""); w.Code != 401 {
		t.Fatal("code replay accepted")
	}
	if w = call("/dispatcher-extension/check", "POST", `{}`, "Bearer "+token, ""); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if w = call("/dispatcher-extension/check", "POST", `{}`, "", ""); w.Code != 401 {
		t.Fatal("anonymous access")
	}
	if w = call(adminPath, "PUT", `{"enabled":false,"version":0}`, "", "csrf"); w.Code != 409 {
		t.Fatal("stale revocation accepted")
	}
	if w = call(adminPath, "PUT", `{"enabled":false,"version":1}`, "", "csrf"); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if w = call("/dispatcher-extension/check", "POST", `{}`, "Bearer "+token, ""); w.Code != 401 {
		t.Fatal("revoked token accepted")
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("token response cached")
	}
}
