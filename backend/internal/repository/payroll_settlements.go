package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type PayrollSettlement struct {
	Finalized   bool       `json:"finalized"`
	Override    bool       `json:"override,omitempty"`
	Version     int        `json:"version"`
	FinalizedAt time.Time  `json:"finalizedAt"`
	FinalizedBy string     `json:"finalizedBy"`
	ReopenedAt  *time.Time `json:"reopenedAt"`
	Reason      string     `json:"reason"`
}

type PayrollSettlementInput struct {
	ExpenseRevision           string `json:"expenseRevision,omitempty"`
	RequestID                 string `json:"requestId,omitempty"`
	ExpectedStatementRevision string `json:"expectedStatementRevision,omitempty"`
	WeekStart                 string `json:"weekStart"`
	DriverID                  string `json:"driverId"`
	Revision                  string `json:"revision"`
	Reason                    string `json:"reason"`
	Override                  bool   `json:"override"`
}

// Only these review checks can be bypassed by an explicit finalization override.
// Conflicts, permissions, collection limits and other errors remain ordinary errors.
type PayrollFinalizationBlockedError struct {
	error
	Review *DriverPayWeek
}

func (e *PayrollFinalizationBlockedError) Unwrap() error { return e.error }

func finalizationBlocked(format string, args ...any) error {
	return &PayrollFinalizationBlockedError{error: chargeInvalid(format, args...)}
}

func finalizationReview(report DriverPayWeek, id, format string, args ...any) error {
	if id != "" {
		rows := []DriverPayDriver{}
		for _, d := range report.Drivers {
			if d.ID == id {
				rows = append(rows, d)
			}
		}
		report.Drivers = rows
	}
	report.Pagination = nil
	report.SetupRequired = nil
	return &PayrollFinalizationBlockedError{error: chargeInvalid(format, args...), Review: &report}
}

func payrollRevision(report DriverPayWeek) string {
	report.Revision = ""
	b, _ := json.Marshal(report)
	hash := sha256.Sum256(b)
	return hex.EncodeToString(hash[:])
}
func overlayPayrollSettlements(ctx context.Context, tx pgx.Tx, report DriverPayWeek, driver string) (DriverPayWeek, error) {
	rows, err := tx.Query(ctx, `SELECT s.driver_id,s.finalized,s.version,s.finalized_at,coalesce(u.username,''),s.reopened_at,s.reason,s.report
 FROM payroll_settlements s LEFT JOIN app_users u ON u.id=s.finalized_by WHERE s.week_start=$1::date AND ($2='' OR s.driver_id::text=$2) AND ($3::uuid[] IS NULL OR s.driver_id=ANY($3::uuid[]))`, report.WeekStart, driver, payrollDriverIDs(ctx))
	if err != nil {
		return report, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var meta PayrollSettlement
		var body []byte
		if err = rows.Scan(&id, &meta.Finalized, &meta.Version, &meta.FinalizedAt, &meta.FinalizedBy, &meta.ReopenedAt, &meta.Reason, &body); err != nil {
			return report, err
		}
		var frozen DriverPayDriver
		if err = json.Unmarshal(body, &frozen); err != nil {
			return report, err
		}
		if frozen.Settlement != nil {
			meta.Override = frozen.Settlement.Override
		}
		found := false
		for i := range report.Drivers {
			if report.Drivers[i].ID == id {
				found = true
				if meta.Finalized {
					report.Drivers[i] = frozen
				}
				report.Drivers[i].Settlement = &meta
				break
			}
		}
		if !found && meta.Finalized {
			frozen.Settlement = &meta
			report.Drivers = append(report.Drivers, frozen)
		}
	}
	if err = rows.Err(); err != nil {
		return report, err
	}
	rows.Close()
	if err = populateExpenseReimbursementDates(ctx, tx, report.Drivers); err != nil {
		return report, err
	}
	sort.Slice(report.Drivers, func(i, j int) bool {
		a, b := report.Drivers[i], report.Drivers[j]
		if a.FullName == b.FullName {
			return a.ID < b.ID
		}
		return a.FullName < b.FullName
	})
	return report, nil
}
func assertPayrollOpen(ctx context.Context, tx pgx.Tx, driver, week string) error {
	var closed bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM payroll_settlements WHERE driver_id=$1 AND week_start=$2::date AND finalized)`, driver, week).Scan(&closed); err != nil {
		return err
	}
	if closed {
		return chargeInvalid("Reopen this driver settlement before editing payroll")
	}
	return nil
}

// The exclusive session lock is acquired before opening the snapshot transaction.
// Ordinary charge writes take its shared counterpart, retaining per-driver concurrency.
func (r *DriverPayRepository) Settle(ctx context.Context, week time.Time, driver, revision, actor, reason string, reopen, override bool, commands ...PayrollSettlementCommand) (DriverPayWeek, error) {
	empty := DriverPayWeek{}
	override = override && !reopen
	if !reopen && !override && week.Format(time.DateOnly) > ChargeCurrentWeek() {
		return empty, finalizationBlocked("Future weeks cannot be finalized")
	}
	// Legacy clients may still send a reason; it is no longer collected or saved.
	reason = ""
	if override {
		reason = "Finalized with override"
	}
	root := ""
	if driver != "" {
		root = "driver:" + driver
	}
	var request, expected string
	if len(commands) > 0 {
		request, expected = commands[0].RequestID, commands[0].ExpectedStatementRevision
	}
	tx, closeWrite, err := r.beginPreparedPayrollWrite(ctx, actor, request, expected, root)
	if err != nil {
		return empty, err
	}
	defer closeWrite()
	command, replay, err := beginPayrollSettlement(ctx, tx, "driver", week, driver, revision, actor, reason, reopen, override, commands)
	if err != nil {
		return empty, err
	}
	if replay != nil {
		var result DriverPayWeek
		err = json.Unmarshal(replay, &result)
		return result, err
	}
	prepared := r.preparedFinalization && command != nil && command.expected != ""
	if driver != "" && strings.HasPrefix(revision, "inputs:") && !prepared {
		current := DriverPayWeek{WeekStart: week.Format(time.DateOnly)}
		if err = setPayrollRevision(ctx, tx, "driver", &current); err != nil {
			return empty, err
		}
		if current.Revision != revision {
			return empty, ErrDriverPayConflict
		}
		ctx, err = selectPayrollContext(payrollWeekContext(ctx, current.WeekStart), tx, "driver", driver)
		if err != nil {
			return empty, err
		}
	}
	var report DriverPayWeek
	if prepared {
		if driver != "" {
			ctx, err = selectPayrollContext(payrollWeekContext(ctx, week.Format(time.DateOnly)), tx, "driver", driver)
		}
		if err != nil {
			return empty, err
		}
		report, err = preparedSettlementReport(ctx, tx, command, "driver", week, driver)
		// Settlement writes change canonical ledgers, never the source-only facts.
		// Do not pin the financial snapshot: subsequent reads must observe postings.
		ctx = payrollRoutingFactsContext(ctx, command.beforeSnapshot)
		ctx = context.WithValue(ctx, payrollNormalizedFactsKey{}, true)
	} else if reopen && driver != "" && strings.HasPrefix(revision, "inputs:") {
		report, err = readPayrollReopenTarget(ctx, tx, "driver", week, driver, revision)
	} else {
		report, err = readDriverPayWeek(ctx, tx, week, "")
	}
	if err != nil {
		return empty, err
	}
	if !prepared && (revision == "" || revision != report.Revision) {
		return empty, ErrDriverPayConflict
	}
	if prepared && !reopen {
		expenseRevision := ""
		if len(commands) > 0 {
			expenseRevision = commands[0].ExpenseRevision
		}
		if driver != "" && expenseRevision != "" {
			if len(report.Drivers) != 1 || report.Drivers[0].ExpenseRevision != expenseRevision {
				return empty, ErrDriverPayConflict
			}
		} else if driver == "" || len(report.Drivers) != 1 || command.reviewedExpenseRevision == "" || command.reviewedExpenseRevision != report.Drivers[0].ExpenseRevision {
			// Older clients use their complete input revision as the review
			// fence when the live balance differs from their prepared version.
			// The statement revision itself suffices if that balance is unchanged.
			current := DriverPayWeek{WeekStart: report.WeekStart}
			if err = setPayrollRevision(ctx, tx, "driver", &current); err != nil {
				return empty, err
			}
			if current.Revision != revision {
				return empty, ErrDriverPayConflict
			}
		}
	}
	bulkExpenses := driver == "" && !reopen
	if bulkExpenses {
		ctx, err = preparePayrollFinalization(ctx, tx, "driver", report)
		if err != nil {
			return empty, err
		}
		if err = finalizePayrollExpenses(ctx, tx, "driver", report.WeekStart, actor, report.Drivers); err != nil {
			return empty, err
		}
	}
	selected := 0
	selectedIDs := map[string]bool{}
	bulkReopen := []string{}
	var frozenResult *DriverPayWeek
	pending := map[string][]string{}
	var chargeInputs *ChargeData
	var chargeLoads map[string]map[string]bool
	for _, d := range report.Drivers {
		if driver != "" && d.ID != driver {
			continue
		}
		closed := d.Settlement != nil && d.Settlement.Finalized
		if closed != reopen {
			continue
		}
		if reopen {
			if err = guardDriverSalaryCarryOrder(ctx, tx, d, report.WeekStart); err != nil {
				return empty, err
			}
		}
		selected++
		selectedIDs[d.ID] = true
		if prepared && reopen && driver == "" {
			bulkReopen = append(bulkReopen, d.ID)
			continue
		}
		if reopen {
			var ids []string
			if err = tx.QueryRow(ctx, `UPDATE payroll_settlements SET finalized=false,version=version+1,reopened_at=now(),reopened_by=nullif($3,'')::uuid,reason=$4 WHERE driver_id=$1 AND week_start=$2::date RETURNING confirmed_schedules`, d.ID, report.WeekStart, actor, strings.TrimSpace(reason)).Scan(&ids); err != nil {
				return empty, err
			}
			for _, id := range ids {
				if _, err = tx.Exec(ctx, `UPDATE driver_charge_occurrences SET confirmed_at=NULL,confirmed_by=NULL,version=version+1 WHERE schedule_id=$1 AND week_start=$2::date`, id, report.WeekStart); err != nil {
					return empty, err
				}
				if _, err = tx.Exec(ctx, `UPDATE driver_charge_schedules SET version=version+1 WHERE id=$1`, id); err != nil {
					return empty, err
				}
			}
		} else {
			if err = savePaySources(ctx, tx, d, &d.Edits, "driver", actor); err != nil {
				return empty, err
			}
			// Both the fenced prepared path and the authoritative locked read
			// omit absent zero postings, keeping their canonical ledgers equal.
			postCosts, err := payrollDriverCostsNeedPosting(ctx, tx, d)
			if err != nil {
				return empty, err
			}
			if postCosts {
				if err = saveDriverPayCosts(ctx, tx, d, d.Edits, actor); err != nil {
					return empty, err
				}
			}
			if !override && len(d.Issues) > 0 {
				return empty, finalizationReview(report, driver, "Resolve settlement issues for %s: %s", d.FullName, strings.Join(d.Issues, "; "))
			}
			for _, load := range d.Loads {
				if !override && (load.Fee == "" || len(load.Issues) > 0) {
					return empty, finalizationReview(report, driver, "Resolve the loads needing review for %s before finalizing", d.FullName)
				}
			}
			deductions := append([]ExpenseDeduction(nil), d.Edits.ExpenseDeductions...)
			for i := range deductions {
				deductions[i].Apply = !deductions[i].Saved
			}
			if !bulkExpenses && (!prepared || payrollExpensesNeedPosting(deductions)) {
				if err = finalizePayrollExpenses(ctx, tx, "driver", report.WeekStart, actor, []DriverPayDriver{d}); err != nil {
					return empty, err
				}
			}
			confirmed := []string{}
			for _, row := range d.Edits.GeneratedCharges {
				scheduled, _ := chargeCents(row.ScheduledAmount)
				if err = validatePaySourceAmount(row.Name, row.Amount, scheduled); err != nil {
					return empty, err
				}
				if row.ConfirmedAt != nil {
					continue
				}
				if chargeInputs == nil {
					data, loads, e := chargeData(ctx, tx, "")
					if e != nil {
						return empty, e
					}
					chargeInputs, chargeLoads = &data, loads
				}
				for i := range chargeInputs.Schedules {
					if chargeInputs.Schedules[i].ID == row.ScheduleID {
						if err = freezeChargesBefore(ctx, tx, &chargeInputs.Schedules[i], report.WeekStart, chargeLoads[chargeInputs.Schedules[i].DriverID]); err != nil {
							return empty, err
						}
					}
				}
				row.Overridden = true // Pin every finalized deduction against later source changes.
				if row.Kind == "installment" && strings.HasPrefix(row.Amount, "-") {
					now := time.Now()
					row.ConfirmedAt = &now
					row.ConfirmedBy = &actor
					confirmed = append(confirmed, row.ScheduleID)
				}
				if err = storeOccurrence(ctx, tx, row); err != nil {
					return empty, err
				}
				if _, err = tx.Exec(ctx, `UPDATE driver_charge_schedules SET version=version+1 WHERE id=$1`, row.ScheduleID); err != nil {
					return empty, err
				}
			}
			pending[d.ID] = confirmed
		}
	}
	if selected == 0 {
		return empty, chargeInvalid("No eligible driver settlements selected")
	}
	if len(bulkReopen) > 0 {
		if err = reopenPreparedDriverWeek(ctx, tx, report.WeekStart, actor, reason, bulkReopen); err != nil {
			return empty, err
		}
	}
	if reopen {
		if err = reversePayrollExpenses(ctx, tx, "driver", report.WeekStart, payrollReceipt(report, selectedIDs).Drivers); err != nil {
			return empty, err
		}
		if prepared && payrollConcurrent(tx) {
			// Reversing an ordinary driver expense changes the live balance, just
			// like posting it. Certify those exact events before impact discovery;
			// settlement and all non-expense events retain their normal routing.
			if _, err = preparedPostingUnchanged(ctx, tx, true); err != nil {
				return empty, err
			}
		}
	}
	// All selected financial writes are complete under the exclusive payroll
	// lock. Calculate the resulting fleet once, then freeze those exact rows.
	if len(pending) > 0 {
		unchanged, e := preparedPostingUnchanged(ctx, tx, prepared)
		if e != nil {
			return empty, e
		}
		updated := report
		if !unchanged {
			updated, e = readDriverPayWeek(ctx, tx, week, "")
		} else {
			e = overlayDriverExpenseBalances(ctx, tx, &updated)
		}
		if e != nil {
			return empty, e
		}
		freezeBatch := &pgx.Batch{}
		for _, d := range updated.Drivers {
			confirmed, ok := pending[d.ID]
			if !ok {
				continue
			}
			if err = guardDriverSalaryCarryOrder(ctx, tx, d, report.WeekStart); err != nil {
				return empty, err
			}
			frozen := d
			frozen.Settlement = nil
			if override {
				frozen.Settlement = &PayrollSettlement{Finalized: true, Override: true, Reason: reason}
			}
			body, e := json.Marshal(frozen)
			if e != nil {
				return empty, e
			}
			freezeBatch.Queue(`INSERT INTO payroll_settlements(driver_id,week_start,report,confirmed_schedules,finalized_by,reason) VALUES($1,$2::date,$3,$4,nullif($5,'')::uuid,$6)
   ON CONFLICT(driver_id,week_start) DO UPDATE SET version=payroll_settlements.version+1,finalized=true,report=excluded.report,confirmed_schedules=excluded.confirmed_schedules,finalized_by=excluded.finalized_by,finalized_at=now(),reopened_at=NULL,reopened_by=NULL,reason=excluded.reason`, d.ID, report.WeekStart, body, confirmed, actor, reason)
			delete(pending, d.ID)
		}
		if len(pending) != 0 {
			return empty, ErrDriverPayConflict
		}
		if err = tx.SendBatch(ctx, freezeBatch).Close(); err != nil {
			return empty, err
		}
		frozenResult = &updated
	}
	if payrollReceiptRequested(ctx) && frozenResult != nil && strings.HasPrefix(report.Revision, "inputs:") {
		result, err := overlaySelectedPayrollSettlements(ctx, tx, *frozenResult, selectedIDs)
		if err != nil {
			return empty, err
		}
		// Freezing only adds settlement metadata to these already recalculated rows.
		if err = setPayrollRevision(ctx, tx, "driver", &result); err != nil {
			return empty, err
		}
		if r.preparedFinalization && command != nil && command.expected != "" {
			ctx, err = retainPayrollPostWriteResult(ctx, tx, "driver", result)
			if err != nil {
				return empty, err
			}
		}
		result = payrollReceipt(result, selectedIDs)
		return r.commitPayrollSettlement(ctx, tx, command, "driver", result, selectedIDs)
	}
	if payrollReceiptRequested(ctx) && driver != "" && strings.HasPrefix(report.Revision, "inputs:") {
		ctx, err = selectPayrollContext(payrollWeekContext(ctx, week.Format(time.DateOnly)), tx, "driver", driver)
		if err != nil {
			return empty, err
		}
	}
	if prepared {
		ctx = beginPayrollPostWriteRead(ctx, tx)
	}
	result, err := readDriverPayWeek(ctx, tx, week, "")
	if err != nil {
		return empty, err
	}
	if r.preparedFinalization && command != nil && command.expected != "" {
		ctx, err = retainPayrollPostWriteResult(ctx, tx, "driver", result)
		if err != nil {
			return empty, err
		}
	}
	if payrollReceiptRequested(ctx) {
		result = payrollReceipt(result, selectedIDs)
	}
	return r.commitPayrollSettlement(ctx, tx, command, "driver", result, selectedIDs)
}

// Rows outside this command have already received their current display
// decorations. Replacing those rows with older frozen JSON would discard live
// broker reminders and other display-only updates on an existing settlement.
func overlaySelectedPayrollSettlements(ctx context.Context, tx pgx.Tx, report DriverPayWeek, selected map[string]bool) (DriverPayWeek, error) {
	overlaid, err := overlayPayrollSettlements(ctx, tx, payrollReceipt(report, selected), "")
	if err != nil {
		return report, err
	}
	byID := make(map[string]DriverPayDriver, len(overlaid.Drivers))
	for _, row := range overlaid.Drivers {
		byID[row.ID] = row
	}
	for i, row := range report.Drivers {
		if selected[row.ID] {
			committed, ok := byID[row.ID]
			if !ok || committed.Settlement == nil || !committed.Settlement.Finalized {
				return report, ErrDriverPayConflict
			}
			report.Drivers[i] = committed
		}
	}
	return report, nil
}
