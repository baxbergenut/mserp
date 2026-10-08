package repository

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"strings"
	"testing"
	"time"
)

func TestProfileNotesDatabase(t *testing.T) {
	dsn := os.Getenv("MSERP_ASSIGNMENT_HISTORY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set MSERP_ASSIGNMENT_HISTORY_TEST_DATABASE_URL to a disposable local _test database")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.Contains(cfg.ConnConfig.Database, "_test") {
		t.Fatal("a disposable _test database is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(ctx)
	for _, mode := range []string{"fresh", "migration"} {
		t.Run(mode, func(t *testing.T) {
			schema := fmt.Sprintf("profile_notes_%d", time.Now().UnixNano())
			quoted := pgx.Identifier{schema}.Sanitize()
			exec := func(query string, args ...any) {
				t.Helper()
				if _, err := admin.Exec(ctx, query, args...); err != nil {
					t.Fatal(err)
				}
			}
			exec(`CREATE SCHEMA ` + quoted + `; SET search_path TO ` + quoted + `,public`)
			defer func() { _, _ = admin.Exec(ctx, `SET search_path TO public; DROP SCHEMA `+quoted+` CASCADE`) }()
			init, e := os.ReadFile("../../sql/init.sql")
			if e != nil {
				t.Fatal(e)
			}
			migration, e := os.ReadFile("../../sql/063_profile_notes.sql")
			if e != nil {
				t.Fatal(e)
			}
			source := strings.ReplaceAll(string(init), "\r\n", "\n")
			m := strings.ReplaceAll(string(migration), "\r\n", "\n")
			if mode == "migration" {
				source = strings.Replace(source, m, "", 1)
				if strings.Contains(source, "CREATE TABLE profile_notes") {
					t.Fatal("failed to isolate notes migration")
				}
			}
			exec(source)
			var driver, truck, actor string
			for _, item := range []struct {
				query string
				dest  *string
			}{
				{`INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate,notes) VALUES('Notes Driver','notes driver','cpm',0.5,'Legacy context') RETURNING id`, &driver},
				{`INSERT INTO trucks(unit_number) VALUES('NOTE-1') RETURNING id`, &truck},
				{`INSERT INTO app_users(username,password_hash) VALUES('Notes author','$2test') RETURNING id`, &actor},
			} {
				if e = admin.QueryRow(ctx, item.query).Scan(item.dest); e != nil {
					t.Fatal(e)
				}
			}
			if mode == "migration" {
				exec(m)
			}
			exec(`GRANT USAGE ON SCHEMA ` + quoted + ` TO mserp_app; GRANT SELECT,INSERT,UPDATE,DELETE ON drivers,trucks,app_users,truck_driver_assignments TO mserp_app`)
			app := cfg.Copy()
			app.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
			app.ConnConfig.RuntimeParams["role"] = "mserp_app"
			pool, e := pgxpool.NewWithConfig(ctx, app)
			if e != nil {
				t.Fatal(e)
			}
			defer pool.Close()
			repo := NewFleetRepository(pool)
			id := "63000000-0000-0000-0000-000000000001"
			first, e := repo.AddProfileNote(ctx, driver, false, id, "First note", actor)
			if e != nil || first.ActorName != "Notes author" || first.CreatedAt.IsZero() {
				t.Fatalf("note: %+v %v", first, e)
			}
			retry, e := repo.AddProfileNote(ctx, driver, false, id, "First note", actor)
			if e != nil || retry != first {
				t.Fatalf("retry: %+v %v", retry, e)
			}
			if _, e = repo.AddProfileNote(ctx, truck, true, id, "First note", actor); !errors.Is(e, ErrProfileNoteConflict) {
				t.Fatalf("cross-profile UUID reuse: %v", e)
			}
			if _, e = repo.AddProfileNote(ctx, driver, false, id, "Changed", actor); !errors.Is(e, ErrProfileNoteConflict) {
				t.Fatalf("rewrote note: %v", e)
			}
			exec(`UPDATE app_users SET username='Renamed author' WHERE id=$1`, actor)
			rows, e := repo.ProfileNotes(ctx, driver, false)
			if e != nil || len(rows) != 1 || rows[0].ActorName != "Notes author" {
				t.Fatalf("snapshot: %+v %v", rows, e)
			}
			if _, e = repo.AddProfileNote(ctx, truck, true, "63000000-0000-0000-0000-000000000002", "Truck note", actor); e != nil {
				t.Fatal(e)
			}
			rows, e = repo.ProfileNotes(ctx, truck, true)
			if e != nil || len(rows) != 1 || rows[0].Body != "Truck note" {
				t.Fatalf("truck notes: %+v %v", rows, e)
			}
			var legacy string
			if e = pool.QueryRow(ctx, `SELECT notes FROM drivers WHERE id=$1`, driver).Scan(&legacy); e != nil || legacy != "Legacy context" {
				t.Fatal("legacy notes changed")
			}
			exec(`INSERT INTO truck_driver_assignments(truck_id,driver_id,assigned_at) VALUES($1,$2,'2026-10-05')`, truck, driver)
			history, e := repo.TruckAssignmentHistory(ctx, truck)
			if e != nil || len(history) != 1 || history[0].RelatedID == nil || *history[0].RelatedID != driver || history[0].Kind != "driver" {
				t.Fatalf("truck history: %+v %v", history, e)
			}
			if _, e = repo.ProfileNotes(ctx, "63000000-0000-0000-0000-000000000099", false); !errors.Is(e, ErrNotFound) {
				t.Fatalf("missing profile: %v", e)
			}
		})
	}
}
