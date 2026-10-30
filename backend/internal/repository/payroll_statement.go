package repository

import (
	"context"
	"github.com/jackc/pgx/v5"
	"strings"
	"time"
)

// PayrollStatement deliberately has no whole-week pagination/totals dependency.
func (r *DriverPayRepository) PayrollStatement(ctx context.Context, kind string, week time.Time, id string) (DriverPayWeek, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return DriverPayWeek{}, err
	}
	defer tx.Rollback(ctx)
	snapshot, err := payrollCacheSnapshot(ctx, tx)
	if err != nil {
		return DriverPayWeek{}, err
	}
	ctx = context.WithValue(ctx, payrollSnapshotKey{}, payrollSnapshot{tx, snapshot})
	result, err := r.payrollDetails(ctx, tx, kind, week, []string{id})
	if err != nil {
		return result, err
	}
	if len(result.Drivers) == 0 {
		return result, pgx.ErrNoRows
	}
	return result, tx.Commit(ctx)
}

// Missing rows share one calculation and snapshot; cache entries remain per ID.
func (r *DriverPayRepository) payrollDetails(ctx context.Context, tx pgx.Tx, kind string, week time.Time, ids []string) (DriverPayWeek, error) {
	snapshot, err := payrollCacheSnapshot(ctx, tx)
	if err != nil {
		return DriverPayWeek{}, err
	}
	missing := []string{}
	r.cache.mu.Lock()
	for _, id := range ids {
		entry, ok := r.cache.entries[payrollCacheKey("detail:"+kind+":"+id, week, snapshot)]
		if !ok || !time.Now().Before(entry.expires) {
			missing = append(missing, id)
		}
	}
	r.cache.mu.Unlock()
	var calculated *DriverPayWeek
	result := DriverPayWeek{WeekStart: week.Format(time.DateOnly), Drivers: []DriverPayDriver{}}
	for _, id := range ids {
		value, err := r.cachedWeek(ctx, tx, "detail:"+kind+":"+id, week, func() (DriverPayWeek, error) {
			if calculated == nil {
				// An entry can be evicted between the peek and use. Including all requested
				// roots is conservative and keeps the batch independent of cache pressure.
				roots := append(append([]string{}, missing...), ids...)
				selected := ctx
				if !strings.HasPrefix(snapshot, "snapshot:") {
					nodeKind := "driver"
					if kind == "investor" {
						nodeKind = "truck"
					}
					var e error
					selected, e = selectPayrollContexts(payrollWeekContext(ctx, week.Format(time.DateOnly)), tx, nodeKind, roots)
					if e != nil {
						return result, e
					}
				}
				var full DriverPayWeek
				var e error
				if kind == "investor" {
					full, e = readInvestorPay(selected, tx, week)
				} else {
					full, e = readDriverPayWeek(selected, tx, week, "")
				}
				if e != nil {
					return full, e
				}
				calculated = &full
			}
			row := *calculated
			row.Drivers = []DriverPayDriver{}
			row.SetupRequired = nil
			row.Pagination = nil
			for _, d := range calculated.Drivers {
				if d.ID == id {
					row.Drivers = append(row.Drivers, d)
				}
			}
			return row, nil
		})
		if err != nil {
			return result, err
		}
		result.Revision = value.Revision
		result.Issues = value.Issues
		result.Notices = value.Notices
		result.Drivers = append(result.Drivers, value.Drivers...)
	}
	return result, nil
}

type payrollReceiptKey struct{}

func (r *DriverPayRepository) PayrollPreview(ctx context.Context, kind string, week time.Time) (DriverPayWeek, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return DriverPayWeek{}, err
	}
	defer tx.Rollback(ctx)
	snapshot, err := payrollCacheSnapshot(ctx, tx)
	if err != nil {
		return DriverPayWeek{}, err
	}
	ctx = context.WithValue(ctx, payrollSnapshotKey{}, payrollSnapshot{tx, snapshot})
	result, err := r.payrollSummary(ctx, tx, kind, week, !strings.HasPrefix(snapshot, "snapshot:"))
	if err != nil {
		return result, err
	}
	return result, tx.Commit(ctx)
}

func (r *DriverPayRepository) payrollSummary(ctx context.Context, tx pgx.Tx, kind string, week time.Time, compact bool) (DriverPayWeek, error) {
	return r.cachedWeek(ctx, tx, "summary:"+kind, week, func() (DriverPayWeek, error) {
		var report DriverPayWeek
		var err error
		readCtx := context.WithValue(ctx, payrollSummaryKey{}, compact)
		if kind == "investor" {
			report, err = readInvestorPay(readCtx, tx, week)
		} else {
			report, err = readDriverPayWeek(readCtx, tx, week, "")
		}
		if err == nil {
			for i, d := range report.Drivers {
				report.Drivers[i] = summarizePayStatement(d)
			}
		}
		return report, err
	})
}

func setPaySaveRevision(ctx context.Context, tx pgx.Tx, kind string, edits *DriverPayEdits) error {
	if !payrollReceiptRequested(ctx) {
		return nil
	}
	value := DriverPayWeek{WeekStart: edits.WeekStart}
	if err := setPayrollRevision(ctx, tx, kind, &value); err != nil {
		return err
	}
	if strings.HasPrefix(value.Revision, "inputs:") {
		edits.InputRevision = value.Revision
	}
	return nil
}

func WithPayrollReceipt(ctx context.Context) context.Context {
	return context.WithValue(ctx, payrollReceiptKey{}, true)
}
func payrollReceiptRequested(ctx context.Context) bool {
	v, _ := ctx.Value(payrollReceiptKey{}).(bool)
	return v
}

func payrollReceipt(report DriverPayWeek, selected map[string]bool) DriverPayWeek {
	report.Receipt = true
	report.Pagination = nil
	report.SetupRequired = nil
	report.AffectedStatementIDs = []string{}
	rows := []DriverPayDriver{}
	for _, d := range report.Drivers {
		report.AffectedStatementIDs = append(report.AffectedStatementIDs, d.ID)
		if selected[d.ID] {
			rows = append(rows, d)
		}
	}
	report.Drivers = rows
	return report
}
