package repository

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"strings"
	"testing"
	"time"
)

func TestUpdaterDatabase(t *testing.T) {
	dsn := os.Getenv("MSERP_DRIVER_BOARD_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set disposable local MSERP_DRIVER_BOARD_TEST_DATABASE_URL")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.Contains(cfg.ConnConfig.Database, "_test") || (cfg.ConnConfig.Host != "127.0.0.1" && cfg.ConnConfig.Host != "localhost") {
		t.Fatal("local _test database required")
	}
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(ctx)
	source, err := os.ReadFile("../../sql/init.sql")
	if err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile("../../sql/049_dispatcher_updaters.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"fresh", "migrated"} {
		t.Run(mode, func(t *testing.T) {
			schema := fmt.Sprintf("updater_test_%d", time.Now().UnixNano())
			quoted := pgx.Identifier{schema}.Sanitize()
			exec := func(sql string) {
				t.Helper()
				if _, err := admin.Exec(ctx, sql); err != nil {
					t.Fatal(err)
				}
			}
			exec(`CREATE SCHEMA ` + quoted + `; SET search_path TO ` + quoted + `,public; GRANT USAGE ON SCHEMA ` + quoted + ` TO mserp_app`)
			defer admin.Exec(ctx, `SET search_path TO public; DROP SCHEMA `+quoted+` CASCADE`)
			if mode == "fresh" {
				exec(string(source))
			} else {
				before, _, ok := strings.Cut(string(source), string(migration))
				if !ok {
					t.Fatal("migration differs from init")
				}
				exec(before)
				exec(string(migration))
			}
			exec(`GRANT SELECT,INSERT,UPDATE,DELETE ON ALL TABLES IN SCHEMA ` + quoted + ` TO mserp_app`)
			appcfg := cfg.Copy()
			appcfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
			appcfg.ConnConfig.RuntimeParams["role"] = "mserp_app"
			pool, err := pgxpool.NewWithConfig(ctx, appcfg)
			if err != nil {
				t.Fatal(err)
			}
			defer pool.Close()
			repo := NewFleetRepository(pool)
			ext := 106
			main, err := repo.SaveUpdater(ctx, "", UpdaterInput{FullName: "Roy", Shift: "main", Extension: &ext})
			if err != nil {
				t.Fatal(err)
			}
			after, err := repo.SaveUpdater(ctx, "", UpdaterInput{FullName: "Tim", Shift: "after_hours"})
			if err != nil {
				t.Fatal(err)
			}
			input := DispatcherInput{FullName: "First", Active: true, Extension: &ext, ExtensionSet: true, Updaters: &UpdaterAssignments{MainUpdaterID: &main.ID, AfterHoursUpdaterID: &after.ID}}
			first, err := repo.CreateDispatcher(ctx, input)
			if err != nil {
				t.Fatal(err)
			}
			input.FullName = "Second"
			second, err := repo.CreateDispatcher(ctx, input)
			if err != nil {
				t.Fatal(err)
			}
			if first.Extension == nil || *first.Extension != 106 || *second.MainUpdaterID != main.ID {
				t.Fatal("assignment or extension missing")
			}
			list, err := repo.ListUpdaters(ctx)
			if err != nil || len(list) != 2 || len(list[0].DispatcherNames) != 2 {
				t.Fatalf("shared assignments: %v %v", list, err)
			}
			input.FullName = "First"
			input.Updaters.MainUpdaterID = &after.ID
			if _, err = repo.UpdateDispatcher(ctx, first.ID, input); err == nil {
				t.Fatal("wrong shift accepted")
			}
			got, err := repo.GetDispatcher(ctx, first.ID)
			if err != nil || got.MainUpdaterID == nil || *got.MainUpdaterID != main.ID {
				t.Fatal("failed save lost assignment", err)
			}
			if _, err = repo.SaveUpdater(ctx, main.ID, UpdaterInput{FullName: "Roy", Shift: "after_hours", Version: main.Version}); err != ErrUpdaterConflict {
				t.Fatal("assigned shift change accepted", err)
			}
			renamed, err := repo.SaveUpdater(ctx, main.ID, UpdaterInput{FullName: "Roy Updated", Shift: "main", Extension: &ext, Version: main.Version})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = repo.SaveUpdater(ctx, main.ID, UpdaterInput{FullName: "Stale", Shift: "main", Version: main.Version}); err != ErrUpdaterConflict {
				t.Fatal("stale updater accepted", err)
			}
			legacy, err := repo.UpdateDispatcher(ctx, first.ID, DispatcherInput{FullName: "First", Active: true})
			if err != nil || legacy.Extension == nil || legacy.MainUpdaterID == nil {
				t.Fatal("legacy write erased new fields", err)
			}
			exec(fmt.Sprintf(`INSERT INTO drivers(full_name,normalized_name,dispatcher_id,active,pay_type,pay_rate) VALUES('Board Driver','board driver','%s',true,'cpm',0)`, first.ID))
			board, err := NewDriverBoardRepository(pool).Get(ctx, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			if len(board.Drivers) != 1 || board.Drivers[0].MainUpdaterName != renamed.FullName || board.Drivers[0].AfterHoursUpdaterName != "Tim" {
				t.Fatal("board updater projection missing")
			}
			if err = repo.DeleteUpdater(ctx, main.ID); err != nil {
				t.Fatal(err)
			}
			got, err = repo.GetDispatcher(ctx, first.ID)
			if err != nil || got.MainUpdaterID != nil || got.AfterHoursUpdaterID == nil {
				t.Fatal("delete did not clear only matching slot", err)
			}
		})
	}
}
