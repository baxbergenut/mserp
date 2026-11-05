package repository

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

type payrollSourceImpactMemoKey struct{}
type payrollSourceImpactMemo struct {
	tx     pgx.Tx
	proofs map[[3]string]*payrollPublicationImpact
}

// A table revision can change without touching this command's complete locked
// component. Prove that from every intervening captured revision, including old
// routing images. Missing history, unknown impact and shared resources fail closed.
// This proof is only valid under the resource transaction's shared source gate;
// it never replaces current financial-resource or submitted-version checks.
func payrollSourcesUnrelated(ctx context.Context, tx pgx.Tx, before, current string) (bool, error) {
	return payrollSourcesUnrelatedForWeek(ctx, tx, before, current, "")
}

func payrollSourcesUnrelatedForWeek(ctx context.Context, tx pgx.Tx, before, current, week string) (bool, error) {
	resource, ok := tx.(*payrollResourceTransaction)
	if !ok || !resource.single || resource.resources == nil || before == "" || current == "" {
		return false, nil
	}
	impact, err := payrollChangedSourceImpact(ctx, tx, before, current, week)
	if err != nil || impact == nil {
		return false, err
	}
	if week != "" && impact.weeks != nil && !impact.weeks[week] {
		return true, nil
	}
	claimed := map[string]bool{}
	for _, key := range resource.resources.keys {
		claimed[key] = true
	}
	for _, root := range impact.roots {
		if claimed[root] {
			return false, nil
		}
	}
	return true, nil
}

func retainPayrollPeriodSourceProofs(ctx context.Context, tx pgx.Tx, current string) error {
	resource, ok := tx.(*payrollResourceTransaction)
	if !ok || !resource.single || resource.periodSources != nil {
		return nil
	}
	rows, err := tx.Query(ctx, `SELECT w.week_start::text,g.input_snapshot FROM payroll_projection_weeks w JOIN payroll_projection_generations g ON g.id=w.generation_id`)
	if err != nil {
		return err
	}
	type period struct{ week, snapshot string }
	var periods []period
	for rows.Next() {
		var p period
		if err = rows.Scan(&p.week, &p.snapshot); err != nil {
			rows.Close()
			return err
		}
		periods = append(periods, p)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	resource.periodSources = map[string]bool{}
	for _, p := range periods {
		valid, e := payrollSourcesUnrelatedForWeek(ctx, tx, p.snapshot, current, p.week)
		if e != nil {
			return e
		}
		resource.periodSources[p.week+":"+p.snapshot] = valid
	}
	return nil
}

// A repeatable read may use the same complete capture proof to label each
// statement. Financial commands still require the locked wrapper above.
func payrollChangedSourceRoots(ctx context.Context, tx pgx.Tx, before, current string) ([]string, bool, error) {
	impact, err := payrollChangedSourceImpact(ctx, tx, before, current, "")
	if err != nil || impact == nil {
		return nil, false, err
	}
	return impact.roots, true, nil
}

// Keep the period proof with the principal proof. Discarding it makes a
// current-week board edit look like a change to the same driver's past payroll.
func payrollChangedSourceImpact(ctx context.Context, tx pgx.Tx, before, current, week string) (*payrollPublicationImpact, error) {
	if memo, ok := ctx.Value(payrollSourceImpactMemoKey{}).(*payrollSourceImpactMemo); ok && memo.tx == tx {
		key := [3]string{before, current, week}
		if proof, found := memo.proofs[key]; found {
			return proof, nil
		}
		proof, err := readPayrollChangedSourceImpact(ctx, tx, before, current, week)
		if err == nil {
			memo.proofs[key] = proof
		}
		return proof, err
	}
	return readPayrollChangedSourceImpact(ctx, tx, before, current, week)
}

func readPayrollChangedSourceImpact(ctx context.Context, tx pgx.Tx, before, current, week string) (*payrollPublicationImpact, error) {
	if before == "" || current == "" {
		return nil, nil
	}
	if before == current {
		return &payrollPublicationImpact{roots: []string{}, weeks: map[string]bool{}}, nil
	}
	parse := func(value string) (map[string]int64, bool) {
		result := map[string]int64{}
		for _, entry := range strings.Split(value, ",") {
			name, revision, found := strings.Cut(entry, ":")
			n, err := strconv.ParseInt(revision, 10, 64)
			if _, duplicate := result[name]; !found || name == "" || duplicate || err != nil || n < 0 {
				return nil, false
			}
			result[name] = n
		}
		return result, true
	}
	old, valid := parse(before)
	if !valid {
		return nil, nil
	}
	now, valid := parse(current)
	if !valid || len(old) != len(now) {
		return nil, nil
	}
	type revisionRange struct {
		Table    string `json:"table"`
		Previous int64  `json:"previous"`
		Current  int64  `json:"current"`
	}
	ranges := []revisionRange{}
	for table, previous := range old {
		latest, found := now[table]
		if !found || latest < previous {
			return nil, nil
		}
		if latest != previous {
			ranges = append(ranges, revisionRange{table, previous, latest})
		}
	}
	if len(ranges) == 0 {
		return nil, nil
	}
	body, err := json.Marshal(ranges)
	if err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT c.id,c.table_name,c.source_revision
 FROM jsonb_to_recordset($1::jsonb) r("table" text,previous bigint,current bigint)
 JOIN payroll_projection_changes c ON c.table_name=r."table" AND c.source_revision>r.previous AND c.source_revision<=r.current`, body)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	seen := map[string]map[int64]bool{}
	var ids, boardIDs, otherIDs []int64
	for rows.Next() {
		var id, revision int64
		var table string
		if err = rows.Scan(&id, &table, &revision); err != nil {
			return nil, err
		}
		if seen[table] == nil {
			seen[table] = map[int64]bool{}
		}
		seen[table][revision] = true
		ids = append(ids, id)
		if table == "gross_board_entries" || table == "gross_board_extra_entries" {
			boardIDs = append(boardIDs, id)
		} else {
			otherIDs = append(otherIDs, id)
		}
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	for _, interval := range ranges {
		if int64(len(seen[interval.Table])) != interval.Current-interval.Previous {
			return nil, nil
		}
	}
	impactIDs := ids
	if week != "" && len(boardIDs) > 0 && len(otherIDs) > 0 {
		// A provider or ledger change in the same interval must not discard
		// the date and placement proof for later Gross Board edits.
		board, err := payrollCapturedImpact(ctx, tx, nil, "", "", boardIDs)
		if err != nil {
			return nil, err
		}
		if board != nil && board.weeks != nil && !board.weeks[week] {
			impactIDs = otherIDs
		}
	}
	impact, err := payrollCapturedImpact(ctx, tx, nil, "", "", impactIDs)
	if err != nil || impact == nil || (len(impact.roots) == 0 && impact.weeks == nil) {
		return nil, err
	}
	// Even when a board event cannot change this week's values, it can bump
	// a shared resource epoch. Keep its captured delta in the exact proof.
	impact.changeIDs = ids
	return impact, nil
}

// Only these captured source events may explain a stale resource epoch for an
// unaffected period. Resource-mode financial commands can advance epochs without
// advancing source counters, so never simply ignore a stale resource check.
func payrollSourceResourceDeltas(ctx context.Context, tx pgx.Tx, ids []int64) ([]byte, error) {
	var body []byte
	err := tx.QueryRow(ctx, `SELECT coalesce(jsonb_object_agg(key,n),'{}'::jsonb) FROM (
 SELECT key,count(*) n FROM (
 SELECT DISTINCT c.id,k.key FROM payroll_projection_changes c
 CROSS JOIN LATERAL jsonb_array_elements(c.old_routes||c.new_routes) route
 CROSS JOIN LATERAL unnest(payroll_row_revision_keys(c.table_name,route)) k(key)
 WHERE c.id=ANY($1::bigint[]) AND payroll_resource_table(c.table_name) AND k.key IS NOT NULL
 ) events GROUP BY key
 ) revisions`, ids).Scan(&body)
	return body, err
}
