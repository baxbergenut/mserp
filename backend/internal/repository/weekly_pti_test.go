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

func TestPTIEligibility(t *testing.T) {
	active := PTICrew{Employment: "active", Availability: "active"}
	home := PTICrew{Employment: "active", Availability: "home"}
	for _, test := range []struct {
		name         string
		active, late bool
		condition    string
		crew         []PTICrew
		want         string
	}{
		{"active", true, false, "ready", []PTICrew{active}, "required"},
		{"home", true, false, "ready", []PTICrew{home}, "exempt"},
		{"team available", true, false, "ready", []PTICrew{home, active}, "required"},
		{"team unavailable", true, false, "ready", []PTICrew{home, home}, "exempt"},
		{"shop", true, false, "shop", []PTICrew{active}, "exempt"},
		{"OOS", true, false, "out_of_service", []PTICrew{active}, "exempt"},
		{"hold", true, false, "hold", []PTICrew{active}, "required"},
		{"no driver", true, false, "ready", nil, "exempt"},
		{"inactive", false, false, "ready", nil, "exempt"},
		{"unknown condition", true, false, "", []PTICrew{active}, "unknown"},
		{"unknown driver", true, false, "ready", []PTICrew{{}}, "unknown"},
		{"late capture", true, true, "ready", []PTICrew{active}, "unknown"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, reason := ptiEligibility(test.active, test.late, test.condition, test.crew)
			if got != test.want || reason == "" {
				t.Fatal(got, reason)
			}
		})
	}
	for _, test := range []struct{ at, want string }{{"2026-10-26T03:59:59Z", "2026-10-19"}, {"2026-10-26T04:00:00Z", "2026-10-26"}, {"2026-11-02T04:59:59Z", "2026-10-26"}, {"2026-11-02T05:00:00Z", "2026-11-02"}} {
		at, _ := time.Parse(time.RFC3339, test.at)
		if got := PTICurrentWeek(at); got != test.want {
			t.Fatal(test.at, got, test.want)
		}
	}
}

func TestWeeklyPTIDatabase(t *testing.T) {
	dsn := os.Getenv("MSERP_DRIVER_BOARD_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("local disposable MSERP_DRIVER_BOARD_TEST_DATABASE_URL required")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.HasSuffix(cfg.ConnConfig.Database, "_test") || (cfg.ConnConfig.Host != "localhost" && cfg.ConnConfig.Host != "127.0.0.1") {
		t.Fatal("local _test database required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
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
	migration, err := os.ReadFile("../../sql/140_weekly_pti.sql")
	if err != nil {
		t.Fatal(err)
	}
	prefix, _, ok := strings.Cut(string(init), string(migration))
	if !ok {
		t.Fatal("fresh init must contain exact PTI migration")
	}
	for _, mode := range []string{"fresh", "migrated"} {
		t.Run(mode, func(t *testing.T) {
			schema := fmt.Sprintf("weekly_pti_%d", time.Now().UnixNano())
			quoted := pgx.Identifier{schema}.Sanitize()
			mustAdmin := func(sql string) {
				t.Helper()
				if _, e := admin.Exec(ctx, sql); e != nil {
					t.Fatal(e)
				}
			}
			mustAdmin(`CREATE SCHEMA ` + quoted + `;SET search_path TO ` + quoted + `,public;GRANT USAGE ON SCHEMA ` + quoted + ` TO mserp_app`)
			defer func() { _, _ = admin.Exec(ctx, `SET search_path TO public;DROP SCHEMA `+quoted+` CASCADE`) }()
			if mode == "fresh" {
				mustAdmin(string(init))
			} else {
				mustAdmin(prefix)
				mustAdmin(`INSERT INTO app_roles(name,permissions) VALUES('PTI existing fleet',ARRAY['drivers.read','trucks.read','trucks.edit'])`)
				mustAdmin(string(migration))
			}
			mustAdmin(`GRANT SELECT,INSERT,UPDATE,DELETE ON ALL TABLES IN SCHEMA ` + quoted + ` TO mserp_app;GRANT USAGE,SELECT ON ALL SEQUENCES IN SCHEMA ` + quoted + ` TO mserp_app`)
			app := cfg.Copy()
			app.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
			app.ConnConfig.RuntimeParams["role"] = "mserp_app"
			pool, e := pgxpool.NewWithConfig(ctx, app)
			if e != nil {
				t.Fatal(e)
			}
			defer pool.Close()
			repo := NewFleetRepository(pool)
			exec := func(sql string, args ...any) {
				t.Helper()
				if _, e := pool.Exec(ctx, sql, args...); e != nil {
					t.Fatal(e)
				}
			}
			scalar := func(sql string, args ...any) string {
				t.Helper()
				var v string
				if e := pool.QueryRow(ctx, sql, args...).Scan(&v); e != nil {
					t.Fatal(e)
				}
				return v
			}
			for _, table := range []string{"weekly_pti_weeks", "weekly_pti_rows", "weekly_pti_events"} {
				if scalar(`SELECT tableowner FROM pg_tables WHERE schemaname=$1 AND tablename=$2`, schema, table) != "mserp_app" {
					t.Fatal("wrong runtime owner", table)
				}
			}
			if mode == "migrated" && scalar(`SELECT ('pti.read'=ANY(permissions) AND 'pti.edit'=ANY(permissions))::text FROM app_roles WHERE name='PTI existing fleet'`) != "true" {
				t.Fatal("existing fleet access not preserved")
			}
			week := PTICurrentWeek(time.Now())
			zone, _ := time.LoadLocation("America/New_York")
			monday, _ := time.ParseInLocation("2006-01-02", week, zone)
			actor := scalar(`INSERT INTO app_users(username,password_hash,role_id) VALUES('PTI Reviewer','$2a$test',(SELECT id FROM app_roles WHERE system_role)) RETURNING id::text`)
			exec(`INSERT INTO drivers(id,full_name,normalized_name,pay_type,pay_rate,availability_status,status,created_at) SELECT ('91000000-0000-4000-8000-'||lpad(n::text,12,'0'))::uuid,'PTI Driver '||n,'pti driver '||n,'cpm',0.6,CASE WHEN n IN(2,3) THEN 'home' ELSE 'active' END,CASE WHEN n IN(2,3) THEN 'home' ELSE 'active' END,$1::timestamptz FROM generate_series(1,7) n`, monday.AddDate(0, 0, -21))
			exec(`INSERT INTO trucks(id,unit_number,status,active,created_at) SELECT ('92000000-0000-4000-8000-'||lpad(n::text,12,'0'))::uuid,'PTI-'||n,CASE n WHEN 3 THEN 'maintenance' WHEN 6 THEN 'out_of_service' WHEN 7 THEN 'hold' ELSE 'available' END,n<>8,$1::timestamptz FROM generate_series(1,8) n`, monday.AddDate(0, 0, -21))
			exec(`INSERT INTO trucks(unit_number,created_at) VALUES('PTI-new-midweek',$1)`, monday.AddDate(0, 0, 1))
			exec(`INSERT INTO truck_driver_assignments(truck_id,driver_id,assigned_at) SELECT ('92000000-0000-4000-8000-'||lpad(t::text,12,'0'))::uuid,('91000000-0000-4000-8000-'||lpad(d::text,12,'0'))::uuid,$1 FROM (VALUES(1,1),(2,3),(3,4),(5,5),(6,6),(7,7)) v(t,d)`, monday.AddDate(0, 0, -14))
			exec(`INSERT INTO driver_teams(main_driver_id,team_driver_id,start_week) VALUES('91000000-0000-4000-8000-000000000001','91000000-0000-4000-8000-000000000002',$1)`, monday.AddDate(0, 0, -14).Format("2006-01-02"))
			// Fixture-only known baselines predate Monday. Production baselines are never rewritten.
			exec(`INSERT INTO fleet_status_history(entity_kind,entity_id,entity_name,track,previous_status,status,effective_at,actor_name,source,baseline)
   SELECT 'driver',id,full_name,track,'',CASE track WHEN 'employment' THEN 'active' ELSE availability_status END,$1,'Fixture','test',true FROM drivers CROSS JOIN (VALUES('employment'),('availability')) tracks(track)`, monday.AddDate(0, 0, -7))
			exec(`INSERT INTO fleet_status_history(entity_kind,entity_id,entity_name,track,previous_status,status,effective_at,actor_name,source,baseline)
   SELECT 'truck',id,unit_number,'condition','',CASE status WHEN 'maintenance' THEN 'shop' WHEN 'out_of_service' THEN 'out_of_service' WHEN 'hold' THEN 'hold' ELSE 'ready' END,$1,'Fixture','test',true FROM trucks WHERE unit_number NOT IN('PTI-5','PTI-new-midweek')`, monday.AddDate(0, 0, -7))
			exec(`UPDATE fleet_status_control SET enabled=true`)
			if err := repo.createPTIWeek(ctx, week, actor, monday.Add(time.Minute)); err != nil {
				t.Fatal(err)
			}
			initial, err := repo.PTIWeek(ctx, week)
			if err != nil || !initial.Exists || initial.LateSnapshot || len(initial.Rows) != 8 {
				t.Fatal(initial, err)
			}
			want := map[string]string{"PTI-1": "required", "PTI-2": "exempt", "PTI-3": "exempt", "PTI-4": "exempt", "PTI-5": "unknown", "PTI-6": "exempt", "PTI-7": "required", "PTI-8": "exempt"}
			var selected PTIRow
			for _, row := range initial.Rows {
				if row.Eligibility != want[row.TruckUnit] {
					t.Fatal(row.TruckUnit, row.Eligibility, want[row.TruckUnit])
				}
				if row.TruckUnit == "PTI-1" {
					selected = row
					if len(row.Crew) != 2 {
						t.Fatal("team duplicated or lost", row.Crew)
					}
				}
			}
			if err := repo.CreatePTIWeek(ctx, week, actor); err != nil {
				t.Fatal(err)
			}
			if scalar(`SELECT count(*)::text FROM weekly_pti_rows WHERE week_start=$1`, week) != "8" {
				t.Fatal("idempotent week creation failed")
			}
			if _, err = repo.PTIWeek(ctx, monday.AddDate(0, 0, -7).Format("2006-01-02")); err != nil {
				t.Fatal(err)
			}
			if scalar(`SELECT count(*)::text FROM weekly_pti_weeks`) != "1" {
				t.Fatal("read created a checklist")
			}
			oldWeek := monday.AddDate(0, 0, -7)
			if err = repo.createPTIWeek(ctx, oldWeek.Format("2006-01-02"), actor, oldWeek.AddDate(0, 0, 2)); err != nil {
				t.Fatal(err)
			}
			late, err := repo.PTIWeek(ctx, oldWeek.Format("2006-01-02"))
			if err != nil || !late.LateSnapshot {
				t.Fatal(late, err)
			}
			for _, row := range late.Rows {
				if row.Eligibility != "unknown" {
					t.Fatal("late snapshot invented Monday state", row)
				}
			}
			financialBefore := scalar(`SELECT (SELECT count(*) FROM expenses)::text||':'||(SELECT count(*) FROM driver_pay_weeks)::text||':'||(SELECT count(*) FROM relay_driver_operations)::text`)
			in := PTIChange{RequestID: "93000000-0000-4000-8000-000000000001", Version: selected.Version, Eligibility: "required", EligibilityReason: selected.EligibilityReason, Received: true, Review: "checked", VideoURL: "https://example.test/pti/1", Notes: "Video inspected"}
			changed, err := repo.ChangePTI(ctx, week, selected.TruckID, actor, in)
			if err != nil || changed.ReviewedBy != "PTI Reviewer" || changed.ReceivedBy != "PTI Reviewer" || changed.Version != 2 {
				t.Fatal(changed, err)
			}
			replay, err := repo.ChangePTI(ctx, week, selected.TruckID, actor, in)
			if err != nil || replay.Version != 2 {
				t.Fatal("retry not idempotent", replay, err)
			}
			stale := in
			stale.RequestID = "93000000-0000-4000-8000-000000000002"
			stale.Notes = "stale"
			if _, err = repo.ChangePTI(ctx, week, selected.TruckID, actor, stale); !errors.Is(err, ErrPTIConflict) {
				t.Fatal("stale overwrite", err)
			}
			next := in
			next.Version = changed.Version
			next.RequestID = "93000000-0000-4000-8000-000000000003"
			next.VideoURL = "https://example.test/pti/replacement"
			replaced, err := repo.ChangePTI(ctx, week, selected.TruckID, actor, next)
			if err != nil || replaced.Review != "pending" || replaced.ReviewedAt != nil || replaced.ReviewedBy != "" {
				t.Fatal("new video retained old review", replaced, err)
			}
			noop := next
			noop.Version = replaced.Version
			noop.Review = "pending"
			noop.RequestID = "93000000-0000-4000-8000-000000000004"
			if _, err = repo.ChangePTI(ctx, week, selected.TruckID, actor, noop); err != nil {
				t.Fatal(err)
			}
			events, err := repo.PTIHistory(ctx, week, selected.TruckID)
			if err != nil || len(events) != 2 {
				t.Fatal("no-op created audit", events, err)
			}
			if _, err = pool.Exec(ctx, `DELETE FROM weekly_pti_events`); err == nil {
				t.Fatal("audit was mutable")
			}
			bad := noop
			bad.Received = false
			bad.Review = "checked"
			if _, err = repo.ChangePTI(ctx, week, selected.TruckID, actor, bad); err == nil {
				t.Fatal("review without video")
			}
			bad = noop
			bad.VideoURL = "javascript:alert(1)"
			if _, err = repo.ChangePTI(ctx, week, selected.TruckID, actor, bad); err == nil {
				t.Fatal("unsafe video URL")
			}
			bad = noop
			bad.Eligibility = "exempt"
			bad.EligibilityReason = ""
			if _, err = repo.ChangePTI(ctx, week, selected.TruckID, actor, bad); err == nil {
				t.Fatal("unexplained eligibility correction")
			}
			d, _ := repo.GetDriver(ctx, "91000000-0000-4000-8000-000000000001")
			if _, err = repo.ChangeAvailability(ctx, d.ID, actor, FleetStatusChange{Status: "home", Version: d.StatusVersion}); err != nil {
				t.Fatal(err)
			}
			snapshot, err := repo.PTIWeek(ctx, week)
			if err != nil {
				t.Fatal(err)
			}
			for _, row := range snapshot.Rows {
				if row.TruckID == selected.TruckID && (row.Eligibility != "required" || row.Crew[0].Availability != "active") {
					t.Fatal("later availability rewrote Monday", row)
				}
			}
			financialAfter := scalar(`SELECT (SELECT count(*) FROM expenses)::text||':'||(SELECT count(*) FROM driver_pay_weeks)::text||':'||(SELECT count(*) FROM relay_driver_operations)::text`)
			if financialBefore != financialAfter {
				t.Fatal("PTI changed financial/provider state", financialBefore, financialAfter)
			}
		})
	}
}
