package httpapi

import (
	"encoding/json"
	"mserp/internal/repository"
	"testing"
)

func TestUpdaterValidation(t *testing.T) {
	for _, ext := range []int{-1, 1000000} {
		if validateExtension(&ext) == nil {
			t.Fatal("invalid extension accepted")
		}
	}
	for _, in := range []repository.UpdaterInput{{FullName: "Roy", Shift: "night"}, {FullName: " ", Shift: "main"}} {
		if validateUpdater(in) == nil {
			t.Fatal("invalid updater accepted")
		}
	}
	for _, raw := range []string{`1.2`, `"106"`, `-1`} {
		if _, err := (dispatcherRequest{FullName: "Test", Extension: json.RawMessage(raw)}).validate(); err == nil {
			t.Fatal("invalid dispatcher extension", raw)
		}
	}
	input, err := (dispatcherRequest{FullName: "Test", Extension: json.RawMessage(`null`)}).validate()
	if err != nil || !input.ExtensionSet || input.Extension != nil {
		t.Fatal("explicit clear missing")
	}
	if routePermission("GET", "/updaters") != "fleet.read" || routePermission("PUT", "/updaters/id") != "fleet.write" {
		t.Fatal("updater access mapping missing")
	}
}
