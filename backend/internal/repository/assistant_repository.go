package repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrTelegramNotAuthorized = errors.New("telegram identity is not authorized")
	ErrLastTelegramManager   = errors.New("the final Telegram manager cannot be revoked")
	ErrInvalidLinkToken      = errors.New("the Telegram link is invalid or expired")
)

type AssistantRepository struct {
	pool *pgxpool.Pool
}

func NewAssistantRepository(pool *pgxpool.Pool) *AssistantRepository {
	return &AssistantRepository{pool: pool}
}

type TelegramManager struct {
	UserID           string     `json:"userId"`
	Username         string     `json:"username"`
	Active           bool       `json:"active"`
	Approved         bool       `json:"approved"`
	TelegramUserID   *int64     `json:"telegramUserId"`
	TelegramUsername *string    `json:"telegramUsername"`
	DisplayName      *string    `json:"displayName"`
	LinkedAt         *time.Time `json:"linkedAt"`
	LinkExpiresAt    *time.Time `json:"linkExpiresAt"`
}

type TelegramIdentity struct {
	AppUserID      string
	Username       string
	TelegramUserID int64
	TelegramChatID int64
	LinkExpiresAt  time.Time
}

func (r *AssistantRepository) ListManagers(ctx context.Context) ([]TelegramManager, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT u.id::text, u.username, u.active, m.app_user_id IS NOT NULL,
			i.telegram_user_id, i.telegram_username, i.display_name,
			i.linked_at, i.expires_at
		FROM app_users u
		LEFT JOIN telegram_managers m ON m.app_user_id = u.id
		LEFT JOIN telegram_identities i ON i.app_user_id = u.id
		ORDER BY lower(u.username), u.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := make([]TelegramManager, 0)
	for rows.Next() {
		var value TelegramManager
		if err := rows.Scan(&value.UserID, &value.Username, &value.Active, &value.Approved,
			&value.TelegramUserID, &value.TelegramUsername, &value.DisplayName,
			&value.LinkedAt, &value.LinkExpiresAt); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (r *AssistantRepository) SetManagerApproved(ctx context.Context, actorID, userID string, approved bool) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if approved {
		command, err := tx.Exec(ctx, `
			INSERT INTO telegram_managers (app_user_id, approved_by)
			SELECT id, $1 FROM app_users WHERE id = $2 AND active = true
			ON CONFLICT (app_user_id) DO NOTHING`, actorID, userID)
		if err != nil {
			return err
		}
		if command.RowsAffected() == 0 {
			var exists bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM telegram_managers m JOIN app_users u ON u.id=m.app_user_id WHERE m.app_user_id=$1 AND u.active=true)`, userID).Scan(&exists); err != nil {
				return err
			}
			if !exists {
				return ErrNotFound
			}
		}
		return tx.Commit(ctx)
	}
	if _, err := tx.Exec(ctx, `LOCK TABLE telegram_managers IN SHARE ROW EXCLUSIVE MODE`); err != nil {
		return err
	}
	var targetActive bool
	if err := tx.QueryRow(ctx, `SELECT u.active FROM telegram_managers m JOIN app_users u ON u.id=m.app_user_id WHERE m.app_user_id=$1`, userID).Scan(&targetActive); errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	var count int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM telegram_managers m JOIN app_users u ON u.id=m.app_user_id WHERE u.active=true`).Scan(&count); err != nil {
		return err
	}
	if targetActive && count <= 1 {
		return ErrLastTelegramManager
	}
	command, err := tx.Exec(ctx, `DELETE FROM telegram_managers WHERE app_user_id=$1`, userID)
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		return ErrNotFound
	}
	return tx.Commit(ctx)
}

func (r *AssistantRepository) BootstrapManager(ctx context.Context, username string) error {
	command, err := r.pool.Exec(ctx, `
		INSERT INTO telegram_managers (app_user_id)
		SELECT id FROM app_users WHERE lower(username)=lower($1) AND active=true
		ON CONFLICT (app_user_id) DO NOTHING`, username)
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		var exists bool
		if err := r.pool.QueryRow(ctx, `SELECT EXISTS (
			SELECT 1 FROM app_users u JOIN telegram_managers m ON m.app_user_id=u.id
			WHERE lower(u.username)=lower($1) AND u.active=true)`, username).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return ErrNotFound
		}
	}
	return nil
}

func (r *AssistantRepository) ManagerStatus(ctx context.Context, userID string) (TelegramManager, error) {
	var value TelegramManager
	err := r.pool.QueryRow(ctx, `
		SELECT u.id::text, u.username, u.active, m.app_user_id IS NOT NULL,
			i.telegram_user_id, i.telegram_username, i.display_name,
			i.linked_at, i.expires_at
		FROM app_users u
		LEFT JOIN telegram_managers m ON m.app_user_id=u.id
		LEFT JOIN telegram_identities i ON i.app_user_id=u.id
		WHERE u.id=$1`, userID).Scan(&value.UserID, &value.Username, &value.Active,
		&value.Approved, &value.TelegramUserID, &value.TelegramUsername,
		&value.DisplayName, &value.LinkedAt, &value.LinkExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return TelegramManager{}, ErrNotFound
	}
	return value, err
}

func (r *AssistantRepository) CreateLinkToken(ctx context.Context, userID, tokenHash string, expiresAt time.Time) error {
	command, err := r.pool.Exec(ctx, `
		WITH removed AS (
			DELETE FROM telegram_link_tokens WHERE app_user_id=$1 AND used_at IS NULL
		)
		INSERT INTO telegram_link_tokens (app_user_id, token_hash, expires_at)
		SELECT m.app_user_id, $2, $3
		FROM telegram_managers m JOIN app_users u ON u.id=m.app_user_id
		WHERE m.app_user_id=$1 AND u.active=true`, userID, tokenHash, expiresAt)
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		return ErrTelegramNotAuthorized
	}
	return nil
}

func (r *AssistantRepository) LinkIdentity(ctx context.Context, tokenHash string, telegramUserID, telegramChatID int64, telegramUsername, displayName string, expiresAt time.Time) (TelegramIdentity, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return TelegramIdentity{}, err
	}
	defer tx.Rollback(ctx)
	var userID, username string
	err = tx.QueryRow(ctx, `
		UPDATE telegram_link_tokens t SET used_at=now()
		FROM telegram_managers m JOIN app_users u ON u.id=m.app_user_id
		WHERE t.token_hash=$1 AND t.used_at IS NULL AND t.expires_at>now()
		  AND t.app_user_id=m.app_user_id AND u.active=true
		RETURNING u.id::text, u.username`, tokenHash).Scan(&userID, &username)
	if errors.Is(err, pgx.ErrNoRows) {
		return TelegramIdentity{}, ErrInvalidLinkToken
	}
	if err != nil {
		return TelegramIdentity{}, err
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM telegram_identities
		WHERE telegram_user_id=$1 OR telegram_chat_id=$2 OR app_user_id=$3`, telegramUserID, telegramChatID, userID); err != nil {
		return TelegramIdentity{}, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO telegram_identities (
			app_user_id, telegram_user_id, telegram_chat_id,
			telegram_username, display_name, expires_at
		) VALUES ($1,$2,$3,NULLIF($4,''),NULLIF($5,''),$6)`,
		userID, telegramUserID, telegramChatID, telegramUsername, displayName, expiresAt); err != nil {
		return TelegramIdentity{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return TelegramIdentity{}, err
	}
	return TelegramIdentity{AppUserID: userID, Username: username, TelegramUserID: telegramUserID,
		TelegramChatID: telegramChatID, LinkExpiresAt: expiresAt}, nil
}

func (r *AssistantRepository) FindIdentity(ctx context.Context, telegramUserID, telegramChatID int64) (TelegramIdentity, error) {
	var value TelegramIdentity
	err := r.pool.QueryRow(ctx, `
		SELECT u.id::text, u.username, i.telegram_user_id, i.telegram_chat_id, i.expires_at
		FROM telegram_identities i
		JOIN telegram_managers m ON m.app_user_id=i.app_user_id
		JOIN app_users u ON u.id=i.app_user_id
		WHERE i.telegram_user_id=$1 AND i.telegram_chat_id=$2
		  AND i.expires_at>now() AND u.active=true`, telegramUserID, telegramChatID).Scan(
		&value.AppUserID, &value.Username, &value.TelegramUserID, &value.TelegramChatID, &value.LinkExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return TelegramIdentity{}, ErrTelegramNotAuthorized
	}
	return value, err
}

func (r *AssistantRepository) RevokeIdentity(ctx context.Context, userID string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM telegram_identities WHERE app_user_id=$1`, userID)
	return err
}

type TelegramUpdate struct {
	UpdateID       int64
	TelegramUserID *int64
	TelegramChatID *int64
	Payload        json.RawMessage
	Attempts       int
}

type AssistantActionRequest struct {
	ID             string
	AppUserID      string
	TelegramUserID int64
	TelegramChatID int64
	ActionName     string
	Arguments      json.RawMessage
	BeforeState    json.RawMessage
	BeforeHash     *string
	Preview        string
	ExpiresAt      time.Time
}

func (r *AssistantRepository) CreateActionRequest(ctx context.Context, identity TelegramIdentity, actionName string, arguments, beforeState []byte, beforeHash, preview string) (AssistantActionRequest, error) {
	var value AssistantActionRequest
	err := r.pool.QueryRow(ctx, `
		INSERT INTO assistant_action_requests (
			app_user_id, telegram_user_id, telegram_chat_id, action_name,
			arguments, before_state, before_hash, preview, expires_at
		) VALUES ($1,$2,$3,$4,$5,NULLIF($6,'null'::jsonb),NULLIF($7,''),$8,now()+interval '5 minutes')
		RETURNING id::text, app_user_id::text, telegram_user_id, telegram_chat_id,
			action_name, arguments, COALESCE(before_state,'null'::jsonb), before_hash, preview, expires_at`,
		identity.AppUserID, identity.TelegramUserID, identity.TelegramChatID,
		actionName, arguments, beforeState, beforeHash, preview).Scan(
		&value.ID, &value.AppUserID, &value.TelegramUserID, &value.TelegramChatID,
		&value.ActionName, &value.Arguments, &value.BeforeState, &value.BeforeHash,
		&value.Preview, &value.ExpiresAt)
	return value, err
}

func (r *AssistantRepository) ClaimActionRequest(ctx context.Context, id string, telegramUserID, telegramChatID int64) (AssistantActionRequest, error) {
	var value AssistantActionRequest
	err := r.pool.QueryRow(ctx, `
		UPDATE assistant_action_requests SET status='executing'
		WHERE id=$1 AND telegram_user_id=$2 AND telegram_chat_id=$3
		  AND status='pending' AND expires_at>now()
		RETURNING id::text, app_user_id::text, telegram_user_id, telegram_chat_id,
			action_name, arguments, COALESCE(before_state,'null'::jsonb), before_hash, preview, expires_at`,
		id, telegramUserID, telegramChatID).Scan(&value.ID, &value.AppUserID,
		&value.TelegramUserID, &value.TelegramChatID, &value.ActionName,
		&value.Arguments, &value.BeforeState, &value.BeforeHash, &value.Preview, &value.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return AssistantActionRequest{}, ErrNotFound
	}
	return value, err
}

func (r *AssistantRepository) CancelActionRequest(ctx context.Context, id string, telegramUserID, telegramChatID int64) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var userID, actionName string
	var arguments, before json.RawMessage
	err = tx.QueryRow(ctx, `UPDATE assistant_action_requests SET status='cancelled', completed_at=now()
		WHERE id=$1 AND telegram_user_id=$2 AND telegram_chat_id=$3 AND status='pending'
		RETURNING app_user_id::text,action_name,arguments,COALESCE(before_state,'null'::jsonb)`, id, telegramUserID, telegramChatID).Scan(&userID, &actionName, &arguments, &before)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO assistant_action_audit(app_user_id,action_request_id,action_name,arguments,before_state,outcome)
		VALUES($1,$2,$3,$4,NULLIF($5,'null'::jsonb),'cancelled')`, userID, id, actionName, arguments, before); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *AssistantRepository) FinishActionRequest(ctx context.Context, request AssistantActionRequest, status string, afterState []byte, actionErr error) error {
	var message *string
	if actionErr != nil {
		value := actionErr.Error()
		message = &value
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `UPDATE assistant_action_requests SET status=$2, completed_at=now() WHERE id=$1`, request.ID, status); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO assistant_action_audit (
		app_user_id, action_request_id, action_name, arguments, before_state, after_state, outcome, error_message
	) VALUES ($1,$2,$3,$4,NULLIF($5,'null'::jsonb),NULLIF($6,'null'::jsonb),$7,$8)`,
		request.AppUserID, request.ID, request.ActionName, request.Arguments,
		request.BeforeState, afterState, status, message); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *AssistantRepository) WriteActionAudit(ctx context.Context, userID, actionName string, arguments []byte, beforeState, afterState any, outcome string, actionErr error) error {
	before, _ := json.Marshal(beforeState)
	after, _ := json.Marshal(afterState)
	var message *string
	if actionErr != nil {
		value := actionErr.Error()
		message = &value
	}
	_, err := r.pool.Exec(ctx, `INSERT INTO assistant_action_audit (
		app_user_id, action_name, arguments, before_state, after_state, outcome, error_message
	) VALUES ($1,$2,$3,NULLIF($4,'null'::jsonb),NULLIF($5,'null'::jsonb),$6,$7)`,
		userID, actionName, arguments, before, after, outcome, message)
	return err
}

func (r *AssistantRepository) EnqueueUpdate(ctx context.Context, updateID int64, userID, chatID *int64, payload []byte, status string) (bool, error) {
	command, err := r.pool.Exec(ctx, `
		INSERT INTO telegram_updates (update_id, telegram_user_id, telegram_chat_id, payload, status)
		VALUES ($1,$2,$3,$4,$5) ON CONFLICT (update_id) DO NOTHING`, updateID, userID, chatID, payload, status)
	return command.RowsAffected() == 1, err
}

func (r *AssistantRepository) HasUpdate(ctx context.Context, updateID int64) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM telegram_updates WHERE update_id=$1)`, updateID).Scan(&exists)
	return exists, err
}

func (r *AssistantRepository) ClaimNextUpdate(ctx context.Context) (TelegramUpdate, error) {
	var value TelegramUpdate
	err := r.pool.QueryRow(ctx, `
		WITH candidate AS (
			SELECT u.update_id FROM telegram_updates u
			WHERE u.status IN ('queued','failed') AND u.next_attempt_at<=now()
			  AND u.attempts<5
			  AND NOT EXISTS (
				SELECT 1 FROM telegram_updates active
				WHERE active.status='processing'
				  AND active.telegram_user_id IS NOT DISTINCT FROM u.telegram_user_id)
			ORDER BY u.received_at, u.update_id
			FOR UPDATE SKIP LOCKED LIMIT 1
		)
		UPDATE telegram_updates u SET status='processing', attempts=u.attempts+1,
			started_at=now(), last_error=NULL
		FROM candidate c WHERE u.update_id=c.update_id
		RETURNING u.update_id, u.telegram_user_id, u.telegram_chat_id, u.payload, u.attempts`).Scan(
		&value.UpdateID, &value.TelegramUserID, &value.TelegramChatID, &value.Payload, &value.Attempts)
	return value, err
}

func (r *AssistantRepository) FinishUpdate(ctx context.Context, updateID int64, status string, processErr error) error {
	var message *string
	if processErr != nil {
		value := processErr.Error()
		if len(value) > 1000 {
			value = value[:1000]
		}
		message = &value
	}
	next := time.Now().Add(5 * time.Second)
	_, err := r.pool.Exec(ctx, `
		UPDATE telegram_updates SET status=$2, last_error=$3,
			next_attempt_at=$4, completed_at=CASE WHEN $2 IN ('completed','rejected') THEN now() END
		WHERE update_id=$1`, updateID, status, message, next)
	return err
}

func (r *AssistantRepository) LoadConversation(ctx context.Context, userID string) (json.RawMessage, error) {
	var messages json.RawMessage
	err := r.pool.QueryRow(ctx, `
		SELECT messages FROM assistant_conversations
		WHERE app_user_id=$1 AND expires_at>now()`, userID).Scan(&messages)
	if errors.Is(err, pgx.ErrNoRows) {
		return json.RawMessage(`[]`), nil
	}
	return messages, err
}

func (r *AssistantRepository) SaveConversation(ctx context.Context, userID string, messages []byte) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO assistant_conversations (app_user_id, messages, expires_at)
		VALUES ($1,$2,now()+interval '24 hours')
		ON CONFLICT (app_user_id) DO UPDATE SET messages=EXCLUDED.messages,
			expires_at=EXCLUDED.expires_at, updated_at=now()`, userID, messages)
	return err
}

func (r *AssistantRepository) ClearConversation(ctx context.Context, userID string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM assistant_conversations WHERE app_user_id=$1`, userID)
	return err
}

func (r *AssistantRepository) WriteAudit(ctx context.Context, userID string, updateID int64, eventType, prompt, response, outcome string, toolCalls, toolResults any, processErr error) error {
	calls, _ := json.Marshal(toolCalls)
	results, _ := json.Marshal(toolResults)
	var message *string
	if processErr != nil {
		value := processErr.Error()
		message = &value
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO assistant_audit_log (
			app_user_id, telegram_update_id, event_type, prompt, response,
			tool_calls, tool_results, outcome, error_message
		) VALUES ($1,$2,$3,NULLIF($4,''),NULLIF($5,''),$6,$7,$8,$9)`,
		userID, updateID, eventType, prompt, response, calls, results, outcome, message)
	return err
}

type AssistantAuditEntry struct {
	ID           string          `json:"id"`
	Username     *string         `json:"username"`
	EventType    string          `json:"eventType"`
	Prompt       *string         `json:"prompt"`
	Response     *string         `json:"response"`
	ToolCalls    json.RawMessage `json:"toolCalls"`
	ToolResults  json.RawMessage `json:"toolResults"`
	Outcome      string          `json:"outcome"`
	ErrorMessage *string         `json:"errorMessage"`
	CreatedAt    time.Time       `json:"createdAt"`
}

func (r *AssistantRepository) ListAudit(ctx context.Context, limit int) ([]AssistantAuditEntry, error) {
	if limit < 1 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	rows, err := r.pool.Query(ctx, `SELECT id,username,event_type,prompt,response,
		tool_calls,tool_results,outcome,error_message,created_at FROM (
		SELECT a.id::text id,u.username,a.event_type,a.prompt,a.response,
			a.tool_calls,a.tool_results,a.outcome,a.error_message,a.created_at
		FROM assistant_audit_log a LEFT JOIN app_users u ON u.id=a.app_user_id
		UNION ALL
		SELECT a.id::text,u.username,'action:'||a.action_name,a.arguments::text,
			a.after_state::text,'[]'::jsonb,'[]'::jsonb,a.outcome,a.error_message,a.created_at
		FROM assistant_action_audit a LEFT JOIN app_users u ON u.id=a.app_user_id
	) audit ORDER BY created_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := make([]AssistantAuditEntry, 0, limit)
	for rows.Next() {
		var value AssistantAuditEntry
		if err := rows.Scan(&value.ID, &value.Username, &value.EventType, &value.Prompt, &value.Response, &value.ToolCalls, &value.ToolResults, &value.Outcome, &value.ErrorMessage, &value.CreatedAt); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (r *AssistantRepository) Cleanup(ctx context.Context) error {
	queries := []string{
		`DELETE FROM telegram_link_tokens WHERE expires_at<now() OR used_at IS NOT NULL`,
		`DELETE FROM telegram_identities WHERE expires_at<now()`,
		`DELETE FROM assistant_conversations WHERE expires_at<now()`,
		`UPDATE assistant_action_requests SET status='expired', completed_at=now() WHERE status='pending' AND expires_at<now()`,
		`UPDATE assistant_action_requests SET status='failed', completed_at=now() WHERE status='executing' AND created_at<now()-interval '10 minutes'`,
		`DELETE FROM assistant_audit_log WHERE expires_at<now()`,
		`DELETE FROM assistant_action_audit WHERE expires_at<now()`,
		`DELETE FROM assistant_action_requests WHERE created_at<now()-interval '1 year'`,
		`DELETE FROM telegram_updates WHERE received_at<now()-interval '30 days' AND status IN ('completed','rejected')`,
	}
	for _, query := range queries {
		if _, err := r.pool.Exec(ctx, query); err != nil {
			return err
		}
	}
	return nil
}

func (r *AssistantRepository) PendingCount(ctx context.Context, telegramUserID int64) (int, error) {
	var count int
	err := r.pool.QueryRow(ctx, `SELECT count(*) FROM telegram_updates
		WHERE telegram_user_id=$1 AND status IN ('queued','processing','failed')`, telegramUserID).Scan(&count)
	return count, err
}

func (r *AssistantRepository) DataFreshness(ctx context.Context, source string) (*time.Time, error) {
	queries := map[string]string{
		"loads": `SELECT max(synced_at) FROM loads`,
		"fuel":  `SELECT max(synced_at) FROM fuel_transactions`,
		"tolls": `SELECT max(created_at) FROM tolls`,
		"financial": `SELECT max(value) FROM (
			SELECT max(synced_at) value FROM loads UNION ALL
			SELECT max(synced_at) FROM fuel_transactions UNION ALL
			SELECT max(created_at) FROM tolls) data_values`,
	}
	query, ok := queries[source]
	if !ok {
		return nil, errors.New("unknown freshness source")
	}
	var value *time.Time
	err := r.pool.QueryRow(ctx, query).Scan(&value)
	return value, err
}
