package repository

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"strings"
	"testing"
	"time"
)

// Reuses the fresh/migrated split fixture, including a cross-week repetition.
func checkReviewedLoadSplit(t *testing.T, ctx context.Context, pool *pgxpool.Pool, repo *GrossBoardRepository) {
	t.Helper()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	workspace := func() LoadSplitWorkspace {
		t.Helper()
		w, err := repo.SplitWorkspace(ctx, 8101)
		if err != nil {
			t.Fatal(err)
		}
		return w
	}
	prepare := func(w LoadSplitWorkspace) GrossBoardEntry {
		w.Allocations = []SplitAllocation{{101, "500"}, {102, "1313"}}
		w.Reason = "Confirmed against weekly sheet"
		for i := range w.Entries {
			if *w.Entries[i].Entry.LoadTripID == 101 {
				w.Entries[i].Entry.DriverRate = "700"
			} else {
				w.Entries[i].Entry.DriverRate = "1313"
			}
		}
		e := w.Entries[0].Entry
		e.SplitReview = &w.LoadSplitReview
		return e
	}
	w := workspace()
	e := prepare(w)
	// Mismatch is specifically permitted between provider amounts and reviewed
	// original gross; the original allocations themselves must balance exactly.
	e.SplitReview.Allocations[0].OriginalRate = "700"
	if _, err := repo.SaveEntries(ctx, []GrossBoardEntry{e}, "review-test"); err == nil {
		t.Fatal("unbalanced revenue accepted")
	}
	e = prepare(workspace())
	e.SplitReview.SourceHash = "stale"
	if _, err := repo.SaveEntries(ctx, []GrossBoardEntry{e}); err == nil {
		t.Fatal("stale source accepted")
	}
	e = prepare(workspace())
	e.SplitReview.Entries[1].Entry.Version++
	if _, err := repo.SaveEntries(ctx, []GrossBoardEntry{e}); err == nil {
		t.Fatal("stale companion accepted")
	}
	// Editing the first driver's split cannot change a finalized other driver.
	e = prepare(workspace())
	other := ""
	for _, item := range e.SplitReview.Entries {
		if item.Entry.DriverID != e.DriverID {
			other = item.Entry.DriverID
			break
		}
	}
	if other == "" {
		t.Fatal("missing companion driver")
	}
	exec(`INSERT INTO payroll_settlements(driver_id,week_start,report) VALUES($1,'2026-10-19','{}')`, other)
	if _, err := repo.SaveEntries(ctx, []GrossBoardEntry{e}); err == nil {
		t.Fatal("finalized companion changed")
	}
	exec(`DELETE FROM payroll_settlements WHERE driver_id=$1`, other)
	// The same protection applies to a frozen investor snapshot on the other side.
	exec(`INSERT INTO investors(id,full_name) VALUES('83000000-0000-0000-0000-000000000001','Split Investor')`)
	var otherEntry GrossBoardEntry
	for _, item := range e.SplitReview.Entries {
		if item.Entry.DriverID == other {
			otherEntry = item.Entry
			break
		}
	}
	exec(`INSERT INTO investor_pay_weeks(truck_id,week_start,owner_id,edits,finalized,report)
 VALUES('82000000-0000-0000-0000-000000000002','2026-10-19','83000000-0000-0000-0000-000000000001','{}',true,
 jsonb_build_object('loads',jsonb_build_array(jsonb_build_object('sourceDriverId',$1::text,'sourceDate',$2::text,'sourceSlot',$3::integer))))`, other, otherEntry.Date, otherEntry.Slot)
	if _, err := repo.SaveEntries(ctx, []GrossBoardEntry{e}); err == nil {
		t.Fatal("finalized investor companion changed")
	}
	exec(`DELETE FROM investor_pay_weeks WHERE owner_id='83000000-0000-0000-0000-000000000001'`)
	exec(`INSERT INTO payroll_settlements(driver_id,week_start,report) VALUES($1,'2026-11-02','{"loads":[{"loadRecordId":8101}]}')`, other)
	if _, err := repo.SaveEntries(ctx, []GrossBoardEntry{e}); err == nil {
		t.Fatal("frozen load recognized in another week changed")
	}
	exec(`DELETE FROM payroll_settlements WHERE driver_id=$1`, other)
	var audits int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM load_split_history`).Scan(&audits); err != nil || audits != 0 {
		t.Fatal("failed review left a receipt", err, audits)
	}
	e = prepare(workspace())
	saved, err := repo.SaveEntries(ctx, []GrossBoardEntry{e}, "review-test")
	if err != nil {
		t.Fatal("review failed", err)
	}
	if len(saved) != len(w.Entries) {
		t.Fatal("all linked entries, including repeat, must save", len(saved))
	}
	for _, row := range saved {
		if row.TripReview || row.SplitReview != nil {
			t.Fatal("review not cleared", row)
		}
		wantOriginal, wantDriver := "500.00", "700.00"
		if *row.LoadTripID == 102 {
			wantOriginal, wantDriver = "1313.00", "1313.00"
		}
		if row.OriginalRate != wantOriginal || row.DriverRate != wantDriver {
			t.Fatal("independent amounts lost", row)
		}
	}
	if _, err = repo.SaveEntries(ctx, []GrossBoardEntry{e}); err == nil {
		t.Fatal("stale review replay accepted")
	}
	var source string
	if err = pool.QueryRow(ctx, `SELECT raw_payload->'trips'->0->>'total_load_pay' FROM loads WHERE id=8101`).Scan(&source); err != nil || source != "700" {
		t.Fatal("provider data overwritten", source, err)
	}
	matches, err := repo.SearchLoads(ctx, "SPLIT-1")
	if err != nil || len(matches) != 1 || matches[0].TripReviewReason != "" {
		t.Fatal("load search lost reviewed allocation", matches, err)
	}
	var total string
	if err = pool.QueryRow(ctx, companyDashboardCTE+` SELECT sum(gross)::text FROM dated WHERE inclusion='included'`, "2026-10-19", "2026-10-25").Scan(&total); err != nil || total != "1813.00" {
		t.Fatal("dashboard revenue", total, err)
	}
	week, _ := time.Parse(time.DateOnly, "2026-10-19")
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	pay, err := readDriverPaySourceWeek(ctx, tx, week, "")
	tx.Rollback(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range pay.Drivers {
		for _, l := range d.Loads {
			if *l.LoadTripID == 101 && (l.OriginalRate != "500.00" || l.DriverGross != "700.00" || l.TripReview) {
				t.Fatal("payroll amounts", l)
			}
		}
	}
	// Ordinary autosave cannot unbalance a reviewed allocation.
	changed := saved[0]
	changed.OriginalRate = "501"
	changed.EnteredOriginalRate = "501"
	if _, err = repo.SaveEntries(ctx, []GrossBoardEntry{changed}); err == nil {
		t.Fatal("ordinary edit bypassed whole-load allocation")
	}
	// Routine sync timestamps do not reopen review. Meaningful source changes do,
	// without deleting the saved decision or rewriting entered amounts.
	exec(`UPDATE loads SET synced_at=now() WHERE id=8101`)
	board, err := repo.Get(ctx, week)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range board.Entries {
		if row.TripReview {
			t.Fatal("timestamp reopened review")
		}
	}
	exec(`UPDATE loads SET raw_payload=jsonb_set(raw_payload,'{trips,0,total_load_pay}','701') WHERE id=8101`)
	board, err = repo.Get(ctx, week)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range board.Entries {
		if !row.TripReview || !strings.Contains(row.TripReviewReason, "saved allocation is retained") || row.EnteredOriginalRate == "" {
			t.Fatal("source change lost review evidence", row)
		}
	}
	w = workspace()
	if w.Version != 1 || w.Allocations[0].OriginalRate != "500.00" {
		t.Fatal("saved allocation overwritten", w)
	}
	// Explicit re-review keeps the same $500 revenue despite the new source amount.
	if _, err = repo.SaveEntries(ctx, []GrossBoardEntry{prepare(w)}, "review-test"); err != nil {
		t.Fatal("re-review", err)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM load_split_history WHERE actor_id='review-test'`).Scan(&audits); err != nil || audits != 2 {
		t.Fatal("audit history", audits, err)
	}
}
