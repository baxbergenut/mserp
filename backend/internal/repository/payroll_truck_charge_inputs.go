package repository

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"
)

// Keep excluded phases in the result: removing them before selecting the
// winner would revive a preceding charge. Each scope has its own dated cut.
const payrollEffectiveTruckPhasesSQL = `SELECT DISTINCT ON (p.truck_id,p.type_id)
 p.truck_id::text,p.type_id::text,p.week_start::text,p.amount::text,p.included,p.version,t.version
 FROM truck_charge_phases p JOIN driver_charge_types t ON t.id=p.type_id
 WHERE p.week_start<=scope.week AND (scope.trucks IS NULL OR p.truck_id=ANY(scope.trucks))
 ORDER BY p.truck_id,p.type_id,p.week_start DESC`

func payrollTruckPhases(ctx context.Context, q chargeQuery, week string) ([]TruckChargePhase, error) {
	trucks := payrollTruckIDs(ctx)
	pub, _ := ctx.Value(payrollPublicationReadKey{}).(*payrollPublicationReadState)
	authoritative, _ := ctx.Value(payrollAuthoritativeKey{}).(bool)
	if !authoritative && pub != nil && pub.tx == q {
		if phases, ok := pub.truckPhases[payrollTruckInputKey(week, trucks, false)]; ok {
			return phases, nil
		}
	}
	rows, err := q.Query(ctx, `SELECT p.* FROM (SELECT $1::date AS week,$2::uuid[] AS trucks) scope
 CROSS JOIN LATERAL (`+payrollEffectiveTruckPhasesSQL+`) p`, week, trucks)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var phases []TruckChargePhase
	for rows.Next() {
		var p TruckChargePhase
		if err = rows.Scan(&p.TruckID, &p.TypeID, &p.WeekStart, &p.Amount, &p.Included, &p.Version, &p.TypeVersion); err != nil {
			return nil, err
		}
		phases = append(phases, p)
	}
	return phases, rows.Err()
}

func preparePayrollTruckPhases(ctx context.Context, tx pgx.Tx, scopes []payrollTruckInputScope) error {
	pub, _ := ctx.Value(payrollPublicationReadKey{}).(*payrollPublicationReadState)
	if pub == nil || pub.tx != tx || len(scopes) < 2 {
		return nil
	}
	body, err := json.Marshal(scopes)
	if err != nil {
		return err
	}
	keys := map[string]string{}
	for _, s := range scopes {
		key := payrollTruckInputKey(s.Week, s.Trucks, false)
		keys[s.Week] = key
		pub.truckPhases[key] = nil
	}
	rows, err := tx.Query(ctx, `SELECT scope.week::text,p.* FROM jsonb_to_recordset($1::jsonb) scope(week date,trucks uuid[])
 CROSS JOIN LATERAL (`+payrollEffectiveTruckPhasesSQL+`) p`, body)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var week string
		var p TruckChargePhase
		if err = rows.Scan(&week, &p.TruckID, &p.TypeID, &p.WeekStart, &p.Amount, &p.Included, &p.Version, &p.TypeVersion); err != nil {
			return err
		}
		key := keys[week]
		pub.truckPhases[key] = append(pub.truckPhases[key], p)
	}
	return rows.Err()
}

func indexEffectiveTruckPhases(phases []TruckChargePhase, week string) map[string]map[string]TruckChargePhase {
	result := map[string]map[string]TruckChargePhase{}
	for _, p := range phases {
		if p.WeekStart > week {
			continue
		}
		if result[p.TruckID] == nil {
			result[p.TruckID] = map[string]TruckChargePhase{}
		}
		if prev, ok := result[p.TruckID][p.TypeID]; !ok || prev.WeekStart < p.WeekStart {
			result[p.TruckID][p.TypeID] = p
		}
	}
	return result
}
