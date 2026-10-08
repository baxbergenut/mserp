package repository

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type CustomTaskRepository struct{ pool *pgxpool.Pool }

func NewCustomTaskRepository(pool *pgxpool.Pool) *CustomTaskRepository {
	return &CustomTaskRepository{pool: pool}
}

type CustomTask struct {
	ID              string     `json:"id"`
	Title           string     `json:"title"`
	Notes           string     `json:"notes"`
	Status          string     `json:"status"`
	CompletedAt     *time.Time `json:"completedAt"`
	CreatedBy       *string    `json:"createdBy"`
	AssignedTo      *string    `json:"assignedTo"`
	AssignedBy      *string    `json:"assignedBy"`
	AssigneeName    string     `json:"assigneeName"`
	AssignerName    string     `json:"assignerName"`
	CompletedByName string     `json:"completedByName"`
	SystemTaskKind  *string    `json:"systemTaskKind"`
	CreatedAt       time.Time  `json:"createdAt"`
	UpdatedAt       time.Time  `json:"updatedAt"`
}

type CustomTaskInput struct {
	Title      string  `json:"title"`
	Notes      string  `json:"notes"`
	AssignedTo *string `json:"assignedTo"`
}

const customTaskColumns = `id::text, title, notes, completed_at, created_by::text, created_at, updated_at,
 assigned_to::text, assigned_by::text, coalesce((SELECT username FROM app_users WHERE id=custom_tasks.assigned_to),''), system_task_kind,
 coalesce((SELECT username FROM app_users WHERE id=coalesce(custom_tasks.assigned_by,custom_tasks.created_by)),''), completed_by_name,
 CASE WHEN completed_at IS NOT NULL THEN 'completed' WHEN in_process THEN 'in_process' ELSE 'open' END`

func scanCustomTask(row pgx.Row) (CustomTask, error) {
	var task CustomTask
	err := row.Scan(&task.ID, &task.Title, &task.Notes, &task.CompletedAt, &task.CreatedBy, &task.CreatedAt, &task.UpdatedAt, &task.AssignedTo, &task.AssignedBy, &task.AssigneeName, &task.SystemTaskKind, &task.AssignerName, &task.CompletedByName, &task.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return task, ErrNotFound
	}
	return task, err
}

func (repo *CustomTaskRepository) List(ctx context.Context, pagination Pagination, search, status string) (Page[CustomTask], error) {
	where := ` WHERE ($1 = '' OR strpos(lower(title || ' ' || notes), lower($1)) > 0)
 AND ($2 = 'all' OR ($2 = 'open' AND completed_at IS NULL AND NOT in_process) OR ($2 = 'in_process' AND completed_at IS NULL AND in_process) OR ($2 = 'completed' AND completed_at IS NOT NULL))` + customTaskVisibility(3, 4)
	admin, userID := taskViewerArgs(ctx)
	var total int
	if err := repo.pool.QueryRow(ctx, `SELECT count(*) FROM custom_tasks`+where, search, status, admin, userID).Scan(&total); err != nil {
		return Page[CustomTask]{}, err
	}
	pagination = pagination.Normalize(total)
	rows, err := repo.pool.Query(ctx, `SELECT `+customTaskColumns+` FROM custom_tasks`+where+`
 ORDER BY (completed_at IS NOT NULL), created_at DESC, id LIMIT $5 OFFSET $6`, search, status, admin, userID, pagination.PageSize, pagination.Offset())
	if err != nil {
		return Page[CustomTask]{}, err
	}
	defer rows.Close()
	items := make([]CustomTask, 0)
	for rows.Next() {
		task, err := scanCustomTask(rows)
		if err != nil {
			return Page[CustomTask]{}, err
		}
		items = append(items, task)
	}
	return NewPage(items, total, pagination), rows.Err()
}

func (repo *CustomTaskRepository) Create(ctx context.Context, input CustomTaskInput, userID string) (CustomTask, error) {
	return scanCustomTask(repo.pool.QueryRow(ctx, `INSERT INTO custom_tasks(title, notes, created_by,assigned_to,assigned_by)
 SELECT $1,$2,$3,NULLIF($4,'')::uuid,CASE WHEN NULLIF($4,'') IS NOT NULL THEN $3::uuid END
 WHERE NULLIF($4,'') IS NULL OR EXISTS(SELECT 1 FROM app_users WHERE id=NULLIF($4,'')::uuid AND active)
 RETURNING `+customTaskColumns, input.Title, input.Notes, userID, input.AssignedTo))
}

func (repo *CustomTaskRepository) Update(ctx context.Context, id string, input CustomTaskInput) (CustomTask, error) {
	admin, userID := taskViewerArgs(ctx)
	return scanCustomTask(repo.pool.QueryRow(ctx, `UPDATE custom_tasks SET title=$2, notes=$3, updated_at=now(),
 assigned_to=CASE WHEN $4::text IS NULL THEN assigned_to ELSE NULLIF($4,'')::uuid END,
 assigned_by=CASE WHEN $4::text IS NOT NULL AND assigned_to IS DISTINCT FROM NULLIF($4,'')::uuid THEN NULLIF($6,'')::uuid ELSE assigned_by END
 WHERE id=$1 AND system_task_kind IS NULL`+customTaskVisibility(5, 6)+`
 AND ($4::text IS NULL OR (system_task_kind IS NULL AND (NULLIF($4,'') IS NULL OR assigned_to=NULLIF($4,'')::uuid OR EXISTS(SELECT 1 FROM app_users WHERE id=NULLIF($4,'')::uuid AND active))))
 RETURNING `+customTaskColumns, id, input.Title, input.Notes, input.AssignedTo, admin, userID))
}

func (repo *CustomTaskRepository) SetCompleted(ctx context.Context, id string, completed bool) (CustomTask, error) {
	status := "open"
	if completed {
		status = "completed"
	}
	return repo.SetStatus(ctx, id, status)
}

func (repo *CustomTaskRepository) SetStatus(ctx context.Context, id, status string) (CustomTask, error) {
	if status != "open" && status != "in_process" && status != "completed" {
		return CustomTask{}, errors.New("invalid task status")
	}
	admin, userID := taskViewerArgs(ctx)
	return scanCustomTask(repo.pool.QueryRow(ctx, `UPDATE custom_tasks
 SET in_process=($2='in_process'), completed_at=CASE WHEN $2='completed' THEN COALESCE(completed_at, now()) ELSE NULL END,
 completed_by=CASE WHEN $2<>'completed' THEN NULL WHEN completed_at IS NULL THEN NULLIF($4,'')::uuid ELSE completed_by END,
 completed_by_name=CASE WHEN $2<>'completed' THEN '' WHEN completed_at IS NULL THEN coalesce((SELECT username FROM app_users WHERE id=NULLIF($4,'')::uuid),'') ELSE completed_by_name END,
 updated_at=now()
 WHERE id=$1 AND system_task_kind IS NULL`+customTaskVisibility(3, 4)+` RETURNING `+customTaskColumns, id, status, admin, userID))
}

func (repo *CustomTaskRepository) Delete(ctx context.Context, id string) error {
	admin, userID := taskViewerArgs(ctx)
	result, err := repo.pool.Exec(ctx, `DELETE FROM custom_tasks WHERE id=$1 AND system_task_kind IS NULL`+customTaskVisibility(2, 3), id, admin, userID)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
