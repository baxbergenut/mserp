package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type AccountingOpening struct {
	DriverID  string `json:"driverId"`
	Name      string `json:"name,omitempty"`
	Amount    string `json:"amount"`
	Reference string `json:"reference"`
}
type AccountingCutoverDraft struct {
	Version          int                 `json:"version"`
	WeekStart        string              `json:"weekStart"`
	BackupReference  string              `json:"backupReference"`
	SheetsReference  string              `json:"sheetsReference"`
	Openings         []AccountingOpening `json:"openings"`
	LegacyExpenseIDs []string            `json:"legacyExpenseIds"`
}
type AccountingCutover struct {
	AccountingCutoverDraft
	Active      bool            `json:"active"`
	ActivatedAt *time.Time      `json:"activatedAt,omitempty"`
	Receipt     json.RawMessage `json:"receipt,omitempty"`
	Drivers     []PayPerson     `json:"drivers"`
}
type CutoverItem struct {
	Kind        string `json:"kind"`
	ID          string `json:"id"`
	Week        string `json:"week"`
	Description string `json:"description"`
	Blocking    bool   `json:"blocking"`
}
type CutoverPreview struct {
	ID            string        `json:"id"`
	Version       int           `json:"version"`
	Revision      string        `json:"revision"`
	Items         []CutoverItem `json:"items"`
	ArchivedWeeks int           `json:"archivedWeeks"`
}
type cutoverArchive struct {
	Kind   string        `json:"kind"`
	Week   string        `json:"week"`
	Report DriverPayWeek `json:"report"`
}

func readAccountingCutover(ctx context.Context, q chargeQuery) (AccountingCutover, error) {
	var v AccountingCutover
	var body []byte
	err := q.QueryRow(ctx, `SELECT version,week_start::text,active,draft,activated_at,receipt FROM accounting_cutover WHERE id`).Scan(&v.Version, &v.WeekStart, &v.Active, &body, &v.ActivatedAt, &v.Receipt)
	if err != nil {
		return v, err
	}
	version, week := v.Version, v.WeekStart
	if err = json.Unmarshal(body, &v.AccountingCutoverDraft); err != nil {
		return v, err
	}
	v.Version, v.WeekStart = version, week
	if v.Openings == nil {
		v.Openings = []AccountingOpening{}
	}
	if v.LegacyExpenseIDs == nil {
		v.LegacyExpenseIDs = []string{}
	}
	if v.Active {
		v.Openings = []AccountingOpening{}
		rows, e := q.Query(ctx, `SELECT driver_id::text,sum(amount)::text,string_agg(reference,'; ' ORDER BY created_at) FROM (
 SELECT driver_id,amount,reference,'-infinity'::timestamptz created_at FROM accounting_rate_openings
 UNION ALL SELECT driver_id,amount,reference,created_at FROM accounting_rate_corrections) s GROUP BY driver_id ORDER BY driver_id`)
		if e != nil {
			return v, e
		}
		for rows.Next() {
			var o AccountingOpening
			if e = rows.Scan(&o.DriverID, &o.Amount, &o.Reference); e != nil {
				rows.Close()
				return v, e
			}
			v.Openings = append(v.Openings, o)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return v, e
		}
	}
	v.Drivers = []PayPerson{}
	rows, err := q.Query(ctx, `SELECT id::text,full_name FROM drivers WHERE active OR EXISTS(SELECT 1 FROM gross_board_entries b WHERE b.driver_id=drivers.id AND b.service_date>=$1::date) ORDER BY full_name,id`, week)
	if err != nil {
		return v, err
	}
	defer rows.Close()
	for rows.Next() {
		var d PayPerson
		if err = rows.Scan(&d.ID, &d.Name); err != nil {
			return v, err
		}
		v.Drivers = append(v.Drivers, d)
	}
	return v, rows.Err()
}
func (r *DriverPayRepository) AccountingCutover(ctx context.Context) (AccountingCutover, error) {
	return readAccountingCutover(ctx, r.pool)
}

func (r *DriverPayRepository) SaveAccountingCutover(ctx context.Context, d AccountingCutoverDraft) (AccountingCutover, error) {
	week, err := chargeWeek(d.WeekStart)
	if err != nil || week.Format(time.DateOnly) != d.WeekStart || d.WeekStart > ChargeCurrentWeek() {
		return AccountingCutover{}, chargeInvalid("Choose a Monday no later than the current week")
	}
	if len(d.Openings) > 5000 || len(d.LegacyExpenseIDs) > 10000 || len(d.BackupReference) > 2000 || len(d.SheetsReference) > 2000 {
		return AccountingCutover{}, chargeInvalid("Cutover draft is too large")
	}
	seen := map[string]bool{}
	for i, v := range d.Openings {
		if _, err = cutoverUUID(v.DriverID); err != nil || seen[v.DriverID] {
			return AccountingCutover{}, chargeInvalid("Use distinct driver IDs")
		}
		seen[v.DriverID] = true
		n, e := chargeCents(v.Amount)
		if e != nil || strings.TrimSpace(v.Reference) == "" || len(v.Reference) > 2000 {
			return AccountingCutover{}, chargeInvalid("Every opening needs an exact amount and Sheets reference")
		}
		d.Openings[i].Amount = chargeMoney(n)
	}
	for _, id := range d.LegacyExpenseIDs {
		if _, err = cutoverUUID(id); err != nil {
			return AccountingCutover{}, chargeInvalid("Invalid legacy expense ID")
		}
	}
	body, _ := json.Marshal(d)
	tag, err := r.pool.Exec(ctx, `UPDATE accounting_cutover SET week_start=$1::date,draft=$2,version=version+1 WHERE id AND NOT active AND version=$3`, d.WeekStart, body, d.Version)
	if err != nil {
		return AccountingCutover{}, err
	}
	if tag.RowsAffected() != 1 {
		return AccountingCutover{}, ErrDriverPayConflict
	}
	return r.AccountingCutover(ctx)
}

// All source revisions participate. Preview/archive writes themselves do not
// invalidate a financial preview; a financial commit always does.
func cutoverRevision(ctx context.Context, q chargeQuery) (string, error) {
	var raw string
	err := q.QueryRow(ctx, `SELECT coalesce(string_agg(table_name||':'||revision::text,',' ORDER BY table_name),'') FROM payroll_cache_revisions WHERE NOT table_name=ANY($1::text[])`, payrollUnrelatedTables).Scan(&raw)
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:]), err
}
func cutoverItems(ctx context.Context, tx pgx.Tx, d AccountingCutover) ([]CutoverItem, error) {
	items := []CutoverItem{}
	add := func(k, id, w, desc string, blocking bool) {
		items = append(items, CutoverItem{k, id, w, desc, blocking})
	}
	if strings.TrimSpace(d.BackupReference) == "" || strings.TrimSpace(d.SheetsReference) == "" {
		add("preparation", "", "", "Record the database backup and dated Sheets copy references", true)
	}
	openings := map[string]bool{}
	for _, o := range d.Openings {
		openings[o.DriverID] = true
	}
	for _, p := range d.Drivers {
		if !openings[p.ID] {
			add("opening", p.ID, d.WeekStart, "Confirm the Sheets rate opening for "+p.Name+" (zero must be explicit)", true)
		}
	}
	rows, err := tx.Query(ctx, `SELECT kind,id,w::text,description,blocking FROM (
 SELECT 'expense' kind,e.id::text id,p.week_start w,'A saved current-week expense collection belongs to Sheets; correct it through payroll before activation' description,true blocking
 FROM expenses e JOIN expense_payments p ON p.expense_id=e.id WHERE p.week_start>=$1::date AND p.amount<>0 AND (e.expense_date IS NULL OR e.expense_date<$1::date OR e.id=ANY($2::uuid[]))
 UNION ALL SELECT 'reimburse',e.id::text,p.week_start,'A saved reimbursement belongs to Sheets; correct it before activation',true FROM expenses e JOIN expense_reimbursement_payments p ON p.expense_id=e.id WHERE p.week_start>=$1::date AND p.amount<>0 AND (e.expense_date IS NULL OR e.expense_date<$1::date OR e.id=ANY($2::uuid[]))
 UNION ALL SELECT 'escrow',p.escrow_id::text,p.week_start,'An existing escrow has a current/future collection; replace it explicitly with a Sheets adjustment',true FROM driver_escrow_payments p WHERE p.week_start>=$1::date AND p.amount<>0
 UNION ALL SELECT 'escrow',p.escrow_id::text,p.week_start,'An existing escrow has a current/future release; resolve it before activation',true FROM driver_escrow_releases p WHERE p.week_start>=$1::date AND NOT p.cancelled
 UNION ALL SELECT 'installment',o.schedule_id::text,o.week_start,'An existing installment has a saved current/future collection; resolve it before activation',true FROM driver_charge_occurrences o JOIN driver_charge_schedules s ON s.id=o.schedule_id WHERE s.kind='installment' AND o.week_start>=$1::date AND o.amount<>0
 UNION ALL SELECT 'driver',driver_id::text,week_start,'Review this saved current/future statement against the new boundary; its notes and edits are retained',false FROM driver_pay_weeks WHERE week_start>=$1::date
 UNION ALL SELECT 'investor',truck_id::text,week_start,'Review this saved current/future statement against the new boundary; its notes and edits are retained',false FROM investor_pay_weeks WHERE week_start>=$1::date
 UNION ALL SELECT 'finalized',driver_id::text,week_start,'Reopen this post-cutover statement before changing its financial boundary',true FROM payroll_settlements WHERE finalized AND week_start>=$1::date
 UNION ALL SELECT 'finalized',truck_id::text,week_start,'Reopen this post-cutover investor statement before changing its financial boundary',true FROM investor_pay_weeks WHERE finalized AND week_start>=$1::date
 UNION ALL SELECT 'overlap',e.id::text,$1::date,concat(coalesce(nullif(e.expense_type,''),e.category),' · ',e.expense_date::text,' · ',coalesce(e.amount::text,'amount missing'),' · ',coalesce((SELECT full_name FROM drivers WHERE id=coalesce(e.charge_driver_id,e.driver_id)),nullif(e.driver_name,''),'unassigned'),': confirm whether this Monday–Wednesday expense was handled in the last Sheets payroll'),false FROM expenses e WHERE e.expense_date>=$1::date AND e.expense_date<$1::date+3 AND NOT e.id=ANY($2::uuid[])
 UNION ALL SELECT 'transfer',driver_id::text,source_date,'A load crosses the cutover boundary; resolve its placement before activation',true FROM gross_board_transfers WHERE active AND least(source_date,settlement_date,dispatch_date)<$1::date AND greatest(source_date,settlement_date,dispatch_date)>=$1::date
 ) x ORDER BY blocking DESC,kind,w,id`, d.WeekStart, d.LegacyExpenseIDs)
	if err != nil {
		return items, err
	}
	defer rows.Close()
	for rows.Next() {
		var i CutoverItem
		if err = rows.Scan(&i.Kind, &i.ID, &i.Week, &i.Description, &i.Blocking); err != nil {
			return items, err
		}
		items = append(items, i)
	}
	return items, rows.Err()
}
func (r *DriverPayRepository) PreviewAccountingCutover(ctx context.Context, actor string) (CutoverPreview, error) {
	out := CutoverPreview{}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	if err = tx.QueryRow(ctx, `SELECT gen_random_uuid()::text`).Scan(&out.ID); err != nil {
		return out, err
	}
	d, err := readAccountingCutover(ctx, tx)
	if err != nil {
		return out, err
	}
	if d.Active {
		return out, chargeInvalid("Cutover is already active")
	}
	out.Version = d.Version
	if out.Revision, err = cutoverRevision(ctx, tx); err != nil {
		return out, err
	}
	if out.Items, err = cutoverItems(ctx, tx, d); err != nil {
		return out, err
	}
	weeks := []string{}
	rows, err := tx.Query(ctx, `SELECT DISTINCT w::text FROM (
 SELECT date_trunc('week',service_date)::date w FROM gross_board_entries UNION SELECT date_trunc('week',service_date)::date FROM gross_board_extra_entries
 UNION SELECT week_start FROM driver_pay_weeks UNION SELECT week_start FROM payroll_settlements UNION SELECT week_start FROM investor_pay_weeks) s WHERE w<$1::date ORDER BY w`, d.WeekStart)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var w string
		if err = rows.Scan(&w); err != nil {
			rows.Close()
			return out, err
		}
		weeks = append(weeks, w)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	archives := []cutoverArchive{}
	for _, w := range weeks {
		week, _ := time.Parse(time.DateOnly, w)
		driver, e := readDriverPayWeek(ctx, tx, week, "")
		if e != nil {
			return out, e
		}
		investor, e := readInvestorPay(ctx, tx, week)
		if e != nil {
			return out, e
		}
		driver.ManagedInSheets = true
		investor.ManagedInSheets = true
		archives = append(archives, cutoverArchive{"driver", w, driver}, cutoverArchive{"investor", w, investor})
	}
	out.ArchivedWeeks = len(weeks)
	// Preview the actual boundary against current/future saved statements. The
	// savepoint rolls back temporary source classifications and cache revisions.
	currentWeeks := []string{}
	currentRows, err := tx.Query(ctx, `SELECT DISTINCT w::text FROM (SELECT week_start w FROM driver_pay_weeks UNION SELECT week_start FROM investor_pay_weeks) s WHERE w >= $1::date ORDER BY w`, d.WeekStart)
	if err != nil {
		return out, err
	}
	for currentRows.Next() {
		var w string
		if err = currentRows.Scan(&w); err != nil {
			currentRows.Close()
			return out, err
		}
		currentWeeks = append(currentWeeks, w)
	}
	err = currentRows.Err()
	currentRows.Close()
	if err != nil {
		return out, err
	}
	before := map[string]DriverPayWeek{}
	for _, w := range currentWeeks {
		week, _ := time.Parse(time.DateOnly, w)
		for _, kind := range []string{"driver", "investor"} {
			var report DriverPayWeek
			if kind == "driver" {
				report, err = readDriverPayWeek(ctx, tx, week, "")
			} else {
				report, err = readInvestorPay(ctx, tx, week)
			}
			if err != nil {
				return out, err
			}
			before[kind+w] = report
		}
	}
	if _, err = tx.Exec(ctx, `SAVEPOINT cutover_projection`); err != nil {
		return out, err
	}
	if _, err = tx.Exec(ctx, `SELECT set_config('mserp.cutover_preview',$1,true)`, d.WeekStart); err != nil {
		return out, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO accounting_legacy_sources(kind,source_id) SELECT 'escrow',id FROM driver_escrows UNION SELECT 'installment',id FROM driver_charge_schedules WHERE kind='installment' UNION SELECT 'expense',unnest($1::uuid[])`, d.LegacyExpenseIDs); err != nil {
		return out, err
	}
	for _, w := range currentWeeks {
		week, _ := time.Parse(time.DateOnly, w)
		for _, kind := range []string{"driver", "investor"} {
			var after DriverPayWeek
			if kind == "driver" {
				after, err = readDriverPayWeek(ctx, tx, week, "")
			} else {
				after, err = readInvestorPay(ctx, tx, week)
			}
			if err != nil {
				return out, err
			}
			old := map[string]string{}
			for _, card := range before[kind+w].Drivers {
				old[card.ID] = summarizePayStatement(card).Summary.Payable
			}
			for _, card := range after.Drivers {
				next := summarizePayStatement(card).Summary.Payable
				if old[card.ID] != next {
					out.Items = append(out.Items, CutoverItem{kind, card.ID, w, "Review payable for " + card.FullName + ": " + old[card.ID] + " → " + next, false})
				}
				delete(old, card.ID)
				for _, source := range card.Edits.SourceCharges {
					if source.Saved {
						due, _ := chargeCents(source.Due)
						if e := validatePaySourceAmount(source.Name, source.Amount, due); e != nil {
							out.Items = append(out.Items, CutoverItem{kind, card.ID, w, e.Error() + "; correct the saved source collection before activation", true})
						}
					}
				}
				if state := card.Edits.Costs; state != nil {
					for _, cost := range []struct {
						name, due string
						override  *string
					}{{"Fuel", state.FuelDue, card.Edits.FuelOverride}, {"Toll", state.TollDue, card.Edits.TollOverride}} {
						if cost.override != nil {
							due, _ := chargeCents(cost.due)
							if e := validatePaySourceAmount(cost.name, *cost.override, -due); e != nil {
								out.Items = append(out.Items, CutoverItem{kind, card.ID, w, e.Error() + "; correct the saved amount before activation", true})
							}
						}
					}
				}
			}
			for id, amount := range old {
				out.Items = append(out.Items, CutoverItem{kind, id, w, "This legacy-only statement leaves active payroll (previous payable " + amount + ")", false})
			}
		}
	}
	if _, err = tx.Exec(ctx, `ROLLBACK TO SAVEPOINT cutover_projection`); err != nil {
		return out, err
	}
	body, _ := json.Marshal(out)
	archiveBody, _ := json.Marshal(archives)
	_, err = tx.Exec(ctx, `INSERT INTO accounting_cutover_previews(id,version,revision,result,archives,created_by) VALUES($1,$2,$3,$4,$5,$6)`, out.ID, out.Version, out.Revision, body, archiveBody, actor)
	if err != nil {
		return out, err
	}
	return out, tx.Commit(ctx)
}
func (r *DriverPayRepository) ActivateAccountingCutover(ctx context.Context, id, actor string) (AccountingCutover, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return AccountingCutover{}, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SET LOCAL lock_timeout='5s'`); err != nil {
		return AccountingCutover{}, err
	}
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(736281940)`); err != nil {
		return AccountingCutover{}, err
	}
	// Source-table locks fence sync/import writers that do not take the financial lock.
	var tables []string
	rows, err := tx.Query(ctx, `SELECT quote_ident(table_name) FROM payroll_cache_revisions WHERE NOT table_name=ANY($1::text[]) AND to_regclass(table_name) IS NOT NULL ORDER BY table_name`, payrollUnrelatedTables)
	if err != nil {
		return AccountingCutover{}, err
	}
	for rows.Next() {
		var t string
		if err = rows.Scan(&t); err != nil {
			rows.Close()
			return AccountingCutover{}, err
		}
		tables = append(tables, t)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return AccountingCutover{}, err
	}
	if _, err = tx.Exec(ctx, `LOCK TABLE `+strings.Join(tables, ",")+` IN SHARE ROW EXCLUSIVE MODE`); err != nil {
		return AccountingCutover{}, err
	}
	d, err := readAccountingCutover(ctx, tx)
	if err != nil {
		return d, err
	}
	if d.Active {
		return d, chargeInvalid("Cutover is already active")
	}
	var p CutoverPreview
	var body, archives []byte
	if err = tx.QueryRow(ctx, `SELECT result,archives FROM accounting_cutover_previews WHERE id=$1`, id).Scan(&body, &archives); err != nil {
		return d, err
	}
	if err = json.Unmarshal(body, &p); err != nil {
		return d, err
	}
	rev, err := cutoverRevision(ctx, tx)
	if err != nil {
		return d, err
	}
	if p.Version != d.Version || p.Revision != rev {
		return d, ErrDriverPayConflict
	}
	for _, i := range p.Items {
		if i.Blocking {
			return d, chargeInvalid("Resolve every blocking preview item before activation")
		}
	}
	for _, v := range d.Openings {
		if _, err = tx.Exec(ctx, `INSERT INTO accounting_rate_openings(driver_id,amount,reference) VALUES($1,$2::numeric,$3)`, v.DriverID, v.Amount, v.Reference); err != nil {
			return d, err
		}
	}
	if _, err = tx.Exec(ctx, `INSERT INTO accounting_legacy_sources(kind,source_id) SELECT 'escrow',id FROM driver_escrows UNION SELECT 'installment',id FROM driver_charge_schedules WHERE kind='installment' UNION SELECT 'expense',unnest($1::uuid[])`, d.LegacyExpenseIDs); err != nil {
		return d, err
	}
	var snapshots []cutoverArchive
	if err = json.Unmarshal(archives, &snapshots); err != nil {
		return d, err
	}
	for _, s := range snapshots {
		b, _ := json.Marshal(s.Report)
		if _, err = tx.Exec(ctx, `INSERT INTO accounting_payroll_archives(kind,week_start,report) VALUES($1,$2::date,$3)`, s.Kind, s.Week, b); err != nil {
			return d, err
		}
	}
	receipt, _ := json.Marshal(map[string]any{"preview": p, "draft": d.AccountingCutoverDraft, "actorId": actor})
	if _, err = tx.Exec(ctx, `UPDATE accounting_cutover SET active=true,version=version+1,activated_at=now(),activated_by=$1,receipt=$2 WHERE id`, actor, receipt); err != nil {
		return d, err
	}
	if err = tx.Commit(ctx); err != nil {
		return d, err
	}
	return r.AccountingCutover(ctx)
}

func archivedPayroll(ctx context.Context, tx pgx.Tx, kind string, week time.Time) (DriverPayWeek, bool, error) {
	var active bool
	if err := tx.QueryRow(ctx, `SELECT coalesce($1::date<accounting_cutover_date(),false)`, week).Scan(&active); err != nil {
		return DriverPayWeek{}, false, err
	}
	if !active {
		return DriverPayWeek{}, false, nil
	}
	out := DriverPayWeek{WeekStart: week.Format(time.DateOnly), ManagedInSheets: true, Drivers: []DriverPayDriver{}}
	var body []byte
	err := tx.QueryRow(ctx, `SELECT report FROM accounting_payroll_archives WHERE kind=$1 AND week_start=$2::date`, kind, week).Scan(&body)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, true, nil
	}
	if err != nil {
		return out, true, err
	}
	err = json.Unmarshal(body, &out)
	out.ManagedInSheets = true
	return out, true, err
}

func cutoverUUID(s string) (pgtype.UUID, error) { var u pgtype.UUID; err := u.Scan(s); return u, err }

type RateOpeningCorrection struct {
	ID             string  `json:"id"`
	DriverID       string  `json:"driverId"`
	Amount         string  `json:"amount"`
	Reference      string  `json:"reference"`
	ExpectedAmount *string `json:"expectedAmount"`
}

// Append a signed correction; never rewrite the reviewed activation opening.
func (r *DriverPayRepository) CorrectRateOpening(ctx context.Context, input RateOpeningCorrection, actor string) (AccountingCutover, error) {
	if _, e := cutoverUUID(input.ID); e != nil {
		return AccountingCutover{}, chargeInvalid("Invalid request ID")
	}
	if _, e := cutoverUUID(input.DriverID); e != nil {
		return AccountingCutover{}, chargeInvalid("Invalid driver")
	}
	n, e := chargeCents(input.Amount)
	if e != nil || strings.TrimSpace(input.Reference) == "" || len(input.Reference) > 2000 {
		return AccountingCutover{}, chargeInvalid("Provide an exact signed correction and Sheets reference")
	}
	tx, e := r.pool.Begin(ctx)
	if e != nil {
		return AccountingCutover{}, e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(736281940)`); e != nil {
		return AccountingCutover{}, e
	}
	var same bool
	e = tx.QueryRow(ctx, `SELECT driver_id=$2::uuid AND amount=$3::numeric AND reference=$4 FROM accounting_rate_corrections WHERE id=$1`, input.ID, input.DriverID, chargeMoney(n), strings.TrimSpace(input.Reference)).Scan(&same)
	if e == nil {
		if !same {
			return AccountingCutover{}, ErrDriverPayConflict
		}
		if e = tx.Commit(ctx); e != nil {
			return AccountingCutover{}, e
		}
		return r.AccountingCutover(ctx)
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return AccountingCutover{}, e
	}
	d, e := readAccountingCutover(ctx, tx)
	if e != nil {
		return d, e
	}
	if !d.Active {
		return d, chargeInvalid("Review openings in the cutover draft before activation")
	}
	var current *string
	for _, o := range d.Openings {
		if o.DriverID == input.DriverID {
			v := o.Amount
			current = &v
			break
		}
	}
	if (current == nil) != (input.ExpectedAmount == nil) || (current != nil && *current != *input.ExpectedAmount) {
		return d, ErrDriverPayConflict
	}
	if _, e = tx.Exec(ctx, `INSERT INTO accounting_rate_corrections(id,driver_id,amount,reference,actor_id) VALUES($1,$2,$3::numeric,$4,$5)`, input.ID, input.DriverID, chargeMoney(n), strings.TrimSpace(input.Reference), actor); e != nil {
		return d, e
	}
	if e = tx.Commit(ctx); e != nil {
		return d, e
	}
	return r.AccountingCutover(ctx)
}

// Posting date still chooses the payroll week. A crossing from the Sheets era
// is held outside automatic payroll, even when PrePass posts it after cutover.
// Accounting reconciles it in Sheets and uses a referenced weekly adjustment.
func cutoverPayrollNotices(ctx context.Context, tx pgx.Tx, report *DriverPayWeek) error {
	var count int
	err := tx.QueryRow(ctx, `SELECT count(*) FROM tolls WHERE posting_date >= $1::date AND posting_date < $1::date+7
 AND posting_date >= accounting_cutover_date() AND (exit_date < accounting_cutover_date() OR exit_date IS NULL)
 AND (prepass_environment IS NULL OR prepass_environment='production')`, report.WeekStart).Scan(&count)
	if err == nil && count > 0 {
		report.Notices = append(report.Notices, fmt.Sprintf("%d late-posted tolls have crossings before cutover or unknown crossing dates. They are excluded from automatic payroll. Review them in Tolls and Sheets; use a referenced Sheets adjustment for any collection still required.", count))
	}
	return err
}
