package repository

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"testing"
)

// Called against both a fresh schema and numbered migrations, as mserp_app.
func testDriverBoardHistory(t *testing.T, ctx context.Context, pool *pgxpool.Pool, repo *DriverBoardRepository, fleet *FleetRepository) {
	t.Helper()
	var actor string
	if err := pool.QueryRow(ctx, `INSERT INTO app_users(username,password_hash) VALUES('Board updater',crypt('test-only-password',gen_salt('bf'))) RETURNING id`).Scan(&actor); err != nil {
		t.Fatal(err)
	}
	input := DriverInput{FullName: "Audit Driver", PayType: "cpm", PayRate: 0.65, Active: true, ChargeActor: actor}
	driver, err := fleet.CreateDriver(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	history := func() DriverBoardHistory {
		t.Helper()
		h, err := repo.History(ctx, []string{driver.ID}, 0)
		if err != nil {
			t.Fatal(err)
		}
		return h
	}
	if len(history().Items) != 0 {
		t.Fatal("invented initial history")
	}
	e := DriverBoardEntry{DriverID: driver.ID, CurrentLoad: "Load A", Status: "ENROUTE"}
	save := func() {
		t.Helper()
		rows, err := repo.Save(ctx, []DriverBoardEntry{e}, actor)
		if err != nil {
			t.Fatal(err)
		}
		e = rows[0]
	}
	save()
	h := history()
	if len(h.Items) != 1 || len(h.Items[0].After) != 2 || h.Items[0].ActorID != actor || h.Items[0].ActorName != "Board updater" || h.Items[0].Source != "board" {
		t.Fatalf("grouped audit: %+v", h)
	}
	first := h.Items[0].ID
	save()
	if len(history().Items) != 1 {
		t.Fatal("no-op created history")
	}
	if _, err = repo.Undo(ctx, first, driver.ID, e.Version-1, actor); !errors.Is(err, ErrDriverBoardConflict) {
		t.Fatal("stale undo accepted", err)
	}
	e.Notes = "Keep this later note"
	save()
	e, err = repo.Undo(ctx, first, driver.ID, e.Version, actor)
	if err != nil || e.CurrentLoad != "" || e.Status != "" || e.Notes != "Keep this later note" {
		t.Fatal("undo must preserve unrelated newer fields", e, err)
	}
	h = history()
	if h.Items[0].UndoOf == nil || *h.Items[0].UndoOf != first || h.Items[0].Source != "undo" {
		t.Fatal("undo audit missing", h)
	}
	if _, err = repo.Undo(ctx, first, driver.ID, e.Version, actor); !errors.Is(err, ErrDriverBoardConflict) {
		t.Fatal("repeat undo accepted", err)
	}
	// Profile address changes cannot be overwritten by board autosaves or undo.
	city := "Boston"
	input.City = &city
	if _, err = fleet.UpdateDriver(ctx, driver.ID, input); err != nil {
		t.Fatal(err)
	}
	// Returning to the original value does not make an older event safe to undo.
	e.ETA = "Friday"
	save()
	etaID := history().Items[0].ID
	e.ETA = "Saturday"
	save()
	e.ETA = "Friday"
	save()
	if _, err = repo.Undo(ctx, etaID, driver.ID, e.Version, actor); !errors.Is(err, ErrDriverBoardConflict) {
		t.Fatal("ABA undo accepted", err)
	}
	count := len(history().Items)
	// Trailer notes survive reads and unrelated edits, and are independently audited.
	e.TrailerNumber = "TR-300"
	e.TrailerNote = "Door seal 42"
	save()
	noteEvent := history().Items[0]
	if noteEvent.After["trailerNote"] != "Door seal 42" || noteEvent.ActorID != actor {
		t.Fatal("trailer note attribution missing", noteEvent)
	}
	board, err := repo.Get(ctx, repo.now())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, row := range board.Entries {
		if row.DriverID == driver.ID {
			found = row.TrailerNote == e.TrailerNote
		}
	}
	if !found {
		t.Fatal("trailer note missing from board read")
	}
	filtered, err := repo.History(ctx, []string{driver.ID}, 0, "trailerNote")
	if err != nil || len(filtered.Items) != 1 || filtered.Items[0].ID != noteEvent.ID {
		t.Fatal("cell history filter", filtered, err)
	}
	all, err := repo.History(ctx, nil, 0, "trailerNote")
	if err != nil || len(all.Items) == 0 || all.Items[0].ID != noteEvent.ID {
		t.Fatal("whole-board history", all, err)
	}
	if _, err = repo.Undo(ctx, noteEvent.ID, driver.ID, e.Version, "another-user", true); !errors.Is(err, ErrNotFound) {
		t.Fatal("personal undo accepted another actor", err)
	}
	e, err = repo.Undo(ctx, noteEvent.ID, driver.ID, e.Version, actor, true)
	if err != nil || e.TrailerNumber != "" || e.TrailerNote != "" {
		t.Fatal("trailer note undo", e, err)
	}
	count = len(history().Items)
	stale := e
	stale.Version--
	if _, err = repo.Save(ctx, []DriverBoardEntry{stale}, actor); !errors.Is(err, ErrDriverBoardConflict) {
		t.Fatal(err)
	}
	if len(history().Items) != count {
		t.Fatal("failed save leaked audit")
	}
	for _, sql := range []string{`DELETE FROM driver_board_history WHERE driver_id=$1`, `UPDATE driver_board_history SET actor_name='rewritten' WHERE driver_id=$1`} {
		if _, err = pool.Exec(ctx, sql, driver.ID); err == nil {
			t.Fatal("history mutated")
		}
	}
	for i := 0; i < 52; i++ {
		e.Notes = fmt.Sprintf("Note %d", i)
		save()
	}
	h = history()
	if len(h.Items) != 50 || h.NextCursor == 0 {
		t.Fatal("history pagination", h)
	}
	older, err := repo.History(ctx, []string{driver.ID}, h.NextCursor)
	if err != nil || len(older.Items) == 0 || older.Items[0].ID >= h.NextCursor {
		t.Fatal("cursor pagination", err)
	}
	// No identity FK: history is retained independently of profile deletion.
	if _, err = pool.Exec(ctx, `DELETE FROM drivers WHERE id=$1`, driver.ID); err != nil {
		t.Fatal(err)
	}
	if len(history().Items) != 50 {
		t.Fatal("deleted driver lost history")
	}
	if _, err = pool.Exec(ctx, `DELETE FROM app_users WHERE id=$1`, actor); err != nil {
		t.Fatal(err)
	}
	if history().Items[0].ActorName != "Board updater" {
		t.Fatal("deleted actor lost identity snapshot")
	}
}
