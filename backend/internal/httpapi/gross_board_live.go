package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"log/slog"
	"mserp/internal/repository"
	"net/http"
	"time"
)

type grossWeekContext struct{}

func registerGrossBoardLive(r chi.Router, pool *pgxpool.Pool, logger *slog.Logger, auth *authHandler, repo *repository.GrossBoardRepository) func() {
	events := newDatabaseEvents(pool, logger, "mserp_gross_board", "board.read")
	peers := newDatabaseEvents(pool, logger, "mserp_gross_presence", "board.read")
	key := func(ctx context.Context) string {
		return ctx.Value(grossWeekContext{}).(time.Time).Format(time.DateOnly)
	}
	events.snapshotKey = key
	peers.snapshotKey = key
	events.snapshotEvent = "grossBoard"
	peers.snapshotEvent = "peers"
	events.snapshot = func(ctx context.Context) ([]byte, error) {
		value, err := repo.Get(ctx, ctx.Value(grossWeekContext{}).(time.Time))
		if err != nil {
			return nil, err
		}
		return json.Marshal(value)
	}
	peers.snapshot = func(ctx context.Context) ([]byte, error) {
		value, err := repo.Presence(ctx, ctx.Value(grossWeekContext{}).(time.Time))
		if err != nil {
			return nil, err
		}
		return json.Marshal(value)
	}
	r.Get("/gross-board/events", func(w http.ResponseWriter, r *http.Request) {
		week, err := grossBoardWeek(r.URL.Query().Get("weekStart"))
		if err != nil {
			writeAPIError(w, 400, err.Error())
			return
		}
		events.serve(auth, peers)(w, r.WithContext(context.WithValue(r.Context(), grossWeekContext{}, week)))
	})
	fail := func(w http.ResponseWriter, err error) {
		if errors.Is(err, repository.ErrGrossBoardConflict) {
			writeAPIError(w, 409, "This cell or tab is no longer available")
			return
		}
		logger.Error("gross board presence failed", "error", err)
		writeAPIError(w, 503, "Live presence is temporarily unavailable")
	}
	r.Get("/gross-board/presence", func(w http.ResponseWriter, r *http.Request) {
		week, err := grossBoardWeek(r.URL.Query().Get("weekStart"))
		if err != nil {
			writeAPIError(w, 400, err.Error())
			return
		}
		value, err := repo.Presence(r.Context(), week)
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, value)
	})
	r.Put("/gross-board/presence", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			repository.GrossBoardCell
			ClientID string `json:"clientId"`
		}
		if err := decodeJSON(r, &input); err != nil || !isUUID(input.ClientID) || !isUUID(input.DriverID) {
			writeAPIError(w, 400, "Valid tab and driver IDs are required")
			return
		}
		week, err := grossBoardWeek(input.WeekStart)
		valid := err == nil && input.Slot >= -1 && input.Slot <= 99
		switch input.Column {
		case "driver", "truck", "originalTotal", "driverTotal", "milesTotal", "balance":
			valid = valid && input.Date == "" && input.Slot == 0
		case "loadNumber", "originalRate", "driverRate", "miles", "note":
			date, e := time.Parse(time.DateOnly, input.Date)
			valid = valid && e == nil && !date.Before(week) && date.Before(week.AddDate(0, 0, 7))
		default:
			valid = false
		}
		if !valid {
			writeAPIError(w, 400, "A valid week, date, load slot and board field are required")
			return
		}
		session, _ := authSessionFromContext(r.Context())
		cookie, e := r.Cookie(sessionCookieName)
		if e != nil {
			writeAPIError(w, 401, "Session required")
			return
		}
		if err = repo.SetPresence(r.Context(), input.ClientID, session.User.ID, hashToken(cookie.Value), input.GrossBoardCell); err != nil {
			fail(w, err)
			return
		}
		w.WriteHeader(204)
	})
	r.Delete("/gross-board/presence/{clientId}", func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "clientId")
		if !isUUID(id) {
			writeAPIError(w, 400, "Invalid tab ID")
			return
		}
		session, _ := authSessionFromContext(r.Context())
		cookie, err := r.Cookie(sessionCookieName)
		if err != nil {
			writeAPIError(w, 401, "Session required")
			return
		}
		if err = repo.ClearPresence(r.Context(), id, session.User.ID, hashToken(cookie.Value)); err != nil {
			fail(w, err)
			return
		}
		w.WriteHeader(204)
	})
	return func() { events.shutdown(); peers.shutdown() }
}
