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
	var lateMigrations []string
	for _, name := range []string{"180_payroll_revision_triggers.sql", "181_payroll_load_revision.sql", "182_payroll_projections.sql", "183_payroll_telemetry_classification.sql", "184_payroll_operational_sources.sql", "185_payroll_shared_dependencies.sql", "186_expense_verification_metadata.sql", "187_load_source_availability.sql", "188_status_board_operational.sql", "189_payroll_current_storage.sql", "191_payroll_pti_classification.sql", "192_payroll_unrelated_sources.sql", "193_payroll_unassigned_company_expenses.sql", "196_datatruck_modified_sync.sql", "197_board_route_usage.sql"} {
		body, readErr := os.ReadFile("../../sql/" + name)
		if readErr != nil {
			t.Fatal(readErr)
		}
		lateMigrations = append(lateMigrations, strings.ReplaceAll(string(body), "\r\n", "\n"))
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
				before, after, ok := strings.Cut(string(source), string(migration))
				if !ok {
					t.Fatal("migration differs from init")
				}
				prior := strings.ReplaceAll(before+after, "\r\n", "\n")
				for _, body := range lateMigrations {
					prior = strings.Replace(prior, body, "", 1)
				}
				exec(withoutBoardLiveMigration(t, prior))
				exec(string(migration))
				applyBoardLiveMigration(t, ctx, admin)
				// Reproduce migration 160 enrollment after restoring its tables,
				// then install revision/protocol/board guards in numbered order.
				exec(`INSERT INTO payroll_cache_revisions(table_name)
 SELECT tablename FROM pg_tables WHERE schemaname=current_schema()
 AND tablename NOT IN ('payroll_cache_revisions','auth_sessions','user_login_activity','schema_migrations')
 ON CONFLICT(table_name) DO NOTHING`)
				for _, body := range lateMigrations {
					exec(body)
				}
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
