package repository

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrFeedbackConflict = errors.New("feedback changed; reopen it and try again")
var ErrFeedbackForbidden = errors.New("only the author or an Administrator can edit this feedback; only Administrators can change its status")

type FeedbackRepository struct{ pool *pgxpool.Pool }

func NewFeedbackRepository(pool *pgxpool.Pool) *FeedbackRepository { return &FeedbackRepository{pool} }

type FeedbackPost struct {
	ID           string               `json:"id"`
	AuthorID     *string              `json:"authorId"`
	AuthorName   string               `json:"authorName"`
	Title        string               `json:"title"`
	Body         string               `json:"body"`
	Tags         []string             `json:"tags"`
	Status       string               `json:"status"`
	Version      int                  `json:"version"`
	Votes        int                  `json:"votes"`
	Voted        bool                 `json:"voted"`
	CommentCount int                  `json:"commentCount"`
	CreatedAt    time.Time            `json:"createdAt"`
	UpdatedAt    time.Time            `json:"updatedAt"`
	Attachments  []FeedbackAttachment `json:"attachments"`
}
type FeedbackAttachment struct {
	ID          string `json:"id"`
	FileName    string `json:"fileName"`
	ContentType string `json:"contentType"`
	Size        int    `json:"size"`
	Data        []byte `json:"-"`
}
type FeedbackComment struct {
	ID         string    `json:"id"`
	AuthorName string    `json:"authorName"`
	Body       string    `json:"body"`
	CreatedAt  time.Time `json:"createdAt"`
}
type FeedbackStatusChange struct {
	ActorName string    `json:"actorName"`
	OldStatus string    `json:"oldStatus"`
	NewStatus string    `json:"newStatus"`
	CreatedAt time.Time `json:"createdAt"`
}
type FeedbackDetail struct {
	FeedbackPost
	Comments []FeedbackComment      `json:"comments"`
	History  []FeedbackStatusChange `json:"history"`
}
type FeedbackInput struct {
	ID      string   `json:"id"`
	Title   string   `json:"title"`
	Body    string   `json:"body"`
	Tags    []string `json:"tags"`
	Status  string   `json:"status"`
	Version int      `json:"version"`
}
type FeedbackQuery struct {
	Pagination
	Search, Status, Tag, Sort string
}

const feedbackColumns = `p.id::text,p.author_id::text,p.author_name,p.title,p.body,p.tags,p.status,p.version,
 (SELECT count(*) FROM feedback_votes v WHERE v.post_id=p.id),
 EXISTS(SELECT 1 FROM feedback_votes v WHERE v.post_id=p.id AND v.user_id=$1::uuid),
 (SELECT count(*) FROM feedback_comments c WHERE c.post_id=p.id),p.created_at,p.updated_at`

func scanFeedback(row pgx.Row) (FeedbackPost, error) {
	var p FeedbackPost
	err := row.Scan(&p.ID, &p.AuthorID, &p.AuthorName, &p.Title, &p.Body, &p.Tags, &p.Status, &p.Version, &p.Votes, &p.Voted, &p.CommentCount, &p.CreatedAt, &p.UpdatedAt)
	p.Attachments = []FeedbackAttachment{}
	if errors.Is(err, pgx.ErrNoRows) {
		return p, ErrNotFound
	}
	return p, err
}
func (repo *FeedbackRepository) List(ctx context.Context, userID string, q FeedbackQuery) (Page[FeedbackPost], error) {
	where := ` FROM feedback_posts p WHERE $1::uuid IS NOT NULL AND ($2='' OR strpos(lower(p.title||' '||p.body),lower($2))>0) AND ($3='' OR p.status=$3) AND ($4='' OR $4=ANY(p.tags))`
	var total int
	if err := repo.pool.QueryRow(ctx, `SELECT count(*)`+where, userID, q.Search, q.Status, q.Tag).Scan(&total); err != nil {
		return Page[FeedbackPost]{}, err
	}
	q.Pagination = q.Pagination.Normalize(total)
	order := `p.created_at DESC,p.id`
	if q.Sort == "top" {
		order = `(SELECT count(*) FROM feedback_votes v WHERE v.post_id=p.id) DESC,` + order
	}
	rows, err := repo.pool.Query(ctx, `SELECT `+feedbackColumns+where+` ORDER BY `+order+` LIMIT $5 OFFSET $6`, userID, q.Search, q.Status, q.Tag, q.PageSize, q.Offset())
	if err != nil {
		return Page[FeedbackPost]{}, err
	}
	items := []FeedbackPost{}
	for rows.Next() {
		p, e := scanFeedback(rows)
		if e != nil {
			rows.Close()
			return Page[FeedbackPost]{}, e
		}
		items = append(items, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return Page[FeedbackPost]{}, err
	}
	ids := []string{}
	for _, p := range items {
		ids = append(ids, p.ID)
	}
	attachments, err := repo.pool.Query(ctx, `SELECT id::text,post_id::text,file_name,content_type,octet_length(data) FROM feedback_attachments WHERE post_id=ANY($1::uuid[]) ORDER BY created_at,id`, ids)
	if err != nil {
		return Page[FeedbackPost]{}, err
	}
	defer attachments.Close()
	for attachments.Next() {
		var a FeedbackAttachment
		var id string
		if err = attachments.Scan(&a.ID, &id, &a.FileName, &a.ContentType, &a.Size); err != nil {
			return Page[FeedbackPost]{}, err
		}
		for i := range items {
			if items[i].ID == id {
				items[i].Attachments = append(items[i].Attachments, a)
				break
			}
		}
	}
	return NewPage(items, total, q.Pagination), attachments.Err()
}
func (repo *FeedbackRepository) Detail(ctx context.Context, id, userID string) (FeedbackDetail, error) {
	p, err := scanFeedback(repo.pool.QueryRow(ctx, `SELECT `+feedbackColumns+` FROM feedback_posts p WHERE p.id=$2`, userID, id))
	d := FeedbackDetail{FeedbackPost: p, Comments: []FeedbackComment{}, History: []FeedbackStatusChange{}}
	if err != nil {
		return d, err
	}
	rows, err := repo.pool.Query(ctx, `SELECT id::text,file_name,content_type,octet_length(data) FROM feedback_attachments WHERE post_id=$1 ORDER BY created_at,id`, id)
	if err != nil {
		return d, err
	}
	for rows.Next() {
		var a FeedbackAttachment
		if err = rows.Scan(&a.ID, &a.FileName, &a.ContentType, &a.Size); err != nil {
			rows.Close()
			return d, err
		}
		d.Attachments = append(d.Attachments, a)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return d, err
	}
	rows, err = repo.pool.Query(ctx, `SELECT id::text,author_name,body,created_at FROM feedback_comments WHERE post_id=$1 ORDER BY created_at,id`, id)
	if err != nil {
		return d, err
	}
	for rows.Next() {
		var c FeedbackComment
		if err = rows.Scan(&c.ID, &c.AuthorName, &c.Body, &c.CreatedAt); err != nil {
			rows.Close()
			return d, err
		}
		d.Comments = append(d.Comments, c)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return d, err
	}
	rows, err = repo.pool.Query(ctx, `SELECT actor_name,old_status,new_status,created_at FROM feedback_status_history WHERE post_id=$1 ORDER BY created_at,id`, id)
	if err != nil {
		return d, err
	}
	defer rows.Close()
	for rows.Next() {
		var h FeedbackStatusChange
		if err = rows.Scan(&h.ActorName, &h.OldStatus, &h.NewStatus, &h.CreatedAt); err != nil {
			return d, err
		}
		d.History = append(d.History, h)
	}
	return d, rows.Err()
}
func (repo *FeedbackRepository) Create(ctx context.Context, in FeedbackInput, user AuthUser, hash string, files []FeedbackAttachment) (string, error) {
	tx, err := repo.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	result, err := tx.Exec(ctx, `INSERT INTO feedback_posts(id,author_id,author_name,title,body,tags,submission_hash) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(id) DO NOTHING`, in.ID, user.ID, user.Username, in.Title, in.Body, in.Tags, hash)
	if err != nil {
		return "", err
	}
	if result.RowsAffected() == 0 {
		var matches bool
		err = tx.QueryRow(ctx, `SELECT author_id=$2 AND submission_hash=$3 FROM feedback_posts WHERE id=$1`, in.ID, user.ID, hash).Scan(&matches)
		if err != nil {
			return "", err
		}
		if !matches {
			return "", ErrFeedbackConflict
		}
	} else {
		for _, a := range files {
			if _, err = tx.Exec(ctx, `INSERT INTO feedback_attachments(post_id,file_name,content_type,data) VALUES($1,$2,$3,$4)`, in.ID, a.FileName, a.ContentType, a.Data); err != nil {
				return "", err
			}
		}
	}
	return in.ID, tx.Commit(ctx)
}
func (repo *FeedbackRepository) Update(ctx context.Context, id string, in FeedbackInput, user AuthUser) error {
	tx, err := repo.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var author *string
	var status string
	var version int
	err = tx.QueryRow(ctx, `SELECT author_id::text,status,version FROM feedback_posts WHERE id=$1 FOR UPDATE`, id).Scan(&author, &status, &version)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if !user.Administrator && (author == nil || *author != user.ID || status != in.Status) {
		return ErrFeedbackForbidden
	}
	if version != in.Version {
		return ErrFeedbackConflict
	}
	if _, err = tx.Exec(ctx, `UPDATE feedback_posts SET title=$2,body=$3,tags=$4,status=$5,version=version+1,updated_at=now() WHERE id=$1`, id, in.Title, in.Body, in.Tags, in.Status); err != nil {
		return err
	}
	if status != in.Status {
		if _, err = tx.Exec(ctx, `INSERT INTO feedback_status_history(post_id,actor_name,old_status,new_status) VALUES($1,$2,$3,$4)`, id, user.Username, status, in.Status); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
func (repo *FeedbackRepository) Vote(ctx context.Context, id, userID string, voted bool) error {
	// Serialize this user's explicit desired vote on the post; repeated PUTs never toggle.
	tx, err := repo.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var found string
	err = tx.QueryRow(ctx, `SELECT id::text FROM feedback_posts WHERE id=$1 FOR UPDATE`, id).Scan(&found)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if voted {
		_, err = tx.Exec(ctx, `INSERT INTO feedback_votes(post_id,user_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, id, userID)
	} else {
		_, err = tx.Exec(ctx, `DELETE FROM feedback_votes WHERE post_id=$1 AND user_id=$2`, id, userID)
	}
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (repo *FeedbackRepository) Comment(ctx context.Context, postID, id, body string, user AuthUser) error {
	result, err := repo.pool.Exec(ctx, `INSERT INTO feedback_comments(id,post_id,author_id,author_name,body) SELECT $1,id,$3,$4,$5 FROM feedback_posts WHERE id=$2 ON CONFLICT(id) DO NOTHING`, id, postID, user.ID, user.Username, body)
	if err != nil {
		return err
	}
	if result.RowsAffected() > 0 {
		return nil
	}
	var same bool
	err = repo.pool.QueryRow(ctx, `SELECT post_id=$2 AND author_id=$3 AND body=$4 FROM feedback_comments WHERE id=$1`, id, postID, user.ID, body).Scan(&same)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if !same {
		return ErrFeedbackConflict
	}
	return nil
}
func (repo *FeedbackRepository) Attachment(ctx context.Context, id string) (FeedbackAttachment, error) {
	var a FeedbackAttachment
	err := repo.pool.QueryRow(ctx, `SELECT id::text,file_name,content_type,data FROM feedback_attachments WHERE id=$1`, id).Scan(&a.ID, &a.FileName, &a.ContentType, &a.Data)
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrNotFound
	}
	return a, err
}
func (repo *FeedbackRepository) Tags(ctx context.Context) ([]string, error) {
	rows, err := repo.pool.Query(ctx, `SELECT DISTINCT unnest(tags) AS tag FROM feedback_posts ORDER BY tag`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tags := []string{}
	for rows.Next() {
		var tag string
		if err = rows.Scan(&tag); err != nil {
			return nil, err
		}
		tags = append(tags, tag)
	}
	return tags, rows.Err()
}
