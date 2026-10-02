package repository

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"mserp/internal/fleetscope"
)

// Each run owns a new schema. Supply only an isolated test database; no runtime
// .env is loaded. Both baseline and incremental migration paths are exercised.
func TestFleetScopeDatabase(t *testing.T) {
	dsn := os.Getenv("MSERP_FLEETSCOPE_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set MSERP_FLEETSCOPE_TEST_DATABASE_URL to an isolated PostgreSQL database")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal("invalid test database configuration")
	}
	defer admin.Close()
	if _, err = admin.Exec(ctx, `CREATE EXTENSION IF NOT EXISTS pgcrypto WITH SCHEMA public`); err != nil {
		t.Fatal(err)
	}
	baseline, err := os.ReadFile("../../sql/init.sql")
	if err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile("../../sql/023_add_fleetscope_driver_intake.sql")
	if err != nil {
		t.Fatal(err)
	}
	ownership, err := os.ReadFile("../../sql/025_fix_intake_and_review_table_owners.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, incremental := range []bool{false, true} {
		t.Run(fmt.Sprintf("incremental=%v", incremental), func(t *testing.T) {
			schema := fmt.Sprintf("fleetscope_intake_test_%d", time.Now().UnixNano())
			quoted := pgx.Identifier{schema}.Sanitize()
			if _, err := admin.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
				t.Fatal(err)
			}
			defer func() {
				if _, err := admin.Exec(ctx, "DROP SCHEMA "+quoted+" CASCADE"); err != nil {
					t.Error(err)
				}
			}()
			cfg, err := pgxpool.ParseConfig(dsn)
			if err != nil {
				t.Fatal("invalid test database configuration")
			}
			cfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
			pool, err := pgxpool.NewWithConfig(ctx, cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer pool.Close()
			sql := string(baseline)
			if incremental {
				sql = strings.Split(sql, "-- New hires are staged separately")[0] + "COMMIT;"
			}
			if _, err = pool.Exec(ctx, sql); err != nil {
				t.Fatal(err)
			}
			if incremental {
				if _, err = pool.Exec(ctx, string(migration)); err != nil {
					t.Fatal(err)
				}
				if _, err = pool.Exec(ctx, string(ownership)); err != nil {
					t.Fatal(err)
				}
			}
			if incremental {
				owners, e := os.ReadFile("../../sql/032_add_investors.sql")
				if e != nil {
					t.Fatal(e)
				}
				if _, e = pool.Exec(ctx, string(owners)); e != nil {
					t.Fatal(e)
				}
				charges, e := os.ReadFile("../../sql/033_add_driver_charges.sql")
				if e != nil {
					t.Fatal(e)
				}
				if _, e = pool.Exec(ctx, string(charges)); e != nil {
					t.Fatal(e)
				}
				matrix, e := os.ReadFile("../../sql/034_driver_charge_matrix.sql")
				if e != nil {
					t.Fatal(e)
				}
				if _, e = pool.Exec(ctx, string(matrix)); e != nil {
					t.Fatal(e)
				}
				backdated, e := os.ReadFile("../../sql/035_backdated_driver_charges.sql")
				if e != nil {
					t.Fatal(e)
				}
				if _, e = pool.Exec(ctx, string(backdated)); e != nil {
					t.Fatal(e)
				}
			}
			if incremental {
				phones, e := os.ReadFile("../../sql/042_standardize_phones.sql")
				if e != nil {
					t.Fatal(e)
				}
				if _, e = pool.Exec(ctx, string(phones)); e != nil {
					t.Fatal(e)
				}
			}
			var runtimeRoleExists bool
			if err = pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_roles WHERE rolname='mserp_app')`).Scan(&runtimeRoleExists); err != nil {
				t.Fatal(err)
			}
			if runtimeRoleExists {
				if _, err = pool.Exec(ctx, "GRANT USAGE ON SCHEMA "+quoted+" TO mserp_app"); err != nil {
					t.Fatal(err)
				}
				tx, err := pool.Begin(ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer tx.Rollback(ctx)
				if _, err = tx.Exec(ctx, `SET LOCAL ROLE mserp_app`); err != nil {
					t.Fatal(err)
				}
				for _, table := range []string{"fleetscope_driver_intake", "fleetscope_webhook_receipts", "relay_identity_reviews"} {
					var allowed bool
					if err = tx.QueryRow(ctx, `SELECT has_table_privilege(current_user, $1, 'SELECT,INSERT,UPDATE,DELETE')`, table).Scan(&allowed); err != nil || !allowed {
						t.Fatalf("runtime access to %s: %v", table, err)
					}
					if _, err = tx.Exec(ctx, "SELECT 1 FROM "+pgx.Identifier{table}.Sanitize()+" LIMIT 0"); err != nil {
						t.Fatalf("runtime read of %s: %v", table, err)
					}
				}
				if err = tx.Rollback(ctx); err != nil {
					t.Fatal(err)
				}
			}
			testFleetScopeLifecycle(t, pool)
		})
	}
}

func testFleetScopeLifecycle(t *testing.T, pool *pgxpool.Pool) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	repo := NewFleetRepository(pool)
	userID := "00000000-0000-0000-0000-000000000001"
	if _, err := pool.Exec(ctx, `INSERT INTO app_users(id,username,password_hash) VALUES($1,'intake-test','$2test')`, userID); err != nil {
		t.Fatal(err)
	}
	event := fleetscope.Event{Version: 1, Type: "driver.hired", EventID: "00000000-0000-0000-0000-000000000002", CompanyID: "00000000-0000-0000-0000-000000000003", OccurredAt: time.Now(), Driver: fleetscope.Driver{ID: "00000000-0000-0000-0000-000000000004", FullName: "Test New Hire", DriverType: "company", HireDate: "2026-09-28", Email: "hire@example.test"}}
	var wg sync.WaitGroup
	results := make(chan IntakeResult, 6)
	failures := make(chan error, 6)
	for range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := repo.AcceptFleetScopeHire(ctx, event, "original-hash")
			if err != nil {
				failures <- err
			} else {
				results <- result
			}
		}()
	}
	wg.Wait()
	close(results)
	close(failures)
	for err := range failures {
		t.Fatal(err)
	}
	accepted := 0
	intakeID := ""
	for result := range results {
		if result.Status == "accepted" {
			accepted++
		}
		if intakeID != "" && intakeID != result.IntakeID {
			t.Fatal("duplicate intake")
		}
		intakeID = result.IntakeID
	}
	if accepted != 1 {
		t.Fatalf("accepted=%d", accepted)
	}
	if _, err := repo.AcceptFleetScopeHire(ctx, event, "changed-hash"); !errors.Is(err, ErrFleetScopeEventConflict) {
		t.Fatalf("event conflict: %v", err)
	}
	changed := event
	changed.EventID = "00000000-0000-0000-0000-000000000005"
	changed.Driver.FullName = "Changed Name"
	if result, err := repo.AcceptFleetScopeHire(ctx, changed, "new-event"); err != nil || result.IntakeID != intakeID || result.Status != "duplicate" {
		t.Fatalf("same driver retry: %+v %v", result, err)
	}
	page, err := repo.ListDriverIntake(ctx, Pagination{Page: 1, PageSize: 25})
	if err != nil || page.Total != 1 || page.Items[0].Driver.FullName != event.Driver.FullName {
		t.Fatalf("snapshot changed: %+v %v", page, err)
	}
	assertDriverCount := func(want int) {
		t.Helper()
		var n int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM drivers`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != want {
			t.Fatalf("drivers=%d want=%d", n, want)
		}
	}
	assertDriverCount(0) // Pending hires never enter payroll or the fleet.
	directory, err := repo.ListDriverDirectory(ctx, Pagination{}, "hire@example.test", false)
	if err != nil || directory.Total != 1 || directory.Items[0].IntakeID != intakeID || directory.Items[0].FullName != event.Driver.FullName {
		t.Fatalf("pending directory row: %+v %v", directory, err)
	}
	if _, err := repo.GetDriverIntake(ctx, intakeID); err != nil {
		t.Fatal(err)
	}
	tasks, err := repo.ListDriverIntake(ctx, Pagination{}, "no such hire")
	if err != nil || tasks.Total != 0 {
		t.Fatalf("task search: %+v %v", tasks, err)
	}
	missingTruck := "00000000-0000-0000-0000-000000000099"
	input := DriverInput{FullName: event.Driver.FullName, PayType: "cpm", PayRate: 0.65, Active: true, TruckID: &missingTruck}
	if _, err = repo.CompleteDriverIntake(ctx, intakeID, userID, "", &input, false); err == nil {
		t.Fatal("invalid assignment succeeded")
	}
	assertDriverCount(0)
	page, err = repo.ListDriverIntake(ctx, Pagination{})
	if err != nil || page.Total != 1 {
		t.Fatal("failed setup consumed intake", err)
	}
	var truckID, dispatcherID string
	if err = pool.QueryRow(ctx, `INSERT INTO trucks(unit_number) VALUES('TEST1') RETURNING id`).Scan(&truckID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO dispatchers(full_name,normalized_name) VALUES('Test Dispatch','test dispatch') RETURNING id`).Scan(&dispatcherID); err != nil {
		t.Fatal(err)
	}
	input.TruckID = &truckID
	input.DispatcherID = &dispatcherID
	completed := make(chan Driver, 2)
	completionErrors := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			driver, err := repo.CompleteDriverIntake(ctx, intakeID, userID, "", &input, false)
			if err != nil {
				completionErrors <- err
			} else {
				completed <- driver
			}
		}()
	}
	wg.Wait()
	close(completed)
	close(completionErrors)
	if len(completed) != 1 || len(completionErrors) != 1 {
		t.Fatal("concurrent completion was not serialized")
	}
	driver := <-completed
	if err := <-completionErrors; !errors.Is(err, ErrIntakeCompleted) {
		t.Fatal(err)
	}
	if driver.PayRate != 0.65 || driver.TruckID == nil || *driver.TruckID != truckID || driver.DispatcherID == nil {
		t.Fatalf("setup missing assignments: %+v", driver)
	}
	assertDriverCount(1)
	directory, err = repo.ListDriverDirectory(ctx, Pagination{}, "", false)
	if err != nil || directory.Total != 1 || directory.Items[0].IntakeID != "" || directory.Items[0].ID != driver.ID || directory.Items[0].PayRate != .65 {
		t.Fatalf("completed directory row: %+v %v", directory, err)
	}
	if _, err := repo.GetDriverIntake(ctx, intakeID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("completed task remained accessible: %v", err)
	}
	if _, err = repo.CompleteDriverIntake(ctx, intakeID, userID, "", &input, false); !errors.Is(err, ErrIntakeCompleted) {
		t.Fatalf("duplicate completion: %v", err)
	}
	page, err = repo.ListDriverIntake(ctx, Pagination{})
	if err != nil || page.Total != 0 {
		t.Fatal("completed intake still pending", err)
	}

	// A new source identity resembling an existing driver needs human review;
	// confirmed linking must not change that driver's rates or truck assignment.
	other := event
	other.EventID = "00000000-0000-0000-0000-000000000006"
	other.Driver.ID = "00000000-0000-0000-0000-000000000007"
	result, err := repo.AcceptFleetScopeHire(ctx, other, "other-hash")
	if err != nil {
		t.Fatal(err)
	}
	page, err = repo.ListDriverIntake(ctx, Pagination{})
	if err != nil || len(page.Items) != 1 || len(page.Items[0].Candidates) != 1 {
		t.Fatalf("missing match: %+v %v", page, err)
	}
	directory, err = repo.ListDriverDirectory(ctx, Pagination{PageSize: 1}, "Test", false)
	if err != nil || directory.Total != 2 || directory.TotalPages != 2 || len(directory.Items) != 1 || directory.Items[0].IntakeID != result.IntakeID {
		t.Fatalf("combined first page: %+v %v", directory, err)
	}
	directory, err = repo.ListDriverDirectory(ctx, Pagination{Page: 2, PageSize: 1}, "Test", false)
	if err != nil || directory.Total != 2 || directory.Items[0].ID != driver.ID || directory.Items[0].IntakeID != "" {
		t.Fatalf("combined second page: %+v %v", directory, err)
	}
	if _, err = repo.CompleteDriverIntake(ctx, result.IntakeID, userID, "", &input, false); !errors.Is(err, ErrIntakeMatch) {
		t.Fatalf("unreviewed match: %v", err)
	}
	linked, err := repo.CompleteDriverIntake(ctx, result.IntakeID, userID, driver.ID, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(driver, linked) {
		t.Fatal("link overwrote existing driver")
	}
	assertDriverCount(1)

	// Even after deleting a local driver, delivery retries must not recreate it
	// or reopen the completed intake.
	if err = repo.DeleteDriver(ctx, driver.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = repo.AcceptFleetScopeHire(ctx, event, "original-hash"); err != nil {
		t.Fatal(err)
	}
	assertDriverCount(0)
	page, err = repo.ListDriverIntake(ctx, Pagination{})
	if err != nil || page.Total != 0 {
		t.Fatal("retry reopened deleted driver", err)
	}
}
