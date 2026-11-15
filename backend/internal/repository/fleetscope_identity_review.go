package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// This is an operator-reviewed UUID manifest, never an automatic name matcher.
// Saving identities does not enroll outbound fleet sync or rewrite profiles.
type FleetScopeIdentityReview struct {
	CompanyID string                       `json:"companyId"`
	Review    string                       `json:"review"`
	Links     []FleetScopeReviewedIdentity `json:"links"`
}

type FleetScopeReviewedIdentity struct {
	DriverID           string    `json:"driverId"`
	FleetScopeDriverID string    `json:"fleetscopeDriverId"`
	UpdatedAt          time.Time `json:"updatedAt"`
	Evidence           string    `json:"evidence"`
	TerminationID      string    `json:"terminationId,omitempty"`
}

type FleetScopeIdentityReviewResult struct {
	Linked       int  `json:"linked"`
	Existing     int  `json:"existing"`
	Terminations int  `json:"terminations"`
	Applied      bool `json:"applied"`
}

// Preview executes all database guards inside a rolled-back transaction. Apply
// requires the command's exact manifest hash and repeats the same version guards.
func (r *FleetRepository) ReviewFleetScopeIdentities(ctx context.Context, review FleetScopeIdentityReview, apply bool) (FleetScopeIdentityReviewResult, error) {
	result := FleetScopeIdentityReviewResult{}
	if strings.TrimSpace(review.Review) == "" || len(review.Links) == 0 || len(review.Links) > 1000 {
		return result, errors.New("a review note and 1-1000 explicit identity pairs are required")
	}
	tx, err := beginPayrollSourceMutation(ctx, r.pool)
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('fleetscope:intake',0))`); err != nil {
		return result, err
	}
	if err = setBoardActor(ctx, tx, "", "fleetscope_identity_review"); err != nil {
		return result, err
	}
	// Recovery is observed today, just like a newly received termination.
	// Backdating assignments to Monday would erase work already performed.
	loc, _ := time.LoadLocation("America/New_York")
	if err = setAssignmentWeek(ctx, tx, time.Now().In(loc).Format(time.DateOnly)); err != nil {
		return result, err
	}
	if _, err = tx.Exec(ctx, `LOCK TABLE fleetscope_driver_identity IN SHARE ROW EXCLUSIVE MODE`); err != nil {
		return result, err
	}
	local, remote := map[string]bool{}, map[string]bool{}
	for _, link := range review.Links {
		if link.DriverID == "" || link.FleetScopeDriverID == "" || link.UpdatedAt.IsZero() || strings.TrimSpace(link.Evidence) == "" || local[link.DriverID] || remote[link.FleetScopeDriverID] {
			return result, errors.New("each identity must be unique and include its reviewed version and evidence")
		}
		local[link.DriverID], remote[link.FleetScopeDriverID] = true, true
		var updated time.Time
		var status string
		if err = tx.QueryRow(ctx, `SELECT updated_at,status FROM drivers WHERE id=$1 FOR UPDATE`, link.DriverID).Scan(&updated, &status); err != nil {
			return result, err
		}
		if !updated.Equal(link.UpdatedAt) {
			return result, fmt.Errorf("driver %s changed since review", link.DriverID)
		}
		var conflict bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM fleetscope_driver_identity WHERE company_id=$1 AND (driver_id=$2 OR fleetscope_driver_id=$3) AND (driver_id<>$2 OR fleetscope_driver_id<>$3 OR NOT confirmed)) OR EXISTS(SELECT 1 FROM fleetscope_driver_intake WHERE company_id=$1 AND completed_at IS NOT NULL AND (driver_id=$2 OR fleetscope_driver_id=$3) AND (driver_id IS DISTINCT FROM $2::uuid OR fleetscope_driver_id<>$3))`, review.CompanyID, link.DriverID, link.FleetScopeDriverID).Scan(&conflict); err != nil {
			return result, err
		}
		if conflict {
			return result, fmt.Errorf("identity conflict for driver %s; review existing enrollment", link.DriverID)
		}
		tag, err := tx.Exec(ctx, `INSERT INTO fleetscope_driver_identity(company_id,fleetscope_driver_id,driver_id,source) VALUES($1,$2,$3,'review') ON CONFLICT(company_id,fleetscope_driver_id) DO NOTHING`, review.CompanyID, link.FleetScopeDriverID, link.DriverID)
		if err != nil {
			return result, err
		}
		if tag.RowsAffected() == 1 {
			result.Linked++
		} else {
			result.Existing++
		}
		if link.TerminationID != "" {
			var date string
			var occurred time.Time
			var linked, task *string
			if err = tx.QueryRow(ctx, `SELECT termination_date::text,occurred_at,driver_id::text,task_id::text FROM fleetscope_driver_terminations WHERE id=$1 AND company_id=$2 AND fleetscope_driver_id=$3 FOR UPDATE`, link.TerminationID, review.CompanyID, link.FleetScopeDriverID).Scan(&date, &occurred, &linked, &task); err != nil {
				return result, err
			}
			if linked != nil && *linked != link.DriverID {
				return result, errors.New("termination is linked to a different driver")
			}
			if linked == nil {
				// An old held event must not terminate a later rehire.
				var rehired bool
				if err = tx.QueryRow(ctx, `SELECT coalesce(hire_date > $2::date,false) FROM drivers WHERE id=$1`, link.DriverID, date).Scan(&rehired); err != nil {
					return result, err
				}
				if rehired && status != "terminated" {
					return result, errors.New("termination predates the current hire; review employment separately")
				}
				notes := "Driver was already terminated; existing termination date and fleet state retained.\n"
				if status != "terminated" {
					notes, err = applyFleetScopeTermination(ctx, tx, link.DriverID, date)
					if err != nil {
						return result, err
					}
				}
				if _, err = tx.Exec(ctx, `UPDATE fleetscope_driver_terminations SET driver_id=$2 WHERE id=$1`, link.TerminationID, link.DriverID); err != nil {
					return result, err
				}
				if _, err = tx.Exec(ctx, `UPDATE fleetscope_driver_intake SET terminated_at=$3 WHERE company_id=$1 AND fleetscope_driver_id=$2`, review.CompanyID, link.FleetScopeDriverID, occurred); err != nil {
					return result, err
				}
				if task != nil {
					if _, err = tx.Exec(ctx, `UPDATE custom_tasks SET notes=notes || E'\n\nIDENTITY REVIEW RESOLVED: ' || $2,updated_at=now() WHERE id=$1`, *task, notes+"Clustie driver ID: "+link.DriverID+"\n"+review.Review); err != nil {
						return result, err
					}
				}
				result.Terminations++
			}
		}
	}
	if err = syncAudit(ctx, tx, "", "review_driver_identities", review); err != nil {
		return result, err
	}
	if apply {
		if err = tx.Commit(ctx); err != nil {
			return result, err
		}
		result.Applied = true
	}
	return result, nil
}
