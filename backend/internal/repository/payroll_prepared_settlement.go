package repository

import (
	"context"
	"encoding/json"
	"slices"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
)

type payrollPostWriteResultKey struct{}
type payrollPostWriteReadKey struct{}

// Only use with a freshly fenced prepared statement, whose saved allocations
// are already authoritative. Unsaved zero deferrals still post.
func payrollExpensesNeedPosting(rows []ExpenseDeduction) bool {
	for _, row := range rows {
		if !row.Saved {
			return true
		}
	}
	return false
}

// A fenced statement with no fuel/toll basis, carry, deduction or override has
// nothing to post. Writing a zero ledger row would invalidate other periods
// merely to represent the same zero. Explicit deferrals/legacy overrides keep
// the normal canonical posting path.
func payrollDriverCostsNeedPosting(ctx context.Context, tx pgx.Tx, d DriverPayDriver) (bool, error) {
	if d.Edits.FuelOverride != nil || d.Edits.TollOverride != nil || d.Edits.Costs == nil {
		return true, nil
	}
	fuel, toll := driverCostBase(d)
	c := d.Edits.Costs
	if fuel != 0 || toll != 0 || c.FuelCarry != "0.00" || c.TollCarry != "0.00" || c.FuelDue != "0.00" || c.TollDue != "0.00" {
		return true, nil
	}
	// An old collection may need clearing after a source correction, even if
	// today's calculated basis is zero. Only omit a genuinely absent posting.
	var exists bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM driver_pay_cost_collections WHERE driver_id=$1 AND week_start=$2::date)`, d.ID, d.Edits.WeekStart).Scan(&exists)
	return exists, err
}

type payrollPostWriteResult struct {
	tx                   pgx.Tx
	kind, week, snapshot string
	selection            *payrollSelection
	body                 []byte
	read                 *payrollReadState
}

// Start an explicit read phase only after the command's final canonical write.
// Its inputs can accompany the result to publication if the same snapshot and
// exact component still apply. Never attach a pre-posting or pre-freeze memo.
func beginPayrollPostWriteRead(ctx context.Context, tx pgx.Tx) context.Context {
	ctx = context.WithValue(ctx, payrollReadKey{}, (*payrollReadState)(nil))
	ctx = payrollReadContext(ctx, tx)
	read := payrollReadMemo(ctx, tx)
	read.projectionSources = map[string][]byte{}
	read.projectionRoutes = map[string][]byte{}
	return context.WithValue(ctx, payrollPostWriteReadKey{}, read)
}

// A validated prepared report remains exact only when the posting phase wrote
// no captured input. Check actual transaction events, including indirect trigger
// writes, rather than inferring equivalence from displayed amounts. This check
// runs before freezing; ordinary first-time allocations still reconstruct.
func preparedPostingUnchanged(ctx context.Context, tx pgx.Tx, prepared bool) (bool, error) {
	if !prepared {
		return false, nil
	}
	var unchanged bool
	err := tx.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM payroll_projection_changes WHERE source_xid=pg_current_xact_id())`).Scan(&unchanged)
	return unchanged, err
}

// Only prepared settlement callers holding the source fences may retain a
// result here. All canonical posting and freezing is complete. Financial read
// memoization from before writes remains invalidated. Explicit post-write read
// phases require the same exact revision and component at publication.
func retainPayrollPostWriteResult(ctx context.Context, tx pgx.Tx, kind string, report DriverPayWeek) (context.Context, error) {
	snapshot, err := payrollCacheSnapshot(context.WithValue(ctx, payrollSnapshotKey{}, payrollSnapshot{}), tx)
	if err != nil {
		return ctx, err
	}
	body, err := json.Marshal(report)
	if err != nil {
		return ctx, err
	}
	selection, _ := ctx.Value(payrollSelectionKey{}).(*payrollSelection)
	read, _ := ctx.Value(payrollPostWriteReadKey{}).(*payrollReadState)
	return context.WithValue(ctx, payrollPostWriteResultKey{}, &payrollPostWriteResult{tx: tx, kind: kind, week: report.WeekStart, snapshot: snapshot, selection: selection, body: body, read: read}), nil
}

func payrollRetainedPostWrite(ctx context.Context, tx pgx.Tx, week time.Time) *payrollPostWriteResult {
	known, _ := ctx.Value(payrollPostWriteResultKey{}).(*payrollPostWriteResult)
	full, _ := ctx.Value(payrollAuthoritativeKey{}).(bool)
	if full || known == nil || known.tx != tx || known.week != week.Format(time.DateOnly) {
		return nil
	}
	snapshot, ok := ctx.Value(payrollSnapshotKey{}).(payrollSnapshot)
	if !ok || snapshot.tx != tx || snapshot.value != known.snapshot {
		return nil
	}
	selection, _ := ctx.Value(payrollSelectionKey{}).(*payrollSelection)
	if (selection == nil) != (known.selection == nil) {
		return nil
	}
	if selection != nil && (!slices.Equal(selection.drivers, known.selection.drivers) || !slices.Equal(selection.trucks, known.selection.trucks) || !slices.Equal(selection.owners, known.selection.owners)) {
		return nil
	}
	return known
}

func payrollPostWriteReport(ctx context.Context, tx pgx.Tx, kind string, week time.Time) (DriverPayWeek, bool, error) {
	var report DriverPayWeek
	known := payrollRetainedPostWrite(ctx, tx, week)
	if known == nil || known.kind != kind {
		return report, false, nil
	}
	err := json.Unmarshal(known.body, &report)
	return report, true, err
}

// The caller holds the financial lock and has validated the reviewed version.
// Fence every captured input before using its prepared financial values. A
// source that committed since the transaction snapshot causes serialization
// failure; no command is retried on a newer revision. Authoritative posting,
// collection-limit guards and post-write freezing remain in the settlement code.
func preparedSettlementReport(ctx context.Context, tx pgx.Tx, c *payrollCommand, kind string, week time.Time, id string) (DriverPayWeek, error) {
	var report DriverPayWeek
	if c == nil || c.beforeSnapshot == "" {
		return report, ErrDriverPayConflict
	}
	// Resource transactions already hold the shared source gate and exact
	// principal claims. Table-wide revision row locks add no protection there.
	if !payrollConcurrent(tx) {
		rows, err := tx.Query(ctx, `SELECT table_name,revision FROM payroll_cache_revisions WHERE NOT table_name=ANY($1::text[]) ORDER BY table_name FOR SHARE`, payrollUnrelatedTables)
		if err != nil {
			return report, err
		}
		for rows.Next() {
		}
		rows.Close()
		if err = rows.Err(); err != nil {
			return report, err
		}
	}
	var header, body []byte
	var snapshot string
	var currentResources bool
	err := tx.QueryRow(ctx, `SELECT CASE WHEN $2='driver' THEN g.driver_header ELSE g.investor_header END,g.input_snapshot,
 coalesce(rows.body,'[]'),coalesce(rows.current,true)
 FROM payroll_projection_weeks w JOIN payroll_projection_generations g ON g.id=w.generation_id
 LEFT JOIN LATERAL (SELECT jsonb_agg(v.detail) body,bool_and(payroll_resources_current(v.resource_dependencies)) current
 FROM payroll_statement_state s JOIN payroll_statement_versions v ON v.id=s.version_id
 WHERE s.week_start=w.week_start AND s.kind=$2 AND ($3='' OR s.statement_id=nullif($3,'')::uuid)) rows ON true
 WHERE w.week_start=$1`, week, kind, id).Scan(&header, &snapshot, &body, &currentResources)
	if err != nil {
		return report, err
	}
	if snapshot != c.beforeSnapshot || !currentResources {
		if id == "" || c.expected == "" {
			return report, ErrDriverPayConflict
		}
		valid, e := preparedPayrollStatementCurrent(ctx, tx, kind, id, week.Format(time.DateOnly), c.expected, c.beforeSnapshot)
		if e != nil {
			return report, e
		}
		if !valid {
			return report, ErrDriverPayConflict
		}
	}
	if err = json.Unmarshal(header, &report); err != nil {
		return report, err
	}
	if err = json.Unmarshal(body, &report.Drivers); err != nil {
		return report, err
	}
	// Match the authoritative allocator's order, independently of database collation.
	sort.SliceStable(report.Drivers, func(i, j int) bool {
		a, b := report.Drivers[i], report.Drivers[j]
		if a.FullName != b.FullName {
			return a.FullName < b.FullName
		}
		if kind == "driver" {
			return a.ID < b.ID
		}
		if a.InvestorID != b.InvestorID {
			return a.InvestorID < b.InvestorID
		}
		return a.TruckUnit < b.TruckUnit
	})
	return report, nil
}
