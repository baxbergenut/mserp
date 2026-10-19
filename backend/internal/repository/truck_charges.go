package repository

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
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

func truckChargeData(ctx context.Context, q chargeQuery) (TruckChargeData, error) {
	d := TruckChargeData{Terms: []TruckTerm{}, Phases: []TruckChargePhase{}}
	rows, err := q.Query(ctx, `SELECT truck_id::text,week_start::text,owner_id::text,share_percent::text,version FROM truck_settlement_terms WHERE ($1::uuid[] IS NULL OR truck_id=ANY($1::uuid[])) ORDER BY week_start,truck_id`, payrollTruckIDs(ctx))
	if err != nil {
		return d, err
	}
	for rows.Next() {
		var v TruckTerm
		if err = rows.Scan(&v.TruckID, &v.WeekStart, &v.OwnerID, &v.SharePercent, &v.Version); err != nil {
			rows.Close()
			return d, err
		}
		d.Terms = append(d.Terms, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return d, err
	}
	rows, err = q.Query(ctx, `SELECT p.truck_id::text,p.type_id::text,p.week_start::text,p.amount::text,p.included,p.version,t.version FROM truck_charge_phases p JOIN driver_charge_types t ON t.id=p.type_id WHERE ($1::uuid[] IS NULL OR p.truck_id=ANY($1::uuid[])) ORDER BY p.week_start,p.truck_id,p.type_id`, payrollTruckIDs(ctx))
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
	d, err := truckChargeData(ctx, r.pool)
	if err != nil {
		return d, err
	}
	d.EligibleTruckIDs = []string{}
	d.Owners = []TruckChargeOwner{}
	rows, err := r.pool.Query(ctx, `SELECT t.id::text,i.id::text,coalesce(driver.full_name,i.full_name)
 FROM trucks t JOIN LATERAL (
 SELECT h.owner_id FROM truck_ownership_history h WHERE h.truck_id=t.id
 AND (NOT h.start_known OR h.unassigned_at IS NULL OR h.unassigned_at>h.assigned_at)
 AND (NOT h.start_known OR h.assigned_at < (($1::date+7)::timestamp AT TIME ZONE 'America/New_York'))
 AND (h.unassigned_at IS NULL OR h.unassigned_at >= (($1::date+7)::timestamp AT TIME ZONE 'America/New_York'))
 ORDER BY h.assigned_at DESC,h.id LIMIT 1
 ) ownership ON true JOIN investors i ON i.id=ownership.owner_id
 LEFT JOIN drivers driver ON driver.id=i.driver_id WHERE NOT i.is_company ORDER BY t.id`, week)
	if err != nil {
		return d, err
	}
	defer rows.Close()
	eligible := map[string]bool{}
	for rows.Next() {
		var owner TruckChargeOwner
		if err = rows.Scan(&owner.TruckID, &owner.OwnerID, &owner.OwnerName); err != nil {
			return d, err
		}
		d.EligibleTruckIDs = append(d.EligibleTruckIDs, owner.TruckID)
		d.Owners = append(d.Owners, owner)
		eligible[owner.TruckID] = true
	}
	terms := []TruckTerm{}
	for _, term := range d.Terms {
		if eligible[term.TruckID] {
			terms = append(terms, term)
		}
	}
	phases := []TruckChargePhase{}
	for _, phase := range d.Phases {
		if eligible[phase.TruckID] {
			phases = append(phases, phase)
		}
	}
	d.Terms, d.Phases = terms, phases
	if err = rows.Err(); err != nil {
		return d, err
	}
	rows.Close()
	// Use the same affected-period check as writes, including later finalized
	// statements, so selecting all can omit protected cells before saving.
	d.LockedCharges = []TruckChargeLock{}
	rows, err = r.pool.Query(ctx, `SELECT input.truck_id::text,input.type_id::text
 FROM (SELECT t.id AS truck_id,c.id AS type_id,$2::date AS week_start,'charge'::text AS kind
 FROM trucks t CROSS JOIN driver_charge_types c WHERE t.id=ANY($1::uuid[])) input
 WHERE `+truckPeriodClosedSQL, d.EligibleTruckIDs, week)
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

func requireInvestorTruck(ctx context.Context, tx pgx.Tx, truck string) error {
	var eligible bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM trucks t JOIN investors i ON i.id=t.owner_id WHERE t.id=$1 AND `+investorTruckEligibility+`)`, truck).Scan(&eligible); err != nil {
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
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(736281940)`)
	return err
}

const truckPeriodClosedSQL = `EXISTS(SELECT 1 FROM investor_pay_weeks w WHERE w.truck_id=input.truck_id AND w.finalized AND w.week_start >= input.week_start
 AND w.week_start < coalesce((SELECT min(week_start) FROM truck_settlement_terms WHERE truck_id=input.truck_id AND week_start>input.week_start AND input.kind='terms'),
 (SELECT min(week_start) FROM truck_charge_phases WHERE truck_id=input.truck_id AND type_id=input.type_id AND week_start>input.week_start AND input.kind='charge'),'2101-01-01'::date))
 OR EXISTS(SELECT 1 FROM payroll_settlements s WHERE s.finalized AND s.week_start >= input.week_start AND
 (EXISTS(SELECT 1 FROM truck_settlement_terms t JOIN investors i ON i.id=t.owner_id WHERE t.truck_id=input.truck_id AND i.driver_id=s.driver_id)
 OR EXISTS(SELECT 1 FROM truck_ownership_history h JOIN investors i ON i.id=h.owner_id WHERE h.truck_id=input.truck_id AND i.driver_id=s.driver_id)))`

func protectTruckPeriod(ctx context.Context, tx pgx.Tx, truck, week, kind, typeID string) error {
	var closed bool
	err := tx.QueryRow(ctx, `SELECT `+truckPeriodClosedSQL+`
 FROM (SELECT $1::uuid AS truck_id,$2::date AS week_start,$3::text AS kind,nullif($4,'')::uuid AS type_id) input`, truck, week, kind, typeID).Scan(&closed)
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
	tx, err := r.pool.Begin(ctx)
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
	if err = requireInvestorTruck(ctx, tx, v.TruckID); err != nil {
		return err
	}
	var previousOwner bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM investor_pay_weeks w WHERE w.truck_id=$1 AND w.owner_id<>$3 AND w.week_start >= $2::date AND w.week_start < coalesce((SELECT min(week_start) FROM truck_settlement_terms WHERE truck_id=$1 AND week_start>$2::date),'2101-01-01'::date))`, v.TruckID, v.WeekStart, v.OwnerID).Scan(&previousOwner); err != nil {
		return err
	}
	if previousOwner {
		return chargeInvalid("Saved investor statements retain their owner; use a later effective week")
	}
	var active bool
	if err = tx.QueryRow(ctx, `SELECT active AND NOT is_company FROM investors WHERE id=$1`, v.OwnerID).Scan(&active); err != nil {
		return err
	}
	if !active {
		return chargeInvalid("Choose an active investor")
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
	return tx.Commit(ctx)
}
func (r *DriverChargeRepository) SaveTruckCharge(ctx context.Context, v TruckChargePhase, actor string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = lockTruckAccounting(ctx, tx); err != nil {
		return err
	}
	if err = saveTruckCharge(ctx, tx, v, actor); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// One request, financial lock and commit for the whole selection. Savepoints
// isolate expected row failures so a stale or finalized truck cannot stop later
// trucks. Unexpected database failures roll back the batch rather than claiming
// that any uncommitted rows succeeded.
func (r *DriverChargeRepository) SaveTruckCharges(ctx context.Context, updates []TruckChargePhase, actor string) ([]TruckChargeBulkResult, error) {
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
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if err = lockTruckAccounting(ctx, tx); err != nil {
		return nil, err
	}
	results := make([]TruckChargeBulkResult, 0, len(updates))
	for _, v := range updates {
		row, err := tx.Begin(ctx) // pgx nested transactions are savepoints.
		if err != nil {
			return nil, err
		}
		result := TruckChargeBulkResult{TruckID: v.TruckID, TypeID: v.TypeID, WeekStart: v.WeekStart}
		if err = saveTruckCharge(ctx, row, v, actor); err != nil {
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
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return results, nil
}

func saveTruckCharge(ctx context.Context, tx pgx.Tx, v TruckChargePhase, actor string) error {
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
	if err = requireInvestorTruck(ctx, tx, v.TruckID); err != nil {
		return err
	}
	var options []string
	var version int
	var archived bool
	if err = tx.QueryRow(ctx, `SELECT amounts::text[],version,archived FROM driver_charge_types WHERE id=$1 FOR SHARE`, v.TypeID).Scan(&options, &version, &archived); err != nil {
		return err
	}
	if version != v.TypeVersion {
		return ErrChargeConflict
	}
	if v.Included && (archived || !allowedChargeAmount(options, v.Amount)) {
		return chargeInvalid("Choose an available amount from an active charge type")
	}
	if v.MoveScheduleID != "" {
		return chargeInvalid("Driver recurring charges have been removed; select the truck fee directly")
	}
	if v.Version == 0 {
		err = tx.QueryRow(ctx, `INSERT INTO truck_charge_phases(truck_id,type_id,week_start,amount,included,updated_by) VALUES($1,$2,$3::date,$4::numeric,$5,nullif($6,'')::uuid) ON CONFLICT DO NOTHING RETURNING version`, v.TruckID, v.TypeID, v.WeekStart, v.Amount, v.Included, actor).Scan(&version)
	} else {
		err = tx.QueryRow(ctx, `UPDATE truck_charge_phases SET amount=$4::numeric,included=$5,updated_by=nullif($6,'')::uuid,version=version+1 WHERE truck_id=$1 AND type_id=$2 AND week_start=$3::date AND version=$7 RETURNING version`, v.TruckID, v.TypeID, v.WeekStart, v.Amount, v.Included, actor, v.Version).Scan(&version)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrChargeConflict
	}
	if err != nil {
		return err
	}
	if err = truckAudit(ctx, tx, v.TruckID, v.WeekStart, "charge", actor, v); err != nil {
		return err
	}
	return nil
}
