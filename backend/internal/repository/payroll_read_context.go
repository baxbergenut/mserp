package repository

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"mserp/internal/datatruck"
	"strings"
)

// Calculation inputs are reusable only inside one uninterrupted read phase of
// the same transaction. Writers start a fresh phase after every mutation.
type payrollReadKey struct{}
type payrollCostsOnlyKey struct{}
type payrollAuthoritativeKey struct{}

// The same prepared query serves full weeks and tiny connected components.
// PostgreSQL's generic plan severely underestimates full-week CTE row counts and
// chooses repeated nested scans after a few statement reads. Plan using this
// request's date/selection, scoped to this transaction only. Full-week CTE row
// estimates can remain too small even with a custom plan: prefer hash/merge
// joins there, while small components keep indexed nested-loop lookups. Disabled
// loop paths receive large planner costs; disable JIT explicitly so those costs
// cannot trigger expensive compilation in callers outside the application pool.
func payrollQueryPlans(ctx context.Context, tx pgx.Tx) error {
	loop := "off"
	if ids := payrollDriverIDs(ctx); ids != nil && len(ids) <= 16 {
		loop = "on"
	}
	_, err := tx.Exec(ctx, `SET LOCAL jit = off; SET LOCAL plan_cache_mode = force_custom_plan; SET LOCAL enable_nestloop = `+loop)
	return err
}

// Historical carry needs dated operating costs and routing, not installments,
// personal expenses or escrow capacity calculations for the whole fleet.
func payrollCostsOnly(ctx context.Context) bool {
	v, _ := ctx.Value(payrollCostsOnlyKey{}).(bool)
	return v
}

type payrollReadState struct {
	tx                pgx.Tx
	vacations         map[string]vacationWeek
	configuration     *payrollConfigurationState
	costs             []payCostRecord
	costsLoaded       bool
	loads             map[string]datatruck.Load
	projectionSources map[string][]byte
	projectionRoutes  map[string][]byte
	scale             map[string]payrollScaleWeek
}

type payrollConfigurationState struct {
	charges map[string][]byte
	trucks  []byte
}

type payrollPublicationReadKey struct{}
type payrollPublicationReadState struct {
	tx                pgx.Tx
	configurations    map[string]*payrollConfigurationState
	resourceLinks     map[string]map[string][]string
	scale             map[string]payrollScaleWeek
	truckInputs       map[string]truckPayInputs
	driverEligibility map[string]map[string]payrollDriverEligibility
}

// Start only after all canonical writes. Configuration queries read every phase
// and occurrence for their exact component, independently of the report week.
// Period-specific balances, routing and reports remain separate read memos.
func payrollPublicationReadContext(ctx context.Context, tx pgx.Tx) context.Context {
	return context.WithValue(ctx, payrollPublicationReadKey{}, &payrollPublicationReadState{tx: tx, configurations: map[string]*payrollConfigurationState{}, resourceLinks: map[string]map[string][]string{}, scale: map[string]payrollScaleWeek{}, truckInputs: map[string]truckPayInputs{}})
}

func payrollReadContext(ctx context.Context, tx pgx.Tx) context.Context {
	if s, ok := ctx.Value(payrollReadKey{}).(*payrollReadState); ok && s != nil && s.tx == tx {
		return ctx
	}
	configuration := &payrollConfigurationState{charges: map[string][]byte{}}
	scale := map[string]payrollScaleWeek{}
	publication, _ := ctx.Value(payrollPublicationReadKey{}).(*payrollPublicationReadState)
	authoritative, _ := ctx.Value(payrollAuthoritativeKey{}).(bool)
	if publication != nil && publication.tx == tx && !authoritative {
		scale = publication.scale
		key := "all"
		if selected, _ := ctx.Value(payrollSelectionKey{}).(*payrollSelection); selected != nil {
			key = strings.Join(selected.drivers, ",") + ":" + strings.Join(selected.trucks, ",") + ":" + strings.Join(selected.owners, ",")
		}
		if previous := publication.configurations[key]; previous != nil {
			configuration = previous
		} else {
			publication.configurations[key] = configuration
		}
	}
	return context.WithValue(ctx, payrollReadKey{}, &payrollReadState{tx: tx, configuration: configuration, scale: scale})
}
func payrollReadMemo(ctx context.Context, q chargeQuery) *payrollReadState {
	s, _ := ctx.Value(payrollReadKey{}).(*payrollReadState)
	if s != nil && s.tx == q {
		return s
	}
	return nil
}

type payrollChargeInputs struct {
	Data  ChargeData
	Loads map[string]map[string]bool
}

func payrollChargeData(ctx context.Context, q chargeQuery, driver string) (ChargeData, map[string]map[string]bool, error) {
	s := payrollReadMemo(ctx, q)
	if s != nil {
		if b := s.configuration.charges[driver]; b != nil {
			var v payrollChargeInputs
			err := json.Unmarshal(b, &v)
			return v.Data, v.Loads, err
		}
	}
	d, l, e := chargeData(ctx, q, driver)
	if e == nil && s != nil {
		s.configuration.charges[driver], _ = json.Marshal(payrollChargeInputs{d, l})
	}
	return d, l, e
}
func payrollTruckData(ctx context.Context, q chargeQuery) (TruckChargeData, error) {
	s := payrollReadMemo(ctx, q)
	if s != nil && s.configuration.trucks != nil {
		var d TruckChargeData
		e := json.Unmarshal(s.configuration.trucks, &d)
		return d, e
	}
	d, e := truckChargeData(ctx, q)
	if e == nil && s != nil {
		s.configuration.trucks, _ = json.Marshal(d)
	}
	return d, e
}
