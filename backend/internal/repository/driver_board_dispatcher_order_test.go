package repository

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func testDispatcherBoardOrder(t *testing.T, ctx context.Context, pool *pgxpool.Pool, repo *DriverBoardRepository) {
	t.Helper()
	dispatchers := []string{}
	drivers := []string{}
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM drivers WHERE id=ANY($1::uuid[])`, drivers)
		_, _ = pool.Exec(ctx, `DELETE FROM dispatchers WHERE id=ANY($1::uuid[])`, dispatchers)
		_, _ = pool.Exec(ctx, `DELETE FROM driver_board_dispatcher_order WHERE id=true`)
	}()
	for _, name := range []string{"Order A", "Order B", "Order C"} {
		var dispatcher, driver string
		if err := pool.QueryRow(ctx, `INSERT INTO dispatchers(full_name,normalized_name) VALUES($1,gen_random_uuid()::text) RETURNING id`, name).Scan(&dispatcher); err != nil {
			t.Fatal(err)
		}
		dispatchers = append(dispatchers, dispatcher)
		if err := pool.QueryRow(ctx, `INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate,dispatcher_id) VALUES($1,gen_random_uuid()::text,'cpm',0,$2) RETURNING id`, name+" Driver", dispatcher).Scan(&driver); err != nil {
			t.Fatal(err)
		}
		drivers = append(drivers, driver)
	}
	get := func() DriverBoard {
		b, err := repo.Get(ctx, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	b := get()
	if !slices.Equal(boardDispatcherIDs(b.Drivers), dispatchers) {
		t.Fatal("initial dispatcher order", boardDispatcherIDs(b.Drivers))
	}
	action := DispatcherBoardOrderAction{DispatcherID: dispatchers[2], TargetDispatcherID: dispatchers[0], Revision: b.DispatcherOrderRevision}
	result, err := repo.ReorderDispatchers(ctx, action, "")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{dispatchers[2], dispatchers[0], dispatchers[1]}
	b = get()
	if !slices.Equal(result.DispatcherIDs, want) || !slices.Equal(boardDispatcherIDs(b.Drivers), want) || b.DispatcherOrderRevision != result.Revision {
		t.Fatal("dispatcher order not persisted", result, boardDispatcherIDs(b.Drivers))
	}
	if _, err = repo.ReorderDispatchers(ctx, action, ""); !errors.Is(err, ErrDriverBoardOrderConflict) {
		t.Fatal("stale reorder accepted", err)
	}
	var added string
	if err = pool.QueryRow(ctx, `INSERT INTO dispatchers(full_name,normalized_name) VALUES('Order AA',gen_random_uuid()::text) RETURNING id`).Scan(&added); err != nil {
		t.Fatal(err)
	}
	dispatchers = append(dispatchers, added)
	var addedDriver string
	if err = pool.QueryRow(ctx, `INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate,dispatcher_id) VALUES('Order AA Driver',gen_random_uuid()::text,'cpm',0,$1) RETURNING id`, added).Scan(&addedDriver); err != nil {
		t.Fatal(err)
	}
	drivers = append(drivers, addedDriver)
	b = get()
	if !slices.Equal(boardDispatcherIDs(b.Drivers), append(want, added)) {
		t.Fatal("new dispatcher did not append", boardDispatcherIDs(b.Drivers))
	}
	action.Revision = result.Revision
	if _, err = repo.ReorderDispatchers(ctx, action, ""); !errors.Is(err, ErrDriverBoardOrderConflict) {
		t.Fatal("roster change was not fenced", err)
	}
	var owner string
	if err = pool.QueryRow(ctx, `SELECT tableowner FROM pg_tables WHERE schemaname=current_schema() AND tablename='driver_board_dispatcher_order'`).Scan(&owner); err != nil || owner != "mserp_app" {
		t.Fatal("dispatcher order ownership", owner, err)
	}
}
