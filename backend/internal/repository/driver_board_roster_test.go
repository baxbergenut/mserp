package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func testDriverBoardDepartureWeek(t *testing.T, ctx context.Context, pool *pgxpool.Pool, fleet *FleetRepository) {
	t.Helper()
	loc, _ := time.LoadLocation("America/New_York")
	today := time.Now().In(loc)
	week, _ := time.ParseInLocation(time.DateOnly, ChargeCurrentWeek(), loc)
	var dispatcher string
	if err := pool.QueryRow(ctx, `INSERT INTO dispatchers(full_name,normalized_name) VALUES('Departure dispatcher','departure dispatcher') RETURNING id`).Scan(&dispatcher); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, `DELETE FROM dispatchers WHERE id=$1`, dispatcher)
	d, err := fleet.CreateDriver(ctx, DriverInput{FullName: "Departure driver", PayType: "cpm", PayRate: 0.5, Active: true, DispatcherID: &dispatcher, AssignmentWeek: week.AddDate(0, 0, -7).Format(time.DateOnly)})
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, `DELETE FROM drivers WHERE id=$1`, d.ID)
	truck, err := fleet.CreateTruck(ctx, TruckInput{UnitNumber: "DEPARTURE-075", DriverID: &d.ID, Status: "available", Active: true, IsCompanyOwned: true, AssignmentWeek: week.AddDate(0, 0, -7).Format(time.DateOnly)})
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, `DELETE FROM trucks WHERE id=$1`, truck.ID)
	if _, err = pool.Exec(ctx, `INSERT INTO gross_board_entries(driver_id,service_date,load_number,original_rate,driver_rate,entered_original_rate) VALUES($1,$2,'DEPARTURE-LOAD',2600,2600,2600)`, d.ID, week); err != nil {
		t.Fatal(err)
	}
	d, err = fleet.GetDriver(ctx, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Exercise real offboarding, including the legacy Monday assignment boundary.
	_, err = fleet.ChangeDriverStatus(ctx, d.ID, "", DriverStatusChange{Status: "terminated", TerminationDate: today.Format(time.DateOnly), AssignmentWeek: week.Format(time.DateOnly), ChargePauseWeek: week.Format(time.DateOnly), UpdatedAt: d.UpdatedAt})
	if err != nil {
		t.Fatal(err)
	}
	repo := NewDriverBoardRepository(pool)
	repo.now = func() time.Time { return today }
	board, err := repo.Get(ctx, week)
	if err != nil {
		t.Fatal(err)
	}
	var retained *DriverBoardDriver
	var entry DriverBoardEntry
	for i := range board.Drivers {
		if board.Drivers[i].ID == d.ID {
			retained = &board.Drivers[i]
		}
	}
	for _, e := range board.Entries {
		if e.DriverID == d.ID {
			entry = e
		}
	}
	if retained == nil || !retained.Departed || retained.DispatcherID != dispatcher || retained.TruckUnit != truck.UnitNumber || retained.Location != nil || entry.Status != "Left" {
		t.Fatalf("departure row: %+v entry: %+v", retained, entry)
	}
	foundGross := false
	for _, e := range board.GrossEntries {
		if e.DriverID == d.ID && e.DriverRate == "2600.00" {
			foundGross = true
		}
	}
	if !foundGross {
		t.Fatal("departure gross disappeared")
	}
	// The visible roster and mutation revision must agree, including Left rows.
	if err = repo.SetDriverAutoOrder(ctx, dispatcher, true, ""); err != nil {
		t.Fatal(err)
	}
	board, err = repo.Get(ctx, week)
	if err != nil {
		t.Fatal(err)
	}
	destination := ""
	_, err = repo.ReorderDrivers(ctx, DriverBoardOrderAction{DispatcherID: dispatcher, DriverID: d.ID, Revision: board.DriverOrderRevisions[dispatcher], TargetDispatcherID: &destination, TargetRevision: board.DriverOrderRevisions[""]}, "")
	if !errors.Is(err, ErrDriverBoardOrderConflict) {
		t.Fatal("departed driver transfer accepted", err)
	}
	if _, err = repo.Save(ctx, []DriverBoardEntry{entry}); !errors.Is(err, ErrDriverBoardConflict) {
		t.Fatal("departed driver edit accepted", err)
	}
	for _, check := range []struct {
		at      time.Time
		visible bool
	}{
		{week.AddDate(0, 0, 7).Add(-time.Nanosecond), true},
		{week.AddDate(0, 0, 7), false},
	} {
		repo.now = func() time.Time { return check.at }
		b, e := repo.Get(ctx, week)
		if e != nil {
			t.Fatal(e)
		}
		visible := false
		for _, row := range b.Drivers {
			if row.ID == d.ID {
				visible = true
			}
		}
		if visible != check.visible {
			t.Fatalf("boundary %s visible=%v", check.at, visible)
		}
	}
	// Viewing old gross after Monday must not restore a live board row or change work.
	var rate string
	if err = pool.QueryRow(ctx, `SELECT driver_rate::text FROM gross_board_entries WHERE driver_id=$1`, d.ID).Scan(&rate); err != nil || rate != "2600.00" {
		t.Fatal("historical gross changed", rate, err)
	}
}
