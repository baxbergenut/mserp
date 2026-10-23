package repository

import (
	"context"
	"encoding/json"
)

type PTIMatrixTruck struct {
	ID        string `json:"id"`
	Unit      string `json:"unit"`
	Ownership string `json:"ownership"`
	Group     string `json:"group"`
	Position  *int   `json:"position"`
}
type PTIMatrix struct {
	Weeks         []string         `json:"weeks"`
	Trucks        []PTIMatrixTruck `json:"trucks"`
	Rows          []PTIRow         `json:"rows"`
	LayoutVersion int64            `json:"layoutVersion"`
}

// Only active trucks appear, independently of weekly eligibility. Display groups
// and positions never mutate the separately returned recorded ownership.
func (r *FleetRepository) PTIMatrix(ctx context.Context) (PTIMatrix, error) {
	result := PTIMatrix{Weeks: []string{}, Trucks: []PTIMatrixTruck{}, Rows: []PTIRow{}}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SET TRANSACTION ISOLATION LEVEL REPEATABLE READ, READ ONLY`); err != nil {
		return result, err
	}
	if err = tx.QueryRow(ctx, `SELECT version FROM weekly_pti_layout_state WHERE id`).Scan(&result.LayoutVersion); err != nil {
		return result, err
	}
	weeks, err := tx.Query(ctx, `SELECT week_start::text FROM weekly_pti_weeks ORDER BY week_start`)
	if err != nil {
		return result, err
	}
	for weeks.Next() {
		var week string
		if err = weeks.Scan(&week); err != nil {
			weeks.Close()
			return result, err
		}
		result.Weeks = append(result.Weeks, week)
	}
	err = weeks.Err()
	weeks.Close()
	if err != nil {
		return result, err
	}
	trucks, err := tx.Query(ctx, `SELECT t.id::text,t.unit_number,CASE WHEN i.is_company THEN 'company' WHEN i.driver_id IS NOT NULL THEN 'owner_operator' ELSE 'investor' END,coalesce(l.display_group,CASE WHEN NOT i.is_company AND i.driver_id IS NOT NULL THEN 'owner_operator' ELSE 'company' END),l.position FROM trucks t JOIN investors i ON i.id=t.owner_id LEFT JOIN weekly_pti_layout l ON l.truck_id=t.id WHERE t.active ORDER BY t.unit_number,t.id`)
	if err != nil {
		return result, err
	}
	for trucks.Next() {
		var truck PTIMatrixTruck
		if err = trucks.Scan(&truck.ID, &truck.Unit, &truck.Ownership, &truck.Group, &truck.Position); err != nil {
			trucks.Close()
			return result, err
		}
		result.Trucks = append(result.Trucks, truck)
	}
	err = trucks.Err()
	trucks.Close()
	if err != nil {
		return result, err
	}
	rows, err := tx.Query(ctx, `SELECT p.version,p.data FROM weekly_pti_rows p JOIN trucks t ON t.id=p.truck_id WHERE t.active ORDER BY p.week_start,p.truck_id`)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var row PTIRow
		var raw []byte
		var version int64
		if err = rows.Scan(&version, &raw); err != nil {
			rows.Close()
			return result, err
		}
		if err = json.Unmarshal(raw, &row); err != nil {
			rows.Close()
			return result, err
		}
		row.Version = version
		result.Rows = append(result.Rows, row)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	return result, tx.Commit(ctx)
}
