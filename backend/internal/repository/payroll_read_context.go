package repository

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
)

// Calculation inputs are reusable only inside one uninterrupted read phase of
// the same transaction. Writers start a fresh phase after every mutation.
type payrollReadKey struct{}
type payrollReadState struct {
	tx          pgx.Tx
	charges     map[string][]byte
	trucks      []byte
	costs       []payCostRecord
	costsLoaded bool
}

func payrollReadContext(ctx context.Context, tx pgx.Tx) context.Context {
	if s, ok := ctx.Value(payrollReadKey{}).(*payrollReadState); ok && s.tx == tx {
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
