package repository

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"mserp/internal/fleetscope"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestFleetScopeIdentityRecoveryResourceProtocolDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		if _, err := admin.Exec(ctx, `UPDATE payroll_projection_control SET protocol_mode='resources',capture_enabled=true WHERE singleton`); err != nil {
			t.Fatal(err)
		}
		testFleetScopeIdentityRecovery(t, pool)
	})
}

func testFleetScopeIdentityRecovery(t *testing.T, pool *pgxpool.Pool) {
	ctx := context.Background()
	repo := NewFleetRepository(pool)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	uuid := func() string {
		var id string
		must(pool.QueryRow(ctx, `SELECT gen_random_uuid()::text`).Scan(&id))
		return id
	}
	for _, owned := range []bool{false, true} {
		driver, err := repo.CreateDriver(ctx, DriverInput{FullName: "Identity Recovery " + uuid(), Active: true, PayType: "cpm", PayRate: .75, EscrowAmount: "0"})
		must(err)
		input := TruckInput{UnitNumber: uuid(), Status: "available", Active: true, DriverID: &driver.ID}
		if owned {
			input.OwnerDriverID = &driver.ID
		}
		truck, err := repo.CreateTruck(ctx, input)
		must(err)
		event := fleetscope.Event{Version: 1, Type: "driver.terminated", EventID: uuid(), CompanyID: uuid(), OccurredAt: time.Now(), TerminationDate: boardLoadToday(time.Now()), Driver: fleetscope.Driver{ID: uuid(), FullName: driver.FullName}}
		receipt, err := repo.AcceptFleetScopeTermination(ctx, event, "identity-recovery")
		must(err)
		var updated time.Time
		must(pool.QueryRow(ctx, `SELECT updated_at FROM drivers WHERE id=$1`, driver.ID).Scan(&updated))
		review := FleetScopeIdentityReview{CompanyID: event.CompanyID, Review: "Explicit synthetic identity review", Links: []FleetScopeReviewedIdentity{{DriverID: driver.ID, FleetScopeDriverID: event.Driver.ID, UpdatedAt: updated, Evidence: "Reviewed UUID pair", TerminationID: receipt.TerminationID}}}
		preview, err := repo.ReviewFleetScopeIdentities(ctx, review, false)
		must(err)
		if preview.Linked != 1 || preview.Terminations != 1 || preview.Applied {
			t.Fatal(preview)
		}
		driver, err = repo.GetDriver(ctx, driver.ID)
		must(err)
		if !driver.Active {
			t.Fatal("preview wrote employment")
		}
		var count int
		must(pool.QueryRow(ctx, `SELECT count(*) FROM fleetscope_driver_identity WHERE company_id=$1`, event.CompanyID).Scan(&count))
		if count != 0 {
			t.Fatal("preview saved identity")
		}
		review.Links[0].UpdatedAt = updated.Add(-time.Second)
		if _, err = repo.ReviewFleetScopeIdentities(ctx, review, true); err == nil {
			t.Fatal("stale review applied")
		}
		review.Links[0].UpdatedAt = updated
		applied, err := repo.ReviewFleetScopeIdentities(ctx, review, true)
		must(err)
		if !applied.Applied || applied.Terminations != 1 {
			t.Fatal(applied)
		}
		driver, err = repo.GetDriver(ctx, driver.ID)
		must(err)
		truck, err = repo.GetTruck(ctx, truck.ID)
		must(err)
		if driver.Status != "terminated" || driver.Active {
			t.Fatal("driver not terminated")
		}
		if !owned {
			var ended string
			must(pool.QueryRow(ctx, `SELECT (unassigned_at AT TIME ZONE 'America/New_York')::date::text FROM truck_driver_assignments WHERE truck_id=$1 AND driver_id=$2 ORDER BY unassigned_at DESC NULLS LAST LIMIT 1`, truck.ID, driver.ID).Scan(&ended))
			if ended != boardLoadToday(time.Now()) {
				t.Fatalf("recovery backdated the assignment: got %s", ended)
			}
		}
		if owned {
			if truck.Status != "terminated" || truck.DriverID == nil {
				t.Fatal("owned truck not terminated with retained link")
			}
		} else {
			if truck.OperatingStatus != "ready" || truck.DriverID != nil {
				t.Fatal("company truck not ready")
			}
		}
		duplicate, err := repo.AcceptFleetScopeTermination(ctx, event, "identity-recovery")
		must(err)
		if duplicate.Status != "duplicate" {
			t.Fatal("webhook receipt changed")
		}
		must(pool.QueryRow(ctx, `SELECT updated_at FROM drivers WHERE id=$1`, driver.ID).Scan(&review.Links[0].UpdatedAt))
		replay, err := repo.ReviewFleetScopeIdentities(ctx, review, true)
		must(err)
		if replay.Terminations != 0 || replay.Existing != 1 {
			t.Fatal("recovery repeated")
		}
		review.Links[0].FleetScopeDriverID = uuid()
		review.Links[0].TerminationID = ""
		if _, err = repo.ReviewFleetScopeIdentities(ctx, review, true); err == nil {
			t.Fatal("conflicting identity replaced")
		}
	}
	// A reviewed link alone enables subsequent notifications without fleet sync.
	d, err := repo.CreateDriver(ctx, DriverInput{FullName: "Future Identity", Active: true, PayType: "cpm", PayRate: .75, EscrowAmount: "0"})
	must(err)
	var updated time.Time
	must(pool.QueryRow(ctx, `SELECT updated_at FROM drivers WHERE id=$1`, d.ID).Scan(&updated))
	company, remote := uuid(), uuid()
	review := FleetScopeIdentityReview{CompanyID: company, Review: "Future notifications", Links: []FleetScopeReviewedIdentity{{DriverID: d.ID, FleetScopeDriverID: remote, UpdatedAt: updated, Evidence: "Reviewed identity"}}}
	_, err = repo.ReviewFleetScopeIdentities(ctx, review, true)
	must(err)
	_, err = repo.AcceptFleetScopeTermination(ctx, fleetscope.Event{Version: 1, Type: "driver.terminated", EventID: uuid(), CompanyID: company, OccurredAt: time.Now(), TerminationDate: boardLoadToday(time.Now()), Driver: fleetscope.Driver{ID: remote, FullName: d.FullName}}, "future-reviewed")
	must(err)
	d, err = repo.GetDriver(ctx, d.ID)
	must(err)
	if d.Status != "terminated" {
		t.Fatal("future notification did not terminate linked driver")
	}
	// Resolving an older receipt for an already-terminated driver does not
	// replace the manual termination date or perform operational work again.
	_, err = pool.Exec(ctx, `DELETE FROM fleetscope_driver_identity WHERE company_id=$1`, company)
	must(err)
	otherRemote := uuid()
	oldDate := time.Now().AddDate(0, 0, -2).Format(time.DateOnly)
	old, err := repo.AcceptFleetScopeTermination(ctx, fleetscope.Event{Version: 1, Type: "driver.terminated", EventID: uuid(), CompanyID: company, OccurredAt: time.Now(), TerminationDate: oldDate, Driver: fleetscope.Driver{ID: otherRemote, FullName: d.FullName}}, "already-terminated")
	must(err)
	must(pool.QueryRow(ctx, `SELECT updated_at FROM drivers WHERE id=$1`, d.ID).Scan(&updated))
	review.Links[0] = FleetScopeReviewedIdentity{DriverID: d.ID, FleetScopeDriverID: otherRemote, UpdatedAt: updated, Evidence: "Explicit identity correction", TerminationID: old.TerminationID}
	_, err = repo.ReviewFleetScopeIdentities(ctx, review, true)
	must(err)
	var date string
	must(pool.QueryRow(ctx, `SELECT termination_date::text FROM drivers WHERE id=$1`, d.ID).Scan(&date))
	if date != boardLoadToday(time.Now()) {
		t.Fatal("recovery replaced existing termination date")
	}
	// A held receipt predating a current hire is rejected atomically.
	newHire, err := repo.CreateDriver(ctx, DriverInput{FullName: "Rehired Identity", Active: true, PayType: "cpm", PayRate: .75, EscrowAmount: "0"})
	must(err)
	_, err = pool.Exec(ctx, `UPDATE drivers SET hire_date=$2::date WHERE id=$1`, newHire.ID, boardLoadToday(time.Now()))
	must(err)
	rehireRemote := uuid()
	held, err := repo.AcceptFleetScopeTermination(ctx, fleetscope.Event{Version: 1, Type: "driver.terminated", EventID: uuid(), CompanyID: company, OccurredAt: time.Now(), TerminationDate: oldDate, Driver: fleetscope.Driver{ID: rehireRemote, FullName: newHire.FullName}}, "rehired-held")
	must(err)
	must(pool.QueryRow(ctx, `SELECT updated_at FROM drivers WHERE id=$1`, newHire.ID).Scan(&updated))
	review.Links[0] = FleetScopeReviewedIdentity{DriverID: newHire.ID, FleetScopeDriverID: rehireRemote, UpdatedAt: updated, Evidence: "Reviewed identity", TerminationID: held.TerminationID}
	if _, err = repo.ReviewFleetScopeIdentities(ctx, review, true); err == nil {
		t.Fatal("old termination applied to later hire")
	}
	var count int
	must(pool.QueryRow(ctx, `SELECT count(*) FROM fleetscope_driver_identity WHERE fleetscope_driver_id=$1`, rehireRemote).Scan(&count))
	if count != 0 {
		t.Fatal("rejected batch saved partial identity")
	}
}
