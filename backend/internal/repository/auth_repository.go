package repository

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrAuthRecordNotFound = errors.New("authentication record not found")

type AuthUser struct {
	AccessRevision        string
	Theme                 string
	ID                    string
	Username              string
	PasswordHash          string
	Email                 string
	RoleID                string
	RoleName              string
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
	if _, err = tx.Exec(ctx, `INSERT INTO user_login_activity(user_id,last_login_at,last_activity_at) VALUES($1,now(),now()) ON CONFLICT(user_id) DO UPDATE SET last_login_at=GREATEST(user_login_activity.last_login_at,EXCLUDED.last_login_at),last_activity_at=GREATEST(user_login_activity.last_activity_at,EXCLUDED.last_activity_at)`, id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Only the browser's session heartbeat updates presence; background SSE session
// validation must not keep a closed or hidden browser online.
func (r *AuthRepository) TouchSession(ctx context.Context, hash string) error {
	// Persist the same throttled heartbeat outside the session so logout,
	// revocation and expired-session cleanup retain the last activity time.
	_, err := r.pool.Exec(ctx, `WITH seen AS (
		UPDATE auth_sessions SET last_seen_at=now()
		WHERE token_hash=$1 AND expires_at>now() AND (last_seen_at IS NULL OR last_seen_at<now()-interval '20 seconds')
		RETURNING user_id,last_seen_at,created_at
	) INSERT INTO user_login_activity(user_id,last_login_at,last_activity_at)
	SELECT user_id,created_at,last_seen_at FROM seen
	ON CONFLICT(user_id) DO UPDATE SET last_activity_at=GREATEST(user_login_activity.last_activity_at,EXCLUDED.last_activity_at)`, hash)
	return err
}

func (r *AuthRepository) FindSessionByTokenHash(ctx context.Context, hash string) (AuthSession, error) {
	var s AuthSession
	var system bool
	var overrides PermissionOverrides
	var categoryOverrides ExpenseCategoryOverrides
	err := r.pool.QueryRow(ctx, `SELECT u.id::text,u.username,coalesce(u.email,''),u.role_id::text,r.system_role,r.permissions,s.csrf_token,s.expires_at,u.theme,r.name,u.version::text || ':' || r.version::text,u.permission_overrides,u.expense_category_overrides FROM auth_sessions s JOIN app_users u ON u.id=s.user_id JOIN app_roles r ON r.id=u.role_id WHERE s.token_hash=$1 AND s.expires_at>now() AND u.active`, hash).Scan(&s.User.ID, &s.User.Username, &s.User.Email, &s.User.RoleID, &system, &s.User.Permissions, &s.CSRFToken, &s.ExpiresAt, &s.User.Theme, &s.User.RoleName, &s.User.AccessRevision, &overrides, &categoryOverrides)
	if errors.Is(err, pgx.ErrNoRows) {
		return AuthSession{}, ErrAuthRecordNotFound
	}
	if err != nil {
		return AuthSession{}, err
	}
	s.User.Permissions = EffectivePermissions(s.User.Permissions, overrides)
	if system {
		s.User.Administrator = true
		s.User.Permissions = PermissionKeys()
	}
	s.User.ExpenseCategoryAccess = []ExpenseCategoryAccess{}
	query := `SELECT s.id::text,true,true,true,true,(s.active AND coalesce((to_jsonb(s)->>'accessorials')::boolean,false)) FROM expense_settings s WHERE s.kind='category' ORDER BY s.id`
	args := []any{}
	if !system {
		query = `SELECT s.id::text,coalesce(a.can_view,false),coalesce(a.can_create,false),coalesce(a.can_edit,false),coalesce(a.can_delete,false),(s.active AND coalesce((to_jsonb(s)->>'accessorials')::boolean,false)) FROM expense_settings s LEFT JOIN role_expense_category_access a ON a.category_id=s.id AND a.role_id=$1 WHERE s.kind='category' ORDER BY s.id`
		args = append(args, s.User.RoleID)
	}
	rows, accessErr := r.pool.Query(ctx, query, args...)
	if accessErr != nil {
		return AuthSession{}, accessErr
	}
	defer rows.Close()
	for rows.Next() {
		var access ExpenseCategoryAccess
		if accessErr = rows.Scan(&access.CategoryID, &access.CanView, &access.CanCreate, &access.CanEdit, &access.CanDelete, &access.Accessorials); accessErr != nil {
			return AuthSession{}, accessErr
		}
		if !system {
			access = effectiveCategoryAccess(access, categoryOverrides[access.CategoryID])
		}
		s.User.ExpenseCategoryAccess = append(s.User.ExpenseCategoryAccess, access)
	}
	if accessErr = rows.Err(); accessErr != nil {
		return AuthSession{}, accessErr
	}
	if slices.ContainsFunc(s.User.ExpenseCategoryAccess, func(a ExpenseCategoryAccess) bool { return a.CanView }) {
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

// Appearance changes do not change security versions or revoke sessions.
func (r *AuthRepository) SetTheme(ctx context.Context, userID, theme string) error {
	tx, err := beginPayrollUserMetadata(ctx, r.pool)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	result, err := tx.Exec(ctx, `UPDATE app_users SET theme=$2 WHERE id=$1 AND active`, userID, theme)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return ErrAuthRecordNotFound
	}
	return tx.Commit(ctx)
}

func (r *AuthRepository) ChangePassword(ctx context.Context, id, oldHash, newHash string) error {
	tx, err := beginPayrollUserMetadata(ctx, r.pool)
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
