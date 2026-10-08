package repository

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

type BoardTask struct {
	CustomTask
	Outcome string `json:"outcome"`
}

// Apply assignment privacy before pagination/counting, and preserve the existing
// fleet.read boundary for onboarding details. Completed records remain durable.
const boardTaskCTE = `WITH tasks AS (
 SELECT c.id,c.title,c.notes,c.completed_at,c.created_by,c.created_at,c.updated_at,
 c.assigned_to,c.assigned_by,c.system_task_kind,c.completed_by_name,''::text AS outcome,c.in_process
 FROM custom_tasks c
 UNION ALL
 SELECT s.id,s.title,s.notes,s.completed_at,NULL::uuid,s.created_at,s.created_at,
 NULL::uuid,NULL::uuid,s.kind,s.completed_by_name,s.outcome,false FROM system_task_records s
 WHERE s.kind<>'escrow_release' OR s.completed_at IS NOT NULL OR EXISTS(
 SELECT 1 FROM escrow_release_reviews e WHERE e.id=s.id AND e.due_date<=(now() AT TIME ZONE 'America/New_York')::date)
), visible AS (
 SELECT t.*,CASE WHEN t.system_task_kind IS NULL THEN coalesce(u.username,'') ELSE coalesce((SELECT string_agg(au.username, ', ' ORDER BY au.username) FROM app_users au WHERE au.id=a.assignee_id OR au.id=ANY(a.assignee_ids)),'') END AS assignee_name,
 CASE WHEN t.system_task_kind IS NOT NULL THEN 'System' ELSE coalesce(b.username,'') END AS assigner_name,
 CASE WHEN t.system_task_kind IS NOT NULL THEN a.assignee_id ELSE t.assigned_to END AS effective_assignee
 FROM tasks t
 LEFT JOIN system_task_assignments a ON a.kind=t.system_task_kind
 LEFT JOIN app_users u ON u.id=CASE WHEN t.system_task_kind IS NOT NULL THEN a.assignee_id ELSE t.assigned_to END
 LEFT JOIN app_users b ON b.id=coalesce(t.assigned_by,t.created_by)
 WHERE ($1::boolean OR
 (t.system_task_kind IS NULL AND (t.assigned_to IS NULL OR t.assigned_to=NULLIF($2,'')::uuid OR t.assigned_by=NULLIF($2,'')::uuid)) OR
 (t.system_task_kind IS NOT NULL AND EXISTS(SELECT 1 FROM app_users au WHERE au.active AND au.id=NULLIF($2,'')::uuid AND (au.id=a.assignee_id OR au.id=ANY(a.assignee_ids)))))
 AND (t.system_task_kind IS DISTINCT FROM 'driver_onboarding' OR $3::boolean)
) `

func (repo *CustomTaskRepository) Board(ctx context.Context, pagination Pagination, search, status string, fleetRead bool) (Page[BoardTask], error) {
	admin, user := taskViewerArgs(ctx)
	tx, err := repo.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return Page[BoardTask]{}, err
	}
	defer tx.Rollback(ctx)
	where := ` WHERE ($4='' OR strpos(lower(concat_ws(' ',title,notes,assignee_name,assigner_name,completed_by_name)),lower($4))>0)
 AND ($5='all' OR ($5='open' AND completed_at IS NULL AND NOT in_process) OR ($5='in_process' AND completed_at IS NULL AND in_process) OR ($5='completed' AND completed_at IS NOT NULL))`
	var total int
	if err = tx.QueryRow(ctx, boardTaskCTE+`SELECT count(*) FROM visible`+where, admin, user, fleetRead, search, status).Scan(&total); err != nil {
		return Page[BoardTask]{}, err
	}
	pagination = pagination.Normalize(total)
	rows, err := tx.Query(ctx, boardTaskCTE+`SELECT id::text,title,notes,completed_at,created_by::text,created_at,updated_at,effective_assignee::text,assigned_by::text,assignee_name,system_task_kind,assigner_name,completed_by_name,outcome,CASE WHEN completed_at IS NOT NULL THEN 'completed' WHEN in_process THEN 'in_process' ELSE 'open' END FROM visible`+where+` ORDER BY (completed_at IS NOT NULL),coalesce(completed_at,created_at) DESC,id LIMIT $6 OFFSET $7`, admin, user, fleetRead, search, status, pagination.PageSize, pagination.Offset())
	if err != nil {
		return Page[BoardTask]{}, err
	}
	defer rows.Close()
	items := []BoardTask{}
	for rows.Next() {
		var t BoardTask
		if err = rows.Scan(&t.ID, &t.Title, &t.Notes, &t.CompletedAt, &t.CreatedBy, &t.CreatedAt, &t.UpdatedAt, &t.AssignedTo, &t.AssignedBy, &t.AssigneeName, &t.SystemTaskKind, &t.AssignerName, &t.CompletedByName, &t.Outcome, &t.Status); err != nil {
			return Page[BoardTask]{}, err
		}
		items = append(items, t)
	}
	return NewPage(items, total, pagination), rows.Err()
}

func (repo *CustomTaskRepository) OpenCount(ctx context.Context, fleetRead bool) (int, error) {
	admin, user := taskViewerArgs(ctx)
	var count int
	err := repo.pool.QueryRow(ctx, boardTaskCTE+`SELECT count(*) FROM visible WHERE completed_at IS NULL`, admin, user, fleetRead).Scan(&count)
	return count, err
}

// Offboarding is a source workflow, never a generic board status update.
// Its checklist confirmation is retained with the authenticated actor.
func (repo *CustomTaskRepository) ConfirmOffboarding(ctx context.Context, id string) error {
	admin, user := taskViewerArgs(ctx)
	result, err := repo.pool.Exec(ctx, `UPDATE custom_tasks SET completed_at=now(),completed_by=NULLIF($3,'')::uuid,
 completed_by_name=coalesce((SELECT username FROM app_users WHERE id=NULLIF($3,'')::uuid),''),updated_at=$4
 WHERE id=$1 AND system_task_kind='driver_offboarding' AND completed_at IS NULL`+customTaskVisibility(2, 3), id, admin, user, time.Now())
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
