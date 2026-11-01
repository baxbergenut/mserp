package repository

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// Both queries read all source identities, regardless of the calculation's
// selected component. Their complete source-only results are safe to retain on
// a selected miss, unlike facts whose SQL filters by the selected identities.
// Financial allocations, charge phases and frozen overlays are applied later.
func payrollCompleteRoutingContext(ctx context.Context) context.Context {
	ctx = context.WithValue(ctx, payrollSelectionKey{}, (*payrollSelection)(nil))
	return context.WithValue(ctx, payrollCostDetailKey{}, false)
}

type payrollVacationFact struct {
	Drivers map[string]bool
	Trucks  map[string]bool
}

func readPreparedVacationState(ctx context.Context, q chargeQuery, week string, includeTrucks bool) (vacationWeek, error) {
	tx, ok := q.(pgx.Tx)
	if !ok || !includeTrucks {
		return readVacationStateUncached(ctx, q, week, includeTrucks)
	}
	fact, err := payrollRoutingFact(payrollCompleteRoutingContext(ctx), tx, "vacations", week, func() (payrollVacationFact, error) {
		v, err := readVacationStateUncached(ctx, q, week, true)
		return payrollVacationFact{Drivers: v.drivers, Trucks: v.trucks}, err
	})
	return vacationWeek{drivers: fact.Drivers, trucks: fact.Trucks}, err
}

func readPreparedStoppedTruckCharges(ctx context.Context, q chargeQuery, week string) (map[string]bool, error) {
	tx, ok := q.(pgx.Tx)
	if !ok {
		return readStoppedTruckChargesUncached(ctx, q, week)
	}
	return payrollRoutingFact(payrollCompleteRoutingContext(ctx), tx, "stopped_charges", week, func() (map[string]bool, error) {
		return readStoppedTruckChargesUncached(ctx, q, week)
	})
}
