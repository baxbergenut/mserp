package repository

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"strings"
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

// Prove that the ordinary financial save would perform no ledger writes. This
// requires identical submitted financial fields, no applied expense rows, no
// charge reset/first carry capture, and exact existing canonical cost/source
// allocations. In particular an unsaved first comment never skips collection.
func unchangedPreparedPayEffects(ctx context.Context, tx pgx.Tx, c *payrollCommand, kind string, edits DriverPayEdits) (bool, error) {
	if c == nil || c.before == nil || !bytes.Equal(payrollFinancialEdits(c.before.Edits), payrollFinancialEdits(edits)) {
		return false, nil
	}
	d := *c.before
	for _, row := range edits.ExpenseDeductions {
		if row.Apply {
			return false, nil
		}
	}
	for _, row := range edits.GeneratedCharges {
		if row.Reset || (row.Version == 0 && row.CarryForward != "0.00" && row.CarryForward != "") {
			return false, nil
		}
	}
	type expectedSource struct {
		Source string `json:"source"`
		Owner  string `json:"owner"`
		Base   string `json:"base"`
		Amount string `json:"amount"`
	}
	sources := []expectedSource{}
	for _, row := range d.Edits.SourceCharges {
		amount := row.Amount
		if kind == "investor" && strings.HasPrefix(row.Source, "investor_cost:") {
			amount = row.Due
			override := edits.FuelOverride
			if strings.HasSuffix(row.Source, ":toll") {
				override = edits.TollOverride
			}
			if override != nil {
				amount = *override
			}
		}
		due, err := chargeCents(row.Due)
		if err != nil {
			return false, err
		}
		if err = validatePaySourceAmount(row.Name, amount, due); err != nil {
			return false, err
		}
		if !row.Saved && row.Base == "0.00" && row.Due == "0.00" && amount == "0.00" {
			continue
		}
		sources = append(sources, expectedSource{row.Source, row.OwnerID, row.Base, amount})
	}
	fuel, toll, fuelAmount, tollAmount := "0.00", "0.00", "0.00", "0.00"
	if kind == "driver" {
		state := d.Edits.Costs
		if state == nil {
			return false, nil
		}
		f, t := driverCostBase(d)
		fuel, toll = chargeMoney(f), chargeMoney(t)
		fd, err := chargeCents(state.FuelDue)
		if err != nil {
			return false, err
		}
		td, err := chargeCents(state.TollDue)
		if err != nil {
			return false, err
		}
		if edits.FuelOverride != nil {
			if err = validatePaySourceAmount("Fuel", *edits.FuelOverride, -fd); err != nil {
				return false, err
			}
		}
		if edits.TollOverride != nil {
			if err = validatePaySourceAmount("Toll", *edits.TollOverride, -td); err != nil {
				return false, err
			}
		}
		fuelAmount = chargeMoney(payrollCostAmount(edits.FuelOverride, fd, d.PayType == "cpm" && state.FuelCarry == "0.00"))
		tollAmount = chargeMoney(payrollCostAmount(edits.TollOverride, td, d.PayType == "cpm" && state.TollCarry == "0.00"))
	}
	body, err := json.Marshal(sources)
	if err != nil {
		return false, err
	}
	var unchanged bool
	err = tx.QueryRow(ctx, `SELECT ($1<>'driver' OR EXISTS(SELECT 1 FROM driver_pay_cost_collections WHERE driver_id=$2::uuid AND week_start=$3::date
 AND fuel_base=$4::numeric AND toll_base=$5::numeric AND fuel_amount=$6::numeric AND toll_amount=$7::numeric))
 AND NOT EXISTS(SELECT 1 FROM jsonb_to_recordset($8::jsonb) expected(source text,owner uuid,base numeric,amount numeric)
 WHERE NOT EXISTS(SELECT 1 FROM pay_source_collections c WHERE c.source=expected.source AND c.owner_id=expected.owner AND c.week_start=$3::date
 AND c.kind=$1 AND c.statement_id=$2::uuid AND c.base=expected.base AND c.amount=expected.amount))`, kind, d.ID, edits.WeekStart, fuel, toll, fuelAmount, tollAmount, body).Scan(&unchanged)
	return unchanged, err
}

// Run the ordinary financial save first. This optimization is permitted only
// after both the resulting financial fields and the transaction's actual source
// events prove no collection/reservation/balance change. First comment saves
// that create deductions necessarily take the normal financial publication path.
func publishPayrollTextSave(ctx context.Context, tx pgx.Tx, c *payrollCommand, kind string, edits DriverPayEdits) ([]PayrollStatementIdentity, bool, error) {
	if c == nil || c.before == nil || c.beforeSnapshot == "" || !bytes.Equal(payrollFinancialEdits(c.before.Edits), payrollFinancialEdits(edits)) {
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
	detail, err := json.Marshal(d)
	if err != nil {
		return nil, false, err
	}
	summary, err := json.Marshal(summarizePayStatement(d))
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
 SELECT g.week_start,g.requested_generation,g.input_snapshot,g.calendar_week,g.calculator_version,g.schema_version,g.dependencies,g.driver_header,g.investor_header
 FROM payroll_projection_weeks w JOIN payroll_projection_generations g ON g.id=w.generation_id
 WHERE w.week_start=$7::date AND g.input_snapshot=$1 AND g.calendar_week=$2::date AND g.calculator_version=$3 AND g.schema_version=$4
 RETURNING id
 ), version AS (
 INSERT INTO payroll_statement_versions(generation_id,kind,statement_id,week_start,detail,summary,fee_cents,payable_cents,finalized,edit_version,settlement_version,cost_evidence,resource_dependencies)
 SELECT g.id,v.kind,v.statement_id,v.week_start,$9,$10,v.fee_cents,v.payable_cents,v.finalized,$11,v.settlement_version,v.cost_evidence,
 payroll_pack_resource_snapshot(ARRAY(SELECT jsonb_object_keys(payroll_expand_resource_dependencies(v.resource_dependencies))), 'statement:'||v.kind||':'||v.statement_id::text||':'||v.week_start::text)
 FROM payroll_statement_versions v CROSS JOIN generation g
 WHERE v.id=$8::uuid AND v.kind=$5 AND v.statement_id=$6::uuid AND v.week_start=$7::date RETURNING id
 ), pointer AS (
 UPDATE payroll_statement_state SET version_id=(SELECT id FROM version)
 WHERE kind=$5 AND statement_id=$6::uuid AND week_start=$7::date AND version_id=$8::uuid AND EXISTS(SELECT 1 FROM version) RETURNING version_id
 ) SELECT count(*) FROM pointer`, c.beforeSnapshot, ChargeCurrentWeek(), payrollCalculatorVersion, payrollProjectionSchemaVersion,
			kind, edits.DriverID, edits.WeekStart, c.expected, detail, summary, edits.Version).Scan(&published)
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
 jsonb_set(driver_header,'{revision}',to_jsonb('inputs:'||encode(sha256(convert_to('driver:'||week_start::text||':'||$2||':'||$5,'UTF8')),'hex'))),
 jsonb_set(investor_header,'{revision}',to_jsonb('inputs:'||encode(sha256(convert_to('investor:'||week_start::text||':'||$2||':'||$5,'UTF8')),'hex')))
 FROM eligible RETURNING id,week_start
 ), version AS (
 INSERT INTO payroll_statement_versions(generation_id,kind,statement_id,week_start,detail,summary,fee_cents,payable_cents,finalized,edit_version,settlement_version,cost_evidence,resource_dependencies)
 SELECT g.id,v.kind,v.statement_id,v.week_start,$10,$11,v.fee_cents,v.payable_cents,v.finalized,$12,v.settlement_version,v.cost_evidence,
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
