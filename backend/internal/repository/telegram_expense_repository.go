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
