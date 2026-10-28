package repository

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
)

// Existing investor overrides were stored before operating-cost carry existed.
// Resolve their historical source (or frozen report) without writing on GET.
func readLegacyInvestorSources(ctx context.Context, tx pgx.Tx, result []paySourceRecord) ([]paySourceRecord, error) {
	rows, err := tx.Query(ctx, `SELECT truck_id::text,owner_id::text,week_start::text,edits,report,finalized,
 EXISTS(SELECT 1 FROM pay_source_collections c WHERE c.owner_id=w.owner_id AND c.week_start=w.week_start AND c.source='investor_cost:'||w.truck_id::text||':fuel'),
 EXISTS(SELECT 1 FROM pay_source_collections c WHERE c.owner_id=w.owner_id AND c.week_start=w.week_start AND c.source='investor_cost:'||w.truck_id::text||':toll') FROM investor_pay_weeks w
	WHERE accounting_live_date(w.week_start) AND ($1::uuid[] IS NULL OR w.owner_id=ANY($1::uuid[])) AND ((edits->>'fuelOverride' IS NOT NULL AND NOT EXISTS(SELECT 1 FROM pay_source_collections c WHERE c.owner_id=w.owner_id AND c.week_start=w.week_start AND c.source='investor_cost:'||w.truck_id::text||':fuel'))
	OR (edits->>'tollOverride' IS NOT NULL AND NOT EXISTS(SELECT 1 FROM pay_source_collections c WHERE c.owner_id=w.owner_id AND c.week_start=w.week_start AND c.source='investor_cost:'||w.truck_id::text||':toll'))  ) ORDER BY week_start,truck_id`, payrollOwnerIDs(ctx))
	if err != nil {
		return nil, err
	}
	type legacy struct {
		id, owner, week        string
		edits                  DriverPayEdits
		card                   DriverPayDriver
		closed                 bool
		fuelStored, tollStored bool
	}
	old := []legacy{}
	for rows.Next() {
		var v legacy
		var body, frozen []byte
		if err = rows.Scan(&v.id, &v.owner, &v.week, &body, &frozen, &v.closed, &v.fuelStored, &v.tollStored); err != nil {
			rows.Close()
			return nil, err
		}
		if err = json.Unmarshal(body, &v.edits); err != nil {
			rows.Close()
			return nil, err
		}
		if v.closed {
			if err = json.Unmarshal(frozen, &v.card); err != nil {
				rows.Close()
				return nil, err
			}
		}
		old = append(old, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	reports := map[string]DriverPayWeek{}
	for _, v := range old {
		card := v.card
		if !v.closed {
			report, ok := reports[v.week]
			if !ok {
				w, _ := chargeWeek(v.week)
				costCtx := context.WithValue(ctx, payrollCostsOnlyKey{}, true)
				ids := []string{}
				for _, other := range old {
					if other.week == v.week && !other.closed {
						ids = append(ids, other.id)
					}
				}
				costCtx, e := selectPayrollContexts(costCtx, tx, "truck", ids)
				if e != nil {
					return nil, e
				}
				costCtx = context.WithValue(costCtx, payrollReadKey{}, (*payrollReadState)(nil))
				costCtx = payrollReadContext(costCtx, tx)
				source, e := readDriverPaySourceWeek(costCtx, tx, w, "")
				if e != nil {
					return nil, e
				}
				_, report, e = routeTruckPaySources(costCtx, tx, source)
				if e != nil {
					return nil, e
				}
				reports[v.week] = report
			}
			for _, d := range report.Drivers {
				if d.ID == v.id {
					card = d
					break
				}
			}
		}
		if card.ID == "" {
			return nil, chargeInvalid("Review the saved investor costs for %s; their truck statement is unavailable", v.week)
		}
		for _, cost := range []struct {
			name, base string
			amount     *string
		}{{"Fuel", card.FuelTotal, v.edits.FuelOverride}, {"Toll", card.TollTotal, v.edits.TollOverride}} {
			if cost.amount == nil {
				continue
			}
			source := "investor_cost:" + v.id + ":" + strings.ToLower(cost.name)
			stored := (cost.name == "Fuel" && v.fuelStored) || (cost.name == "Toll" && v.tollStored)
			for _, r := range result {
				if r.Source == source && r.Owner == v.owner && r.Week == v.week {
					stored = true
					break
				}
			}
			if stored {
				continue
			}
			base, _ := chargeCents(cost.base)
			amount, e := chargeCents(*cost.amount)
			if e != nil {
				return nil, e
			}
			result = append(result, paySourceRecord{Source: source, Owner: v.owner, Week: v.week, Kind: "investor", ID: v.id, Truck: v.id, Name: cost.name, Base: chargeMoney(-base), Amount: chargeMoney(amount), Statement: card})
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Week != result[j].Week {
			return result[i].Week < result[j].Week
		}
		if result[i].Source != result[j].Source {
			return result[i].Source < result[j].Source
		}
		return result[i].Owner < result[j].Owner
	})
	return result, nil
}
