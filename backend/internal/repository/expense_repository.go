package repository

import (
	"context"
	"errors"
	"sort"
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
type Expense struct {
	Payments            []ExpensePayment `json:"payments"`
	OwnerID             *string          `json:"ownerId"`
	OwnerName           *string          `json:"ownerName"`
	ChargeDriverID      *string          `json:"chargeDriverId"`
	PaidAmount          *string          `json:"paidAmount"`
	RemainingAmount     *string          `json:"remainingAmount"`
	DriverSettled       bool             `json:"driverSettled"`
	ID                  string           `json:"id"`
	TruckID             *string          `json:"truckId"`
	DriverID            *string          `json:"driverId"`
	Company             string           `json:"company"`
	Category            string           `json:"category"`
	WeekStart           *string          `json:"weekStart"`
	ExpenseDate         *string          `json:"expenseDate"`
	UnitNumber          *string          `json:"unitNumber"`
	DriverName          *string          `json:"driverName"`
	Amount              *string          `json:"amount"`
	PaymentType         *string          `json:"paymentType"`
	ExpenseType         *string          `json:"expenseType"`
	ReferenceNumber     *string          `json:"referenceNumber"`
	Description         *string          `json:"description"`
	CoveredBy           *string          `json:"coveredBy"`
	PaidBy              *string          `json:"paidBy"`
	ManagerVerified     bool             `json:"managerVerified"`
	AccountingVerified  bool             `json:"accountingVerified"`
	SourceSpreadsheetID *string          `json:"sourceSpreadsheetId"`
	SourceSheet         *string          `json:"sourceSheet"`
	SourceRow           *int             `json:"sourceRow"`
	CreatedAt           time.Time        `json:"createdAt"`
	UpdatedAt           time.Time        `json:"updatedAt"`
}

type ExpenseInput struct {
	OwnerID            *string
	Company            string
	Category           string
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
	Responsibility string
	ChargeDriverID *string
	Pagination     Pagination
	Search         string
	Category       string
	Company        string
	DateFrom       *time.Time
	DateTo         *time.Time
	TruckID        *string
	DriverID       *string
}

type ExpenseFilterOptions struct {
	Settings     []ExpenseSetting `json:"settings"`
	Categories   []string         `json:"categories"`
	Companies    []string         `json:"companies"`
	PaymentTypes []string         `json:"paymentTypes"`
	ExpenseTypes []string         `json:"expenseTypes"`
	PaidBy       []string         `json:"paidBy"`
	CoveredBy    []string         `json:"coveredBy"`
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
WHERE ($1 = '' OR concat_ws(' ', e.company, e.category, COALESCE(t.unit_number, e.unit_number),
		COALESCE(d.full_name, e.driver_name), e.payment_type, e.expense_type,
		e.reference_number, e.description, e.covered_by, e.paid_by)
	ILIKE '%' || $1 || '%')
	AND ($2 = '' OR e.category = $2)
	AND ($3 = '' OR e.company = $3)
	AND ($4::date IS NULL OR e.expense_date >= $4)
	AND ($5::date IS NULL OR e.expense_date <= $5)
	AND ($6::uuid IS NULL OR e.truck_id = $6)
	AND ($7::uuid IS NULL OR e.driver_id = $7)
 AND ($8::uuid IS NULL OR e.charge_driver_id=$8)
 AND ($9='' OR ($9='non_personal' AND e.charge_driver_id IS DISTINCT FROM $7::uuid))`
	args := []any{
		query.Search, query.Category, query.Company, query.DateFrom, query.DateTo,
		query.TruckID, query.DriverID, query.ChargeDriverID, query.Responsibility,
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
		LIMIT $10 OFFSET $11`, pageArgs...)
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

	options := ExpenseFilterOptions{}
	if err := r.pool.QueryRow(ctx, `
		SELECT
			COALESCE(array_agg(DISTINCT category ORDER BY category)
				FILTER (WHERE category <> ''), '{}'),
			COALESCE(array_agg(DISTINCT company ORDER BY company)
				FILTER (WHERE company <> ''), '{}'),
			COALESCE(array_agg(DISTINCT covered_by ORDER BY covered_by)
				FILTER (WHERE covered_by IS NOT NULL AND covered_by <> ''), '{}')
		FROM expenses e`).Scan(
		&options.Categories, &options.Companies, &options.CoveredBy,
	); err != nil {
		return ExpensePage{}, err
	}

	settings, err := r.ListExpenseSettings(ctx)
	if err != nil {
		return ExpensePage{}, err
	}
	options.Settings = settings
	options.PaymentTypes = []string{}
	options.PaidBy = []string{}
	// Keep the legacy field; defaults now come from category-linked settings.
	options.ExpenseTypes = []string{}
	categories := map[string]bool{}
	for _, name := range options.Categories {
		categories[name] = true
	}
	for _, item := range settings {
		if !item.Active {
			continue
		}
		switch item.Kind {
		case "category":
			categories[item.Name] = true
		case "payment_method":
			options.PaymentTypes = append(options.PaymentTypes, item.Name)
		case "payer":
			options.PaidBy = append(options.PaidBy, item.Name)
		}
	}
	options.Categories = []string{}
	for name := range categories {
		options.Categories = append(options.Categories, name)
	}
	sort.Strings(options.Categories)
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
	var id string
	err := queryer.QueryRow(ctx, `
		INSERT INTO expenses (
			company, category, expense_date, truck_id, driver_id, unit_number, driver_name, amount,
			payment_type, expense_type, reference_number, description, covered_by,
			paid_by, manager_verified, accounting_verified, owner_id
		) VALUES (
			$1, $2, $3, $4, $5,
			CASE WHEN $4::uuid IS NULL THEN $6 ELSE (SELECT unit_number FROM trucks WHERE id = $4) END,
			CASE WHEN $5::uuid IS NULL THEN $7 ELSE (SELECT full_name FROM drivers WHERE id = $5) END,
			$8::numeric, $9, $10, $11, $12, $13, $14, $15, $16, $17
		) RETURNING id`,
		input.Company, input.Category, input.ExpenseDate, input.TruckID, input.DriverID,
		input.UnitNumber, input.DriverName, input.Amount, input.PaymentType, input.ExpenseType,
		input.ReferenceNumber, input.Description, input.CoveredBy, input.PaidBy,
		input.ManagerVerified, input.AccountingVerified, input.OwnerID,
	).Scan(&id)
	return id, err
}

func (r *ExpenseRepository) UpdateExpense(ctx context.Context, id string, input ExpenseInput) (Expense, error) {
	command, err := r.pool.Exec(ctx, `
		UPDATE expenses SET
			company = $2, category = $3, expense_date = $4, truck_id = $5, driver_id = $6,
			unit_number = CASE WHEN $5::uuid IS NULL THEN $7 ELSE (SELECT unit_number FROM trucks WHERE id = $5) END,
			driver_name = CASE WHEN $6::uuid IS NULL THEN $8 ELSE (SELECT full_name FROM drivers WHERE id = $6) END,
			amount = $9::numeric, payment_type = $10,
			expense_type = $11, reference_number = $12, description = $13,
			covered_by = $14, paid_by = $15, manager_verified = $16,
			accounting_verified = $17, owner_id = $18, updated_at = now()
		WHERE id = $1`,
		id, input.Company, input.Category, input.ExpenseDate, input.TruckID, input.DriverID,
		input.UnitNumber, input.DriverName, input.Amount, input.PaymentType, input.ExpenseType,
		input.ReferenceNumber, input.Description, input.CoveredBy, input.PaidBy,
		input.ManagerVerified, input.AccountingVerified, input.OwnerID,
	)
	if err != nil {
		return Expense{}, err
	}
	if command.RowsAffected() == 0 {
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
SELECT e.id, e.truck_id, e.driver_id, e.company, e.category,
	to_char(date_trunc('week', e.expense_date)::date, 'YYYY-MM-DD'),
	to_char(e.expense_date, 'YYYY-MM-DD'), COALESCE(t.unit_number, e.unit_number),
	COALESCE(d.full_name, e.driver_name), e.amount::text, e.payment_type,
	e.expense_type, e.reference_number, e.description, e.covered_by, e.paid_by,
	e.manager_verified, e.accounting_verified, e.source_spreadsheet_id,
	e.source_sheet, e.source_row, e.created_at, e.updated_at,
 CASE WHEN (lower(btrim(e.covered_by))='driver' OR e.charge_driver_id IS NOT NULL) THEN
   CASE WHEN e.driver_settled THEN e.amount ELSE coalesce((SELECT sum(amount) FROM expense_payments WHERE expense_id=e.id),0) END::text END,
 CASE WHEN (lower(btrim(e.covered_by))='driver' OR e.charge_driver_id IS NOT NULL) THEN
   CASE WHEN e.driver_settled THEN 0 ELSE e.amount-coalesce((SELECT sum(amount) FROM expense_payments WHERE expense_id=e.id),0) END::text END,
 e.driver_settled,e.owner_id,(SELECT coalesce(od.full_name,i.full_name) FROM investors i LEFT JOIN drivers od ON od.id=i.driver_id WHERE i.id=e.owner_id),e.charge_driver_id,
 coalesce((SELECT jsonb_agg(jsonb_build_object('weekStart',p.week_start::text,'amount',p.amount::text) ORDER BY p.week_start) FROM expense_payments p WHERE p.expense_id=e.id),'[]'::jsonb)
FROM expenses e
LEFT JOIN trucks t ON t.id = e.truck_id
LEFT JOIN drivers d ON d.id = e.driver_id`

func scanExpense(row rowScanner) (Expense, error) {
	var value Expense
	err := row.Scan(
		&value.ID, &value.TruckID, &value.DriverID, &value.Company, &value.Category, &value.WeekStart,
		&value.ExpenseDate, &value.UnitNumber, &value.DriverName, &value.Amount,
		&value.PaymentType, &value.ExpenseType, &value.ReferenceNumber,
		&value.Description, &value.CoveredBy, &value.PaidBy,
		&value.ManagerVerified, &value.AccountingVerified,
		&value.SourceSpreadsheetID, &value.SourceSheet, &value.SourceRow,
		&value.CreatedAt, &value.UpdatedAt, &value.PaidAmount, &value.RemainingAmount, &value.DriverSettled, &value.OwnerID, &value.OwnerName, &value.ChargeDriverID, &value.Payments,
	)
	return value, err
}
