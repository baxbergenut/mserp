package repository

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

// Ownership corrections cancel only fees generated for the wrong owner. Debt
// generated before the effective week remains with that owner; later collections
// may still repay that debt, but cannot collect the cancelled new fees.
func correctOwnershipFees(ctx context.Context, tx pgx.Tx, truck, day, actor string) error {
	date, err := time.Parse(time.DateOnly, day)
	if err != nil {
		return err
	}
	week := date.AddDate(0, 0, -(int(date.Weekday())+6)%7).Format(time.DateOnly)
	rows, err := tx.Query(ctx, `SELECT c.source,c.owner_id::text,c.week_start::text,c.kind,c.statement_id::text,
 c.truck_id::text,c.name,c.base::text,c.amount::text,c.version,
 coalesce((SELECT h.owner_id=c.owner_id FROM truck_ownership_history h WHERE h.truck_id=c.truck_id
 AND (NOT h.start_known OR (h.assigned_at AT TIME ZONE 'America/New_York')::date<c.week_start+7)
 AND (h.unassigned_at IS NULL OR (h.unassigned_at AT TIME ZONE 'America/New_York')::date>=c.week_start+7)
 ORDER BY h.assigned_at DESC LIMIT 1),false)
 FROM pay_source_collections c WHERE c.truck_id=$1 AND c.source LIKE 'truck_charge:%'
 ORDER BY c.source,c.owner_id,c.week_start FOR UPDATE OF c`, truck)
	if err != nil {
		return err
	}
	type entry struct {
		paySourceRecord
		valid bool
	}
	entries := []entry{}
	for rows.Next() {
		var v entry
		if err = rows.Scan(&v.Source, &v.Owner, &v.Week, &v.Kind, &v.ID, &v.Truck, &v.Name, &v.Base, &v.Amount, &v.Version, &v.valid); err != nil {
			rows.Close()
			return err
		}
		entries = append(entries, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	balances := map[string]int64{}
	correcting := map[string]bool{}
	for _, v := range entries {
		key := sourceKey(v.Source, v.Owner)
		base, _ := chargeCents(v.Base)
		amount, _ := chargeCents(v.Amount)
		if v.Week >= week && !v.valid {
			correcting[key] = true
		}
		if correcting[key] {
			newBase := base
			if !v.valid {
				newBase = 0
			}
			due := balances[key] + newBase
			newAmount := amount
			// Keep explicit deferrals/partial collections, bounded by the corrected debt.
			if due >= 0 {
				newAmount = min(max(amount, 0), due)
			} else {
				newAmount = max(min(amount, 0), due)
			}
			if newBase != base || newAmount != amount {
				var closed bool
				err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM payroll_settlements s WHERE s.finalized AND s.week_start>=$2::date
 AND (s.driver_id=payroll_driver_at((SELECT driver_id FROM investors WHERE id=$3),s.week_start)
 OR s.report @> jsonb_build_object('autoCharges',jsonb_build_array(jsonb_build_object('source',$4::text)))))
 OR EXISTS(SELECT 1 FROM investor_pay_weeks w WHERE w.finalized AND w.truck_id=$1 AND w.week_start>=$2::date)`, truck, v.Week, v.Owner, v.Source).Scan(&closed)
				if err != nil {
					return err
				}
				if closed {
					return chargeInvalid("Reopen affected finalized statements before clearing recurring fees from the ownership date")
				}
				if err = truckAudit(ctx, tx, truck, v.Week, "ownership_fee_correction", actor, map[string]any{
					"effectiveDate": day, "source": v.Source, "ownerId": v.Owner, "kind": v.Kind, "statementId": v.ID,
					"previousBase": v.Base, "previousAmount": v.Amount, "base": chargeMoney(newBase), "amount": chargeMoney(newAmount),
				}); err != nil {
					return err
				}
				// Remove empty invalid allocations, so they cannot resurrect a fee on
				// the former owner's statement. The audit retains the original values.
				if newBase == 0 && newAmount == 0 && balances[key] == 0 {
					_, err = tx.Exec(ctx, `DELETE FROM pay_source_collections WHERE source=$1 AND owner_id=$2 AND week_start=$3::date`, v.Source, v.Owner, v.Week)
				} else {
					_, err = tx.Exec(ctx, `UPDATE pay_source_collections SET base=$4::numeric,amount=$5::numeric,version=version+1,updated_at=now(),updated_by=nullif($6,'')::uuid
 WHERE source=$1 AND owner_id=$2 AND week_start=$3::date`, v.Source, v.Owner, v.Week, chargeMoney(newBase), chargeMoney(newAmount), actor)
				}
				if err != nil {
					return err
				}
				base, amount = newBase, newAmount
			}
		}
		balances[key] += base - amount
	}
	return nil
}
