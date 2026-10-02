package repository

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestAssignmentHistoryDatabase(t *testing.T) {
	dsn := os.Getenv("MSERP_ASSIGNMENT_HISTORY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set MSERP_ASSIGNMENT_HISTORY_TEST_DATABASE_URL to a disposable test database")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.Contains(cfg.ConnConfig.Database, "_test") {
		t.Fatal("a separate _test database is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal("test database unavailable")
	}
	defer admin.Close(ctx)
	for _, mode := range []string{"fresh", "migration"} {
		t.Run(mode, func(t *testing.T) {
			schema := fmt.Sprintf("assignment_history_test_%d", time.Now().UnixNano())
			quoted := pgx.Identifier{schema}.Sanitize()
			exec := func(sql string, args ...any) {
				t.Helper()
				if _, err := admin.Exec(ctx, sql, args...); err != nil {
					t.Fatal(err)
				}
			}
			exec(`CREATE SCHEMA ` + quoted + `; SET search_path TO ` + quoted + `,public`)
			exec(`GRANT USAGE ON SCHEMA ` + quoted + ` TO mserp_app`)
			defer func() { _, _ = admin.Exec(ctx, `SET search_path TO public; DROP SCHEMA `+quoted+` CASCADE`) }()
			source, err := os.ReadFile("../../sql/init.sql")
			if err != nil {
				t.Fatal(err)
			}
			if mode == "migration" {
				// Install the immediately preceding schema, then seed an existing link.
				before, _, ok := strings.Cut(string(source), "ALTER TABLE truck_driver_assignments ADD COLUMN source")
				if !ok {
					t.Fatal("missing assignment history schema boundary")
				}
				exec(before + "COMMIT;")
			} else {
				exec(string(source))
			}
			var first, second, driver, truck string
			for _, item := range []struct {
				name string
				dest *string
			}{{"First", &first}, {"Second", &second}} {
				if err := admin.QueryRow(ctx, `INSERT INTO dispatchers(full_name,normalized_name) VALUES($1,lower($1)) RETURNING id::text`, item.name).Scan(item.dest); err != nil {
					t.Fatal(err)
				}
			}
			if err := admin.QueryRow(ctx, `INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate,dispatcher_id) VALUES('Driver','driver','cpm',0.75,$1) RETURNING id::text`, first).Scan(&driver); err != nil {
				t.Fatal(err)
			}
			if err := admin.QueryRow(ctx, `INSERT INTO trucks(unit_number) VALUES('TEST-1') RETURNING id::text`).Scan(&truck); err != nil {
				t.Fatal(err)
			}
			if mode == "migration" {
				migration, err := os.ReadFile("../../sql/031_add_dispatcher_assignment_history.sql")
				if err != nil {
					t.Fatal(err)
				}
				exec(string(migration))
				applyLaterTestMigrations(t, ctx, admin, "031")
			}
			exec(`GRANT USAGE ON SCHEMA ` + quoted + ` TO mserp_app; GRANT SELECT,INSERT,UPDATE,DELETE ON drivers,dispatchers,trucks,truck_driver_assignments TO mserp_app`)
			appcfg := cfg.Copy()
			appcfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
			appcfg.ConnConfig.RuntimeParams["role"] = "mserp_app"
			pool, err := pgxpool.NewWithConfig(ctx, appcfg)
			if err != nil {
				t.Fatal(err)
			}
			defer pool.Close()
			repo := NewFleetRepository(pool)
			initial, err := repo.DriverAssignmentHistory(ctx, driver)
			if err != nil || len(initial) != 1 || initial[0].Name != "First" || initial[0].StartKnown != (mode == "fresh") {
				t.Fatalf("initial history: %+v %v", initial, err)
			}
			if _, err = pool.Exec(ctx, `UPDATE drivers SET dispatcher_id=dispatcher_id WHERE id=$1`, driver); err != nil {
				t.Fatal(err)
			}
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = tx.Exec(ctx, `UPDATE drivers SET dispatcher_id=$2 WHERE id=$1`, driver, second); err != nil {
				t.Fatal(err)
			}
			if err = tx.Rollback(ctx); err != nil {
				t.Fatal(err)
			}
			check, err := repo.DriverAssignmentHistory(ctx, driver)
			if err != nil || len(check) != 1 {
				t.Fatalf("no-op or rollback produced history: %+v %v", check, err)
			}
			if _, err = pool.Exec(ctx, `UPDATE drivers SET dispatcher_id=$2 WHERE id=$1`, driver, second); err != nil {
				t.Fatal(err)
			}
			if _, err = pool.Exec(ctx, `DELETE FROM dispatchers WHERE id=$1`, second); err != nil {
				t.Fatal(err)
			}
			check, err = repo.DriverAssignmentHistory(ctx, driver)
			if err != nil || len(check) != 3 || check[0].Name != "Unassigned" || check[0].UnassignedAt != nil || check[1].Name != "Second" || check[1].RelatedID != nil || check[1].UnassignedAt == nil {
				t.Fatalf("change/unlink/deletion: %+v %v", check, err)
			}
			for n := 0; n < 2; n++ {
				tx, err = pool.Begin(ctx)
				if err != nil {
					t.Fatal(err)
				}
				if err = assignTruck(ctx, tx, truck, driver); err != nil {
					_ = tx.Rollback(ctx)
					t.Fatal(err)
				}
				if err = tx.Commit(ctx); err != nil {
					t.Fatal(err)
				}
			}
			check, err = repo.DriverAssignmentHistory(ctx, driver)
			if err != nil || len(check) != 4 {
				t.Fatalf("same truck must not add a second assignment: %+v %v", check, err)
			}
			tx, err = pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if err = releaseDriverTruck(ctx, tx, driver); err != nil {
				_ = tx.Rollback(ctx)
				t.Fatal(err)
			}
			if err = tx.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			check, err = repo.DriverAssignmentHistory(ctx, driver)
			if err != nil || check[0].Kind != "truck" || check[0].UnassignedAt == nil {
				t.Fatalf("truck release: %+v %v", check, err)
			}
		})
	}
}
