package httpapi

import (
	"io"
	"log/slog"
	"mserp/internal/repository"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestSharedDispatcherOrderRetired(t *testing.T) {
	router := chi.NewRouter()
	// No repository is needed: old clients can never reach a shared database write.
	registerDriverBoardRoutes(router, slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/driver-board/dispatcher-order", strings.NewReader(`{}`)))
	if response.Code != http.StatusGone {
		t.Fatalf("retired ordering returned %d: %s", response.Code, response.Body.String())
	}
}

func TestDriverBoardValidation(t *testing.T) {
	good := repository.DriverBoardEntry{DriverID: "00000000-0000-0000-0000-000000000001", Status: "ENROUTE"}
	for _, tc := range []struct {
		name    string
		entries []repository.DriverBoardEntry
		valid   bool
	}{
		{"valid", []repository.DriverBoardEntry{good}, true}, {"empty", nil, false},
		{"duplicate", []repository.DriverBoardEntry{good, good}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := driverBoardRequest{Entries: tc.entries}
			if (request.validate() == nil) != tc.valid {
				t.Fatal("unexpected validation")
			}
		})
	}
	for _, mutate := range []func(*repository.DriverBoardEntry){
		func(e *repository.DriverBoardEntry) { e.Status = "unknown" }, func(e *repository.DriverBoardEntry) { e.Version = -1 },
		func(e *repository.DriverBoardEntry) { e.DriverID = "invalid" },
		func(e *repository.DriverBoardEntry) { e.Notes = strings.Repeat("a", 5001) }, func(e *repository.DriverBoardEntry) { e.Notes = "nul\x00" },
		func(e *repository.DriverBoardEntry) { e.TrailerNote = strings.Repeat("a", 5001) }, func(e *repository.DriverBoardEntry) { e.TrailerNote = "nul\x00" },
	} {
		entry := good
		mutate(&entry)
		request := driverBoardRequest{Entries: []repository.DriverBoardEntry{entry}}
		if request.validate() == nil {
			t.Errorf("accepted invalid entry %+v", entry)
		}
	}
	good.Notes = "Typing a "
	request := driverBoardRequest{Entries: []repository.DriverBoardEntry{good}}
	if err := request.validate(); err != nil || request.Entries[0].Notes != good.Notes {
		t.Fatal("immediate autosave must preserve typed spaces", err)
	}
	if routePermission("GET", "/driver-board") != "driver_board.read" || routePermission("PUT", "/driver-board") != "driver_board.write" {
		t.Fatal("driver board must have explicit access controls")
	}
	if routePermission("POST", "/driver-board/order") != "driver_board.write" || routePermission("PUT", "/driver-board/auto-order") != "driver_board.write" {
		t.Fatal("driver ordering requires board write permission")
	}
	if routePermission("POST", "/driver-board/dispatcher-order") != "driver_board.write" {
		t.Fatal("dispatcher ordering requires board write permission")
	}
	for _, action := range []struct{ method, path string }{{"GET", "/driver-board/events"}, {"GET", "/driver-board/presence"}, {"PUT", "/driver-board/presence"}, {"DELETE", "/driver-board/presence/tab"}} {
		if routePermission(action.method, action.path) != "driver_board.read" {
			t.Fatal("presence requires board visibility", action)
		}
	}
}

func TestDriverBoardSheetStatuses(t *testing.T) {
	for _, status := range []string{"READY", "REST", "COVERED", "DISPATCHED", "ENROUTE", "RESERVED", "VACATION", "DEADHEAD", "HOME", "SHOP", "STOP", "In the YARD", "Hold for Load", "Left", "Family issue", "Issue with PU/Del", "At PU", "Health issue", "At DEL", "HOME (YARD)", "NEW DRIVER", "34 RESET", "ACCIDENT"} {
		request := driverBoardRequest{Entries: []repository.DriverBoardEntry{{DriverID: "00000000-0000-0000-0000-000000000001", Status: status, StatusEdited: true, LeavePlan: &repository.DriverLeavePlan{HomeStart: "2026-10-23"}}}}
		if err := request.validate(); err != nil {
			t.Errorf("sheet status %q: %v", status, err)
		}
	}
	for _, status := range []string{"RESET", "NO LOAD", "STUCK", "LATE DEL", "TRUCK ISSUE", "LEFT", "LOAD CANCELLED", "REJECTED"} {
		request := driverBoardRequest{Entries: []repository.DriverBoardEntry{{DriverID: "00000000-0000-0000-0000-000000000001", Status: status, Notes: "unrelated edit"}}}
		if err := request.validate(); err != nil {
			t.Errorf("existing %q blocked unrelated edit: %v", status, err)
		}
		request.Entries[0].StatusEdited = true
		if request.validate() == nil {
			t.Errorf("offered legacy %q as a new driver status", status)
		}
	}
}

func TestETARetryRequiresBoardWrite(t *testing.T) {
	if routePermission("POST", "/driver-board/eta-retry/9ccb2c6e-9211-4c70-ae31-c79065da120f") != "driver_board.write" {
		t.Fatal("manual automatic ETA retry must require board write permission")
	}
}

// Load completion is an ordinary Status Board edit, not an administrator action.
func TestQueuedLoadCompletionAccess(t *testing.T) {
	for _, tc := range []struct {
		role                 string
		write, csrf, session bool
		want                 int
	}{
		{"Updater", true, true, true, 204},
		{"Dispatcher", true, true, true, 204},
		{"Accountant", true, true, true, 204},
		{"Fleet", false, true, true, 403},
		{"Recruiter", false, true, true, 403},
		{"Updater", true, false, true, 403},
		{"Updater", true, true, false, 401},
	} {
		t.Run(tc.role, func(t *testing.T) {
			permissions := []string{"driver_board.read"}
			if tc.write {
				permissions = append(permissions, "driver_board.write")
			}
			store := &fakeAuthStore{sessions: map[string]repository.AuthSession{hashToken("session"): {User: repository.AuthUser{ID: "editor", RoleName: tc.role, Administrator: false, Permissions: permissions}, CSRFToken: "csrf"}}}
			logger := slog.New(slog.NewTextHandler(io.Discard, nil))
			auth := newAuthHandler(logger, store, AuthOptions{})
			router := chi.NewRouter()
			router.Use(auth.requireSession, auth.requireCSRF, requirePermission)
			router.Post("/driver-board/loads/{driverId}", func(w http.ResponseWriter, r *http.Request) {
				var action repository.BoardLoadAction
				if err := decodeJSON(r, &action); err != nil || action.Action != "complete_queued" {
					t.Fatal("unexpected completion request", err)
				}
				w.WriteHeader(http.StatusNoContent)
			})
			req := httptest.NewRequest("POST", "/driver-board/loads/e4540000-0000-0000-0000-000000000001", strings.NewReader(`{"action":"complete_queued","planId":"queued-plan"}`))
			if tc.session {
				req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session"})
			}
			if tc.csrf {
				req.Header.Set("X-CSRF-Token", "csrf")
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, req)
			if response.Code != tc.want {
				t.Fatalf("got %d, want %d: %s", response.Code, tc.want, response.Body.String())
			}
		})
	}
}
