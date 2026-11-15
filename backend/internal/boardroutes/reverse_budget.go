package boardroutes

import (
	"context"
	"errors"
	"time"
)

var ErrReverseGeocodeBudgetExhausted = errors.New("monthly reverse geocoding request limit reached")

func (b *PostgresBudget) ReserveReverseGeocode(ctx context.Context, o Options) error {
	err := b.reserveReverseGeocode(ctx, o, time.Time{})
	if err != nil && !errors.Is(err, ErrReverseGeocodeBudgetExhausted) {
		return errors.New("reverse geocoding usage accounting unavailable")
	}
	return err
}

// This is an operational request cap, not an assertion of an account's free
// allowance. Actual search billing is separate from the routing SKU.
func (b *PostgresBudget) reserveReverseGeocode(ctx context.Context, o Options, testNow time.Time) error {
	if o.ReverseGeocodeMonthlyLimit > 1000 {
		return ErrReverseGeocodeBudgetExhausted
	}
	return b.reserveTomTom(ctx, o, testNow, "tomtom_reverse", o.ReverseGeocodeMonthlyLimit, false, ErrReverseGeocodeBudgetExhausted)
}
