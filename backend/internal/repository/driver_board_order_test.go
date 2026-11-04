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
	// Old saved order no longer changes presentation, even for new viewers.
	if _, err := pool.Exec(ctx, `INSERT INTO driver_board_driver_order(dispatcher_key,dispatcher_id,driver_ids) VALUES($1,$1::uuid,$2)`, dispatcher, []string{ids[2], ids[0], ids[1]}); err != nil {
		t.Fatal(err)
	}
	b = get()
	if !slices.Equal(group(b, dispatcher), ids) {
		t.Fatal("legacy order affected presentation", b.Drivers)
	}
	action := DriverBoardOrderAction{DispatcherID: dispatcher, DriverID: ids[2], TargetDriverID: ids[0], Revision: b.DriverOrderRevisions[dispatcher]}
	if _, err := repo.ReorderDrivers(ctx, action, ""); !errors.Is(err, ErrDriverBoardOrderLocal) {
		t.Fatal("shared reorder accepted", err)
	}
	var added string
	if err := pool.QueryRow(ctx, `INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate,dispatcher_id) VALUES('Order Aardvark',gen_random_uuid()::text,'cpm',0,$1) RETURNING id`, dispatcher).Scan(&added); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, `DELETE FROM drivers WHERE id=$1`, added)
	if !slices.Equal(group(get(), dispatcher), []string{added, ids[0], ids[1], ids[2]}) {
		t.Fatal("default order is not alphabetical")
	}
	if _, err := pool.Exec(ctx, `UPDATE drivers SET dispatcher_id=NULL WHERE id=ANY($1::uuid[])`, ids[:2]); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, `DELETE FROM driver_board_driver_order WHERE dispatcher_key=''`)
	var err error
	var result DriverBoardOrder
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
}
