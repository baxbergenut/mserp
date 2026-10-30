package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

type SplitAllocation struct {
	TripID       int    `json:"tripId"`
	OriginalRate string `json:"originalRate"`
}

type SplitReviewEntry struct {
	DriverName string          `json:"driverName"`
	Entry      GrossBoardEntry `json:"entry"`
}

// Sent with the ordinary versioned board save, so nested Daily loads drafts and
// autosave keep the same atomic boundary as the allocation and both drivers.
type LoadSplitReview struct {
	LoadID      int                `json:"loadId"`
	Version     int                `json:"version"`
	SourceHash  string             `json:"sourceHash"`
	Reason      string             `json:"reason"`
	Allocations []SplitAllocation  `json:"allocations"`
	Entries     []SplitReviewEntry `json:"entries"`
}

type LoadSplitWorkspace struct {
	LoadSplitReview
	LoadNumber   string           `json:"loadNumber"`
	Total        string           `json:"total"`
	Miles        string           `json:"miles"`
	Trips        []GrossBoardTrip `json:"trips"`
	ReviewReason string           `json:"reviewReason"`
}

func splitEntryKey(e GrossBoardEntry) string {
	return fmt.Sprintf("%s/%s/%d", e.DriverID, e.Date, e.Slot)
}

func (r *GrossBoardRepository) SplitWorkspace(ctx context.Context, id int) (LoadSplitWorkspace, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return LoadSplitWorkspace{}, err
	}
	defer tx.Rollback(ctx)
	result, err := readSplitWorkspace(ctx, tx, id)
	if err != nil {
		return result, err
	}
	return result, tx.Commit(ctx)
}

func readSplitWorkspace(ctx context.Context, tx pgx.Tx, id int) (LoadSplitWorkspace, error) {
	w := LoadSplitWorkspace{LoadSplitReview: LoadSplitReview{LoadID: id, Allocations: []SplitAllocation{}, Entries: []SplitReviewEntry{}}}
	var trips, saved []byte
	var split bool
	err := tx.QueryRow(ctx, `SELECT l.load_id,coalesce(l.total_pay::text,''),coalesce(l.total_miles::text,''),trip_source.options,
 trip_source.source_hash,trip_source.allocation_version,trip_source.review_reason,trip_source.split,l.split_allocation
 FROM loads l CROSS JOIN (SELECT NULL::integer load_trip_id) e `+loadTripSourceSQL+` WHERE l.id=$1`, id).
		Scan(&w.LoadNumber, &w.Total, &w.Miles, &trips, &w.SourceHash, &w.Version, &w.ReviewReason, &split, &saved)
	if errors.Is(err, pgx.ErrNoRows) {
		return w, ErrGrossBoardLoad
	}
	if err != nil {
		return w, err
	}
	if !split {
		return w, chargeInvalid("This load has no split trips")
	}
	if err = json.Unmarshal(trips, &w.Trips); err != nil {
		return w, err
	}
	if len(saved) > 0 {
		var prior LoadSplitReview
		if err = json.Unmarshal(saved, &prior); err != nil {
			return w, err
		}
		w.Allocations = prior.Allocations
		w.Reason = prior.Reason
	}
	rows, err := tx.Query(ctx, `SELECT e.driver_id,e.service_date::text,e.slot,e.version,e.load_number,l.id,e.load_trip_id,
 coalesce(e.original_rate::text,''),coalesce(e.entered_original_rate::text,''),coalesce(e.driver_rate::text,''),
 coalesce(e.miles::text,''),coalesce(e.entered_miles::text,''),e.note,d.full_name
 FROM `+grossBoardEntriesSQL+` e `+grossBoardResolvedLoad+` JOIN drivers d ON d.id=e.driver_id
 WHERE l.id=$1 AND NOT e.deleted AND e.day_status='' ORDER BY e.service_date,e.driver_id,e.slot`, id)
	if err != nil {
		return w, err
	}
	defer rows.Close()
	for rows.Next() {
		var item SplitReviewEntry
		e := &item.Entry
		if err = rows.Scan(&e.DriverID, &e.Date, &e.Slot, &e.Version, &e.LoadNumber, &e.LoadRecordID, &e.LoadTripID,
			&e.OriginalRate, &e.EnteredOriginalRate, &e.DriverRate, &e.Miles, &e.EnteredMiles, &e.Note, &item.DriverName); err != nil {
			return w, err
		}
		w.Entries = append(w.Entries, item)
	}
	return w, rows.Err()
}

// Caller holds fleet/financial locks. The load lock fences source refreshes;
// every linked board slot is versioned, including repeats outside this week.
func prepareSplitReviews(ctx context.Context, tx pgx.Tx, entries []GrossBoardEntry) ([]GrossBoardEntry, error) {
	inputs := map[string]bool{}
	reviewed := map[int]bool{}
	for _, e := range entries {
		inputs[splitEntryKey(e)] = true
	}
	extra := []GrossBoardEntry{}
	for i := range entries {
		e := &entries[i]
		request := e.SplitReview
		if request == nil {
			continue
		}
		if e.LoadRecordID == nil || request.LoadID != *e.LoadRecordID || e.LoadTripID == nil || e.Deleted || e.DayStatus != "" {
			return nil, ErrGrossBoardTrip
		}
		if reviewed[request.LoadID] {
			return nil, chargeInvalid("Review each split load once per save")
		}
		reviewed[request.LoadID] = true
		if len(request.Allocations) < 2 || len(request.Allocations) > 100 || len(request.Entries) > 1000 || len(strings.TrimSpace(request.Reason)) == 0 || len([]rune(request.Reason)) > 2000 {
			return nil, chargeInvalid("Provide all trip allocations and a review note (up to 2000 characters)")
		}
		if _, err := tx.Exec(ctx, `SELECT id FROM loads WHERE id=$1 FOR UPDATE`, request.LoadID); err != nil {
			return nil, err
		}
		current, err := readSplitWorkspace(ctx, tx, request.LoadID)
		if err != nil {
			return nil, err
		}
		if request.Version != current.Version || request.SourceHash != current.SourceHash {
			return nil, chargeInvalid("The split or DataTruck source changed. Discard unsaved changes, then reopen the split editor")
		}
		// A load can also be recognized in a different payroll week. Fence every
		// frozen snapshot of the parent order, including legacy number-only rows.
		var frozen bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM (
 SELECT report FROM payroll_settlements WHERE finalized UNION ALL SELECT report FROM investor_pay_weeks WHERE finalized) s
 CROSS JOIN LATERAL jsonb_array_elements(coalesce(s.report->'loads','[]')) item
 WHERE item->>'loadRecordId'=($1::integer)::text OR (item->>'loadRecordId' IS NULL AND lower(btrim(item->>'loadNumber'))=lower(btrim($2))))`, request.LoadID, current.LoadNumber).Scan(&frozen); err != nil {
			return nil, err
		}
		if frozen {
			return nil, chargeInvalid("Reopen all driver and investor statements containing this load before reviewing its split")
		}
		placementRows, err := tx.Query(ctx, `SELECT driver_id::text,truck_id::text,week_start::text FROM payroll_load_placements WHERE load_record_id=$1 AND included`, request.LoadID)
		if err != nil {
			return nil, err
		}
		type placement struct {
			driver, week string
			truck        *string
		}
		var placements []placement
		for placementRows.Next() {
			var p placement
			if err = placementRows.Scan(&p.driver, &p.truck, &p.week); err != nil {
				placementRows.Close()
				return nil, err
			}
			placements = append(placements, p)
		}
		err = placementRows.Err()
		placementRows.Close()
		if err != nil {
			return nil, err
		}
		for _, p := range placements {
			if err = assertTransferSettlementsOpen(ctx, tx, p.driver, p.truck, p.week); err != nil {
				return nil, err
			}
		}
		allocations := map[int]string{}
		var total, miles int64
		for _, a := range request.Allocations {
			amount, err := chargeCents(a.OriginalRate)
			if err != nil || a.OriginalRate == "" || a.TripID <= 0 || allocations[a.TripID] != "" {
				return nil, chargeInvalid("Each trip needs one valid original gross allocation")
			}
			allocations[a.TripID] = chargeMoney(amount)
			total += amount
		}
		orderTotal, err := chargeCents(current.Total)
		if err != nil || current.Total == "" || total != orderTotal {
			return nil, chargeInvalid("Original gross allocations must total the order's $%s", current.Total)
		}
		if len(current.Trips) != len(allocations) {
			return nil, chargeInvalid("Review every current DataTruck trip")
		}
		tripMiles := map[int]string{}
		for _, trip := range current.Trips {
			n, err := chargeCents(trip.Miles)
			if err != nil || trip.ID <= 0 || tripMiles[trip.ID] != "" || trip.DriverName == "" || trip.Miles == "" || trip.OriginalRate == "" || allocations[trip.ID] == "" {
				return nil, chargeInvalid("Trip identities, amounts or mileage are missing. Refresh or correct the source first")
			}
			miles += n
			tripMiles[trip.ID] = trip.Miles
		}
		orderMiles, err := chargeCents(current.Miles)
		if err != nil || current.Miles == "" || miles != orderMiles {
			return nil, chargeInvalid("Trip mileage must reconcile with the order before reviewing its split")
		}
		proposed := map[string]GrossBoardEntry{}
		for _, item := range request.Entries {
			key := splitEntryKey(item.Entry)
			if _, exists := proposed[key]; exists {
				return nil, chargeInvalid("A split entry can only appear once")
			}
			if _, err := chargeCents(item.Entry.DriverRate); item.Entry.DriverRate != "" && err != nil {
				return nil, chargeInvalid("Driver gross must be a valid decimal amount")
			}
			proposed[key] = item.Entry
		}
		seen := map[string]bool{}
		for _, item := range current.Entries {
			old := item.Entry
			key := splitEntryKey(old)
			p, ok := proposed[key]
			if !ok || old.Version != p.Version {
				return nil, ErrGrossBoardConflict
			}
			seen[key] = true
			if err = assertPaySourceOpen(ctx, tx, PaySourceAcceptance{DriverID: old.DriverID, Date: old.Date, Slot: old.Slot}); err != nil {
				return nil, err
			}
			if key == splitEntryKey(*e) {
				if e.Version != old.Version {
					return nil, ErrGrossBoardConflict
				}
				continue
			}
			if inputs[key] {
				return nil, chargeInvalid("Save other pending edits to this load before reviewing the split")
			}
			old.LoadTripID = p.LoadTripID
			old.DriverRate = p.DriverRate
			if old.LoadTripID == nil || allocations[*old.LoadTripID] == "" {
				return nil, chargeInvalid("Select a trip for every linked board entry")
			}
			old.OriginalRate = allocations[*old.LoadTripID]
			old.EnteredOriginalRate = old.OriginalRate
			old.Miles = tripMiles[*old.LoadTripID]
			old.EnteredMiles = old.Miles
			old.AcceptSystemValues = true
			extra = append(extra, old)
		}
		for key := range proposed {
			if !seen[key] && key != splitEntryKey(*e) {
				return nil, ErrGrossBoardConflict
			}
		}
		if allocations[*e.LoadTripID] == "" {
			return nil, ErrGrossBoardTrip
		}
		// All source/board/finalization fences passed. The surrounding transaction
		// rolls this back if any companion entry later fails its ordinary save guard.
		saved := *request
		saved.Version++
		saved.Entries = nil
		saved.Reason = strings.TrimSpace(saved.Reason)
		for j := range saved.Allocations {
			saved.Allocations[j].OriginalRate = allocations[saved.Allocations[j].TripID]
		}
		body, err := json.Marshal(saved)
		if err != nil {
			return nil, err
		}
		if _, err = tx.Exec(ctx, `UPDATE loads SET split_allocation=$2::jsonb WHERE id=$1`, request.LoadID, body); err != nil {
			return nil, err
		}
		e.OriginalRate = allocations[*e.LoadTripID]
		e.EnteredOriginalRate = e.OriginalRate
		e.Miles = tripMiles[*e.LoadTripID]
		e.EnteredMiles = e.Miles
		e.AcceptSystemValues = true
		e.SplitReview = nil
	}
	return append(entries, extra...), nil
}
