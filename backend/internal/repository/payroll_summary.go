package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// PayrollExportPage uses prepared statements when current. Driver Pay can
// export during preparation by reading the same authoritative financial report
// in a fresh snapshot, including saved finalizations and current drafts.
func (r *DriverPayRepository) PayrollExportPage(ctx context.Context, kind string, week time.Time, query PayPageQuery) (DriverPayWeek, error) {
	if r.PreparedReadsEnabled() {
		report, err := r.PreparedPayroll(ctx, kind, week, query, false)
		if kind != "driver" || !errors.Is(err, ErrPayrollPreparing) {
			return report, err
		}
	}
	return r.PayrollPage(ctx, kind, week, query)
}

// Financial routing is shared with full reports. Cache the compact projection,
// then hydrate only the requested connected set in the SAME database snapshot.
// Pagination metadata and the whole-week revision always come from that summary.
func (r *DriverPayRepository) PayrollPage(ctx context.Context, kind string, week time.Time, query PayPageQuery) (DriverPayWeek, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return DriverPayWeek{}, err
	}
	defer tx.Rollback(ctx)
	ctx, snapshot, err := payrollReadSnapshotContext(ctx, tx)
	if err != nil {
		return DriverPayWeek{}, err
	}
	// Old/test schemas and future untracked dependencies retain the original
	// rendered-report revision contract until all inputs have durable revisions.
	compact := !strings.HasPrefix(snapshot, "snapshot:")
	summary, err := r.payrollSummary(ctx, tx, kind, week, compact)
	if err != nil {
		return summary, err
	}
	pageQuery := query
	pageQuery.Summary = true
	pageQuery.DetailIDs = nil
	page := PaginatePay(summary, pageQuery)
	ids := []string{}
	for _, d := range page.Drivers {
		if !query.Summary || query.DetailIDs[d.ID] {
			ids = append(ids, d.ID)
		}
	}
	if len(ids) > 0 {
		details, e := r.payrollDetails(ctx, tx, kind, week, ids)
		if e != nil {
			return page, e
		}
		byID := map[string]DriverPayDriver{}
		for _, d := range details.Drivers {
			byID[d.ID] = d
		}
		for i, d := range page.Drivers {
			if !query.Summary || query.DetailIDs[d.ID] {
				if full, ok := byID[d.ID]; ok {
					page.Drivers[i] = full
				} else {
					return page, ErrDriverPayConflict
				}
			}
		}
	}
	if err := decoratePayHomeWeeks(ctx, tx, kind, &page); err != nil {
		return page, err
	}
	if err := decoratePayDispatcherComparisons(ctx, tx, kind, &page); err != nil {
		return page, err
	}
	return page, tx.Commit(ctx)
}

type payrollSummaryKey struct{}

func payrollSummaryOnly(ctx context.Context) bool {
	v, _ := ctx.Value(payrollSummaryKey{}).(bool)
	return v
}

// Totals and routing still include every participant. Compact reads discard
// display-only load data and comments, omit transfer decoration and report JSON
// hashing, and produce no expanded statements. Finalization always recalculates
// full reports under the financial lock and compares this committed-input token.
func finishPayrollRead(ctx context.Context, tx pgx.Tx, kind string, report *DriverPayWeek) error {
	if err := applyPayrollReportExpenseChoices(ctx, tx, kind, report); err != nil {
		return err
	}
	if kind == "driver" {
		if err := readDriverScaleDeductions(ctx, tx, report); err != nil {
			return err
		}
		if err := markDriverPayMembership(ctx, tx, report); err != nil {
			return err
		}
		if err := overlayDriverExpenseBalances(ctx, tx, report); err != nil {
			return err
		}
	}
	for i := range report.Drivers {
		card := &report.Drivers[i]
		if card.Settlement == nil || !card.Settlement.Finalized {
			applyScaleDeductions(card)
		}
	}
	if err := applyCostStartDisplay(ctx, tx, report); err != nil {
		return err
	}
	if err := filterVacationPay(ctx, tx, kind, report); err != nil {
		return err
	}
	if err := decoratePayAssignments(ctx, tx, kind, report); err != nil {
		return err
	}
	if err := decorateBrokerReimbursements(ctx, tx, report); err != nil {
		return err
	}
	if payrollSummaryOnly(ctx) {
		for i, d := range report.Drivers {
			report.Drivers[i] = summarizePayStatement(d)
		}
	} else if err := decorateTransferLoads(ctx, tx, report); err != nil {
		return err
	}
	if !payrollSummaryOnly(ctx) {
		if err := decoratePayDayStatuses(ctx, tx, kind, report); err != nil {
			return err
		}
	}
	return setPayrollRevision(ctx, tx, kind, report)
}

func setPayrollRevision(ctx context.Context, tx pgx.Tx, kind string, report *DriverPayWeek) error {
	snapshot, err := payrollFinancialSnapshot(ctx, tx)
	if err != nil {
		return err
	}
	if strings.HasPrefix(snapshot, "snapshot:") {
		report.Revision = payrollRevision(*report)
		return nil
	}
	report.Revision = payrollInputRevision(kind, report.WeekStart, ChargeCurrentWeek(), snapshot)
	return nil
}

func payrollInputRevision(kind, week, calendar, snapshot string) string {
	// An open statement reviewed under an older calculator shape may
	// omit newly eligible charges even when its financial inputs are unchanged.
	sum := sha256.Sum256([]byte(kind + ":" + week + ":" + calendar + ":" + snapshot + ":" + strconv.Itoa(payrollProjectionSchemaVersion)))
	return "inputs:" + hex.EncodeToString(sum[:])
}
