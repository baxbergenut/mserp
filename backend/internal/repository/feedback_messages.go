package repository

import (
	"context"
	"time"
)

type FeedbackMessage struct {
	ID          string     `json:"id"`
	PostID      string     `json:"postId"`
	Title       string     `json:"title"`
	CompletedBy string     `json:"completedBy"`
	CreatedAt   time.Time  `json:"createdAt"`
	ReadAt      *time.Time `json:"readAt"`
}

func (repo *FeedbackRepository) Messages(ctx context.Context, recipientID string, pagination Pagination) (Page[FeedbackMessage], error) {
	var total int
	if err := repo.pool.QueryRow(ctx, `SELECT count(*) FROM feedback_messages WHERE recipient_id=$1`, recipientID).Scan(&total); err != nil {
		return Page[FeedbackMessage]{}, err
	}
	pagination = pagination.Normalize(total)
	rows, err := repo.pool.Query(ctx, `SELECT id::text,post_id::text,title,completed_by,created_at,read_at FROM feedback_messages WHERE recipient_id=$1 ORDER BY created_at DESC,id LIMIT $2 OFFSET $3`, recipientID, pagination.PageSize, pagination.Offset())
	if err != nil {
		return Page[FeedbackMessage]{}, err
	}
	defer rows.Close()
	items := []FeedbackMessage{}
	for rows.Next() {
		var m FeedbackMessage
		if err = rows.Scan(&m.ID, &m.PostID, &m.Title, &m.CompletedBy, &m.CreatedAt, &m.ReadAt); err != nil {
			return Page[FeedbackMessage]{}, err
		}
		items = append(items, m)
	}
	return NewPage(items, total, pagination), rows.Err()
}
func (repo *FeedbackRepository) UnreadMessageCount(ctx context.Context, recipientID string) (int, error) {
	var count int
	err := repo.pool.QueryRow(ctx, `SELECT count(*) FROM feedback_messages WHERE recipient_id=$1 AND read_at IS NULL`, recipientID).Scan(&count)
	return count, err
}
func (repo *FeedbackRepository) ReadMessage(ctx context.Context, id, recipientID string) error {
	result, err := repo.pool.Exec(ctx, `UPDATE feedback_messages SET read_at=coalesce(read_at,now()) WHERE id=$1 AND recipient_id=$2`, id, recipientID)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
