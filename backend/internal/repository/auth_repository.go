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
	ID           string
	Username     string
	PasswordHash string
}

type AuthSession struct {
	User      AuthUser
	CSRFToken string
	ExpiresAt time.Time
}

type AuthRepository struct {
	pool *pgxpool.Pool
}

func NewAuthRepository(pool *pgxpool.Pool) *AuthRepository {
	return &AuthRepository{pool: pool}
}

func (r *AuthRepository) FindUserByUsername(ctx context.Context, username string) (AuthUser, error) {
	var user AuthUser
	err := r.pool.QueryRow(ctx, `
		SELECT id::text, username, password_hash
		FROM app_users
		WHERE lower(username) = lower($1) AND active = true
	`, username).Scan(&user.ID, &user.Username, &user.PasswordHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return AuthUser{}, ErrAuthRecordNotFound
	}
	return user, err
}

func (r *AuthRepository) CreateSession(
	ctx context.Context,
	userID string,
	tokenHash string,
	csrfToken string,
	expiresAt time.Time,
) error {
	_, err := r.pool.Exec(ctx, `
		WITH removed AS (
			DELETE FROM auth_sessions WHERE expires_at <= now()
		)
		INSERT INTO auth_sessions (user_id, token_hash, csrf_token, expires_at)
		VALUES ($1, $2, $3, $4)
	`, userID, tokenHash, csrfToken, expiresAt)
	return err
}

func (r *AuthRepository) FindSessionByTokenHash(ctx context.Context, tokenHash string) (AuthSession, error) {
	var session AuthSession
	err := r.pool.QueryRow(ctx, `
		SELECT u.id::text, u.username, s.csrf_token, s.expires_at
		FROM auth_sessions s
		JOIN app_users u ON u.id = s.user_id
		WHERE s.token_hash = $1
		  AND s.expires_at > now()
		  AND u.active = true
	`, tokenHash).Scan(
		&session.User.ID,
		&session.User.Username,
		&session.CSRFToken,
		&session.ExpiresAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return AuthSession{}, ErrAuthRecordNotFound
	}
	return session, err
}

func (r *AuthRepository) DeleteSessionByTokenHash(ctx context.Context, tokenHash string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM auth_sessions WHERE token_hash = $1`, tokenHash)
	return err
}
