package repository

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func testDriverBoardOrder(t *testing.T, ctx context.Context, pool *pgxpool.Pool, repo *DriverBoardRepository, _ *FleetRepository) {
	t.Helper()
	var dispatcher, otherDispatcher string
	for _, dest := range []*string{&dispatcher, &otherDispatcher} {
		if err := pool.QueryRow(ctx, `INSERT INTO dispatchers(full_name,normalized_name) VALUES('Order dispatcher',gen_random_uuid()::text) RETURNING id`).Scan(dest); err != nil {
			t.Fatal(err)
		}
	}
	defer pool.Exec(ctx, `DELETE FROM dispatchers WHERE id=ANY($1::uuid[])`, []string{dispatcher, otherDispatcher})
	ids := []string{}
	for _, name := range []string{"Order Alpha", "Order Bravo", "Order Charlie"} {
		var id string
		if err := pool.QueryRow(ctx, `INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate,dispatcher_id) VALUES($1,$1,'cpm',0,$2) RETURNING id`, name, dispatcher).Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	defer pool.Exec(ctx, `DELETE FROM drivers WHERE id=ANY($1::uuid[])`, ids)
	get := func() DriverBoard {
		t.Helper()
		b, err := repo.Get(ctx, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	group := func(b DriverBoard, key string) []string {
		result := []string{}
		for _, d := range b.Drivers {
			if d.DispatcherID == key {
				result = append(result, d.ID)
			}
		}
		return result
	}
	b := get()
	if !slices.Equal(group(b, dispatcher), ids) {
		t.Fatal("initial alphabetical order", b.Drivers)
	}
	stale := b.DriverOrderRevisions[dispatcher]
	action := DriverBoardOrderAction{DispatcherID: dispatcher, DriverID: ids[2], TargetDriverID: ids[0], Revision: stale}
	result, err := repo.ReorderDrivers(ctx, action, "")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{ids[2], ids[0], ids[1]}
	b = get()
	if !slices.Equal(result.DriverIDs, want) || !slices.Equal(group(b, dispatcher), want) || b.DriverOrderRevisions[dispatcher] != result.Revision {
		t.Fatal("order not persisted", result, b.Drivers)
	}
	if _, err = repo.ReorderDrivers(ctx, action, ""); !errors.Is(err, ErrDriverBoardOrderConflict) {
		t.Fatal("stale reorder accepted", err)
	}
	action.Revision = result.Revision
	action.TargetDriverID = ids[1]
	action.After = true
	result, err = repo.ReorderDrivers(ctx, action, "")
	if err != nil || !slices.Equal(result.DriverIDs, ids) {
		t.Fatal("move after", result, err)
	}
	// A changed roster invalidates the gesture before it can overwrite another group.
	if _, err = pool.Exec(ctx, `UPDATE drivers SET dispatcher_id=$2 WHERE id=$1`, ids[1], otherDispatcher); err != nil {
		t.Fatal(err)
	}
	action.Revision = result.Revision
	if _, err = repo.ReorderDrivers(ctx, action, ""); !errors.Is(err, ErrDriverBoardOrderConflict) {
		t.Fatal("cross-dispatcher move accepted", err)
	}
	b = get()
	if !slices.Equal(group(b, dispatcher), []string{ids[0], ids[2]}) {
		t.Fatal("departed member remained", b.Drivers)
	}
	action.TargetDriverID = ids[0]
	action.After = false
	if _, err = repo.ReorderDrivers(ctx, action, ""); !errors.Is(err, ErrDriverBoardOrderConflict) {
		t.Fatal("roster revision not fenced", err)
	}
	// New drivers append without disturbing the saved sequence, even if alphabetically first.
	var added string
	if err = pool.QueryRow(ctx, `INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate,dispatcher_id) VALUES('Order Aardvark',gen_random_uuid()::text,'cpm',0,$1) RETURNING id`, dispatcher).Scan(&added); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, `DELETE FROM drivers WHERE id=$1`, added)
	b = get()
	if !slices.Equal(group(b, dispatcher), []string{ids[0], ids[2], added}) {
		t.Fatal("new member did not append", b.Drivers)
	}
	// Unassigned is a real group too, with the same concurrency rules.
	if _, err = pool.Exec(ctx, `UPDATE drivers SET dispatcher_id=NULL WHERE id=ANY($1::uuid[])`, ids[:2]); err != nil {
		t.Fatal(err)
	}
	b = get()
	_, err = repo.ReorderDrivers(ctx, DriverBoardOrderAction{DispatcherID: "", DriverID: ids[1], TargetDriverID: ids[0], Revision: b.DriverOrderRevisions[""]}, "")
	if err != nil {
		t.Fatal("unassigned reorder", err)
	}
	defer pool.Exec(ctx, `DELETE FROM driver_board_driver_order WHERE dispatcher_key=''`)
	// Cross-group drops preserve prior history and become effective now, with
	// source/destination roster fences and no duplicate history on stale replay.
	b = get()
	destination := dispatcher
	transfer := DriverBoardOrderAction{DispatcherID: "", DriverID: ids[0], TargetDriverID: ids[2], TargetDispatcherID: &destination, Revision: b.DriverOrderRevisions[""], TargetRevision: b.DriverOrderRevisions[dispatcher]}
	var previousStart time.Time
	if err = pool.QueryRow(ctx, `SELECT assigned_at FROM driver_dispatcher_assignments WHERE driver_id=$1 AND unassigned_at IS NULL`, ids[0]).Scan(&previousStart); err != nil {
		t.Fatal(err)
	}
	var actor string
	if err = pool.QueryRow(ctx, `INSERT INTO app_users(username,password_hash,role_id) SELECT 'Board mover',crypt('test-only-password',gen_salt('bf')),id FROM app_roles WHERE system_role LIMIT 1 RETURNING id`).Scan(&actor); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, `DELETE FROM app_users WHERE id=$1`, actor)
	beforeMove := time.Now()
	result, err = repo.ReorderDrivers(ctx, transfer, actor)
	if err != nil {
		t.Fatal("cross-group drop", err)
	}
	b = get()
	if result.SourceRevision != b.DriverOrderRevisions[""] || result.Revision != b.DriverOrderRevisions[dispatcher] {
		t.Fatal("transfer response revisions", result, b.DriverOrderRevisions)
	}
	if !slices.Equal(result.DriverIDs, []string{ids[0], ids[2], added}) {
		t.Fatal("destination placement", result)
	}
	var assignedAt, oldStart, oldEnd time.Time
	var source, savedActor, ownerID string
	if err = pool.QueryRow(ctx, `SELECT assigned_at,source,actor_id,dispatcher_id::text FROM driver_dispatcher_assignments WHERE driver_id=$1 AND unassigned_at IS NULL`, ids[0]).Scan(&assignedAt, &source, &savedActor, &ownerID); err != nil {
		t.Fatal(err)
	}
	if assignedAt.Before(beforeMove) || assignedAt.After(time.Now()) || source != "Status Board drag" || savedActor != actor || ownerID != dispatcher {
		t.Fatal("immediate assignment attribution", assignedAt, source, savedActor, ownerID)
	}
	if err = pool.QueryRow(ctx, `SELECT assigned_at,unassigned_at FROM driver_dispatcher_assignments WHERE driver_id=$1 AND unassigned_at=$2`, ids[0], assignedAt).Scan(&oldStart, &oldEnd); err != nil || !oldStart.Equal(previousStart) || !oldEnd.Equal(assignedAt) {
		t.Fatal("historical interval changed", oldStart, oldEnd, err)
	}
	if _, err = repo.ReorderDrivers(ctx, transfer, actor); !errors.Is(err, ErrDriverBoardOrderConflict) {
		t.Fatal("replayed transfer accepted", err)
	}
	b = get()
	destination = ""
	transfer = DriverBoardOrderAction{DispatcherID: dispatcher, DriverID: ids[0], TargetDispatcherID: &destination, Revision: b.DriverOrderRevisions[dispatcher], TargetRevision: "stale"}
	if _, err = repo.ReorderDrivers(ctx, transfer, actor); !errors.Is(err, ErrDriverBoardOrderConflict) {
		t.Fatal("stale destination accepted", err)
	}
	transfer.TargetRevision = b.DriverOrderRevisions[""]
	if _, err = repo.ReorderDrivers(ctx, transfer, actor); err != nil {
		t.Fatal("unassign drop", err)
	}
	b = get()
	if b.Drivers[0].DispatcherID != "" {
		t.Fatal("Unassigned is not first")
	}
	if _, err = pool.Exec(ctx, `UPDATE dispatchers SET active=false WHERE id=$1`, dispatcher); err != nil {
		t.Fatal(err)
	}
	destination = dispatcher
	transfer = DriverBoardOrderAction{DispatcherID: "", DriverID: ids[0], TargetDispatcherID: &destination, Revision: b.DriverOrderRevisions[""], TargetRevision: b.DriverOrderRevisions[dispatcher]}
	if _, err = repo.ReorderDrivers(ctx, transfer, actor); !errors.Is(err, ErrDriverBoardOrderConflict) {
		t.Fatal("inactive destination accepted", err)
	}
	// Order changes never edit dispatch fields or their versions.
	var count int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM driver_board WHERE driver_id=ANY($1::uuid[])`, ids).Scan(&count); err != nil || count != 0 {
		t.Fatal("reorder changed dispatch entries", count, err)
	}
	var owner string
	if err = pool.QueryRow(ctx, `SELECT tableowner FROM pg_tables WHERE schemaname=current_schema() AND tablename='driver_board_driver_order'`).Scan(&owner); err != nil || owner != "mserp_app" {
		t.Fatal("order ownership", owner, err)
	}

	t.Run("shared auto-order", func(t *testing.T) {
		exec := func(query string, args ...any) {
			t.Helper()
			if _, err := pool.Exec(ctx, query, args...); err != nil {
				t.Fatal(err)
			}
		}
		exec(`UPDATE dispatchers SET active=true WHERE id=$1`, dispatcher)
		exec(`UPDATE drivers SET dispatcher_id=$1 WHERE id=ANY($2::uuid[])`, dispatcher, ids)
		exec(`INSERT INTO driver_board(driver_id,status) VALUES ($1,'HOME'),($2,'DISPATCHED'),($3,'READY'),($4,'REST')`, ids[0], ids[1], ids[2], added)
		if err := repo.SetDriverAutoOrder(ctx, dispatcher, true, actor); err != nil {
			t.Fatal(err)
		}
		current := get()
		want := []string{ids[2], ids[1], added, ids[0]}
		if !current.AutoOrderDispatchers[dispatcher] || current.AutoOrderDispatchers[otherDispatcher] || !slices.Equal(group(current, dispatcher), want) {
			t.Fatal("shared auto-order not applied", group(current, dispatcher), current.AutoOrderDispatchers)
		}
		move := DriverBoardOrderAction{DispatcherID: dispatcher, DriverID: ids[1], TargetDriverID: ids[2], Revision: current.DriverOrderRevisions[dispatcher]}
		if _, err := repo.ReorderDrivers(ctx, move, actor); !errors.Is(err, ErrDriverBoardAutoOrder) {
			t.Fatal("manual reorder was not blocked", err)
		}
		if err := repo.SetDriverAutoOrder(ctx, otherDispatcher, true, actor); err != nil {
			t.Fatal(err)
		}
		current = get()
		if current.AutoOrderGroupNames[otherDispatcher] != "Order dispatcher" || current.DriverOrderRevisions[otherDispatcher] == "" {
			t.Fatal("empty Auto-order destination is missing", current.AutoOrderGroupNames)
		}
		transfer := DriverBoardOrderAction{DispatcherID: dispatcher, DriverID: ids[1], TargetDispatcherID: &otherDispatcher, Revision: move.Revision, TargetRevision: current.DriverOrderRevisions[otherDispatcher]}
		result, err := repo.ReorderDrivers(ctx, transfer, actor)
		if err != nil || !slices.Equal(result.DriverIDs, []string{ids[1]}) {
			t.Fatal("transfer between Auto-order groups", result, err)
		}
		current = get()
		if !slices.Equal(group(current, dispatcher), []string{ids[2], added, ids[0]}) || !slices.Equal(group(current, otherDispatcher), result.DriverIDs) {
			t.Fatal("transferred driver did not change groups", current.Drivers)
		}
		back := DriverBoardOrderAction{DispatcherID: otherDispatcher, DriverID: ids[1], TargetDriverID: ids[0], After: true, TargetDispatcherID: &dispatcher, Revision: result.Revision, TargetRevision: move.Revision}
		if _, err := repo.ReorderDrivers(ctx, back, actor); !errors.Is(err, ErrDriverBoardOrderConflict) {
			t.Fatal("stale Auto-order destination accepted", err)
		}
		back.TargetRevision = current.DriverOrderRevisions[dispatcher]
		result, err = repo.ReorderDrivers(ctx, back, actor)
		current = get()
		if err != nil || !slices.Equal(result.DriverIDs, want) || !slices.Equal(group(current, dispatcher), want) ||
			result.Revision != current.DriverOrderRevisions[dispatcher] || result.SourceRevision != current.DriverOrderRevisions[otherDispatcher] {
			t.Fatal("transfer did not return the status-sorted placement and saved roster revisions", result, err)
		}
		move.Revision = result.Revision

		exec(`UPDATE driver_board SET status='READY' WHERE driver_id=$1`, added)
		exec(`UPDATE driver_board SET status='REST' WHERE driver_id=$1`, ids[2])
		want = []string{added, ids[1], ids[2], ids[0]}
		if got := group(get(), dispatcher); !slices.Equal(got, want) {
			t.Fatal("status changes did not update the order", got)
		}
		if err := repo.SetDriverAutoOrder(ctx, dispatcher, false, actor); err != nil {
			t.Fatal(err)
		}
		exec(`UPDATE driver_board SET status='HOME' WHERE driver_id=$1`, added)
		frozen := get()
		if frozen.AutoOrderDispatchers[dispatcher] || !slices.Equal(group(frozen, dispatcher), want) {
			t.Fatal("disabling did not retain the current order", group(frozen, dispatcher))
		}
		if _, err := repo.ReorderDrivers(ctx, move, actor); !errors.Is(err, ErrDriverBoardOrderConflict) {
			t.Fatal("a drag from before the mode change was accepted", err)
		}
		move.Revision = frozen.DriverOrderRevisions[dispatcher]
		move.TargetDriverID = added
		if result, err := repo.ReorderDrivers(ctx, move, actor); err != nil || !slices.Equal(result.DriverIDs, []string{ids[1], added, ids[2], ids[0]}) {
			t.Fatal("manual reorder did not resume", result, err)
		}
	})
}
