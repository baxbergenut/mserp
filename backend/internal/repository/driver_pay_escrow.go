package repository

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type DriverPayCandidate struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Active bool   `json:"active"`
}

type PayrollEscrowDraft struct {
	EscrowID       string            `json:"escrowId"`
	Version        int               `json:"version"`
	Return         string            `json:"returnAmount"`
	Expenses       map[string]string `json:"expenses"`
	StopCollection *bool             `json:"stopCollection,omitempty"`
}

type PayrollEscrowAccount struct {
	ID        string `json:"id"`
	Version   int    `json:"version"`
	Held      string `json:"held"`
	Available string `json:"available"`
	Stopped   bool   `json:"stopped"`
}

type DriverPayEscrowEditor struct {
	Accounts        []PayrollEscrowAccount `json:"accounts"`
	Expenses        []ExpenseDeduction     `json:"expenses"`
	Draft           PayrollEscrowDraft     `json:"draft"`
	EditVersion     int                    `json:"editVersion"`
	ExpenseRevision string                 `json:"expenseRevision"`
	SuggestStop     bool                   `json:"suggestStop"`
}

func (r *DriverPayRepository) DriverPayCandidates(ctx context.Context, week time.Time, search string) ([]DriverPayCandidate, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	report, err := readDriverPayWeek(context.WithValue(ctx, payrollSummaryKey{}, true), tx, week, "")
	if err != nil {
		return nil, err
	}
	represented := []string{}
	for _, d := range report.Drivers {
		represented = append(represented, d.ID)
	}
	rows, err := tx.Query(ctx, `SELECT id::text,full_name,active FROM drivers
 WHERE NOT (id=ANY($2::uuid[])) AND payroll_driver_at(id,$1::date)=id
 AND (full_name ILIKE '%'||$3||'%') ORDER BY lower(full_name),id`, week, represented, strings.TrimSpace(search))
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[DriverPayCandidate])
}

func (r *DriverPayRepository) ChangeDriverPayMembership(ctx context.Context, week time.Time, driver string, remove bool) error {
	tx, closeWrite, err := r.beginPayrollWrite(ctx, "driver:"+driver)
	if err != nil {
		return err
	}
	defer closeWrite()
	date := week.Format(time.DateOnly)
	if err = assertPayrollOpen(ctx, tx, driver, date); err != nil {
		return err
	}
	if remove {
		tag, err := tx.Exec(ctx, `DELETE FROM driver_pay_weeks w WHERE w.driver_id=$1 AND w.week_start=$2
 AND manually_added AND notes='' AND comments='{}' AND adjustments='[]' AND expense_choices='{}' AND escrow_draft='{}'
 AND fuel_override IS NULL AND toll_override IS NULL AND cardinality(layover_excluded_dates)=0
 AND NOT EXISTS(SELECT 1 FROM payroll_settlements s WHERE s.driver_id=w.driver_id AND s.week_start=w.week_start)
 AND version=1`, driver, week)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return chargeInvalid("Only an unused manually added statement can be removed")
		}
	} else {
		var solo bool
		err = tx.QueryRow(ctx, `SELECT payroll_driver_at(id,$2::date)=id FROM drivers WHERE id=$1`, driver, week).Scan(&solo)
		if errors.Is(err, pgx.ErrNoRows) {
			return chargeInvalid("Select an existing driver")
		}
		if err != nil {
			return err
		}
		if !solo {
			return chargeInvalid("This driver is represented by their team statement for the selected week")
		}
		report, err := readDriverPayWeek(ctx, tx, week, driver)
		if err != nil {
			return err
		}
		if len(report.Drivers) > 0 {
			return ErrDriverPayConflict
		}
		tag, err := tx.Exec(ctx, `INSERT INTO driver_pay_weeks(driver_id,week_start,manually_added)
 VALUES($1,$2,true) ON CONFLICT DO NOTHING`, driver, week)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return ErrDriverPayConflict
		}
	}
	return commitPayrollMutation(ctx, tx)
}

func (r *DriverPayRepository) DriverPayEscrow(ctx context.Context, week time.Time, driver string) (DriverPayEscrowEditor, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return DriverPayEscrowEditor{}, err
	}
	defer tx.Rollback(ctx)
	return readDriverPayEscrow(ctx, tx, week, driver)
}

func readDriverPayEscrow(ctx context.Context, tx pgx.Tx, week time.Time, driver string) (DriverPayEscrowEditor, error) {
	editor := DriverPayEscrowEditor{Accounts: []PayrollEscrowAccount{}, Expenses: []ExpenseDeduction{}}
	report, err := readDriverPayWeek(ctx, tx, week, driver)
	if err != nil {
		return editor, err
	}
	if len(report.Drivers) != 1 {
		return editor, chargeInvalid("Add the driver to this week first")
	}
	d := report.Drivers[0]
	if d.Settlement != nil && d.Settlement.Finalized {
		return editor, chargeInvalid("Reopen this statement before editing escrow")
	}
	editor.EditVersion, editor.ExpenseRevision = d.Edits.Version, d.ExpenseRevision
	for _, expense := range d.Edits.ExpenseDeductions {
		if expense.Source == "" && !expense.Saved && expense.EscrowEligible {
			editor.Expenses = append(editor.Expenses, expense)
		}
	}
	var body []byte
	err = tx.QueryRow(ctx, `SELECT coalesce(w.escrow_draft,'{}'), NOT d.active AND d.termination_date IS NOT NULL AND d.termination_date <= $2::date+6
 FROM drivers d LEFT JOIN driver_pay_weeks w ON w.driver_id=d.id AND w.week_start=$2 WHERE d.id=$1`, driver, week).Scan(&body, &editor.SuggestStop)
	if err != nil {
		return editor, err
	}
	if err = json.Unmarshal(body, &editor.Draft); err != nil {
		return editor, err
	}
	editor.Accounts, err = payrollEscrowAccounts(ctx, tx, report.WeekStart, driver)
	return editor, err
}

// All existing commitments count. The minimum dated headroom from the
// statement week forward prevents spending later collections or funds already
// promised to a later release.
func payrollEscrowAccounts(ctx context.Context, tx pgx.Tx, week, driver string) ([]PayrollEscrowAccount, error) {
	rows, err := tx.Query(ctx, `SELECT e.id::text,e.balance_version,
 (e.opening_paid+coalesce((SELECT sum(amount) FROM driver_escrow_payments WHERE escrow_id=e.id),0)
  -coalesce((SELECT sum(amount) FROM driver_escrow_releases WHERE escrow_id=e.id AND NOT cancelled),0))::text,
 greatest(0,(SELECT min(e.opening_paid
 +coalesce((SELECT sum(amount) FROM driver_escrow_payments p WHERE p.escrow_id=e.id AND p.week_start<=w.week),0)
 -coalesce((SELECT sum(amount) FROM driver_escrow_releases r WHERE r.escrow_id=e.id AND NOT r.cancelled AND r.week_start<=w.week),0))
 FROM (SELECT $2::date week UNION SELECT week_start FROM driver_escrow_releases
 WHERE escrow_id=e.id AND NOT cancelled AND week_start>$2::date) w))::text,
 escrow_collection_stopped(e.id,$2::date)
 FROM driver_escrows e WHERE e.driver_id=$1 AND e.start_date<$2::date+7 AND accounting_live_source('escrow',e.id,e.start_date)
 ORDER BY e.start_date,e.id`, driver, week)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[PayrollEscrowAccount])
}

func (r *DriverPayRepository) SaveDriverPayEscrow(ctx context.Context, week time.Time, driver string, editVersion int, expenseRevision string, draft PayrollEscrowDraft) error {
	tx, closeWrite, err := r.beginPayrollWrite(ctx, "driver:"+driver)
	if err != nil {
		return err
	}
	defer closeWrite()
	editor, err := readDriverPayEscrow(ctx, tx, week, driver)
	if err != nil {
		return err
	}
	if editVersion != editor.EditVersion || expenseRevision != editor.ExpenseRevision {
		return ErrDriverPayConflict
	}
	if draft.EscrowID != "" {
		if _, err = validatePayrollEscrowDraft(draft, editor.Accounts, editor.Expenses); err != nil {
			return err
		}
	} else {
		draft = PayrollEscrowDraft{}
	}
	body := []byte("{}")
	if draft.EscrowID != "" {
		body, err = json.Marshal(draft)
		if err != nil {
			return err
		}
	}
	if editVersion == 0 && draft.EscrowID == "" {
		return nil
	}
	_, err = tx.Exec(ctx, `INSERT INTO driver_pay_weeks(driver_id,week_start,escrow_draft) VALUES($1,$2,$3)
 ON CONFLICT(driver_id,week_start) DO UPDATE SET escrow_draft=excluded.escrow_draft,version=driver_pay_weeks.version+1,updated_at=now()`, driver, week, body)
	if err != nil {
		return err
	}
	return commitPayrollMutation(ctx, tx)
}

func validatePayrollEscrowDraft(draft PayrollEscrowDraft, accounts []PayrollEscrowAccount, expenses []ExpenseDeduction) (int64, error) {
	var account PayrollEscrowAccount
	for _, a := range accounts {
		if a.ID == draft.EscrowID {
			account = a
			break
		}
	}
	if account.ID == "" || account.Version != draft.Version {
		return 0, chargeInvalid("Escrow changed. Review the current balance in Manage escrow and save again")
	}
	total, err := chargeCents(draft.Return)
	if err != nil || total < 0 || total > 99999999999999 {
		return 0, chargeInvalid("Enter a valid escrow return amount")
	}
	rows := map[string]ExpenseDeduction{}
	for _, e := range expenses {
		if e.Source == "" && !e.Saved && e.EscrowEligible {
			rows[e.ExpenseID] = e
		}
	}
	for id, amount := range draft.Expenses {
		row, ok := rows[id]
		n, e := chargeCents(amount)
		limit, _ := chargeCents(row.Available)
		if !ok || e != nil || n <= 0 || n > limit {
			return 0, chargeInvalid("An escrow allocation exceeds the outstanding charge or the charge is no longer available")
		}
		if row.Mode == "fixed" {
			salary, _ := chargeCents(row.Amount)
			if salary+n > limit {
				return 0, chargeInvalid("Reduce the salary deduction for %s before applying this much escrow", row.Name)
			}
		}
		total += n
		if total > 99999999999999 {
			return 0, chargeInvalid("Escrow amount is too large")
		}
	}
	available, _ := chargeCents(account.Available)
	if total > available {
		return 0, chargeInvalid("Escrow use exceeds the available balance (%s)", account.Available)
	}
	if total == 0 && draft.StopCollection == nil {
		return 0, chargeInvalid("Enter an escrow amount or select a collection change")
	}
	return total, nil
}

type payrollEscrowPosting struct {
	draft PayrollEscrowDraft
	total int64
}

func prepareStatementEscrow(ctx context.Context, tx pgx.Tx, week string, d DriverPayDriver) (payrollEscrowPosting, error) {
	var post payrollEscrowPosting
	var body []byte
	err := tx.QueryRow(ctx, `SELECT escrow_draft FROM driver_pay_weeks WHERE driver_id=$1 AND week_start=$2 AND escrow_draft<>'{}'`, d.ID, week).Scan(&body)
	if errors.Is(err, pgx.ErrNoRows) {
		return post, nil
	}
	if err != nil {
		return post, err
	}
	if err = json.Unmarshal(body, &post.draft); err != nil {
		return post, err
	}
	accounts, err := payrollEscrowAccounts(ctx, tx, week, d.ID)
	if err != nil {
		return post, err
	}
	post.total, err = validatePayrollEscrowDraft(post.draft, accounts, d.Edits.ExpenseDeductions)
	if err != nil {
		return post, err
	}
	var later bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM payroll_settlements WHERE driver_id=$1 AND week_start>$2 AND finalized)
 OR ($4 AND EXISTS(SELECT 1 FROM driver_escrow_payments WHERE escrow_id=$3 AND week_start>=$2 AND amount>0
 AND NOT EXISTS(SELECT 1 FROM driver_escrow_releases r WHERE r.escrow_id=$3 AND NOT r.cancelled AND r.collection_stopped=false AND r.week_start>$2 AND r.week_start<driver_escrow_payments.week_start)))`,
		d.ID, week, post.draft.EscrowID, post.draft.StopCollection != nil && *post.draft.StopCollection).Scan(&later)
	if err == nil && later {
		err = chargeInvalid("Reopen later finalized statements and clear conflicting escrow collections before posting this escrow change")
	}
	return post, err
}

func postStatementEscrow(ctx context.Context, tx pgx.Tx, week, actor string, post payrollEscrowPosting) error {
	if post.draft.EscrowID == "" {
		return nil
	}
	_, err := tx.Exec(ctx, `INSERT INTO driver_escrow_releases(escrow_id,week_start,amount,payroll_return,collection_stopped,updated_by)
 VALUES($1,$2,$3::numeric,$4::numeric,$5,nullif($6,'')::uuid)`, post.draft.EscrowID, week, chargeMoney(post.total), post.draft.Return, post.draft.StopCollection, actor)
	return err
}

func reverseStatementEscrow(ctx context.Context, tx pgx.Tx, week, actor string, d DriverPayDriver) error {
	var posted, blocked bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM driver_escrow_releases r JOIN driver_escrows e ON e.id=r.escrow_id
 WHERE e.driver_id=$1 AND r.week_start=$2 AND NOT r.cancelled AND r.payroll_return IS NOT NULL),
 EXISTS(SELECT 1 FROM payroll_settlements WHERE driver_id=$1 AND week_start>$2 AND finalized)`, d.ID, week).Scan(&posted, &blocked)
	if err != nil || !posted {
		return err
	}
	if blocked {
		return chargeInvalid("Reopen later finalized statements before reversing this escrow transaction")
	}
	_, err = tx.Exec(ctx, `WITH reversed AS (
 UPDATE driver_escrow_releases r SET cancelled=true,updated_by=nullif($3,'')::uuid FROM driver_escrows e
 WHERE e.id=r.escrow_id AND e.driver_id=$1 AND r.week_start=$2 AND NOT r.cancelled AND r.payroll_return IS NOT NULL RETURNING r.escrow_id
 ) UPDATE driver_pay_weeks w SET escrow_draft=jsonb_set(w.escrow_draft,'{version}',to_jsonb(e.balance_version+1))
 FROM reversed r JOIN driver_escrows e ON e.id=r.escrow_id WHERE w.driver_id=$1 AND w.week_start=$2`, d.ID, week, actor)
	return err
}

func markDriverPayMembership(ctx context.Context, tx pgx.Tx, report *DriverPayWeek) error {
	if len(report.Drivers) == 0 {
		return nil
	}
	ids := make([]string, 0, len(report.Drivers))
	for _, d := range report.Drivers {
		if d.ID != "" {
			ids = append(ids, d.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	rows, err := tx.Query(ctx, `SELECT driver_id::text,manually_added,manually_added AND version=1 AND escrow_draft='{}' AND NOT EXISTS(
 SELECT 1 FROM payroll_settlements s WHERE s.driver_id=w.driver_id AND s.week_start=w.week_start),(escrow_draft->>'stopCollection')::boolean
 FROM driver_pay_weeks w WHERE week_start=$1 AND driver_id=ANY($2::uuid[]) AND (manually_added OR escrow_draft<>'{}')`, report.WeekStart, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	type statementActions struct {
		manual, removable bool
		collection        *bool
	}
	actions := map[string]statementActions{}
	for rows.Next() {
		var id string
		var value statementActions
		if err = rows.Scan(&id, &value.manual, &value.removable, &value.collection); err != nil {
			return err
		}
		actions[id] = value
	}
	for i := range report.Drivers {
		d := &report.Drivers[i]
		value := actions[d.ID]
		d.CanRemoveAddition, d.ManualAddition, d.EscrowCollectionChange = value.removable, value.manual, value.collection
	}
	return rows.Err()
}
