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

func TestFiveELDTruckTasksDatabase(t *testing.T) {
	dsn := os.Getenv("MSERP_DRIVER_BOARD_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set local disposable MSERP_DRIVER_BOARD_TEST_DATABASE_URL")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.HasSuffix(cfg.ConnConfig.Database, "_test") || (cfg.ConnConfig.Host != "127.0.0.1" && cfg.ConnConfig.Host != "localhost") {
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
	migration, err := os.ReadFile("../../sql/094_five_eld_truck_tasks.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"fresh", "migration"} {
		t.Run(mode, func(t *testing.T) {
			schema := fmt.Sprintf("eld_tasks_%d", time.Now().UnixNano())
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
				exec(string(source))
			} else {
				before, _, ok := strings.Cut(strings.ReplaceAll(string(source), "\r\n", "\n"), strings.ReplaceAll(string(migration), "\r\n", "\n"))
				if !ok {
					t.Fatal("migration boundary")
				}
				exec(before)
				exec(string(migration))
				later, err := os.ReadFile("../../sql/101_maintenance_tasks_roles.sql")
				if err != nil {
					t.Fatal(err)
				}
				exec(string(later))
			}
			var actor, other string
			if err = admin.QueryRow(ctx, `INSERT INTO app_users(username,password_hash) VALUES('ELD reviewer','$2test') RETURNING id::text`).Scan(&actor); err != nil {
				t.Fatal(err)
			}
			if err = admin.QueryRow(ctx, `INSERT INTO app_users(username,password_hash) VALUES('Other reviewer','$2test') RETURNING id::text`).Scan(&other); err != nil {
				t.Fatal(err)
			}
			exec(`UPDATE system_task_assignments SET assignee_id=$1,assignee_ids=ARRAY[$1::uuid] WHERE kind='five_eld_truck'`, actor)
			exec(`GRANT SELECT,INSERT,UPDATE,DELETE ON ALL TABLES IN SCHEMA ` + quoted + ` TO mserp_app; GRANT USAGE,SELECT ON ALL SEQUENCES IN SCHEMA ` + quoted + ` TO mserp_app`)
			app := cfg.Copy()
			app.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
			app.ConnConfig.RuntimeParams["role"] = "mserp_app"
			pool, e := pgxpool.NewWithConfig(ctx, app)
			if e != nil {
				t.Fatal(e)
			}
			defer pool.Close()
			eld := NewFiveELDRepository(pool)
			fleet := NewFleetRepository(pool)
			now := time.Now().UTC().Truncate(time.Microsecond)
			vin := "1FUJGLDR0CLBP1234"
			existingVIN := "1FUJGLDR0CLBP9999"
			exec(`INSERT INTO trucks(unit_number,vin,active) VALUES('existing',$1,false)`, existingVIN)
			locations := []FiveELDLocation{{VIN: vin, ProviderTruckNumber: "0012", Latitude: 40, Longitude: -80, ReportedAt: now, FetchedAt: now}, {VIN: existingVIN, ProviderTruckNumber: "existing", Latitude: 40, Longitude: -80, ReportedAt: now, FetchedAt: now}}
			sync := func() {
				t.Helper()
				if e := eld.StoreLocations(ctx, locations, []string{vin, existingVIN}, []string{}, 0, FiveELDSyncResult{SyncedAt: now}); e != nil {
					t.Fatal(e)
				}
			}
			sync()
			sync()
			var id string
			var count int
			if e = pool.QueryRow(ctx, `SELECT count(*) FROM system_task_records WHERE kind='five_eld_truck'`).Scan(&count); e != nil || count != 1 {
				t.Fatalf("one missing-truck task: %d %v", count, e)
			}
			if e = pool.QueryRow(ctx, `SELECT id::text FROM system_task_records WHERE kind='five_eld_truck'`).Scan(&id); e != nil {
				t.Fatal(e)
			}
			viewer := WithTaskViewer(ctx, actor, false)
			detail, e := fleet.FiveELDTruckReview(viewer, id)
			if e != nil || detail.UnitNumber != "0012" {
				t.Fatal(detail, e)
			}
			if _, e = fleet.FiveELDTruckReview(WithTaskViewer(ctx, other, false), id); !errors.Is(e, ErrNotFound) {
				t.Fatal("privacy", e)
			}
			exec(`UPDATE system_task_assignments SET assignee_ids=ARRAY[$1::uuid,$2::uuid] WHERE kind='five_eld_truck'`, actor, other)
			if _, e = fleet.FiveELDTruckReview(WithTaskViewer(ctx, other, false), id); e != nil {
				t.Fatal("second assignee must share the same task", e)
			}
			board := &CustomTaskRepository{pool: pool}
			if n, e := board.OpenCount(viewer, false, true); e != nil || n != 1 {
				t.Fatal("shared task count", n, e)
			}
			if n, e := board.OpenCount(viewer, true, false); e != nil || n != 0 {
				t.Fatal("fleet permission boundary", n, e)
			}
			exec(`UPDATE system_task_assignments SET assignee_id=$1,assignee_ids=ARRAY[$1::uuid] WHERE kind='five_eld_truck'`, other)
			if _, e = fleet.FiveELDTruckReview(viewer, id); !errors.Is(e, ErrNotFound) {
				t.Fatal("reassignment", e)
			}
			viewer = WithTaskViewer(ctx, other, false)
			corrected := "1FUJGLDR0CLBP1236"
			input := TruckInput{UnitNumber: "0012-edited", VIN: &corrected, Active: true, Status: "available", IsCompanyOwned: true}
			if _, e = fleet.CompleteFiveELDTruckReview(viewer, id, other, detail.Version+1, input); !errors.Is(e, ErrFiveELDReviewConflict) {
				t.Fatal("stale version", e)
			}
			duplicate := input
			duplicate.VIN = &existingVIN
			if _, e = fleet.CompleteFiveELDTruckReview(viewer, id, other, detail.Version, duplicate); !errors.Is(e, ErrFiveELDReviewConflict) {
				t.Fatal("edited VIN cannot duplicate an inactive truck", e)
			}
			truckID, e := fleet.CompleteFiveELDTruckReview(viewer, id, other, detail.Version, input)
			if e != nil {
				t.Fatal(e)
			}
			var savedVIN, unit, completedBy string
			if e = pool.QueryRow(ctx, `SELECT vin,unit_number FROM trucks WHERE id=$1`, truckID).Scan(&savedVIN, &unit); e != nil || savedVIN != corrected || unit != "0012-EDITED" {
				t.Fatal("edited profile", savedVIN, unit, e)
			}
			if _, e = fleet.CompleteFiveELDTruckReview(viewer, id, other, detail.Version, input); !errors.Is(e, ErrFiveELDReviewConflict) {
				t.Fatal("duplicate save", e)
			}
			sync()
			if e = pool.QueryRow(ctx, `SELECT completed_by_name FROM system_task_records WHERE id=$1 AND completed_at IS NOT NULL`, id).Scan(&completedBy); e != nil || completedBy != "Other reviewer" {
				t.Fatal("durable receipt", completedBy, e)
			}
		})
	}
}
