package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"mserp/internal/prepass"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPayrollNativePrePassPublicationDatabase(t *testing.T) {
	for _, scenario := range []string{"success", "publication_failure", "identity_handoff"} {
		t.Run(scenario, func(t *testing.T) {
			withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
				seedPayrollProjectionTest(t, ctx, pool)
				exec := func(sql string, args ...any) {
					t.Helper()
					if _, err := pool.Exec(ctx, sql, args...); err != nil {
						t.Fatal(err)
					}
				}
				id := func(sql string, args ...any) string {
					t.Helper()
					var result string
					if err := pool.QueryRow(ctx, sql, args...).Scan(&result); err != nil {
						t.Fatal(err)
					}
					return result
				}
				actor := id(`INSERT INTO app_users(username,password_hash) VALUES('PrePass publication',crypt('test-only',gen_salt('bf'))) RETURNING id::text`)
				const vin = "1XKADP9X8DJ364740"
				exec(`UPDATE trucks SET vin=$2,license_plate='LOCAL12',license_state='IN',updated_at=clock_timestamp() WHERE id=$1`, projectionTruck, vin)
				exec(`INSERT INTO prepass_fleet_links(environment,account_number,vin,truck_id,updated_by) VALUES('production','123',$1,$2,$3)`, vin, projectionTruck, actor)
				fake := &prepassFleetFake{snapshot: prepass.FleetSnapshot{Accounts: []string{"123"}, Vehicles: []prepass.Vehicle{{AccountNumber: "123", VIN: vin, VehicleNumber: "PROJECTION", LicensePlateNumber: "ABC123", LicensePlateState: "OH"}}, Transponders: []prepass.Transponder{}}}
				tolls := NewTollRepository(pool)
				tolls.ConfigurePrePassFleet(fake, "production")
				if err := tolls.RefreshPrePassFleet(ctx); err != nil {
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
				list, err := tolls.PrePassFleet(ctx)
				if err != nil || len(list.Vehicles) != 1 {
					t.Fatal(list, err)
				}
				var updated time.Time
				if err = pool.QueryRow(ctx, `SELECT updated_at FROM trucks WHERE id=$1`, projectionTruck).Scan(&updated); err != nil {
					t.Fatal(err)
				}
				in := PrePassFleetChange{RequestID: id(`SELECT gen_random_uuid()::text`), Action: "edit", PlateSource: "mserp", AccountNumber: "123", VIN: vin, TruckID: projectionTruck, TruckUpdatedAt: updated.Format(time.RFC3339Nano), Revision: list.Vehicles[0].Revision}
				versions := id(`SELECT count(*)::text FROM payroll_statement_versions`)
				if scenario == "publication_failure" {
					if _, err = admin.Exec(ctx, `CREATE FUNCTION fail_prepass_publication() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'test PrePass publication failure'; END $$;
 CREATE TRIGGER fail_prepass_publication BEFORE INSERT ON payroll_statement_versions FOR EACH ROW EXECUTE FUNCTION fail_prepass_publication()`); err != nil {
						t.Fatal(err)
					}
				}
				changed := make(chan error, 1)
				fake.beforeChange = func(call context.Context) error {
					tx, err := pool.Begin(call)
					if err != nil {
						return err
					}
					var unlocked bool
					if err = tx.QueryRow(call, `SELECT pg_try_advisory_xact_lock(736281940)`).Scan(&unlocked); err != nil || !unlocked {
						_ = tx.Rollback(call)
						return fmt.Errorf("provider call held payroll lock: %v", err)
					}
					if scenario != "identity_handoff" {
						return tx.Rollback(call)
					}
					// Queue a properly ordered local writer while the provider phase
					// holds the truck row. It completes when that phase releases the
					// row, before publication can acquire the financial lock.
					go func() {
						defer tx.Rollback(call)
						_, err := tx.Exec(call, `UPDATE trucks SET license_plate='NEWLOCAL',updated_at=clock_timestamp() WHERE id=$1`, projectionTruck)
						if err == nil {
							err = tx.Commit(call)
						}
						changed <- err
					}()
					return nil
				}
				err = tolls.ChangePrePassFleet(WithPayrollPublication(ctx), in, actor)
				if scenario == "identity_handoff" {
					if changeErr := <-changed; changeErr != nil {
						t.Fatal(changeErr)
					}
				}
				if fake.calls != 1 || fake.snapshot.Vehicles[0].LicensePlateNumber != "LOCAL12" {
					t.Fatal("provider write or lock boundary failed", fake.calls, err)
				}
				state := id(`SELECT state FROM prepass_fleet_operations WHERE id=$1`, in.RequestID)
				if scenario != "success" {
					if err == nil || state != "pending" || versions != id(`SELECT count(*)::text FROM payroll_statement_versions`) {
						t.Fatal("failed local publication changed receipt or statements", state, err)
					}
					if retry := tolls.ChangePrePassFleet(WithPayrollPublication(ctx), in, actor); retry == nil || fake.calls != 1 {
						t.Fatal("unconfirmed provider request was replayed", retry, fake.calls)
					}
					want := "LOCAL12"
					if scenario == "identity_handoff" {
						want = "NEWLOCAL"
					}
					if got := id(`SELECT license_plate FROM trucks WHERE id=$1`, projectionTruck); got != want {
						t.Fatal("local identity was overwritten", got, want)
					}
					return
				}
				if err != nil || state != "confirmed" {
					t.Fatal("local provider result did not commit", state, err)
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
						t.Fatalf("provider completion publication differs %s: %s %v\n%s\n%s", kind, page.State, err, a, b)
					}
				}
			})
		})
	}
}
