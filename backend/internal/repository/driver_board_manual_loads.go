package repository

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"
)

// Resolve all manually queued numbers in one cached database query. Never choose
// between multiple imported records, and retain the entered number and queue ID.
func readManualBoardLoads(ctx context.Context, tx pgx.Tx, states map[string]boardLoadState) (map[string][]BoardLoad, error) {
	key := func(number string) string { return strings.ToLower(strings.TrimSpace(number)) }
	numbers := []string{}
	for _, s := range states {
		for _, p := range s.Manual {
			numbers = append(numbers, key(p.Number))
		}
	}
	result := map[string][]BoardLoad{}
	if len(numbers) == 0 {
		return result, nil
	}
	rows, err := tx.Query(ctx, `SELECT id,load_id,status,synced_at::text,raw_payload,`+boardAvailableSourceSQL+`
 FROM loads l WHERE lower(btrim(load_id))=ANY($1::text[]) ORDER BY id`, numbers)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	counts := map[string]int{}
	missing := map[string]bool{}
	sources := map[string]BoardLoad{}
	for rows.Next() {
		var p BoardLoad
		var id int
		var raw []byte
		var available bool
		if err = rows.Scan(&id, &p.Number, &p.SourceStatus, &p.SyncedAt, &raw, &available); err != nil {
			return nil, err
		}
		if !available {
			missing[key(p.Number)] = true
			continue
		}
		p.LoadID, p.Stops = &id, boardStops(raw)
		counts[key(p.Number)]++
		sources[key(p.Number)] = p
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	for driver, s := range states {
		for _, saved := range s.Manual {
			p := BoardLoad{Manual: true, PlanID: saved.PlanID, Number: saved.Number, Stops: []BoardStop{}}
			p.SourceMissing = counts[key(saved.Number)] == 0 && missing[key(saved.Number)]
			if counts[key(saved.Number)] == 1 {
				p = sources[key(saved.Number)]
				p.Manual, p.PlanID, p.Number = true, saved.PlanID, saved.Number
			}
			annotateLoad(&p, driver)
			result[driver] = append(result[driver], p)
		}
	}
	return result, nil
}
