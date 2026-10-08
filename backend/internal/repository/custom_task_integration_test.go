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

// Requires a disposable database with an mserp_app role and an administrator
// test connection. Each case uses a private schema, removed after the test.
func TestCustomTaskDatabase(t *testing.T) {
	dsn := os.Getenv("MSERP_CUSTOM_TASK_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set MSERP_CUSTOM_TASK_TEST_DATABASE_URL for isolated PostgreSQL checks")
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
			schema := fmt.Sprintf("custom_task_test_%d", time.Now().UnixNano())
			quoted := pgx.Identifier{schema}.Sanitize()
			if _, err := admin.Exec(ctx, `CREATE SCHEMA `+quoted+`; SET search_path TO `+quoted+`, public`); err != nil {
				t.Fatal(err)
			}
			defer func() { _, _ = admin.Exec(ctx, `SET search_path TO public; DROP SCHEMA `+quoted+` CASCADE`) }()
			filename := "../../sql/init.sql"
			if mode == "migration" {
				filename = "../../sql/026_add_custom_tasks.sql"
				if _, err := admin.Exec(ctx, `CREATE TABLE app_users(id uuid PRIMARY KEY DEFAULT gen_random_uuid(), username text, password_hash text, active boolean NOT NULL DEFAULT true)`); err != nil {
					t.Fatal(err)
				}
			}
			source, err := os.ReadFile(filename)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := admin.Exec(ctx, string(source)); err != nil {
				t.Fatal(err)
			}
			if mode == "migration" {
				migration, err := os.ReadFile("../../sql/054_task_assignments.sql")
				if err != nil {
					t.Fatal(err)
				}
				if _, err = admin.Exec(ctx, string(migration)); err != nil {
					t.Fatal(err)
				}
				// This legacy fixture contains only custom-task tables. The complete
				// 066 migration is exercised by TestTaskAssignmentsDatabase.
				if _, err = admin.Exec(ctx, `ALTER TABLE custom_tasks ADD COLUMN completed_by uuid REFERENCES app_users(id) ON DELETE SET NULL;
				ALTER TABLE custom_tasks ADD COLUMN completed_by_name text NOT NULL DEFAULT ''; ALTER TABLE custom_tasks ADD COLUMN in_process boolean NOT NULL DEFAULT false`); err != nil {
					t.Fatal(err)
				}
			}
			var user string
			if err := admin.QueryRow(ctx, `INSERT INTO app_users(username,password_hash) VALUES('task-tester','$2test') RETURNING id::text`).Scan(&user); err != nil {
				t.Fatal(err)
			}
			if _, err := admin.Exec(ctx, `GRANT USAGE ON SCHEMA `+quoted+` TO mserp_app; GRANT SELECT ON app_users TO mserp_app`); err != nil {
				t.Fatal(err)
			}
			config, err := pgxpool.ParseConfig(dsn)
			if err != nil {
				t.Fatal("invalid test database configuration")
			}
			config.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
			config.ConnConfig.RuntimeParams["role"] = "mserp_app"
			pool, err := pgxpool.NewWithConfig(ctx, config)
			if err != nil {
				t.Fatal(err)
			}
			defer pool.Close()
			repo := NewCustomTaskRepository(pool)
			task, err := repo.Create(ctx, CustomTaskInput{Title: "Call shop", Notes: "Truck 100%"}, user)
			if err != nil || task.CreatedBy == nil || *task.CreatedBy != user {
				t.Fatalf("create: %+v %v", task, err)
			}
			if _, err := repo.Create(ctx, CustomTaskInput{Title: "Second task"}, user); err != nil {
				t.Fatal(err)
			}
			list, err := repo.List(ctx, Pagination{Page: 99, PageSize: 1}, "", "open")
			if err != nil || list.Total != 2 || list.Page != 2 || len(list.Items) != 1 {
				t.Fatalf("pagination: %+v %v", list, err)
			}
			list, err = repo.List(ctx, Pagination{}, "%", "all")
			if err != nil || list.Total != 1 {
				t.Fatalf("literal search: %+v %v", list, err)
			}
			done, err := repo.SetCompleted(ctx, task.ID, true)
			if err != nil || done.CompletedAt == nil {
				t.Fatalf("complete: %+v %v", done, err)
			}
			again, err := repo.SetCompleted(ctx, task.ID, true)
			if err != nil || again.CompletedAt == nil || !again.CompletedAt.Equal(*done.CompletedAt) {
				t.Fatalf("idempotent completion: %+v %v", again, err)
			}
			edited, err := repo.Update(ctx, task.ID, CustomTaskInput{Title: "Call tire shop", Notes: "New notes"})
			if err != nil || edited.CompletedAt == nil || edited.Title != "Call tire shop" {
				t.Fatalf("edit preserves completion: %+v %v", edited, err)
			}
			list, err = repo.List(ctx, Pagination{}, "TIRE", "completed")
			if err != nil || list.Total != 1 {
				t.Fatalf("completed search: %+v %v", list, err)
			}
			reopened, err := repo.SetCompleted(ctx, task.ID, false)
			if err != nil || reopened.CompletedAt != nil || reopened.Notes != "New notes" {
				t.Fatalf("reopen preserves content: %+v %v", reopened, err)
			}
			if _, err := repo.Create(ctx, CustomTaskInput{Title: strings.Repeat("x", 201)}, user); err == nil {
				t.Fatal("missing database title constraint")
			}
			if err := repo.Delete(ctx, task.ID); err != nil {
				t.Fatal(err)
			}
			if err := repo.Delete(ctx, task.ID); !errors.Is(err, ErrNotFound) {
				t.Fatalf("deleted task: %v", err)
			}
			if _, err := repo.Update(ctx, task.ID, CustomTaskInput{Title: "Missing"}); !errors.Is(err, ErrNotFound) {
				t.Fatalf("missing edit: %v", err)
			}
			if _, err := repo.SetCompleted(ctx, task.ID, true); !errors.Is(err, ErrNotFound) {
				t.Fatalf("missing complete: %v", err)
			}
			list, err = repo.List(ctx, Pagination{}, "no match", "all")
			if err != nil || list.Total != 0 || list.Items == nil {
				t.Fatalf("empty page: %+v %v", list, err)
			}
		})
	}
}
