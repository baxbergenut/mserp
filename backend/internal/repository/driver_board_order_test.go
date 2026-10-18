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
	// Order changes never edit dispatch fields or their versions.
	var count int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM driver_board WHERE driver_id=ANY($1::uuid[])`, ids).Scan(&count); err != nil || count != 0 {
		t.Fatal("reorder changed dispatch entries", count, err)
	}
	var owner string
	if err = pool.QueryRow(ctx, `SELECT tableowner FROM pg_tables WHERE schemaname=current_schema() AND tablename='driver_board_driver_order'`).Scan(&owner); err != nil || owner != "mserp_app" {
		t.Fatal("order ownership", owner, err)
	}
}
