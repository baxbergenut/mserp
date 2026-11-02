package repository

import (
	"context"
	"encoding/json"
)

type PTIPlacement struct {
	TruckID string `json:"truckId"`
	Group   string `json:"group"`
}
type PTILayoutChange struct {
	Version    int64          `json:"version"`
	Placements []PTIPlacement `json:"placements"`
}

// PTI arrangement is independent of ownership, assignment, status and weekly marks.
func (r *FleetRepository) SavePTILayout(ctx context.Context, actor string, in PTILayoutChange) (int64, error) {
	if in.Version < 1 || len(in.Placements) > 10000 {
		return 0, chargeInvalid("Invalid PTI layout")
	}
	seen := map[string]bool{}
	for _, p := range in.Placements {
		if seen[p.TruckID] || (p.Group != "company" && p.Group != "owner_operator") {
			return 0, chargeInvalid("Invalid PTI placement")
		}
		seen[p.TruckID] = true
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	var version int64
	if err = tx.QueryRow(ctx, `SELECT version FROM weekly_pti_layout_state WHERE id FOR UPDATE`).Scan(&version); err != nil {
		return 0, err
	}
	if version != in.Version {
		return 0, ErrPTIConflict
	}
	active, err := tx.Query(ctx, `SELECT id::text FROM trucks WHERE active ORDER BY id FOR SHARE`)
	if err != nil {
		return 0, err
	}
	count := 0
	valid := true
	for active.Next() {
		var id string
		if err = active.Scan(&id); err != nil {
			active.Close()
			return 0, err
		}
		count++
		if !seen[id] {
			valid = false
		}
	}
	err = active.Err()
	active.Close()
	if err != nil {
		return 0, err
	}
	if !valid || count != len(seen) {
		return 0, ErrPTIConflict
	}
	var before []byte
	if err = tx.QueryRow(ctx, `SELECT coalesce(jsonb_agg(to_jsonb(l) ORDER BY truck_id),'[]'::jsonb) FROM weekly_pti_layout l`).Scan(&before); err != nil {
		return 0, err
	}
	for index, p := range in.Placements {
		if _, err = tx.Exec(ctx, `INSERT INTO weekly_pti_layout(truck_id,display_group,position) VALUES($1,$2,$3) ON CONFLICT(truck_id) DO UPDATE SET display_group=EXCLUDED.display_group,position=EXCLUDED.position`, p.TruckID, p.Group, index); err != nil {
			return 0, err
		}
	}
	after, _ := json.Marshal(in.Placements)
	if _, err = tx.Exec(ctx, `INSERT INTO weekly_pti_layout_events(actor_id,before_layout,after_layout) VALUES($1,$2,$3)`, actor, before, after); err != nil {
		return 0, err
	}
	if _, err = tx.Exec(ctx, `UPDATE weekly_pti_layout_state SET version=version+1 WHERE id`); err != nil {
		return 0, err
	}
	return version + 1, tx.Commit(ctx)
}
