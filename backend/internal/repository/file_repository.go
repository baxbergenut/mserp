package repository

import (
	"context"
	"crypto/sha256"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// StoredFileMetadata is safe to include in normal API responses. File data is
// only loaded by GetFile when a caller explicitly downloads it.
type StoredFileMetadata struct {
	ID          string    `json:"id"`
	FileName    string    `json:"fileName"`
	ContentType string    `json:"contentType"`
	SizeBytes   int64     `json:"sizeBytes"`
	SHA256      string    `json:"sha256"`
	CreatedAt   time.Time `json:"createdAt"`
}

type StoredFile struct {
	StoredFileMetadata
	Data []byte `json:"-"`
}

type FileRepository struct {
	pool *pgxpool.Pool
}

func NewFileRepository(pool *pgxpool.Pool) *FileRepository {
	return &FileRepository{pool: pool}
}

func (r *FileRepository) CreateFile(
	ctx context.Context,
	fileName string,
	contentType string,
	data []byte,
	actor ...string,
) (StoredFileMetadata, error) {
	var uploader *string
	if len(actor) > 0 && actor[0] != "" {
		uploader = &actor[0]
	}
	digest := sha256.Sum256(data)
	var value StoredFileMetadata
	err := r.pool.QueryRow(ctx, `
		INSERT INTO files (file_name, content_type, size_bytes, sha256, data, uploaded_by)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, file_name, content_type, size_bytes, sha256, created_at`,
		fileName, contentType, len(data), fmt.Sprintf("%x", digest), data, uploader,
	).Scan(
		&value.ID, &value.FileName, &value.ContentType, &value.SizeBytes,
		&value.SHA256, &value.CreatedAt,
	)
	return value, err
}

func (r *FileRepository) GetFile(ctx context.Context, id string) (StoredFile, error) {
	var value StoredFile
	err := r.pool.QueryRow(ctx, `
		SELECT id, file_name, content_type, size_bytes, sha256, created_at, data
		FROM files
		WHERE id = $1`, id,
	).Scan(
		&value.ID, &value.FileName, &value.ContentType, &value.SizeBytes,
		&value.SHA256, &value.CreatedAt, &value.Data,
	)
	if err == pgx.ErrNoRows {
		return StoredFile{}, ErrNotFound
	}
	return value, err
}

func (r *FileRepository) CanAccessFile(ctx context.Context, id string, user AuthUser, kind string) (bool, error) {
	var allowed bool
	err := r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM files f WHERE f.id=$1 AND (
 ($2 AND ($5='' OR $5='drivers') AND EXISTS(SELECT 1 FROM drivers d WHERE d.cdl_file_id=f.id)) OR
 ($3 AND ($5='' OR $5='trucks') AND EXISTS(SELECT 1 FROM trucks t WHERE t.irp_file_id=f.id)) OR
 ((f.uploaded_by=$4::uuid OR $6) AND NOT EXISTS(SELECT 1 FROM drivers d WHERE d.cdl_file_id=f.id) AND NOT EXISTS(SELECT 1 FROM trucks t WHERE t.irp_file_id=f.id))
 ))`, id, HasPermission(user, "drivers.read") && HasPermission(user, "drivers.documents.read"), HasPermission(user, "trucks.read") && HasPermission(user, "trucks.documents.read"), user.ID, kind, user.Administrator).Scan(&allowed)
	return allowed, err
}
func (r *FleetRepository) CanAttachFile(ctx context.Context, id string, user AuthUser, kind string) (bool, error) {
	return NewFileRepository(r.pool).CanAccessFile(ctx, id, user, kind)
}
