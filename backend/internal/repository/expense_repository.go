package repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
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
	CostBreakdown       []ExpenseCostItem     `json:"costBreakdown"`
	BaseAmount          *string               `json:"baseAmount"`
	PaymentFee          string                `json:"paymentFee"`
	Verifications       []ExpenseVerification `json:"verifications"`
	Payments            []ExpensePayment      `json:"payments"`
	OwnerID             *string               `json:"ownerId"`
	OwnerName           *string               `json:"ownerName"`
	ChargeDriverID      *string               `json:"chargeDriverId"`
	PaidAmount          *string               `json:"paidAmount"`
	RemainingAmount     *string               `json:"remainingAmount"`
	DriverSettled       bool                  `json:"driverSettled"`
	ID                  string                `json:"id"`
	TruckID             *string               `json:"truckId"`
	DriverID            *string               `json:"driverId"`
	Category            string                `json:"category"`
	CategoryID          string                `json:"categoryId"`
	CreatedByName       *string               `json:"createdByName"`
	WeekStart           *string               `json:"weekStart"`
	ExpenseDate         *string               `json:"expenseDate"`
	UnitNumber          *string               `json:"unitNumber"`
	DriverName          *string               `json:"driverName"`
	Amount              *string               `json:"amount"`
	PaymentType         *string               `json:"paymentType"`
	ExpenseType         *string               `json:"expenseType"`
	ReferenceNumber     *string               `json:"referenceNumber"`
	Description         *string               `json:"description"`
	CoveredBy           *string               `json:"coveredBy"`
	PaidBy              *string               `json:"paidBy"`
	ManagerVerified     bool                  `json:"managerVerified"`
	AccountingVerified  bool                  `json:"accountingVerified"`
	SourceSpreadsheetID *string               `json:"sourceSpreadsheetId"`
	SourceSheet         *string               `json:"sourceSheet"`
	SourceRow           *int                  `json:"sourceRow"`
	CreatedAt           time.Time             `json:"createdAt"`
	UpdatedAt           time.Time             `json:"updatedAt"`
}

type ExpenseInput struct {
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
	ManagerVerified    bool
	AccountingVerified bool
}

type ExpensePageQuery struct {
	PaymentType           string
	PaidBy                string
	CoveredBy             string
	UnitNumber            string
	DriverName            string
	AccountantVerified    *bool
	AdminVerified         *bool
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
	Filters      ExpenseFilterValues     `json:"filters"`
	Settings     []ExpenseSetting        `json:"settings"`
	Categories   []ExpenseFilterCategory `json:"categories"`
	PaymentTypes []string                `json:"paymentTypes"`
	ExpenseTypes []string                `json:"expenseTypes"`
	PaidBy       []string                `json:"paidBy"`
	CoveredBy    []string                `json:"coveredBy"`
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
WHERE ($1 = '' OR concat_ws(' ', e.category, COALESCE(t.unit_number, e.unit_number),
		COALESCE(d.full_name, e.driver_name), e.payment_type, e.expense_type,
		e.reference_number, e.description, e.covered_by, e.paid_by)
	ILIKE '%' || $1 || '%')
	AND e.category_id=ANY($2::uuid[])
	AND ($3 = '' OR e.category_id = $3::uuid)
	AND ($4 = '' OR e.payment_type = $4)
	AND ($5::date IS NULL OR e.expense_date >= $5)
	AND ($6::date IS NULL OR e.expense_date <= $6)
	AND ($7::uuid IS NULL OR e.truck_id = $7)
	AND ($8::uuid IS NULL OR e.driver_id = $8)
 AND ($9::uuid IS NULL OR e.charge_driver_id=$9)
 AND ($10='' OR ($10='non_personal' AND e.charge_driver_id IS DISTINCT FROM $8::uuid))
 AND ($11::boolean IS NULL OR e.accounting_verified=$11)
 AND ($12::boolean IS NULL OR e.manager_verified=$12)
 AND ($13='' OR e.paid_by=$13)
 AND ($14='' OR e.covered_by=$14)
 AND ($15='' OR COALESCE(t.unit_number,e.unit_number)=$15)
 AND ($16='' OR COALESCE(d.full_name,e.driver_name)=$16)`
	args := []any{
		query.Search, query.VisibleCategoryIDs, query.CategoryID, query.PaymentType, query.DateFrom, query.DateTo,
		query.TruckID, query.DriverID, query.ChargeDriverID, query.Responsibility,
		query.AccountantVerified, query.AdminVerified, query.PaidBy, query.CoveredBy, query.UnitNumber, query.DriverName,
	}

	var total int
	summary := ExpenseSummary{}
	if err := r.pool.QueryRow(ctx, `
		SELECT count(*), COALESCE(sum(amount), 0)::text,
			count(*) FILTER (WHERE expense_date IS NULL OR amount IS NULL)
		FROM expenses e
		LEFT JOIN trucks t ON t.id = e.truck_id
		LEFT JOIN drivers d ON d.id = e.driver_id `+where, args...).Scan(
		&total, &summary.Amount, &summary.IncompleteCount,
	); err != nil {
		return ExpensePage{}, err
	}

	query.Pagination = query.Pagination.Normalize(total)
	pageArgs := append(args, query.Pagination.PageSize, query.Pagination.Offset())
	rows, err := r.pool.Query(ctx, selectExpensesSQL+where+`
		ORDER BY e.expense_date DESC NULLS LAST, e.created_at DESC, e.id
		LIMIT $17 OFFSET $18`, pageArgs...)
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

	options := ExpenseFilterOptions{Categories: []ExpenseFilterCategory{}}
	if err := r.pool.QueryRow(ctx, `SELECT
 COALESCE(array_agg(DISTINCT COALESCE(d.full_name,e.driver_name) ORDER BY COALESCE(d.full_name,e.driver_name)) FILTER(WHERE COALESCE(d.full_name,e.driver_name,'')<>''),'{}'),
 COALESCE(array_agg(DISTINCT COALESCE(t.unit_number,e.unit_number) ORDER BY COALESCE(t.unit_number,e.unit_number)) FILTER(WHERE COALESCE(t.unit_number,e.unit_number,'')<>''),'{}'),
 COALESCE(array_agg(DISTINCT e.payment_type ORDER BY e.payment_type) FILTER(WHERE coalesce(e.payment_type,'')<>''),'{}'),
 COALESCE(array_agg(DISTINCT e.paid_by ORDER BY e.paid_by) FILTER(WHERE coalesce(e.paid_by,'')<>''),'{}'),
 COALESCE(array_agg(DISTINCT e.covered_by ORDER BY e.covered_by) FILTER(WHERE coalesce(e.covered_by,'')<>''),'{}')
 FROM expenses e LEFT JOIN trucks t ON t.id=e.truck_id LEFT JOIN drivers d ON d.id=e.driver_id
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
	return value, mapNotFound(err)
}

func (r *ExpenseRepository) CreateExpense(ctx context.Context, input ExpenseInput) (Expense, error) {
	id, err := insertExpense(ctx, r.pool, input)
	if err != nil {
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
		id, insertErr := insertExpense(ctx, tx, input)
		if insertErr != nil {
			return nil, insertErr
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
	return values, nil
}

func (r *ExpenseRepository) ResolveExpenseLinks(
	ctx context.Context,
	unitNumber, driverName *string,
) (*string, *string, error) {
	var truckID, driverID *string
	if unitNumber != nil && normalizeTruckUnit(*unitNumber) != "" {
		var id string
		err := r.pool.QueryRow(ctx, `
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
		rows, err := r.pool.Query(ctx, `SELECT id, full_name FROM drivers WHERE active = true`)
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
		err := r.pool.QueryRow(ctx, `
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
		err := r.pool.QueryRow(ctx, `
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
			paid_by, manager_verified, accounting_verified, owner_id, created_by, created_by_name, base_amount, payment_fee, cost_breakdown
		) VALUES (
			$1, (SELECT name FROM expense_settings WHERE id=$1 AND kind='category' AND active), $2, $3, $4,
			CASE WHEN $3::uuid IS NULL THEN $5 ELSE (SELECT unit_number FROM trucks WHERE id = $3) END,
			CASE WHEN $4::uuid IS NULL THEN $6 ELSE (SELECT full_name FROM drivers WHERE id = $4) END,
			$7::numeric, $8, $9, $10, $11, $12, $13, $14, $15, $16, NULLIF($17,'')::uuid, NULLIF($18,''), $19::numeric, coalesce(nullif($20,''),'0')::numeric, coalesce($21::jsonb,'[]'::jsonb)
		) RETURNING id`,
		input.CategoryID, input.ExpenseDate, input.TruckID, input.DriverID,
		input.UnitNumber, input.DriverName, input.Amount, input.PaymentType, input.ExpenseType,
		input.ReferenceNumber, input.Description, input.CoveredBy, input.PaidBy,
		input.ManagerVerified, input.AccountingVerified, input.OwnerID, input.CreatedBy, input.CreatedByName,
		input.BaseAmount, input.PaymentFee, breakdown,
	).Scan(&id)
	return id, err
}

func (r *ExpenseRepository) UpdateExpense(ctx context.Context, id string, input ExpenseInput) (Expense, error) {
	breakdown, err := expenseCostJSON(input.CostBreakdown)
	if err != nil {
		return Expense{}, err
	}
	command, err := r.pool.Exec(ctx, `
		UPDATE expenses SET
			category_id = $2, category = CASE WHEN category_id=$2::uuid THEN category ELSE (SELECT name FROM expense_settings WHERE id=$2 AND kind='category' AND active) END, expense_date = $3, truck_id = $4, driver_id = $5,
			unit_number = CASE WHEN $4::uuid IS NULL THEN $6 ELSE (SELECT unit_number FROM trucks WHERE id = $4) END,
			driver_name = CASE WHEN $5::uuid IS NULL THEN $7 ELSE (SELECT full_name FROM drivers WHERE id = $5) END,
			amount = $8::numeric, payment_type = $9,
			expense_type = $10, reference_number = $11, description = $12,
			covered_by = $13, paid_by = $14, owner_id = $15, updated_at = now(), base_amount=$16::numeric, payment_fee=coalesce(nullif($17,''),'0')::numeric, cost_breakdown=coalesce($19::jsonb,cost_breakdown)
		WHERE id = $1 AND ($18::timestamptz IS NULL OR updated_at=$18)`,
		id, input.CategoryID, input.ExpenseDate, input.TruckID, input.DriverID,
		input.UnitNumber, input.DriverName, input.Amount, input.PaymentType, input.ExpenseType,
		input.ReferenceNumber, input.Description, input.CoveredBy, input.PaidBy,
		input.OwnerID, input.BaseAmount, input.PaymentFee, input.UpdatedAt, breakdown,
	)
	if err != nil {
		return Expense{}, err
	}
	if command.RowsAffected() == 0 {
		if input.UpdatedAt != nil {
			return Expense{}, ErrExpenseSettingConflict
		}
		return Expense{}, ErrNotFound
	}
	return r.GetExpense(ctx, id)
}

func (r *ExpenseRepository) DeleteExpense(ctx context.Context, id string) error {
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
SELECT e.id, e.truck_id, e.driver_id, e.category, e.category_id, e.created_by_name,
	to_char(date_trunc('week', e.expense_date)::date, 'YYYY-MM-DD'),
	to_char(e.expense_date, 'YYYY-MM-DD'), COALESCE(t.unit_number, e.unit_number),
	COALESCE(d.full_name, e.driver_name), e.amount::text, e.payment_type,
	e.expense_type, e.reference_number, e.description, e.covered_by, e.paid_by,
	e.manager_verified, e.accounting_verified, e.source_spreadsheet_id,
	e.source_sheet, e.source_row, e.created_at, e.updated_at,
 CASE WHEN (lower(btrim(e.covered_by)) IN ('driver','truck owner') OR e.charge_driver_id IS NOT NULL) THEN
   CASE WHEN e.driver_settled THEN e.amount ELSE coalesce((SELECT sum(amount) FROM expense_payments WHERE expense_id=e.id),0) END::text END,
 CASE WHEN (lower(btrim(e.covered_by)) IN ('driver','truck owner') OR e.charge_driver_id IS NOT NULL) THEN
   CASE WHEN e.driver_settled THEN 0 ELSE e.amount-coalesce((SELECT sum(amount) FROM expense_payments WHERE expense_id=e.id),0) END::text END,
 e.driver_settled,e.owner_id,(SELECT coalesce(od.full_name,i.full_name) FROM investors i LEFT JOIN drivers od ON od.id=i.driver_id WHERE i.id=e.owner_id),e.charge_driver_id,
 coalesce((SELECT jsonb_agg(jsonb_build_object('weekStart',p.week_start::text,'amount',p.amount::text) ORDER BY p.week_start) FROM expense_payments p WHERE p.expense_id=e.id),'[]'::jsonb)
, coalesce((SELECT jsonb_agg(jsonb_build_object('id',v.id,'role',v.verifier_role,'userId',v.user_id,'userName',v.user_name,'verifiedAt',v.verified_at) ORDER BY v.verifier_role) FROM expense_verifications v WHERE v.expense_id=e.id AND v.revoked_at IS NULL),'[]'::jsonb)
, e.base_amount::text, e.payment_fee::text, e.cost_breakdown
FROM expenses e
LEFT JOIN trucks t ON t.id = e.truck_id
LEFT JOIN drivers d ON d.id = e.driver_id`

func scanExpense(row rowScanner) (Expense, error) {
	var value Expense
	err := row.Scan(
		&value.ID, &value.TruckID, &value.DriverID, &value.Category, &value.CategoryID, &value.CreatedByName, &value.WeekStart,
		&value.ExpenseDate, &value.UnitNumber, &value.DriverName, &value.Amount,
		&value.PaymentType, &value.ExpenseType, &value.ReferenceNumber,
		&value.Description, &value.CoveredBy, &value.PaidBy,
		&value.ManagerVerified, &value.AccountingVerified,
		&value.SourceSpreadsheetID, &value.SourceSheet, &value.SourceRow,
		&value.CreatedAt, &value.UpdatedAt, &value.PaidAmount, &value.RemainingAmount, &value.DriverSettled, &value.OwnerID, &value.OwnerName, &value.ChargeDriverID, &value.Payments, &value.Verifications,
		&value.BaseAmount, &value.PaymentFee, &value.CostBreakdown,
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
