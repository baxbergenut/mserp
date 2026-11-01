package repository

import (
	"context"
	"encoding/json"
	"mserp/internal/fleetscope"
	"mserp/internal/relay"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPayrollNativeIntakePublicationDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		seedPayrollProjectionTest(t, ctx, pool)
		var actor string
		if err := pool.QueryRow(ctx, `INSERT INTO app_users(username,password_hash) VALUES('Intake publication',crypt('test-only',gen_salt('bf'))) RETURNING id::text`).Scan(&actor); err != nil {
			t.Fatal(err)
		}
		fleet := NewFleetRepository(pool)
		event := fleetscope.Event{Version: 1, Type: "driver.hired", EventID: "18200000-0000-0000-0000-000000002001", CompanyID: "18200000-0000-0000-0000-000000002002", OccurredAt: time.Now(), Driver: fleetscope.Driver{ID: "18200000-0000-0000-0000-000000002003", FullName: "Intake Publication", FirstName: "Intake", LastName: "Publication", DriverType: "company", HireDate: "2026-10-05"}}
		intake, err := fleet.AcceptFleetScopeHire(ctx, event, "payroll-intake-test")
		if err != nil {
			t.Fatal(err)
		}
		pay := NewDriverPayRepository(pool)
		if err = pay.EnablePayrollShadow(ctx); err != nil {
			t.Fatal(err)
		}
		week, _ := chargeWeek("2026-10-05")
		if err = pay.RequestPayrollPreparation(ctx, week, false); err != nil {
			t.Fatal(err)
		}
		if _, err = pay.PrepareNextPayrollWeek(ctx); err != nil {
			t.Fatal(err)
		}
		input := DriverInput{FullName: "Intake Publication", FirstName: payTestString("Intake"), LastName: payTestString("Publication"), HireDate: &week, PayType: "cpm", PayRate: 0.65, Active: true, AssignmentWeek: "2026-10-05"}
		if _, err = admin.Exec(ctx, `CREATE FUNCTION fail_intake_publication() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'test intake publication failure'; END $$;
 CREATE TRIGGER fail_intake_publication BEFORE INSERT ON payroll_statement_versions FOR EACH ROW EXECUTE FUNCTION fail_intake_publication()`); err != nil {
			t.Fatal(err)
		}
		if _, err = fleet.CompleteDriverIntake(WithPayrollPublication(ctx), intake.IntakeID, actor, "", &input, false); err == nil {
			t.Fatal("intake committed without payroll publication")
		}
		var unchanged bool
		if err = pool.QueryRow(ctx, `SELECT completed_at IS NULL AND driver_id IS NULL AND NOT EXISTS(SELECT 1 FROM drivers WHERE full_name='Intake Publication') FROM fleetscope_driver_intake WHERE id=$1`, intake.IntakeID).Scan(&unchanged); err != nil || !unchanged {
			t.Fatal("failed publication created a driver or completed intake", unchanged, err)
		}
		if _, err = admin.Exec(ctx, `DROP TRIGGER fail_intake_publication ON payroll_statement_versions; DROP FUNCTION fail_intake_publication()`); err != nil {
			t.Fatal(err)
		}
		if _, err = fleet.CompleteDriverIntake(WithPayrollPublication(ctx), intake.IntakeID, actor, "", &input, false); err != nil {
			t.Fatal(err)
		}
		read, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
		if err != nil {
			t.Fatal(err)
		}
		full, err := readPayrollProjectionReference(ctx, read, week, true)
		_ = read.Rollback(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for i, kind := range []string{"driver", "investor"} {
			page, err := pay.ReadPreparedPayrollPage(ctx, kind, week, PayPageQuery{})
			a, _ := json.Marshal(PaginatePay(full[i], PayPageQuery{}))
			b, _ := json.Marshal(page.Report)
			if err != nil || page.State != "ready" || string(a) != string(b) {
				t.Fatalf("intake publication differs %s: %v\n%s\n%s", kind, err, a, b)
			}
		}
	})
}

func TestPayrollNativeRelayPublicationDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		seedPayrollProjectionTest(t, ctx, pool)
		var actor string
		if err := pool.QueryRow(ctx, `INSERT INTO app_users(username,password_hash) VALUES('Relay publication',crypt('test-only',gen_salt('bf'))) RETURNING id::text`).Scan(&actor); err != nil {
			t.Fatal(err)
		}
		week, _ := chargeWeek("2026-10-05")
		fuel := NewFuelRepository(pool)
		purchase := relayTestPurchase(t, "native-payroll-fuel", "native-payroll-identity", week.Add(12*time.Hour), relay.Transaction{})
		if err := fuel.UpsertDay(ctx, "production", week, []relay.Transaction{purchase}, week, true); err != nil {
			t.Fatal(err)
		}
		var identity string
		if err := pool.QueryRow(ctx, `SELECT id::text FROM relay_driver_links WHERE driver_id IS NULL`).Scan(&identity); err != nil {
			t.Fatal(err)
		}
		pay := NewDriverPayRepository(pool)
		if err := pay.EnablePayrollShadow(ctx); err != nil {
			t.Fatal(err)
		}
		if err := pay.RequestPayrollPreparation(ctx, week, false); err != nil {
			t.Fatal(err)
		}
		if _, err := pay.PrepareNextPayrollWeek(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := admin.Exec(ctx, `CREATE FUNCTION fail_relay_publication() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'test Relay publication failure'; END $$;
 CREATE TRIGGER fail_relay_publication BEFORE INSERT ON payroll_statement_versions FOR EACH ROW EXECUTE FUNCTION fail_relay_publication()`); err != nil {
			t.Fatal(err)
		}
		if _, err := fuel.ReviewRelayIdentity(WithPayrollPublication(ctx), identity, projectionDriver, "link", actor); err == nil {
			t.Fatal("fuel identity committed without payroll publication")
		}
		var unchanged bool
		if err := pool.QueryRow(ctx, `SELECT driver_id IS NULL AND NOT EXISTS(SELECT 1 FROM relay_identity_reviews WHERE identity_id=$1)
 AND NOT EXISTS(SELECT 1 FROM fuel_transactions WHERE driver_id IS NOT NULL) FROM relay_driver_links WHERE id=$1`, identity).Scan(&unchanged); err != nil || !unchanged {
			t.Fatal("failed publication left a fuel link, backfill or audit", unchanged, err)
		}
		if _, err := admin.Exec(ctx, `DROP TRIGGER fail_relay_publication ON payroll_statement_versions; DROP FUNCTION fail_relay_publication()`); err != nil {
			t.Fatal(err)
		}
		if n, err := fuel.ReviewRelayIdentity(WithPayrollPublication(ctx), identity, projectionDriver, "link", actor); err != nil || n != 1 {
			t.Fatal("fuel attribution", n, err)
		}
		read, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
		if err != nil {
			t.Fatal(err)
		}
		full, err := readPayrollProjectionReference(ctx, read, week, true)
		_ = read.Rollback(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for i, kind := range []string{"driver", "investor"} {
			page, err := pay.ReadPreparedPayrollPage(ctx, kind, week, PayPageQuery{})
			a, _ := json.Marshal(PaginatePay(full[i], PayPageQuery{}))
			b, _ := json.Marshal(page.Report)
			if err != nil || page.State != "ready" || string(a) != string(b) {
				t.Fatalf("fuel identity publication differs %s: %v\n%s\n%s", kind, err, a, b)
			}
		}
	})
}

func TestPayrollNativeAvailabilityPublicationDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		seedPayrollProjectionTest(t, ctx, pool)
		if _, err := pool.Exec(ctx, `UPDATE fleet_status_control SET enabled=true`); err != nil {
			t.Fatal(err)
		}
		repo := NewDriverPayRepository(pool)
		if err := repo.EnablePayrollShadow(ctx); err != nil {
			t.Fatal(err)
		}
		week, _ := chargeWeek("2026-10-05")
		if err := repo.RequestPayrollPreparation(ctx, week, false); err != nil {
			t.Fatal(err)
		}
		if _, err := repo.PrepareNextPayrollWeek(ctx); err != nil {
			t.Fatal(err)
		}
		var version int
		var status string
		if err := pool.QueryRow(ctx, `SELECT status_version,availability_status FROM drivers WHERE id=$1`, projectionDriver).Scan(&version, &status); err != nil {
			t.Fatal(err)
		}
		if _, err := admin.Exec(ctx, `CREATE FUNCTION fail_availability_publication() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'test availability publication failure'; END $$;
 CREATE TRIGGER fail_availability_publication BEFORE INSERT ON payroll_statement_versions FOR EACH ROW EXECUTE FUNCTION fail_availability_publication()`); err != nil {
			t.Fatal(err)
		}
		fleet := NewFleetRepository(pool)
		input := FleetStatusChange{Status: "vacation", Version: version, EffectiveAt: week.Add(12 * time.Hour), Reason: "Reviewed availability"}
		if _, err := fleet.ChangeAvailability(WithPayrollPublication(ctx), projectionDriver, "", input); err == nil {
			t.Fatal("availability committed without payroll publication")
		}
		var unchanged bool
		if err := pool.QueryRow(ctx, `SELECT status_version=$2 AND availability_status=$3 FROM drivers WHERE id=$1`, projectionDriver, version, status).Scan(&unchanged); err != nil || !unchanged {
			t.Fatal("failed publication left an availability change", unchanged, err)
		}
		if _, err := admin.Exec(ctx, `DROP TRIGGER fail_availability_publication ON payroll_statement_versions; DROP FUNCTION fail_availability_publication()`); err != nil {
			t.Fatal(err)
		}
		if _, err := fleet.ChangeAvailability(WithPayrollPublication(ctx), projectionDriver, "", input); err != nil {
			t.Fatal(err)
		}
		read, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
		if err != nil {
			t.Fatal(err)
		}
		full, err := readPayrollProjectionReference(ctx, read, week, true)
		_ = read.Rollback(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for i, kind := range []string{"driver", "investor"} {
			page, err := repo.ReadPreparedPayrollPage(ctx, kind, week, PayPageQuery{})
			a, _ := json.Marshal(PaginatePay(full[i], PayPageQuery{}))
			b, _ := json.Marshal(page.Report)
			if err != nil || page.State != "ready" || string(a) != string(b) {
				t.Fatalf("availability publication differs %s: %v\n%s\n%s", kind, err, a, b)
			}
		}
	})
}

func TestPayrollNativeTollPublicationDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		seedPayrollProjectionTest(t, ctx, pool)
		var actor, toll string
		if err := pool.QueryRow(ctx, `INSERT INTO app_users(username,password_hash) VALUES('Native publication',crypt('test-only',gen_salt('bf'))) RETURNING id::text`).Scan(&actor); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `INSERT INTO tolls(posting_date,invoice_date,customer_id,source,read_type,transponder_or_plate,equipment_unit,agency,exit_plaza,exit_date,exit_time,toll_class,amount,row_fingerprint)
 VALUES('2026-10-06','2026-10-06','','','','','PROJECTION','','','2026-10-06','12:00','',40,repeat('n',64)) RETURNING id::text`).Scan(&toll); err != nil {
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
		repo := NewTollRepository(pool)
		input := PrePassTruckLinkInput{ID: "18200000-0000-0000-0000-000000000092", TruckID: projectionTruck, TransactionIDs: []string{toll}}
		preview, err := repo.PreviewPrePassTruckLink(ctx, input)
		if err != nil || preview.Count != 1 || preview.Closed {
			t.Fatal("link preview", preview, err)
		}
		input.Revision = preview.Revision
		if _, err = admin.Exec(ctx, `CREATE FUNCTION fail_native_publication() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'test publication failure'; END $$;
 CREATE TRIGGER fail_native_publication BEFORE INSERT ON payroll_statement_versions FOR EACH ROW EXECUTE FUNCTION fail_native_publication()`); err != nil {
			t.Fatal(err)
		}
		if err = repo.LinkPrePassTruck(WithPayrollPublication(ctx), input, actor); err == nil {
			t.Fatal("source edit committed without its statements")
		}
		var untouched bool
		if err = pool.QueryRow(ctx, `SELECT truck_id IS NULL AND NOT EXISTS(SELECT 1 FROM toll_transaction_links WHERE id=$2) FROM tolls WHERE id=$1`, toll, input.ID).Scan(&untouched); err != nil || !untouched {
			t.Fatal("failed publication left a source edit or audit receipt", untouched, err)
		}
		if _, err = admin.Exec(ctx, `DROP TRIGGER fail_native_publication ON payroll_statement_versions; DROP FUNCTION fail_native_publication()`); err != nil {
			t.Fatal(err)
		}
		if err = repo.LinkPrePassTruck(WithPayrollPublication(ctx), input, actor); err != nil {
			t.Fatal(err)
		}
		// No worker dispatch is necessary between the successful mutation and read.
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
			if err != nil || page.State != "ready" {
				t.Fatal("native publication not immediately current", kind, page.State, err)
			}
			a, _ := json.Marshal(PaginatePay(full[i], PayPageQuery{}))
			b, _ := json.Marshal(page.Report)
			if string(a) != string(b) {
				t.Fatalf("native publication differs %s\n%s\n%s", kind, a, b)
			}
		}
		if err = repo.LinkPrePassTruck(WithPayrollPublication(ctx), input, actor); err != nil {
			t.Fatal("native receipt replay", err)
		}
	})
}

func TestPayrollNativeLedgerPublicationDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		seedPayrollProjectionTest(t, ctx, pool)
		const independent = "18200000-0000-0000-0000-000000001400"
		if _, err := pool.Exec(ctx, `INSERT INTO drivers(id,full_name,normalized_name,pay_type,pay_rate,hire_date)
 VALUES('18200000-0000-0000-0000-000000001400','Independent ledger','independent ledger','cpm',0.65,'2026-01-01');
 INSERT INTO gross_board_entries(driver_id,service_date,load_number,driver_rate,entered_original_rate,entered_miles)
 VALUES('18200000-0000-0000-0000-000000001400','2026-10-05','INDEPENDENT',200,300,120)`); err != nil {
			t.Fatal(err)
		}
		repo := NewDriverPayRepository(pool)
		if err := repo.EnablePayrollShadow(ctx); err != nil {
			t.Fatal(err)
		}
		week, _ := chargeWeek("2026-10-05")
		if err := repo.RequestPayrollPreparation(ctx, week, false); err != nil {
			t.Fatal(err)
		}
		if _, err := repo.PrepareNextPayrollWeek(ctx); err != nil {
			t.Fatal(err)
		}
		before, err := repo.ReadPreparedPayrollPage(ctx, "driver", week, PayPageQuery{})
		if err != nil {
			t.Fatal(err)
		}
		write := WithPayrollPublication(ctx)
		tx, err := beginPayrollMutation(write, pool)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		if _, err = tx.Exec(ctx, `INSERT INTO driver_pay_cost_collections(driver_id,week_start,fuel_base,toll_base,fuel_amount,toll_amount) VALUES($1,$2,0,0,0,0)`, projectionDriver, week); err != nil {
			t.Fatal(err)
		}
		if err = commitPayrollMutation(write, tx); err != nil {
			t.Fatal(err)
		}
		reference, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
		if err != nil {
			t.Fatal(err)
		}
		full, err := readPayrollProjectionReference(ctx, reference, week, true)
		_ = reference.Rollback(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for i, kind := range []string{"driver", "investor"} {
			after, err := repo.ReadPreparedPayrollPage(ctx, kind, week, PayPageQuery{})
			a, _ := json.Marshal(PaginatePay(full[i], PayPageQuery{}))
			b, _ := json.Marshal(after.Report)
			if err != nil || after.State != "ready" || string(a) != string(b) {
				t.Fatal("native ledger publication differs", kind, err, string(a), string(b))
			}
			if kind == "driver" && before.Statements[independent].Revision != after.Statements[independent].Revision {
				t.Fatal("native ledger edit reconstructed unrelated statement")
			}
		}
	})
}
