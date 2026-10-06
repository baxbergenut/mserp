package repository

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrAuthRecordNotFound = errors.New("authentication record not found")

type AuthUser struct {
	ID                    string
	Username              string
	PasswordHash          string
	Email                 string
	RoleID                string
	Permissions           []string
	Version               int
	Administrator         bool
	ExpenseCategoryAccess []ExpenseCategoryAccess
}
type AuthSession struct {
	User      AuthUser
	CSRFToken string
	ExpiresAt time.Time
}
type AuthRepository struct{ pool *pgxpool.Pool }

func NewAuthRepository(pool *pgxpool.Pool) *AuthRepository { return &AuthRepository{pool: pool} }

func (r *AuthRepository) FindUserByEmail(ctx context.Context, login string) (AuthUser, error) {
	var u AuthUser
	// Match exactly one identity. Username fallback is limited to legacy accounts
	// without an email, and ambiguity fails closed rather than selecting a user.
	rows, err := r.pool.Query(ctx, `SELECT id::text,username,password_hash,coalesce(email,''),role_id::text,version FROM app_users WHERE active AND role_id IS NOT NULL AND (lower(email)=lower($1) OR (email IS NULL AND lower(username)=lower($1)))`, login)
	if err != nil {
		return u, err
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		count++
		if err = rows.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Email, &u.RoleID, &u.Version); err != nil {
			return AuthUser{}, err
		}
	}
	if err = rows.Err(); err != nil {
		return AuthUser{}, err
	}
	if count != 1 {
		return AuthUser{}, ErrAuthRecordNotFound
	}
	return u, nil
}

// Serialize issuance against password changes, deactivation, and revocation.
func (r *AuthRepository) CreatePasswordSession(ctx context.Context, u AuthUser, hash, csrf string, expiry time.Time) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var id string
	err = tx.QueryRow(ctx, `SELECT id::text FROM app_users WHERE id=$1 AND active AND password_hash=$2 AND version=$3 AND role_id IS NOT NULL FOR UPDATE`, u.ID, u.PasswordHash, u.Version).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrAuthRecordNotFound
	}
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM auth_sessions WHERE expires_at<=now()`); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO auth_sessions(user_id,token_hash,csrf_token,expires_at) VALUES($1,$2,$3,$4)`, id, hash, csrf, expiry); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *AuthRepository) FindSessionByTokenHash(ctx context.Context, hash string) (AuthSession, error) {
	var s AuthSession
	var system bool
	err := r.pool.QueryRow(ctx, `SELECT u.id::text,u.username,coalesce(u.email,''),u.role_id::text,r.system_role,r.permissions,s.csrf_token,s.expires_at FROM auth_sessions s JOIN app_users u ON u.id=s.user_id JOIN app_roles r ON r.id=u.role_id WHERE s.token_hash=$1 AND s.expires_at>now() AND u.active`, hash).Scan(&s.User.ID, &s.User.Username, &s.User.Email, &s.User.RoleID, &system, &s.User.Permissions, &s.CSRFToken, &s.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return AuthSession{}, ErrAuthRecordNotFound
	}
	if system {
		s.User.Administrator = true
		s.User.Permissions = PermissionKeys()
	}
	s.User.ExpenseCategoryAccess = []ExpenseCategoryAccess{}
	query := `SELECT s.id::text,true,true,true,true FROM expense_settings s WHERE s.kind='category'`
	args := []any{}
	if !system {
		query = `SELECT category_id::text,can_view,can_create,can_edit,can_delete FROM role_expense_category_access WHERE role_id=$1`
		args = append(args, s.User.RoleID)
	}
	rows, accessErr := r.pool.Query(ctx, query, args...)
	if accessErr != nil {
		return AuthSession{}, accessErr
	}
	defer rows.Close()
	for rows.Next() {
		var access ExpenseCategoryAccess
		if accessErr = rows.Scan(&access.CategoryID, &access.CanView, &access.CanCreate, &access.CanEdit, &access.CanDelete); accessErr != nil {
			return AuthSession{}, accessErr
		}
		s.User.ExpenseCategoryAccess = append(s.User.ExpenseCategoryAccess, access)
	}
	if accessErr = rows.Err(); accessErr != nil {
		return AuthSession{}, accessErr
	}
	if len(s.User.ExpenseCategoryAccess) > 0 {
		s.User.Permissions = append(s.User.Permissions, "expenses.read")
	}
	for _, access := range s.User.ExpenseCategoryAccess {
		if access.CanCreate || access.CanEdit || access.CanDelete {
			s.User.Permissions = append(s.User.Permissions, "expenses.write")
			break
		}
	}
	return s, err
}
func (r *AuthRepository) DeleteSessionByTokenHash(ctx context.Context, hash string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM auth_sessions WHERE token_hash=$1`, hash)
	return err
}

func (r *AuthRepository) ChangePassword(ctx context.Context, id, oldHash, newHash string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var found string
	err = tx.QueryRow(ctx, `UPDATE app_users SET password_hash=$3,version=version+1,updated_at=now() WHERE id=$1 AND password_hash=$2 AND active RETURNING id::text`, id, oldHash, newHash).Scan(&found)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrAuthRecordNotFound
	}
	if err != nil {
		return err
	}
	if err = revokeUser(ctx, tx, id); err != nil {
		return err
	}
	if err = accessAudit(ctx, tx, id, "change_password", id, struct{}{}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
