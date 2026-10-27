package httpapi

import (
	"github.com/go-chi/chi/v5"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPrePassTruckLinkPermissions(t *testing.T) {
	if got := routePermission(http.MethodPost, "/tolls/reviews/bulk"); got != "tolls.link" {
		t.Fatalf("bulk review permission=%q", got)
	}
	if got := routePermission(http.MethodPost, "/tolls/id/review"); got != "tolls.link" {
		t.Fatalf("review permission=%q", got)
	}
	for _, v := range []struct{ method, path, want string }{{http.MethodGet, "/tolls/truck-links", "tolls.read"}, {http.MethodPost, "/tolls/truck-links/preview", "tolls.link"}, {http.MethodPost, "/tolls/truck-links", "tolls.link"}, {http.MethodPost, "/tolls/truck-links/id/undo", "tolls.link"}, {http.MethodPost, "/relay-drivers/bulk", "authenticated"}} {
		if got := routePermission(v.method, v.path); got != v.want {
			t.Errorf("%s %s = %q, want %q", v.method, v.path, got, v.want)
		}
	}
}

func TestBulkTollReviewValidation(t *testing.T) {
	r := chi.NewRouter()
	(tollHandler{}).registerTruckLinks(r)
	for _, body := range []string{`{`, `{}`, `{"changes":[]}`, `{"changes":[{}]}`} {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/tolls/reviews/bulk", strings.NewReader(body))
		r.ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("body %s: got %d", body, response.Code)
		}
	}
}
