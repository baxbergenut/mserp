package repository

import (
	"context"
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
	// A legacy client's shared preference must no longer influence anyone's board.
	if _, err := pool.Exec(ctx, `INSERT INTO driver_board_dispatcher_order(id,dispatcher_ids) VALUES(true,$1)
 ON CONFLICT(id) DO UPDATE SET dispatcher_ids=EXCLUDED.dispatcher_ids`, []string{dispatchers[2], dispatchers[0], dispatchers[1]}); err != nil {
		t.Fatal(err)
	}
	b = get()
	if !slices.Equal(boardDispatcherIDs(b.Drivers), dispatchers) || b.DispatcherOrderRevision != "" {
		t.Fatal("retired shared preference affected the board", b)
	}
	var err error
	var owner string
	if err = pool.QueryRow(ctx, `SELECT tableowner FROM pg_tables WHERE schemaname=current_schema() AND tablename='driver_board_dispatcher_order'`).Scan(&owner); err != nil || owner != "mserp_app" {
		t.Fatal("dispatcher order ownership", owner, err)
	}
}
