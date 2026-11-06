package repository

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
)

func payrollFinancialEdits(e DriverPayEdits) []byte {
	stripPayCommandMetadata(&e)
	e.Notes, e.Comments, e.Version, e.InputRevision = "", nil, 0, ""
	if len(e.Adjustments) == 0 {
		e.Adjustments = nil
	}
	b, _ := json.Marshal(e)
	return b
}

// Notes, adjustments and ordinary expense choices belong to this statement.
// Merely saving them must not post unrelated displayed source/cost balances.
// Compare the remaining submitted financial fields against the locked server
// card; explicit reimbursement/escrow applications still take the ledger path.
func payrollStatementLocalSave(before, after DriverPayEdits) bool {
	if after.Costs != nil && (before.Costs == nil || before.Costs.Revision != after.Costs.Revision) {
		return false
	}
	for _, row := range after.ExpenseDeductions {
		if !draftExpenseEffect(row) && row.Apply {
			return false
		}
	}
	for _, row := range after.GeneratedCharges {
		if row.Reset {
			return false
		}
	}
	normalize := func(e DriverPayEdits) []byte {
		e.Adjustments, e.ExpenseDeductions, e.Costs = nil, nil, nil
		e.ScaleDeductions = nil
		return payrollFinancialEdits(e)
	}
	return bytes.Equal(normalize(before), normalize(after))
}

// Publish the locally changed statement after canonical fields and captured
// events prove that no shared collection, source or cost allocation changed.
func publishPayrollTextSave(ctx context.Context, tx pgx.Tx, c *payrollCommand, kind string, edits DriverPayEdits) ([]PayrollStatementIdentity, bool, error) {
	if c == nil || c.before == nil || c.beforeSnapshot == "" || !payrollStatementLocalSave(c.before.Edits, edits) {
		return nil, false, nil
	}
	canonical, key := "driver_pay_weeks", "driver_id"
	allowed := []string{canonical}
	if kind == "investor" {
		canonical, key = "investor_pay_weeks", "truck_id"
		allowed = []string{canonical, "truck_settlement_events"}
	}
	var safe bool
	err := tx.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM payroll_projection_changes e WHERE e.source_xid=pg_current_xact_id()
 AND (NOT e.table_name=ANY($1::text[]) OR e.operation NOT IN ('INSERT','UPDATE') OR EXISTS(
 SELECT 1 FROM jsonb_array_elements(e.old_routes||e.new_routes) route
 WHERE coalesce(route->>$2,'')<>$3 OR coalesce(route->>'week_start','')<>$4)))`, allowed, key, edits.DriverID, edits.WeekStart).Scan(&safe)
	if err != nil || !safe {
		return nil, false, err
	}
	if err = payrollProjectionCoverage(ctx, tx); err != nil {
		return nil, false, err
	}
	rows, err := tx.Query(ctx, `SELECT table_name,revision FROM payroll_cache_revisions WHERE NOT table_name=ANY($1::text[]) ORDER BY table_name FOR SHARE`, payrollUnrelatedTables)
	if err != nil {
		return nil, false, err
	}
	dependencies := map[string]int64{}
	for rows.Next() {
		var table string
		var revision int64
		if err = rows.Scan(&table, &revision); err != nil {
			rows.Close()
			return nil, false, err
		}
		dependencies[table] = revision
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, false, err
	}
	ctx = context.WithValue(ctx, payrollSnapshotKey{}, payrollSnapshot{})
	snapshot, err := payrollCacheSnapshot(ctx, tx)
	if err != nil {
		return nil, false, err
	}
	depJSON, err := json.Marshal(dependencies)
	if err != nil {
		return nil, false, err
	}
	d := *c.before
	d.Edits = edits
	stripPayCommandMetadata(&d.Edits)
	d.Edits.InputRevision = ""
	applyScaleDeductions(&d)
	if kind == "driver" {
		// A choice changes the reviewed expense set even though it posts no
		// balance. Return its new token in the save receipt, so immediate
		// finalization does not require a separate refresh.
		live := DriverPayWeek{WeekStart: edits.WeekStart, Drivers: []DriverPayDriver{d}}
		if err = overlayDriverExpenseBalances(ctx, tx, &live); err != nil {
			return nil, false, err
		}
		d = live.Drivers[0]
	}
	detail, err := json.Marshal(d)
	if err != nil {
		return nil, false, err
	}
	calculated := summarizePayStatement(d)
	summary, err := json.Marshal(calculated)
	if err != nil {
		return nil, false, err
	}
	// Retain immutable membership for every generation. Unchanged statements keep
	// their reviewed version; their new generation certifies this nonimpact proof.
	// A week already stale for another reason is never certified by this command.
	var published int
	if payrollConcurrent(tx) {
		err = tx.QueryRow(ctx, `WITH generation AS (
 INSERT INTO payroll_projection_generations(week_start,requested_generation,input_snapshot,calendar_week,calculator_version,schema_version,dependencies,driver_header,investor_header)
 SELECT g.week_start,g.requested_generation,$12,g.calendar_week,g.calculator_version,g.schema_version,$13,g.driver_header,g.investor_header
 FROM payroll_projection_weeks w JOIN payroll_projection_generations g ON g.id=w.generation_id
 WHERE w.week_start=$7::date AND g.input_snapshot=$1 AND g.calendar_week=$2::date AND g.calculator_version=$3 AND g.schema_version=$4
 RETURNING id
 ), version AS (
 INSERT INTO payroll_statement_versions(generation_id,kind,statement_id,week_start,detail,summary,fee_cents,payable_cents,finalized,edit_version,settlement_version,cost_evidence,resource_dependencies)
 SELECT g.id,v.kind,v.statement_id,v.week_start,$9,$10,(($10::jsonb#>>'{summary,fee}')::numeric*100)::bigint,(($10::jsonb#>>'{summary,payable}')::numeric*100)::bigint,v.finalized,$11,v.settlement_version,v.cost_evidence,
 payroll_pack_resource_snapshot(ARRAY(SELECT jsonb_object_keys(payroll_expand_resource_dependencies(v.resource_dependencies))), 'statement:'||v.kind||':'||v.statement_id::text||':'||v.week_start::text)
 FROM payroll_statement_versions v CROSS JOIN generation g
 WHERE v.id=$8::uuid AND v.kind=$5 AND v.statement_id=$6::uuid AND v.week_start=$7::date RETURNING id
 ), pointer AS (
 UPDATE payroll_statement_state SET version_id=(SELECT id FROM version)
 WHERE kind=$5 AND statement_id=$6::uuid AND week_start=$7::date AND version_id=$8::uuid AND EXISTS(SELECT 1 FROM version) RETURNING version_id
 ) SELECT count(*) FROM pointer`, c.beforeSnapshot, ChargeCurrentWeek(), payrollCalculatorVersion, payrollProjectionSchemaVersion,
			kind, edits.DriverID, edits.WeekStart, c.expected, detail, summary, edits.Version, snapshot, depJSON).Scan(&published)
	} else {
		err = tx.QueryRow(ctx, `WITH eligible AS MATERIALIZED (
 SELECT w.week_start,w.generation_id,g.driver_header,g.investor_header,j.requested_generation
 FROM payroll_projection_weeks w JOIN payroll_projection_generations g ON g.id=w.generation_id
 JOIN payroll_projection_jobs j ON j.week_start=w.week_start
 WHERE g.input_snapshot=$1 AND g.calendar_week=$2::date AND g.calculator_version=$3 AND g.schema_version=$4
 FOR UPDATE OF w,j
 ), generations AS (
 INSERT INTO payroll_projection_generations(week_start,requested_generation,input_snapshot,calendar_week,calculator_version,schema_version,dependencies,driver_header,investor_header)
 SELECT week_start,requested_generation,$5,$2::date,$3,$4,$6,
 jsonb_set(driver_header,'{revision}',to_jsonb('inputs:'||encode(sha256(convert_to('driver:'||week_start::text||':'||$2||':'||$5||':'||$4::integer::text,'UTF8')),'hex'))),
 jsonb_set(investor_header,'{revision}',to_jsonb('inputs:'||encode(sha256(convert_to('investor:'||week_start::text||':'||$2||':'||$5||':'||$4::integer::text,'UTF8')),'hex')))
 FROM eligible RETURNING id,week_start
 ), version AS (
 INSERT INTO payroll_statement_versions(generation_id,kind,statement_id,week_start,detail,summary,fee_cents,payable_cents,finalized,edit_version,settlement_version,cost_evidence,resource_dependencies)
 SELECT g.id,v.kind,v.statement_id,v.week_start,$10,$11,(($11::jsonb#>>'{summary,fee}')::numeric*100)::bigint,(($11::jsonb#>>'{summary,payable}')::numeric*100)::bigint,v.finalized,$12,v.settlement_version,v.cost_evidence,
 payroll_pack_resource_snapshot(ARRAY(SELECT jsonb_object_keys(payroll_expand_resource_dependencies(v.resource_dependencies))), 'statement:'||v.kind||':'||v.statement_id::text||':'||v.week_start::text)
 FROM payroll_statement_versions v JOIN generations g ON g.week_start=v.week_start
 WHERE v.id=$7::uuid AND v.kind=$8 AND v.statement_id=$9::uuid
 RETURNING id,kind,statement_id,week_start
 ), pointers AS (
 UPDATE payroll_statement_state s SET version_id=v.id FROM version v
 WHERE s.kind=v.kind AND s.statement_id=v.statement_id AND s.week_start=v.week_start RETURNING s.version_id
 ), weeks AS (
 UPDATE payroll_projection_weeks w SET generation_id=g.id FROM generations g WHERE w.week_start=g.week_start RETURNING w.week_start
 ), jobs AS (
 UPDATE payroll_projection_jobs j SET completed_generation=requested_generation,lease_token=NULL,lease_until=NULL,last_error='',attempts=0
 FROM generations g WHERE j.week_start=g.week_start RETURNING j.week_start
 ) SELECT count(*) FROM pointers`, c.beforeSnapshot, ChargeCurrentWeek(), payrollCalculatorVersion, payrollProjectionSchemaVersion, snapshot, depJSON, c.expected, kind, edits.DriverID, detail, summary, edits.Version).Scan(&published)
	}
	if err != nil {
		return nil, false, err
	}
	if published != 1 {
		return nil, false, ErrDriverPayConflict
	}
	_, err = tx.Exec(ctx, `UPDATE payroll_projection_changes SET dispatched_at=clock_timestamp() WHERE source_xid=pg_current_xact_id() AND dispatched_at IS NULL`)
	return []PayrollStatementIdentity{{Kind: kind, ID: edits.DriverID, WeekStart: edits.WeekStart}}, true, err
}
