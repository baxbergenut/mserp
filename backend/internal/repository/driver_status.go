package repository

import (
	"context"
	"time"
)

type DriverStatusChange struct {
	Status          string    `json:"status"`
	TerminationDate string    `json:"terminationDate"`
	AssignmentWeek  string    `json:"assignmentWeek"`
	ChargePauseWeek string    `json:"chargePauseWeek"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

// Status actions never replace a profile snapshot. Locks, assignment dates and
// charge pauses follow the same managed workflow as the full profile editor.
func (r *FleetRepository) ChangeDriverStatus(ctx context.Context, id, actor string, input DriverStatusChange) (Driver, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Driver{}, err
	}
	defer tx.Rollback(ctx)
	if err = setBoardActor(ctx, tx, actor, "profile"); err != nil {
		return Driver{}, err
	}
	if err = setAssignmentWeek(ctx, tx, input.AssignmentWeek); err != nil {
		return Driver{}, err
	}
	if err = lockChargeDrivers(ctx, tx, []string{id}); err != nil {
		return Driver{}, err
	}
	var active bool
	var updated time.Time
	if err = tx.QueryRow(ctx, `SELECT active,updated_at FROM drivers WHERE id=$1`, id).Scan(&active, &updated); err != nil {
		return Driver{}, mapNotFound(err)
	}
	if !updated.Equal(input.UpdatedAt) {
		return Driver{}, ErrDriverBoardConflict
	}
	if input.Status == "terminated" && active {
		if err = pauseDriverCharges(ctx, tx, id, input.ChargePauseWeek, actor); err != nil {
			return Driver{}, err
		}
		if err = releaseDriverTruck(ctx, tx, id); err != nil {
			return Driver{}, err
		}
	}
	_, err = tx.Exec(ctx, `UPDATE drivers SET status=$2,active=($2<>'terminated'),
 termination_date=CASE WHEN $2='terminated' THEN nullif($3,'')::date ELSE NULL END,
 dispatcher_id=CASE WHEN $2='terminated' THEN NULL ELSE dispatcher_id END,updated_at=now() WHERE id=$1`, id, input.Status, input.TerminationDate)
	if err != nil {
		return Driver{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Driver{}, err
	}
	return r.GetDriver(ctx, id)
}
