package repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"mserp/internal/fleetscope"
)

var (
	ErrFleetScopeEventConflict = errors.New("event ID was already used with another payload")
	ErrIntakeCompleted         = errors.New("this hire has already been completed; refresh the list")
	ErrIntakeMatch             = errors.New("possible existing drivers found; review matches before creating a separate driver")
)

type IntakeResult struct {
	Status   string `json:"status"`
	IntakeID string `json:"intakeId"`
}

type DriverIntake struct {
	ID         string            `json:"id"`
	Driver     fleetscope.Driver `json:"driver"`
	ReceivedAt time.Time         `json:"receivedAt"`
	Candidates []IntakeCandidate `json:"candidates"`
}

type IntakeCandidate struct {
	ID       string  `json:"id"`
	FullName string  `json:"fullName"`
	Phone    *string `json:"phone"`
	Email    *string `json:"email"`
}

// DriverDirectoryEntry is for management only; pending hires are excluded from
// accounting and assignment lookup lists until their compensation is reviewed.
type DriverDirectoryEntry struct {
	Driver
	IntakeID string `json:"intakeId,omitempty"`
}

func (r *FleetRepository) GetDriverIntake(ctx context.Context, id string) (DriverIntake, error) {
	var item DriverIntake
	var data []byte
	err := r.pool.QueryRow(ctx, `SELECT id, driver_data, received_at FROM fleetscope_driver_intake WHERE id=$1 AND completed_at IS NULL`, id).Scan(&item.ID, &data, &item.ReceivedAt)
	if err != nil {
		return item, mapNotFound(err)
	}
	if err = json.Unmarshal(data, &item.Driver); err != nil {
		return item, err
	}
	item.Candidates, err = intakeCandidates(ctx, r.pool, item.Driver)
	return item, err
}

func (r *FleetRepository) ListDriverDirectory(ctx context.Context, pagination Pagination, search string, includeInactive bool) (Page[DriverDirectoryEntry], error) {
	const directory = `WITH directory AS (
	 SELECT d.id, d.full_name, false AS pending, NULL::jsonb AS data, d.created_at AS received_at
	 FROM drivers d
	 LEFT JOIN dispatchers dp ON dp.id=d.dispatcher_id
	 LEFT JOIN truck_driver_assignments a ON a.driver_id=d.id AND a.unassigned_at IS NULL
	 LEFT JOIN trucks t ON t.id=a.truck_id
	 WHERE ($1='' OR concat_ws(' ',d.full_name,d.email,d.phone,t.unit_number,dp.full_name,d.license_number) ILIKE '%' || $1 || '%') AND ($2 OR d.active)
	 UNION ALL
	 SELECT id, driver_data->>'fullName', true, driver_data, received_at FROM fleetscope_driver_intake
	 WHERE completed_at IS NULL AND ($1='' OR concat_ws(' ',driver_data->>'fullName',driver_data->>'email',driver_data->>'phone',driver_data->>'licenseNumber') ILIKE '%' || $1 || '%')
	) `
	var total int
	if err := r.pool.QueryRow(ctx, directory+`SELECT count(*) FROM directory`, search, includeInactive).Scan(&total); err != nil {
		return Page[DriverDirectoryEntry]{}, err
	}
	pagination = pagination.Normalize(total)
	rows, err := r.pool.Query(ctx, directory+`SELECT id,pending,data,received_at FROM directory ORDER BY pending DESC,full_name,id LIMIT $3 OFFSET $4`, search, includeInactive, pagination.PageSize, pagination.Offset())
	if err != nil {
		return Page[DriverDirectoryEntry]{}, err
	}
	items := make([]DriverDirectoryEntry, 0, pagination.PageSize)
	ids := make([]string, 0, pagination.PageSize)
	for rows.Next() {
		var item DriverDirectoryEntry
		var pending bool
		var data []byte
		if err = rows.Scan(&item.ID, &pending, &data, &item.CreatedAt); err != nil {
			break
		}
		if pending {
			var source fleetscope.Driver
			if err = json.Unmarshal(data, &source); err != nil {
				break
			}
			item.IntakeID = item.ID
			item.FullName = formatPersonName(source.FullName)
			item.IsOwnerOperator = source.DriverType == "owner_operator"
			item.Phone = &source.Phone
			item.Email = &source.Email
		} else {
			ids = append(ids, item.ID)
		}
		items = append(items, item)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return Page[DriverDirectoryEntry]{}, err
	}
	if len(ids) > 0 {
		rows, err = r.pool.Query(ctx, selectDriversSQL+` WHERE d.id=ANY($1::uuid[])`, ids)
		if err != nil {
			return Page[DriverDirectoryEntry]{}, err
		}
		byID := make(map[string]Driver)
		for rows.Next() {
			var d Driver
			d, err = scanDriver(rows)
			if err != nil {
				break
			}
			byID[d.ID] = d
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
		if err != nil {
			return Page[DriverDirectoryEntry]{}, err
		}
		for i := range items {
			if items[i].IntakeID == "" {
				if d, ok := byID[items[i].ID]; ok {
					items[i].Driver = d
				}
			}
		}
	}
	return NewPage(items, total, pagination), nil
}

// AcceptFleetScopeHire acknowledges only after both the immutable intake and
// receipt commit. Neither new IDs nor retries ever refresh a driver's profile.
func (r *FleetRepository) AcceptFleetScopeHire(ctx context.Context, event fleetscope.Event, hash string) (IntakeResult, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return IntakeResult{}, err
	}
	defer tx.Rollback(ctx)
	// Low-volume integration: serialize receipts to handle concurrent deliveries
	// of both the same event and different events referring to the same driver.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('fleetscope:intake', 0))`); err != nil {
		return IntakeResult{}, err
	}
	result := IntakeResult{Status: "duplicate"}
	var existingHash string
	err = tx.QueryRow(ctx, `SELECT intake_id, body_sha256 FROM fleetscope_webhook_receipts WHERE event_id=$1`, event.EventID).Scan(&result.IntakeID, &existingHash)
	if err == nil {
		if hash != existingHash {
			return IntakeResult{}, ErrFleetScopeEventConflict
		}
		return result, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return IntakeResult{}, err
	}
	data, err := json.Marshal(event.Driver)
	if err != nil {
		return IntakeResult{}, err
	}
	err = tx.QueryRow(ctx, `INSERT INTO fleetscope_driver_intake(company_id, fleetscope_driver_id, driver_data, normalized_name, occurred_at)
		VALUES($1,$2,$3,$4,$5) ON CONFLICT(company_id, fleetscope_driver_id) DO NOTHING RETURNING id`,
		event.CompanyID, event.Driver.ID, data, normalizeName(event.Driver.FullName), event.OccurredAt).Scan(&result.IntakeID)
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, `SELECT id FROM fleetscope_driver_intake WHERE company_id=$1 AND fleetscope_driver_id=$2`, event.CompanyID, event.Driver.ID).Scan(&result.IntakeID)
	} else if err == nil {
		result.Status = "accepted"
	}
	if err != nil {
		return IntakeResult{}, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO fleetscope_webhook_receipts(event_id, body_sha256, intake_id) VALUES($1,$2,$3)`, event.EventID, hash, result.IntakeID)
	if err != nil {
		return IntakeResult{}, err
	}
	return result, tx.Commit(ctx)
}

func (r *FleetRepository) ListDriverIntake(ctx context.Context, pagination Pagination, searches ...string) (Page[DriverIntake], error) {
	search := ""
	if len(searches) > 0 {
		search = searches[0]
	}
	const filter = ` WHERE completed_at IS NULL AND ($1='' OR concat_ws(' ',driver_data->>'fullName',driver_data->>'email',driver_data->>'phone') ILIKE '%' || $1 || '%')`
	var total int
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM fleetscope_driver_intake`+filter, search).Scan(&total); err != nil {
		return Page[DriverIntake]{}, err
	}
	pagination = pagination.Normalize(total)
	rows, err := r.pool.Query(ctx, `SELECT id, driver_data, received_at FROM fleetscope_driver_intake`+filter+` ORDER BY received_at, id LIMIT $2 OFFSET $3`, search, pagination.PageSize, pagination.Offset())
	if err != nil {
		return Page[DriverIntake]{}, err
	}
	items := make([]DriverIntake, 0)
	for rows.Next() {
		var item DriverIntake
		var data []byte
		if err = rows.Scan(&item.ID, &data, &item.ReceivedAt); err != nil {
			rows.Close()
			return Page[DriverIntake]{}, err
		}
		if err = json.Unmarshal(data, &item.Driver); err != nil {
			rows.Close()
			return Page[DriverIntake]{}, err
		}
		items = append(items, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return Page[DriverIntake]{}, err
	}
	for i := range items {
		items[i].Candidates, err = intakeCandidates(ctx, r.pool, items[i].Driver)
		if err != nil {
			return Page[DriverIntake]{}, err
		}
	}
	return NewPage(items, total, pagination), nil
}

type intakeQuerier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

// Candidates are suggestions for human review, never automatic identity merges.
func intakeCandidates(ctx context.Context, db intakeQuerier, d fleetscope.Driver) ([]IntakeCandidate, error) {
	rows, err := db.Query(ctx, `SELECT id, full_name, phone, email FROM drivers
		WHERE normalized_name=$1
		OR ($2<>'' AND lower(trim(email))=lower(trim($2)))
		OR ($3<>'' AND regexp_replace(phone, '[^0-9]', '', 'g')=regexp_replace($3, '[^0-9]', '', 'g'))
		OR ($4<>'' AND $5<>'' AND upper(trim(license_number))=upper(trim($4)) AND upper(trim(license_state))=upper(trim($5)))
		ORDER BY full_name, id`, normalizeName(d.FullName), d.Email, d.Phone, d.LicenseNumber, d.LicenseState)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]IntakeCandidate, 0)
	for rows.Next() {
		var candidate IntakeCandidate
		if err := rows.Scan(&candidate.ID, &candidate.FullName, &candidate.Phone, &candidate.Email); err != nil {
			return nil, err
		}
		result = append(result, candidate)
	}
	return result, rows.Err()
}

func (r *FleetRepository) CompleteDriverIntake(ctx context.Context, id, userID, linkDriverID string, input *DriverInput, separateConfirmed bool) (Driver, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Driver{}, err
	}
	defer tx.Rollback(ctx)
	// Coordinate identity review with another simultaneous intake completion.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('fleetscope:complete', 0))`); err != nil {
		return Driver{}, err
	}
	var data []byte
	var completedAt *time.Time
	if err = tx.QueryRow(ctx, `SELECT driver_data, completed_at FROM fleetscope_driver_intake WHERE id=$1 FOR UPDATE`, id).Scan(&data, &completedAt); err != nil {
		return Driver{}, mapNotFound(err)
	}
	if completedAt != nil {
		return Driver{}, ErrIntakeCompleted
	}
	if linkDriverID != "" {
		// Lock the existing record against deletion. Linking is deliberately a
		// no-op on its identity, compensation, assignments, and active status.
		var existing string
		if err = tx.QueryRow(ctx, `SELECT id FROM drivers WHERE id=$1 FOR UPDATE`, linkDriverID).Scan(&existing); err != nil {
			return Driver{}, mapNotFound(err)
		}
	} else {
		if input == nil {
			return Driver{}, errors.New("driver input is required")
		}
		var source fleetscope.Driver
		if err = json.Unmarshal(data, &source); err != nil {
			return Driver{}, err
		}
		candidates, err := intakeCandidates(ctx, tx, source)
		if err != nil {
			return Driver{}, err
		}
		if len(candidates) > 0 && !separateConfirmed {
			return Driver{}, ErrIntakeMatch
		}
		linkDriverID, err = createDriverTx(ctx, tx, *input)
		if err != nil {
			return Driver{}, err
		}
	}
	_, err = tx.Exec(ctx, `UPDATE fleetscope_driver_intake SET driver_id=$2, completed_at=now(), completed_by=$3 WHERE id=$1`, id, linkDriverID, userID)
	if err != nil {
		return Driver{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Driver{}, err
	}
	return r.GetDriver(ctx, linkDriverID)
}
