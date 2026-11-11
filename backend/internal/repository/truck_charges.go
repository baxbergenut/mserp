package repository

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"mserp/internal/db"
	"regexp"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type TruckTerm struct {
	TruckID      string `json:"truckId"`
	WeekStart    string `json:"weekStart"`
	OwnerID      string `json:"ownerId"`
	SharePercent string `json:"sharePercent"`
	Version      int    `json:"version"`
	EndsBefore   string `json:"endsBefore,omitempty"`
}
type TruckChargePhase struct {
	TruckID     string `json:"truckId"`
	TypeID      string `json:"typeId"`
	WeekStart   string `json:"weekStart"`
	Amount      string `json:"amount"`
	Included    bool   `json:"included"`
	Version     int    `json:"version"`
	TypeVersion int    `json:"typeVersion"`
	// Explicitly move an existing recurring assignment, preserving past weeks.
	MoveScheduleID      string `json:"moveScheduleId,omitempty"`
	MoveScheduleVersion int    `json:"moveScheduleVersion,omitempty"`
}
type TruckChargeData struct {
	EligibleTruckIDs []string           `json:"eligibleTruckIds"`
	Owners           []TruckChargeOwner `json:"owners,omitempty"`
	Terms            []TruckTerm        `json:"terms"`
	Phases           []TruckChargePhase `json:"phases"`
	LockedCharges    []TruckChargeLock  `json:"lockedCharges"`
}

type TruckChargeLock struct {
	TruckID string `json:"truckId"`
	TypeID  string `json:"typeId"`
}

// Ownership comes from the fleet history, independently of settlement agreements.
type TruckChargeOwner struct {
	TruckID   string `json:"truckId"`
	OwnerID   string `json:"ownerId"`
	OwnerName string `json:"ownerName"`
}

type TruckChargeBulkResult struct {
	TruckID   string `json:"truckId"`
	TypeID    string `json:"typeId"`
	WeekStart string `json:"weekStart"`
	Error     string `json:"error,omitempty"`
}

const MaxTruckChargeBulkSize = 1000

// The first ownership period belonging to someone else ends the agreement.
// Ignore periods that closed before the agreement's first Sunday, so a new
// owner can set terms from the Monday of their acquisition week.
const truckTermEndSQL = `SELECT min(CASE WHEN h.start_known
 THEN greatest(s.week_start,date_trunc('week',h.assigned_at AT TIME ZONE 'America/New_York')::date)
 ELSE s.week_start END)
 FROM truck_ownership_history h WHERE h.truck_id=s.truck_id AND h.owner_id<>s.owner_id
 AND (NOT h.start_known OR h.unassigned_at IS NULL OR h.unassigned_at>h.assigned_at)
 AND (h.unassigned_at IS NULL OR h.unassigned_at>=((s.week_start+7)::timestamp AT TIME ZONE 'America/New_York'))`

// s is the latest agreement on or before week. An expired agreement never
// revives an older one, including when the truck returns to a previous owner.
func truckTermCoversWeekSQL(week string) string {
	return `coalesce((` + truckTermEndSQL + `)>` + week + `,true)`
}

const latestTruckTermForWeekSQL = `SELECT s.* FROM truck_settlement_terms s
 WHERE s.truck_id=t.id AND s.week_start<=$1::date ORDER BY s.week_start DESC LIMIT 1`

func truckTermData(ctx context.Context, q chargeQuery) ([]TruckTerm, error) {
	terms := []TruckTerm{}
	rows, err := q.Query(ctx, `SELECT s.truck_id::text,s.week_start::text,s.owner_id::text,s.share_percent::text,s.version,
 coalesce((`+truckTermEndSQL+`)::text,'') FROM truck_settlement_terms s
 WHERE ($1::uuid[] IS NULL OR s.truck_id=ANY($1::uuid[])) ORDER BY s.week_start,s.truck_id`, payrollTruckIDs(ctx))
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var v TruckTerm
		if err = rows.Scan(&v.TruckID, &v.WeekStart, &v.OwnerID, &v.SharePercent, &v.Version, &v.EndsBefore); err != nil {
			rows.Close()
			return nil, err
		}
		terms = append(terms, v)
	}
	err = rows.Err()
	rows.Close()
	return terms, err
}

func truckChargeData(ctx context.Context, q chargeQuery) (TruckChargeData, error) {
	terms, err := truckTermData(ctx, q)
	d := TruckChargeData{Terms: terms, Phases: []TruckChargePhase{}}
	if err != nil {
		return d, err
	}
	rows, err := q.Query(ctx, `SELECT p.truck_id::text,p.type_id::text,p.week_start::text,p.amount::text,p.included,p.version,t.version FROM truck_charge_phases p JOIN driver_charge_types t ON t.id=p.type_id WHERE ($1::uuid[] IS NULL OR p.truck_id=ANY($1::uuid[])) ORDER BY p.week_start,p.truck_id,p.type_id`, payrollTruckIDs(ctx))
	if err != nil {
		return d, err
	}
	defer rows.Close()
	for rows.Next() {
		var v TruckChargePhase
		if err = rows.Scan(&v.TruckID, &v.TypeID, &v.WeekStart, &v.Amount, &v.Included, &v.Version, &v.TypeVersion); err != nil {
			return d, err
		}
		d.Phases = append(d.Phases, v)
	}
	return d, rows.Err()
}
func (r *DriverChargeRepository) TruckCharges(ctx context.Context) (TruckChargeData, error) {
	return r.TruckChargesForWeek(ctx, ChargeCurrentWeek())
}

func (r *DriverChargeRepository) TruckChargesForWeek(ctx context.Context, week string) (TruckChargeData, error) {
	if week == "" {
		week = ChargeCurrentWeek()
	}
	if _, err := chargeWeek(week); err != nil {
		return TruckChargeData{}, err
	}
	d := TruckChargeData{Terms: []TruckTerm{}, Phases: []TruckChargePhase{},
		EligibleTruckIDs: []string{}, Owners: []TruckChargeOwner{}, LockedCharges: []TruckChargeLock{}}
	rows, err := r.pool.Query(ctx, `SELECT t.id::text,i.id::text,coalesce(driver.full_name,i.full_name)
 FROM trucks t `+truckOwnershipForWeekSQL+`
 LEFT JOIN drivers driver ON driver.id=i.driver_id WHERE t.active AND NOT i.is_company AND NOT (`+terminatedOwnerTruckChargesSQL+`) ORDER BY t.id`, week)
	if err != nil {
		return d, err
	}
	defer rows.Close()
	for rows.Next() {
		var owner TruckChargeOwner
		if err = rows.Scan(&owner.TruckID, &owner.OwnerID, &owner.OwnerName); err != nil {
			return d, err
		}
		d.EligibleTruckIDs = append(d.EligibleTruckIDs, owner.TruckID)
		d.Owners = append(d.Owners, owner)
	}
	if err = rows.Err(); err != nil {
		return d, err
	}
	rows.Close()

	// Keep excluded phases and expired terms as the latest candidates.
	rows, err = r.pool.Query(ctx, `SELECT s.truck_id::text,s.week_start::text,s.owner_id::text,s.share_percent::text,s.version,
 coalesce((`+truckTermEndSQL+`)::text,'')
 FROM unnest($2::uuid[]) ids(id) CROSS JOIN LATERAL (
 SELECT * FROM truck_settlement_terms WHERE truck_id=ids.id AND week_start<=$1::date
 ORDER BY week_start DESC LIMIT 1) s`, week, d.EligibleTruckIDs)
	if err != nil {
		return d, err
	}
	defer rows.Close()
	for rows.Next() {
		var v TruckTerm
		if err = rows.Scan(&v.TruckID, &v.WeekStart, &v.OwnerID, &v.SharePercent, &v.Version, &v.EndsBefore); err != nil {
			return d, err
		}
		d.Terms = append(d.Terms, v)
	}
	if err = rows.Err(); err != nil {
		return d, err
	}
	rows.Close()
	rows, err = r.pool.Query(ctx, `SELECT p.truck_id::text,p.type_id::text,p.week_start::text,p.amount::text,p.included,p.version,t.version
 FROM unnest($2::uuid[]) ids(id) CROSS JOIN driver_charge_types t CROSS JOIN LATERAL (
 SELECT * FROM truck_charge_phases WHERE truck_id=ids.id AND type_id=t.id AND week_start<=$1::date
 ORDER BY week_start DESC LIMIT 1) p`, week, d.EligibleTruckIDs)
	if err != nil {
		return d, err
	}
	defer rows.Close()
	for rows.Next() {
		var v TruckChargePhase
		if err = rows.Scan(&v.TruckID, &v.TypeID, &v.WeekStart, &v.Amount, &v.Included, &v.Version, &v.TypeVersion); err != nil {
			return d, err
		}
		d.Phases = append(d.Phases, v)
	}
	if err = rows.Err(); err != nil {
		return d, err
	}
	rows.Close()
	rows, err = r.pool.Query(ctx, truckClosedPeriodsSQL+` SELECT truck_id::text,type_id::text FROM closed`, week, d.EligibleTruckIDs, "charge", nil)
	if err != nil {
		return d, err
	}
	defer rows.Close()
	for rows.Next() {
		var locked TruckChargeLock
		if err = rows.Scan(&locked.TruckID, &locked.TypeID); err != nil {
			return d, err
		}
		d.LockedCharges = append(d.LockedCharges, locked)
	}
	return d, rows.Err()
}

// All non-company trucks have truck-based recurring fees.
const investorTruckEligibility = `NOT i.is_company`

// Employment stops fees only for the departing owner's own operating truck (or
// an inactive investor's fleet). Continuing investor fleets and employed crews
// remain independent. Compare with Monday, so the termination week is retained.
// Use dated ownership/assignments, not the legacy owner-operator classification.
const terminatedOwnerTruckChargesSQL = `EXISTS (
 SELECT 1 FROM drivers departing WHERE departing.id=i.driver_id
 AND departing.status='terminated' AND departing.termination_date < $1::date
 AND (NOT i.active OR EXISTS (
   SELECT 1 FROM truck_driver_assignments own WHERE own.truck_id=t.id AND own.driver_id=departing.id
   AND (own.assigned_at AT TIME ZONE 'America/New_York')::date<=departing.termination_date
   AND (own.unassigned_at IS NULL OR (own.unassigned_at AT TIME ZONE 'America/New_York')::date>departing.termination_date)
 ) OR EXISTS (
   SELECT 1 FROM driver_termination_truck_actions receipt
   WHERE receipt.truck_id=t.id AND receipt.termination_id=departing.termination_id
   AND receipt.outcome IN ('inactivated','already_inactive')
 ))
 AND NOT EXISTS (
   SELECT 1 FROM truck_driver_assignments crew JOIN drivers person ON person.id=crew.driver_id
   WHERE crew.truck_id=t.id AND person.id<>departing.id
   AND (person.active OR person.termination_date >= $1::date)
   AND crew.assigned_at < (($1::date+7)::timestamp AT TIME ZONE 'America/New_York')
   AND (crew.unassigned_at IS NULL OR crew.unassigned_at > ($1::date::timestamp AT TIME ZONE 'America/New_York'))
 )
 AND NOT EXISTS (
   SELECT 1 FROM truck_driver_assignments crew JOIN driver_teams team ON team.main_driver_id=crew.driver_id
   JOIN drivers person ON person.id=team.team_driver_id
   WHERE crew.truck_id=t.id AND person.id<>departing.id
   AND (person.active OR person.termination_date >= $1::date)
   AND team.start_week<=$1::date AND (team.end_week IS NULL OR team.end_week>$1::date)
   AND crew.assigned_at < (($1::date+7)::timestamp AT TIME ZONE 'America/New_York')
   AND (crew.unassigned_at IS NULL OR crew.unassigned_at > ($1::date::timestamp AT TIME ZONE 'America/New_York'))
 ))`

// Filter only recurring phases: settlement terms, other costs, and frozen
// reports keep their existing accounting and ownership rules. Build a new slice
// because payroll inputs may be reused for a different historical week.
func truckChargesForEmploymentWeek(ctx context.Context, q chargeQuery, data TruckChargeData, week string) (TruckChargeData, error) {
	if len(data.Phases) == 0 {
		return data, nil
	}
	vacation, err := readVacationWeek(ctx, q, week)
	if err != nil {
		return data, err
	}
	stopped, err := readPreparedStoppedTruckCharges(ctx, q, week)
	if err != nil {
		return data, err
	}
	phases := make([]TruckChargePhase, 0, len(data.Phases))
	for _, phase := range data.Phases {
		if !stopped[phase.TruckID] && !vacation.trucks[phase.TruckID] {
			phases = append(phases, phase)
		}
	}
	data.Phases = phases
	return data, nil
}

func readStoppedTruckChargesUncached(ctx context.Context, q chargeQuery, week string) (map[string]bool, error) {
	rows, err := q.Query(ctx, `SELECT t.id::text FROM trucks t `+truckOwnershipForWeekSQL+`
 WHERE `+terminatedOwnerTruckChargesSQL, week)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	stopped := map[string]bool{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		stopped[id] = true
	}
	return stopped, rows.Err()
}

// Both the directory and writes use ownership at the selected week's end.
// A later transfer to/from the company must not change historical eligibility.
const truckOwnershipForWeekSQL = `JOIN LATERAL (
 SELECT h.owner_id FROM truck_ownership_history h WHERE h.truck_id=t.id
 AND (NOT h.start_known OR h.unassigned_at IS NULL OR h.unassigned_at>h.assigned_at)
 AND (NOT h.start_known OR h.assigned_at < (($1::date+7)::timestamp AT TIME ZONE 'America/New_York'))
 AND (h.unassigned_at IS NULL OR h.unassigned_at >= (($1::date+7)::timestamp AT TIME ZONE 'America/New_York'))
 ORDER BY h.assigned_at DESC,h.id LIMIT 1
 ) ownership ON true JOIN investors i ON i.id=ownership.owner_id`

func requireInvestorTruck(ctx context.Context, tx pgx.Tx, truck, week string) error {
	var eligible bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM trucks t `+truckOwnershipForWeekSQL+` WHERE t.id=$2 AND `+investorTruckEligibility+`)`, week, truck).Scan(&eligible); err != nil {
		return err
	}
	if !eligible {
		return chargeInvalid("Recurring charges apply only to non-company trucks")
	}
	return nil
}

func truckAudit(ctx context.Context, tx pgx.Tx, truck, week, action, actor string, detail any) error {
	b, err := json.Marshal(detail)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO truck_settlement_events(truck_id,week_start,action,actor_id,details) VALUES($1,$2::date,$3,nullif($4,'')::uuid,$5)`, truck, week, action, actor, b)
	return err
}

// Serialize against both payroll finalization and driver charge edits. Financial
// changes never silently invalidate an already finalized settlement.
func lockTruckAccounting(ctx context.Context, tx pgx.Tx) error {
	if payrollConcurrent(tx) {
		return nil
	}
	defer db.Phase(ctx, "payroll_accounting_lock")()
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(736281940)`)
	return err
}

// One lock rule for the grid and the fenced write. The driver check is
// intentionally broader than the investor interval and independent of type.
const truckClosedPeriodsSQL = `WITH targets AS MATERIALIZED (
 SELECT id AS truck_id FROM unnest($2::uuid[]) u(id)
), driver_closed AS MATERIALIZED (
 SELECT t.truck_id FROM targets x JOIN truck_settlement_terms t USING(truck_id)
 JOIN investors i ON i.id=t.owner_id JOIN payroll_settlements p ON p.driver_id=i.driver_id
 WHERE p.finalized AND p.week_start >= $1::date
 UNION
 SELECT h.truck_id FROM targets x JOIN truck_ownership_history h USING(truck_id)
 JOIN investors i ON i.id=h.owner_id JOIN payroll_settlements p ON p.driver_id=i.driver_id
 WHERE p.finalized AND p.week_start >= $1::date
), investor_first AS MATERIALIZED (
 SELECT w.truck_id,min(w.week_start) AS week_start FROM targets x JOIN investor_pay_weeks w USING(truck_id)
 WHERE w.finalized AND w.week_start >= $1::date GROUP BY w.truck_id
), types AS (
 SELECT id AS type_id FROM driver_charge_types WHERE $3::text='charge' AND $4::uuid IS NULL
 UNION ALL SELECT $4::uuid WHERE $4::uuid IS NOT NULL OR $3::text='terms'
), closed AS (
 SELECT x.truck_id,c.type_id FROM targets x CROSS JOIN types c
 LEFT JOIN driver_closed d USING(truck_id) LEFT JOIN investor_first w USING(truck_id)
 WHERE CASE WHEN d.truck_id IS NOT NULL THEN true WHEN w.week_start IS NULL THEN false
 WHEN $3::text='terms' THEN w.week_start < coalesce((
 SELECT min(s.week_start) FROM truck_settlement_terms s WHERE s.truck_id=x.truck_id AND s.week_start>$1::date),'2101-01-01'::date)
 ELSE w.week_start < coalesce((
 SELECT min(p.week_start) FROM truck_charge_phases p WHERE p.truck_id=x.truck_id AND p.type_id=c.type_id AND p.week_start>$1::date),'2101-01-01'::date)
 END
)`

func protectTruckPeriod(ctx context.Context, tx pgx.Tx, truck, week, kind, typeID string) error {
	var closed bool
	var typ *string
	if typeID != "" {
		typ = &typeID
	}
	err := tx.QueryRow(ctx, truckClosedPeriodsSQL+` SELECT EXISTS(SELECT 1 FROM closed)`, week, []string{truck}, kind, typ).Scan(&closed)
	if err != nil {
		return err
	}
	if closed {
		return chargeInvalid("Reopen affected finalized settlements before changing truck terms or charges")
	}
	return nil
}
func (r *DriverChargeRepository) SaveTruckTerm(ctx context.Context, v TruckTerm, actor string) error {
	if _, err := chargeWeek(v.WeekStart); err != nil {
		return err
	}
	rate, ok := new(big.Rat).SetString(v.SharePercent)
	if !regexp.MustCompile(`^[0-9]{1,3}(\.[0-9]{1,4})?$`).MatchString(v.SharePercent) || !ok || rate.Sign() <= 0 || rate.Cmp(big.NewRat(100, 1)) > 0 || !new(big.Rat).Mul(rate, big.NewRat(10000, 1)).IsInt() {
		return chargeInvalid("Investor share must be greater than zero and at most 100%%, with at most four decimals")
	}
	tx, err := beginPayrollMutation(ctx, r.pool)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = lockTruckAccounting(ctx, tx); err != nil {
		return err
	}
	if err = protectTruckPeriod(ctx, tx, v.TruckID, v.WeekStart, "terms", ""); err != nil {
		return err
	}
	if err = requireInvestorTruck(ctx, tx, v.TruckID, v.WeekStart); err != nil {
		return err
	}
	var previousOwner bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM investor_pay_weeks w WHERE w.truck_id=$1 AND w.owner_id<>$3 AND w.week_start >= $2::date AND w.week_start < coalesce((SELECT min(week_start) FROM truck_settlement_terms WHERE truck_id=$1 AND week_start>$2::date),'2101-01-01'::date))`, v.TruckID, v.WeekStart, v.OwnerID).Scan(&previousOwner); err != nil {
		return err
	}
	if previousOwner {
		return chargeInvalid("Saved investor statements retain their owner; use a later effective week")
	}
	var validOwner bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM trucks t `+truckOwnershipForWeekSQL+`
 WHERE t.id=$2 AND i.id=$3 AND i.active AND NOT i.is_company)`, v.WeekStart, v.TruckID, v.OwnerID).Scan(&validOwner); err != nil {
		return err
	}
	if !validOwner {
		return chargeInvalid("Choose the truck's active owner for the effective week")
	}
	var version int
	if v.Version == 0 {
		err = tx.QueryRow(ctx, `INSERT INTO truck_settlement_terms(truck_id,week_start,owner_id,share_percent,updated_by) VALUES($1,$2::date,$3,$4::numeric,nullif($5,'')::uuid) ON CONFLICT DO NOTHING RETURNING version`, v.TruckID, v.WeekStart, v.OwnerID, v.SharePercent, actor).Scan(&version)
	} else {
		err = tx.QueryRow(ctx, `UPDATE truck_settlement_terms SET owner_id=$3,share_percent=$4::numeric,version=version+1,updated_by=nullif($5,'')::uuid,updated_at=now() WHERE truck_id=$1 AND week_start=$2::date AND version=$6 RETURNING version`, v.TruckID, v.WeekStart, v.OwnerID, v.SharePercent, actor, v.Version).Scan(&version)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrChargeConflict
	}
	if err != nil {
		return err
	}
	if err = truckAudit(ctx, tx, v.TruckID, v.WeekStart, "terms", actor, v); err != nil {
		return err
	}
	return commitPayrollMutation(ctx, tx)
}
func lockTruckChargeWrite(ctx context.Context, tx pgx.Tx) error {
	if _, locked := tx.(*payrollMutationTransaction); locked {
		// Prepared source mutations hold the exclusive accounting gate from
		// before the pre-write snapshot.
		return nil
	}
	return lockTruckAccounting(ctx, tx)
}

func (r *DriverChargeRepository) SaveTruckCharge(ctx context.Context, v TruckChargePhase) error {
	tx, err := beginPayrollMutation(ctx, r.pool)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = lockTruckChargeWrite(ctx, tx); err != nil {
		return err
	}
	if err = saveTruckCharge(ctx, tx, v); err != nil {
		return err
	}
	return commitPayrollMutation(ctx, tx)
}

// One request, financial lock and commit for the whole selection. Savepoints
// isolate expected row failures so a stale or finalized truck cannot stop later
// trucks. Unexpected database failures roll back the batch rather than claiming
// that any uncommitted rows succeeded.
func (r *DriverChargeRepository) SaveTruckCharges(ctx context.Context, updates []TruckChargePhase) ([]TruckChargeBulkResult, error) {
	if len(updates) == 0 || len(updates) > MaxTruckChargeBulkSize {
		return nil, chargeInvalid("Choose between 1 and %d truck charges", MaxTruckChargeBulkSize)
	}
	seen := make(map[string]bool, len(updates))
	for _, v := range updates {
		key := v.TruckID + ":" + v.TypeID + ":" + v.WeekStart
		if seen[key] {
			return nil, chargeInvalid("Choose each truck charge only once")
		}
		seen[key] = true
	}
	tx, err := beginPayrollMutation(ctx, r.pool)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if err = lockTruckChargeWrite(ctx, tx); err != nil {
		return nil, err
	}
	results := make([]TruckChargeBulkResult, 0, len(updates))
	for _, v := range updates {
		row, err := tx.Begin(ctx) // pgx nested transactions are savepoints.
		if err != nil {
			return nil, err
		}
		result := TruckChargeBulkResult{TruckID: v.TruckID, TypeID: v.TypeID, WeekStart: v.WeekStart}
		if err = saveTruckCharge(ctx, row, v); err != nil {
			if rollbackErr := row.Rollback(ctx); rollbackErr != nil {
				return nil, rollbackErr
			}
			var invalid *ChargeValidationError
			var pg *pgconn.PgError
			switch {
			case errors.As(err, &invalid), errors.Is(err, ErrChargeConflict):
				result.Error = err.Error()
			case errors.Is(err, pgx.ErrNoRows):
				result.Error = "Record not found"
			case errors.As(err, &pg) && (pg.Code == "23514" || pg.Code == "23503" || pg.Code == "23505"):
				result.Error = "A related settlement or charge changed; reload and review"
			default:
				return nil, err
			}
		} else if err = row.Commit(ctx); err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	if err = commitPayrollMutation(ctx, tx); err != nil {
		return nil, err
	}
	return results, nil
}

func saveTruckCharge(ctx context.Context, tx pgx.Tx, v TruckChargePhase) error {
	defer db.Phase(ctx, "truck_charge_write")()
	if _, err := chargeWeek(v.WeekStart); err != nil {
		return err
	}
	amount, err := chargeCents(v.Amount)
	if err != nil || amount < 0 {
		return chargeInvalid("Choose a nonnegative charge amount")
	}
	if err = protectTruckPeriod(ctx, tx, v.TruckID, v.WeekStart, "charge", v.TypeID); err != nil {
		return err
	}
	var eligible, active, stopped bool
	if err = tx.QueryRow(ctx, `SELECT coalesce(e.eligible,false),coalesce(e.active,false),coalesce(e.stopped,false)
 FROM (SELECT 1) request LEFT JOIN LATERAL (
 SELECT NOT i.is_company AS eligible,t.active,(`+terminatedOwnerTruckChargesSQL+`) AS stopped
 FROM trucks t `+truckOwnershipForWeekSQL+` WHERE t.id=$2) e ON true`, v.WeekStart, v.TruckID).Scan(&eligible, &active, &stopped); err != nil {
		return err
	}
	if !eligible {
		return chargeInvalid("Recurring charges apply only to non-company trucks")
	}
	if !active {
		return chargeInvalid("Recurring charges apply only to active trucks")
	}
	if stopped {
		return chargeInvalid("Recurring charges stop the week after the owner-driver's termination")
	}
	var options []string
	var version, exactVersion int
	var archived, unchanged bool
	if err = tx.QueryRow(ctx, `SELECT t.amounts::text[],t.version,t.archived,
 coalesce(CASE WHEN p.week_start=$3::date THEN p.version END,0),
 coalesce(p.amount=$4::numeric AND p.included=$5,false)
 FROM driver_charge_types t LEFT JOIN LATERAL (
 SELECT week_start,version,amount,included FROM truck_charge_phases
 WHERE truck_id=$2 AND type_id=t.id AND week_start<=$3::date
 ORDER BY week_start DESC LIMIT 1) p ON true WHERE t.id=$1 FOR SHARE OF t`,
		v.TypeID, v.TruckID, v.WeekStart, v.Amount, v.Included).Scan(&options, &version, &archived, &exactVersion, &unchanged); err != nil {
		return err
	}
	if version != v.TypeVersion || exactVersion != v.Version {
		return ErrChargeConflict
	}
	if v.Included && (archived || !allowedChargeAmount(options, v.Amount)) {
		return chargeInvalid("Choose an available amount from an active charge type")
	}
	if v.MoveScheduleID != "" {
		return chargeInvalid("Driver recurring charges have been removed; select the truck fee directly")
	}
	if unchanged {
		db.Event(ctx, "truck_charge_noop")
		return nil
	}
	if v.Version == 0 {
		err = tx.QueryRow(ctx, `INSERT INTO truck_charge_phases(truck_id,type_id,week_start,amount,included) VALUES($1,$2,$3::date,$4::numeric,$5) ON CONFLICT DO NOTHING RETURNING version`, v.TruckID, v.TypeID, v.WeekStart, v.Amount, v.Included).Scan(&version)
	} else {
		err = tx.QueryRow(ctx, `UPDATE truck_charge_phases SET amount=$4::numeric,included=$5,updated_by=NULL,version=version+1 WHERE truck_id=$1 AND type_id=$2 AND week_start=$3::date AND version=$6 RETURNING version`, v.TruckID, v.TypeID, v.WeekStart, v.Amount, v.Included, v.Version).Scan(&version)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrChargeConflict
	}
	if err != nil {
		return err
	}
	return nil
}
