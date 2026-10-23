package repository

import (
	"context"
	"github.com/jackc/pgx/v5"
)

func createDispatcherAccount(ctx context.Context, tx pgx.Tx, input DispatcherInput) (string, error) {
	if err := accessLock(ctx, tx, input.AccountActor); err != nil {
		return "", err
	}
	var id string
	err := tx.QueryRow(ctx, `INSERT INTO app_users(username,email,role_id,active,password_hash) SELECT $1,$2,id,$3,$4 FROM app_roles WHERE dispatcher_role RETURNING id::text`, formatPersonName(input.FullName), input.Email, input.Active, input.AccountPasswordHash).Scan(&id)
	if err != nil {
		return "", err
	}
	// Neither plaintext nor password hashes belong in the audit payload.
	err = accessAudit(ctx, tx, input.AccountActor, "create_dispatcher_account", id, map[string]any{"name": formatPersonName(input.FullName), "email": input.Email, "active": input.Active})
	return id, err
}
