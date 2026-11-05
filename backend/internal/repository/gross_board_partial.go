package repository

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"mserp/internal/db"
)

type GrossBoardSaveLocation struct {
	DriverID   string `json:"driverId"`
	Date       string `json:"date"`
	Slot       int    `json:"slot"`
	LoadNumber string `json:"loadNumber,omitempty"`
}

type GrossBoardSaveIssue struct {
	GrossBoardSaveLocation
	Code        string                   `json:"code"`
	Message     string                   `json:"message"`
	Fields      []string                 `json:"fields"`
	Pending     []GrossBoardSaveLocation `json:"pending"`
	Source      map[string]string        `json:"source,omitempty"`
	CurrentLoad string                   `json:"currentLoad,omitempty"`
}

type GrossBoardSaveResult struct {
	Entries []GrossBoardEntry     `json:"entries"`
	Issues  []GrossBoardSaveIssue `json:"issues"`
}

type grossBoardRejection struct {
	cause       error
	location    GrossBoardSaveLocation
	code        string
	fields      []string
	source      map[string]string
	currentLoad string
}

func (e grossBoardRejection) Error() string { return e.cause.Error() }
func (e grossBoardRejection) Unwrap() error { return e.cause }

func grossBoardLocation(e GrossBoardEntry) GrossBoardSaveLocation {
	number := e.LoadNumber
	if number == "" && e.PreviousLoadNumber != nil {
		number = *e.PreviousLoadNumber
	}
	return GrossBoardSaveLocation{DriverID: e.DriverID, Date: e.Date, Slot: e.Slot, LoadNumber: number}
}

func locateGrossBoardRejection(entry GrossBoardEntry, err error) error {
	var rejection grossBoardRejection
	if !errors.As(err, &rejection) {
		rejection.cause = err
	}
	if rejection.location.DriverID == "" {
		rejection.location = grossBoardLocation(entry)
	}
	return rejection
}

func grossBoardIssue(err error, group []GrossBoardEntry) (GrossBoardSaveIssue, bool) {
	var rejection grossBoardRejection
	errors.As(err, &rejection)
	issue := GrossBoardSaveIssue{
		GrossBoardSaveLocation: rejection.location,
		Code:                   rejection.code, Fields: rejection.fields, Message: err.Error(), Source: rejection.source, CurrentLoad: rejection.currentLoad,
		Pending: make([]GrossBoardSaveLocation, 0, len(group)),
	}
	var validation *ChargeValidationError
	var pgErr *pgconn.PgError
	switch {
	case issue.Code != "":
	case errors.Is(err, ErrGrossBoardLoad):
		issue.Code, issue.Fields = "load_unavailable", []string{"loadNumber"}
	case errors.Is(err, ErrGrossBoardTrip):
		issue.Code, issue.Fields = "trip_review", []string{"loadTripId"}
	case errors.Is(err, ErrGrossBoardConflict), errors.Is(err, ErrDriverPayConflict), errors.Is(err, ErrTruckUnavailable), errors.As(err, &validation):
		issue.Code = "save_conflict"
	case errors.As(err, &pgErr) && pgErr.Code == "23503":
		issue.Code, issue.Message = "load_unavailable", "A driver or load was removed. Review the selected load before retrying."
	default:
		return GrossBoardSaveIssue{}, false
	}
	if issue.DriverID == "" {
		issue.GrossBoardSaveLocation = grossBoardLocation(group[0])
	}
	for _, entry := range group {
		location := grossBoardLocation(entry)
		issue.Pending = append(issue.Pending, location)
		if (location.DriverID == issue.DriverID && location.Date == issue.Date && location.Slot == issue.Slot) && len(issue.Fields) == 0 {
			issue.Fields = entry.Fields
			if entry.SplitReview != nil {
				issue.Fields = []string{"splitReview"}
			}
		}
	}
	return issue, true
}

// Try the complete batch first, as most saves succeed and related changes may
// depend on the final batch state. After a recoverable failure, roll it back
// and retry each independent group under the same native locks. Only report
// successful groups after the enclosing transaction commits.
func (r *GrossBoardRepository) SavePartialChanges(ctx context.Context, entries []GrossBoardEntry, actor string) (GrossBoardSaveResult, error) {
	finish := r.workPriority.interactive()
	defer finish()
	ctx = context.WithValue(ctx, payrollPublicationKey{}, false)
	for i := range entries {
		if entries[i].Fields == nil {
			entries[i].Fields = GrossBoardEditableFields
		}
	}
	finishLockWait := db.Phase(ctx, "gross_source_lock_wait")
	tx, err := beginPayrollMutation(ctx, r.pool)
	finishLockWait()
	if err != nil {
		return GrossBoardSaveResult{}, err
	}
	defer tx.Rollback(ctx)
	if err = lockGrossBoardWrite(ctx, tx, actor); err != nil {
		return GrossBoardSaveResult{}, err
	}
	attempt := func(group []GrossBoardEntry) ([]GrossBoardEntry, error) {
		savepoint, err := tx.Begin(ctx)
		if err != nil {
			return nil, err
		}
		saved, err := r.saveEntriesLocked(ctx, savepoint, slices.Clone(group))
		if err != nil {
			if rollbackErr := savepoint.Rollback(ctx); rollbackErr != nil {
				return nil, rollbackErr
			}
			return nil, err
		}
		return saved, savepoint.Commit(ctx)
	}
	result := GrossBoardSaveResult{Entries: []GrossBoardEntry{}, Issues: []GrossBoardSaveIssue{}}
	saved, err := attempt(entries)
	if err == nil {
		result.Entries = saved
	} else {
		issue, recoverable := grossBoardIssue(err, entries)
		if !recoverable {
			return GrossBoardSaveResult{}, err
		}
		groups, groupErr := grossBoardSaveGroups(ctx, tx, entries)
		if groupErr != nil {
			return GrossBoardSaveResult{}, groupErr
		}
		if len(groups) == 1 {
			result.Issues = append(result.Issues, issue)
		} else {
			for _, group := range groups {
				saved, err = attempt(group)
				if err == nil {
					result.Entries = append(result.Entries, saved...)
					continue
				}
				issue, recoverable = grossBoardIssue(err, group)
				if !recoverable {
					return GrossBoardSaveResult{}, err
				}
				result.Issues = append(result.Issues, issue)
			}
		}
	}
	if err = commitPayrollMutation(ctx, tx); err != nil {
		return GrossBoardSaveResult{}, err
	}
	return result, nil
}

func grossBoardSaveGroups(ctx context.Context, tx pgx.Tx, entries []GrossBoardEntry) ([][]GrossBoardEntry, error) {
	keys := make([]GrossBoardSaveLocation, len(entries))
	for i, entry := range entries {
		keys[i] = grossBoardLocation(entry)
	}
	body, err := json.Marshal(keys)
	if err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT input."driverId"::text,input.date::text,input.slot,coalesce(b.load_number,''),b.load_record_id
 FROM jsonb_to_recordset($1::jsonb) AS input("driverId" uuid,date date,slot integer)
 JOIN `+grossBoardEntriesSQL+` b ON (b.driver_id,b.service_date,b.slot)=(input."driverId",input.date,input.slot)
 WHERE NOT b.deleted AND b.day_status=''`, string(body))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	current := make(map[string]GrossBoardEntry)
	for rows.Next() {
		var entry GrossBoardEntry
		if err = rows.Scan(&entry.DriverID, &entry.Date, &entry.Slot, &entry.LoadNumber, &entry.LoadRecordID); err != nil {
			return nil, err
		}
		current[splitEntryKey(entry)] = entry
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return groupGrossBoardEdits(entries, current), nil
}

// A move can release a load number in one slot and acquire it in another.
// Group both the saved and proposed identities, plus the editor's original
// number for moves queued before another dispatcher changed a slot. Splits
// also connect their explicit companion slots, including pending clears.
func groupGrossBoardEdits(entries []GrossBoardEntry, current map[string]GrossBoardEntry) [][]GrossBoardEntry {
	parent := make([]int, len(entries))
	for i := range parent {
		parent[i] = i
	}
	root := func(i int) int {
		for parent[i] != i {
			parent[i] = parent[parent[i]]
			i = parent[i]
		}
		return i
	}
	claims := make(map[string]int)
	connect := func(i int, key string) {
		if other, ok := claims[key]; ok {
			parent[root(i)] = root(other)
		} else {
			claims[key] = i
		}
	}
	for i, entry := range entries {
		slot := splitEntryKey(entry)
		old := current[slot]
		connect(i, "slot:"+slot)
		var previous string
		if entry.PreviousLoadNumber != nil {
			previous = *entry.PreviousLoadNumber
		}
		for _, number := range []string{old.LoadNumber, entry.LoadNumber, previous} {
			if number = strings.ToLower(strings.TrimSpace(number)); number != "" {
				connect(i, "number:"+number)
			}
		}
		for _, id := range []*int{old.LoadRecordID, entry.LoadRecordID} {
			if id != nil {
				connect(i, "load:"+strconv.Itoa(*id))
			}
		}
		if review := entry.SplitReview; review != nil {
			connect(i, "load:"+strconv.Itoa(review.LoadID))
			for _, companion := range review.Entries {
				connect(i, "slot:"+splitEntryKey(companion.Entry))
			}
		}
	}
	var groups [][]GrossBoardEntry
	positions := make(map[int]int)
	for i, entry := range entries {
		r := root(i)
		position, ok := positions[r]
		if !ok {
			position = len(groups)
			positions[r] = position
			groups = append(groups, nil)
		}
		groups[position] = append(groups[position], entry)
	}
	return groups
}
