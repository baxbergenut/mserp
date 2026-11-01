package repository

import (
	"context"

	"github.com/jackc/pgx/v5"
)

type payrollSelectionFactsKey struct{}

func payrollFuelSelectionFingerprint(ctx context.Context, snapshot string) (string, error) {
	facts := payrollRoutingFactsContext(ctx, snapshot).Value(payrollRoutingFactsKey{}).(*payrollRoutingFactsState)
	return payrollRoutingFingerprint(facts, "selection_fuel")
}

// Fuel relationships are source facts, independent of allocations and saved
// balances. Persist their exact weekly and all-history edges once per captured
// source revision. The all-history row is also the completeness marker: absent
// week rows then mean no edges, not an incomplete partial preparation.
func preparePayrollFuelSelectionFacts(ctx context.Context, tx pgx.Tx, snapshot string) error {
	fingerprint, err := payrollFuelSelectionFingerprint(ctx, snapshot)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `WITH needed AS (
 SELECT NOT EXISTS(SELECT 1 FROM payroll_routing_facts WHERE kind='selection_fuel' AND week_start='1970-01-05' AND entity_id='all' AND calculator_version=$1 AND input_fingerprint=$2) yes
 ), edges AS MATERIALIZED (
 SELECT DISTINCT date_trunc('week',f.purchased_on)::date week,f.driver_id::text driver,a.truck_id::text truck
 FROM fuel_transactions f CROSS JOIN LATERAL unnest(f.reported_truck_units) unit(key)
 JOIN truck_unit_aliases a ON a.unit_key=unit.key
 WHERE f.driver_id IS NOT NULL AND (SELECT yes FROM needed)
 ), weeks AS (
 SELECT week,jsonb_agg(jsonb_build_object('driver',driver,'truck',truck) ORDER BY driver,truck) payload FROM edges WHERE week IS NOT NULL GROUP BY week
 ), all_edges AS (SELECT DISTINCT driver,truck FROM edges)
 INSERT INTO payroll_routing_facts(kind,week_start,calculator_version,input_fingerprint,entity_id,payload)
 SELECT 'selection_fuel',week,$1,$2,'',payload FROM weeks
 UNION ALL SELECT 'selection_fuel','1970-01-05'::date,$1,$2,'all',
 coalesce((SELECT jsonb_agg(jsonb_build_object('driver',driver,'truck',truck) ORDER BY driver,truck) FROM all_edges),'[]'::jsonb)
 WHERE (SELECT yes FROM needed) ON CONFLICT DO NOTHING`, payrollCalculatorVersion, fingerprint)
	return err
}

const payrollRawFuelSelectionSQL = `SELECT 'driver:'||f.driver_id a,'truck:'||a.truck_id b FROM (
 SELECT f.driver_id,f.reported_truck_units FROM source_weeks w JOIN fuel_transactions f ON f.purchased_on>=w.week AND f.purchased_on<w.week+7 WHERE f.driver_id IS NOT NULL AND $1<>''
 UNION SELECT driver_id,reported_truck_units FROM fuel_transactions WHERE driver_id IS NOT NULL AND $1=''
 ) f CROSS JOIN LATERAL unnest(f.reported_truck_units) unit(key) JOIN truck_unit_aliases a ON a.unit_key=unit.key`

// Source revisions, including aliases and empty selection scopes, must match
// exactly. A missing fact cohort always falls back to the original source query.
const payrollPreparedFuelSelectionSQL = `WITH complete AS MATERIALIZED (
 SELECT EXISTS(SELECT 1 FROM payroll_routing_facts WHERE kind='selection_fuel' AND week_start='1970-01-05' AND entity_id='all' AND calculator_version=$3 AND input_fingerprint=$2) yes
 ) SELECT 'driver:'||edge.driver a,'truck:'||edge.truck b
 FROM payroll_routing_facts f CROSS JOIN LATERAL jsonb_to_recordset(f.payload) edge(driver text,truck text)
 WHERE f.kind='selection_fuel' AND f.calculator_version=$3 AND f.input_fingerprint=$2 AND (SELECT yes FROM complete)
 AND (($1='' AND f.entity_id='all' AND f.week_start='1970-01-05') OR ($1<>'' AND f.entity_id='' AND f.week_start IN(SELECT week FROM source_weeks)))
 UNION ALL SELECT raw.a,raw.b FROM (` + payrollRawFuelSelectionSQL + `) raw WHERE NOT (SELECT yes FROM complete)`
