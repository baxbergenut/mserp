package repository

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
)

// Calculation inputs are reusable only inside one uninterrupted read phase of
// the same transaction. Writers start a fresh phase after every mutation.
type payrollReadKey struct{}
type payrollCostsOnlyKey struct{}

// The same prepared query serves full weeks and tiny connected components.
// PostgreSQL's generic plan severely underestimates full-week CTE row counts and
// chooses repeated nested scans after a few statement reads. Plan using this
// request's date/selection, scoped to this transaction only.
func payrollQueryPlans(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, `SET LOCAL plan_cache_mode = force_custom_plan`)
	return err
}

// Historical carry needs dated operating costs and routing, not installments,
// personal expenses, escrow capacity, or layover calculations for the whole fleet.
func payrollCostsOnly(ctx context.Context) bool {
	v, _ := ctx.Value(payrollCostsOnlyKey{}).(bool)
	return v
}

type payrollReadState struct {
	tx          pgx.Tx
	vacations   map[string]vacationWeek
	charges     map[string][]byte
	trucks      []byte
	costs       []payCostRecord
	costsLoaded bool
}

func payrollReadContext(ctx context.Context, tx pgx.Tx) context.Context {
	if s, ok := ctx.Value(payrollReadKey{}).(*payrollReadState); ok && s != nil && s.tx == tx {
		return ctx
	}
	return context.WithValue(ctx, payrollReadKey{}, &payrollReadState{tx: tx, charges: map[string][]byte{}})
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
		if b := s.charges[driver]; b != nil {
			var v payrollChargeInputs
			err := json.Unmarshal(b, &v)
			return v.Data, v.Loads, err
		}
	}
	d, l, e := chargeData(ctx, q, driver)
	if e == nil && s != nil {
		s.charges[driver], _ = json.Marshal(payrollChargeInputs{d, l})
	}
	return d, l, e
}
func payrollTruckData(ctx context.Context, q chargeQuery) (TruckChargeData, error) {
	s := payrollReadMemo(ctx, q)
	if s != nil && s.trucks != nil {
		var d TruckChargeData
		e := json.Unmarshal(s.trucks, &d)
		return d, e
	}
	d, e := truckChargeData(ctx, q)
	if e == nil && s != nil {
		s.trucks, _ = json.Marshal(d)
	}
	return d, e
}
