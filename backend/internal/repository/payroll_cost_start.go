package repository

import (
	"context"
	"github.com/jackc/pgx/v5"
	"strings"
)

// A first-live-week frozen statement retains its collected amount. Only its
// obsolete pre-start opening-balance display is removed; stored JSON is intact.
func applyCostStartDisplay(ctx context.Context, tx pgx.Tx, report *DriverPayWeek) error {
	frozen := false
	for _, d := range report.Drivers {
		if d.Settlement != nil && d.Settlement.Finalized {
			frozen = true
			break
		}
	}
	if !frozen {
		return nil
	}
	var first bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM payroll_cost_start WHERE week_start=$1::date)`, report.WeekStart).Scan(&first); err != nil {
		return err
	}
	if !first {
		return nil
	}
	for i := range report.Drivers {
		d := &report.Drivers[i]
		if d.Settlement == nil || !d.Settlement.Finalized {
			continue
		}
		if c := d.Edits.Costs; c != nil {
			v := *c
			fuel, _ := chargeCents(v.FuelDue)
			fuelCarry, _ := chargeCents(v.FuelCarry)
			toll, _ := chargeCents(v.TollDue)
			tollCarry, _ := chargeCents(v.TollCarry)
			v.FuelCarry = "0.00"
			v.TollCarry = "0.00"
			v.FuelDue = chargeMoney(fuel - fuelCarry)
			v.TollDue = chargeMoney(toll - tollCarry)
			d.Edits.Costs = &v
		}
		for j := range d.Edits.SourceCharges {
			v := &d.Edits.SourceCharges[j]
			if strings.HasPrefix(v.Source, "investor_cost:") {
				v.Carry = "0.00"
				v.Due = v.Base
			}
		}
	}
	return nil
}
