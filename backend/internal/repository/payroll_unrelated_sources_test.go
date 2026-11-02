package repository

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"mserp/internal/prepass"
	"mserp/internal/relay"
	"mserp/internal/weighmytruck"
	"os"
	"strings"
	"testing"
	"time"
)

func withoutUnrelatedPayrollMigration(t *testing.T, source string) string {
	t.Helper()
	b, e := os.ReadFile("../../sql/192_payroll_unrelated_sources.sql")
	if e != nil {
		t.Fatal(e)
	}
	return strings.Replace(source, strings.ReplaceAll(string(b), "\r\n", "\n"), "", 1)
}
func applyUnrelatedPayrollMigration(t *testing.T, ctx context.Context, c *pgx.Conn) {
	t.Helper()
	b, e := os.ReadFile("../../sql/192_payroll_unrelated_sources.sql")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = c.Exec(ctx, string(b)); e != nil {
		t.Fatal(e)
	}
}

func TestPayrollUnrelatedSourcesDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		seedPayrollProjectionTest(t, ctx, pool)
		var actor, membership, dispatcherUser string
		if e := pool.QueryRow(ctx, `INSERT INTO app_users(username,password_hash,role_id) SELECT 'Operational actor',crypt('test-only',gen_salt('bf')),id FROM app_roles WHERE system_role RETURNING id::text`).Scan(&actor); e != nil {
			t.Fatal(e)
		}
		if e := pool.QueryRow(ctx, `INSERT INTO weighmytruck_memberships(driver_id,email,first_name,last_name,phone) VALUES($1,'metadata@example.test','Test','Driver','3125550000') RETURNING id::text`, projectionDriver).Scan(&membership); e != nil {
			t.Fatal(e)
		}
		if e := pool.QueryRow(ctx, `INSERT INTO app_users(username,password_hash,role_id) SELECT 'Dispatcher metadata',crypt('test-only',gen_salt('bf')),id FROM app_roles WHERE dispatcher_role RETURNING id::text`).Scan(&dispatcherUser); e != nil {
			t.Fatal(e)
		}
		if _, e := pool.Exec(ctx, `INSERT INTO dispatchers(full_name,normalized_name,user_account_id) VALUES('Dispatcher metadata','dispatcher metadata',$1)`, dispatcherUser); e != nil {
			t.Fatal(e)
		}
		repo := NewDriverPayRepository(pool)
		if e := repo.EnablePayrollShadow(ctx); e != nil {
			t.Fatal(e)
		}
		if _, e := pool.Exec(ctx, `UPDATE payroll_projection_control SET protocol_mode='resources'`); e != nil {
			t.Fatal(e)
		}
		week, _ := chargeWeek("2026-10-05")
		if e := repo.RequestPayrollPreparation(ctx, week, false); e != nil {
			t.Fatal(e)
		}
		if done, e := repo.PrepareNextPayrollWeek(ctx); e != nil || !done {
			t.Fatal(done, e)
		}
		read := func() (string, [2]DriverPayWeek, string) {
			t.Helper()
			tx, e := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
			if e != nil {
				t.Fatal(e)
			}
			defer tx.Rollback(ctx)
			f, e := payrollProjectionFence(ctx, tx)
			if e != nil {
				t.Fatal(e)
			}
			r, e := readPayrollProjectionReference(ctx, tx, week, true)
			if e != nil {
				t.Fatal(e)
			}
			var state string
			if e = tx.QueryRow(ctx, `SELECT jsonb_build_array((SELECT jsonb_agg(to_jsonb(s) ORDER BY kind,statement_id,week_start) FROM payroll_statement_state s),(SELECT jsonb_agg(to_jsonb(j) ORDER BY week_start) FROM payroll_projection_jobs j))::text`).Scan(&state); e != nil {
				t.Fatal(e)
			}
			return f, r, state
		}
		before, reports, state := read()
		hold, e := admin.Begin(ctx)
		if e != nil {
			t.Fatal(e)
		}
		defer hold.Rollback(ctx)
		if _, e = hold.Exec(ctx, `SELECT pg_advisory_xact_lock_shared(736281940)`); e != nil {
			t.Fatal(e)
		}
		work, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		// Every exclusion must agree with the database and have no capture/gate
		// triggers. Empty updates catch statement-level locks without fixture noise.
		for _, table := range payrollUnrelatedTables {
			var classification, column string
			var triggers int
			e = pool.QueryRow(work, `SELECT s.classification,(SELECT count(*) FROM pg_trigger WHERE tgrelid=s.table_name::regclass AND (tgname LIKE 'payroll_projection_%' OR tgname LIKE 'payroll_protocol_%')),(SELECT attname FROM pg_attribute WHERE attrelid=s.table_name::regclass AND attnum>0 AND NOT attisdropped AND attgenerated='' AND attidentity='' ORDER BY attnum LIMIT 1) FROM payroll_projection_sources s WHERE table_name=$1`, table).Scan(&classification, &triggers, &column)
			if e == pgx.ErrNoRows {
				continue
			}
			if e != nil {
				t.Fatal(table, e)
			}
			// Derived tables are excluded from the source fingerprint too; only the
			// newly reviewed operational tables are exercised as writable sources.
			if classification == "derived" {
				continue
			}
			if classification != "unrelated" || triggers != 0 {
				t.Fatal(table, classification, triggers)
			}
			if _, e = pool.Exec(work, "UPDATE "+pgx.Identifier{table}.Sanitize()+" SET "+pgx.Identifier{column}.Sanitize()+"="+pgx.Identifier{column}.Sanitize()+" WHERE false"); e != nil {
				t.Fatal(table, e)
			}
		}
		feedback := NewFeedbackRepository(pool)
		user := AuthUser{ID: actor, Username: "Operational actor", Administrator: true}
		post := "19200000-0000-0000-0000-000000000001"
		maintenance := NewMaintenanceRepository(pool)
		interval := 10000
		truck := projectionTruck
		for _, action := range []struct {
			name string
			run  func() error
		}{
			{"feedback create", func() error {
				_, e := feedback.Create(work, FeedbackInput{ID: post, Title: "Workflow", Body: "Review", Tags: []string{}}, user, "test", nil)
				return e
			}},
			{"feedback edit", func() error {
				return feedback.Update(work, post, FeedbackInput{Title: "Reviewed", Body: "Done", Tags: []string{}, Status: "completed", Version: 1}, user)
			}},
			{"feedback vote", func() error { return feedback.Vote(work, post, actor, true) }},
			{"maintenance reading", func() error {
				_, e := maintenance.SaveReading(work, MaintenanceReading{TruckID: projectionTruck, Date: "2026-10-05", Mileage: 1000, ActorName: "Operator"})
				return e
			}},
			{"maintenance source", func() error {
				return maintenance.SaveOdometerSource(work, projectionTruck, OdometerSourceInput{Source: "auto"})
			}},
			{"maintenance schedule", func() error {
				_, e := maintenance.SaveSchedule(work, MaintenanceSchedule{TruckID: &truck, ServiceCode: "pm", IntervalMiles: &interval, WarningMiles: 10, Enabled: true})
				return e
			}},
			{"service program", func() error {
				_, e := maintenance.SaveProgram(work, ServiceProgram{Name: "Inspection", IntervalMiles: &interval, Active: true}, actor)
				return e
			}},
			{"WMT membership", func() error {
				return NewWeighMyTruckRepository(pool, nil).Verify(work, membership, WMTVerification{Version: 1, Enrolled: true, Reason: "Verified website"}, actor)
			}},
			{"inventory", func() error {
				_, e := NewInventoryRepository(pool).Save(work, "assets", InventorySave{RequestID: inventoryID(), Record: InventoryRecord{ID: inventoryID(), Data: InventoryFields{Active: true, DeviceType: "tablet", IMEI: "000000000000001"}}}, InventoryActor{ID: actor, Name: "Operator"})
				return e
			}},
			{"role", func() error {
				return NewAuthRepository(pool).SaveRole(work, actor, AccessRole{Name: "Operational review", Permissions: []string{"feedback.read"}})
			}},
			{"theme", func() error { return NewAuthRepository(pool).SetTheme(work, actor, "dark-modern") }},
			{"dispatcher theme", func() error { return NewAuthRepository(pool).SetTheme(work, dispatcherUser, "dark-modern") }},
			{"password", func() error {
				var hash string
				if e := pool.QueryRow(work, `SELECT password_hash FROM app_users WHERE id=$1`, actor).Scan(&hash); e != nil {
					return e
				}
				return NewAuthRepository(pool).ChangePassword(work, actor, hash, hash+"-changed")
			}},
			{"revoke sessions", func() error { return NewAuthRepository(pool).RevokeAccess(work, actor, actor) }},
			{"commission rules read", func() error { _, e := NewDispatcherPayRepository(pool).Rules(work); return e }},
		} {
			if e = action.run(); e != nil {
				t.Fatal(action.name, e)
			}
		}
		_ = hold.Rollback(ctx)
		after, updated, afterState := read()
		if before != after || !samePayrollProjection(reports, updated) || state != afterState {
			t.Fatal("operational saves changed payroll inputs, values, pointers or jobs")
		}
		if count, e := repo.DispatchPayrollChanges(ctx); e != nil || count != 0 {
			t.Fatal("operational saves queued payroll", count, e)
		}
		for _, kind := range []string{"driver", "investor"} {
			p, e := repo.ReadPreparedPayrollPage(ctx, kind, week, PayPageQuery{})
			if e != nil || p.State != "ready" {
				t.Fatal(kind, p.State, e)
			}
		}
		// The bypass cannot change identity, including mixed metadata edits.
		for _, set := range []string{"username='Changed',theme='default-light'", "active=false", "email='changed@example.test'", "role_id=NULL", "id=gen_random_uuid()"} {
			tx, e := beginPayrollUserMetadata(ctx, pool)
			if e != nil {
				t.Fatal(e)
			}
			_, e = tx.Exec(ctx, "UPDATE app_users SET "+set+" WHERE id=$1", actor)
			_ = tx.Rollback(ctx)
			if e == nil {
				t.Fatal("metadata bypass admitted identity change", set)
			}
		}
		if _, e = pool.Exec(ctx, `UPDATE app_users SET username='Renamed actor' WHERE id=$1`, actor); e != nil {
			t.Fatal(e)
		}
		after, _, _ = read()
		if after == before {
			t.Fatal("actor rename did not invalidate payroll")
		}
		if _, e = pool.Exec(ctx, `UPDATE truck_settlement_terms SET share_percent=80 WHERE truck_id=$1`, projectionTruck); e != nil {
			t.Fatal(e)
		}
		financial, _, _ := read()
		if financial == after {
			t.Fatal("financial terms stopped invalidating payroll")
		}
	})
}

func TestPayrollRepeatedImportsDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		seedPayrollProjectionTest(t, ctx, pool)
		week, _ := chargeWeek("2026-10-05")
		fuel := NewFuelRepository(pool)
		tolls := NewTollRepository(pool)
		scale := NewWeighMyTruckRepository(pool, nil)
		purchase := relayTestPurchase(t, "repeat-fuel", "repeat-relay", week.Add(12*time.Hour), relay.Transaction{})
		ticket := weighmytruck.Ticket{Number: "repeat", Site: "1", Date: "2026-10-05", Fee: "15.25", Tractor: "PROJECTION", Raw: map[string]string{"Ticket Number": "repeat"}}
		toll := prepass.Transaction{TollID: 19201, AccountNumber: json.Number("1"), PostDateTime: "2026-10-05T12:00:00", ExitDateTime: "2026-10-05T12:00:00", VehicleNumber: "PROJECTION", PPDeviceID: "REPEAT", TollCharge: json.Number("2.50")}
		sync := func() {
			t.Helper()
			if e := fuel.UpsertDay(ctx, "production", week, []relay.Transaction{purchase}, time.Now(), true); e != nil {
				t.Fatal(e)
			}
			if e := scale.StoreSettlements(ctx, ticket.Date, []weighmytruck.Ticket{ticket}); e != nil {
				t.Fatal(e)
			}
			if _, e := tolls.UpsertDay(ctx, "production", week, []prepass.Transaction{toll}, time.Now(), true); e != nil {
				t.Fatal(e)
			}
		}
		sync()
		repo := NewDriverPayRepository(pool)
		if e := repo.EnablePayrollShadow(ctx); e != nil {
			t.Fatal(e)
		}
		if _, e := pool.Exec(ctx, `UPDATE payroll_projection_control SET protocol_mode='resources'`); e != nil {
			t.Fatal(e)
		}
		fence := func() string {
			tx, e := pool.Begin(ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer tx.Rollback(ctx)
			f, e := payrollProjectionFence(ctx, tx)
			if e != nil {
				t.Fatal(e)
			}
			return f
		}
		before := fence()
		sync()
		if fence() != before {
			t.Fatal("identical provider refresh invalidated payroll")
		}
		if n, e := repo.DispatchPayrollChanges(ctx); e != nil || n != 0 {
			t.Fatal("identical import queued rebuilding", n, e)
		}
		// Changed cost data must still invalidate, including a changed fuel item.
		purchase = relayTestPurchase(t, "repeat-fuel", "repeat-relay", week.Add(12*time.Hour), relay.Transaction{})
		purchase.FuelItems[0].FuelTypeDescription = "Changed source evidence"
		sync()
		after := fence()
		if after == before {
			t.Fatal("changed fuel item disappeared")
		}
		ticket.Fee = "16.25"
		sync()
		next := fence()
		if next == after {
			t.Fatal("changed scale amount disappeared")
		}
		toll.TollCharge = json.Number("3.50")
		sync()
		if fence() == next {
			t.Fatal("changed toll amount disappeared")
		}
	})
}
