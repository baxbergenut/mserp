package repository

import (
	"context"
	"errors"
)

var ErrFiveELDReviewConflict = errors.New("This truck review changed or the truck already exists. Reload and review the current details.")

type FiveELDTruckReview struct {
	ID         string `json:"id"`
	VIN        string `json:"vin"`
	UnitNumber string `json:"unitNumber"`
	Version    int    `json:"version"`
	Completed  bool   `json:"completed"`
}

func (r *FleetRepository) FiveELDTruckReview(ctx context.Context, id string) (FiveELDTruckReview, error) {
	var v FiveELDTruckReview
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return v, err
	}
	defer tx.Rollback(ctx)
	visible, err := systemTaskVisible(ctx, tx, "five_eld_truck", false)
	if err != nil {
		return v, err
	}
	if !visible {
		return v, ErrNotFound
	}
	err = tx.QueryRow(ctx, `SELECT id::text,eld_vin,eld_unit,eld_version,completed_at IS NOT NULL FROM system_task_records WHERE id=$1 AND kind='five_eld_truck'`, id).Scan(&v.ID, &v.VIN, &v.UnitNumber, &v.Version, &v.Completed)
	return v, mapNotFound(err)
}

// Truck creation and review completion share one transaction. The original ELD
// VIN remains the source identity even when the reviewer corrects the saved VIN.
func (r *FleetRepository) CompleteFiveELDTruckReview(ctx context.Context, id, actor string, version int, input TruckInput) (string, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	visible, err := systemTaskVisible(ctx, tx, "five_eld_truck", true)
	if err != nil {
		return "", err
	}
	if !visible {
		return "", ErrNotFound
	}
	var vin string
	var current int
	var completed bool
	err = tx.QueryRow(ctx, `SELECT eld_vin,eld_version,completed_at IS NOT NULL FROM system_task_records WHERE id=$1 AND kind='five_eld_truck' FOR UPDATE`, id).Scan(&vin, &current, &completed)
	if err != nil {
		return "", mapNotFound(err)
	}
	if completed || current != version {
		return "", ErrFiveELDReviewConflict
	}
	if err = setAssignmentWeek(ctx, tx, input.AssignmentWeek); err != nil {
		return "", err
	}
	// Serialize against normal truck inserts/updates as well as other reviews.
	if _, err = tx.Exec(ctx, `LOCK TABLE trucks IN SHARE ROW EXCLUSIVE MODE`); err != nil {
		return "", err
	}
	var exists bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM trucks WHERE upper(regexp_replace(vin,'[^A-Za-z0-9]','','g')) IN ($1,upper(regexp_replace($2,'[^A-Za-z0-9]','','g')))) OR EXISTS(SELECT 1 FROM five_eld_sync_state WHERE $1=ANY(ambiguous_vins))`, vin, input.VIN).Scan(&exists)
	if err != nil {
		return "", err
	}
	if exists {
		return "", ErrFiveELDReviewConflict
	}
	truckID, err := createTruckTx(ctx, tx, input)
	if err != nil {
		return "", err
	}
	_, err = tx.Exec(ctx, `UPDATE system_task_records SET completed_at=now(),completed_by_name=coalesce((SELECT username FROM app_users WHERE id=$2),''),outcome='Added truck to MSERP',eld_truck_id=$3,eld_version=eld_version+1 WHERE id=$1`, id, actor, truckID)
	if err != nil {
		return "", err
	}
	return truckID, tx.Commit(ctx)
}
