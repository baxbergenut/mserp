package repository

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
	"strings"
	"testing"
	"time"
)

func testGrossPresence(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	var driver, user, role string
	if err := pool.QueryRow(ctx, `INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate,active) VALUES('Gross Presence','gross presence','cpm',0,false) RETURNING id`).Scan(&driver); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO app_roles(name,permissions) VALUES('Gross Presence Viewer','{board.read}') RETURNING id`).Scan(&role); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO app_users(username,password_hash,role_id) VALUES('Gross Presence Viewer','$2fake',$1) RETURNING id`, role).Scan(&user); err != nil {
		t.Fatal(err)
	}
	hash, other := strings.Repeat("c", 64), strings.Repeat("d", 64)
	for _, h := range []string{hash, other} {
		exec(`INSERT INTO auth_sessions(user_id,token_hash,csrf_token,expires_at) VALUES($1,$2,$3,now()+interval '1 hour')`, user, h, strings.Repeat("f", 43))
	}
	repo := NewGrossBoardRepository(pool)
	week, _ := time.Parse(time.DateOnly, "2026-10-12")
	client := "e5600000-0000-0000-0000-000000000001"
	cell := GrossBoardCell{DriverID: driver, WeekStart: "2026-10-12", Date: "2026-10-13", Slot: 1, Column: "driverRate"}
	if err := repo.SetPresence(ctx, client, user, hash, cell); err != nil {
		t.Fatal(err)
	}
	read := func(w time.Time) []GrossBoardPresence {
		t.Helper()
		rows, err := repo.Presence(ctx, w)
		if err != nil {
			t.Fatal(err)
		}
		return rows
	}
	rows := read(week)
	if len(rows) != 1 || rows[0].Column != "driverRate" || rows[0].Slot != 1 || rows[0].UserName != "Gross Presence Viewer" {
		t.Fatal("missing exact cursor", rows)
	}
	if len(read(week.AddDate(0, 0, 7))) != 0 {
		t.Fatal("cursor crossed weeks")
	}
	if err := repo.SetPresence(ctx, client, user, other, cell); !errors.Is(err, ErrGrossBoardConflict) {
		t.Fatal("another session took the cursor", err)
	}
	if err := repo.ClearPresence(ctx, client, user, other); err != nil {
		t.Fatal(err)
	}
	if len(read(week)) != 1 {
		t.Fatal("another session cleared cursor")
	}
	cell.Column = "originalRate"
	cell.Slot = 0
	if err := repo.SetPresence(ctx, client, user, hash, cell); err != nil {
		t.Fatal(err)
	}
	if rows = read(week); len(rows) != 1 || rows[0].Column != "originalRate" || rows[0].Slot != 0 {
		t.Fatal("cursor did not move", rows)
	}
	exec(`UPDATE gross_board_presence SET expires_at=now()-interval '1 second'`)
	if len(read(week)) != 0 {
		t.Fatal("expired cursor visible")
	}
	if err := repo.SetPresence(ctx, client, user, hash, cell); err != nil {
		t.Fatal(err)
	}
	exec(`UPDATE app_roles SET permissions='{}' WHERE id=$1`, role)
	if len(read(week)) != 0 {
		t.Fatal("revoked viewer visible")
	}
	exec(`UPDATE app_roles SET permissions='{board.read}' WHERE id=$1`, role)
	exec(`DELETE FROM auth_sessions WHERE token_hash=$1`, hash)
	if len(read(week)) != 0 {
		t.Fatal("logout retained cursor")
	}
	if err := repo.SetPresence(ctx, client, user, hash, cell); !errors.Is(err, ErrGrossBoardConflict) {
		t.Fatal("revoked session recreated cursor", err)
	}
}
