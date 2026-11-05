package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"mserp/internal/weighmytruck"
)

type WMTSettlementProvider interface {
	FetchSettlements(context.Context, string) ([]weighmytruck.Ticket, error)
	SettlementStart() string
}

type WMTTransaction struct {
	ID             string `json:"id"`
	Date           string `json:"date"`
	Time           string `json:"time"`
	Ticket         string `json:"ticket"`
	OriginalTicket string `json:"originalTicket"`
	Amount         string `json:"amount"`
	TruckUnit      string `json:"truckUnit"`
	DriverCode     string `json:"driverCode"`
	DriverID       string `json:"driverId"`
	DriverName     string `json:"driverName"`
	TruckID        string `json:"truckId"`
	Location       string `json:"location"`
	City           string `json:"city"`
	State          string `json:"state"`
	Payer          string `json:"payer"`
	InvestorName   string `json:"investorName"`
	Warning        string `json:"warning"`
	Version        int    `json:"version"`
}
type WMTTransactions struct {
	Page[WMTTransaction]
	TotalAmount string     `json:"totalAmount"`
	LastSync    *time.Time `json:"lastSync"`
}
type WMTBilling struct {
	DriverID  string `json:"driverId"`
	WeekStart string `json:"weekStart"`
	Payer     string `json:"payer"`
	Version   int    `json:"version"`
}

// Resolve every ticket from its dated truck ownership and team relationship.
// Legacy manual billing preferences are retained as history, never as routing
// inputs. Missing/conflicting evidence stays in review instead of billing a driver.
var wmtChargeFrom = ` FROM weighmytruck_transactions t
 LEFT JOIN drivers d ON d.id=t.driver_id` + wmtChargeJoins + wmtFrozenJoinSQL

var wmtChargeJoins = `
 LEFT JOIN LATERAL(SELECT payroll_driver_at(t.driver_id,t.transaction_date) driver_id) pd ON true
 LEFT JOIN LATERAL(SELECT (array_agg(DISTINCT h.owner_id))[1] owner_id FROM truck_ownership_history h
 WHERE h.truck_id=t.truck_id AND (NOT h.start_known OR (h.assigned_at AT TIME ZONE 'America/New_York')::date<=t.transaction_date)
 AND (h.unassigned_at IS NULL OR t.transaction_date<(h.unassigned_at AT TIME ZONE 'America/New_York')::date)
 HAVING count(DISTINCT h.owner_id)=1) ownership ON true
 LEFT JOIN investors i ON i.id=ownership.owner_id
 CROSS JOIN LATERAL(SELECT CASE WHEN t.driver_id IS NULL OR i.id IS NULL OR t.match_warning LIKE '%conflicts%' THEN 'review'
 WHEN i.is_company THEN 'company' WHEN i.driver_id=pd.driver_id THEN 'driver' ELSE 'investor' END payer) b
 LEFT JOIN LATERAL(SELECT * FROM truck_settlement_terms s WHERE s.truck_id=t.truck_id AND s.week_start<=date_trunc('week',t.transaction_date)::date ORDER BY s.week_start DESC LIMIT 1) s
 ON ` + truckTermCoversWeekSQL("date_trunc('week',t.transaction_date)::date") + `
`

const wmtStatementReportsSQL = `SELECT 'driver' kind,driver_id destination,week_start,report,finalized FROM payroll_settlements
 UNION ALL SELECT 'investor',truck_id,week_start,report,finalized FROM investor_pay_weeks`

// Frozen reports serve as collection receipts across all weeks. Reopening
// clears the finalized flag, which releases the ticket for review again.
const wmtFrozenJoinSQL = ` LEFT JOIN LATERAL (
 SELECT bool_or(f.kind<>b.payer OR f.destination IS DISTINCT FROM CASE WHEN b.payer='driver' THEN pd.driver_id ELSE t.truck_id END) misplaced,
 bool_or(NOT f.report @> jsonb_build_object('autoCharges',jsonb_build_array(jsonb_build_object('source','weighmytruck:'||t.id,'amount',(-t.amount)::text)))) amount_changed
 FROM (` + wmtStatementReportsSQL + `) f
 WHERE f.finalized AND f.report @> jsonb_build_object('autoCharges',jsonb_build_array(jsonb_build_object('source','weighmytruck:'||t.id)))
 ) frozen ON true `

// Carry preserves the ticket's original payer. The receiving investor week
// must have an active agreement for that same owner.
var wmtPayrollScaleSQL = `WITH collected AS MATERIALIZED (
 SELECT c->>'source' source, array_agg(DISTINCT f.kind||':'||f.destination) destinations
 FROM (` + wmtStatementReportsSQL + `) f CROSS JOIN LATERAL ` + wmtReportChargeRowsSQL + `
 WHERE f.finalized AND c->>'source' LIKE 'weighmytruck:%' GROUP BY 1
 ) SELECT w.week::text,t.id::text,t.ticket_number,t.transaction_date::text,t.amount::text,
 coalesce(pd.driver_id::text,''),coalesce(t.truck_id::text,''),b.payer,
 paid.source IS NOT NULL,t.transaction_date>=w.week AND t.transaction_date<w.week+7
 FROM unnest($1::date[]) w(week) CROSS JOIN weighmytruck_transactions t` + wmtChargeJoins + `
 LEFT JOIN collected paid ON paid.source='weighmytruck:'||t.id
 LEFT JOIN LATERAL (
 SELECT * FROM truck_settlement_terms s WHERE s.truck_id=t.truck_id AND s.week_start<=w.week ORDER BY s.week_start DESC LIMIT 1
 ) target ON true
 WHERE (b.payer='driver' AND paid.source IS NULL) OR
 (b.payer='investor' AND s.owner_id=i.id AND target.owner_id=i.id
  AND (paid.source IS NULL OR (paid.destinations=ARRAY['investor:'||t.truck_id] AND t.transaction_date>=w.week AND t.transaction_date<w.week+7))
  AND EXISTS(SELECT 1 FROM truck_settlement_terms s WHERE s.truck_id=target.truck_id AND s.week_start=target.week_start AND ` + truckTermCoversWeekSQL("w.week") + `))
 ORDER BY t.transaction_date,t.ticket_number,t.site_number`

func (r *WeighMyTruckRepository) Transactions(ctx context.Context, from, to, search string, page Pagination) (WMTTransactions, error) {
	tx, e := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return WMTTransactions{}, e
	}
	defer tx.Rollback(ctx)
	where := ` WHERE t.transaction_date>=$1::date AND t.transaction_date<=$2::date AND ($3='' OR concat_ws(' ',t.ticket_number,t.tractor_number,t.driver_code,d.full_name,t.location,t.city,t.state) ILIKE '%'||$3||'%')`
	var total int
	result := WMTTransactions{}
	e = tx.QueryRow(ctx, `SELECT count(*),coalesce(sum(t.amount),0)::text`+wmtChargeFrom+where, from, to, search).Scan(&total, &result.TotalAmount)
	if e != nil {
		return result, e
	}
	page = page.Normalize(total)
	rows, e := tx.Query(ctx, `SELECT t.id::text,t.transaction_date::text,t.approximate_time,t.ticket_number,t.original_ticket_number,t.amount::text,t.tractor_number,t.driver_code,
 coalesce(t.driver_id::text,''),coalesce(d.full_name,''),coalesce(t.truck_id::text,''),t.location,t.city,t.state,b.payer,CASE WHEN b.payer='investor' THEN coalesce(i.full_name,'') ELSE '' END,
 concat_ws(' ',nullif(t.match_warning,''),CASE WHEN b.payer='review' THEN 'Scale charge needs unambiguous dated driver and truck ownership.' END,CASE WHEN b.payer='investor' AND s.owner_id IS DISTINCT FROM i.id THEN 'Investor charge needs a matching dated truck agreement.' END,
 CASE WHEN frozen.misplaced OR frozen.amount_changed THEN 'Finalized statement differs. Reopen the statement that collected this ticket to review the correction.' END),t.version`+wmtChargeFrom+where+` ORDER BY t.transaction_date DESC,t.approximate_time DESC,t.ticket_number LIMIT $4 OFFSET $5`, from, to, search, page.PageSize, page.Offset())
	if e != nil {
		return result, e
	}
	items := []WMTTransaction{}
	for rows.Next() {
		var v WMTTransaction
		e = rows.Scan(&v.ID, &v.Date, &v.Time, &v.Ticket, &v.OriginalTicket, &v.Amount, &v.TruckUnit, &v.DriverCode, &v.DriverID, &v.DriverName, &v.TruckID, &v.Location, &v.City, &v.State, &v.Payer, &v.InvestorName, &v.Warning, &v.Version)
		if e != nil {
			rows.Close()
			return result, e
		}
		items = append(items, v)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return result, e
	}
	result.Page = NewPage(items, total, page)
	e = tx.QueryRow(ctx, `SELECT max(synced_at) FROM weighmytruck_sync_days`).Scan(&result.LastSync)
	if e != nil {
		return result, e
	}
	return result, tx.Commit(ctx)
}

// Keep the old endpoint explicit for clients that still have the payer editor open.
func (r *WeighMyTruckRepository) SaveBilling(ctx context.Context, in WMTBilling, actor string) error {
	return &WMTValidationError{"Scale charges are automatic based on dated truck ownership. Refresh the page."}
}

func (r *WeighMyTruckRepository) SyncSettlements(ctx context.Context) (int, error) {
	p, ok := r.provider.(WMTSettlementProvider)
	if !ok || !r.provider.Configured() {
		return 0, &WMTValidationError{"WeighMyTruck settlements are not configured."}
	}
	conn, e := r.pool.Acquire(ctx)
	if e != nil {
		return 0, e
	}
	defer conn.Release()
	var locked bool
	if e = conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(730075)`).Scan(&locked); e != nil {
		return 0, e
	}
	if !locked {
		return 0, &WMTValidationError{"A settlement sync is already running."}
	}
	defer func() {
		c, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if _, e := conn.Exec(c, `SELECT pg_advisory_unlock(730075)`); e != nil {
			conn.Conn().Close(c)
		}
	}()
	start, e := time.Parse(time.DateOnly, p.SettlementStart())
	if e != nil {
		return 0, &WMTValidationError{"Configure WEIGHMYTRUCK_SYNC_START_DATE on the server."}
	}
	loc, _ := time.LoadLocation("America/New_York")
	now := time.Now().In(loc)
	today, _ := time.Parse(time.DateOnly, now.Format(time.DateOnly))
	earliest := today.AddDate(0, 0, -29)
	if start.Before(earliest) {
		start = earliest
	}
	saved := 0
	for day := start; !day.After(today); day = day.AddDate(0, 0, 1) {
		tickets, err := p.FetchSettlements(ctx, day.Format(time.DateOnly))
		if err != nil {
			return saved, err
		}
		if err = r.StoreSettlements(ctx, day.Format(time.DateOnly), tickets); err != nil {
			return saved, err
		}
		saved += len(tickets)
	}
	return saved, nil
}

func (r *WeighMyTruckRepository) StoreSettlements(ctx context.Context, day string, tickets []weighmytruck.Ticket) error {
	tx, e := beginPayrollSourceMutation(ctx, r.pool)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock_shared(736281940); SELECT pg_advisory_xact_lock(730074)`); e != nil {
		return e
	}
	for _, t := range tickets {
		if t.Date != day {
			return fmt.Errorf("WeighMyTruck returned a ticket outside requested date %s", day)
		}
		var dateConflict bool
		if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM weighmytruck_transactions WHERE site_number=$1 AND ticket_number=$2 AND transaction_date<>$3::date)`, t.Site, t.Number, day).Scan(&dateConflict); e != nil {
			return e
		}
		if dateConflict {
			return fmt.Errorf("WeighMyTruck ticket date changed; source reconciliation required for %s", day)
		}
		raw, _ := json.Marshal(t.Raw)
		// Import is idempotent; changed vendor values are retained in the source row.
		// Finalized payroll uses its existing immutable report, never this live row.
		_, e = tx.Exec(ctx, `INSERT INTO weighmytruck_transactions(site_number,ticket_number,original_ticket_number,transaction_date,approximate_time,amount,tractor_number,trailer_number,driver_code,location,city,state,raw_payload)
 VALUES($1,$2,$3,$4::date,$5,$6::numeric,$7,$8,$9,$10,$11,$12,$13)
 ON CONFLICT(site_number,ticket_number) DO UPDATE SET source_conflict=weighmytruck_transactions.source_conflict OR weighmytruck_transactions.tractor_number<>excluded.tractor_number OR weighmytruck_transactions.driver_code<>excluded.driver_code,tractor_number=excluded.tractor_number,driver_code=excluded.driver_code,amount=excluded.amount,raw_payload=excluded.raw_payload,synced_at=now(),version=weighmytruck_transactions.version+CASE WHEN (weighmytruck_transactions.amount,weighmytruck_transactions.tractor_number,weighmytruck_transactions.driver_code) IS DISTINCT FROM (excluded.amount,excluded.tractor_number,excluded.driver_code) THEN 1 ELSE 0 END`, t.Site, t.Number, t.OriginalNumber, t.Date, t.Time, t.Fee, t.Tractor, t.Trailer, t.DriverCode, t.Location, t.City, t.State, raw)
		if e != nil {
			return e
		}
	}
	if e = reconcileWMT(ctx, tx); e != nil {
		return e
	}
	_, e = tx.Exec(ctx, `INSERT INTO weighmytruck_sync_days(sync_date,ticket_count) VALUES($1::date,$2) ON CONFLICT(sync_date) DO UPDATE SET synced_at=now(),ticket_count=excluded.ticket_count`, day, len(tickets))
	if e != nil {
		return e
	}
	return tx.Commit(ctx)
}

func reconcileWMT(ctx context.Context, tx pgx.Tx) error {
	// Stable linked IDs survive renames and assignment changes. No fuzzy matching.
	_, e := tx.Exec(ctx, `UPDATE weighmytruck_transactions t SET truck_id=a.id FROM (SELECT unit_key,(array_agg(DISTINCT truck_id))[1] id FROM truck_unit_aliases GROUP BY unit_key HAVING count(DISTINCT truck_id)=1) a WHERE t.truck_id IS NULL AND NOT t.manually_linked AND upper(btrim(t.tractor_number))=a.unit_key`)
	if e != nil {
		return e
	}
	_, e = tx.Exec(ctx, `UPDATE weighmytruck_transactions t SET driver_id=m.id FROM (SELECT lower(btrim(driver_code)) code,(array_agg(DISTINCT driver_id))[1] id FROM weighmytruck_memberships WHERE driver_id IS NOT NULL AND btrim(driver_code)<>'' GROUP BY lower(btrim(driver_code)) HAVING count(DISTINCT driver_id)=1) m WHERE t.driver_id IS NULL AND NOT t.manually_linked AND lower(btrim(t.driver_code))=m.code`)
	if e != nil {
		return e
	}
	_, e = tx.Exec(ctx, `UPDATE weighmytruck_transactions t SET driver_id=a.id FROM (SELECT t.id,(array_agg(DISTINCT a.driver_id))[1] id_driver FROM weighmytruck_transactions t JOIN truck_driver_assignments a ON a.truck_id=t.truck_id AND (a.assigned_at AT TIME ZONE 'America/New_York')::date<=t.transaction_date AND (a.unassigned_at IS NULL OR t.transaction_date<(a.unassigned_at AT TIME ZONE 'America/New_York')::date) WHERE t.driver_id IS NULL AND btrim(t.driver_code)='' GROUP BY t.id HAVING count(DISTINCT a.driver_id)=1) x CROSS JOIN LATERAL(SELECT x.id_driver id) a WHERE t.id=x.id AND NOT t.manually_linked`)
	if e != nil {
		return e
	}
	_, e = tx.Exec(ctx, `UPDATE weighmytruck_transactions t SET match_warning=concat_ws(' ',CASE WHEN source_conflict THEN 'Source identity conflicts with saved ticket; review link.' END,CASE WHEN driver_id IS NULL THEN 'Driver needs linking.' END,CASE WHEN truck_id IS NULL THEN 'Truck needs linking.' END,CASE WHEN driver_id IS NOT NULL AND truck_id IS NOT NULL AND NOT manually_linked AND EXISTS(SELECT 1 FROM truck_driver_assignments a WHERE a.truck_id=t.truck_id AND a.driver_id<>payroll_driver_at(t.driver_id,t.transaction_date) AND (a.assigned_at AT TIME ZONE 'America/New_York')::date<=t.transaction_date AND (a.unassigned_at IS NULL OR t.transaction_date<(a.unassigned_at AT TIME ZONE 'America/New_York')::date)) THEN 'Driver code conflicts with truck assignment; review link.' END)`)
	return e
}

func wmtPayrollCharges(ctx context.Context, tx pgx.Tx, week string) (map[string][]PayAutoCharge, map[string][]PayAutoCharge, error) {
	cache := payrollScaleCache(ctx, tx)
	if value, ok := cache[week]; ok {
		copy := copyPayrollScaleWeek(value)
		return copy[0], copy[1], nil
	}
	drivers, trucks, err := wmtPayrollChargesUncached(ctx, tx, week)
	if err == nil && cache != nil {
		cache[week] = copyPayrollScaleWeek(payrollScaleWeek{drivers, trucks})
	}
	return drivers, trucks, err
}

func wmtPayrollChargesUncached(ctx context.Context, tx pgx.Tx, week string) (map[string][]PayAutoCharge, map[string][]PayAutoCharge, error) {
	values, err := readPayrollScaleWeeks(ctx, tx, []string{week})
	if err != nil {
		return nil, nil, err
	}
	value := values[week]
	return value[0], value[1], nil
}

func (r *WeighMyTruckRepository) LinkTransaction(ctx context.Context, id, driver, truck string, version int, actor string) error {
	if strings.TrimSpace(driver) == "" || strings.TrimSpace(truck) == "" {
		return &WMTValidationError{"Select both a driver and truck."}
	}
	tx, e := beginPayrollMutation(ctx, r.pool)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock_shared(736281940); SELECT pg_advisory_xact_lock(730074)`); e != nil {
		return e
	}
	var closed bool
	e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM payroll_settlements p WHERE p.week_start=date_trunc('week',t.transaction_date)::date AND p.driver_id IN(t.driver_id,$2::uuid,payroll_driver_at(t.driver_id,t.transaction_date),payroll_driver_at($2::uuid,t.transaction_date)) AND p.finalized) OR EXISTS(SELECT 1 FROM investor_pay_weeks p WHERE p.week_start=date_trunc('week',t.transaction_date)::date AND p.truck_id IN(t.truck_id,$3::uuid) AND p.finalized)
 OR EXISTS(SELECT 1 FROM (`+wmtStatementReportsSQL+`) f WHERE f.finalized AND f.report @> jsonb_build_object('autoCharges',jsonb_build_array(jsonb_build_object('source','weighmytruck:'||t.id)))
 FROM weighmytruck_transactions t WHERE t.id=$1`, id, driver, truck).Scan(&closed)
	if e != nil {
		return e
	}
	if closed {
		return &WMTValidationError{"Reopen affected statements before correcting this transaction."}
	}
	tag, e := tx.Exec(ctx, `UPDATE weighmytruck_transactions SET driver_id=$2,truck_id=$3,manually_linked=true,source_conflict=false,match_warning='',version=version+1 WHERE id=$1 AND version=$4`, id, driver, truck, version)
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return ErrWMTConflict
	}
	detail, _ := json.Marshal(map[string]any{"transactionId": id, "driverId": driver, "truckId": truck, "version": version})
	_, e = tx.Exec(ctx, `INSERT INTO weighmytruck_audit(actor_id,action,detail) VALUES($1,'transaction_linked',$2)`, actor, detail)
	if e != nil {
		return e
	}
	return commitPayrollMutation(ctx, tx)
}
