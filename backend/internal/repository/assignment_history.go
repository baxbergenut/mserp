package repository

import (
	"context"
	"time"
)

type AssignmentHistoryEntry struct {
	ID              string     `json:"id"`
	Kind            string     `json:"kind"`
	RelatedID       *string    `json:"relatedId"`
	Name            string     `json:"name"`
	AssignedAt      time.Time  `json:"assignedAt"`
	UnassignedAt    *time.Time `json:"unassignedAt"`
	StartKnown      bool       `json:"startKnown"`
	Source          string     `json:"source"`
	ActorID         string     `json:"actorId"`
	ActorName       string     `json:"actorName"`
	RecordedAt      *time.Time `json:"recordedAt"`
	EndedByID       string     `json:"endedById"`
	EndedByName     string     `json:"endedByName"`
	EndedRecordedAt *time.Time `json:"endedRecordedAt"`
}

func (r *FleetRepository) TruckAssignmentHistory(ctx context.Context, truckID string) ([]AssignmentHistoryEntry, error) {
	var exists bool
	if err := r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM trucks WHERE id=$1)`, truckID).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrNotFound
	}
	rows, err := r.pool.Query(ctx, `SELECT a.id,'driver',a.driver_id,d.full_name,a.assigned_at,a.unassigned_at,true,a.source,
 a.actor_id,a.actor_name,a.recorded_at,a.ended_by_id,a.ended_by_name,a.ended_recorded_at
 FROM truck_driver_assignments a JOIN drivers d ON d.id=a.driver_id WHERE a.truck_id=$1 ORDER BY a.assigned_at DESC,a.id`, truckID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []AssignmentHistoryEntry{}
	for rows.Next() {
		var e AssignmentHistoryEntry
		if err = rows.Scan(&e.ID, &e.Kind, &e.RelatedID, &e.Name, &e.AssignedAt, &e.UnassignedAt, &e.StartKnown, &e.Source, &e.ActorID, &e.ActorName, &e.RecordedAt, &e.EndedByID, &e.EndedByName, &e.EndedRecordedAt); err != nil {
			return nil, err
		}
		result = append(result, e)
	}
	return result, rows.Err()
}

func (r *FleetRepository) DriverAssignmentHistory(ctx context.Context, driverID string) ([]AssignmentHistoryEntry, error) {
	var exists bool
	if err := r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM drivers WHERE id=$1)`, driverID).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrNotFound
	}
	rows, err := r.pool.Query(ctx, `
 SELECT a.id,'truck' AS kind,a.truck_id AS related_id,t.unit_number AS name,
 a.assigned_at,a.unassigned_at,true AS start_known,a.source,
 a.actor_id,a.actor_name,a.recorded_at,a.ended_by_id,a.ended_by_name,a.ended_recorded_at
 FROM truck_driver_assignments a JOIN trucks t ON t.id=a.truck_id WHERE a.driver_id=$1
 UNION ALL
 SELECT a.id,'dispatcher',a.dispatcher_id,a.dispatcher_name,
 a.assigned_at,a.unassigned_at,a.start_known,a.source,
 a.actor_id,a.actor_name,a.recorded_at,a.ended_by_id,a.ended_by_name,a.ended_recorded_at
 FROM driver_dispatcher_assignments a WHERE a.driver_id=$1
 ORDER BY assigned_at DESC,id`, driverID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []AssignmentHistoryEntry{}
	for rows.Next() {
		var e AssignmentHistoryEntry
		if err = rows.Scan(&e.ID, &e.Kind, &e.RelatedID, &e.Name, &e.AssignedAt, &e.UnassignedAt, &e.StartKnown, &e.Source, &e.ActorID, &e.ActorName, &e.RecordedAt, &e.EndedByID, &e.EndedByName, &e.EndedRecordedAt); err != nil {
			return nil, err
		}
		result = append(result, e)
	}
	return result, rows.Err()
}
