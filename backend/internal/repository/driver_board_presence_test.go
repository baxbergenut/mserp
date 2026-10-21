package repository

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Legacy fixtures remove prerequisite tables from init.sql. Install their
// notification triggers only after those tables have been migrated back in.
func withoutBoardLiveMigration(t *testing.T, source string) string {
	t.Helper()
	for _, name := range []string{"113_status_board_live.sql", "115_gross_board_live.sql", "116_status_board_automation.sql", "118_status_board_driver_order.sql", "125_status_board_dispatcher_order.sql", "131_samsara_telemetry.sql", "133_odometer_source_selection.sql"} {
		body, err := os.ReadFile("../../sql/" + name)
		if err != nil {
			t.Fatal(err)
		}
		source = strings.ReplaceAll(source, "\r\n", "\n")
		normalized := strings.ReplaceAll(string(body), "\r\n", "\n")
		if !strings.Contains(source, normalized) {
			t.Fatal("init must contain the exact live-board migration")
		}
		source = strings.Replace(source, normalized, "", 1)
	}
	return source
}

func applyBoardLiveMigration(t *testing.T, ctx context.Context, conn *pgx.Conn) {
	t.Helper()
	for _, name := range []string{"113_status_board_live.sql", "115_gross_board_live.sql", "116_status_board_automation.sql", "118_status_board_driver_order.sql", "125_status_board_dispatcher_order.sql", "131_samsara_telemetry.sql", "133_odometer_source_selection.sql"} {
		body, err := os.ReadFile("../../sql/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = conn.Exec(ctx, string(body)); err != nil {
			t.Fatal(err)
		}
	}
}

func testBoardPresence(t *testing.T, ctx context.Context, pool *pgxpool.Pool, repo *DriverBoardRepository, fleet *FleetRepository) {
	t.Helper()
	driver, err := fleet.CreateDriver(ctx, DriverInput{FullName: "Presence Driver", PayType: "cpm", Active: true})
	if err != nil {
		t.Fatal(err)
	}
	var user, role string
	if err = pool.QueryRow(ctx, `INSERT INTO app_roles(name,permissions) VALUES('Presence viewer','{driver_board.read}') RETURNING id`).Scan(&role); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO app_users(username,password_hash,role_id) VALUES('Presence User','$2placeholder',$1) RETURNING id`, role).Scan(&user); err != nil {
		t.Fatal(err)
	}
	hash, otherHash := strings.Repeat("a", 64), strings.Repeat("b", 64)
	for _, token := range []string{hash, otherHash} {
		if _, err = pool.Exec(ctx, `INSERT INTO auth_sessions(user_id,token_hash,csrf_token,expires_at) VALUES($1,$2,$3,now()+interval '1 hour')`, user, token, strings.Repeat("c", 43)); err != nil {
			t.Fatal(err)
		}
	}
	client := "e5500000-0000-0000-0000-000000000001"
	set := func(session, column string) error {
		return repo.SetPresence(ctx, client, user, session, driver.ID, column)
	}
	read := func() []BoardPresence {
		t.Helper()
		rows, err := repo.Presence(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return rows
	}
	if err = set(hash, "notes"); err != nil {
		t.Fatal(err)
	}
	rows := read()
	if len(rows) != 1 || rows[0].UserName != "Presence User" || rows[0].Column != "notes" {
		t.Fatal("missing named cursor", rows)
	}
	if err = set(hash, "eta"); err != nil {
		t.Fatal(err)
	}
	if rows = read(); len(rows) != 1 || rows[0].Column != "eta" {
		t.Fatal("cursor did not move", rows)
	}
	if err = set(otherHash, "status"); !errors.Is(err, ErrDriverBoardConflict) {
		t.Fatal("another session took cursor", err)
	}
	if err = repo.ClearPresence(ctx, client, user, otherHash); err != nil {
		t.Fatal(err)
	}
	if len(read()) != 1 {
		t.Fatal("another session removed cursor")
	}
	if _, err = pool.Exec(ctx, `UPDATE driver_board_presence SET expires_at=now()-interval '1 second'`); err != nil {
		t.Fatal(err)
	}
	if len(read()) != 0 {
		t.Fatal("expired cursor visible")
	}
	if err = set(hash, "notes"); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `UPDATE app_roles SET permissions='{}' WHERE id=$1`, role); err != nil {
		t.Fatal(err)
	}
	if len(read()) != 0 {
		t.Fatal("revoked viewer visible")
	}
	if _, err = pool.Exec(ctx, `UPDATE app_roles SET permissions='{driver_board.read}' WHERE id=$1`, role); err != nil {
		t.Fatal(err)
	}
	if len(read()) != 1 {
		t.Fatal("read-only viewer cursor missing")
	}
	if _, err = pool.Exec(ctx, `DELETE FROM auth_sessions WHERE token_hash=$1`, hash); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM driver_board_presence`).Scan(&count); err != nil || count != 0 {
		t.Fatal("logout retained cursor", err, count)
	}
	if err = set(hash, "notes"); !errors.Is(err, ErrDriverBoardConflict) {
		t.Fatal("revoked session recreated cursor", err)
	}
	if _, err = pool.Exec(ctx, `UPDATE drivers SET active=false WHERE id=$1`, driver.ID); err != nil {
		t.Fatal(err)
	}
}
