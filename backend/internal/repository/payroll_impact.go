package repository

import (
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type payrollPublicationImpact struct {
	roots         []string
	snapshot      string
	weeks         map[string]bool // nil means every materialized week may be affected.
	refreshHeader bool
	loadIDs       []int64 // Changed sources need new immutable facts before publication.
	finalizedKind string  // Metadata-only freeze/reopen preserves the other page's values.
	changeIDs     []int64 // Complete captured events for source/command revision proofs.
	liveOnly      bool    // Base membership is certified; only dynamic expense balances changed.
}
type payrollPublicationSelectionKey struct{}
type payrollPublicationHeaderKey struct{}
type payrollPublicationImpactKey struct{}

// Narrow registered ledger and principal edits using both event images. Deleted
// principals are covered only when their old routing was captured in this same
// transaction. Unknown tables and unresolved routes retain full publication.
func payrollMutationImpact(ctx context.Context, tx pgx.Tx, c *payrollCommand, kind, id string) (*payrollPublicationImpact, error) {
	if c == nil || c.beforeSnapshot == "" {
		return nil, nil
	}
	return payrollCapturedImpact(ctx, tx, c, kind, id, nil)
}

func payrollCapturedImpact(ctx context.Context, tx pgx.Tx, c *payrollCommand, kind, id string, changeIDs []int64) (*payrollPublicationImpact, error) {
	allowed := map[string]string{
		"driver_pay_weeks": "driver_id", "driver_pay_cost_collections": "driver_id",
		"investor_pay_weeks": "truck_id", "truck_settlement_events": "truck_id",
		"pay_source_collections": "owner_id", "expense_payments": "expense_id",
		"expense_reimbursement_payments": "expense_id", "driver_escrow_payments": "escrow_id",
		"driver_charge_occurrences": "schedule_id", "driver_charge_events": "schedule_id",
		"payroll_settlements": "driver_id", "payroll_settlement_events": "driver_id",
		"expenses": "id", "expense_balance_corrections": "expense_id", "expense_external_settlements": "expense_id",
		"driver_escrows": "id", "driver_escrow_releases": "escrow_id", "driver_escrow_release_events": "escrow_id", "driver_escrow_opening_events": "escrow_id",
		"driver_charge_schedules": "id", "driver_charge_phases": "schedule_id",
		"truck_driver_assignments": "truck_id", "driver_teams": "main_driver_id",
		"truck_ownership_history": "truck_id", "truck_settlement_terms": "truck_id", "truck_charge_phases": "truck_id",
		"trucks":  "id",
		"drivers": "id", "investors": "id", "fleet_status_history": "entity_id", "driver_termination_truck_actions": "driver_id", "driver_dispatcher_assignments": "driver_id",
		"fuel_transactions": "id", "fuel_transaction_items": "fuel_transaction_id", "fuel_transaction_fees": "fuel_transaction_id",
		"tolls":               "id",
		"gross_board_entries": "driver_id", "gross_board_extra_entries": "driver_id", "gross_board_transfers": "driver_id",
		"loads":              "id",
		"load_split_history": "load_id", "payroll_source_actions": "driver_id",
		"payroll_load_placements": "driver_id", "payroll_load_placement_history": "driver_id", "gross_board_transfer_history": "driver_id",
	}
	// Relationship edits must retain both ends of every old and new edge. Reading
	// only the post-write graph would lose the disconnected former destination.
	relationships := map[string][]string{
		"truck_driver_assignments":         {"truck_id", "driver_id"},
		"driver_teams":                     {"main_driver_id", "team_driver_id"},
		"truck_ownership_history":          {"truck_id", "owner_id"},
		"truck_settlement_terms":           {"truck_id", "owner_id"},
		"truck_charge_phases":              {"truck_id"},
		"driver_termination_truck_actions": {"driver_id", "truck_id"},
	}
	rows, err := tx.Query(ctx, `SELECT id,table_name,operation,old_routes||new_routes,revision_only,
 live_expense FROM payroll_projection_changes
 WHERE CASE WHEN $1::bigint[] IS NULL THEN source_xid=pg_current_xact_id() ELSE id=ANY($1::bigint[]) END`, changeIDs)
	if err != nil {
		return nil, err
	}
	roots := map[string]bool{}
	if kind == "investor" {
		kind = "truck"
	}
	if id != "" {
		roots[kind+":"+id] = true
	}
	refs := map[string]map[string]bool{"expense_id": {}, "escrow_id": {}, "schedule_id": {}, "fuel_transaction_id": {}}
	principals := map[string]map[string]bool{"expense_id": {}, "escrow_id": {}, "schedule_id": {}, "fuel_transaction_id": {}}
	principalKeys := map[string]string{"expenses": "expense_id", "driver_escrows": "escrow_id", "driver_charge_schedules": "schedule_id"}
	fuelUnits := map[string]bool{}
	loadIDs := map[int64]bool{}
	loadNumbers := map[string]bool{}
	refreshHeader := false
	safe := true
	periodOnly := c != nil && (strings.HasSuffix(c.name, ".finalize") || strings.HasSuffix(c.name, ".reopen"))
	changedWeeks := map[string]bool{}
	boardOnly := true
	boardStart := ""
	boardDrivers := map[string]bool{}
	loadsOnly := true
	capturedIDs := []int64{}
	nonLiveEvents := 0
	for rows.Next() {
		var changeID int64
		var table, operation string
		var body []byte
		var revisionOnly bool
		var liveExpense bool
		if err = rows.Scan(&changeID, &table, &operation, &body, &revisionOnly, &liveExpense); err != nil {
			rows.Close()
			return nil, err
		}
		capturedIDs = append(capturedIDs, changeID)
		if liveExpense {
			continue
		}
		nonLiveEvents++
		key, known := allowed[table]
		loadsOnly = loadsOnly && table == "loads"
		if table != "gross_board_entries" && table != "gross_board_extra_entries" {
			boardOnly = false
		}
		if table != "payroll_settlements" && table != "payroll_settlement_events" && table != "investor_pay_weeks" && table != "truck_settlement_events" {
			periodOnly = false
		}
		if revisionOnly && (table == "expenses" || table == "driver_escrows" || table == "driver_charge_schedules") {
			key, known = "id", true
		}
		if !known || operation == "TRUNCATE" {
			safe = false
			continue
		}
		var routes []map[string]json.RawMessage
		if err = json.Unmarshal(body, &routes); err != nil {
			rows.Close()
			return nil, err
		}
		if len(routes) == 0 {
			boardOnly = false
		}
		for _, route := range routes {
			value := func(k string) string { var v string; _ = json.Unmarshal(route[k], &v); return v }
			if boardOnly {
				day, e := time.Parse(time.DateOnly, value("service_date"))
				if e != nil || value("driver_id") == "" {
					boardOnly = false
				} else {
					start := day.AddDate(0, 0, -(int(day.Weekday())+6)%7).Format(time.DateOnly)
					if boardStart == "" || start < boardStart {
						boardStart = start
					}
					boardDrivers[value("driver_id")] = true
				}
			}
			// Editing the company identity or adding/removing a legacy company
			// ledger can change graph connectivity across all its trucks.
			if (table == "investors" && value("id") == CompanyOwnerID) ||
				(value("owner_id") == CompanyOwnerID && ((table == "expenses" && strings.EqualFold(strings.TrimSpace(value("covered_by")), "Truck Owner")) || table == "truck_settlement_terms" ||
					table == "investor_pay_weeks" || table == "pay_source_collections")) {
				safe = false
			}
			broker := false
			if table == "load_split_history" || table == "payroll_load_placements" || table == "payroll_load_placement_history" {
				field := "load_record_id"
				if table == "load_split_history" {
					field = "load_id"
				}
				loadID, parseErr := strconv.ParseInt(string(route[field]), 10, 64)
				if parseErr != nil || loadID <= 0 {
					safe = false
				} else {
					loadIDs[loadID] = true
				}
				refreshHeader = true
				if table == "load_split_history" {
					continue
				}
			}
			if table == "loads" {
				// Imported record IDs are integers, unlike financial UUIDs. Capture
				// both old and new display numbers: introducing a duplicate can
				// invalidate a previously unique match without changing that row.
				loadID, parseErr := strconv.ParseInt(string(route["id"]), 10, 64)
				_, capturedNumber := route["load_id"]
				if parseErr != nil || loadID <= 0 || !capturedNumber {
					safe = false
					continue
				}
				loadIDs[loadID] = true
				loadNumbers[strings.ToLower(strings.TrimSpace(value("load_id")))] = true
				refreshHeader = true
				for _, field := range []string{"driver_id", "truck_id"} {
					if v := value(field); v != "" {
						roots[strings.TrimSuffix(field, "_id")+":"+v] = true
					}
				}
				continue
			}
			if (table == "trucks" || table == "drivers" || table == "investors") && value("id") != "" {
				kind := map[string]string{"trucks": "truck", "drivers": "driver", "investors": "owner"}[table]
				roots[kind+":"+value("id")] = true
				refreshHeader = true
			}
			if table == "fleet_status_history" {
				kind := value("entity_kind")
				if kind != "driver" && kind != "truck" {
					safe = false
				} else if entity := value("entity_id"); entity != "" {
					roots[kind+":"+entity] = true
				}
				refreshHeader = true
			}
			if table == "gross_board_entries" || table == "gross_board_extra_entries" || table == "gross_board_transfers" || table == "gross_board_transfer_history" || table == "payroll_source_actions" || table == "driver_dispatcher_assignments" {
				refreshHeader = true
			}
			if fields, relationship := relationships[table]; relationship {
				refreshHeader = true
				for _, field := range fields {
					if value(field) == "" {
						safe = false
					}
				}
			}
			if table == "expenses" && !revisionOnly {
				if _, captured := route["covered_by"]; !captured {
					safe = false
				}
				if strings.EqualFold(strings.TrimSpace(value("covered_by")), "Broker") {
					// The reminder follows both old and new numbers, independently
					// of the expense's optional native driver/truck attribution.
					if _, captured := route["broker_load_number"]; !captured {
						safe = false
					}
					broker = true
					if number := strings.ToLower(strings.TrimSpace(value("broker_load_number"))); number != "" {
						loadNumbers[number] = true
					}
				}
			}
			if week := value("week_start"); week != "" {
				changedWeeks[week] = true
			} else {
				periodOnly = false
			}
			if value(key) == "" {
				safe = false
				continue
			}
			routed := broker
			for _, field := range []string{"driver_id", "main_driver_id", "team_driver_id", "charge_driver_id", "truck_id", "related_truck_id", "investor_truck_id", "owner_id"} {
				if table == "expenses" && (field == "truck_id" || field == "owner_id") && !strings.EqualFold(strings.TrimSpace(value("covered_by")), "Truck Owner") {
					continue // Bookkeeping attribution does not make this truck owe the invoice.
				}
				v := value(field)
				if v == "" {
					continue
				}
				prefix := "driver"
				if field == "truck_id" || field == "related_truck_id" || field == "investor_truck_id" {
					prefix = "truck"
				}
				if field == "owner_id" {
					prefix = "owner"
				}
				roots[prefix+":"+v] = true
				routed = true
			}
			if parentKey := principalKeys[table]; parentKey != "" {
				if !routed {
					safe = false
				}
				principals[parentKey][value("id")] = routed
				refreshHeader = refreshHeader || !revisionOnly
			}
			if table == "tolls" {
				refreshHeader = true
				safe = safe && value("truck_id") != ""
			}
			if table == "fuel_transactions" {
				refreshHeader = true
				var units []string
				if err := json.Unmarshal(route["reported_truck_units"], &units); err != nil {
					safe = false
				}
				for _, unit := range units {
					fuelUnits[unit] = true
				}
				principals["fuel_transaction_id"][value("id")] = routed || len(units) > 0
				if !routed && len(units) == 0 {
					safe = false
				}
			}
			if statement := value("statement_id"); statement != "" {
				prefix := value("kind")
				if prefix == "" {
					prefix = value("statement_kind")
				}
				if prefix == "investor" {
					prefix = "truck"
				}
				if prefix != "driver" && prefix != "truck" {
					safe = false
				} else {
					roots[prefix+":"+statement] = true
				}
			}
			if set, ok := refs[key]; ok {
				set[value(key)] = true
			}
		}
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if !safe {
		return nil, nil
	}
	if nonLiveEvents == 0 && len(capturedIDs) > 0 {
		return &payrollPublicationImpact{roots: []string{}, weeks: map[string]bool{}, changeIDs: capturedIDs, liveOnly: true}, nil
	}
	keys := func(m map[string]bool) []string {
		s := make([]string, 0, len(m))
		for k := range m {
			s = append(s, k)
		}
		sort.Strings(s)
		return s
	}
	// Resolve principal references in one query. A missing parent is never silently
	// ignored: deletion belongs to the conservative source-change path.
	if len(refs["expense_id"])+len(refs["escrow_id"])+len(refs["schedule_id"]) > 0 {
		rows, err = tx.Query(ctx, `SELECT 'expense_id',id::text,ARRAY['driver:'||driver_id,'driver:'||charge_driver_id,
 CASE WHEN lower(btrim(covered_by))='truck owner' THEN 'truck:'||truck_id END,
 CASE WHEN lower(btrim(covered_by))='truck owner' THEN 'owner:'||owner_id END] FROM expenses WHERE id=ANY($1::uuid[])
 UNION ALL SELECT 'expense_id',expense_id::text,ARRAY['truck:'||investor_truck_id] FROM expense_payments WHERE expense_id=ANY($1::uuid[]) AND investor_truck_id IS NOT NULL
 UNION ALL SELECT 'escrow_id',id::text,ARRAY['driver:'||driver_id] FROM driver_escrows WHERE id=ANY($2::uuid[])
 UNION ALL SELECT 'schedule_id',id::text,ARRAY['driver:'||driver_id] FROM driver_charge_schedules WHERE id=ANY($3::uuid[])`, keys(refs["expense_id"]), keys(refs["escrow_id"]), keys(refs["schedule_id"]))
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var key, id string
			var nodes []*string
			if err = rows.Scan(&key, &id, &nodes); err != nil {
				rows.Close()
				return nil, err
			}
			delete(refs[key], id)
			for _, node := range nodes {
				if node != nil {
					roots[*node] = true
				}
			}
		}
		rows.Close()
		if err = rows.Err(); err != nil {
			return nil, err
		}
	}
	for key, set := range refs {
		if key == "fuel_transaction_id" {
			continue
		}
		for id := range set {
			if !principals[key][id] {
				return nil, nil
			}
		}
	}
	if len(refs["fuel_transaction_id"]) > 0 {
		refreshHeader = true
		rows, err = tx.Query(ctx, `SELECT id::text,coalesce(driver_id::text,''),reported_truck_units FROM fuel_transactions WHERE id=ANY($1::uuid[])`, keys(refs["fuel_transaction_id"]))
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id, driver string
			var units []string
			if err = rows.Scan(&id, &driver, &units); err != nil {
				rows.Close()
				return nil, err
			}
			delete(refs["fuel_transaction_id"], id)
			if driver != "" {
				roots["driver:"+driver] = true
			}
			for _, unit := range units {
				fuelUnits[unit] = true
			}
			if driver == "" && len(units) == 0 {
				safe = false
			}
		}
		rows.Close()
		if err = rows.Err(); err != nil {
			return nil, err
		}
		for id := range refs["fuel_transaction_id"] {
			if !principals["fuel_transaction_id"][id] {
				safe = false
			}
		}
	}
	if len(fuelUnits) > 0 {
		// Retain every alias candidate. Ambiguity is resolved by the existing
		// dated calculator, never by impact discovery. Unknown aliases require
		// conservative publication because they can affect unlinked warnings.
		rows, err = tx.Query(ctx, `SELECT unit_key,truck_id::text FROM truck_unit_aliases WHERE unit_key=ANY($1::text[])`, keys(fuelUnits))
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var unit, truck string
			if err = rows.Scan(&unit, &truck); err != nil {
				rows.Close()
				return nil, err
			}
			delete(fuelUnits, unit)
			roots["truck:"+truck] = true
		}
		rows.Close()
		if err = rows.Err(); err != nil {
			return nil, err
		}
		if len(fuelUnits) > 0 {
			safe = false
		}
	}
	if !safe {
		return nil, nil
	}
	changedLoads := make([]int64, 0, len(loadIDs))
	for id := range loadIDs {
		changedLoads = append(changedLoads, id)
	}
	sort.Slice(changedLoads, func(i, j int) bool { return changedLoads[i] < changedLoads[j] })
	if len(changedLoads) > 0 || len(loadNumbers) > 0 {
		if err := payrollLoadImpactRoots(ctx, tx, changedLoads, keys(loadNumbers), roots); err != nil {
			return nil, err
		}
	}
	// Expand these old/new roots separately for EVERY materialized week. The
	// selection contract includes that week's dated routing, open legacy cost
	// weeks, future reservations and unpaid saved destinations. Fully paid old
	// assignments do not connect otherwise independent present-day obligations.
	impact := &payrollPublicationImpact{roots: keys(roots), refreshHeader: refreshHeader, loadIDs: changedLoads}
	// A complete load identity/old-new-number lookup covered native board rows,
	// explicit placements and frozen reports. An unplaced provider load cannot
	// affect a statement. Keep that proven empty result distinct from unknown.
	if loadsOnly && len(changedLoads) > 0 && len(roots) == 0 {
		impact.weeks = map[string]bool{}
		impact.refreshHeader = false
	}
	if c != nil {
		impact.snapshot = c.beforeSnapshot
	}
	earliest := ""
	if boardOnly && boardStart != "" {
		// Board duplicate ranking is chronological and transfers only move a
		// weekend into the following week. Ordinary board edits therefore cannot
		// change earlier weeks. Explicit placements can bring that source back
		// into an earlier statement, including frozen discrepancy evidence.
		// Include cross-driver placements and retained placement history too:
		// a removed/reassigned placement can still be shown by a frozen report.
		err = tx.QueryRow(ctx, `SELECT least($1::date,coalesce(min(week_start),$1::date))::text
 FROM (
 SELECT week_start FROM payroll_load_placements WHERE driver_id=ANY($2::uuid[]) OR source_date >= $1::date
 UNION ALL SELECT (image->>'week_start')::date FROM payroll_load_placement_history h
 CROSS JOIN LATERAL (VALUES(h.before_value),(h.after_value)) v(image)
 WHERE image->>'driver_id'=ANY($2::text[]) OR (image->>'source_date')::date >= $1::date
 ) placements`, boardStart, keys(boardDrivers)).Scan(&earliest)
		if err != nil {
			return nil, err
		}
	}
	if earliest != "" {
		periods, e := tx.Query(ctx, `SELECT week_start::text FROM payroll_projection_weeks WHERE week_start >= $1::date`, earliest)
		if e != nil {
			return nil, e
		}
		impact.weeks = map[string]bool{}
		for periods.Next() {
			var week string
			if e = periods.Scan(&week); e != nil {
				periods.Close()
				return nil, e
			}
			impact.weeks[week] = true
		}
		periods.Close()
		if e = periods.Err(); e != nil {
			return nil, e
		}
	}
	if periodOnly && len(changedWeeks) > 0 {
		// Finalization metadata can affect other weeks through legacy overrides
		// whose base is reconstructed from a frozen report. Require canonical cost
		// coverage before treating these specific commands as period-local. Any
		// posting/confirmation event above already disables this optimization.
		// A frozen or just-reopened scale receipt also changes every open week.
		var affectsOtherWeeks bool
		err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM driver_pay_weeks w
 WHERE w.week_start::text=ANY($1::text[]) AND ('driver:'||w.driver_id::text)=ANY($2::text[])
 AND (w.fuel_override IS NOT NULL OR w.toll_override IS NOT NULL)
 AND NOT EXISTS(SELECT 1 FROM driver_pay_cost_collections c WHERE c.driver_id=w.driver_id AND c.week_start=w.week_start))
 OR EXISTS(SELECT 1 FROM investor_pay_weeks w
 WHERE w.week_start::text=ANY($1::text[]) AND ('truck:'||w.truck_id::text)=ANY($2::text[]) AND (
 (w.edits->>'fuelOverride' IS NOT NULL AND NOT EXISTS(SELECT 1 FROM pay_source_collections c WHERE c.owner_id=w.owner_id AND c.week_start=w.week_start AND c.source='investor_cost:'||w.truck_id::text||':fuel')) OR
 (w.edits->>'tollOverride' IS NOT NULL AND NOT EXISTS(SELECT 1 FROM pay_source_collections c WHERE c.owner_id=w.owner_id AND c.week_start=w.week_start AND c.source='investor_cost:'||w.truck_id::text||':toll'))))
 OR EXISTS(SELECT 1 FROM (`+wmtStatementReportsSQL+`) f
 WHERE f.week_start::text=ANY($1::text[])
 AND (CASE f.kind WHEN 'driver' THEN 'driver:' ELSE 'truck:' END||f.destination)=ANY($2::text[])
 AND jsonb_path_exists(f.report, '$.autoCharges[*].source ? (@ starts with "weighmytruck:")'))`, keys(changedWeeks), impact.roots).Scan(&affectsOtherWeeks)
		if err != nil {
			return nil, err
		}
		if !affectsOtherWeeks && strings.HasPrefix(c.name, "driver.") {
			affectsOtherWeeks, err = driverSalaryCarryAffectsOtherWeeks(ctx, tx, keys(changedWeeks), impact.roots)
			if err != nil {
				return nil, err
			}
		}
		if !affectsOtherWeeks {
			impact.weeks = changedWeeks
			if strings.HasSuffix(c.name, ".finalize") || (payrollConcurrent(tx) && strings.HasSuffix(c.name, ".reopen")) {
				impact.finalizedKind = strings.TrimSuffix(strings.TrimSuffix(c.name, ".finalize"), ".reopen")
			}
		}
	}
	impact.changeIDs = capturedIDs
	return impact, nil
}
