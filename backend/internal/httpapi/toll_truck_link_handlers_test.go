package httpapi

import (
	"net/http"
	"testing"
)

func TestPrePassTruckLinkPermissions(t *testing.T) {
	if got := routePermission(http.MethodPost, "/tolls/id/review"); got != "tolls.link" {
		t.Fatalf("review permission=%q", got)
	}
	for _, v := range []struct{ method, path, want string }{{http.MethodGet, "/tolls/truck-links", "tolls.read"}, {http.MethodPost, "/tolls/truck-links/preview", "tolls.link"}, {http.MethodPost, "/tolls/truck-links", "tolls.link"}, {http.MethodPost, "/tolls/truck-links/id/undo", "tolls.link"}, {http.MethodPost, "/relay-drivers/bulk", "authenticated"}} {
		if got := routePermission(v.method, v.path); got != v.want {
			t.Errorf("%s %s = %q, want %q", v.method, v.path, got, v.want)
		}
	}
}
