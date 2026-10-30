package repository

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type payrollCostDetailKey struct{}

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
	Transactions []PayCostTransaction `json:"transactions"`
	SourceTotal  string               `json:"sourceTotal"`
	Carry        string               `json:"carry"`
	Due          string               `json:"due"`
	Notice       string               `json:"notice,omitempty"`
}

func readNativeCostTransactions(ctx context.Context, tx pgx.Tx, week string) ([]PayCostTransaction, error) {
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
	report := drivers
	if kind == "investor" {
		report = investors
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
			return d.costTransactions, nil
		}
	}
	return nil, nil
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

func (r *DriverPayRepository) CostTransactions(ctx context.Context, kind, id string, week time.Time, cost string) (PayCostTransactions, error) {
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
	// Exact weeks are needed here, unlike the grouped report fast path.
	ctx = context.WithValue(ctx, payrollCostDetailKey{}, true)
	if kind == "driver" {
		records, e := readPayCostRecords(context.WithValue(ctx, payrollReadKey{}, (*payrollReadState)(nil)), tx)
		if e != nil {
			return result, e
		}
		for _, v := range records {
			if v.Driver != id || v.Week >= report.WeekStart {
				continue
			}
			base, amount := v.Fuel, v.FuelAmount
			if cost == "toll" {
				base, amount = v.Toll, v.TollAmount
			}
			history = append(history, costHistoryWeek{v.Week, base, amount, "driver", id})
		}
	} else {
		records, e := readPaySources(ctx, tx)
		if e != nil {
			return result, e
		}
		for _, v := range records {
			if v.Source != "investor_cost:"+id+":"+cost || v.Owner != card.InvestorID || v.Week >= report.WeekStart {
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
	carry, _ := chargeCents(result.Carry)
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
	rows, err := traceRoutedCostWeek(ctx, tx, report.WeekStart, kind, id)
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
	return result, tx.Commit(ctx)
}
