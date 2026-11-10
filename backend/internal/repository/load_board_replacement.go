package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"mserp/internal/datatruck"
)

// Confirmed missing links and unlinked number-only slots with a current candidate need
// provider work. Finalized driver weeks are filtered before spending requests;
// all driver/investor/transfer/placement guards are checked again under lock.
func (r *LoadRepository) MissingBoardReplacementIDs(ctx context.Context) ([]int, error) {
	rows, err := r.pool.Query(ctx, `WITH numbers AS MATERIALIZED (
 SELECT DISTINCT lower(btrim(e.load_number)) number FROM `+grossBoardEntriesSQL+` e
 LEFT JOIN load_source_availability a ON a.load_id=e.load_record_id
 WHERE NOT e.deleted AND e.day_status='' AND btrim(e.load_number)<>''
 AND (e.load_record_id IS NULL OR NOT a.present)
 AND NOT EXISTS(SELECT 1 FROM payroll_settlements s WHERE s.finalized
   AND s.driver_id=payroll_driver_at(e.driver_id,date_trunc('week',e.service_date)::date)
   AND s.week_start=date_trunc('week',e.service_date)::date)
 ) SELECT l.id FROM loads l JOIN numbers n ON lower(btrim(l.load_id))=n.number
 WHERE EXISTS(SELECT 1 FROM loads candidate WHERE lower(btrim(candidate.load_id))=n.number
   AND NOT EXISTS(SELECT 1 FROM load_source_availability a WHERE a.load_id=candidate.id AND NOT a.present))
 ORDER BY l.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []int{}
	for rows.Next() {
		var id int
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// The user-approved replacement rule uses the displayed business number once
// complete provider evidence proves exactly one remaining ID. Dates/revenue may
// have changed on a replacement; local entered values are retained for review.
func prepareMissingLoadReplacement(g *LoadReconciliationGroup) {
	if g.Hold != "" {
		return
	}
	var current *LoadReconciliationSource
	missing := map[int]bool{}
	known := map[int]bool{}
	for i := range g.Sources {
		s := &g.Sources[i]
		known[s.ID] = true
		if s.Split {
			g.Hold = "Trip or split allocations require individual review"
			return
		}
		if s.Present {
			if current != nil {
				g.Hold = "More than one provider record remains present"
				return
			}
			current = s
		} else {
			missing[s.ID] = true
		}
	}
	// A single unlinked slot has no predecessor identity to replace. Link only
	// after complete provider evidence proves one live source; never combine
	// unresolved slots or reinterpret an existing explicit selection.
	if current != nil && len(g.Before) == 1 && g.Before[0].LoadRecordID == nil {
		keep := g.Before[0]
		if keep.LoadTripID != nil {
			g.Hold = "Trip allocation requires individual review"
			return
		}
		g.CurrentID = current.ID
		keep.LoadRecordID = &g.CurrentID
		g.DriverGross = keep.DriverRate
		if keep.EnteredOriginalRate == "" {
			keep.EnteredOriginalRate = keep.OriginalRate
		}
		if keep.EnteredMiles == "" {
			keep.EnteredMiles = keep.Miles
		}
		keep.OriginalRate, keep.Miles = keep.EnteredOriginalRate, keep.EnteredMiles
		keep.AcceptSystemValues, keep.AcceptDriverGross = false, false
		g.After = append(g.After, keep)
		return
	}
	if current == nil || len(missing) == 0 {
		g.Hold = "No unique current replacement with missing predecessors"
		return
	}
	g.CurrentID = current.ID
	if len(g.Before) == 0 {
		return
	}
	first := g.Before[0]
	seen := map[int]bool{}
	var gross int64
	hasGross, hasMissing := false, false
	survivor := 0
	for i, e := range g.Before {
		if e.DriverID != first.DriverID || e.Date != first.Date {
			g.Hold = "Multiple drivers or board dates require individual review"
			return
		}
		if e.LoadRecordID == nil || !known[*e.LoadRecordID] || seen[*e.LoadRecordID] || e.LoadTripID != nil {
			g.Hold = "Unresolved/repeated identity or trip requires review"
			return
		}
		seen[*e.LoadRecordID] = true
		hasMissing = hasMissing || missing[*e.LoadRecordID]
		if *e.LoadRecordID == current.ID {
			survivor = i
		}
		// Combining independently edited original amounts, miles or notes would
		// require choosing which edit wins. A single link retains all its edits.
		if len(g.Before) > 1 && (e.EnteredOriginalRate != "" || e.EnteredMiles != "" || (e.Note != nil && strings.TrimSpace(*e.Note) != "")) {
			g.Hold = "Multiple entries with manual values or notes require review"
			return
		}
		if e.DriverRate != "" {
			cents, err := chargeCents(e.DriverRate)
			if err != nil {
				g.Hold = "Invalid driver gross requires review"
				return
			}
			gross += cents
			hasGross = true
		} else if len(g.Before) > 1 {
			g.Hold = "Multiple entries with unknown driver gross require review"
			return
		}
	}
	if !hasMissing {
		return
	}
	keep := g.Before[survivor]
	keep.LoadRecordID = &g.CurrentID
	if hasGross {
		g.DriverGross = chargeMoney(gross)
		keep.DriverRate = g.DriverGross
	}
	// Empty entered values remain source-driven. Nonempty manual values stay
	// unchanged and continue to compare against the replacement's source values.
	keep.OriginalRate, keep.Miles = keep.EnteredOriginalRate, keep.EnteredMiles
	keep.AcceptSystemValues, keep.AcceptDriverGross = false, false
	g.After = append(g.After, keep)
	for i, e := range g.Before {
		if i != survivor {
			g.After = append(g.After, GrossBoardEntry{DriverID: e.DriverID, Date: e.Date, Slot: e.Slot, Version: e.Version, Deleted: e.Slot > 0})
		}
	}
}

// Provider calls finish before taking source/financial locks. Rebuilding the
// plan under those locks checks the complete identity set, current source
// values, local edits and finalization together. Successful repairs stop being
// candidates, so retries cannot consolidate twice.
func (r *LoadRepository) ReplaceMissingBoardLoads(ctx context.Context, ids []int, live []datatruck.Load) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	requested := make(map[int]bool, len(ids))
	for _, id := range ids {
		if id <= 0 || requested[id] {
			return 0, fmt.Errorf("invalid replacement lookup identity set")
		}
		requested[id] = true
	}
	returned := make(map[int]bool, len(live))
	for _, load := range live {
		if !requested[load.ID] || returned[load.ID] {
			return 0, fmt.Errorf("invalid replacement provider evidence")
		}
		returned[load.ID] = true
	}
	ctx = WithPayrollPublication(ctx)
	tx, err := beginPayrollMutation(ctx, r.pool)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	present := make([]int, 0, len(live))
	for _, load := range live {
		present = append(present, load.ID)
	}
	var verified int
	if err = tx.QueryRow(ctx, `WITH locked AS MATERIALIZED (
 SELECT load_id,present FROM load_source_availability WHERE load_id=ANY($1::int[]) ORDER BY load_id FOR UPDATE
 ) SELECT count(*) FROM locked WHERE present=(load_id=ANY($2::int[]))`, ids, present).Scan(&verified); err != nil {
		return 0, err
	}
	if verified != len(ids) {
		return 0, fmt.Errorf("source availability changed during replacement; retry the complete lookup")
	}
	plan, err := buildLoadReconciliationPolicy(ctx, tx, "", ids, live, true)
	if err != nil {
		return 0, err
	}
	var entries []GrossBoardEntry
	groups := 0
	for _, g := range plan.Groups {
		if g.Hold == "" && len(g.After) > 0 {
			entries = append(entries, g.After...)
			groups++
		}
	}
	if groups == 0 {
		return 0, nil
	}
	board := NewGrossBoardRepository(r.pool)
	saved, err := board.saveEntriesTx(ctx, tx, entries)
	if err != nil {
		return 0, err
	}
	savedByKey := make(map[string]GrossBoardEntry, len(saved))
	for _, e := range saved {
		savedByKey[splitEntryKey(e)] = e
	}
	for _, g := range plan.Groups {
		if g.Hold != "" || len(g.After) == 0 {
			continue
		}
		e := g.Before[0]
		var amount string
		var count int
		err = tx.QueryRow(ctx, `SELECT coalesce(sum(driver_rate),0)::text,count(*) FROM `+grossBoardEntriesSQL+` WHERE driver_id=$1 AND service_date=$2::date AND NOT deleted AND day_status='' AND lower(btrim(load_number))=lower(btrim($3))`, e.DriverID, e.Date, g.Number).Scan(&amount, &count)
		if err != nil {
			return 0, err
		}
		a, _ := chargeCents(amount)
		b, _ := chargeCents(g.DriverGross)
		if a != b || count != 1 {
			return 0, fmt.Errorf("driver gross preservation failed for %s", g.Number)
		}
		for _, before := range g.Before {
			after, ok := savedByKey[splitEntryKey(before)]
			if !ok {
				return 0, fmt.Errorf("replacement did not save every source entry")
			}
			oldBody, err := json.Marshal(before)
			if err != nil {
				return 0, err
			}
			newBody, err := json.Marshal(after)
			if err != nil {
				return 0, err
			}
			// Reuse the immutable load/trip identity history for these unsplit
			// source-link transitions, with an explicit system actor.
			if _, err = tx.Exec(ctx, `INSERT INTO gross_board_trip_history(actor_id,before_values,after_values) VALUES('datatruck_replacement',$1::jsonb,$2::jsonb)`, string(oldBody), string(newBody)); err != nil {
				return 0, err
			}
		}
	}
	if err = commitPayrollMutation(ctx, tx); err != nil {
		return 0, err
	}
	return groups, nil
}
