package repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ExpenseRepository struct {
	pool *pgxpool.Pool
}

func NewExpenseRepository(pool *pgxpool.Pool) *ExpenseRepository {
	return &ExpenseRepository{pool: pool}
}

type ExpensePayment struct {
	WeekStart string `json:"weekStart"`
	Amount    string `json:"amount"`
}
type ExpenseVerification struct {
	ID         string     `json:"id"`
	Role       string     `json:"role"`
	UserID     *string    `json:"userId"`
	UserName   string     `json:"userName"`
	VerifiedAt *time.Time `json:"verifiedAt"`
}

type ExpenseCostItem struct {
	Name   string `json:"name"`
	Amount string `json:"amount"`
}

type Expense struct {
	Maintenance          *ExpenseMaintenance   `json:"maintenance,omitempty"`
	CreatorRoleID        string                `json:"-"`
	CanEdit              bool                  `json:"canEdit"`
	ApplicantID          *string               `json:"applicantId"`
	CostBreakdown        []ExpenseCostItem     `json:"costBreakdown"`
	BaseAmount           *string               `json:"baseAmount"`
	PaymentFee           string                `json:"paymentFee"`
	Verifications        []ExpenseVerification `json:"verifications"`
	Payments             []ExpensePayment      `json:"payments"`
	OwnerID              *string               `json:"ownerId"`
	OwnerName            *string               `json:"ownerName"`
	ChargeDriverID       *string               `json:"chargeDriverId"`
	PaidAmount           *string               `json:"paidAmount"`
	RemainingAmount      *string               `json:"remainingAmount"`
	DriverSettled        bool                  `json:"driverSettled"`
	ID                   string                `json:"id"`
	TruckID              *string               `json:"truckId"`
	DriverID             *string               `json:"driverId"`
	Category             string                `json:"category"`
	CategoryID           string                `json:"categoryId"`
	CreatedByName        *string               `json:"createdByName"`
	WeekStart            *string               `json:"weekStart"`
	ExpenseDate          *string               `json:"expenseDate"`
	UnitNumber           *string               `json:"unitNumber"`
	DriverName           *string               `json:"driverName"`
	Amount               *string               `json:"amount"`
	PaymentType          *string               `json:"paymentType"`
	ExpenseType          *string               `json:"expenseType"`
	ReferenceNumber      *string               `json:"referenceNumber"`
	Description          *string               `json:"description"`
	WorkOrderDescription *string               `json:"workOrderDescription"`
	CoveredBy            *string               `json:"coveredBy"`
	PaidBy               *string               `json:"paidBy"`
	AccountingVerified   bool                  `json:"accountingVerified"`
	SourceSpreadsheetID  *string               `json:"sourceSpreadsheetId"`
	SourceSheet          *string               `json:"sourceSheet"`
	SourceRow            *int                  `json:"sourceRow"`
	CreatedAt            time.Time             `json:"createdAt"`
	UpdatedAt            time.Time             `json:"updatedAt"`
}

type ExpenseInput struct {
	Maintenance        *ExpenseMaintenance
	ApplicantID        *string
	CostBreakdown      *[]ExpenseCostItem
	BaseAmount         *string
	PaymentFee         string
	UpdatedAt          *time.Time
	OwnerID            *string
	CategoryID         string
	CreatedBy          string
	CreatedByName      string
	ExpenseDate        time.Time
	TruckID            *string
	DriverID           *string
	UnitNumber         *string
	DriverName         *string
	Amount             string
	PaymentType        *string
	ExpenseType        *string
	ReferenceNumber    *string
	Description        *string
	CoveredBy          *string
	PaidBy             *string
	AccountingVerified bool
}

type ExpensePageQuery struct {
	PaymentType           string
	PaidBy                string
	CoveredBy             string
	UnitNumber            string
	DriverName            string
	AccountantVerified    *bool
	Responsibility        string
	ChargeDriverID        *string
	Pagination            Pagination
	Search                string
	CategoryID            string
	VisibleCategoryIDs    []string
	AccessibleCategoryIDs []string
	DateFrom              *time.Time
	DateTo                *time.Time
	TruckID               *string
	DriverID              *string
}

type ExpenseFilterValues struct {
	Drivers      []string `json:"drivers"`
	Trucks       []string `json:"trucks"`
	PaymentTypes []string `json:"paymentTypes"`
	PaidBy       []string `json:"paidBy"`
	CoveredBy    []string `json:"coveredBy"`
}

type ExpenseFilterOptions struct {
	Applicants           []ExpenseApplicant      `json:"applicants"`
	WorkOrdersCategoryID string                  `json:"workOrdersCategoryId"`
	Filters              ExpenseFilterValues     `json:"filters"`
	Settings             []ExpenseSetting        `json:"settings"`
	Categories           []ExpenseFilterCategory `json:"categories"`
	PaymentTypes         []string                `json:"paymentTypes"`
	ExpenseTypes         []string                `json:"expenseTypes"`
	PaidBy               []string                `json:"paidBy"`
	CoveredBy            []string                `json:"coveredBy"`
}

type ExpenseFilterCategory struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type ExpenseSummary struct {
	Amount          string `json:"amount"`
	IncompleteCount int    `json:"incompleteCount"`
}

type ExpensePage struct {
	Page[Expense]
	Options ExpenseFilterOptions `json:"options"`
	Summary ExpenseSummary       `json:"summary"`
}

func (r *ExpenseRepository) ListExpensesPage(ctx context.Context, query ExpensePageQuery) (ExpensePage, error) {
	const where = `
WHERE ($1 = '' OR concat_ws(' ', e.category, (SELECT name FROM expense_settings WHERE id=e.category_id), COALESCE(t.unit_number, e.unit_number),
		COALESCE(d.full_name, e.driver_name), e.payment_type, e.expense_type,
		e.reference_number, e.description, e.covered_by, e.paid_by)
	ILIKE '%' || $1 || '%')
	AND e.category_id=ANY($2::uuid[])
	AND ($3 = '' OR e.category_id = $3::uuid)
	AND ($4 = '' OR e.payment_type = $4)
	AND ($5::date IS NULL OR e.expense_date >= $5)
	AND ($6::date IS NULL OR e.expense_date <= $6)
	AND ($7::uuid IS NULL OR e.truck_id = $7)
	AND ($8::uuid IS NULL OR COALESCE(e.driver_id,expense_applicant_driver(e.applicant_id)) = $8)
 AND ($9::uuid IS NULL OR e.charge_driver_id=$9)
 AND ($10='' OR ($10='non_personal' AND e.charge_driver_id IS DISTINCT FROM $8::uuid))
 AND ($11::boolean IS NULL OR e.accounting_verified=$11)
 AND ($12='' OR e.paid_by=$12)
 AND ($13='' OR e.covered_by=$13)
 AND ($14='' OR COALESCE(t.unit_number,e.unit_number)=$14)
 AND ($15='' OR COALESCE(d.full_name,e.driver_name)=$15)`
	args := []any{
		query.Search, query.VisibleCategoryIDs, query.CategoryID, query.PaymentType, query.DateFrom, query.DateTo,
		query.TruckID, query.DriverID, query.ChargeDriverID, query.Responsibility,
		query.AccountantVerified, query.PaidBy, query.CoveredBy, query.UnitNumber, query.DriverName,
	}

	var total int
	summary := ExpenseSummary{}
	if err := r.pool.QueryRow(ctx, `
		SELECT count(*), COALESCE(sum(amount), 0)::text,
			count(*) FILTER (WHERE expense_date IS NULL OR amount IS NULL)
		FROM expenses e
		LEFT JOIN trucks t ON t.id = e.truck_id
		LEFT JOIN drivers d ON d.id = COALESCE(e.driver_id,expense_applicant_driver(e.applicant_id)) `+where, args...).Scan(
		&total, &summary.Amount, &summary.IncompleteCount,
	); err != nil {
		return ExpensePage{}, err
	}

	query.Pagination = query.Pagination.Normalize(total)
	pageArgs := append(args, query.Pagination.PageSize, query.Pagination.Offset())
	rows, err := r.pool.Query(ctx, selectExpensesSQL+where+`
		ORDER BY e.expense_date DESC NULLS LAST, e.created_at DESC, e.id
		LIMIT $16 OFFSET $17`, pageArgs...)
	if err != nil {
		return ExpensePage{}, err
	}
	defer rows.Close()

	values := make([]Expense, 0, query.Pagination.PageSize)
	for rows.Next() {
		value, scanErr := scanExpense(rows)
		if scanErr != nil {
			return ExpensePage{}, scanErr
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return ExpensePage{}, err
	}
	if err := r.loadWorkOrderDescriptions(ctx, values); err != nil {
		return ExpensePage{}, err
	}

	options := ExpenseFilterOptions{Categories: []ExpenseFilterCategory{}}
	options.Applicants, err = r.ListExpenseApplicants(ctx)
	if err != nil {
		return ExpensePage{}, err
	}
	if err := r.pool.QueryRow(ctx, `SELECT
 COALESCE(array_agg(DISTINCT COALESCE(d.full_name,e.driver_name) ORDER BY COALESCE(d.full_name,e.driver_name)) FILTER(WHERE COALESCE(d.full_name,e.driver_name,'')<>''),'{}'),
 COALESCE(array_agg(DISTINCT COALESCE(t.unit_number,e.unit_number) ORDER BY COALESCE(t.unit_number,e.unit_number)) FILTER(WHERE COALESCE(t.unit_number,e.unit_number,'')<>''),'{}'),
 COALESCE(array_agg(DISTINCT e.payment_type ORDER BY e.payment_type) FILTER(WHERE coalesce(e.payment_type,'')<>''),'{}'),
 COALESCE(array_agg(DISTINCT e.paid_by ORDER BY e.paid_by) FILTER(WHERE coalesce(e.paid_by,'')<>''),'{}'),
 COALESCE(array_agg(DISTINCT e.covered_by ORDER BY e.covered_by) FILTER(WHERE coalesce(e.covered_by,'')<>''),'{}')
 FROM expenses e LEFT JOIN trucks t ON t.id=e.truck_id LEFT JOIN drivers d ON d.id=COALESCE(e.driver_id,expense_applicant_driver(e.applicant_id))
 WHERE e.category_id=ANY($1::uuid[])`, query.VisibleCategoryIDs).Scan(
		&options.Filters.Drivers, &options.Filters.Trucks, &options.Filters.PaymentTypes, &options.Filters.PaidBy, &options.Filters.CoveredBy,
	); err != nil {
		return ExpensePage{}, err
	}
	options.CoveredBy = options.Filters.CoveredBy

	settings, err := r.ListExpenseSettings(ctx)
	if err != nil {
		return ExpensePage{}, err
	}
	accessible := map[string]bool{}
	for _, id := range query.AccessibleCategoryIDs {
		accessible[id] = true
	}
	visible := map[string]bool{}
	for _, id := range query.VisibleCategoryIDs {
		visible[id] = true
	}
	for _, item := range settings {
		if item.Kind == "category" && accessible[item.ID] {
			options.Settings = append(options.Settings, item)
			if visible[item.ID] {
				options.Categories = append(options.Categories, ExpenseFilterCategory{ID: item.ID, Name: item.Name})
			}
		}
		if item.Kind == "name" && item.CategoryID != nil && accessible[*item.CategoryID] {
			options.Settings = append(options.Settings, item)
		}
		if item.Kind == "payment_method" || item.Kind == "payer" {
			options.Settings = append(options.Settings, item)
		}
	}
	options.PaymentTypes = []string{}
	if err := r.pool.QueryRow(ctx, `SELECT COALESCE(array_agg(username ORDER BY lower(username)), '{}') FROM app_users WHERE active`).Scan(&options.PaidBy); err != nil {
		return ExpensePage{}, err
	}
	// Keep the legacy field; defaults now come from category-linked settings.
	options.ExpenseTypes = []string{}
	for _, item := range options.Settings {
		if !item.Active {
			continue
		}
		switch item.Kind {
		case "payment_method":
			options.PaymentTypes = append(options.PaymentTypes, item.Name)
		}
	}
	return ExpensePage{
		Page:    NewPage(values, total, query.Pagination),
		Options: options,
		Summary: summary,
	}, nil
}

func (r *ExpenseRepository) GetExpense(ctx context.Context, id string) (Expense, error) {
	value, err := scanExpense(r.pool.QueryRow(ctx, selectExpensesSQL+" WHERE e.id = $1", id))
	if err != nil {
		return value, mapNotFound(err)
	}
	values := []Expense{value}
	err = r.loadWorkOrderDescriptions(ctx, values)
	return values[0], err
}

// Load display text separately so legacy expense-only schemas remain readable.
// A page uses one bounded lookup and never replaces the saved invoice description.
func (r *ExpenseRepository) loadWorkOrderDescriptions(ctx context.Context, values []Expense) error {
	if len(values) == 0 {
		return nil
	}
	ids := make([]string, len(values))
	byID := make(map[string]int, len(values))
	for i := range values {
		ids[i] = values[i].ID
		byID[values[i].ID] = i
	}
	rows, err := r.pool.Query(ctx, `SELECT expense_id::text, nullif(btrim(notes),''), jsonb_build_object('recordId',id,'version',version,'odometer',odometer,'vendorId',vendor_id,'vendorName',vendor_name,'vendorAddress',vendor_address,'vendorPhone',vendor_phone,'vendorContact',vendor_contact,'oilType',coalesce(to_jsonb(m)->>'oil_type','')), (SELECT s.name FROM expense_settings s JOIN maintenance_configuration c ON c.category_id=s.id) FROM maintenance_records m WHERE expense_id=ANY($1::uuid[])`, ids)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "42P01" {
			return nil // Work orders were introduced in migration 096.
		}
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var description *string
		var maintenance ExpenseMaintenance
		var category string
		if err := rows.Scan(&id, &description, &maintenance, &category); err != nil {
			return err
		}
		if i, ok := byID[id]; ok {
			values[i].WorkOrderDescription = description
			values[i].Maintenance = &maintenance
			values[i].Category = category
		}
	}
	return rows.Err()
}

func (r *ExpenseRepository) CreateExpense(ctx context.Context, input ExpenseInput) (Expense, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Expense{}, err
	}
	defer tx.Rollback(ctx)
	if err = prepareExpenseMaintenance(ctx, tx, input); err != nil {
		return Expense{}, err
	}
	id, err := insertExpense(ctx, tx, input)
	if err != nil {
		return Expense{}, err
	}
	if err = syncExpenseMaintenance(ctx, tx, id, input, nil); err != nil {
		return Expense{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Expense{}, err
	}
	return r.GetExpense(ctx, id)
}

func (r *ExpenseRepository) CreateExpenses(ctx context.Context, inputs []ExpenseInput) ([]Expense, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	values := make([]Expense, 0, len(inputs))
	for _, input := range inputs {
		if err = prepareExpenseMaintenance(ctx, tx, input); err != nil {
			return nil, err
		}
		id, insertErr := insertExpense(ctx, tx, input)
		if insertErr != nil {
			return nil, insertErr
		}
		if err = syncExpenseMaintenance(ctx, tx, id, input, nil); err != nil {
			return nil, err
		}
		value, scanErr := scanExpense(tx.QueryRow(ctx, selectExpensesSQL+" WHERE e.id = $1", id))
		if scanErr != nil {
			return nil, scanErr
		}
		values = append(values, value)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	if err = r.loadWorkOrderDescriptions(ctx, values); err != nil {
		return nil, err
	}
	return values, nil
}

func (r *ExpenseRepository) ResolveExpenseLinks(ctx context.Context, unitNumber, driverName *string) (*string, *string, error) {
	truck, driver, _, err := r.ResolveExpenseDraftLinks(ctx, unitNumber, driverName)
	return truck, driver, err
}

func (r *ExpenseRepository) ResolveExpenseDraftLinks(
	ctx context.Context,
	unitNumber, driverName *string,
) (*string, *string, *string, error) {
	var truckID, driverID, applicantID *string
	driverNameProvided := driverName != nil && normalizeName(*driverName) != ""
	if unitNumber != nil && normalizeTruckUnit(*unitNumber) != "" {
		var id string
		err := r.pool.QueryRow(ctx, `
			SELECT id FROM trucks
			WHERE upper(regexp_replace(trim(unit_number), '\s+', ' ', 'g')) = $1
			LIMIT 1`, normalizeTruckUnit(*unitNumber)).Scan(&id)
		if err == nil {
			truckID = &id
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, nil, err
		}
	}
	if driverNameProvided {
		rows, err := r.pool.Query(ctx, `SELECT id, full_name, false FROM drivers WHERE active = true
 UNION ALL SELECT id, full_name, true FROM fleetscope_applicants WHERE expense_applicant_driver(id) IS NULL`)
		if err != nil {
			return nil, nil, nil, err
		}
		type candidate struct {
			id        string
			quality   int
			applicant bool
		}
		best := candidate{}
		second := 0
		for rows.Next() {
			var id, name string
			var applicant bool
			if err := rows.Scan(&id, &name, &applicant); err != nil {
				rows.Close()
				return nil, nil, nil, err
			}
			quality := relayDriverNameMatchQuality(*driverName, name)
			if quality > best.quality {
				second = best.quality
				best = candidate{id: id, quality: quality, applicant: applicant}
			} else if quality > second {
				second = quality
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, nil, nil, err
		}
		rows.Close()
		if best.quality >= 90 && best.quality > second {
			if best.applicant {
				applicantID = &best.id
			} else {
				driverID = &best.id
			}
		}
	}

	// An explicit unmatched or ambiguous name requires review, even on a linked truck.
	if truckID != nil && driverID == nil && !driverNameProvided {
		var id string
		err := r.pool.QueryRow(ctx, `
			SELECT driver_id FROM truck_driver_assignments
			WHERE truck_id = $1 AND unassigned_at IS NULL`, *truckID).Scan(&id)
		if err == nil {
			driverID = &id
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, nil, err
		}
	}
	if driverID != nil && truckID == nil {
		var id string
		err := r.pool.QueryRow(ctx, `
			SELECT truck_id FROM truck_driver_assignments
			WHERE driver_id = $1 AND unassigned_at IS NULL`, *driverID).Scan(&id)
		if err == nil {
			truckID = &id
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, nil, err
		}
	}
	return truckID, driverID, applicantID, nil
}

type expenseQueryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func insertExpense(ctx context.Context, queryer expenseQueryer, input ExpenseInput) (string, error) {
	breakdown, err := expenseCostJSON(input.CostBreakdown)
	if err != nil {
		return "", err
	}
	var id string
	err = queryer.QueryRow(ctx, `
		INSERT INTO expenses (
			category_id, category, expense_date, truck_id, driver_id, unit_number, driver_name, amount,
			payment_type, expense_type, reference_number, description, covered_by,
			paid_by, accounting_verified, owner_id, created_by, created_by_name, base_amount, payment_fee, cost_breakdown, applicant_id
		) VALUES (
			$1, (SELECT name FROM expense_settings WHERE id=$1 AND kind='category' AND active), $2, $3, $4,
			CASE WHEN $3::uuid IS NULL THEN $5 ELSE (SELECT unit_number FROM trucks WHERE id = $3) END,
			CASE WHEN $4::uuid IS NULL THEN $6 ELSE (SELECT full_name FROM drivers WHERE id = $4) END,
			$7::numeric, $8, $9, $10, $11, $12, $13, $14, $15, NULLIF($16,'')::uuid, NULLIF($17,''), $18::numeric, coalesce(nullif($19,''),'0')::numeric, coalesce($20::jsonb,'[]'::jsonb), $21
		) RETURNING id`,
		input.CategoryID, input.ExpenseDate, input.TruckID, input.DriverID,
		input.UnitNumber, input.DriverName, input.Amount, input.PaymentType, input.ExpenseType,
		input.ReferenceNumber, input.Description, input.CoveredBy, input.PaidBy,
		input.AccountingVerified, input.OwnerID, input.CreatedBy, input.CreatedByName,
		input.BaseAmount, input.PaymentFee, breakdown, input.ApplicantID,
	).Scan(&id)
	return id, err
}

func (r *ExpenseRepository) UpdateExpense(ctx context.Context, id string, input ExpenseInput) (Expense, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Expense{}, err
	}
	defer tx.Rollback(ctx)
	if err = prepareExpenseMaintenance(ctx, tx, input); err != nil {
		return Expense{}, err
	}
	previous, err := scanExpense(tx.QueryRow(ctx, selectExpensesSQL+" WHERE e.id=$1 FOR UPDATE OF e", id))
	if err != nil {
		return Expense{}, mapNotFound(err)
	}
	if err := updateExpenseRow(ctx, tx, id, input); err != nil {
		return Expense{}, err
	}
	if err = syncExpenseMaintenance(ctx, tx, id, input, &previous); err != nil {
		return Expense{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Expense{}, err
	}
	return r.GetExpense(ctx, id)
}

func updateExpenseRow(ctx context.Context, exec interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}, id string, input ExpenseInput) error {
	breakdown, err := expenseCostJSON(input.CostBreakdown)
	if err != nil {
		return err
	}
	command, err := exec.Exec(ctx, `
		UPDATE expenses SET
			category_id = $2, category = CASE WHEN category_id=$2::uuid THEN category ELSE (SELECT name FROM expense_settings WHERE id=$2 AND kind='category' AND active) END, expense_date = $3, truck_id = $4, driver_id = $5,
			unit_number = CASE WHEN $4::uuid IS NULL THEN $6 ELSE (SELECT unit_number FROM trucks WHERE id = $4) END,
			driver_name = CASE WHEN $5::uuid IS NULL THEN $7 ELSE (SELECT full_name FROM drivers WHERE id = $5) END,
			amount = $8::numeric, payment_type = $9,
			expense_type = $10, reference_number = $11, description = $12,
			covered_by = $13, paid_by = $14, owner_id = $15, updated_at = now(), base_amount=$16::numeric, payment_fee=coalesce(nullif($17,''),'0')::numeric, cost_breakdown=coalesce($19::jsonb,cost_breakdown), applicant_id=$20
		WHERE id = $1 AND ($18::timestamptz IS NULL OR updated_at=$18)`,
		id, input.CategoryID, input.ExpenseDate, input.TruckID, input.DriverID,
		input.UnitNumber, input.DriverName, input.Amount, input.PaymentType, input.ExpenseType,
		input.ReferenceNumber, input.Description, input.CoveredBy, input.PaidBy,
		input.OwnerID, input.BaseAmount, input.PaymentFee, input.UpdatedAt, breakdown, input.ApplicantID,
	)
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		if input.UpdatedAt != nil {
			return ErrExpenseSettingConflict
		}
		return ErrNotFound
	}
	return nil
}

func (r *ExpenseRepository) DeleteExpense(ctx context.Context, id string, actors ...string) error {
	// Maintenance deletion uses the same invoice/collection protections and keeps
	// the service snapshot, documents and odometer audit instead of orphaning them.
	var enabled bool
	if err := r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_attribute WHERE attrelid=to_regclass('maintenance_records') AND attname='expense_entry' AND NOT attisdropped)`).Scan(&enabled); err != nil {
		return err
	}
	if enabled {
		var record string
		var version int
		err := r.pool.QueryRow(ctx, `SELECT id::text,version FROM maintenance_records WHERE expense_id=$1 AND expense_entry`, id).Scan(&record, &version)
		if err == nil {
			actor := "Expense deletion"
			if len(actors) > 0 {
				actor = actors[0]
			}
			return NewMaintenanceRepository(r.pool).Void(ctx, record, version, "Deleted maintenance expense", actor)
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
	}
	command, err := r.pool.Exec(ctx, "DELETE FROM expenses WHERE id = $1", id)
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

const selectExpensesSQL = `
SELECT e.id, e.truck_id, COALESCE(e.driver_id,expense_applicant_driver(e.applicant_id)), e.category, e.category_id, e.created_by_name,
	to_char(date_trunc('week', e.expense_date)::date, 'YYYY-MM-DD'),
	to_char(e.expense_date, 'YYYY-MM-DD'), COALESCE(t.unit_number, e.unit_number),
	COALESCE(d.full_name, e.driver_name), e.amount::text, e.payment_type,
	e.expense_type, e.reference_number, e.description, e.covered_by, e.paid_by,
	e.accounting_verified, e.source_spreadsheet_id,
	e.source_sheet, e.source_row, e.created_at, e.updated_at,
 CASE WHEN (lower(btrim(e.covered_by)) IN ('driver','truck owner') OR e.charge_driver_id IS NOT NULL) THEN
   CASE WHEN e.driver_settled THEN e.amount ELSE coalesce((SELECT sum(amount) FROM expense_payments WHERE expense_id=e.id),0) END::text END,
 CASE WHEN (lower(btrim(e.covered_by)) IN ('driver','truck owner') OR e.charge_driver_id IS NOT NULL) THEN
   CASE WHEN e.driver_settled THEN 0 ELSE ` + expenseSettlementTotalSQL + `-coalesce((SELECT sum(amount) FROM expense_payments WHERE expense_id=e.id),0) END::text END,
 e.driver_settled,e.owner_id,(SELECT coalesce(od.full_name,i.full_name) FROM investors i LEFT JOIN drivers od ON od.id=i.driver_id WHERE i.id=e.owner_id),e.charge_driver_id,
 coalesce((SELECT jsonb_agg(jsonb_build_object('weekStart',p.week_start::text,'amount',p.amount::text) ORDER BY p.week_start) FROM expense_payments p WHERE p.expense_id=e.id),'[]'::jsonb)
, coalesce((SELECT jsonb_agg(jsonb_build_object('id',v.id,'role',v.verifier_role,'userId',v.user_id,'userName',v.user_name,'verifiedAt',v.verified_at) ORDER BY v.verifier_role) FROM expense_verifications v WHERE v.expense_id=e.id AND v.verifier_role='accountant' AND v.revoked_at IS NULL),'[]'::jsonb)
, e.base_amount::text, e.payment_fee::text, e.cost_breakdown, e.applicant_id, coalesce((SELECT role_id::text FROM app_users WHERE id=e.created_by),'')
FROM expenses e
LEFT JOIN trucks t ON t.id = e.truck_id
LEFT JOIN drivers d ON d.id = COALESCE(e.driver_id,expense_applicant_driver(e.applicant_id))`

func scanExpense(row rowScanner) (Expense, error) {
	var value Expense
	err := row.Scan(
		&value.ID, &value.TruckID, &value.DriverID, &value.Category, &value.CategoryID, &value.CreatedByName, &value.WeekStart,
		&value.ExpenseDate, &value.UnitNumber, &value.DriverName, &value.Amount,
		&value.PaymentType, &value.ExpenseType, &value.ReferenceNumber,
		&value.Description, &value.CoveredBy, &value.PaidBy,
		&value.AccountingVerified,
		&value.SourceSpreadsheetID, &value.SourceSheet, &value.SourceRow,
		&value.CreatedAt, &value.UpdatedAt, &value.PaidAmount, &value.RemainingAmount, &value.DriverSettled, &value.OwnerID, &value.OwnerName, &value.ChargeDriverID, &value.Payments, &value.Verifications,
		&value.BaseAmount, &value.PaymentFee, &value.CostBreakdown, &value.ApplicantID, &value.CreatorRoleID,
	)
	return value, err
}

func expenseCostJSON(items *[]ExpenseCostItem) ([]byte, error) {
	if items == nil {
		return nil, nil
	}
	if *items == nil {
		return []byte("[]"), nil
	}
	return json.Marshal(*items)
}

// Pool allows domain services to coordinate the shared expense ledger.
func (r *ExpenseRepository) Pool() *pgxpool.Pool { return r.pool }
