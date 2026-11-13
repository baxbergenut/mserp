package fleetreview

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5"
)

type profileBatch struct {
	Source   Snapshot    `json:"source"`
	Receiver Snapshot    `json:"receiver"`
	Plan     profilePlan `json:"plan"`
}

func (d *DB) profilePreview(ctx context.Context, saved State) (*profileBatch, error) {
	a, b, err := d.snapshot(ctx, true)
	if err != nil {
		return nil, err
	}
	return &profileBatch{Source: a, Receiver: b, Plan: buildProfilePlan(saved, a, b)}, nil
}

// PrintPreviewSummary lets operators verify the plan without using or exposing
// reviewer credentials or printing fleet contact information.
func PrintPreviewSummary(ctx context.Context, db *DB, directory string, out io.Writer) error {
	body, err := os.ReadFile(filepath.Join(directory, "state.json"))
	if err != nil {
		return err
	}
	var state State
	if err := json.Unmarshal(body, &state); err != nil {
		return err
	}
	if state.Batch != nil {
		return fmt.Errorf("a batch is already frozen")
	}
	preview, err := db.profilePreview(ctx, state)
	if err != nil {
		return err
	}
	plan := preview.Plan
	fields := map[string]int{}
	for _, row := range plan.Rows {
		for _, change := range row.Changes {
			fields[change.System+" "+change.Field]++
		}
	}
	blocker := ""
	if err := db.CheckApplyReady(ctx); err != nil {
		blocker = publicError(err)
	}
	return json.NewEncoder(out).Encode(map[string]any{
		"drivers": plan.Drivers, "trucks": plan.Trucks, "review": plan.Review, "matching": plan.Matching,
		"fields": fields, "applyBlocker": blocker, "version": state.Version, "hash": digest(preview),
	})
}

// Names are derived by Clustie's existing name-parts trigger. Keeping this
// UPDATE out of the lifecycle path preserves employment, payroll and crews.
func applyLocalProfile(ctx context.Context, tx pgx.Tx, r profileRow) error {
	if r.Kind == "driver" {
		return updateProfile(ctx, tx, "drivers", []any{r.LocalID}, r.values(clustie), localDriverColumns)
	}
	return updateProfile(ctx, tx, "trucks", []any{r.LocalID}, r.values(clustie), localTruckColumns)
}

var localDriverColumns = map[string]string{
	"firstName": "first_name", "lastName": "last_name", "email": "email", "phone": "phone",
}
var localTruckColumns = map[string]string{
	"make": "make", "model": "model", "color": "color", "year": "year", "plate": "license_plate", "plateState": "license_state",
}
var remoteDriverColumns = map[string]string{
	"firstName": "first_name", "lastName": "last_name", "email": "email", "phone": "phone_number", "status": "status",
}
var remoteTruckColumns = map[string]string{
	"make": "make", "model": "model", "color": "color", "year": "year", "plate": "license_plate", "plateState": "license_plate_state",
	"status": "status", "operationalStatus": "mserp_operational_status", "operationalActive": "mserp_operational_active",
	"ownerKind": "ownership", "ownerDriverRef": "driver_id", "ownerSnapshot": "mserp_owner_snapshot", "coverageHold": "mserp_coverage_review",
}

func (d *DB) applyRemoteProfile(ctx context.Context, tx pgx.Tx, r profileRow) error {
	values := r.values(fleetScope)
	ids := []any{r.RemoteID, d.CompanyID}
	if r.Kind == "driver" {
		return updateProfile(ctx, tx, "drivers", ids, values, remoteDriverColumns)
	}
	return updateProfile(ctx, tx, "vehicles", ids, values, remoteTruckColumns)
}

// Column names come only from the fixed allowlists above. A status clause is
// emitted only for a real status change, which matters for FleetScope triggers.
func updateProfile(ctx context.Context, tx pgx.Tx, table string, args []any, values Record, allowed map[string]string) error {
	if len(values) == 0 {
		return nil
	}
	where := "id=$1"
	if len(args) == 2 {
		where += " AND company_id=$2"
	}
	set := []string{"updated_at=now()"}
	for key, value := range values {
		column, ok := allowed[key]
		if !ok {
			return fmt.Errorf("invalid profile field %s", key)
		}
		args = append(args, value)
		arg := fmt.Sprintf("$%d", len(args))
		switch key {
		case "year":
			arg += "::text::integer"
		case "operationalActive", "coverageHold":
			arg += "::text::boolean"
		case "ownerDriverRef":
			arg = "NULLIF(" + arg + ",'')::uuid"
		case "ownerSnapshot":
			arg += "::text::jsonb"
			set = append(set, "mserp_managed=true")
		}
		set = append(set, column+"="+arg)
	}
	tag, err := tx.Exec(ctx, "UPDATE "+table+" SET "+strings.Join(set, ",")+" WHERE "+where, args...)
	if err != nil {
		return fmt.Errorf("update %s profile: %w", table, err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("%s profile disappeared during apply", table)
	}
	return nil
}
