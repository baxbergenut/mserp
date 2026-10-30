package repository

import (
	"context"
	"testing"
)

func testDriverBoardSheetStatuses(t *testing.T, ctx context.Context, repo *DriverBoardRepository, fleet *FleetRepository) {
	t.Helper()
	driver, err := fleet.CreateDriver(ctx, DriverInput{FullName: "Sheet Status Driver", PayType: "cpm", Active: true})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := repo.pool.Exec(ctx, `UPDATE drivers SET active=false WHERE id=$1`, driver.ID); err != nil {
			t.Error(err)
		}
	}()
	entry := DriverBoardEntry{DriverID: driver.ID, Notes: "Preserve notes"}
	for _, status := range []string{"READY", "REST", "COVERED", "DISPATCHED", "ENROUTE", "RESERVED", "VACATION", "DEADHEAD", "HOME", "SHOP", "STOP", "In the YARD", "Hold for Load", "Left", "Family issue", "Issue with PU/Del", "Health issue", "NEW DRIVER", "ACCIDENT"} {
		entry.Status, entry.StatusEdited = status, true
		saved, err := repo.Save(ctx, []DriverBoardEntry{entry})
		if err != nil {
			t.Fatalf("save sheet status %q: %v", status, err)
		}
		entry = saved[0]
		if entry.Status != status || entry.Notes != "Preserve notes" {
			t.Fatalf("sheet status lost: %+v", entry)
		}
	}
}
