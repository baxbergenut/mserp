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

	"github.com/go-chi/chi/v5"
	"mserp/internal/repository"
)

func TestGrossBoardPlanningValidation(t *testing.T) {
	base := repository.GrossBoardPlanningEntry{DriverID: "aaaaaaaa-0000-0000-0000-000000000001", Date: "2026-11-02", Kind: "note", Text: " family emergency "}
	for _, tc := range []struct {
		name  string
		edit  func(*grossBoardPlanningRequest)
		valid bool
	}{
		{"note", func(r *grossBoardPlanningRequest) {}, true},
		{"destination alongside note", func(r *grossBoardPlanningRequest) {
			e := base
			e.Kind = "destination"
			r.Entries = append(r.Entries, e)
		}, true},
		{"1000 Unicode characters", func(r *grossBoardPlanningRequest) { r.Entries[0].Text = strings.Repeat("🚛", 1000) }, true},
		{"delete", func(r *grossBoardPlanningRequest) { r.Entries[0].Text = ""; r.Entries[0].Version = 3 }, true},
		{"invalid kind", func(r *grossBoardPlanningRequest) { r.Entries[0].Kind = "load" }, false},
		{"long text", func(r *grossBoardPlanningRequest) { r.Entries[0].Text = strings.Repeat("a", 1001) }, false},
		{"null", func(r *grossBoardPlanningRequest) { r.Entries[0].Text = "a\x00b" }, false},
		{"negative version", func(r *grossBoardPlanningRequest) { r.Entries[0].Version = -1 }, false},
		{"overflow version", func(r *grossBoardPlanningRequest) { r.Entries[0].Version = 2147483647 }, false},
		{"invalid driver", func(r *grossBoardPlanningRequest) { r.Entries[0].DriverID = "driver" }, false},
		{"other week", func(r *grossBoardPlanningRequest) { r.Entries[0].Date = "2026-11-09" }, false},
		{"non Monday", func(r *grossBoardPlanningRequest) { r.WeekStart = "2026-11-03" }, false},
		{"duplicate UUID case", func(r *grossBoardPlanningRequest) {
			e := base
			e.DriverID = strings.ToUpper(e.DriverID)
			r.Entries = append(r.Entries, e)
		}, false},
		{"empty batch", func(r *grossBoardPlanningRequest) { r.Entries = nil }, false},
		{"oversized batch", func(r *grossBoardPlanningRequest) { r.Entries = make([]repository.GrossBoardPlanningEntry, 1001) }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := grossBoardPlanningRequest{WeekStart: "2026-11-02", Entries: []repository.GrossBoardPlanningEntry{base}}
			tc.edit(&r)
			err := r.validate()
			if (err == nil) != tc.valid {
				t.Fatal(err)
			}
			if tc.name == "note" && r.Entries[0].Text != "family emergency" {
				t.Fatal("text was not trimmed")
			}
		})
	}
}

func TestGrossBoardExplicitStatusWordLoad(t *testing.T) {
	request := grossBoardRequest{WeekStart: "2026-11-02", Entries: []repository.GrossBoardEntry{{DriverID: "aaaaaaaa-0000-0000-0000-000000000001", Date: "2026-11-02", LoadNumber: "HOME", DayStatus: ""}}}
	if err := request.validate(); err != nil || request.Entries[0].LoadNumber != "HOME" || request.Entries[0].DayStatus != "" {
		t.Fatal("an explicitly chosen manual load was inferred to be a status", request, err)
	}
}

func TestGrossBoardPlanningPermissionAndPayloadBoundaries(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	router := chi.NewRouter()
	router.Use(requirePermission)
	registerGrossBoardRoutes(router, logger, nil)
	for _, permission := range []string{"board.read", "driver_board.write", "driver_pay.edit", "investor_pay.edit"} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, capabilityRequest(http.MethodPut, "/gross-board/planning-entries", `{}`, permission))
		if w.Code != http.StatusForbidden {
			t.Fatalf("permission %s: %d", permission, w.Code)
		}
	}
	for _, extra := range []string{`"loadNumber":"84X0165"`, `"dayStatus":"HOME"`, `"driverRate":"900"`, `"loadRecordId":1`} {
		w := httptest.NewRecorder()
		body := `{"weekStart":"2026-11-02","entries":[{"driverId":"aaaaaaaa-0000-0000-0000-000000000001","date":"2026-11-02","kind":"note","text":"hello","version":0,` + extra + `}]}`
		router.ServeHTTP(w, capabilityRequest(http.MethodPut, "/gross-board/planning-entries", body, "board.write"))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("load field accepted: %s: %d", extra, w.Code)
		}
	}
	for _, path := range []string{"/gross-board", "/gross-board/events", "/driver-board", "/driver-pay"} {
		for _, allowed := range []bool{false, true} {
			user := repository.AuthUser{}
			if allowed {
				user.Permissions = []string{"board.read"}
			}
			payload := map[string]any{"planningEntries": []any{map[string]any{"text": "private planning"}}}
			projectFields(payload, user, path)
			_, exists := payload["planningEntries"]
			if exists != (allowed && (path == "/gross-board" || path == "/gross-board/events")) {
				t.Fatal("planning projection boundary", path, allowed)
			}
		}
	}
}

func TestGrossBoardPlanningLiveSnapshotProjection(t *testing.T) {
	h := newDatabaseEvents(nil, slog.New(slog.NewTextHandler(io.Discard, nil)), "mserp_gross_board", "board.read")
	defer h.shutdown()
	h.snapshotEvent = "grossBoard"
	h.snapshot = func(context.Context) ([]byte, error) {
		return json.Marshal(map[string]any{"planningEntries": []repository.GrossBoardPlanningEntry{{Kind: "note", Text: "private planning"}}})
	}
	for _, allowed := range []bool{false, true} {
		user := repository.AuthUser{}
		if allowed {
			user.Permissions = []string{"board.read"}
		}
		ctx := context.WithValue(context.Background(), authContextKey{}, repository.AuthSession{User: user})
		frame := h.frame(ctx, "changed")
		if strings.Contains(frame, "private planning") != allowed {
			t.Fatal("cached snapshot leaked or lost planning", frame)
		}
	}
}
