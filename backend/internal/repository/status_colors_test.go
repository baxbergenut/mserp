package repository

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestStatusColorValidation(t *testing.T) {
	for _, s := range []StatusColors{{nil, 1}, {map[string]string{"unknown": "#123456"}, 1}, {map[string]string{"home": "red"}, 1}, {map[string]string{"home": "#123456"}, 0}, {map[string]string{"home.bg.fg": "#123456"}, 1}, {map[string]string{"unknown.fg": "#123456"}, 1}, {map[string]string{"home.bg": "url(evil)"}, 1}} {
		if validStatusColors(s) {
			t.Fatalf("invalid settings accepted: %+v", s)
		}
	}
	if !validStatusColors(StatusColors{map[string]string{}, 1}) {
		t.Fatal("reset to defaults rejected")
	}
	if !validStatusColors(StatusColors{map[string]string{"driver_ready": "#ABCDEF", "truck_ready": "#000000", "home.fg": "#FFFFFF", "home.bg": "#ff0000"}, 1}) {
		t.Fatal("valid settings rejected")
	}
}

func TestStatusColorsDatabase(t *testing.T) {
	dsn := os.Getenv("MSERP_DRIVER_BOARD_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("local disposable MSERP_DRIVER_BOARD_TEST_DATABASE_URL required")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.HasSuffix(cfg.ConnConfig.Database, "_test") || (cfg.ConnConfig.Host != "localhost" && cfg.ConnConfig.Host != "127.0.0.1") {
		t.Fatal("local _test database required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(ctx)
	init, err := os.ReadFile("../../sql/init.sql")
	if err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile("../../sql/171_status_colors.sql")
	if err != nil {
		t.Fatal(err)
	}
	prefix, _, ok := strings.Cut(strings.ReplaceAll(string(init), "\r\n", "\n"), strings.ReplaceAll(string(migration), "\r\n", "\n"))
	if !ok {
		t.Fatal("init must contain exact migration")
	}
	for _, mode := range []string{"fresh", "migration"} {
		t.Run(mode, func(t *testing.T) {
			schema := fmt.Sprintf("status_colors_%d", time.Now().UnixNano())
			quoted := pgx.Identifier{schema}.Sanitize()
			exec := func(sql string, args ...any) {
				t.Helper()
				if _, e := admin.Exec(ctx, sql, args...); e != nil {
					t.Fatal(e)
				}
			}
			exec(`CREATE SCHEMA ` + quoted + `;SET search_path TO ` + quoted + `,public;GRANT USAGE ON SCHEMA ` + quoted + ` TO mserp_app`)
			defer func() { _, _ = admin.Exec(ctx, `SET search_path TO public;DROP SCHEMA `+quoted+` CASCADE`) }()
			if mode == "fresh" {
				exec(string(init))
			} else {
				exec(prefix)
				exec(`INSERT INTO drivers(id,full_name,normalized_name,pay_type,pay_rate,active,availability_status) VALUES('00000000-0000-0000-0000-000000000171','Rest Driver','rest driver','cpm',0,true,'reset');
    INSERT INTO driver_board(driver_id,status,eta,notes) VALUES('00000000-0000-0000-0000-000000000171','34 RESET','13:00','Keep notes');
    INSERT INTO gross_board_entries(driver_id,service_date,day_status) VALUES('00000000-0000-0000-0000-000000000171','2026-10-01','IN TRANSIT')`)
				exec(string(migration))
				var label, availability, eta, note, day string
				err = admin.QueryRow(ctx, `SELECT b.status,d.availability_status,b.eta,b.notes,g.day_status FROM drivers d JOIN driver_board b ON b.driver_id=d.id JOIN gross_board_entries g ON g.driver_id=d.id`).Scan(&label, &availability, &eta, &note, &day)
				if err != nil || label != "REST" || availability != "rest" || eta != "13:00" || note != "Keep notes" || day != "IN TRANSIT" {
					t.Fatalf("migration lost data: %s %s %s %s %s %v", label, availability, eta, note, day, err)
				}
			}
			var retired int
			if err = admin.QueryRow(ctx, `SELECT count(*) FROM fleet_status_rules WHERE board_status IN ('At PU','At DEL','HOME (YARD)','34 RESET')`).Scan(&retired); err != nil || retired != 0 {
				t.Fatal("retired catalog choices", retired, err)
			}
			exec(`GRANT SELECT,INSERT,UPDATE,DELETE ON ALL TABLES IN SCHEMA ` + quoted + ` TO mserp_app;GRANT USAGE,SELECT ON ALL SEQUENCES IN SCHEMA ` + quoted + ` TO mserp_app`)
			app := cfg.Copy()
			app.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
			app.ConnConfig.RuntimeParams["role"] = "mserp_app"
			pool, e := pgxpool.NewWithConfig(ctx, app)
			if e != nil {
				t.Fatal(e)
			}
			defer pool.Close()
			repo := NewFleetRepository(pool)
			original, e := repo.StatusColors(ctx)
			if e != nil || original.Version != 1 || len(original.Colors) != 0 {
				t.Fatal(original, e)
			}
			saved, e := repo.SaveStatusColors(ctx, StatusColors{map[string]string{"home": "#ABCDEF", "driver_ready": "#ff0000", "home.fg": "#FEDCBA", "home.bg": "#123456"}, original.Version}, "")
			if e != nil || saved.Version != 2 || saved.Colors["home"] != "#abcdef" || saved.Colors["home.fg"] != "#fedcba" || saved.Colors["home.bg"] != "#123456" {
				t.Fatal(saved, e)
			}
			if _, e = repo.SaveStatusColors(ctx, original, ""); !errors.Is(e, ErrStatusColorsConflict) {
				t.Fatal("stale save must conflict", e)
			}
			if _, e = repo.SaveStatusColors(ctx, StatusColors{map[string]string{"home": "url(evil)"}, 2}, ""); e == nil {
				t.Fatal("unsafe color accepted")
			}
			reset, e := repo.SaveStatusColors(ctx, StatusColors{map[string]string{}, 2}, "")
			if e != nil || reset.Version != 3 || len(reset.Colors) != 0 {
				t.Fatal(reset, e)
			}
			var count int
			if e = pool.QueryRow(ctx, `SELECT count(*) FROM status_color_history`).Scan(&count); e != nil || count != 2 {
				t.Fatal("history", count, e)
			}
			if _, e = pool.Exec(ctx, `UPDATE status_color_history SET colors='{}' WHERE version=2`); e == nil {
				t.Fatal("history must be immutable")
			}
			// Direct/legacy writers and undo recovery cannot recreate retired states.
			driver, e := repo.CreateDriver(ctx, DriverInput{FullName: "Canonical Driver", PayType: "cpm", Active: true})
			if e != nil {
				t.Fatal(e)
			}
			for old, want := range map[string]string{"At PU": "DISPATCHED", "At DEL": "ENROUTE", "HOME (YARD)": "HOME", "34 RESET": "REST"} {
				if _, e = pool.Exec(ctx, `INSERT INTO driver_board(driver_id,status) VALUES($1,$2) ON CONFLICT(driver_id) DO UPDATE SET status=excluded.status`, driver.ID, old); e != nil {
					t.Fatal(e)
				}
				var got string
				if e = pool.QueryRow(ctx, `SELECT status FROM driver_board WHERE driver_id=$1`, driver.ID).Scan(&got); e != nil || got != want {
					t.Fatal(old, got, e)
				}
			}
		})
	}
}
