package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"github.com/jackc/pgx/v5"
	"sort"
	"strings"
	"time"
)

// Financial routing is shared with full reports. Cache the compact projection,
// then hydrate only the requested connected set in the SAME database snapshot.
// Pagination metadata and the whole-week revision always come from that summary.
func (r *DriverPayRepository) PayrollPage(ctx context.Context, kind string, week time.Time, query PayPageQuery) (DriverPayWeek, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return DriverPayWeek{}, err
	}
	defer tx.Rollback(ctx)
	snapshot, err := payrollCacheSnapshot(ctx, tx)
	if err != nil {
		return DriverPayWeek{}, err
	}
	// Old/test schemas and future untracked dependencies retain the original
	// rendered-report revision contract until all inputs have durable revisions.
	compact := !strings.HasPrefix(snapshot, "snapshot:")
	summary, err := r.cachedWeek(ctx, tx, "summary:"+kind, week, func() (DriverPayWeek, error) {
		var full DriverPayWeek
		var e error
		if kind == "investor" {
			full, e = readInvestorPay(context.WithValue(ctx, payrollSummaryKey{}, compact), tx, week)
		} else {
			full, e = readDriverPayWeek(context.WithValue(ctx, payrollSummaryKey{}, compact), tx, week, "")
		}
		if e != nil {
			return full, e
		}
		for i, d := range full.Drivers {
			full.Drivers[i] = summarizePayStatement(d)
		}
		return full, nil
	})
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
		sort.Strings(ids)
		details, e := r.cachedWeek(ctx, tx, "detail:"+kind+":"+strings.Join(ids, ","), week, func() (DriverPayWeek, error) {
			nodeKind := "driver"
			if kind == "investor" {
				nodeKind = "truck"
			}
			selected, e := selectPayrollContexts(payrollWeekContext(ctx, week.Format(time.DateOnly)), tx, nodeKind, ids)
			if e != nil {
				return DriverPayWeek{}, e
			}
			if kind == "investor" {
				return readInvestorPay(selected, tx, week)
			}
			return readDriverPayWeek(selected, tx, week, "")
		})
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
	if err := applyCostStartDisplay(ctx, tx, report); err != nil {
		return err
	}
	if err := filterVacationPay(ctx, tx, kind, report); err != nil {
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
	snapshot, err := payrollCacheSnapshot(ctx, tx)
	if err != nil {
		return err
	}
	if strings.HasPrefix(snapshot, "snapshot:") {
		report.Revision = payrollRevision(*report)
		return nil
	}
	sum := sha256.Sum256([]byte(kind + ":" + report.WeekStart + ":" + ChargeCurrentWeek() + ":" + snapshot))
	report.Revision = "inputs:" + hex.EncodeToString(sum[:])
	return nil
}
