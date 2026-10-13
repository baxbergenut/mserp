package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

func (r *AuthRepository) UpdaterExtensionAccess(ctx context.Context, id string) (DispatcherExtensionAccess, error) {
	a := DispatcherExtensionAccess{UpdaterID: id}
	err := r.pool.QueryRow(ctx, `SELECT coalesce(a.enabled,false),coalesce(a.account_label,''),coalesce(a.version,0),a.activated_at,a.token_expires_at,a.activation_expires_at
 FROM updaters u LEFT JOIN updater_extension_access a ON a.updater_id=u.id WHERE u.id=$1`, id).Scan(&a.Enabled, &a.AccountLabel, &a.Version, &a.ActivatedAt, &a.TokenExpiresAt, &a.ActivationExpiresAt)
	return a, err
}

// Updaters have no active flag. Explicit revocation or deleting the updater
// removes access; assignment and shift changes do not change their identity.
func (r *AuthRepository) SetUpdaterExtensionAccess(ctx context.Context, id, actor, label, codeHash string, version int, enabled bool) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = accessLock(ctx, tx, actor); err != nil {
		return err
	}
	var profileID string
	if err = tx.QueryRow(ctx, `SELECT id FROM updaters WHERE id=$1 FOR UPDATE`, id).Scan(&profileID); err != nil {
		return err
	}
	var current int
	err = tx.QueryRow(ctx, `SELECT version FROM updater_extension_access WHERE updater_id=$1 FOR UPDATE`, id).Scan(&current)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if current != version {
		return ErrAccessConflict
	}
	_, err = tx.Exec(ctx, `INSERT INTO updater_extension_access(updater_id,enabled,account_label,activation_hash,activation_expires_at,updated_by)
 VALUES($1,$2,$3,nullif($4,''),CASE WHEN $2 THEN now()+interval '10 minutes' END,$5)
 ON CONFLICT(updater_id) DO UPDATE SET enabled=excluded.enabled,account_label=excluded.account_label,
 activation_hash=excluded.activation_hash,activation_expires_at=excluded.activation_expires_at,
 token_hash=NULL,token_expires_at=NULL,activated_at=NULL,version=updater_extension_access.version+1,updated_at=now(),updated_by=excluded.updated_by`, id, enabled, label, codeHash, actor)
	if err != nil {
		return err
	}
	if err = accessAudit(ctx, tx, actor, "updater_extension_access", id, map[string]any{"enabled": enabled, "version": version + 1}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
