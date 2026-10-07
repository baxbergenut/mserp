package repository

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"mserp/internal/datatruck"
)

var ErrDriverPayConflict = errors.New("this driver's week was edited elsewhere; reload before saving again")

type DriverPayRepository struct{ pool *pgxpool.Pool }

func NewDriverPayRepository(pool *pgxpool.Pool) *DriverPayRepository {
	return &DriverPayRepository{pool: pool}
}

type DriverPayAdjustment struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	Note   string `json:"note"`
	Amount string `json:"amount"`
}
type DriverPayEdits struct {
	Costs             *DriverPayCosts       `json:"costs,omitempty"`
	ExpenseDeductions []ExpenseDeduction    `json:"expenseDeductions,omitempty"`
	DriverID          string                `json:"driverId"`
	WeekStart         string                `json:"weekStart"`
	Notes             string                `json:"notes"`
	Comments          map[string]string     `json:"comments"`
	Adjustments       []DriverPayAdjustment `json:"adjustments"`
	FuelOverride      *string               `json:"fuelOverride"`
	TollOverride      *string               `json:"tollOverride"`
	Version           int                   `json:"version"`
	GeneratedCharges  []ChargeOccurrence    `json:"generatedCharges,omitempty"`
}
type DriverPayLoad struct {
	SourceDriverID   string   `json:"sourceDriverId,omitempty"`
	DriverFee        string   `json:"driverFee,omitempty"`
	Date             string   `json:"date"`
	Slot             int      `json:"slot"`
	LoadNumber       string   `json:"loadNumber"`
	LoadRecordID     *int     `json:"loadRecordId"`
	CommentKey       string   `json:"commentKey"`
	PickupDate       string   `json:"pickupDate"`
	PickupLocation   string   `json:"pickupLocation"`
	DeliveryLocation string   `json:"deliveryLocation"`
	OriginalRate     string   `json:"originalRate"`
	DriverGross      string   `json:"driverGross"`
	TotalMiles       string   `json:"totalMiles"`
	LoadedMiles      string   `json:"loadedMiles"`
	DeadheadMiles    string   `json:"deadheadMiles"`
	Fee              string   `json:"fee"`
	Issues           []string `json:"issues"`
}
type DriverPayDriver struct {
	InvestorID      string             `json:"investorId,omitempty"`
	ProfileDriverID string             `json:"profileDriverId,omitempty"`
	TruckID         string             `json:"truckId,omitempty"`
	AutoCharges     []PayAutoCharge    `json:"autoCharges,omitempty"`
	Issues          []string           `json:"issues,omitempty"`
	Settlement      *PayrollSettlement `json:"settlement,omitempty"`
	ID              string             `json:"id"`
	FullName        string             `json:"fullName"`
	TruckUnit       string             `json:"truckUnit"`
	DispatcherID    string             `json:"dispatcherId"`
	DispatcherName  string             `json:"dispatcherName"`
	IsOwnerOperator bool               `json:"isOwnerOperator"`
	PayType         string             `json:"payType"`
	PayRate         string             `json:"payRate"`
	FuelTotal       string             `json:"fuelTotal"`
	TollTotal       string             `json:"tollTotal"`
	Loads           []DriverPayLoad    `json:"loads"`
	Edits           DriverPayEdits     `json:"edits"`
}
type DriverPayWeek struct {
	Issues    []string          `json:"issues,omitempty"`
	Revision  string            `json:"revision"`
	WeekStart string            `json:"weekStart"`
	Drivers   []DriverPayDriver `json:"drivers"`
}

// One repeatable-read transaction provides a consistent snapshot of placement,
// source data, tariffs, accounting edits, and charge schedules. No load status
// or active-driver filter.
func (r *DriverPayRepository) Get(ctx context.Context, week time.Time) (DriverPayWeek, error) {
	return r.GetDriverWeek(ctx, week, "")
}

func (r *DriverPayRepository) GetDriverWeek(ctx context.Context, week time.Time, driverID string) (DriverPayWeek, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return DriverPayWeek{}, err
	}
	defer tx.Rollback(ctx)
	result, err := readDriverPayWeek(ctx, tx, week, driverID)
	if err != nil {
		return result, err
	}
	return result, tx.Commit(ctx)
}
func readDriverPayWeek(ctx context.Context, tx pgx.Tx, week time.Time, driverID string) (DriverPayWeek, error) {
	result, err := readDriverPaySourceWeek(ctx, tx, week, "")
	if err != nil {
		return result, err
	}
	result, _, err = routeTruckPay(ctx, tx, result)
	if err != nil {
		return result, err
	}
	if err = applyDriverPayCarry(ctx, tx, &result); err != nil {
		return result, err
	}
	if driverID != "" {
		selected := []DriverPayDriver{}
		for _, d := range result.Drivers {
			if d.ID == driverID {
				selected = append(selected, d)
			}
		}
		result.Drivers = selected
	}
	return overlayPayrollSettlements(ctx, tx, result, driverID)
}
func readDriverPaySourceWeek(ctx context.Context, tx pgx.Tx, week time.Time, driverID string) (DriverPayWeek, error) {
	result := DriverPayWeek{WeekStart: week.Format(time.DateOnly), Drivers: []DriverPayDriver{}}
	rows, err := tx.Query(ctx, driverPayCostsSQL+` SELECT d.id,d.full_name,coalesce(t.id::text,''),coalesce(t.unit_number,''),
 coalesce(dp.id::text,''),coalesce(dp.full_name,historical_dispatcher.dispatcher_name,'Unassigned'),d.pay_type,d.pay_rate::text,d.is_owner_operator,
 coalesce(e.service_date::text,''),coalesce(e.slot,0),coalesce(e.load_number,''),l.id,
 coalesce((coalesce(l.pickup_time,l.pickup_appointment_time) AT TIME ZONE 'UTC')::date::text,''),
 coalesce(l.total_pay::text,''),coalesce(e.driver_rate::text,''),coalesce(l.total_miles::text,''),l.raw_payload,
 coalesce(w.notes,''),coalesce(w.comments,'{}'::jsonb),coalesce(w.adjustments,'[]'::jsonb),coalesce(w.version,0),
 w.fuel_override::text,w.toll_override::text,coalesce(fuel.total,0)::text,coalesce(toll.total,0)::text
 FROM drivers d LEFT JOIN `+grossBoardEntriesSQL+` e ON d.id=e.driver_id
 AND e.service_date >= $1::date AND e.service_date < $1::date+7 AND NOT e.deleted AND btrim(e.load_number)<>''
 `+weeklyAssignmentJoins+`
 `+grossBoardResolvedLoad+`
 LEFT JOIN driver_pay_weeks w ON w.driver_id=d.id AND w.week_start=$1::date
 LEFT JOIN weekly_fuel fuel ON fuel.driver_id=d.id
 LEFT JOIN weekly_tolls toll ON toll.driver_id=d.id
 WHERE (NULLIF($2,'')::uuid IS NULL OR d.id=NULLIF($2,'')::uuid) AND (e.driver_id IS NOT NULL OR w.driver_id IS NOT NULL OR EXISTS
 (SELECT 1 FROM driver_charge_schedules cs WHERE cs.driver_id=d.id AND cs.start_week<=$1::date)
 OR EXISTS (SELECT 1 FROM expenses x WHERE x.charge_driver_id=d.id
 AND NOT x.driver_settled AND x.expense_date<$1::date+7)
 OR EXISTS (SELECT 1 FROM driver_escrows x WHERE x.driver_id=d.id AND x.start_date<$1::date+7)
 OR (d.is_owner_operator AND d.pay_type='gross_percentage' AND (coalesce(fuel.total,0)<>0 OR coalesce(toll.total,0)<>0)))
 ORDER BY d.full_name,d.id,e.service_date,e.slot`, week, driverID)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	index := map[string]int{}
	for rows.Next() {
		var d DriverPayDriver
		var l DriverPayLoad
		var raw, comments, adjustments []byte
		if err := rows.Scan(&d.ID, &d.FullName, &d.TruckID, &d.TruckUnit, &d.DispatcherID, &d.DispatcherName, &d.PayType, &d.PayRate, &d.IsOwnerOperator,
			&l.Date, &l.Slot, &l.LoadNumber, &l.LoadRecordID, &l.PickupDate, &l.OriginalRate, &l.DriverGross, &l.TotalMiles, &raw,
			&d.Edits.Notes, &comments, &adjustments, &d.Edits.Version,
			&d.Edits.FuelOverride, &d.Edits.TollOverride, &d.FuelTotal, &d.TollTotal); err != nil {
			return result, err
		}
		l.CommentKey = l.Date + ":" + strconv.Itoa(l.Slot) + ":" + strings.ToLower(strings.TrimSpace(l.LoadNumber))
		l.Issues = []string{}
		if l.LoadRecordID == nil {
			l.Issues = append(l.Issues, "Load not matched — resolve in Gross Board")
		}
		if len(raw) > 0 {
			var load datatruck.Load
			if err := json.Unmarshal(raw, &load); err != nil {
				return result, err
			}
			l.PickupLocation, l.DeliveryLocation = payLoadLocations(load.Stops)
			if load.Trip != nil {
				l.LoadedMiles = paySourceMiles(load.Trip.LoadedMiles)
				l.DeadheadMiles = paySourceMiles(load.Trip.DeadheadMiles)
			}
		}
		if l.LoadRecordID != nil && (l.PickupDate == "" || l.PickupLocation == "" || l.DeliveryLocation == "" || l.TotalMiles == "" || l.LoadedMiles == "" || l.DeadheadMiles == "") {
			l.Issues = append(l.Issues, "Source details incomplete — refresh load details")
		}
		basis := l.DriverGross
		if d.PayType == "cpm" {
			basis = l.TotalMiles
		}
		l.Fee = driverPayFee(d.PayType, d.PayRate, basis)
		if l.Fee == "" {
			l.Issues = append(l.Issues, "Pay needs a valid tariff and "+map[bool]string{true: "system mileage", false: "Gross Board driver gross"}[d.PayType == "cpm"])
		}
		i, ok := index[d.ID]
		if !ok {
			if err := json.Unmarshal(comments, &d.Edits.Comments); err != nil {
				return result, err
			}
			if err := json.Unmarshal(adjustments, &d.Edits.Adjustments); err != nil {
				return result, err
			}
			d.Edits.DriverID = d.ID
			d.Edits.WeekStart = result.WeekStart
			d.Loads = []DriverPayLoad{}
			i = len(result.Drivers)
			index[d.ID] = i
			result.Drivers = append(result.Drivers, d)
		}
		if l.LoadNumber != "" {
			result.Drivers[i].Loads = append(result.Drivers[i].Loads, l)
		}
	}
	if err = rows.Err(); err != nil {
		return result, err
	}
	rows.Close()
	data, loads, err := chargeData(ctx, tx, driverID)
	if err != nil {
		return result, err
	}
	generated, err := generatedForWeek(data, loads, result.WeekStart)
	if err != nil {
		return result, err
	}
	kept := []DriverPayDriver{}
	for _, d := range result.Drivers {
		d.Edits.ExpenseDeductions, err = expenseDeductions(ctx, tx, d.ID, result.WeekStart)
		if err != nil {
			return result, err
		}
		d.Edits.GeneratedCharges = generated[d.ID]
		if d.Edits.GeneratedCharges == nil {
			d.Edits.GeneratedCharges = []ChargeOccurrence{}
		}
		if len(d.Loads) > 0 || d.Edits.Version > 0 || len(d.Edits.GeneratedCharges) > 0 || len(d.Edits.ExpenseDeductions) > 0 || (d.IsOwnerOperator && d.PayType == "gross_percentage" && (d.FuelTotal != "0" && d.FuelTotal != "0.00" || d.TollTotal != "0" && d.TollTotal != "0.00")) {
			kept = append(kept, d)
		}
	}
	result.Drivers = kept
	return result, nil
}

func payLoadLocations(stops []datatruck.LoadStop) (string, string) {
	sorted := append([]datatruck.LoadStop(nil), stops...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Ordering < sorted[j].Ordering })
	pickup, delivery := "", ""
	for _, s := range sorted {
		parts := []string{}
		for _, v := range []string{s.Location.City, s.Location.State, s.Location.ZipCode} {
			if strings.TrimSpace(v) != "" {
				parts = append(parts, strings.TrimSpace(v))
			}
		}
		location := strings.Join(parts, ", ")
		if location == "" {
			location = strings.TrimSpace(s.Location.Address)
		}
		if s.StopType == "pickup" && pickup == "" {
			pickup = location
		}
		if s.StopType == "delivery" {
			delivery = location
		}
	}
	return pickup, delivery
}

func paySourceMiles(value *datatruck.FlexibleString) string {
	if value == nil {
		return ""
	}
	r, ok := new(big.Rat).SetString(value.String())
	if !ok || r.Sign() < 0 {
		return ""
	}
	return r.FloatString(2)
}

// Exact decimal multiplication, rounded once per visible load fee (half away
// from zero). Totals sum those rounded fees, so displayed rows reconcile.
func driverPayFee(kind, rate, basis string) string {
	tariff, ok := new(big.Rat).SetString(rate)
	if !ok || tariff.Sign() <= 0 {
		return ""
	}
	amount, ok := new(big.Rat).SetString(basis)
	if !ok {
		return ""
	}
	if kind == "cpm" && amount.Sign() < 0 {
		return ""
	}
	fee := new(big.Rat).Mul(tariff, amount)
	if kind == "gross_percentage" {
		fee.Quo(fee, big.NewRat(100, 1))
	} else if kind != "cpm" {
		return ""
	}
	return fee.FloatString(2)
}

func (r *DriverPayRepository) Save(ctx context.Context, edits DriverPayEdits, actors ...string) (DriverPayEdits, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return edits, err
	}
	defer tx.Rollback(ctx)
	if err = lockChargeDrivers(ctx, tx, []string{edits.DriverID}); err != nil {
		return edits, err
	}
	if err = assertPayrollOpen(ctx, tx, edits.DriverID, edits.WeekStart); err != nil {
		return edits, err
	}
	actor := ""
	if len(actors) > 0 {
		actor = actors[0]
	}
	costDriver, err := currentDriverPayCosts(ctx, tx, edits)
	if err != nil {
		return edits, err
	}
	if err = saveDriverPayCosts(ctx, tx, costDriver, edits, actor); err != nil {
		return edits, err
	}
	if edits.GeneratedCharges != nil {
		edits.GeneratedCharges, err = saveGeneratedCharges(ctx, tx, edits.DriverID, edits.WeekStart, actor, edits.GeneratedCharges)
		if err != nil {
			return edits, err
		}
	}
	if edits.ExpenseDeductions != nil {
		week, e := chargeWeek(edits.WeekStart)
		if e != nil {
			return edits, e
		}
		routed, e := readDriverPayWeek(ctx, tx, week, edits.DriverID)
		if e != nil {
			return edits, e
		}
		allowed := map[string]bool{}
		for _, d := range routed.Drivers {
			for _, x := range d.Edits.ExpenseDeductions {
				allowed[x.ExpenseID] = true
			}
		}
		for _, x := range edits.ExpenseDeductions {
			if x.Apply && !allowed[x.ExpenseID] {
				return edits, chargeInvalid("This expense belongs to another settlement; reload payroll")
			}
		}
		edits.ExpenseDeductions, err = saveExpenseDeductions(ctx, tx, edits.DriverID, edits.WeekStart, actor, edits.ExpenseDeductions)
		if err != nil {
			return edits, err
		}
		filtered := []ExpenseDeduction{}
		for _, item := range edits.ExpenseDeductions {
			if allowed[item.ExpenseID] {
				filtered = append(filtered, item)
			}
		}
		edits.ExpenseDeductions = filtered
	}
	comments, err := json.Marshal(edits.Comments)
	if err != nil {
		return edits, err
	}
	adjustments, err := json.Marshal(edits.Adjustments)
	if err != nil {
		return edits, err
	}
	var version int
	if edits.Version == 0 {
		err = tx.QueryRow(ctx, `INSERT INTO driver_pay_weeks(driver_id,week_start,notes,comments,adjustments,fuel_override,toll_override)
 VALUES($1,$2::date,$3,$4,$5,$6::numeric,$7::numeric) ON CONFLICT DO NOTHING RETURNING version`, edits.DriverID, edits.WeekStart, edits.Notes, comments, adjustments, edits.FuelOverride, edits.TollOverride).Scan(&version)
	} else {
		err = tx.QueryRow(ctx, `UPDATE driver_pay_weeks SET notes=$3,comments=$4,adjustments=$5,fuel_override=$7::numeric,toll_override=$8::numeric,version=version+1,updated_at=now()
 WHERE driver_id=$1 AND week_start=$2::date AND version=$6 RETURNING version`, edits.DriverID, edits.WeekStart, edits.Notes, comments, adjustments, edits.Version, edits.FuelOverride, edits.TollOverride).Scan(&version)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return edits, ErrDriverPayConflict
	}
	edits.Version = version
	if err != nil {
		return edits, err
	}
	refreshed, err := currentDriverPayCosts(ctx, tx, edits)
	if err != nil {
		return edits, err
	}
	edits.Costs = refreshed.Edits.Costs
	return edits, tx.Commit(ctx)
}
