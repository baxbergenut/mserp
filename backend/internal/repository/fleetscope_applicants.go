package repository

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"mserp/internal/fleetscope"
)

type ExpenseApplicant struct {
	ID       string `json:"id"`
	FullName string `json:"fullName"`
	Status   string `json:"status"`
}

// Applicant delivery shares the hire receipt lock and global event-ID namespace.
// Older source snapshots are acknowledged without overwriting newer ones.
func (r *FleetRepository) AcceptFleetScopeApplicant(ctx context.Context, event fleetscope.Event, hash string) (IntakeResult, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return IntakeResult{}, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('fleetscope:intake',0))`); err != nil {
		return IntakeResult{}, err
	}
	result := IntakeResult{Status: "duplicate"}
	a := event.Applicant
	if a == nil {
		return IntakeResult{}, errors.New("missing applicant")
	}
	var existingHash string
	err = tx.QueryRow(ctx, `SELECT coalesce(applicant_id::text,''),body_sha256 FROM fleetscope_webhook_receipts WHERE event_id=$1`, event.EventID).Scan(&result.ApplicantID, &existingHash)
	if err == nil {
		if hash != existingHash || result.ApplicantID == "" {
			return IntakeResult{}, ErrFleetScopeEventConflict
		}
		return result, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return IntakeResult{}, err
	}
	// Equal timestamps with different state are a source conflict, not last-writer wins.
	var conflict bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM fleetscope_applicants WHERE company_id=$1 AND fleetscope_applicant_id=$2 AND ((source_updated_at=$3 AND (full_name<>$4 OR status<>$5 OR fleetscope_driver_id IS DISTINCT FROM NULLIF($6,'')::uuid)) OR (source_updated_at<=$3 AND fleetscope_driver_id IS NOT NULL AND fleetscope_driver_id IS DISTINCT FROM NULLIF($6,'')::uuid)))`, event.CompanyID, a.ID, event.OccurredAt, a.FullName, a.Status, a.HiredDriverID).Scan(&conflict); err != nil {
		return IntakeResult{}, err
	}
	if conflict {
		return IntakeResult{}, ErrFleetScopeEventConflict
	}
	command, err := tx.Exec(ctx, `INSERT INTO fleetscope_applicants(company_id,fleetscope_applicant_id,full_name,status,source_updated_at,fleetscope_driver_id)
 VALUES($1,$2,$3,$4,$5,NULLIF($6,'')::uuid) ON CONFLICT(company_id,fleetscope_applicant_id) DO UPDATE
 SET fleetscope_driver_id=excluded.fleetscope_driver_id,full_name=excluded.full_name,status=excluded.status,source_updated_at=excluded.source_updated_at,received_at=now()
 WHERE fleetscope_applicants.source_updated_at<excluded.source_updated_at`, event.CompanyID, a.ID, a.FullName, a.Status, event.OccurredAt, a.HiredDriverID)
	if err != nil {
		return IntakeResult{}, mapApplicantConflict(err)
	}
	if command.RowsAffected() > 0 {
		result.Status = "accepted"
	}
	if err = tx.QueryRow(ctx, `SELECT id FROM fleetscope_applicants WHERE company_id=$1 AND fleetscope_applicant_id=$2`, event.CompanyID, a.ID).Scan(&result.ApplicantID); err != nil {
		return IntakeResult{}, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO fleetscope_webhook_receipts(event_id,body_sha256,applicant_id) VALUES($1,$2,$3)`, event.EventID, hash, result.ApplicantID); err != nil {
		return IntakeResult{}, err
	}
	return result, tx.Commit(ctx)
}

func (r *ExpenseRepository) ListExpenseApplicants(ctx context.Context) ([]ExpenseApplicant, error) {
	rows, err := r.pool.Query(ctx, `SELECT a.id,a.full_name,CASE WHEN EXISTS(SELECT 1 FROM fleetscope_driver_intake h WHERE h.company_id=a.company_id AND h.fleetscope_driver_id=a.fleetscope_driver_id) THEN 'hired' ELSE a.status END
 FROM fleetscope_applicants a WHERE expense_applicant_driver(a.id) IS NULL ORDER BY lower(a.full_name),a.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []ExpenseApplicant{}
	for rows.Next() {
		var item ExpenseApplicant
		if err = rows.Scan(&item.ID, &item.FullName, &item.Status); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func mapApplicantConflict(err error) error {
	var p *pgconn.PgError
	if errors.As(err, &p) && p.Code == "23505" {
		return ErrFleetScopeEventConflict
	}
	return err
}
