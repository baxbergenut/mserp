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

func TestLegacyGrossBoardDayStatus(t *testing.T) {
	for input, want := range map[string]string{"ENROUTE": "IN TRANSIT", "REST": "RESET", "HOME": "HOME", "SHOP": "SHOP", "": ""} {
		if got := LegacyGrossBoardDayStatus(input); got != want {
			t.Fatal(input, got, want)
		}
	}
}

func TestStatusPaletteRollbackDatabase(t *testing.T) {
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
	migration, err := os.ReadFile("../../sql/172_restore_status_board_palette.sql")
	if err != nil {
		t.Fatal(err)
	}
	prefix, _, ok := strings.Cut(strings.ReplaceAll(string(init), "\r\n", "\n"), strings.ReplaceAll(string(migration), "\r\n", "\n"))
	if !ok {
		t.Fatal("init must contain exact rollback migration")
	}
	for _, mode := range []string{"fresh", "upgrade"} {
		t.Run(mode, func(t *testing.T) {
			schema := fmt.Sprintf("status_palette_rollback_%d", time.Now().UnixNano())
			quoted := pgx.Identifier{schema}.Sanitize()
			exec := func(query string, args ...any) {
				t.Helper()
				if _, e := admin.Exec(ctx, query, args...); e != nil {
					t.Fatal(e)
				}
			}
			exec(`CREATE SCHEMA ` + quoted + `;SET search_path TO ` + quoted + `,public;GRANT USAGE ON SCHEMA ` + quoted + ` TO mserp_app`)
			defer func() { _, _ = admin.Exec(ctx, `SET search_path TO public;DROP SCHEMA `+quoted+` CASCADE`) }()
			if mode == "fresh" {
				exec(string(init))
			} else {
				exec(prefix)
			}
			exec(`INSERT INTO status_color_history(version,colors) VALUES(99,'{"home":"#ff0000"}');
 INSERT INTO drivers(id,full_name,normalized_name,pay_type,pay_rate,active) VALUES('00000000-0000-0000-0000-000000000172','Rollback driver','rollback driver','cpm',0,true);
 INSERT INTO gross_board_entries(driver_id,service_date,day_status) VALUES('00000000-0000-0000-0000-000000000172','2026-10-01','ENROUTE'),('00000000-0000-0000-0000-000000000172','2026-10-02','REST')`)
			if mode == "upgrade" {
				exec(string(migration))
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
			catalog, e := NewFleetRepository(pool).FleetStatusCatalog(ctx)
			if e != nil || len(catalog.Rules) != 23 {
				t.Fatal("original catalog", len(catalog.Rules), e)
			}
			for _, restored := range []string{"At PU", "At DEL", "HOME (YARD)", "34 RESET"} {
				found := false
				for _, rule := range catalog.Rules {
					if rule.BoardStatus == restored {
						found = true
					}
				}
				if !found {
					t.Fatal("missing original choice", restored)
				}
			}
			if _, e = pool.Exec(ctx, `UPDATE drivers SET availability_status='reset' WHERE id='00000000-0000-0000-0000-000000000172';INSERT INTO driver_board(driver_id,status) VALUES('00000000-0000-0000-0000-000000000172','At PU')`); e != nil {
				t.Fatal(e)
			}
			var availability, label, board string
			var history, aliases int
			e = pool.QueryRow(ctx, `SELECT d.availability_status,fleet_status_personal_label(d.availability_status),b.status,(SELECT count(*) FROM status_color_history WHERE version=99),(SELECT count(*) FROM gross_board_entries WHERE day_status IN ('ENROUTE','REST')) FROM drivers d JOIN driver_board b ON b.driver_id=d.id WHERE d.id='00000000-0000-0000-0000-000000000172'`).Scan(&availability, &label, &board, &history, &aliases)
			if e != nil || availability != "reset" || label != "34 RESET" || board != "At PU" || history != 1 || aliases != 2 {
				t.Fatal("rollback must restore commands and preserve source/audits", availability, label, board, history, aliases, e)
			}
		})
	}
}
