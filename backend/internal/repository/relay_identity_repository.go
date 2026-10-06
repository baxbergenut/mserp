package repository

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"mserp/internal/phone"

	"github.com/jackc/pgx/v5"
)

var ErrRelayReviewConflict = errors.New("this Relay account has already been linked; refresh the task list")

type RelaySuggestion struct {
	DriverID string   `json:"driverId"`
	Name     string   `json:"name"`
	Email    *string  `json:"email"`
	Phone    *string  `json:"phone"`
	Active   bool     `json:"active"`
	Reasons  []string `json:"reasons"`
	score    int
}

type RelayIdentityTask struct {
	ID                string            `json:"id"`
	Environment       string            `json:"environment"`
	RelayDriverID     string            `json:"relayDriverId"`
	IntegrationID     *string           `json:"integrationId"`
	Name              string            `json:"name"`
	Email             *string           `json:"email"`
	Phone             *string           `json:"phone"`
	TransactionCount  int               `json:"transactionCount"`
	LatestTransaction *time.Time        `json:"latestTransaction"`
	Suggestions       []RelaySuggestion `json:"suggestions"`
	RejectedDriverIDs []string          `json:"rejectedDriverIds"`
}

func relaySuggestions(task RelayIdentityTask, drivers []RelaySuggestion) []RelaySuggestion {
	result := make([]RelaySuggestion, 0)
	rejected := map[string]bool{}
	for _, id := range task.RejectedDriverIDs {
		rejected[id] = true
	}
	for _, d := range drivers {
		if rejected[d.DriverID] {
			continue
		}
		quality := relayDriverNameMatchQuality(task.Name, d.Name)
		email := normalizeEmail(stringValue(task.Email))
		emailMatch := email != "" && email == normalizeEmail(stringValue(d.Email))
		phoneMatch := phoneSetsOverlap(phoneKeys(stringValue(task.Phone)), phoneKeys(stringValue(d.Phone)))
		if quality < 80 && !emailMatch && !phoneMatch {
			continue
		}
		d.Reasons = make([]string, 0)
		d.score = quality
		if emailMatch {
			d.Reasons = append(d.Reasons, "Same email")
			d.score += 200
		}
		if phoneMatch {
			d.Reasons = append(d.Reasons, "Same phone")
			d.score += 200
		}
		if quality >= 90 {
			d.Reasons = append(d.Reasons, "Compatible name (order / middle names / suffixes)")
		} else if quality >= 80 {
			d.Reasons = append(d.Reasons, "Similar name spelling")
		} else {
			d.Reasons = append(d.Reasons, "Names differ — verify shared contact before linking")
		}
		result = append(result, d)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].score != result[j].score {
			return result[i].score > result[j].score
		}
		if result[i].Name != result[j].Name {
			return result[i].Name < result[j].Name
		}
		return result[i].DriverID < result[j].DriverID
	})
	if len(result) > 5 {
		result = result[:5]
	}
	return result
}

func (r *FuelRepository) RelayIdentityTasks(ctx context.Context, pagination Pagination, search string) (Page[RelayIdentityTask], error) {
	search = phone.Search(search)
	// A consistent read also releases all rows before the next query, supporting a single-connection pool.
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return Page[RelayIdentityTask]{}, err
	}
	defer tx.Rollback(ctx)
	visible, err := systemTaskVisible(ctx, tx, "relay_review", false)
	if err != nil {
		return Page[RelayIdentityTask]{}, err
	}
	if !visible {
		return NewPage([]RelayIdentityTask{}, 0, pagination.Normalize(0)), nil
	}
	const where = ` WHERE l.driver_id IS NULL AND
 ($1 = '' OR concat_ws(' ',l.relay_first_name,l.relay_last_name,l.relay_email,l.relay_phone,l.relay_driver_id,l.relay_integration_id) ILIKE '%' || $1 || '%')`
	var total int
	if err = tx.QueryRow(ctx, "SELECT count(*) FROM relay_driver_links l"+where, search).Scan(&total); err != nil {
		return Page[RelayIdentityTask]{}, err
	}
	pagination = pagination.Normalize(total)
	rows, err := tx.Query(ctx, `SELECT l.id,l.relay_environment,l.relay_driver_id,l.relay_integration_id,
 concat_ws(' ',l.relay_first_name,l.relay_last_name),l.relay_email,l.relay_phone,
 (SELECT count(*) FROM fuel_transactions t WHERE t.relay_environment=l.relay_environment AND t.relay_driver_id=l.relay_driver_id),
 (SELECT max(purchased_at) FROM fuel_transactions t WHERE t.relay_environment=l.relay_environment AND t.relay_driver_id=l.relay_driver_id),
 ARRAY(SELECT driver_id::text FROM relay_identity_reviews v WHERE v.identity_id=l.id AND v.action='reject')
 FROM relay_driver_links l`+where+` ORDER BY l.created_at,l.id LIMIT $2 OFFSET $3`, search, pagination.PageSize, pagination.Offset())
	if err != nil {
		return Page[RelayIdentityTask]{}, err
	}
	tasks := make([]RelayIdentityTask, 0)
	for rows.Next() {
		var task RelayIdentityTask
		if err = rows.Scan(&task.ID, &task.Environment, &task.RelayDriverID, &task.IntegrationID, &task.Name, &task.Email, &task.Phone, &task.TransactionCount, &task.LatestTransaction, &task.RejectedDriverIDs); err != nil {
			rows.Close()
			return Page[RelayIdentityTask]{}, err
		}
		task.Name = formatPersonName(task.Name)
		tasks = append(tasks, task)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return Page[RelayIdentityTask]{}, err
	}
	drivers := make([]RelaySuggestion, 0)
	if len(tasks) > 0 {
		rows, err = tx.Query(ctx, "SELECT id,full_name,email,phone,active FROM drivers ORDER BY full_name,id")
		if err != nil {
			return Page[RelayIdentityTask]{}, err
		}
		for rows.Next() {
			var d RelaySuggestion
			if err = rows.Scan(&d.DriverID, &d.Name, &d.Email, &d.Phone, &d.Active); err != nil {
				rows.Close()
				return Page[RelayIdentityTask]{}, err
			}
			drivers = append(drivers, d)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return Page[RelayIdentityTask]{}, err
		}
	}
	for i := range tasks {
		tasks[i].Suggestions = relaySuggestions(tasks[i], drivers)
	}
	if err = tx.Commit(ctx); err != nil {
		return Page[RelayIdentityTask]{}, err
	}
	return NewPage(tasks, total, pagination), nil
}

// ReviewRelayIdentity serializes with import's identity upsert. The mapping,
// historical transactions, and audit entry either all commit or all roll back.
func (r *FuelRepository) ReviewRelayIdentity(ctx context.Context, identityID, driverID, action, userID string) (int64, error) {
	if action != "link" && action != "reject" {
		return 0, errors.New("invalid review action")
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	visible, err := systemTaskVisible(ctx, tx, "relay_review", true)
	if err != nil {
		return 0, err
	}
	if !visible {
		return 0, ErrNotFound
	}
	var environment, relayID string
	var current *string
	err = tx.QueryRow(ctx, `SELECT relay_environment,relay_driver_id,driver_id FROM relay_driver_links WHERE id=$1 FOR UPDATE`, identityID).Scan(&environment, &relayID, &current)
	if err != nil {
		return 0, mapNotFound(err)
	}
	// Normalize UUID text through PostgreSQL before idempotency comparison.
	var target string
	err = tx.QueryRow(ctx, "SELECT id::text FROM drivers WHERE id=$1 FOR KEY SHARE", driverID).Scan(&target)
	if err != nil {
		return 0, mapNotFound(err)
	}
	if current != nil && (action != "link" || *current != target) {
		return 0, ErrRelayReviewConflict
	}
	var affected int64
	if action == "link" {
		// Refuse to silently steal previously attributed purchases.
		var conflicting bool
		err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM fuel_transactions WHERE relay_environment=$1 AND relay_driver_id=$2 AND driver_id IS NOT NULL AND driver_id<>$3)`, environment, relayID, target).Scan(&conflicting)
		if err != nil {
			return 0, err
		}
		if conflicting {
			return 0, ErrRelayReviewConflict
		}
		if _, err = tx.Exec(ctx, "UPDATE relay_driver_links SET driver_id=$2,updated_at=now() WHERE id=$1", identityID, target); err != nil {
			return 0, err
		}
		command, err := tx.Exec(ctx, `UPDATE fuel_transactions SET driver_id=$3 WHERE relay_environment=$1 AND relay_driver_id=$2 AND driver_id IS NULL`, environment, relayID, target)
		if err != nil {
			return 0, err
		}
		affected = command.RowsAffected()
	}
	_, err = tx.Exec(ctx, `INSERT INTO relay_identity_reviews(identity_id,driver_id,action,reviewed_by) VALUES($1,$2,$3,$4) ON CONFLICT(identity_id,driver_id,action) DO NOTHING`, identityID, target, action, strings.TrimSpace(userID))
	if err != nil {
		return 0, err
	}
	return affected, tx.Commit(ctx)
}
