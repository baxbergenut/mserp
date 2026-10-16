package repository

import (
	"context"
	"github.com/jackc/pgx/v5"
	"strings"
)

// ApplyReviewedDriver updates only reconciliation fields inside the caller's
// fenced batch transaction. Accounting and assignment lifecycle guards are shared
// with the ordinary fleet workflow; no full-profile replacement is performed.
func ApplyReviewedDriver(ctx context.Context, tx pgx.Tx, id string, v map[string]string) error {
	if e := setAssignmentWeek(ctx, tx, ChargeCurrentWeek()); e != nil {
		return e
	}
	if e := lockChargeDrivers(ctx, tx, []string{id}); e != nil {
		return e
	}
	var oldActive bool
	if e := tx.QueryRow(ctx, `SELECT active FROM drivers WHERE id=$1 FOR UPDATE`, id).Scan(&oldActive); e != nil {
		return e
	}
	active := v["status"] != "terminated"
	if oldActive && !active {
		if e := pauseDriverCharges(ctx, tx, id, ChargeCurrentWeek(), ""); e != nil {
			return e
		}
		if e := releaseDriverTruck(ctx, tx, id); e != nil {
			return e
		}
	}
	name := strings.Join(strings.Fields(v["firstName"]+" "+v["lastName"]), " ")
	_, e := tx.Exec(ctx, `UPDATE drivers SET full_name=$2,normalized_name=$3,email=nullif($4,''),phone=nullif($5,''),status=$6,active=$7,termination_date=nullif($8,'')::date,dispatcher_id=CASE WHEN $7 THEN dispatcher_id ELSE NULL END,updated_at=now(),first_name=$9,last_name=$10 WHERE id=$1`, id, name, normalizeName(name), v["email"], v["phone"], v["status"], active, v["terminationDate"], v["firstName"], v["lastName"])
	return e
}

// ApplyReviewedTruck preserves maintenance, documents, finance and crew data.
// Ownership changes use the existing dated ownership and finalized-pay guards.
func ApplyReviewedTruck(ctx context.Context, tx pgx.Tx, id string, v map[string]string) error {
	if e := setAssignmentWeek(ctx, tx, ChargeCurrentWeek()); e != nil {
		return e
	}
	input := TruckInput{OwnershipEffectiveDate: v["ownerDate"], ExpectedOwnershipVersion: v["ownershipVersion"]}
	if v["ownerDriverId"] != "" {
		x := v["ownerDriverId"]
		input.OwnerDriverID = &x
	} else {
		x := v["ownerId"]
		input.OwnerID = &x
	}
	if e := resolveTruckDriverOwner(ctx, tx, &input); e != nil {
		return e
	}
	if e := setDatedTruckOwner(ctx, tx, id, input); e != nil {
		return e
	}
	if v["active"] == "false" {
		if e := releaseTruckDriver(ctx, tx, id); e != nil {
			return e
		}
	}
	_, e := tx.Exec(ctx, `UPDATE trucks SET unit_number=$2,vin=$3,year=$4::int,make=$5,model=$6,license_plate=$7,license_state=$8,active=$9::boolean,status=$10,color=nullif($11,''),updated_at=now() WHERE id=$1`, id, v["unit"], v["vin"], v["year"], v["make"], v["model"], v["plate"], v["plateState"], v["active"], v["status"], v["color"])
	return e
}
