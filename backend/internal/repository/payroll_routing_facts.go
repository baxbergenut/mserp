package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
)

type payrollRoutingFactsKey struct{}
type payrollRoutingFactsState struct {
	revisions map[string]string
	values    map[string][]byte
	calendar  string
}

// These are source facts, before expense allocations, saved overrides, carry,
// owner destination decisions and frozen reports. Keep this dependency list in
// step with truckPayLoadMapUncached and readTruckCostAllocationsUncached; changing
// either contract requires a calculator version bump.
var payrollRoutingDependencies = map[string][]string{
	"driver_days":      {"drivers", "driver_teams", "gross_board_entries", "gross_board_extra_entries", "gross_board_transfers"},
	"investor_days":    {"drivers", "driver_teams", "gross_board_entries", "gross_board_extra_entries", "gross_board_transfers", "truck_driver_assignments"},
	"selection_fuel":   {"fuel_transactions", "truck_unit_aliases"},
	"vacations":        {"drivers", "fleet_status_history", "truck_driver_assignments", "driver_teams", "trucks", "truck_ownership_history", "investors"},
	"stopped_charges":  {"drivers", "truck_driver_assignments", "driver_teams", "trucks", "truck_ownership_history", "investors", "driver_termination_truck_actions"},
	"native_costs":     {"fuel_transactions", "fuel_transaction_items", "driver_teams", "truck_unit_aliases", "tolls", "truck_driver_assignments", "drivers", "trucks"},
	"driver_cost_base": {"drivers", "driver_teams", "truck_driver_assignments", "truck_ownership_history", "trucks", "investors", "driver_dispatcher_assignments", "dispatchers", "gross_board_entries", "gross_board_extra_entries", "gross_board_transfers", "loads", "payroll_load_placements", "payroll_load_placement_history", "fuel_transactions", "fuel_transaction_items", "tolls"},
	"driver_base":      {"drivers", "driver_teams", "truck_driver_assignments", "truck_ownership_history", "trucks", "investors", "driver_dispatcher_assignments", "dispatchers", "gross_board_entries", "gross_board_extra_entries", "gross_board_transfers", "loads", "payroll_load_placements", "payroll_load_placement_history", "fuel_transactions", "fuel_transaction_items", "tolls"},
	"load_trucks":      {"gross_board_entries", "gross_board_extra_entries", "gross_board_transfers", "loads", "payroll_load_placements", "payroll_load_placement_history", "truck_driver_assignments"},
	"truck_costs":      {"fuel_transactions", "fuel_transaction_items", "driver_teams", "truck_unit_aliases", "tolls", "truck_driver_assignments"},
}

// Preserve private attribution and ordering alongside the public evidence row.
// These facts contain original transactions only, never collection allocation.
type payrollNativeCostFact struct {
	Row                     PayCostTransaction
	DriverID, TruckID, Kind string
	SortKey                 string
	DriverAttributed        bool
}

func readPreparedNativeCostTransactions(ctx context.Context, tx pgx.Tx, week string) ([]PayCostTransaction, error) {
	// The native query always reads the complete week. It is therefore safe to
	// retain its full result even when called by a selected statement's trace.
	factCtx := context.WithValue(ctx, payrollSelectionKey{}, (*payrollSelection)(nil))
	factCtx = context.WithValue(factCtx, payrollCostDetailKey{}, false)
	facts, err := payrollRoutingFact(factCtx, tx, "native_costs", week, func() ([]payrollNativeCostFact, error) {
		items, err := readNativeCostTransactionsUncached(ctx, tx, week)
		if err != nil {
			return nil, err
		}
		result := make([]payrollNativeCostFact, 0, len(items))
		for _, v := range items {
			result = append(result, payrollNativeCostFact{v, v.driverID, v.truckID, v.kind, v.sortKey, v.driverAttributed})
		}
		return result, nil
	})
	if err != nil {
		return nil, err
	}
	result := make([]PayCostTransaction, 0, len(facts))
	for _, f := range facts {
		v := f.Row
		v.driverID, v.truckID, v.kind, v.sortKey, v.driverAttributed = f.DriverID, f.TruckID, f.Kind, f.SortKey, f.DriverAttributed
		result = append(result, v)
	}
	return result, nil
}

func payrollRoutingFactsContext(ctx context.Context, snapshot string) context.Context {
	s := &payrollRoutingFactsState{revisions: map[string]string{}, values: map[string][]byte{}, calendar: ChargeCurrentWeek()}
	for _, entry := range strings.Split(snapshot, ",") {
		name, revision, ok := strings.Cut(entry, ":")
		if ok {
			s.revisions[name] = revision
		}
	}
	return context.WithValue(ctx, payrollRoutingFactsKey{}, s)
}

func payrollRoutingFingerprint(s *payrollRoutingFactsState, kind string) (string, error) {
	parts := []string{payrollCalculatorVersion, kind}
	// Only source-card shape changed. Retain unrelated normalized load, cost,
	// routing and balance facts instead of invalidating every calculator cache.
	if kind == "driver_base" {
		parts = append(parts, "datatruck-driver-gross-v1")
	}
	// Untracked legacy availability applies only from the current calendar week.
	// Its meaning can change at rollover even without a canonical source write.
	if kind == "vacations" {
		parts = append(parts, s.calendar)
	}
	dependencies, known := payrollRoutingDependencies[kind]
	if !known {
		return "", ErrPayrollProjectionCoverage
	}
	for _, table := range dependencies {
		revision, ok := s.revisions[table]
		if !ok {
			return "", ErrPayrollProjectionCoverage
		}
		parts = append(parts, table+":"+revision)
	}
	digest := sha256.Sum256([]byte(strings.Join(parts, ",")))
	return hex.EncodeToString(digest[:]), nil
}

// Only a complete selection may create a reusable fact. Selected commands can
// consume it and filter below, but a cache miss calculates their exact component
// without accidentally persisting that partial result as a fleet-wide input.
func payrollRoutingFact[T any](ctx context.Context, tx pgx.Tx, kind, week string, compute func() (T, error)) (T, error) {
	s, _ := ctx.Value(payrollRoutingFactsKey{}).(*payrollRoutingFactsState)
	if s == nil || !payrollNormalizedInputs(ctx) || payrollCostDetails(ctx) {
		return compute()
	}
	var zero T
	fingerprint, err := payrollRoutingFingerprint(s, kind)
	if err != nil {
		return zero, err
	}
	key := kind + ":" + week + ":" + fingerprint
	body, found := s.values[key]
	if !found {
		err := tx.QueryRow(ctx, `SELECT payload FROM payroll_routing_facts WHERE kind=$1 AND week_start=$2::date AND calculator_version=$3 AND input_fingerprint=$4 AND entity_id=''`, kind, week, payrollCalculatorVersion, fingerprint).Scan(&body)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return zero, err
		}
		if err == nil {
			s.values[key] = body
			found = true
		}
	}
	if found {
		var value T
		err := json.Unmarshal(body, &value)
		return value, err
	}
	value, err := compute()
	if err != nil || payrollDriverIDs(ctx) != nil || payrollTruckIDs(ctx) != nil {
		return value, err
	}
	body, err = json.Marshal(value)
	if err != nil {
		return zero, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO payroll_routing_facts(kind,week_start,calculator_version,input_fingerprint,payload) VALUES($1,$2::date,$3,$4,$5) ON CONFLICT DO NOTHING`, kind, week, payrollCalculatorVersion, fingerprint, body)
	if err == nil {
		s.values[key] = body
	}
	return value, err
}

func truckPayLoadMap(ctx context.Context, tx pgx.Tx, week string) (map[string]truckPayLoadMatch, error) {
	result, err := payrollRoutingFact(ctx, tx, "load_trucks", week, func() (map[string]truckPayLoadMatch, error) {
		return truckPayLoadMapUncached(ctx, tx, week)
	})
	if ids := payrollDriverIDs(ctx); err == nil && ids != nil {
		keep := map[string]bool{}
		for _, id := range ids {
			keep[id] = true
		}
		for key := range result {
			id, _, _ := strings.Cut(key, ":")
			if !keep[id] {
				delete(result, key)
			}
		}
	}
	return result, err
}

type payrollTruckCostFact struct {
	Fuel, Toll             int64
	DriverFuel, DriverToll map[string]int64
	UnlinkedFuel           bool
}

func readTruckCostAllocations(ctx context.Context, tx pgx.Tx, week string) (map[string]*truckCostAllocation, error) {
	// Itemized evidence has private attribution fields and always uses its own
	// snapshot read; it must never lose them through JSON serialization.
	if payrollCostDetails(ctx) {
		return readTruckCostAllocationsUncached(ctx, tx, week)
	}
	facts, err := payrollRoutingFact(ctx, tx, "truck_costs", week, func() (map[string]payrollTruckCostFact, error) {
		costs, err := readTruckCostAllocationsUncached(ctx, tx, week)
		if err != nil {
			return nil, err
		}
		result := map[string]payrollTruckCostFact{}
		for id, c := range costs {
			result[id] = payrollTruckCostFact{c.fuel, c.toll, c.driverFuel, c.driverToll, c.unlinkedFuel}
		}
		return result, nil
	})
	if err != nil {
		return nil, err
	}
	selected := payrollTruckIDs(ctx)
	keep := map[string]bool{}
	for _, id := range selected {
		keep[id] = true
	}
	result := map[string]*truckCostAllocation{}
	for id, c := range facts {
		if selected == nil || keep[id] {
			result[id] = &truckCostAllocation{fuel: c.Fuel, toll: c.Toll, driverFuel: c.DriverFuel, driverToll: c.DriverToll, unlinkedFuel: c.UnlinkedFuel}
		}
	}
	return result, nil
}
