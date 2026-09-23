package repository

import (
	"context"
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

type Expense struct {
	ID                  string    `json:"id"`
	TruckID             *string   `json:"truckId"`
	DriverID            *string   `json:"driverId"`
	Company             string    `json:"company"`
	Category            string    `json:"category"`
	WeekStart           *string   `json:"weekStart"`
	ExpenseDate         *string   `json:"expenseDate"`
	UnitNumber          *string   `json:"unitNumber"`
	DriverName          *string   `json:"driverName"`
	Amount              *string   `json:"amount"`
	PaymentType         *string   `json:"paymentType"`
	ExpenseType         *string   `json:"expenseType"`
	ReferenceNumber     *string   `json:"referenceNumber"`
	Description         *string   `json:"description"`
	CoveredBy           *string   `json:"coveredBy"`
	PaidBy              *string   `json:"paidBy"`
	ManagerVerified     bool      `json:"managerVerified"`
	AccountingVerified  bool      `json:"accountingVerified"`
	SourceSpreadsheetID *string   `json:"sourceSpreadsheetId"`
	SourceSheet         *string   `json:"sourceSheet"`
	SourceRow           *int      `json:"sourceRow"`
	CreatedAt           time.Time `json:"createdAt"`
	UpdatedAt           time.Time `json:"updatedAt"`
}

type ExpenseInput struct {
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
	Pagination Pagination
	Search     string
	Category   string
	Company    string
	DateFrom   *time.Time
	DateTo     *time.Time
	TruckID    *string
	DriverID   *string
}

type ExpenseFilterOptions struct {
	Categories   []string `json:"categories"`
	Companies    []string `json:"companies"`
	PaymentTypes []string `json:"paymentTypes"`
	ExpenseTypes []string `json:"expenseTypes"`
	PaidBy       []string `json:"paidBy"`
	CoveredBy    []string `json:"coveredBy"`
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
	AND ($7::uuid IS NULL OR e.driver_id = $7)`
	args := []any{
		query.Search, query.Category, query.Company, query.DateFrom, query.DateTo,
		query.TruckID, query.DriverID,
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
		LIMIT $8 OFFSET $9`, pageArgs...)
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
			COALESCE(array_agg(DISTINCT payment_type ORDER BY payment_type)
				FILTER (WHERE payment_type IS NOT NULL AND payment_type <> ''), '{}'),
			COALESCE(array_agg(DISTINCT expense_type ORDER BY expense_type)
				FILTER (WHERE expense_type IS NOT NULL AND expense_type <> ''), '{}'),
			COALESCE(array_agg(DISTINCT paid_by ORDER BY paid_by)
				FILTER (WHERE paid_by IS NOT NULL AND paid_by <> ''), '{}'),
			COALESCE(array_agg(DISTINCT covered_by ORDER BY covered_by)
				FILTER (WHERE covered_by IS NOT NULL AND covered_by <> ''), '{}')
		FROM expenses e`).Scan(
		&options.Categories, &options.Companies, &options.PaymentTypes,
		&options.ExpenseTypes, &options.PaidBy, &options.CoveredBy,
	); err != nil {
		return ExpensePage{}, err
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

type expenseQueryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func insertExpense(ctx context.Context, queryer expenseQueryer, input ExpenseInput) (string, error) {
	var id string
	err := queryer.QueryRow(ctx, `
		INSERT INTO expenses (
			company, category, expense_date, truck_id, driver_id, unit_number, driver_name, amount,
			payment_type, expense_type, reference_number, description, covered_by,
			paid_by, manager_verified, accounting_verified
		) VALUES (
			$1, $2, $3, $4, $5,
			CASE WHEN $4::uuid IS NULL THEN $6 ELSE (SELECT unit_number FROM trucks WHERE id = $4) END,
			CASE WHEN $5::uuid IS NULL THEN $7 ELSE (SELECT full_name FROM drivers WHERE id = $5) END,
			$8::numeric, $9, $10, $11, $12, $13, $14, $15, $16
		) RETURNING id`,
		input.Company, input.Category, input.ExpenseDate, input.TruckID, input.DriverID,
		input.UnitNumber, input.DriverName, input.Amount, input.PaymentType, input.ExpenseType,
		input.ReferenceNumber, input.Description, input.CoveredBy, input.PaidBy,
		input.ManagerVerified, input.AccountingVerified,
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
			accounting_verified = $17, updated_at = now()
		WHERE id = $1`,
		id, input.Company, input.Category, input.ExpenseDate, input.TruckID, input.DriverID,
		input.UnitNumber, input.DriverName, input.Amount, input.PaymentType, input.ExpenseType,
		input.ReferenceNumber, input.Description, input.CoveredBy, input.PaidBy,
		input.ManagerVerified, input.AccountingVerified,
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
	e.source_sheet, e.source_row, e.created_at, e.updated_at
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
		&value.CreatedAt, &value.UpdatedAt,
	)
	return value, err
}
