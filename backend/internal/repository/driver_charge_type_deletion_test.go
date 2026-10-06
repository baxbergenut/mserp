package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func testChargeTypeDeletion(t *testing.T, pool *pgxpool.Pool, actor, driver string) {
	t.Helper()
	ctx := context.Background()
	repo := NewDriverChargeRepository(pool)
	typ, err := repo.SaveType(ctx, ChargeType{Name: "Unused deletion type", Direction: "charge", Amount: "25"}, actor)
	if err != nil {
		t.Fatal(err)
	}
	if err = repo.DeleteType(ctx, typ.ID, typ.Version+1, actor); !errors.Is(err, ErrChargeConflict) {
		t.Fatal("stale deletion accepted", err)
	}
	if err = repo.DeleteType(ctx, typ.ID, typ.Version, actor); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM driver_charge_types WHERE id=$1`, typ.ID).Scan(&count); err != nil || count != 0 {
		t.Fatal("type still present", err)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM driver_charge_events WHERE type_id IS NULL AND details->>'id'=$1`, typ.ID).Scan(&count); err != nil || count != 2 {
		t.Fatal("audit snapshots lost", count, err)
	}
	if err = repo.DeleteType(ctx, typ.ID, typ.Version, actor); !errors.Is(err, ErrNotFound) {
		t.Fatal("missing deletion", err)
	}
	used, err := repo.SaveType(ctx, ChargeType{Name: "Protected deletion type", Direction: "charge", Amount: "30"}, actor)
	if err != nil {
		t.Fatal(err)
	}
	_, err = repo.Create(ctx, ChargeCreate{Kind: "recurring", DriverIDs: []string{driver}, TypeID: used.ID, Amount: "30", StartWeek: ChargeCurrentWeek(), Eligibility: "calendar"}, actor)
	if err != nil {
		t.Fatal(err)
	}
	var invalid *ChargeValidationError
	if err = repo.DeleteType(ctx, used.ID, used.Version, actor); !errors.As(err, &invalid) {
		t.Fatal("used driver type deleted", err)
	}
	truckType, err := repo.SaveType(ctx, ChargeType{Name: "Protected truck deletion type", Direction: "charge", Amount: "35"}, actor)
	if err != nil {
		t.Fatal(err)
	}
	var truck string
	if err = pool.QueryRow(ctx, `INSERT INTO trucks(unit_number) VALUES('DELETE-TEST') RETURNING id::text`).Scan(&truck); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO truck_charge_phases(truck_id,type_id,week_start,amount) VALUES($1,$2,$3::date,35)`, truck, truckType.ID, ChargeCurrentWeek()); err != nil {
		t.Fatal(err)
	}
	if err = repo.DeleteType(ctx, truckType.ID, truckType.Version, actor); !errors.As(err, &invalid) {
		t.Fatal("used truck type deleted", err)
	}
}
