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
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	Notes       string     `json:"notes"`
	CompletedAt *time.Time `json:"completedAt"`
	CreatedBy   *string    `json:"createdBy"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
}

type CustomTaskInput struct {
	Title string `json:"title"`
	Notes string `json:"notes"`
}

const customTaskColumns = `id::text, title, notes, completed_at, created_by::text, created_at, updated_at`

func scanCustomTask(row pgx.Row) (CustomTask, error) {
	var task CustomTask
	err := row.Scan(&task.ID, &task.Title, &task.Notes, &task.CompletedAt, &task.CreatedBy, &task.CreatedAt, &task.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return task, ErrNotFound
	}
	return task, err
}

func (repo *CustomTaskRepository) List(ctx context.Context, pagination Pagination, search, status string) (Page[CustomTask], error) {
	const where = ` WHERE ($1 = '' OR strpos(lower(title || ' ' || notes), lower($1)) > 0)
 AND ($2 = 'all' OR ($2 = 'open' AND completed_at IS NULL) OR ($2 = 'completed' AND completed_at IS NOT NULL))`
	var total int
	if err := repo.pool.QueryRow(ctx, `SELECT count(*) FROM custom_tasks`+where, search, status).Scan(&total); err != nil {
		return Page[CustomTask]{}, err
	}
	pagination = pagination.Normalize(total)
	rows, err := repo.pool.Query(ctx, `SELECT `+customTaskColumns+` FROM custom_tasks`+where+`
 ORDER BY (completed_at IS NOT NULL), created_at DESC, id LIMIT $3 OFFSET $4`, search, status, pagination.PageSize, pagination.Offset())
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
	return scanCustomTask(repo.pool.QueryRow(ctx, `INSERT INTO custom_tasks(title, notes, created_by)
 VALUES ($1, $2, $3) RETURNING `+customTaskColumns, input.Title, input.Notes, userID))
}

func (repo *CustomTaskRepository) Update(ctx context.Context, id string, input CustomTaskInput) (CustomTask, error) {
	return scanCustomTask(repo.pool.QueryRow(ctx, `UPDATE custom_tasks SET title=$2, notes=$3, updated_at=now()
 WHERE id=$1 RETURNING `+customTaskColumns, id, input.Title, input.Notes))
}

func (repo *CustomTaskRepository) SetCompleted(ctx context.Context, id string, completed bool) (CustomTask, error) {
	return scanCustomTask(repo.pool.QueryRow(ctx, `UPDATE custom_tasks
 SET completed_at=CASE WHEN $2 THEN COALESCE(completed_at, now()) ELSE NULL END, updated_at=now()
 WHERE id=$1 RETURNING `+customTaskColumns, id, completed))
}

func (repo *CustomTaskRepository) Delete(ctx context.Context, id string) error {
	result, err := repo.pool.Exec(ctx, `DELETE FROM custom_tasks WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
