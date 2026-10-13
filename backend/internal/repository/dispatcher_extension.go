package repository

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

type DispatcherExtensionAccess struct {
	DispatcherID        string     `json:"dispatcherId,omitempty"`
	UpdaterID           string     `json:"updaterId,omitempty"`
	Enabled             bool       `json:"enabled"`
	AccountLabel        string     `json:"accountLabel"`
	Version             int        `json:"version"`
	ActivatedAt         *time.Time `json:"activatedAt"`
	TokenExpiresAt      *time.Time `json:"tokenExpiresAt"`
	ActivationExpiresAt *time.Time `json:"activationExpiresAt"`
}

func (r *AuthRepository) DispatcherExtensionAccess(ctx context.Context, id string) (DispatcherExtensionAccess, error) {
	a := DispatcherExtensionAccess{DispatcherID: id}
	err := r.pool.QueryRow(ctx, `SELECT coalesce(a.enabled,false),coalesce(a.account_label,''),coalesce(a.version,0),a.activated_at,a.token_expires_at,a.activation_expires_at
 FROM dispatchers d LEFT JOIN dispatcher_extension_access a ON a.dispatcher_id=d.id WHERE d.id=$1`, id).Scan(&a.Enabled, &a.AccountLabel, &a.Version, &a.ActivatedAt, &a.TokenExpiresAt, &a.ActivationExpiresAt)
	return a, err
}

// Every administrative change invalidates the previous device and pending code.
// The dispatcher lock serializes first issuance and concurrent administrators.
func (r *AuthRepository) SetDispatcherExtensionAccess(ctx context.Context, id, actor, label, codeHash string, version int, enabled bool) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = accessLock(ctx, tx, actor); err != nil {
		return err
	}
	var active bool
	if err = tx.QueryRow(ctx, `SELECT active FROM dispatchers WHERE id=$1 FOR UPDATE`, id).Scan(&active); err != nil {
		return err
	}
	if enabled && !active {
		return ErrAccessConflict
	}
	var current int
	err = tx.QueryRow(ctx, `SELECT version FROM dispatcher_extension_access WHERE dispatcher_id=$1 FOR UPDATE`, id).Scan(&current)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if current != version {
		return ErrAccessConflict
	}
	_, err = tx.Exec(ctx, `INSERT INTO dispatcher_extension_access(dispatcher_id,enabled,account_label,activation_hash,activation_expires_at,updated_by)
 VALUES($1,$2,$3,nullif($4,''),CASE WHEN $2 THEN now()+interval '10 minutes' END,$5)
 ON CONFLICT(dispatcher_id) DO UPDATE SET enabled=excluded.enabled,account_label=excluded.account_label,
 activation_hash=excluded.activation_hash,activation_expires_at=excluded.activation_expires_at,
	token_hash=NULL,token_expires_at=NULL,activated_at=NULL,version=dispatcher_extension_access.version+1,updated_at=now(),updated_by=excluded.updated_by`, id, enabled, label, codeHash, actor)
	if err != nil {
		return err
	}
	if err = accessAudit(ctx, tx, actor, "dispatcher_extension_access", id, map[string]any{"enabled": enabled, "version": version + 1}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// A single atomic update consumes the high-entropy code exactly once.
func (r *AuthRepository) ActivateDispatcherExtension(ctx context.Context, codeHash, tokenHash string) (string, error) {
	var name string
	err := r.pool.QueryRow(ctx, `UPDATE dispatcher_extension_access a SET activation_hash=NULL,activation_expires_at=NULL,
 token_hash=$2,token_expires_at=now()+interval '30 days',activated_at=now(),version=a.version+1
 FROM dispatchers d WHERE a.dispatcher_id=d.id AND d.active AND a.enabled AND a.activation_hash=$1 AND a.activation_expires_at>now()
 RETURNING d.full_name`, codeHash, tokenHash).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		err = r.pool.QueryRow(ctx, `UPDATE updater_extension_access a SET activation_hash=NULL,activation_expires_at=NULL,
 token_hash=$2,token_expires_at=now()+interval '30 days',activated_at=now(),version=a.version+1
 FROM updaters u WHERE a.updater_id=u.id AND a.enabled AND a.activation_hash=$1 AND a.activation_expires_at>now()
 RETURNING u.full_name`, codeHash, tokenHash).Scan(&name)
	}
	return name, err
}

// No cookie/session permissions are granted to an extension token. Every cycle
// checks revocation and dispatcher activity; there is no offline authorization.
func (r *AuthRepository) CheckDispatcherExtension(ctx context.Context, hash string) (string, error) {
	var name string
	err := r.pool.QueryRow(ctx, `SELECT d.full_name FROM dispatcher_extension_access a JOIN dispatchers d ON d.id=a.dispatcher_id
 WHERE a.token_hash=$1 AND a.enabled AND d.active AND a.token_expires_at>now()`, hash).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		err = r.pool.QueryRow(ctx, `SELECT u.full_name FROM updater_extension_access a JOIN updaters u ON u.id=a.updater_id
 WHERE a.token_hash=$1 AND a.enabled AND a.token_expires_at>now()`, hash).Scan(&name)
	}
	return name, err
}
