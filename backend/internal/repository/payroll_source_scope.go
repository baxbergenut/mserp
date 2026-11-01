package repository

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

// A table revision can change without touching this command's complete locked
// component. Prove that from every intervening captured revision, including old
// routing images. Missing history, unknown impact and shared resources fail closed.
// This proof is only valid under the resource transaction's shared source gate;
// it never replaces current financial-resource or submitted-version checks.
func payrollSourcesUnrelated(ctx context.Context, tx pgx.Tx, before, current string) (bool, error) {
	resource, ok := tx.(*payrollResourceTransaction)
	if !ok || !resource.single || resource.resources == nil || before == "" || current == "" {
		return false, nil
	}
	if before == current {
		return true, nil
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
		return false, nil
	}
	now, valid := parse(current)
	if !valid || len(old) != len(now) {
		return false, nil
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
			return false, nil
		}
		if latest != previous {
			ranges = append(ranges, revisionRange{table, previous, latest})
		}
	}
	if len(ranges) == 0 {
		return false, nil
	}
	body, err := json.Marshal(ranges)
	if err != nil {
		return false, err
	}
	rows, err := tx.Query(ctx, `SELECT c.id,c.table_name,c.source_revision
 FROM jsonb_to_recordset($1::jsonb) r("table" text,previous bigint,current bigint)
 JOIN payroll_projection_changes c ON c.table_name=r."table" AND c.source_revision>r.previous AND c.source_revision<=r.current`, body)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	seen := map[string]map[int64]bool{}
	ids := []int64{}
	for rows.Next() {
		var id, revision int64
		var table string
		if err = rows.Scan(&id, &table, &revision); err != nil {
			return false, err
		}
		if seen[table] == nil {
			seen[table] = map[int64]bool{}
		}
		seen[table][revision] = true
		ids = append(ids, id)
	}
	if err = rows.Err(); err != nil {
		return false, err
	}
	rows.Close()
	for _, interval := range ranges {
		if int64(len(seen[interval.Table])) != interval.Current-interval.Previous {
			return false, nil
		}
	}
	impact, err := payrollCapturedImpact(ctx, tx, nil, "", "", ids)
	if err != nil || impact == nil || len(impact.roots) == 0 {
		return false, err
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
