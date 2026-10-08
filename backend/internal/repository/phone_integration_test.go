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
	"mserp/internal/phone"
	"mserp/internal/relay"
)

func TestPhoneDatabase(t *testing.T) {
	dsn := os.Getenv("MSERP_PHONE_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set MSERP_PHONE_TEST_DATABASE_URL to a disposable _test database")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.Contains(cfg.ConnConfig.Database, "_test") {
		t.Fatal("a disposable _test database is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal("test database unavailable")
	}
	defer admin.Close(ctx)
	source, err := os.ReadFile("../../sql/init.sql")
	if err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile("../../sql/042_standardize_phones.sql")
	if err != nil {
		t.Fatal(err)
	}
	// Git checkouts may use different line endings for baseline and migration files.
	source = []byte(strings.ReplaceAll(string(source), "\r\n", "\n"))
	migration = []byte(strings.ReplaceAll(string(migration), "\r\n", "\n"))
	for _, mode := range []string{"fresh", "migration"} {
		t.Run(mode, func(t *testing.T) {
			schema := fmt.Sprintf("phone_test_%d", time.Now().UnixNano())
			quoted := pgx.Identifier{schema}.Sanitize()
			exec := func(sql string, args ...any) {
				t.Helper()
				if _, err := admin.Exec(ctx, sql, args...); err != nil {
					t.Fatal(err)
				}
			}
			exec(`CREATE SCHEMA ` + quoted + `; SET search_path TO ` + quoted + `,public; GRANT USAGE ON SCHEMA ` + quoted + ` TO mserp_app`)
			defer func() { _, _ = admin.Exec(ctx, `SET search_path TO public; DROP SCHEMA `+quoted+` CASCADE`) }()
			if mode == "migration" {
				before, _, ok := strings.Cut(string(source), string(migration))
				if !ok {
					t.Fatal("phone migration must match fresh schema")
				}
				exec(before)
				// A valid formatted contact, an invalid short contact, and a driver-
				// linked investor prove the cleanup runs before identity propagation.
				exec(`INSERT INTO drivers(id,full_name,normalized_name,pay_type,pay_rate,phone) VALUES
				 ('00000000-0000-0000-0000-000000000001','Formatted Driver','formatted driver','cpm',0,'+1 (470) 334-4443'),
				 ('00000000-0000-0000-0000-000000000002','Short Driver','short driver','cpm',0,'123');
				 INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate,phone) VALUES('Two Phones','two phones','cpm',0,'4703344443 / +1 (012) 345-6789');
				 INSERT INTO investors(full_name,driver_id,phone) VALUES('Formatted Driver','00000000-0000-0000-0000-000000000001','+1 (470) 334-4443');
				 INSERT INTO dispatchers(full_name,normalized_name,phone) VALUES('Empty Dispatcher','empty dispatcher',' ');
				 INSERT INTO relay_driver_links(relay_environment,relay_driver_id,relay_phone) VALUES('production','legacy','14703344443');
				 INSERT INTO fleetscope_driver_intake(company_id,fleetscope_driver_id,driver_data,normalized_name,occurred_at)
				 VALUES(gen_random_uuid(),gen_random_uuid(),'{"fullName":"Hire","phone":"+1 (470) 334-4443"}','hire',now());`)
				exec(string(migration))
				var auditCount, invalidCount int
				if err := admin.QueryRow(ctx, `SELECT count(*),count(*) FILTER(WHERE reason='invalid') FROM phone_normalization_audit`).Scan(&auditCount, &invalidCount); err != nil || auditCount != 7 || invalidCount != 1 {
					t.Fatalf("cleanup audit: %d %d %v", auditCount, invalidCount, err)
				}
				var cleaned, copy string
				if err := admin.QueryRow(ctx, `SELECT d.phone,i.phone FROM drivers d JOIN investors i ON i.driver_id=d.id`).Scan(&cleaned, &copy); err != nil || cleaned != "4703344443" || copy != cleaned {
					t.Fatalf("identity cleanup: %q %q %v", cleaned, copy, err)
				}
				var cleared *string
				if err := admin.QueryRow(ctx, `SELECT phone FROM drivers WHERE full_name='Short Driver'`).Scan(&cleared); err != nil || cleared != nil {
					t.Fatal("irrecoverable contact must be cleared and archived")
				}
				var primary, original string
				if err := admin.QueryRow(ctx, `SELECT d.phone,a.original_value FROM drivers d JOIN phone_normalization_audit a ON a.record_id=d.id AND a.source_table='drivers' WHERE d.full_name='Two Phones' AND a.reason='primary_selected'`).Scan(&primary, &original); err != nil || primary != "4703344443" || original != "4703344443 / +1 (012) 345-6789" {
					t.Fatal("primary contact selection lost the secondary", err)
				}
			} else {
				exec(string(source))
			}
			if mode == "migration" {
				// Current repository reads require the complete upgraded schema.
				applyLaterTestMigrations(t, ctx, admin, "042")
			}
			exec(`GRANT SELECT,INSERT,UPDATE,DELETE ON drivers,dispatchers,relay_driver_links TO mserp_app; GRANT SELECT ON app_users TO mserp_app`)
			appcfg := cfg.Copy()
			appcfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
			appcfg.ConnConfig.RuntimeParams["role"] = "mserp_app"
			pool, err := pgxpool.NewWithConfig(ctx, appcfg)
			if err != nil {
				t.Fatal(err)
			}
			defer pool.Close()
			for _, input := range []string{"", "0123456789", "+1 (470) 334-4443", "470.334.4443"} {
				var dbValue *string
				if err := pool.QueryRow(ctx, `SELECT canonical_phone($1)`, input).Scan(&dbValue); err != nil {
					t.Fatal(err)
				}
				want, _ := phone.Normalize(input)
				if stringValue(dbValue) != want {
					t.Fatalf("SQL/Go mismatch for %q", input)
				}
			}
			for _, input := range []string{"123", "bad4703344443", "4703344443/4703344444", "１２３４５６７８９０", "24703344443"} {
				if _, err := pool.Exec(ctx, `INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate,phone) VALUES('Invalid','invalid','cpm',0,$1)`, input); err == nil {
					t.Fatalf("database accepted %q", input)
				}
			}
			var driverID, canonical string
			if err := pool.QueryRow(ctx, `INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate,phone) VALUES('New Driver','new driver','cpm',0,'+1 (012) 345-6789') RETURNING id,phone`).Scan(&driverID, &canonical); err != nil || canonical != "0123456789" {
				t.Fatalf("valid app write: %q %v", canonical, err)
			}
			if _, err := pool.Exec(ctx, `INSERT INTO investors(full_name,driver_id,phone) VALUES('New Driver',$1,$2)`, driverID, canonical); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, `UPDATE drivers SET phone='(470) 334-4443' WHERE id=$1`, driverID); err != nil {
				t.Fatal(err)
			}
			if err := pool.QueryRow(ctx, `SELECT phone FROM investors WHERE driver_id=$1`, driverID).Scan(&canonical); err != nil || canonical != "4703344443" {
				t.Fatal("linked contacts diverged", err)
			}
			// A rollback-era webhook writer still supplies only the original JSON.
			var intakeID string
			if err := pool.QueryRow(ctx, `INSERT INTO fleetscope_driver_intake(company_id,fleetscope_driver_id,driver_data,normalized_name,occurred_at)
			 VALUES(gen_random_uuid(),gen_random_uuid(),'{"fullName":"Hire","phone":"+1 (470) 334-4443"}','hire',now()) RETURNING id,phone`).Scan(&intakeID, &canonical); err != nil || canonical != "4703344443" {
				t.Fatal("legacy intake write failed", err)
			}
			var raw string
			if err := pool.QueryRow(ctx, `SELECT driver_data->>'phone' FROM fleetscope_driver_intake WHERE id=$1`, intakeID).Scan(&raw); err != nil || raw != "+1 (470) 334-4443" {
				t.Fatal("source snapshot changed")
			}
			intake, err := NewFleetRepository(pool).GetDriverIntake(ctx, intakeID)
			if err != nil || intake.Driver.Phone != "4703344443" {
				t.Fatal("noncanonical intake response", err)
			}
			// Invalid upstream contact must not block fuel sync or become identity evidence.
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx)
			if _, err := ensureRelayDriver(ctx, tx, "production", relay.TransactionDriver{ID: "invalid-source", Phone: "123"}); err != nil {
				t.Fatal(err)
			}
			var invalid *string
			if err := tx.QueryRow(ctx, `SELECT relay_phone FROM relay_driver_links WHERE relay_driver_id='invalid-source'`).Scan(&invalid); err != nil || invalid != nil {
				t.Fatal("invalid contact persisted", err)
			}
			if err := tx.Rollback(ctx); err != nil {
				t.Fatal(err)
			}
		})
	}
}
