package repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

type payrollSourceProof struct {
	Complete bool            `json:"complete"`
	Roots    []string        `json:"roots"`
	Deltas   json.RawMessage `json:"deltas"`
}

func preparedPayrollStatementCurrent(ctx context.Context, tx pgx.Tx, kind, id, week, expected, current string) (bool, error) {
	date, err := time.Parse(time.DateOnly, week)
	if err != nil {
		return false, err
	}
	proofs, err := payrollStatementSourceProofs(ctx, tx, date, kind, expected, current)
	if err != nil {
		return false, err
	}
	var valid bool
	err = tx.QueryRow(ctx, payrollStatementFreshnessSQL+`SELECT coalesce(bool_and(sources_current AND resources_current),false) FROM freshness WHERE statement_id=$5::uuid`, date, kind, expected, proofs, id).Scan(&valid)
	return valid, err
}

// Queue roots are scheduling hints. Calculate from every captured revision in
// this repeatable snapshot, including commits not yet consumed by the dispatcher.
func payrollPendingWeekImpact(ctx context.Context, tx pgx.Tx, week time.Time, current string) (*payrollPublicationImpact, error) {
	var before string
	err := tx.QueryRow(ctx, `SELECT g.input_snapshot FROM payroll_projection_weeks w
 JOIN payroll_projection_generations g ON g.id=w.generation_id WHERE w.week_start=$1
 AND g.calendar_week=$2::date AND g.calculator_version=$3 AND g.schema_version=$4`,
		week, ChargeCurrentWeek(), payrollCalculatorVersion, payrollProjectionSchemaVersion).Scan(&before)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return payrollChangedSourceImpact(ctx, tx, before, current, week.Format(time.DateOnly))
}

// Each resource certificate belongs to its statement generation, which may be
// newer or older than the weekly header after an independent publication. Never
// apply a header's revision delta to a different statement's resource baseline.
// Global/dual protocols retain their atomic whole-week header certificate.
func payrollStatementSourceProofs(ctx context.Context, tx pgx.Tx, week time.Time, kind, revision, current string, commands ...*payrollPublicationImpact) ([]byte, error) {
	rows, err := tx.Query(ctx, `SELECT DISTINCT v.generation_id::text,g.input_snapshot,g.calendar_week::text,g.calculator_version,g.schema_version
 FROM payroll_statement_state s JOIN payroll_statement_current v ON v.id=s.version_id
 JOIN payroll_projection_weeks w ON w.week_start=s.week_start
 JOIN payroll_projection_generations g ON g.id=CASE WHEN (SELECT protocol_mode='resources' FROM payroll_projection_control WHERE singleton)
 THEN v.generation_id ELSE w.generation_id END
 WHERE s.week_start=$1 AND ($2='' OR s.kind=$2) AND ($3='' OR v.id=nullif($3,'')::uuid)`, week, kind, revision)
	if err != nil {
		return nil, err
	}
	type baseline struct {
		id, snapshot, calendar, calculator string
		schema                             int
	}
	var baselines []baseline
	for rows.Next() {
		var b baseline
		if err = rows.Scan(&b.id, &b.snapshot, &b.calendar, &b.calculator, &b.schema); err != nil {
			rows.Close()
			return nil, err
		}
		baselines = append(baselines, b)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, err
	}
	proofs, cache := map[string]payrollSourceProof{}, map[string]payrollSourceProof{}
	for _, b := range baselines {
		if b.calendar != ChargeCurrentWeek() || b.calculator != payrollCalculatorVersion || b.schema != payrollProjectionSchemaVersion {
			continue
		}
		proof, found := cache[b.snapshot]
		if !found {
			proof = payrollSourceProof{Roots: []string{}, Deltas: json.RawMessage(`{}`)}
			impact, e := payrollChangedSourceImpact(ctx, tx, b.snapshot, current, week.Format(time.DateOnly))
			if e != nil {
				return nil, e
			}
			if impact != nil {
				proof.Complete, proof.Roots = true, impact.roots
				ids := append([]int64{}, impact.changeIDs...)
				if impact.weeks != nil && !impact.weeks[week.Format(time.DateOnly)] {
					proof.Roots = []string{}
				}
				for _, command := range commands {
					ids = append(ids, command.changeIDs...)
					if command.weeks == nil || command.weeks[week.Format(time.DateOnly)] {
						proof.Roots = append(append([]string{}, proof.Roots...), command.roots...)
					}
				}
				if len(ids) != 0 {
					proof.Deltas, e = payrollSourceResourceDeltas(ctx, tx, ids)
					if e != nil {
						return nil, e
					}
				}
			}
			cache[b.snapshot] = proof
		}
		proofs[b.id] = proof
	}
	return json.Marshal(proofs)
}

// The same source proof and exact resource arithmetic feed reads and derived
// rebasing. A financial command's extra epoch cannot be mistaken for a harmless
// source change, including commands that deliberately do not bump source counters.
const payrollSelectedVersionsSQL = `SELECT v.id,v.summary,v.statement_id,v.generation_id,v.resource_dependencies
 FROM payroll_statement_state s JOIN payroll_statement_current v ON v.id=s.version_id
 WHERE s.week_start=$1 AND ($2='' OR s.kind=$2) AND ($3='' OR v.id=nullif($3,'')::uuid)`

const payrollStatementFreshnessSQL = `WITH versions AS MATERIALIZED (` + payrollSelectedVersionsSQL + payrollFreshnessProofsSQL

// A financial writer may update only statements whose non-statement keys it
// holds. Check each packed shared key set once before calculating freshness for
// the remaining versions. Local keys still require their own containment check.
const payrollRebaseClaimsSQL = `WITH claims AS MATERIALIZED (
 SELECT raw IS NULL AS unrestricted,coalesce((SELECT jsonb_object_agg(k,true)
 FROM jsonb_array_elements_text(raw::jsonb) k),'{}'::jsonb) held
 FROM (SELECT nullif(current_setting('mserp.payroll.resources',true),'') raw) setting
 ), candidates AS MATERIALIZED (`

const payrollRebasePermittedSQL = `
 ), permitted_shared AS MATERIALIZED (
 SELECT d.fingerprint FROM payroll_resource_dependency_sets d
 JOIN (SELECT DISTINCT resource_dependencies->>'$shared' fingerprint FROM candidates WHERE resource_dependencies ? '$shared') s USING(fingerprint)
 CROSS JOIN claims WHERE NOT claims.unrestricted AND NOT EXISTS (
 SELECT 1 FROM jsonb_object_keys(d.dependencies) k WHERE k NOT LIKE 'statement:%' AND NOT claims.held ? k)
 ), versions AS MATERIALIZED (
 SELECT v.* FROM candidates v CROSS JOIN claims
 WHERE claims.unrestricted OR ((NOT v.resource_dependencies ? '$shared' OR EXISTS (
 SELECT 1 FROM permitted_shared p WHERE p.fingerprint=v.resource_dependencies->>'$shared'))
 AND NOT EXISTS (SELECT 1 FROM jsonb_object_keys(v.resource_dependencies-'$shared') k
 WHERE k NOT LIKE 'statement:%' AND NOT claims.held ? k))`

const payrollRebaseFreshnessSQL = payrollRebaseClaimsSQL + payrollSelectedVersionsSQL + `
 AND v.generation_id<>$5::uuid AND (NOT $6::boolean OR v.xmin<>pg_current_xact_id()::text::xid)` + payrollRebasePermittedSQL + payrollFreshnessProofsSQL

const payrollFreshnessProofsSQL = `
 ), proofs AS MATERIALIZED (
 SELECT key::uuid generation_id,coalesce((value->>'complete')::boolean,false) complete,
 ARRAY(SELECT jsonb_array_elements_text(value->'roots')) roots,value->'deltas' deltas
 FROM jsonb_each($4::jsonb)
 ), shared AS MATERIALIZED (
 SELECT d.fingerprint,refs.generation_id,d.dependencies<>'{}'::jsonb valid,
 d.dependencies ?| p.roots source_changed,
 checks.changed_keys,checks.stale_keys
 FROM payroll_resource_dependency_sets d JOIN (
 SELECT DISTINCT resource_dependencies->>'$shared' fingerprint,generation_id FROM versions
 WHERE resource_dependencies ? '$shared') refs USING(fingerprint)
 LEFT JOIN proofs p USING(generation_id)
 CROSS JOIN LATERAL (
 SELECT array_agg(e.key) FILTER (WHERE coalesce(r.revision,0)<>e.value::bigint) changed_keys,
 array_agg(e.key) FILTER (WHERE coalesce(r.revision,0)<>e.value::bigint
 AND coalesce(r.revision,0)<>e.value::bigint+coalesce((p.deltas->>e.key)::bigint,0)) stale_keys
 FROM jsonb_each_text(d.dependencies) e(key,value)
 LEFT JOIN payroll_resource_revisions r ON r.resource_key=e.key
 ) checks
 ), freshness AS (
 SELECT v.*,
 (CASE WHEN v.resource_dependencies ? '$shared' THEN shared.fingerprint IS NOT NULL
 AND NOT EXISTS(SELECT 1 FROM unnest(shared.changed_keys) key WHERE NOT v.resource_dependencies ? key)
 ELSE true END)
 AND NOT local.changed resources_exact,
 coalesce(p.complete,false)
 AND (cardinality(p.roots)=0 OR (SELECT protocol_mode='resources' FROM payroll_projection_control WHERE singleton))
 AND NOT(v.resource_dependencies ?| coalesce(p.roots,ARRAY[]::text[]))
 AND NOT coalesce(shared.source_changed,false) sources_current,
 CASE WHEN v.resource_dependencies ? '$shared' THEN
 shared.fingerprint IS NOT NULL AND (shared.valid OR v.resource_dependencies-'$shared'<>'{}'
 OR (SELECT protocol_mode='global' FROM payroll_projection_control WHERE singleton))
 AND NOT EXISTS(SELECT 1 FROM unnest(shared.stale_keys) key WHERE NOT v.resource_dependencies ? key)
 ELSE v.resource_dependencies<>'{}'::jsonb OR (SELECT protocol_mode='global' FROM payroll_projection_control WHERE singleton) END
 AND NOT local.stale resources_current
 FROM versions v LEFT JOIN proofs p ON p.generation_id=v.generation_id
 LEFT JOIN shared ON shared.fingerprint=v.resource_dependencies->>'$shared' AND shared.generation_id=v.generation_id
 CROSS JOIN LATERAL (
 SELECT coalesce(bool_or(coalesce(r.revision,0)<>d.value::bigint),false) changed,
 coalesce(bool_or(coalesce(r.revision,0)<>d.value::bigint AND
 coalesce(r.revision,0)<>d.value::bigint+coalesce((p.deltas->>d.key)::bigint,0)),false) stale
 FROM jsonb_each_text(v.resource_dependencies-'$shared') d(key,value)
 LEFT JOIN payroll_resource_revisions r ON r.resource_key=d.key
 ) local
 ) `

// Rebase only certified unchanged statements, preserving their content/version.
// Exact certificates already match every current resource counter. Preserve their
// representation rather than rebuilding and re-interning the same snapshot. An
// explained source delta still requires the original snapshot refresh below.
// This is derived work under the source gate; all financial revision checks and
// locks remain authoritative. Resource workers may touch only their held claims.
func rebasePayrollStatementResources(ctx context.Context, tx pgx.Tx, week time.Time, generation, snapshot string, commands ...*payrollPublicationImpact) error {
	proofs, err := payrollStatementSourceProofs(ctx, tx, week, "", "", snapshot, commands...)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, payrollRebaseFreshnessSQL+`UPDATE payroll_statement_current c
	 SET generation_id=$5::uuid,resource_dependencies=CASE WHEN f.resources_exact THEN f.resource_dependencies ELSE payroll_pack_resource_snapshot(
	 ARRAY(SELECT jsonb_object_keys(payroll_expand_resource_dependencies(f.resource_dependencies))),
	 'statement:'||c.kind||':'||c.statement_id::text||':'||c.week_start::text) END
 FROM freshness f WHERE c.id=f.id AND f.sources_current AND f.resources_current AND f.generation_id<>$5::uuid
	 AND (NOT $6::boolean OR c.xmin<>pg_current_xact_id()::text::xid)`, week, "", "", proofs, generation, len(commands) > 0)
	return err
}
