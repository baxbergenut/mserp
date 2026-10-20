package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
)

type ExpenseDocument = MaintenanceDocument

func (r *ExpenseRepository) ExpenseDocumentAccess(ctx context.Context, id string) (category, creator string, err error) {
	err = r.pool.QueryRow(ctx, `SELECT category_id,coalesce(created_by::text,'') FROM expenses WHERE id=$1`, id).Scan(&category, &creator)
	return category, creator, mapNotFound(err)
}

func (r *ExpenseRepository) ExpenseDocuments(ctx context.Context, id string) ([]ExpenseDocument, error) {
	rows, err := r.pool.Query(ctx, `SELECT id,file_name,content_type,size_bytes,created_at FROM expense_documents WHERE expense_id=$1 ORDER BY created_at,id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := []ExpenseDocument{}
	for rows.Next() {
		var v ExpenseDocument
		if err := rows.Scan(&v.ID, &v.FileName, &v.ContentType, &v.SizeBytes, &v.CreatedAt); err != nil {
			return nil, err
		}
		values = append(values, v)
	}
	return values, rows.Err()
}

func (r *ExpenseRepository) CreateExpenseDocument(ctx context.Context, expense, category, name, kind, actor string, data []byte) (ExpenseDocument, error) {
	sum := sha256.Sum256(data)
	var v ExpenseDocument
	err := r.pool.QueryRow(ctx, `INSERT INTO expense_documents(expense_id,file_name,content_type,size_bytes,sha256,data,uploaded_by)
        SELECT id,$3,$4,$5,$6,$7,$8 FROM expenses WHERE id=$1 AND category_id=$2
        ON CONFLICT(expense_id,sha256,file_name) DO UPDATE SET file_name=excluded.file_name
        RETURNING id,file_name,content_type,size_bytes,created_at`, expense, category, name, kind, len(data), hex.EncodeToString(sum[:]), data, actor).Scan(&v.ID, &v.FileName, &v.ContentType, &v.SizeBytes, &v.CreatedAt)
	return v, mapNotFound(err)
}

func (r *ExpenseRepository) ExpenseDocumentCategory(ctx context.Context, id string) (string, error) {
	var category string
	err := r.pool.QueryRow(ctx, `SELECT e.category_id FROM expense_documents d JOIN expenses e ON e.id=d.expense_id WHERE d.id=$1`, id).Scan(&category)
	return category, mapNotFound(err)
}

func (r *ExpenseRepository) ExpenseDocument(ctx context.Context, id, category string) (ExpenseDocument, []byte, error) {
	var v ExpenseDocument
	var data []byte
	err := r.pool.QueryRow(ctx, `SELECT d.id,d.file_name,d.content_type,d.size_bytes,d.created_at,d.data FROM expense_documents d JOIN expenses e ON e.id=d.expense_id WHERE d.id=$1 AND e.category_id=$2`, id, category).Scan(&v.ID, &v.FileName, &v.ContentType, &v.SizeBytes, &v.CreatedAt, &data)
	return v, data, mapNotFound(err)
}
