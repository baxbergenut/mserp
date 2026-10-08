package repository

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"
)

type taskViewerKey struct{}
type taskViewer struct {
	userID string
	admin  bool
}

// HTTP requests always set the authenticated viewer. Background integration
// operations and existing repository callers are trusted and have no viewer.
func WithTaskViewer(ctx context.Context, userID string, admin bool) context.Context {
	return context.WithValue(ctx, taskViewerKey{}, taskViewer{userID, admin})
}

func taskViewerArgs(ctx context.Context) (bool, string) {
	viewer, ok := ctx.Value(taskViewerKey{}).(taskViewer)
	if !ok {
		return true, ""
	}
	return viewer.admin, viewer.userID
}

func customTaskVisibility(adminParam, userParam int) string {
	return fmt.Sprintf(` AND ($%d::boolean OR
      (system_task_kind IS NULL AND (assigned_to IS NULL OR assigned_to=NULLIF($%d,'')::uuid OR assigned_by=NULLIF($%d,'')::uuid)) OR
      (system_task_kind IS NOT NULL AND EXISTS(SELECT 1 FROM system_task_assignments a JOIN app_users u ON (u.id=a.assignee_id OR u.id=ANY(a.assignee_ids))
         WHERE a.kind=system_task_kind AND u.active AND u.id=NULLIF($%d,'')::uuid)))`, adminParam, userParam, userParam, userParam)
}

func systemTaskVisible(ctx context.Context, q chargeQuery, kind string, lock bool) (bool, error) {
	admin, userID := taskViewerArgs(ctx)
	if admin {
		return true, nil
	}
	suffix := ""
	if lock {
		suffix = " FOR SHARE OF a"
	}
	var visible bool
	err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM app_users u WHERE u.active AND u.id=NULLIF($2,'')::uuid
 AND (u.id=a.assignee_id OR u.id=ANY(a.assignee_ids))) FROM system_task_assignments a WHERE a.kind=$1`+suffix, kind, userID).Scan(&visible)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return err == nil && visible, err
}

type SystemTaskAssignment struct {
	AssigneeIDs []string `json:"assigneeIds"`
	Kind        string   `json:"kind"`
	AssigneeID  *string  `json:"assigneeId"`
	Version     int      `json:"version"`
}
type TaskUser struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (r *AuthRepository) SystemTaskAssignments(ctx context.Context) ([]SystemTaskAssignment, error) {
	rows, err := r.pool.Query(ctx, `SELECT kind,assignee_id::text,version,assignee_ids::text[] FROM system_task_assignments ORDER BY kind`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []SystemTaskAssignment{}
	for rows.Next() {
		var item SystemTaskAssignment
		if err = rows.Scan(&item.Kind, &item.AssigneeID, &item.Version, &item.AssigneeIDs); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (r *AuthRepository) SaveSystemTaskAssignment(ctx context.Context, actor string, input SystemTaskAssignment) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var before SystemTaskAssignment
	var id string
	before.Kind = input.Kind
	if err = tx.QueryRow(ctx, `SELECT id::text,assignee_id::text,version,assignee_ids::text[] FROM system_task_assignments WHERE kind=$1 FOR UPDATE`, input.Kind).Scan(&id, &before.AssigneeID, &before.Version, &before.AssigneeIDs); err != nil {
		return mapNotFound(err)
	}
	if before.Version != input.Version {
		return ErrAccessConflict
	}
	if input.AssigneeIDs == nil {
		input.AssigneeIDs = []string{}
		if input.AssigneeID != nil {
			input.AssigneeIDs = append(input.AssigneeIDs, *input.AssigneeID)
		}
	}
	slices.Sort(input.AssigneeIDs)
	input.AssigneeIDs = slices.Compact(input.AssigneeIDs)
	input.AssigneeID = nil
	if len(input.AssigneeIDs) > 0 {
		input.AssigneeID = &input.AssigneeIDs[0]
	}
	for _, assignee := range input.AssigneeIDs {
		var id string
		if err = tx.QueryRow(ctx, `SELECT id::text FROM app_users WHERE id=$1 AND active FOR SHARE`, assignee).Scan(&id); err != nil {
			return ErrAccessConflict
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE system_task_assignments SET assignee_id=$2,version=version+1,updated_at=now(),updated_by=$3,assignee_ids=$4::uuid[] WHERE kind=$1`, input.Kind, input.AssigneeID, actor, input.AssigneeIDs); err != nil {
		return err
	}
	input.Version++
	if err = accessAudit(ctx, tx, actor, "system_task_assigned", id, map[string]any{"before": before, "after": input}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *AuthRepository) TaskUsers(ctx context.Context) ([]TaskUser, error) {
	rows, err := r.pool.Query(ctx, `SELECT id::text,username FROM app_users WHERE active AND role_id IS NOT NULL ORDER BY lower(username),id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []TaskUser{}
	for rows.Next() {
		var item TaskUser
		if err = rows.Scan(&item.ID, &item.Name); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}
