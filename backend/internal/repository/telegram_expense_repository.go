package repository

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type TelegramExpenseUpdate struct {
	UpdateID  int64
	ChatID    int64
	MessageID int64
	Payload   json.RawMessage
	Attempts  int
}

type TelegramExpenseDraft struct {
	Company         string
	Category        string
	ExpenseDate     time.Time
	UnitNumber      *string
	DriverName      *string
	Amount          string
	PaymentType     *string
	ExpenseType     *string
	ReferenceNumber *string
	Description     *string
	CoveredBy       *string
	PaidBy          *string
}

type TelegramExpenseActivity struct {
	UpdateID      int64           `json:"updateId"`
	ChatID        int64           `json:"chatId"`
	MessageID     int64           `json:"messageId"`
	ChatType      string          `json:"chatType"`
	ChatTitle     *string         `json:"chatTitle"`
	SenderName    *string         `json:"senderName"`
	MessageText   *string         `json:"messageText"`
	FileName      *string         `json:"fileName"`
	MIMEType      *string         `json:"mimeType"`
	MediaGroupID  *string         `json:"mediaGroupId"`
	Status        string          `json:"status"`
	Attempts      int             `json:"attempts"`
	NextAttemptAt time.Time       `json:"nextAttemptAt"`
	StartedAt     *time.Time      `json:"startedAt"`
	CompletedAt   *time.Time      `json:"completedAt"`
	LastError     *string         `json:"lastError"`
	ExtractedData json.RawMessage `json:"extractedData"`
	ExpenseID     *string         `json:"expenseId"`
	ExpenseDate   *string         `json:"expenseDate"`
	Company       *string         `json:"company"`
	Category      *string         `json:"category"`
	Amount        *string         `json:"amount"`
	UnitNumber    *string         `json:"unitNumber"`
	DriverName    *string         `json:"driverName"`
	TruckID       *string         `json:"truckId"`
	DriverID      *string         `json:"driverId"`
	ExpenseType   *string         `json:"expenseType"`
	Description   *string         `json:"description"`
	CreatedAt     time.Time       `json:"createdAt"`
	UpdatedAt     time.Time       `json:"updatedAt"`
}

type TelegramExpenseActivitySummary struct {
	Received24Hours int        `json:"received24Hours"`
	Completed       int        `json:"completed"`
	InProgress      int        `json:"inProgress"`
	NeedsReview     int        `json:"needsReview"`
	Ignored         int        `json:"ignored"`
	Failed          int        `json:"failed"`
	Unmatched       int        `json:"unmatched"`
	LastCompletedAt *time.Time `json:"lastCompletedAt"`
}

type TelegramExpenseActivityPage struct {
	Page[TelegramExpenseActivity]
	Summary TelegramExpenseActivitySummary `json:"summary"`
}

func (r *ExpenseRepository) EnqueueTelegramUpdate(
	ctx context.Context,
	updateID, chatID, messageID int64,
	chatType string,
	payload []byte,
) (bool, error) {
	command, err := r.pool.Exec(ctx, `
		INSERT INTO telegram_expense_updates (
			update_id, chat_id, message_id, chat_type, raw_update
		) VALUES ($1, $2, $3, $4, $5::jsonb)
		ON CONFLICT DO NOTHING`,
		updateID, chatID, messageID, chatType, string(payload),
	)
	return err == nil && command.RowsAffected() == 1, err
}

func (r *ExpenseRepository) ClaimTelegramUpdate(ctx context.Context) (*TelegramExpenseUpdate, error) {
	var value TelegramExpenseUpdate
	err := r.pool.QueryRow(ctx, `
		WITH next_update AS (
			SELECT update_id
			FROM telegram_expense_updates
			WHERE (
				status IN ('queued', 'retry') AND next_attempt_at <= now()
			) OR (
				status = 'processing' AND started_at < now() - interval '10 minutes'
			)
			ORDER BY next_attempt_at, update_id
			FOR UPDATE SKIP LOCKED
			LIMIT 1
		)
		UPDATE telegram_expense_updates u
		SET status = 'processing', attempts = u.attempts + 1,
			started_at = now(), updated_at = now(), last_error = NULL
		FROM next_update n
		WHERE u.update_id = n.update_id
		RETURNING u.update_id, u.chat_id, u.message_id, u.raw_update, u.attempts`,
	).Scan(&value.UpdateID, &value.ChatID, &value.MessageID, &value.Payload, &value.Attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &value, nil
}

func (r *ExpenseRepository) RetryTelegramUpdate(ctx context.Context, updateID int64, cause error) error {
	message := strings.TrimSpace(cause.Error())
	if len(message) > 1000 {
		message = message[:1000]
	}
	_, err := r.pool.Exec(ctx, `
		UPDATE telegram_expense_updates
		SET status = 'retry',
			next_attempt_at = now() + (interval '30 seconds' * least(attempts, 30)),
			last_error = $2, updated_at = now()
		WHERE update_id = $1`, updateID, message)
	return err
}

func (r *ExpenseRepository) IgnoreTelegramUpdate(ctx context.Context, updateID int64, reason string) error {
	reason = strings.TrimSpace(reason)
	if len(reason) > 1000 {
		reason = reason[:1000]
	}
	_, err := r.pool.Exec(ctx, `
		UPDATE telegram_expense_updates
		SET status = 'ignored', last_error = NULLIF($2, ''),
			completed_at = now(), updated_at = now()
		WHERE update_id = $1`, updateID, reason)
	return err
}

func (r *ExpenseRepository) StoreTelegramExtraction(ctx context.Context, updateID int64, extraction json.RawMessage) error {
	if len(extraction) == 0 {
		return nil
	}
	_, err := r.pool.Exec(ctx, `
		UPDATE telegram_expense_updates
		SET extracted_data = $2::jsonb, updated_at = now()
		WHERE update_id = $1`, updateID, string(extraction))
	return err
}

func (r *ExpenseRepository) ReviewTelegramUpdate(ctx context.Context, updateID int64, reason string) error {
	reason = strings.TrimSpace(reason)
	if len(reason) > 1000 {
		reason = reason[:1000]
	}
	_, err := r.pool.Exec(ctx, `
		UPDATE telegram_expense_updates
		SET status = 'needs_review', last_error = NULLIF($2, ''),
			completed_at = now(), updated_at = now()
		WHERE update_id = $1`, updateID, reason)
	return err
}

func (r *ExpenseRepository) RetryTelegramUpdateNow(ctx context.Context, updateID int64) (bool, error) {
	command, err := r.pool.Exec(ctx, `
		UPDATE telegram_expense_updates
		SET status = 'queued', next_attempt_at = now(), started_at = NULL,
			completed_at = NULL, last_error = NULL, updated_at = now()
		WHERE update_id = $1
			AND status IN ('queued', 'retry', 'ignored', 'needs_review', 'failed')
			AND expense_id IS NULL`, updateID)
	return err == nil && command.RowsAffected() == 1, err
}

func (r *ExpenseRepository) ResolveTelegramExpense(
	ctx context.Context,
	updateID int64,
	input ExpenseInput,
) (Expense, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Expense{}, err
	}
	defer tx.Rollback(ctx)

	var status string
	var existingID *string
	if err := tx.QueryRow(ctx, `
		SELECT status, expense_id
		FROM telegram_expense_updates
		WHERE update_id = $1
		FOR UPDATE`, updateID).Scan(&status, &existingID); err != nil {
		return Expense{}, mapNotFound(err)
	}
	if status == "completed" && existingID != nil {
		if err := tx.Commit(ctx); err != nil {
			return Expense{}, err
		}
		return r.GetExpense(ctx, *existingID)
	}
	if existingID != nil {
		return Expense{}, errors.New("Telegram update is already linked to an expense")
	}

	expenseID, err := insertExpense(ctx, tx, input)
	if err != nil {
		return Expense{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE telegram_expense_updates
		SET status = 'completed', expense_id = $2, completed_at = now(),
			last_error = NULL, updated_at = now()
		WHERE update_id = $1`, updateID, expenseID); err != nil {
		return Expense{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Expense{}, err
	}
	return r.GetExpense(ctx, expenseID)
}

func (r *ExpenseRepository) ListTelegramExpenseActivities(
	ctx context.Context,
	pagination Pagination,
	status, search string,
) (TelegramExpenseActivityPage, error) {
	status = strings.TrimSpace(status)
	search = strings.TrimSpace(search)
	const where = `
	WHERE ($1 = '' OR u.status = $1)
		AND ($2 = '' OR concat_ws(' ',
			u.raw_update #>> '{message,chat,title}',
			u.raw_update #>> '{message,from,first_name}',
			u.raw_update #>> '{message,from,last_name}',
			u.raw_update #>> '{message,from,username}',
			u.raw_update #>> '{message,text}',
			u.raw_update #>> '{message,caption}',
			u.raw_update #>> '{message,document,file_name}',
			e.company, e.category, e.unit_number, e.driver_name, e.amount::text,
			e.expense_type, e.description, u.last_error
		) ILIKE '%' || $2 || '%')`

	var total int
	if err := r.pool.QueryRow(ctx, `
		SELECT count(*)
		FROM telegram_expense_updates u
		LEFT JOIN expenses e ON e.id = u.expense_id `+where, status, search).Scan(&total); err != nil {
		return TelegramExpenseActivityPage{}, err
	}
	pagination = pagination.Normalize(total)
	rows, err := r.pool.Query(ctx, `
		SELECT u.update_id, u.chat_id, u.message_id, u.chat_type,
			nullif(u.raw_update #>> '{message,chat,title}', ''),
			nullif(btrim(concat_ws(' ',
				u.raw_update #>> '{message,from,first_name}',
				u.raw_update #>> '{message,from,last_name}',
				CASE WHEN coalesce(u.raw_update #>> '{message,from,username}', '') <> ''
					THEN '@' || (u.raw_update #>> '{message,from,username}') END
			)), ''),
			nullif(coalesce(u.raw_update #>> '{message,text}', u.raw_update #>> '{message,caption}'), ''),
			nullif(coalesce(
				u.raw_update #>> '{message,document,file_name}',
				CASE WHEN jsonb_typeof(u.raw_update #> '{message,photo}') = 'array'
					THEN 'Telegram photo' END
			), ''),
			nullif(coalesce(
				u.raw_update #>> '{message,document,mime_type}',
				CASE WHEN jsonb_typeof(u.raw_update #> '{message,photo}') = 'array'
					THEN 'image/jpeg' END
			), ''),
			nullif(u.raw_update #>> '{message,media_group_id}', ''),
			u.status, u.attempts, u.next_attempt_at, u.started_at, u.completed_at,
			u.last_error, u.extracted_data, u.expense_id,
			to_char(e.expense_date, 'YYYY-MM-DD'), e.company, e.category, e.amount::text,
			coalesce(t.unit_number, e.unit_number), coalesce(d.full_name, e.driver_name),
			e.truck_id, e.driver_id, e.expense_type, e.description,
			u.created_at, u.updated_at
		FROM telegram_expense_updates u
		LEFT JOIN expenses e ON e.id = u.expense_id
		LEFT JOIN trucks t ON t.id = e.truck_id
		LEFT JOIN drivers d ON d.id = e.driver_id `+where+`
		ORDER BY u.created_at DESC, u.update_id DESC
		LIMIT $3 OFFSET $4`, status, search, pagination.PageSize, pagination.Offset())
	if err != nil {
		return TelegramExpenseActivityPage{}, err
	}
	defer rows.Close()
	items := make([]TelegramExpenseActivity, 0, pagination.PageSize)
	for rows.Next() {
		var item TelegramExpenseActivity
		var extracted []byte
		if err := rows.Scan(
			&item.UpdateID, &item.ChatID, &item.MessageID, &item.ChatType,
			&item.ChatTitle, &item.SenderName, &item.MessageText, &item.FileName,
			&item.MIMEType, &item.MediaGroupID, &item.Status, &item.Attempts,
			&item.NextAttemptAt, &item.StartedAt, &item.CompletedAt, &item.LastError,
			&extracted, &item.ExpenseID, &item.ExpenseDate, &item.Company,
			&item.Category, &item.Amount, &item.UnitNumber, &item.DriverName,
			&item.TruckID, &item.DriverID, &item.ExpenseType, &item.Description,
			&item.CreatedAt, &item.UpdatedAt,
		); err != nil {
			return TelegramExpenseActivityPage{}, err
		}
		if len(extracted) > 0 {
			item.ExtractedData = json.RawMessage(extracted)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return TelegramExpenseActivityPage{}, err
	}

	var summary TelegramExpenseActivitySummary
	if err := r.pool.QueryRow(ctx, `
		SELECT
			count(*) FILTER (WHERE u.created_at >= now() - interval '24 hours'),
			count(*) FILTER (WHERE u.status = 'completed'),
			count(*) FILTER (WHERE u.status IN ('queued', 'processing', 'retry')),
			count(*) FILTER (WHERE u.status = 'needs_review'),
			count(*) FILTER (WHERE u.status = 'ignored'),
			count(*) FILTER (WHERE u.status = 'failed'),
			count(*) FILTER (WHERE u.status = 'completed' AND (
				(e.unit_number IS NOT NULL AND e.truck_id IS NULL)
				OR (e.driver_name IS NOT NULL AND e.driver_id IS NULL)
			)),
			max(u.completed_at) FILTER (WHERE u.status = 'completed')
		FROM telegram_expense_updates u
		LEFT JOIN expenses e ON e.id = u.expense_id`).Scan(
		&summary.Received24Hours, &summary.Completed, &summary.InProgress,
		&summary.NeedsReview, &summary.Ignored, &summary.Failed,
		&summary.Unmatched, &summary.LastCompletedAt,
	); err != nil {
		return TelegramExpenseActivityPage{}, err
	}

	return TelegramExpenseActivityPage{
		Page:    NewPage(items, total, pagination),
		Summary: summary,
	}, nil
}

func (r *ExpenseRepository) CompleteTelegramExpense(
	ctx context.Context,
	updateID int64,
	draft TelegramExpenseDraft,
) (string, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)

	var status string
	var existingID *string
	if err := tx.QueryRow(ctx, `
		SELECT status, expense_id
		FROM telegram_expense_updates
		WHERE update_id = $1
		FOR UPDATE`, updateID).Scan(&status, &existingID); err != nil {
		return "", err
	}
	if status == "completed" && existingID != nil {
		return *existingID, tx.Commit(ctx)
	}

	truckID, driverID, err := resolveTelegramExpenseLinks(ctx, tx, draft.UnitNumber, draft.DriverName)
	if err != nil {
		return "", err
	}
	var expenseID string
	err = tx.QueryRow(ctx, `
		INSERT INTO expenses (
			company, category, expense_date, truck_id, driver_id, unit_number, driver_name,
			amount, payment_type, expense_type, reference_number, description,
			covered_by, paid_by, manager_verified, accounting_verified
		) VALUES (
			$1, $2, $3, $4, $5,
			CASE WHEN $4::uuid IS NULL THEN $6 ELSE (SELECT unit_number FROM trucks WHERE id = $4) END,
			CASE WHEN $5::uuid IS NULL THEN $7 ELSE (SELECT full_name FROM drivers WHERE id = $5) END,
			$8::numeric, $9, $10, $11, $12, $13, $14, false, false
		) RETURNING id`,
		draft.Company, draft.Category, draft.ExpenseDate, truckID, driverID,
		draft.UnitNumber, draft.DriverName, draft.Amount, draft.PaymentType, draft.ExpenseType,
		draft.ReferenceNumber, draft.Description, draft.CoveredBy, draft.PaidBy,
	).Scan(&expenseID)
	if err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE telegram_expense_updates
		SET status = 'completed', expense_id = $2, completed_at = now(),
			last_error = NULL, updated_at = now()
		WHERE update_id = $1`, updateID, expenseID); err != nil {
		return "", err
	}
	return expenseID, tx.Commit(ctx)
}

func resolveTelegramExpenseLinks(
	ctx context.Context,
	tx pgx.Tx,
	unitNumber, driverName *string,
) (*string, *string, error) {
	var truckID, driverID *string
	if unitNumber != nil && normalizeTruckUnit(*unitNumber) != "" {
		var id string
		err := tx.QueryRow(ctx, `
			SELECT id FROM trucks
			WHERE upper(regexp_replace(trim(unit_number), '\s+', ' ', 'g')) = $1
			LIMIT 1`, normalizeTruckUnit(*unitNumber)).Scan(&id)
		if err == nil {
			truckID = &id
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, err
		}
	}
	if driverName != nil && normalizeName(*driverName) != "" {
		rows, err := tx.Query(ctx, `SELECT id, full_name FROM drivers WHERE active = true`)
		if err != nil {
			return nil, nil, err
		}
		type candidate struct {
			id      string
			quality int
		}
		best := candidate{}
		second := 0
		for rows.Next() {
			var id, name string
			if err := rows.Scan(&id, &name); err != nil {
				rows.Close()
				return nil, nil, err
			}
			quality := relayDriverNameMatchQuality(*driverName, name)
			if quality > best.quality {
				second = best.quality
				best = candidate{id: id, quality: quality}
			} else if quality > second {
				second = quality
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, nil, err
		}
		rows.Close()
		if best.quality >= 90 && best.quality > second {
			driverID = &best.id
		}
	}

	if truckID != nil && driverID == nil {
		var id string
		err := tx.QueryRow(ctx, `
			SELECT driver_id FROM truck_driver_assignments
			WHERE truck_id = $1 AND unassigned_at IS NULL`, *truckID).Scan(&id)
		if err == nil {
			driverID = &id
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, err
		}
	}
	if driverID != nil && truckID == nil {
		var id string
		err := tx.QueryRow(ctx, `
			SELECT truck_id FROM truck_driver_assignments
			WHERE driver_id = $1 AND unassigned_at IS NULL`, *driverID).Scan(&id)
		if err == nil {
			truckID = &id
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, err
		}
	}
	return truckID, driverID, nil
}
