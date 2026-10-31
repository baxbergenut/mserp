package repository

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"mserp/internal/db"
	"strings"
	"time"
)

func (r *DriverPayRepository) SaveInvestorPay(ctx context.Context, edits DriverPayEdits, actor string) (DriverPayEdits, error) {
	edits.InputRevision = ""
	tx, closeWrite, err := r.beginPayrollWrite(ctx)
	if err != nil {
		return edits, err
	}
	defer closeWrite()
	endLock := db.Phase(ctx, "financial_lock_wait")
	err = lockTruckAccounting(ctx, tx)
	endLock()
	if err != nil {
		return edits, err
	}
	week, err := chargeWeek(edits.WeekStart)
	if err != nil {
		return edits, err
	}
	ctx, err = selectPayrollContext(payrollWeekContext(ctx, edits.WeekStart), tx, "truck", edits.DriverID)
	if err != nil {
		return edits, err
	}
	endValidation := db.Phase(ctx, "save_calculation")
	report, err := readInvestorPay(ctx, tx, week)
	endValidation()
	if err != nil {
		return edits, err
	}
	var card *DriverPayDriver
	for i := range report.Drivers {
		if report.Drivers[i].ID == edits.DriverID {
			card = &report.Drivers[i]
		}
	}
	if card == nil {
		return edits, chargeInvalid("This truck has no investor settlement for this week")
	}
	if card.Settlement != nil && card.Settlement.Finalized {
		return edits, chargeInvalid("Reopen this investor settlement before editing")
	}
	if edits.Version != card.Edits.Version {
		return edits, ErrDriverPayConflict
	}
	if len(edits.GeneratedCharges) > 0 {
		return edits, chargeInvalid("Manage recurring truck fees on Charges")
	}
	defer db.Phase(ctx, "writes_and_commit")()
	if err = savePaySources(ctx, tx, *card, &edits, "investor", actor); err != nil {
		return edits, err
	}
	if err = validateInvestorCostEdits(*card, edits); err != nil {
		return edits, err
	}
	if err = saveInvestorExpenses(ctx, tx, *card, actor, edits.ExpenseDeductions); err != nil {
		return edits, err
	}
	edits.ExpenseDeductions = nil
	body, err := json.Marshal(edits)
	if err != nil {
		return edits, err
	}
	var version int
	if edits.Version == 0 {
		err = tx.QueryRow(ctx, `INSERT INTO investor_pay_weeks(truck_id,week_start,owner_id,edits) VALUES($1,$2::date,$3,$4) ON CONFLICT DO NOTHING RETURNING version`, card.ID, edits.WeekStart, card.InvestorID, body).Scan(&version)
	} else {
		err = tx.QueryRow(ctx, `UPDATE investor_pay_weeks SET edits=$3,version=version+1,owner_id=$5 WHERE truck_id=$1 AND week_start=$2::date AND version=$4 AND NOT finalized RETURNING version`, card.ID, edits.WeekStart, body, edits.Version, card.InvestorID).Scan(&version)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return edits, ErrDriverPayConflict
	}
	if err != nil {
		return edits, err
	}
	edits.Version = version
	edits.ExpenseDeductions, err = investorExpenseDeductions(ctx, tx, card.ID, card.InvestorID, edits.WeekStart, false)
	if err != nil {
		return edits, err
	}
	if err = truckAudit(ctx, tx, card.ID, edits.WeekStart, "pay_edits", actor, edits); err != nil {
		return edits, err
	}
	if err = setPaySaveRevision(ctx, tx, "investor", &edits); err != nil {
		return edits, err
	}
	return edits, tx.Commit(ctx)
}
func (r *DriverPayRepository) SettleInvestor(ctx context.Context, week time.Time, truck, revision, actor, reason string, reopen, override bool) (DriverPayWeek, error) {
	empty := DriverPayWeek{}
	override = override && !reopen
	if !reopen && !override && week.Format(time.DateOnly) > ChargeCurrentWeek() {
		return empty, finalizationBlocked("Future weeks cannot be finalized")
	}
	if reopen && (strings.TrimSpace(reason) == "" || len([]rune(reason)) > 2000) {
		return empty, chargeInvalid("Provide a reason for reopening, up to 2,000 characters")
	}
	if override {
		reason = "Finalized with override"
	}
	conn, err := r.pool.Acquire(ctx)
	if err != nil {
		return empty, err
	}
	defer conn.Release()
	endLock := db.Phase(ctx, "financial_lock_wait")
	_, err = conn.Exec(ctx, `SELECT pg_advisory_lock(736281940)`)
	endLock()
	if err != nil {
		return empty, err
	}
	defer func() {
		unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, e := conn.Exec(unlockCtx, `SELECT pg_advisory_unlock(736281940)`); e != nil {
			_ = conn.Conn().Close(context.Background())
		}
	}()
	tx, err := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return empty, err
	}
	defer tx.Rollback(ctx)
	if err = lockTruckAccounting(ctx, tx); err != nil {
		return empty, err
	}
	if truck != "" && strings.HasPrefix(revision, "inputs:") {
		current := DriverPayWeek{WeekStart: week.Format(time.DateOnly)}
		if err = setPayrollRevision(ctx, tx, "investor", &current); err != nil {
			return empty, err
		}
		if current.Revision != revision {
			return empty, ErrDriverPayConflict
		}
		ctx, err = selectPayrollContext(payrollWeekContext(ctx, current.WeekStart), tx, "truck", truck)
		if err != nil {
			return empty, err
		}
	}
	var report DriverPayWeek
	if reopen && truck != "" && strings.HasPrefix(revision, "inputs:") {
		report, err = readPayrollReopenTarget(ctx, tx, "investor", week, truck, revision)
	} else {
		report, err = readInvestorPay(ctx, tx, week)
	}
	if err != nil {
		return empty, err
	}
	if revision == "" || revision != report.Revision {
		return empty, ErrDriverPayConflict
	}
	if !reopen && !override && len(report.Issues) > 0 {
		return empty, finalizationReview(report, truck, "Resolve unattributed loads before finalizing Investor Pay")
	}
	bulkExpenses := truck == "" && !reopen
	if bulkExpenses {
		ctx, err = preparePayrollFinalization(ctx, tx, "investor", report)
		if err != nil {
			return empty, err
		}
		if err = finalizePayrollExpenses(ctx, tx, "investor", report.WeekStart, actor, report.Drivers); err != nil {
			return empty, err
		}
	}
	var expenseInputs truckPayInputs
	if bulkExpenses {
		expenseInputs, err = readTruckPayExpenses(ctx, tx, report.WeekStart, truckPayInputs{})
		if err != nil {
			return empty, err
		}
	}
	writeBatch := &pgx.Batch{}
	count := 0
	selectedIDs := map[string]bool{}
	committed := DriverPayWeek{WeekStart: report.WeekStart, Issues: report.Issues, Notices: report.Notices, Drivers: []DriverPayDriver{}}
	var username string
	var finalizedAt time.Time
	if !reopen {
		if err = tx.QueryRow(ctx, `SELECT username,now() FROM app_users WHERE id=$1`, actor).Scan(&username, &finalizedAt); err != nil {
			return empty, err
		}
	}
	queueFinal := func(d DriverPayDriver) (DriverPayDriver, error) {
		d.Settlement = &PayrollSettlement{Finalized: true, Override: override, Reason: reason, Version: d.Edits.Version + 1, FinalizedAt: finalizedAt, FinalizedBy: username}
		d.Edits.Version++
		body, e := json.Marshal(d)
		if e != nil {
			return d, e
		}
		edits, e := json.Marshal(d.Edits)
		if e != nil {
			return d, e
		}
		writeBatch.Queue(`INSERT INTO investor_pay_weeks(truck_id,week_start,owner_id,edits,report,finalized,finalized_at,finalized_by) VALUES($1,$2::date,$3,$4,$5,true,now(),$6) ON CONFLICT(truck_id,week_start) DO UPDATE SET owner_id=excluded.owner_id,edits=excluded.edits,report=excluded.report,finalized=true,version=investor_pay_weeks.version+1,finalized_at=now(),finalized_by=excluded.finalized_by`, d.ID, report.WeekStart, d.InvestorID, edits, body, actor)
		detail, e := json.Marshal(map[string]any{"reason": reason, "override": override, "issues": report.Issues, "report": d})
		if e != nil {
			return d, e
		}
		writeBatch.Queue(`INSERT INTO truck_settlement_events(truck_id,week_start,action,actor_id,details) VALUES($1,$2::date,'finalized',nullif($3,'')::uuid,$4)`, d.ID, report.WeekStart, actor, detail)
		return d, nil
	}
	inputs, _ := ctx.Value(payrollFinalizeKey{}).(*payrollFinalizeInputs)
	for _, d := range report.Drivers {
		if (truck != "" && d.ID != truck) || (truck == "" && d.TruckInactive) {
			continue
		}
		closed := d.Settlement != nil && d.Settlement.Finalized
		if closed != reopen {
			continue
		}
		count++
		selectedIDs[d.ID] = true
		if reopen {
			writeBatch.Queue(`UPDATE investor_pay_weeks SET finalized=false,version=version+1 WHERE truck_id=$1 AND week_start=$2::date`, d.ID, report.WeekStart)
		} else {
			if err = savePaySources(ctx, tx, d, &d.Edits, "investor", actor); err != nil {
				return empty, err
			}
			if err = validateInvestorCostEdits(d, d.Edits); err != nil {
				return empty, err
			}
			if !override && len(d.Issues) > 0 {
				return empty, finalizationReview(report, truck, "Resolve %s / %s: %s", d.FullName, d.TruckUnit, strings.Join(d.Issues, "; "))
			}
			for _, l := range d.Loads {
				if !override && (l.Fee == "" || len(l.Issues) > 0) {
					return empty, finalizationReview(report, truck, "Resolve loads needing review for truck %s", d.TruckUnit)
				}
			}
			expenses := append([]ExpenseDeduction(nil), d.Edits.ExpenseDeductions...)
			for i := range expenses {
				expenses[i].Apply = !expenses[i].Saved
			}
			if !bulkExpenses {
				if err = saveInvestorExpenses(ctx, tx, d, actor, expenses); err != nil {
					return empty, err
				}
			}
			if bulkExpenses {
				d.Edits.ExpenseDeductions = expenseInputs.deductions(d.ID, false)
			} else {
				d.Edits.ExpenseDeductions, err = investorExpenseDeductions(ctx, tx, d.ID, d.InvestorID, report.WeekStart, false)
				if err != nil {
					return empty, err
				}
			}

		}
		if !reopen {
			if inputs == nil {
				d, err = queueFinal(d)
				if err != nil {
					return empty, err
				}
				// Shared principals retain sequential frozen-statement validation.
				if err = tx.SendBatch(ctx, writeBatch).Close(); err != nil {
					return empty, err
				}
				writeBatch = &pgx.Batch{}
			}
			committed.Drivers = append(committed.Drivers, d)
			continue
		}
		action := "finalized"
		if reopen {
			action = "reopened"
		}
		detail, e := json.Marshal(map[string]any{"reason": reason, "override": override, "issues": report.Issues, "report": d})
		if e != nil {
			return empty, e
		}
		writeBatch.Queue(`INSERT INTO truck_settlement_events(truck_id,week_start,action,actor_id,details) VALUES($1,$2::date,$3,nullif($4,'')::uuid,$5)`, d.ID, report.WeekStart, action, actor, detail)
		committed.Drivers = append(committed.Drivers, d)
	}
	if count == 0 {
		return empty, chargeInvalid("No eligible investor settlements selected")
	}
	if inputs != nil && !reopen {
		records, e := readPaySources(ctx, tx)
		if e != nil {
			return empty, e
		}
		for i, d := range committed.Drivers {
			for j, row := range d.Edits.SourceCharges {
				d.Edits.SourceCharges[j] = projectPaySource(row.Source, row.OwnerID, row.Name, row.Base, report.WeekStart, records)
			}
			setInvestorSourceCosts(&d)
			d, err = queueFinal(d)
			if err != nil {
				return empty, err
			}
			committed.Drivers[i] = d
		}
	}
	if err = tx.SendBatch(ctx, writeBatch).Close(); err != nil {
		return empty, err
	}
	if payrollReceiptRequested(ctx) && !reopen && strings.HasPrefix(report.Revision, "inputs:") {
		if err = setPayrollRevision(ctx, tx, "investor", &committed); err != nil {
			return empty, err
		}
		return payrollReceipt(committed, selectedIDs), tx.Commit(ctx)
	}
	if payrollReceiptRequested(ctx) && truck != "" && strings.HasPrefix(report.Revision, "inputs:") {
		ctx, err = selectPayrollContext(payrollWeekContext(ctx, week.Format(time.DateOnly)), tx, "truck", truck)
		if err != nil {
			return empty, err
		}
	}
	result, err := readInvestorPay(ctx, tx, week)
	if err != nil {
		return empty, err
	}
	if payrollReceiptRequested(ctx) {
		return payrollReceipt(result, selectedIDs), tx.Commit(ctx)
	}
	return investorPayView(result, truck), tx.Commit(ctx)
}

func validateInvestorCostEdits(card DriverPayDriver, edits DriverPayEdits) error {
	for _, row := range []struct {
		name, source string
		amount       *string
	}{
		{"Fuel", card.FuelTotal, edits.FuelOverride}, {"Toll", card.TollTotal, edits.TollOverride},
	} {
		if row.amount == nil {
			continue
		}
		if card.Edits.Costs != nil {
			if row.name == "Fuel" {
				row.source = card.Edits.Costs.FuelDue
			} else {
				row.source = card.Edits.Costs.TollDue
			}
		}
		due, err := chargeCents(row.source)
		if err != nil {
			return err
		}
		if err = validatePaySourceAmount(row.name, *row.amount, -due); err != nil {
			return err
		}
	}
	return nil
}
