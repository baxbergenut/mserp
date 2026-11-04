package repository

import (
	"context"
	"errors"
	"reflect"
	"time"

	"github.com/jackc/pgx/v5"
)

var GrossBoardEditableFields = []string{"loadNumber", "loadRecordId", "loadTripId", "dayStatus", "deleted", "originalRate", "driverRate", "miles", "enteredOriginalRate", "enteredMiles", "note", "acceptSystemValues", "acceptDriverGross"}

// SaveChanges is the interactive Gross Board contract. There is no caller
// version precondition or statement history. Under the financial write lock,
// each patch updates just the selected fields of the latest row. Legacy clients
// that omit fields retain their full-row replacement behavior.
func (r *GrossBoardRepository) SaveChanges(ctx context.Context, entries []GrossBoardEntry, actor string) ([]GrossBoardEntry, error) {
	finish := r.workPriority.interactive()
	defer finish()
	for i := range entries {
		if entries[i].Fields == nil {
			entries[i].Fields = GrossBoardEditableFields
		}
	}
	// A board acknowledgement means its canonical rows are durable. Source
	// revisions and pending payroll work commit in this same transaction, so
	// prepared payroll immediately becomes stale and cannot be finalized. The
	// durable worker publishes it separately, including after a process restart.
	// Do not make a dispatcher wait for every affected statement calculation.
	ctx = context.WithValue(ctx, payrollPublicationKey{}, false)
	return r.SaveEntries(ctx, entries, actor)
}

func mergeGrossBoardPatch(ctx context.Context, tx pgx.Tx, patch GrossBoardEntry) (GrossBoardEntry, bool, error) {
	date, err := time.Parse(time.DateOnly, patch.Date)
	if err != nil {
		return GrossBoardEntry{}, false, err
	}
	current := GrossBoardEntry{DriverID: patch.DriverID, Date: patch.Date, Slot: patch.Slot}
	err = scanGrossBoardEntry(tx.QueryRow(ctx, grossBoardEffectiveForDriver()+"SELECT "+grossBoardEntryColumns+
		" FROM effective WHERE driver_id=$2 AND service_date=$3::date AND slot=$4", date, patch.DriverID, patch.Date, patch.Slot), &current)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return current, false, err
	}
	before := current
	if patch.AcceptDriverGross && (current.Version != patch.Version || !reflect.DeepEqual(current.LoadRecordID, patch.LoadRecordID) || !reflect.DeepEqual(current.LoadTripID, patch.LoadTripID)) {
		return current, false, ErrGrossBoardConflict
	}
	for _, field := range patch.Fields {
		switch field {
		case "loadNumber":
			current.LoadNumber = patch.LoadNumber
		case "loadRecordId":
			current.LoadRecordID = patch.LoadRecordID
		case "loadTripId":
			current.LoadTripID = patch.LoadTripID
		case "dayStatus":
			current.DayStatus = patch.DayStatus
		case "deleted":
			current.Deleted = patch.Deleted
		case "originalRate":
			current.OriginalRate = patch.OriginalRate
		case "driverRate":
			current.DriverRate = patch.DriverRate
		case "miles":
			current.Miles = patch.Miles
		case "enteredOriginalRate":
			current.EnteredOriginalRate = patch.EnteredOriginalRate
		case "enteredMiles":
			current.EnteredMiles = patch.EnteredMiles
		case "note":
			current.Note = patch.Note
		case "acceptSystemValues":
			current.AcceptSystemValues = patch.AcceptSystemValues
		case "acceptDriverGross":
			current.AcceptDriverGross = patch.AcceptDriverGross
		default:
			return current, false, chargeInvalid("Unknown Gross Board field: %s", field)
		}
	}
	current.SplitReview = patch.SplitReview
	compare := current
	for _, pair := range []struct {
		next *string
		old  string
	}{
		{&compare.OriginalRate, before.OriginalRate}, {&compare.DriverRate, before.DriverRate},
		{&compare.Miles, before.Miles}, {&compare.EnteredOriginalRate, before.EnteredOriginalRate}, {&compare.EnteredMiles, before.EnteredMiles},
	} {
		if *pair.next != "" && pair.old != "" {
			a, aErr := chargeCents(*pair.next)
			b, bErr := chargeCents(pair.old)
			if aErr == nil && bErr == nil && a == b {
				*pair.next = pair.old
			}
		}
	}
	return current, before.Version > 0 && patch.SplitReview == nil && reflect.DeepEqual(compare, before), nil
}
