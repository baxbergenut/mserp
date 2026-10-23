package repository

import (
	"context"
	"encoding/json"
)

type PTIMatrixTruck struct {
	ID        string `json:"id"`
	Unit      string `json:"unit"`
	Ownership string `json:"ownership"`
}
type PTIMatrix struct {
	Weeks  []string         `json:"weeks"`
	Trucks []PTIMatrixTruck `json:"trucks"`
	Rows   []PTIRow         `json:"rows"`
}

// The matrix deliberately includes every truck, independently of fleet eligibility.
// Ownership comes from the recorded owner, never from the assigned driver's job.
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
	trucks, err := tx.Query(ctx, `SELECT t.id::text,t.unit_number,CASE WHEN i.is_company THEN 'company' WHEN i.driver_id IS NOT NULL THEN 'owner_operator' ELSE 'investor' END FROM trucks t JOIN investors i ON i.id=t.owner_id ORDER BY t.unit_number,t.id`)
	if err != nil {
		return result, err
	}
	for trucks.Next() {
		var truck PTIMatrixTruck
		if err = trucks.Scan(&truck.ID, &truck.Unit, &truck.Ownership); err != nil {
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
	rows, err := tx.Query(ctx, `SELECT version,data FROM weekly_pti_rows ORDER BY week_start,truck_id`)
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
