package repository

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"
)

// Called inside the seeder's exclusive source gate. Facts are optional caches;
// evict entire obsolete cohorts, never individual rows under a complete marker.
// Audit references and canonical financial records are never garbage-collected.
func maintainPayrollStorage(ctx context.Context, tx pgx.Tx) error {
	snapshot, err := payrollCacheSnapshot(ctx, tx)
	if err != nil {
		return err
	}
	facts := payrollRoutingFactsContext(ctx, snapshot).Value(payrollRoutingFactsKey{}).(*payrollRoutingFactsState)
	fingerprints := map[string]string{"load_cohort": facts.revisions["loads"]}
	for kind := range payrollRoutingDependencies {
		value, e := payrollRoutingFingerprint(facts, kind)
		if e != nil {
			return e
		}
		fingerprints[kind] = value
	}
	body, err := json.Marshal(fingerprints)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `SELECT set_config('mserp.payroll_storage_maintenance','on',true)`); err != nil {
		return err
	}
	for _, statement := range []struct {
		query string
		args  []any
	}{
		{`DELETE FROM payroll_routing_facts WHERE calculator_version<>$1 OR input_fingerprint IS DISTINCT FROM ($2::jsonb->>kind)`, []any{payrollCalculatorVersion, body}},
		{`DELETE FROM payroll_load_facts f WHERE calculator_version<>$1 OR NOT EXISTS(SELECT 1 FROM loads l WHERE l.id=f.load_id AND f.source_hash=` + payrollLoadFactHashSQL + `)`, []any{payrollCalculatorVersion}},
		{`DELETE FROM payroll_projection_generations g WHERE NOT EXISTS(SELECT 1 FROM payroll_projection_weeks w WHERE w.generation_id=g.id)
 AND NOT EXISTS(SELECT 1 FROM payroll_statement_current c WHERE c.generation_id=g.id)`, nil},
		{`DELETE FROM payroll_resource_dependency_sets d WHERE NOT EXISTS(SELECT 1 FROM payroll_statement_current c WHERE c.resource_dependencies->>'$shared'=d.fingerprint)`, nil},
	} {
		if _, err = tx.Exec(ctx, statement.query, statement.args...); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(ctx, `SELECT set_config('mserp.payroll_storage_maintenance','off',true)`); err != nil {
		return err
	}
	return nil
}
