package repository

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type payrollCostDetailKey struct{}
type payrollCostTraceKey struct{}
type payrollCostTrace struct {
	tx                         pgx.Tx
	weeks                      map[string][2]DriverPayWeek
	native                     map[string][]PayCostTransaction
	costs                      []payCostRecord
	sources                    []paySourceRecord
	costsLoaded, sourcesLoaded bool
}

func payrollCostDetails(ctx context.Context) bool {
	v, _ := ctx.Value(payrollCostDetailKey{}).(bool)
	return v
}

// Transaction amounts are positive costs; Remaining is the portion carried
// into this statement. Collections are allocated oldest-first, in cents.
type PayCostTransaction struct {
	ID                      string `json:"id"`
	TransactionID           string `json:"transactionId,omitempty"`
	Date                    string `json:"date"`
	PostedDate              string `json:"postedDate,omitempty"`
	WeekStart               string `json:"weekStart"`
	Description             string `json:"description"`
	Location                string `json:"location"`
	DriverName              string `json:"driverName,omitempty"`
	TruckUnit               string `json:"truckUnit,omitempty"`
	Amount                  string `json:"amount"`
	Remaining               string `json:"remaining,omitempty"`
	Carried                 bool   `json:"carried"`
	BalanceOnly             bool   `json:"balanceOnly,omitempty"`
	driverID, truckID, kind string
	sortKey                 string
	driverAttributed        bool
}
type PayCostTransactions struct {
	Preparation  *PayrollPreparation  `json:"preparation,omitempty"`
	Transactions []PayCostTransaction `json:"transactions"`
	SourceTotal  string               `json:"sourceTotal"`
	Carry        string               `json:"carry"`
	Due          string               `json:"due"`
	Notice       string               `json:"notice,omitempty"`
}

func readNativeCostTransactions(ctx context.Context, tx pgx.Tx, week string) ([]PayCostTransaction, error) {
	memo, _ := ctx.Value(payrollCostTraceKey{}).(*payrollCostTrace)
	if memo != nil && memo.tx == tx {
		if items, ok := memo.native[week]; ok {
			return append([]PayCostTransaction{}, items...), nil
		}
	}
	items, err := readPreparedNativeCostTransactions(ctx, tx, week)
	if err == nil && memo != nil && memo.tx == tx {
		if memo.native == nil {
			memo.native = map[string][]PayCostTransaction{}
		}
		memo.native[week] = append([]PayCostTransaction{}, items...)
	}
	return items, err
}

func readNativeCostTransactionsUncached(ctx context.Context, tx pgx.Tx, week string) ([]PayCostTransaction, error) {
	rows, err := tx.Query(ctx, `WITH aliases AS (
 SELECT unit_key,(array_agg(DISTINCT truck_id))[1] id FROM truck_unit_aliases GROUP BY unit_key HAVING count(DISTINCT truck_id)=1
 ), items AS (
 SELECT i.id::text id,f.relay_transaction_id reference,f.purchased_on::text transaction_day,'' posted,
 'fuel' kind,coalesce(team.main_driver_id,f.driver_id) driver,true attributed,a.id truck,
 concat_ws(' · ',nullif(f.merchant_name,''),upper(i.category)) description,
 concat_ws(', ',nullif(f.city,''),nullif(f.state,'')) location,i.total_amount_paid amount,
 f.purchased_at::text||':'||lpad(i.line_number::text,6,'0') sort_key
 FROM fuel_transactions f JOIN fuel_transaction_items i ON i.fuel_transaction_id=f.id
 LEFT JOIN driver_teams team ON team.team_driver_id=f.driver_id AND team.start_week<=f.purchased_on AND (team.end_week IS NULL OR f.purchased_on<team.end_week)
 LEFT JOIN aliases a ON cardinality(f.reported_truck_units)=1 AND a.unit_key=f.reported_truck_units[1]
 WHERE f.relay_environment='production' AND f.driver_id IS NOT NULL AND i.item_kind='fuel'
 AND f.purchased_on>=$1::date AND f.purchased_on<$1::date+7
 UNION ALL
 SELECT t.id::text,coalesce(t.prepass_toll_id::text,t.id::text),t.exit_date::text,t.posting_date::text,'toll',assigned.driver,
 assigned.n=1,t.truck_id,concat_ws(' · ',nullif(t.agency,''),coalesce(nullif(t.exit_plaza_name,''),t.exit_plaza)),coalesce(t.toll_agency_state,''),t.amount,
 t.exit_date::text||' '||t.exit_time::text
 FROM tolls t LEFT JOIN LATERAL (
 SELECT CASE WHEN count(DISTINCT a.driver_id)=1 THEN (array_agg(DISTINCT a.driver_id))[1] END driver,count(DISTINCT a.driver_id) n FROM truck_driver_assignments a
 WHERE a.truck_id=t.truck_id AND a.assigned_at<((t.exit_date+1)::timestamp AT TIME ZONE 'America/New_York')
 AND (a.unassigned_at IS NULL OR a.unassigned_at>=((t.exit_date+1)::timestamp AT TIME ZONE 'America/New_York'))
 ) assigned ON true
 WHERE (t.prepass_environment IS NULL OR t.prepass_environment='production')
 AND t.posting_date>=$1::date AND t.posting_date<$1::date+7
 AND (accounting_cutover_date() IS NULL OR t.exit_date>=accounting_cutover_date())
 ) SELECT i.id,i.reference,i.transaction_day,i.posted,i.kind,coalesce(i.driver::text,''),i.attributed,
 coalesce(i.truck::text,''),coalesce(d.full_name,''),coalesce(t.unit_number,''),i.description,i.location,i.amount::text,i.sort_key
 FROM items i LEFT JOIN drivers d ON d.id=i.driver LEFT JOIN trucks t ON t.id=i.truck ORDER BY i.transaction_day,i.sort_key,i.id`, week)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []PayCostTransaction{}
	for rows.Next() {
		var v PayCostTransaction
		if err = rows.Scan(&v.ID, &v.TransactionID, &v.Date, &v.PostedDate, &v.kind, &v.driverID, &v.driverAttributed, &v.truckID, &v.DriverName, &v.TruckUnit, &v.Description, &v.Location, &v.Amount, &v.sortKey); err != nil {
			return nil, err
		}
		v.WeekStart = week
		result = append(result, v)
	}
	return result, rows.Err()
}

func traceRoutedCostWeek(ctx context.Context, tx pgx.Tx, week, kind, id string) ([]PayCostTransaction, error) {
	memo, _ := ctx.Value(payrollCostTraceKey{}).(*payrollCostTrace)
	if memo != nil && memo.tx == tx {
		if reports, ok := memo.weeks[week]; ok {
			return selectTracedCost(reports, kind, id), nil
		}
	}
	// Routing can only redistribute original native transactions. A complete
	// empty source week needs no payer reconstruction; any frozen total or carry
	// discrepancy is still reported by preparedCostEvidence below.
	if payrollNormalizedInputs(ctx) {
		items, err := readNativeCostTransactions(ctx, tx, week)
		if err != nil {
			return nil, err
		}
		if len(items) == 0 {
			if memo != nil && memo.tx == tx {
				memo.weeks[week] = [2]DriverPayWeek{}
			}
			return nil, nil
		}
	}
	w, _ := chargeWeek(week)
	ctx = payrollWeekContext(ctx, week)
	ctx = context.WithValue(ctx, payrollReadKey{}, (*payrollReadState)(nil))
	source, err := readDriverPayCostWeek(ctx, tx, w)
	if err != nil {
		return nil, err
	}
	drivers, investors, err := routeTruckPaySources(ctx, tx, source)
	if err != nil {
		return nil, err
	}
	if err = routeTeamPay(ctx, tx, &drivers); err != nil {
		return nil, err
	}
	reports := [2]DriverPayWeek{drivers, investors}
	if memo != nil && memo.tx == tx {
		memo.weeks[week] = reports
	}
	return selectTracedCost(reports, kind, id), nil
}

func selectTracedCost(reports [2]DriverPayWeek, kind, id string) []PayCostTransaction {
	report := reports[0]
	if kind == "investor" {
		report = reports[1]
	}
	for _, d := range report.Drivers {
		if d.ID == id {
			sort.Slice(d.costTransactions, func(i, j int) bool {
				a, b := d.costTransactions[i], d.costTransactions[j]
				if a.Date != b.Date {
					return a.Date < b.Date
				}
				if a.sortKey != b.sortKey {
					return a.sortKey < b.sortKey
				}
				return a.ID < b.ID
			})
			return append([]PayCostTransaction(nil), d.costTransactions...)
		}
	}
	return nil
}

type costHistoryWeek struct{ week, base, amount, kind, id string }

// Carry attribution is a read-only projection of the same saved weekly ledger.
// Changed or missing source evidence is shown as a balance row, never invented
// as a provider transaction. Credits offset the oldest remaining costs first.
func allocateCostCarry(pending []PayCostTransaction, rows []PayCostTransaction, base, collected int64, week string) []PayCostTransaction {
	var total int64
	for _, row := range rows {
		n, _ := chargeCents(row.Amount)
		total += n
		row.Carried = true
		row.Remaining = row.Amount
		pending = append(pending, row)
	}
	if total != base {
		pending = append(pending, PayCostTransaction{ID: "balance:" + week, Date: week, WeekStart: week, Description: "Historical balance · source details changed", Amount: chargeMoney(base - total), Remaining: chargeMoney(base - total), Carried: true, BalanceOnly: true})
	}
	// Pool source credits and the signed collection, then reduce debits FIFO.
	credit := collected
	kept := pending[:0]
	for _, row := range pending {
		n, _ := chargeCents(row.Remaining)
		if n < 0 {
			credit -= n
		} else if n > 0 {
			kept = append(kept, row)
		}
	}
	pending = kept
	if credit < 0 {
		pending = append(pending, PayCostTransaction{ID: "balance:adjustment:" + week, Date: week, WeekStart: week, Description: "Historical collection adjustment", Amount: chargeMoney(-credit), Remaining: chargeMoney(-credit), Carried: true, BalanceOnly: true})
		return pending
	}
	kept = pending[:0]
	for _, row := range pending {
		n, _ := chargeCents(row.Remaining)
		paid := min(n, credit)
		credit -= paid
		n -= paid
		if n != 0 {
			row.Remaining = chargeMoney(n)
			kept = append(kept, row)
		}
	}
	if credit > 0 {
		kept = append(kept, PayCostTransaction{ID: "balance:credit:" + week, Date: week, WeekStart: week, Description: "Carried fuel/toll credit", Amount: chargeMoney(-credit), Remaining: chargeMoney(-credit), Carried: true, BalanceOnly: true})
	}
	return kept
}

func (r *DriverPayRepository) CostTransactions(ctx context.Context, kind, id string, week time.Time, cost string, revisions ...string) (PayCostTransactions, error) {
	if r.preparedReads {
		return r.PreparedCostTransactions(ctx, kind, id, week, cost, revisions...)
	}
	result := PayCostTransactions{Transactions: []PayCostTransaction{}}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	if err = payrollQueryPlans(ctx, tx); err != nil {
		return result, err
	}
	ctx = payrollWeekContext(ctx, week.Format(time.DateOnly))
	node := "driver"
	if kind == "investor" {
		node = "truck"
	}
	ctx, err = selectPayrollContext(ctx, tx, node, id)
	if err != nil {
		return result, err
	}
	var report DriverPayWeek
	if kind == "investor" {
		report, err = readInvestorPay(ctx, tx, week)
	} else {
		report, err = readDriverPayWeek(ctx, tx, week, id)
	}
	if err != nil {
		return result, err
	}
	var card *DriverPayDriver
	for i := range report.Drivers {
		if report.Drivers[i].ID == id {
			card = &report.Drivers[i]
			break
		}
	}
	if card == nil {
		return result, pgx.ErrNoRows
	}
	result, err = preparedCostEvidence(ctx, tx, kind, id, week.Format(time.DateOnly), cost, *card)
	if err != nil {
		return result, err
	}
	return result, tx.Commit(ctx)
}

// Evidence belongs to the same immutable version as the displayed financial
// result. Stale evidence is explicit and never reconstructed on this read path.
func (r *DriverPayRepository) PreparedCostTransactions(ctx context.Context, kind, id string, week time.Time, cost string, revisions ...string) (PayCostTransactions, error) {
	var result PayCostTransactions
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	current, err := payrollProjectionFence(ctx, tx)
	if err != nil {
		return result, err
	}
	var body []byte
	var snapshot, calendar, calculator string
	var schema int
	var revision string
	if len(revisions) > 0 {
		revision = revisions[0]
	}
	err = tx.QueryRow(ctx, `SELECT v.cost_evidence->$4,g.input_snapshot,g.calendar_week::text,g.calculator_version,g.schema_version
FROM payroll_statement_versions v LEFT JOIN payroll_statement_state s ON s.kind=v.kind AND s.statement_id=v.statement_id AND s.week_start=v.week_start
LEFT JOIN payroll_projection_weeks w ON w.week_start=v.week_start
JOIN payroll_projection_generations g ON g.id=CASE WHEN s.version_id=v.id THEN w.generation_id ELSE v.generation_id END
WHERE v.kind=$1 AND v.statement_id=$2::uuid AND v.week_start=$3 AND s.version_id=v.id
AND v.id=coalesce(nullif($5,'')::uuid,s.version_id)`, kind, id, week, cost, revision).Scan(&body, &snapshot, &calendar, &calculator, &schema)
	if errors.Is(err, pgx.ErrNoRows) {
		if revision != "" {
			return result, ErrPayrollVersionExpired
		}
		return result, ErrPayrollPreparing
	}
	if err != nil {
		return result, err
	}
	if err = json.Unmarshal(body, &result); err != nil {
		return result, err
	}
	result.Preparation = &PayrollPreparation{State: "ready"}
	if current != snapshot || calendar != ChargeCurrentWeek() || calculator != payrollCalculatorVersion || schema != payrollProjectionSchemaVersion {
		result.Preparation.State = "updating"
	}
	return result, tx.Commit(ctx)
}

func preparedCostEvidence(ctx context.Context, tx pgx.Tx, kind, id, week, cost string, card DriverPayDriver) (PayCostTransactions, error) {
	result := PayCostTransactions{Transactions: []PayCostTransaction{}}
	result.SourceTotal = card.FuelTotal
	result.Carry = "0.00"
	if cost == "toll" {
		result.SourceTotal = card.TollTotal
	}
	if c := card.Edits.Costs; c != nil {
		result.Carry = c.FuelCarry
		due, _ := chargeCents(c.FuelDue)
		carry, _ := chargeCents(c.FuelCarry)
		result.SourceTotal = chargeMoney(due - carry)
		if cost == "toll" {
			result.Carry = c.TollCarry
			due, _ = chargeCents(c.TollDue)
			carry, _ = chargeCents(c.TollCarry)
			result.SourceTotal = chargeMoney(due - carry)
		}
	}
	history := []costHistoryWeek{}
	carry, _ := chargeCents(result.Carry)
	memo, _ := ctx.Value(payrollCostTraceKey{}).(*payrollCostTrace)
	// Exact weeks are needed here, unlike the grouped report fast path.
	ctx = context.WithValue(ctx, payrollCostDetailKey{}, true)
	if carry != 0 && kind == "driver" {
		var records []payCostRecord
		var e error
		if memo != nil && memo.tx == tx && memo.costsLoaded {
			records = memo.costs
		} else {
			records, e = readPayCostRecords(context.WithValue(ctx, payrollReadKey{}, (*payrollReadState)(nil)), tx)
			if e == nil && memo != nil && memo.tx == tx {
				memo.costs, memo.costsLoaded = records, true
			}
		}
		if e != nil {
			return result, e
		}
		for _, v := range records {
			if v.Driver != id || v.Week >= week {
				continue
			}
			base, amount := v.Fuel, v.FuelAmount
			if cost == "toll" {
				base, amount = v.Toll, v.TollAmount
			}
			history = append(history, costHistoryWeek{v.Week, base, amount, "driver", id})
		}
	} else if carry != 0 {
		var records []paySourceRecord
		var e error
		if memo != nil && memo.tx == tx && memo.sourcesLoaded {
			records = memo.sources
		} else {
			records, e = readPaySources(ctx, tx)
			if e == nil && memo != nil && memo.tx == tx {
				memo.sources, memo.sourcesLoaded = records, true
			}
		}
		if e != nil {
			return result, e
		}
		for _, v := range records {
			if v.Source != "investor_cost:"+id+":"+cost || v.Owner != card.InvestorID || v.Week >= week {
				continue
			}
			n, _ := chargeCents(v.Base)
			history = append(history, costHistoryWeek{v.Week, chargeMoney(-n), v.Amount, v.Kind, v.ID})
		}
	}
	sort.Slice(history, func(i, j int) bool { return history[i].week < history[j].week })
	var running int64
	start := 0
	for i, h := range history {
		b, _ := chargeCents(h.base)
		a, _ := chargeCents(h.amount)
		running += b + a
		if running == 0 {
			start = i + 1
		}
	}
	history = history[start:]
	pending := []PayCostTransaction{}
	if carry != 0 {
		for _, h := range history {
			rows, e := traceRoutedCostWeek(ctx, tx, h.week, h.kind, h.id)
			if e != nil {
				return result, e
			}
			filtered := []PayCostTransaction{}
			for _, v := range rows {
				if v.kind == cost {
					filtered = append(filtered, v)
				}
			}
			base, _ := chargeCents(h.base)
			amount, _ := chargeCents(h.amount)
			pending = allocateCostCarry(pending, filtered, base, -amount, h.week)
		}
	}
	var traced int64
	for _, v := range pending {
		n, _ := chargeCents(v.Remaining)
		traced += n
	}
	if traced != carry {
		pending = append(pending, PayCostTransaction{ID: "balance:unresolved", Description: "Saved historical balance · source details unavailable", Amount: chargeMoney(carry - traced), Remaining: chargeMoney(carry - traced), Carried: true, BalanceOnly: true})
	}
	result.Transactions = append(result.Transactions, pending...)
	rows, err := traceRoutedCostWeek(ctx, tx, week, kind, id)
	if err != nil {
		return result, err
	}
	var source int64
	for _, v := range rows {
		if v.kind == cost {
			result.Transactions = append(result.Transactions, v)
			n, _ := chargeCents(v.Amount)
			source += n
		}
	}
	target, _ := chargeCents(result.SourceTotal)
	if target != source {
		result.Notice = "The saved source total differs from current transaction details. Finalized amounts remain unchanged."
	}
	result.Due = chargeMoney(target + carry)
	if carry != 0 {
		result.Notice = strings.TrimSpace(result.Notice + " Carried balances are allocated to the oldest transactions first; Remaining shows the amount carried into this week.")
	}
	return result, nil
}
