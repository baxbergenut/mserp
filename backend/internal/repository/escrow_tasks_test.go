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

func TestEscrowTerminationWorkflowDatabase(t *testing.T) {
	dsn := os.Getenv("MSERP_DRIVER_PAY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set disposable local MSERP_DRIVER_PAY_TEST_DATABASE_URL")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.HasSuffix(cfg.ConnConfig.Database, "_test") || (cfg.ConnConfig.Host != "127.0.0.1" && cfg.ConnConfig.Host != "localhost") {
		t.Fatal("local _test database required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(ctx)
	source, err := os.ReadFile("../../sql/init.sql")
	if err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile("../../sql/069_driver_status_escrow_tasks.sql")
	if err != nil {
		t.Fatal(err)
	}
	notifications, err := os.ReadFile("../../sql/070_escrow_review_notifications.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"fresh", "migration"} {
		t.Run(mode, func(t *testing.T) {
			schema := fmt.Sprintf("escrow_tasks_%d", time.Now().UnixNano())
			quoted := pgx.Identifier{schema}.Sanitize()
			exec := func(sql string, args ...any) {
				t.Helper()
				if _, err := admin.Exec(ctx, sql, args...); err != nil {
					t.Fatal(err)
				}
			}
			exec(`CREATE SCHEMA ` + quoted + `;SET search_path TO ` + quoted + `,public; GRANT USAGE ON SCHEMA ` + quoted + ` TO mserp_app`)
			defer func() {
				_, _ = admin.Exec(context.Background(), `SET search_path TO public;DROP SCHEMA `+quoted+` CASCADE`)
			}()
			if mode == "migration" {
				before, _, ok := strings.Cut(strings.ReplaceAll(string(source), "\r\n", "\n"), strings.ReplaceAll(string(migration), "\r\n", "\n"))
				if !ok {
					t.Fatal("migration boundary")
				}
				exec(before)
				exec(`INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate,active) VALUES('Legacy inactive','legacy inactive','cpm',0.75,false)`)
				exec(string(migration))
				exec(string(notifications))
				var converted bool
				if err = admin.QueryRow(ctx, `SELECT status='terminated' AND termination_date=(now() AT TIME ZONE 'America/New_York')::date-30 FROM drivers WHERE full_name='Legacy inactive'`).Scan(&converted); err != nil || !converted {
					t.Fatal("inactive conversion", err)
				}
			} else {
				exec(string(source))
			}
			exec(`GRANT SELECT,INSERT,UPDATE,DELETE ON ALL TABLES IN SCHEMA ` + quoted + ` TO mserp_app`)
			app := cfg.Copy()
			app.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
			app.ConnConfig.RuntimeParams["role"] = "mserp_app"
			pool, err := pgxpool.NewWithConfig(ctx, app)
			if err != nil {
				t.Fatal(err)
			}
			defer pool.Close()
			run := func(sql string, args ...any) {
				t.Helper()
				if _, err := pool.Exec(ctx, sql, args...); err != nil {
					t.Fatal(err)
				}
			}
			scalar := func(sql string, args ...any) string {
				t.Helper()
				var result string
				if err := pool.QueryRow(ctx, sql, args...).Scan(&result); err != nil {
					t.Fatal(err)
				}
				return result
			}
			actor := scalar(`INSERT INTO app_users(username,email,password_hash,role_id) SELECT 'Escrow actor','actor@test.example','$2test',id FROM app_roles WHERE system_role RETURNING id::text`)
			second := scalar(`INSERT INTO app_users(username,email,password_hash,role_id) SELECT 'Escrow second','second@test.example','$2test',id FROM app_roles WHERE system_role RETURNING id::text`)
			outsider := scalar(`INSERT INTO app_users(username,email,password_hash,role_id) SELECT 'Escrow outsider','outsider@test.example','$2test',id FROM app_roles WHERE system_role RETURNING id::text`)
			auth := NewAuthRepository(pool)
			if err = auth.SaveSystemTaskAssignment(ctx, actor, SystemTaskAssignment{Kind: "escrow_release", Version: 1, AssigneeIDs: []string{actor, second}}); err != nil {
				t.Fatal(err)
			}
			repo := NewEscrowRepository(pool)
			tasks := NewCustomTaskRepository(pool)
			driver := scalar(`INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate,status,termination_date) VALUES('Termination review','termination review','cpm',0.75,'terminated',(now() AT TIME ZONE 'America/New_York')::date-29) RETURNING id::text`)
			escrow := scalar(`INSERT INTO driver_escrows(driver_id,driver_name,start_date,amount,opening_paid) VALUES($1,'Termination review',(now() AT TIME ZONE 'America/New_York')::date-40,2500,2500) RETURNING id::text`, driver)
			task := scalar(`SELECT termination_id::text FROM drivers WHERE id=$1`, driver)
			if err = tasks.GenerateEscrowTasks(ctx); err != nil {
				t.Fatal(err)
			}
			if scalar(`SELECT count(*)::text FROM system_task_records WHERE id=$1`, task) != "0" {
				t.Fatal("task generated before day 30")
			}
			run(`UPDATE drivers SET termination_date=termination_date-1 WHERE id=$1`, driver)
			for i := 0; i < 2; i++ {
				if err = tasks.GenerateEscrowTasks(ctx); err != nil {
					t.Fatal(err)
				}
			}
			if scalar(`SELECT count(*)::text FROM system_task_records WHERE id=$1`, task) != "1" {
				t.Fatal("missing/duplicate due task")
			}
			for _, user := range []string{actor, second, outsider} {
				viewer := WithTaskViewer(ctx, user, false)
				page, err := tasks.Board(viewer, Pagination{}, "Termination review", "open", true)
				if err != nil {
					t.Fatal(err)
				}
				want := 1
				if user == outsider {
					want = 0
				}
				if page.Total != want {
					t.Fatalf("visibility got %d want %d", page.Total, want)
				}
				if _, err = repo.TaskDetail(viewer, task); (err == nil) != (want == 1) {
					t.Fatal("detail privacy", err)
				}
			}
			exec("LISTEN mserp_tasks")
			run("UPDATE drivers SET termination_date=termination_date+1 WHERE id=$1", driver)
			notificationCtx, stopNotification := context.WithTimeout(ctx, 2*time.Second)
			_, noticeErr := admin.WaitForNotification(notificationCtx)
			stopNotification()
			if noticeErr != nil {
				t.Fatal("review date correction did not notify task feeds", noticeErr)
			}
			hidden, err := tasks.Board(WithTaskViewer(ctx, second, false), Pagination{}, "Termination review", "open", true)
			if err != nil || hidden.Total != 0 {
				t.Fatal("corrected future review stayed visible", hidden, err)
			}
			run("UPDATE drivers SET termination_date=termination_date-1 WHERE id=$1", driver)
			exec("UNLISTEN mserp_tasks")
			viewer := WithTaskViewer(ctx, second, false)
			if _, err = tasks.SetCompleted(viewer, task, true); err == nil {
				t.Fatal("generic completion bypass")
			}
			versions := func() map[string]int {
				t.Helper()
				detail, err := repo.TaskDetail(viewer, task)
				if err != nil {
					t.Fatal(err)
				}
				result := map[string]int{}
				for _, e := range detail.Escrows {
					result[e.ID] = e.Version
				}
				return result
			}
			input := EscrowTaskDecision{Decision: "released", Reason: "Test verified", Versions: versions()}
			if err = repo.CompleteTask(viewer, task, second, input); err == nil {
				t.Fatal("unreleased completion allowed")
			}
			release := EscrowReleaseInput{ID: scalar(`SELECT gen_random_uuid()::text`), WeekStart: ChargeCurrentWeek(), Amount: "500.00", EscrowVersion: input.Versions[escrow]}
			if err = repo.SaveRelease(viewer, escrow, release, second); err != nil {
				t.Fatal(err)
			}
			input.Decision = "partially_released"
			if err = repo.CompleteTask(viewer, task, second, input); err == nil {
				t.Fatal("stale review allowed")
			}
			input.Versions = versions()
			input.Reason = " "
			if err = repo.CompleteTask(viewer, task, second, input); err == nil {
				t.Fatal("empty reason allowed")
			}
			input.Reason = "Retain remaining balance for documented damages"
			if err = repo.CompleteTask(WithTaskViewer(ctx, outsider, false), task, outsider, input); err == nil {
				t.Fatal("outsider completion allowed")
			}
			if err = repo.CompleteTask(viewer, task, second, input); err != nil {
				t.Fatal(err)
			}
			if err = repo.CompleteTask(viewer, task, second, input); err == nil {
				t.Fatal("repeat completion allowed")
			}
			if scalar(`SELECT decision FROM escrow_release_reviews WHERE id=$1`, task) != "partially_released" {
				t.Fatal("decision not recorded")
			}
			page, err := repo.List(ctx, EscrowQuery{Group: "terminated", DriverID: driver})
			if err != nil || len(page.Items) != 1 || page.Items[0].Status != "partially_released" {
				t.Fatal("terminated status", page, err)
			}
			opening := EscrowOpeningInput{OpeningPaid: "2200.00", Version: page.Items[0].Version, Reason: "Migration correction"}
			if err = repo.SaveOpening(ctx, escrow, actor, opening); err != nil {
				t.Fatal(err)
			}
			if err = repo.SaveOpening(ctx, escrow, actor, opening); err == nil {
				t.Fatal("stale opening edit allowed")
			}
			opening.Version++
			opening.OpeningPaid = "100.00"
			if err = repo.SaveOpening(ctx, escrow, actor, opening); err == nil {
				t.Fatal("underfunded release allowed")
			}
			if scalar(`SELECT count(*)::text FROM driver_escrow_opening_events WHERE escrow_id=$1`, escrow) != "1" {
				t.Fatal("opening audit")
			}
			for _, status := range []string{"vacation", "home"} {
				run(`INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate,status) VALUES($1,$1,'cpm',0.75,$1)`, status)
			}
			if scalar(`SELECT count(*)::text FROM drivers WHERE status IN ('vacation','home') AND active`) != "2" {
				t.Fatal("active status flags")
			}
		})
	}
}
