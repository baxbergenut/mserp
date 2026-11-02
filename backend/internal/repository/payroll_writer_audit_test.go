package repository

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPayrollPortalCapturePrivilegesDatabase(t *testing.T) {
	dsn := os.Getenv("MSERP_DRIVER_PAY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("local disposable payroll database required")
	}
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil || !strings.HasSuffix(cfg.Database, "_test") || (cfg.Host != "localhost" && cfg.Host != "127.0.0.1") {
		t.Fatal("local _test database required")
	}
	ctx := context.Background()
	setup, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	_, err = setup.Exec(ctx, `DO $$ BEGIN IF NOT EXISTS(SELECT 1 FROM pg_roles WHERE rolname='fleet_review_portal') THEN CREATE ROLE fleet_review_portal NOLOGIN; END IF; END $$`)
	_ = setup.Close(ctx)
	if err != nil {
		t.Fatal(err)
	}
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		seedPayrollProjectionTest(t, ctx, pool)
		// Reproduce the portal's previously granted canonical-table access without
		// accidentally granting any new derived-table or sequence privileges.
		_, err := admin.Exec(ctx, `DO $$ DECLARE item record; BEGIN
EXECUTE format('GRANT USAGE ON SCHEMA %I TO fleet_review_portal',current_schema());
FOR item IN SELECT c.relname,c.relkind FROM pg_class c WHERE c.relnamespace=current_schema()::regnamespace AND c.relkind IN ('r','S')
AND c.relname NOT LIKE 'payroll_projection_%' AND c.relname NOT LIKE 'payroll_statement_%' AND c.relname NOT LIKE 'payroll_command_%'
AND c.relname NOT IN ('payroll_resource_revisions','payroll_protocol_events','payroll_load_facts','payroll_routing_facts','payroll_cost_months','payroll_source_months') LOOP
IF item.relkind='S' THEN EXECUTE format('GRANT USAGE ON SEQUENCE %I TO fleet_review_portal',item.relname);
ELSE EXECUTE format('GRANT SELECT,INSERT,UPDATE,DELETE ON %I TO fleet_review_portal',item.relname); END IF;
END LOOP; END $$`)
		if err != nil {
			t.Fatal(err)
		}
		if err = NewDriverPayRepository(pool).EnablePayrollShadow(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `UPDATE payroll_projection_control SET protocol_mode='resources'`); err != nil {
			t.Fatal(err)
		}
		portal := pool.Config().Copy()
		portal.ConnConfig.RuntimeParams["role"] = "fleet_review_portal"
		writer, err := pgxpool.NewWithConfig(ctx, portal)
		if err != nil {
			t.Fatal(err)
		}
		defer writer.Close()
		tx, err := beginPayrollSourceMutation(ctx, writer)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		if _, err = tx.Exec(ctx, `UPDATE drivers SET full_name='Portal updated driver' WHERE id=$1`, projectionDriver); err != nil {
			t.Fatal(err)
		}
		if err = tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		var captured bool
		if err = pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM payroll_projection_changes WHERE table_name='drivers')`).Scan(&captured); err != nil || !captured {
			t.Fatal("restricted portal source change not captured", err)
		}
		var denied bool
		if err = writer.QueryRow(ctx, `SELECT NOT has_table_privilege(current_user,'payroll_projection_control','UPDATE')`).Scan(&denied); err != nil || !denied {
			t.Fatal("portal gained protocol-management access", err)
		}
	})
}

func TestPayrollAdditionalWriterGatesDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		seedPayrollProjectionTest(t, ctx, pool)
		if err := NewDriverPayRepository(pool).EnablePayrollShadow(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `UPDATE payroll_projection_control SET protocol_mode='resources'`); err != nil {
			t.Fatal(err)
		}
		hold, err := pool.Acquire(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer hold.Release()
		if _, err = hold.Exec(ctx, `SELECT pg_advisory_lock_shared(736281940)`); err != nil {
			t.Fatal(err)
		}
		defer hold.Exec(ctx, `SELECT pg_advisory_unlock_shared(736281940)`)
		fleet := NewFleetRepository(pool)
		// Nonexistent records deliberately distinguish entering the gate first
		// from proceeding to row lookup/locking before entering the protocol.
		// Inspection-only PTI writers deliberately bypass this gate; their inverse
		// concurrency and payroll invariants are covered by the PTI classification test.
		for _, test := range []struct {
			name string
			run  func(context.Context) error
		}{
			{"eld creation", func(ctx context.Context) error {
				_, err := fleet.CompleteFiveELDTruckReview(ctx, projectionDriver, "", 1, TruckInput{})
				return err
			}},
			{"odometer source", func(ctx context.Context) error {
				return (&MaintenanceRepository{pool: pool}).SaveOdometerSource(ctx, projectionDriver, OdometerSourceInput{Source: "auto"})
			}},
			{"feedback edit", func(ctx context.Context) error {
				return (&FeedbackRepository{pool: pool}).Update(ctx, projectionDriver, FeedbackInput{}, AuthUser{Administrator: true})
			}},
			{"feedback vote", func(ctx context.Context) error {
				return (&FeedbackRepository{pool: pool}).Vote(ctx, projectionDriver, projectionDriver, true)
			}},
		} {
			t.Run(test.name, func(t *testing.T) {
				short, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
				defer cancel()
				if err := test.run(short); !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("writer did not wait at the source gate before record access: %v", err)
				}
			})
		}
	})
}

func TestPayrollELDCreationPublicationDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		seedPayrollProjectionTest(t, ctx, pool)
		var actor string
		if err := pool.QueryRow(ctx, `INSERT INTO app_users(username,password_hash) VALUES('ELD publication',crypt('test-only',gen_salt('bf'))) RETURNING id::text`).Scan(&actor); err != nil {
			t.Fatal(err)
		}
		now := time.Now().UTC()
		vin := "1FUJGLDR0CLBP1234"
		if err := NewFiveELDRepository(pool).StoreLocations(ctx, []FiveELDLocation{{VIN: vin, ProviderTruckNumber: "NEW-ELD", Latitude: 40, Longitude: -80, ReportedAt: now, FetchedAt: now}}, []string{vin}, []string{}, 0, FiveELDSyncResult{SyncedAt: now}); err != nil {
			t.Fatal(err)
		}
		var id string
		var version int
		if err := pool.QueryRow(ctx, `SELECT id::text,eld_version FROM system_task_records WHERE kind='five_eld_truck' AND eld_vin=$1`, vin).Scan(&id, &version); err != nil {
			t.Fatal(err)
		}
		pay := NewDriverPayRepository(pool)
		if err := pay.EnablePayrollShadow(ctx); err != nil {
			t.Fatal(err)
		}
		week, _ := chargeWeek("2026-10-05")
		if err := pay.RequestPayrollPreparation(ctx, week, false); err != nil {
			t.Fatal(err)
		}
		if _, err := pay.PrepareNextPayrollWeek(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := admin.Exec(ctx, `CREATE FUNCTION fail_eld_publication() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'test ELD publication failure'; END $$;
CREATE TRIGGER fail_eld_publication BEFORE INSERT ON payroll_projection_generations FOR EACH ROW EXECUTE FUNCTION fail_eld_publication()`); err != nil {
			t.Fatal(err)
		}
		fleet := NewFleetRepository(pool)
		input := TruckInput{UnitNumber: "NEW-ELD", VIN: &vin, Active: true, Status: "available", IsCompanyOwned: true, AssignmentWeek: "2026-10-05"}
		if _, err := fleet.CompleteFiveELDTruckReview(WithPayrollPublication(ctx), id, actor, version, input); err == nil {
			t.Fatal("creation committed despite publication failure")
		}
		var unchanged bool
		if err := pool.QueryRow(ctx, `SELECT completed_at IS NULL AND NOT EXISTS(SELECT 1 FROM trucks WHERE vin=$2) FROM system_task_records WHERE id=$1`, id, vin).Scan(&unchanged); err != nil || !unchanged {
			t.Fatal("creation/review not rolled back", err)
		}
		if _, err := admin.Exec(ctx, `DROP TRIGGER fail_eld_publication ON payroll_projection_generations; DROP FUNCTION fail_eld_publication()`); err != nil {
			t.Fatal(err)
		}
		if _, err := fleet.CompleteFiveELDTruckReview(WithPayrollPublication(ctx), id, actor, version, input); err != nil {
			t.Fatal(err)
		}
		tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
		if err != nil {
			t.Fatal(err)
		}
		full, err := readPayrollProjectionReference(ctx, tx, week, true)
		_ = tx.Rollback(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for i, kind := range []string{"driver", "investor"} {
			page, err := pay.ReadPreparedPayrollPage(ctx, kind, week, PayPageQuery{})
			want, _ := json.Marshal(PaginatePay(full[i], PayPageQuery{}))
			got, _ := json.Marshal(page.Report)
			if err != nil || page.State != "ready" || string(want) != string(got) {
				t.Fatalf("ELD publication parity %s: %v\n%s\n%s", kind, err, want, got)
			}
		}
	})
}
