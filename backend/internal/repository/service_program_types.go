package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// Edit the inverse of maintenance_types.program_id in the program transaction.
// Omitted codes preserve links for older callers; an explicit [] removes them.
func saveProgramServiceTypes(ctx context.Context, tx pgx.Tx, p *ServiceProgram, actor string) error {
	if p.ServiceTypeCodes != nil {
		if len(p.ServiceTypeCodes) > 500 || len(p.ServiceTypeVersions) > 500 {
			return ErrMaintenanceInvalid
		}
		selected := map[string]bool{}
		for _, code := range p.ServiceTypeCodes {
			if code == "" || code == "oil" || selected[code] {
				return fmt.Errorf("%w: select distinct service types", ErrMaintenanceInvalid)
			}
			selected[code] = true
		}
		rows, err := tx.Query(ctx, `SELECT code,name,active,version,program_id::text FROM maintenance_types WHERE program_id=$1 OR code=ANY($2::text[]) ORDER BY code FOR UPDATE`, p.ID, p.ServiceTypeCodes)
		if err != nil {
			return err
		}
		types := []MaintenanceType{}
		for rows.Next() {
			var t MaintenanceType
			if err = rows.Scan(&t.Code, &t.Name, &t.Active, &t.Version, &t.ProgramID); err != nil {
				rows.Close()
				return err
			}
			types = append(types, t)
		}
		rows.Close()
		if err = rows.Err(); err != nil {
			return err
		}
		found := 0
		for _, t := range types {
			if p.ServiceTypeVersions[t.Code] != t.Version {
				return ErrMaintenanceConflict
			}
			if selected[t.Code] {
				found++
				if t.ProgramID != nil && *t.ProgramID != p.ID {
					return fmt.Errorf("%w: %s already resets another program; unlink it there first", ErrMaintenanceInvalid, t.Name)
				}
				if !t.Active && t.ProgramID == nil {
					return fmt.Errorf("%w: choose active service types", ErrMaintenanceInvalid)
				}
			}
		}
		if found != len(selected) {
			return fmt.Errorf("%w: choose existing service types", ErrMaintenanceInvalid)
		}
		for _, t := range types {
			var next *string
			if selected[t.Code] {
				next = &p.ID
			}
			if pmRecordID(next) == pmRecordID(t.ProgramID) {
				continue
			}
			if _, err = tx.Exec(ctx, `UPDATE maintenance_types SET program_id=$2,version=version+1 WHERE code=$1`, t.Code, next); err != nil {
				return err
			}
			if err = accessAudit(ctx, tx, actor, "service_program_type_saved", p.ID, map[string]any{"typeCode": t.Code, "before": t.ProgramID, "after": next}); err != nil {
				return err
			}
		}
	}
	return tx.QueryRow(ctx, `SELECT ARRAY(SELECT code FROM maintenance_types WHERE program_id=$1 ORDER BY code),coalesce((SELECT jsonb_object_agg(code,version) FROM maintenance_types WHERE program_id=$1),'{}'::jsonb)`, p.ID).Scan(&p.ServiceTypeCodes, &p.ServiceTypeVersions)
}
