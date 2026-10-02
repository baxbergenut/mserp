package repository

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
)

var ErrAccessConflict = errors.New("record changed, email/name already used, or last administrator would be removed")

type AccessRole struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Permissions []string `json:"permissions"`
	System      bool     `json:"system"`
	Version     int      `json:"version"`
}
type ManagedUser struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Email    string `json:"email"`
	RoleID   string `json:"roleId"`
	Active   bool   `json:"active"`
	Password string `json:"password,omitempty"`
	Version  int    `json:"version"`
}
type AccessData struct {
	Users       []ManagedUser `json:"users"`
	Roles       []AccessRole  `json:"roles"`
	Permissions []Permission  `json:"permissions"`
}

func (r *AuthRepository) AccessData(ctx context.Context) (AccessData, error) {
	data := AccessData{Users: []ManagedUser{}, Roles: []AccessRole{}, Permissions: Permissions}
	rows, err := r.pool.Query(ctx, `SELECT id::text,username,coalesce(email,''),coalesce(role_id::text,''),active,version FROM app_users ORDER BY lower(username),id`)
	if err != nil {
		return data, err
	}
	for rows.Next() {
		var u ManagedUser
		if err = rows.Scan(&u.ID, &u.Username, &u.Email, &u.RoleID, &u.Active, &u.Version); err != nil {
			rows.Close()
			return data, err
		}
		data.Users = append(data.Users, u)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return data, err
	}
	rows, err = r.pool.Query(ctx, `SELECT id::text,name,permissions,system_role,version FROM app_roles ORDER BY system_role DESC,lower(name)`)
	if err != nil {
		return data, err
	}
	defer rows.Close()
	for rows.Next() {
		var role AccessRole
		if err = rows.Scan(&role.ID, &role.Name, &role.Permissions, &role.System, &role.Version); err != nil {
			return data, err
		}
		if role.System {
			role.Permissions = PermissionKeys()
		}
		data.Roles = append(data.Roles, role)
	}
	return data, rows.Err()
}

// Serialize access administration, including the last-administrator invariant.
// Recheck the actor inside the transaction, so revocation cannot race a write.
func accessLock(ctx context.Context, tx pgx.Tx, actor string) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(440044)`); err != nil {
		return err
	}
	if actor == "" {
		return ErrAccessConflict
	}
	var allowed bool
	err := tx.QueryRow(ctx, `SELECT u.active AND (r.system_role OR 'access.manage'=ANY(r.permissions)) FROM app_users u JOIN app_roles r ON r.id=u.role_id WHERE u.id=$1`, actor).Scan(&allowed)
	if err != nil {
		return err
	}
	if !allowed {
		return ErrAccessConflict
	}
	return nil
}
func accessAudit(ctx context.Context, tx pgx.Tx, actor, action, target string, details any) error {
	b, err := json.Marshal(details)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO access_audit(actor_id,action,target_id,details) VALUES(NULLIF($1,'')::uuid,$2,$3,$4)`, actor, action, target, b)
	return err
}
func revokeUser(ctx context.Context, tx pgx.Tx, id string) error {
	_, err := tx.Exec(ctx, `DELETE FROM auth_sessions WHERE user_id=$1`, id)
	return err
}

func (r *AuthRepository) SaveUser(ctx context.Context, actor string, input ManagedUser, passwordHash string) (string, error) {
	input.Password = ""
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	if err = accessLock(ctx, tx, actor); err != nil {
		return "", err
	}
	id := input.ID
	if id == "" {
		err = tx.QueryRow(ctx, `INSERT INTO app_users(username,email,role_id,active,password_hash) VALUES($1,$2,$3,$4,$5) RETURNING id::text`, input.Username, input.Email, input.RoleID, input.Active, passwordHash).Scan(&id)
	} else {
		var oldEmail string
		err = tx.QueryRow(ctx, `SELECT coalesce(email,'') FROM app_users WHERE id=$1 AND version=$2 FOR UPDATE`, id, input.Version).Scan(&oldEmail)
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ErrAccessConflict
		}
		if err != nil {
			return "", err
		}
		_, err = tx.Exec(ctx, `UPDATE app_users SET username=$2,email=$3,role_id=$4,active=$5,version=version+1,updated_at=now(),password_hash=CASE WHEN $6='' THEN password_hash ELSE $6 END WHERE id=$1`, id, input.Username, input.Email, input.RoleID, input.Active, passwordHash)
		if err == nil {
			err = revokeUser(ctx, tx, id)
		}
	}
	if err != nil {
		return "", err
	}
	var admins int
	err = tx.QueryRow(ctx, `SELECT count(*) FROM app_users u JOIN app_roles r ON r.id=u.role_id WHERE u.active AND r.system_role`).Scan(&admins)
	if err != nil {
		return "", err
	}
	if admins == 0 {
		return "", ErrAccessConflict
	}
	if err = accessAudit(ctx, tx, actor, "save_user", id, input); err != nil {
		return "", err
	}
	return id, tx.Commit(ctx)
}

func (r *AuthRepository) SaveRole(ctx context.Context, actor string, input AccessRole) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = accessLock(ctx, tx, actor); err != nil {
		return err
	}
	if input.Permissions == nil {
		input.Permissions = []string{}
	}
	if input.ID == "" {
		err = tx.QueryRow(ctx, `INSERT INTO app_roles(name,permissions) VALUES($1,$2) RETURNING id::text`, input.Name, input.Permissions).Scan(&input.ID)
	} else {
		var id string
		err = tx.QueryRow(ctx, `UPDATE app_roles SET name=$2,permissions=$3,version=version+1 WHERE id=$1 AND version=$4 AND NOT system_role RETURNING id::text`, input.ID, input.Name, input.Permissions, input.Version).Scan(&id)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrAccessConflict
	}
	if err != nil {
		return err
	}
	if err = accessAudit(ctx, tx, actor, "save_role", input.ID, input); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *AuthRepository) RevokeAccess(ctx context.Context, actor, id string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = accessLock(ctx, tx, actor); err != nil {
		return err
	}
	var found string
	if err = tx.QueryRow(ctx, `UPDATE app_users SET version=version+1,updated_at=now() WHERE id=$1 RETURNING id::text`, id).Scan(&found); err != nil {
		return err
	}
	if err = revokeUser(ctx, tx, id); err != nil {
		return err
	}
	if err = accessAudit(ctx, tx, actor, "revoke_sessions", id, struct{}{}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
