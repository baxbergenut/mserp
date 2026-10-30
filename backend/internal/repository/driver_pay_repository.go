package repository

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"mserp/internal/db"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"mserp/internal/datatruck"
)

var ErrDriverPayConflict = errors.New("this driver's week was edited elsewhere; reload before saving again")

type DriverPayRepository struct {
	pool  *pgxpool.Pool
	cache payrollCache
}

func NewDriverPayRepository(pool *pgxpool.Pool) *DriverPayRepository {
	return &DriverPayRepository{pool: pool}
}

type DriverPayAdjustment struct {
	LegacySheets bool   `json:"legacySheets,omitempty"`
	ID           string `json:"id"`
	Kind         string `json:"kind"`
	Name         string `json:"name"`
	Note         string `json:"note"`
	Amount       string `json:"amount"`
}
type DriverPayEdits struct {
	SourceCharges        []PaySourceCharge     `json:"sourceCharges,omitempty"`
	LayoverExcludedDates []string              `json:"layoverExcludedDates,omitempty"`
	Costs                *DriverPayCosts       `json:"costs,omitempty"`
	ExpenseDeductions    []ExpenseDeduction    `json:"expenseDeductions,omitempty"`
	DriverID             string                `json:"driverId"`
	WeekStart            string                `json:"weekStart"`
	Notes                string                `json:"notes"`
	Comments             map[string]string     `json:"comments"`
	Adjustments          []DriverPayAdjustment `json:"adjustments"`
	FuelOverride         *string               `json:"fuelOverride"`
	TollOverride         *string               `json:"tollOverride"`
	Version              int                   `json:"version"`
	GeneratedCharges     []ChargeOccurrence    `json:"generatedCharges,omitempty"`
}
type DriverPayLoad struct {
	PayrollSupplement       bool     `json:"payrollSupplement,omitempty"`
	PayrollPlacementVersion int      `json:"payrollPlacementVersion,omitempty"`
	NotReady                bool     `json:"notReady,omitempty"`
	Status                  string   `json:"status,omitempty"`
	PlacementVersion        int      `json:"placementVersion,omitempty"`
	SourceSlot              int      `json:"sourceSlot"`
	SourceDate              string   `json:"sourceDate,omitempty"`
	DispatchDate            string   `json:"dispatchDate,omitempty"`
	DispatchSlot            int      `json:"dispatchSlot,omitempty"`
	TransferVersion         int      `json:"transferVersion,omitempty"`
	CreditedDispatcherID    string   `json:"creditedDispatcherId,omitempty"`
	CreditedDispatcherName  string   `json:"creditedDispatcherName,omitempty"`
	LoadTripID              *int     `json:"loadTripId,omitempty"`
	TripReview              bool     `json:"tripReview,omitempty"`
	BoardVersion            int      `json:"boardVersion"`
	SystemOriginalRate      string   `json:"systemOriginalRate"`
	SystemMiles             string   `json:"systemMiles"`
	SourceDriverID          string   `json:"sourceDriverId,omitempty"`
	DriverFee               string   `json:"driverFee,omitempty"`
	Date                    string   `json:"date"`
	Slot                    int      `json:"slot"`
	LoadNumber              string   `json:"loadNumber"`
	LoadRecordID            *int     `json:"loadRecordId"`
	CommentKey              string   `json:"commentKey"`
	PickupDate              string   `json:"pickupDate"`
	PickupLocation          string   `json:"pickupLocation"`
	DeliveryLocation        string   `json:"deliveryLocation"`
	OriginalRate            string   `json:"originalRate"`
	DriverGross             string   `json:"driverGross"`
	TotalMiles              string   `json:"totalMiles"`
	LoadedMiles             string   `json:"loadedMiles"`
	DeadheadMiles           string   `json:"deadheadMiles"`
	Fee                     string   `json:"fee"`
	Issues                  []string `json:"issues"`
}
type PayPerson struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type DriverPayDriver struct {
	costTransactions []PayCostTransaction
	HomeWeek         bool                 `json:"homeWeek,omitempty"`
	DayStatuses      []PayDayStatus       `json:"dayStatuses"`
	Summary          *PayStatementSummary `json:"summary,omitempty"`
	Layovers         []DriverPayLayover   `json:"layovers,omitempty"`
	TeamNotice       string               `json:"teamNotice,omitempty"`
	TeamDrivers      []PayPerson          `json:"teamDrivers,omitempty"`
	TruckInactive    bool                 `json:"truckInactive,omitempty"`
	OperatingDrivers []PayPerson          `json:"operatingDrivers,omitempty"`
	InvestorID       string               `json:"investorId,omitempty"`
	ProfileDriverID  string               `json:"profileDriverId,omitempty"`
	TruckID          string               `json:"truckId,omitempty"`
	AutoCharges      []PayAutoCharge      `json:"autoCharges,omitempty"`
	Issues           []string             `json:"issues,omitempty"`
	Settlement       *PayrollSettlement   `json:"settlement,omitempty"`
	ID               string               `json:"id"`
	FullName         string               `json:"fullName"`
	TruckUnit        string               `json:"truckUnit"`
	DispatcherID     string               `json:"dispatcherId"`
	DispatcherName   string               `json:"dispatcherName"`
	Dispatchers      []PayPerson          `json:"dispatchers,omitempty"`
	EmploymentType   string               `json:"employmentType,omitempty"`
	IsOwnerOperator  bool                 `json:"isOwnerOperator"`
	PayType          string               `json:"payType"`
	PayRate          string               `json:"payRate"`
	FuelTotal        string               `json:"fuelTotal"`
	TollTotal        string               `json:"tollTotal"`
	Loads            []DriverPayLoad      `json:"loads"`
	Edits            DriverPayEdits       `json:"edits"`
}
type DriverPayWeek struct {
	ManagedInSheets bool                 `json:"managedInSheets,omitempty"`
	Pagination      *PayPageInfo         `json:"pagination,omitempty"`
	SetupRequired   []InvestorTruckSetup `json:"setupRequired,omitempty"`
	Issues          []string             `json:"issues,omitempty"`
	Notices         []string             `json:"notices,omitempty"`
	Revision        string               `json:"revision"`
	WeekStart       string               `json:"weekStart"`
	Drivers         []DriverPayDriver    `json:"drivers"`
}

type InvestorTruckSetup struct {
	DriverID       string `json:"driverId,omitempty"`
	DriverName     string `json:"driverName,omitempty"`
	DispatcherID   string `json:"dispatcherId,omitempty"`
	DispatcherName string `json:"dispatcherName,omitempty"`
	TruckID        string `json:"truckId"`
	TruckUnit      string `json:"truckUnit"`
	OwnerID        string `json:"ownerId"`
	OwnerName      string `json:"ownerName"`
}

// One repeatable-read transaction provides a consistent snapshot of placement,
// source data, tariffs, accounting edits, and charge schedules. No load status
// filter. Inactive drivers require actual weekly work or saved payroll history.
func (r *DriverPayRepository) Get(ctx context.Context, week time.Time) (DriverPayWeek, error) {
	return r.GetDriverWeek(ctx, week, "")
}

func (r *DriverPayRepository) GetDriverWeek(ctx context.Context, week time.Time, driverID string) (DriverPayWeek, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return DriverPayWeek{}, err
	}
	defer tx.Rollback(ctx)
	result, err := r.cachedWeek(ctx, tx, "driver:"+driverID, week, func() (DriverPayWeek, error) { return readDriverPayWeek(ctx, tx, week, driverID) })
	result = selectPayDriver(result, driverID)
	if err != nil {
		return result, err
	}
	return result, tx.Commit(ctx)
}
func readDriverPayWeek(ctx context.Context, tx pgx.Tx, week time.Time, driverID string) (DriverPayWeek, error) {
	defer db.Phase(ctx, "driver_calculation")()
	ctx = payrollWeekContext(ctx, week.Format(time.DateOnly))
	if err := payrollQueryPlans(ctx, tx); err != nil {
		return DriverPayWeek{}, err
	}
	var selectionErr error
	ctx, selectionErr = selectPayrollContext(ctx, tx, "driver", driverID)
	if selectionErr != nil {
		return DriverPayWeek{}, selectionErr
	}
	ctx = payrollReadContext(ctx, tx)
	result, err := readDriverPaySourceWeek(ctx, tx, week, "")
	if err != nil {
		return result, err
	}
	result, _, err = routeTruckPay(ctx, tx, result)
	if err != nil {
		return result, err
	}
	if err = routeTeamPay(ctx, tx, &result); err != nil {
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
	// Carry and team routing can add rows after the source read.
	if err = setPayDispatchers(ctx, tx, &result); err != nil {
		return result, err
	}
	result, err = overlayPayrollSettlements(ctx, tx, result, driverID)
	if err != nil {
		return result, err
	}
	if err = finishPayrollRead(ctx, tx, "driver", &result); err != nil {
		return result, err
	}
	return result, nil
}
func readDriverPaySourceWeek(ctx context.Context, tx pgx.Tx, week time.Time, driverID string) (DriverPayWeek, error) {
	defer db.Phase(ctx, "source")()
	result := DriverPayWeek{WeekStart: week.Format(time.DateOnly), Drivers: []DriverPayDriver{}}
	rows, err := tx.Query(ctx, driverPayCostsSQL+payDriverFactsSQL+","+payrollEscrowCapacityCTE+payDriverEligibilitySQL+","+payrollEntriesCTE+` SELECT d.id,d.full_name,d.pay_truck_id,d.pay_truck_unit,
 d.pay_dispatcher_id,d.pay_dispatcher_name,d.pay_type,d.pay_rate::text,d.pay_employment_type,(d.pay_employment_type='owner_operator'),
 coalesce(e.source_date::text,''),coalesce(e.source_slot,0),coalesce(e.dispatch_date,''),coalesce(e.dispatch_slot,0),coalesce(e.transfer_version,0),coalesce(e.credited_dispatcher_id,''),coalesce(e.credited_dispatcher_name,''),coalesce(e.service_date::text,''),coalesce(e.slot,0),coalesce(e.load_number,''),l.id,
 coalesce((coalesce(l.pickup_time,l.pickup_appointment_time) AT TIME ZONE 'UTC')::date::text,''),
 coalesce((CASE WHEN trip_source.review THEN NULL ELSE coalesce(e.entered_original_rate,trip_source.original,e.original_rate) END)::text,''),coalesce(e.driver_rate::text,''),
 coalesce((CASE WHEN trip_source.review THEN NULL ELSE coalesce(e.entered_miles,trip_source.miles,e.miles) END)::text,''),coalesce(e.version,0),coalesce(trip_source.original::text,''),coalesce(trip_source.miles::text,''),
 CASE WHEN l.id IS NOT NULL THEN jsonb_build_object('stops',l.raw_payload->'stops','trip',CASE WHEN e.load_trip_id IS NOT NULL THEN trip_source.selected_trip ELSE l.raw_payload->'trip' END) END,
 e.load_trip_id,coalesce(trip_source.review,false),coalesce(e.payroll_supplement,false),coalesce(e.payroll_placement_version,0),coalesce(l.status,''),
 e.load_trip_id IS NOT NULL AND EXISTS(SELECT 1 FROM `+grossBoardEntriesSQL+` prior WHERE NOT prior.deleted AND prior.day_status='' AND prior.driver_id=e.driver_id AND prior.load_record_id=l.id AND prior.load_trip_id=e.load_trip_id AND (prior.service_date,prior.slot)<(e.source_date,e.source_slot)),
 CASE WHEN $4::boolean THEN '' ELSE coalesce(w.notes,'') END,CASE WHEN $4::boolean THEN '{}'::jsonb ELSE coalesce(w.comments,'{}'::jsonb) END,coalesce(w.adjustments,'[]'::jsonb),coalesce(w.version,0),
 w.fuel_override::text,w.toll_override::text,coalesce(fuel.total,0)::text,coalesce(toll.total,0)::text,coalesce(w.layover_excluded_dates,'{}'::text[])
 FROM eligible_pay_drivers d LEFT JOIN pay_entries e ON d.id=e.driver_id
 AND (btrim(e.load_number)<>'' OR e.day_status='SHOP')
 `+payrollResolvedLoad+`
 LEFT JOIN driver_pay_weeks w ON w.driver_id=d.id AND w.week_start=$1::date
 LEFT JOIN weekly_fuel fuel ON fuel.driver_id=d.id
 LEFT JOIN weekly_tolls toll ON toll.driver_id=d.id
 WHERE (NULLIF($2,'')::uuid IS NULL OR d.id=NULLIF($2,'')::uuid)
 AND (d.pay_enabled OR e.driver_id IS NOT NULL OR w.driver_id IS NOT NULL OR d.pay_release OR d.pay_scale OR d.pay_reimburse)
 AND (d.pay_reimburse OR d.pay_scale OR d.pay_release OR e.driver_id IS NOT NULL OR w.driver_id IS NOT NULL OR d.pay_charge OR d.pay_expense OR d.pay_escrow
 OR (d.pay_employment_type='owner_operator' AND d.pay_type='gross_percentage' AND (coalesce(fuel.total,0)<>0 OR coalesce(toll.total,0)<>0)))
 ORDER BY d.full_name,d.id,e.service_date,e.slot`, week, driverID, payrollDriverIDs(ctx), payrollSummaryOnly(ctx))
	if err != nil {
		return result, err
	}
	defer rows.Close()
	index := map[string]int{}
	for rows.Next() {
		var d DriverPayDriver
		var l DriverPayLoad
		var raw, comments, adjustments []byte
		var repeatedTrip bool
		if err := rows.Scan(&d.ID, &d.FullName, &d.TruckID, &d.TruckUnit, &d.DispatcherID, &d.DispatcherName, &d.PayType, &d.PayRate, &d.EmploymentType, &d.IsOwnerOperator,
			&l.SourceDate, &l.SourceSlot, &l.DispatchDate, &l.DispatchSlot, &l.TransferVersion, &l.CreditedDispatcherID, &l.CreditedDispatcherName, &l.Date, &l.Slot, &l.LoadNumber, &l.LoadRecordID, &l.PickupDate, &l.OriginalRate, &l.DriverGross, &l.TotalMiles, &l.BoardVersion, &l.SystemOriginalRate, &l.SystemMiles, &raw, &l.LoadTripID, &l.TripReview, &l.PayrollSupplement, &l.PayrollPlacementVersion, &l.Status, &repeatedTrip,
			&d.Edits.Notes, &comments, &adjustments, &d.Edits.Version,
			&d.Edits.FuelOverride, &d.Edits.TollOverride, &d.FuelTotal, &d.TollTotal, &d.Edits.LayoverExcludedDates); err != nil {
			return result, err
		}
		if repeatedTrip {
			continue
		}
		l.CommentKey = l.Date + ":" + strconv.Itoa(l.Slot) + ":" + strings.ToLower(strings.TrimSpace(l.LoadNumber))
		l.Issues = []string{}
		l.NotReady = l.LoadRecordID != nil && !payLoadReady(l.Status)
		if l.NotReady {
			l.Issues = append(l.Issues, "Load is not delivered or invoiced ("+l.Status+")")
		}
		if l.TripReview {
			l.Issues = append(l.Issues, "Split load needs a valid trip selection in Gross Board")
		}
		if l.LoadRecordID == nil {
			l.Issues = append(l.Issues, "Load not matched — resolve in Gross Board")
		}
		if len(raw) > 0 {
			var load datatruck.Load
			if err := json.Unmarshal(raw, &load); err != nil {
				return result, err
			}
			l.PickupLocation, l.DeliveryLocation = payLoadLocations(load.Stops)
			if load.Trip != nil && !l.TripReview {
				if l.LoadTripID != nil {
					l.PickupDate = load.Trip.BeginDate
					if load.Trip.Pickup != nil {
						l.PickupLocation = strings.TrimSpace(load.Trip.Pickup.City + ", " + load.Trip.Pickup.State)
					}
					if load.Trip.Delivery != nil {
						l.DeliveryLocation = strings.TrimSpace(load.Trip.Delivery.City + ", " + load.Trip.Delivery.State)
					}
				}
				l.LoadedMiles = paySourceMiles(load.Trip.LoadedMiles)
				l.DeadheadMiles = paySourceMiles(load.Trip.DeadheadMiles)
			}
		}
		if l.LoadRecordID != nil && (l.PickupDate == "" || l.PickupLocation == "" || l.DeliveryLocation == "" || l.TotalMiles == "" || l.LoadedMiles == "" || l.DeadheadMiles == "") {
			l.Issues = append(l.Issues, "Source details incomplete — refresh load details")
		}
		if payrollSummaryOnly(ctx) {
			l.PickupLocation, l.DeliveryLocation, l.LoadedMiles, l.DeadheadMiles, l.SystemOriginalRate, l.SystemMiles = "", "", "", "", "", ""
		}
		basis := l.DriverGross
		if d.PayType == "cpm" {
			basis = l.TotalMiles
		}
		l.Fee = driverPayFee(d.PayType, d.PayRate, basis)
		if l.TripReview {
			l.Fee = ""
		}
		if l.Fee == "" {
			l.Issues = append(l.Issues, "Pay needs a valid tariff and "+map[bool]string{true: "Gross Board mileage", false: "Gross Board driver gross"}[d.PayType == "cpm"])
		}
		i, ok := index[d.ID]
		if !ok {
			if err := func() error {
				if payrollSummaryOnly(ctx) {
					d.Edits.Notes = ""
					d.Edits.Comments = map[string]string{}
					return nil
				}
				return json.Unmarshal(comments, &d.Edits.Comments)
			}(); err != nil {
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
	if payrollCostsOnly(ctx) {
		return result, nil
	}
	data, loads, err := payrollChargeData(ctx, tx, driverID)
	if err != nil {
		return result, err
	}
	generated, err := generatedForWeek(data, loads, result.WeekStart)
	if err != nil {
		return result, err
	}
	ids := make([]string, 0, len(result.Drivers))
	for _, d := range result.Drivers {
		ids = append(ids, d.ID)
	}
	credits, err := escrowReleaseCreditsBulk(ctx, tx, ids, result.WeekStart)
	if err != nil {
		return result, err
	}
	deductions, err := expenseDeductionsBulk(ctx, tx, ids, result.WeekStart)
	if err != nil {
		return result, err
	}
	scaleCharges, _, err := wmtPayrollCharges(ctx, tx, result.WeekStart)
	if err != nil {
		return result, err
	}
	layovers, err := driverPayLayovers(ctx, tx, result.WeekStart)
	if err != nil {
		return result, err
	}
	kept := []DriverPayDriver{}
	for _, d := range result.Drivers {
		d.Layovers = layovers[d.ID]
		d.AutoCharges = append(credits[d.ID], scaleCharges[d.ID]...)
		if d.AutoCharges == nil {
			d.AutoCharges = []PayAutoCharge{}
		}
		d.Edits.ExpenseDeductions = deductions[d.ID]
		if d.Edits.ExpenseDeductions == nil {
			d.Edits.ExpenseDeductions = []ExpenseDeduction{}
		}
		d.Edits.GeneratedCharges = generated[d.ID]
		if d.Edits.GeneratedCharges == nil {
			d.Edits.GeneratedCharges = []ChargeOccurrence{}
		}
		if len(d.Layovers) > 0 || len(d.AutoCharges) > 0 || len(d.Loads) > 0 || d.Edits.Version > 0 || len(d.Edits.GeneratedCharges) > 0 || len(d.Edits.ExpenseDeductions) > 0 || (d.IsOwnerOperator && d.PayType == "gross_percentage" && (d.FuelTotal != "0" && d.FuelTotal != "0.00" || d.TollTotal != "0" && d.TollTotal != "0.00")) {
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
	if err := validateLayoverExclusions(edits.WeekStart, edits.LayoverExcludedDates); err != nil {
		return edits, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return edits, err
	}
	defer tx.Rollback(ctx)
	endLock := db.Phase(ctx, "financial_lock_wait")
	_, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(736281940)`)
	endLock()
	if err != nil {
		return edits, err
	}
	members, err := teamPayMembers(ctx, tx, edits.DriverID, edits.WeekStart)
	if err != nil {
		return edits, err
	}
	if err = lockChargeDrivers(ctx, tx, members); err != nil {
		return edits, err
	}
	if err = assertPayrollOpen(ctx, tx, edits.DriverID, edits.WeekStart); err != nil {
		return edits, err
	}
	actor := ""
	if len(actors) > 0 {
		actor = actors[0]
	}
	ctx, err = selectPayrollContext(payrollWeekContext(ctx, edits.WeekStart), tx, "driver", edits.DriverID)
	if err != nil {
		return edits, err
	}
	endValidation := db.Phase(ctx, "save_calculation")
	costDriver, err := currentDriverPayCosts(ctx, tx, edits)
	endValidation()
	if err != nil {
		return edits, err
	}
	// Older clients omit this field; omission must not resurrect removed bonuses.
	if edits.LayoverExcludedDates == nil {
		edits.LayoverExcludedDates = costDriver.Edits.LayoverExcludedDates
	}
	if edits.LayoverExcludedDates == nil {
		edits.LayoverExcludedDates = []string{}
	}
	defer db.Phase(ctx, "writes_and_commit")()
	if err = savePaySources(ctx, tx, costDriver, &edits, "driver", actor); err != nil {
		return edits, err
	}
	if err = saveDriverPayCosts(ctx, tx, costDriver, edits, actor); err != nil {
		return edits, err
	}
	if edits.GeneratedCharges != nil {
		edits.GeneratedCharges, err = saveTeamCharges(ctx, tx, edits.DriverID, edits.WeekStart, actor, edits.GeneratedCharges)
		if err != nil {
			return edits, err
		}
	}
	if edits.ExpenseDeductions != nil {
		allowed := map[string]bool{}
		for _, x := range costDriver.Edits.ExpenseDeductions {
			allowed[x.ExpenseID] = true
		}
		for _, x := range edits.ExpenseDeductions {
			if x.Apply && !allowed[x.ExpenseID] {
				// The principal may have been collected in another week or moved to
				// another payer since this tab loaded. Treat both as stale payroll.
				return edits, ErrDriverPayConflict
			}
		}
		edits.ExpenseDeductions, err = saveTeamExpenses(ctx, tx, edits.DriverID, edits.WeekStart, actor, edits.ExpenseDeductions)
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
		err = tx.QueryRow(ctx, `INSERT INTO driver_pay_weeks(driver_id,week_start,notes,comments,adjustments,fuel_override,toll_override,layover_excluded_dates)
 VALUES($1,$2::date,$3,$4,$5,$6::numeric,$7::numeric,$8) ON CONFLICT DO NOTHING RETURNING version`, edits.DriverID, edits.WeekStart, edits.Notes, comments, adjustments, edits.FuelOverride, edits.TollOverride, edits.LayoverExcludedDates).Scan(&version)
	} else {
		err = tx.QueryRow(ctx, `UPDATE driver_pay_weeks SET notes=$3,comments=$4,adjustments=$5,fuel_override=$7::numeric,toll_override=$8::numeric,layover_excluded_dates=$9,version=version+1,updated_at=now()
 WHERE driver_id=$1 AND week_start=$2::date AND version=$6 RETURNING version`, edits.DriverID, edits.WeekStart, edits.Notes, comments, adjustments, edits.Version, edits.FuelOverride, edits.TollOverride, edits.LayoverExcludedDates).Scan(&version)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return edits, ErrDriverPayConflict
	}
	edits.Version = version
	if err != nil {
		return edits, err
	}
	refreshed := DriverPayWeek{WeekStart: edits.WeekStart, Drivers: []DriverPayDriver{costDriver}}
	if err = applyDriverPayCarry(ctx, tx, &refreshed); err != nil {
		return edits, err
	}
	edits.Costs = refreshed.Drivers[0].Edits.Costs
	return edits, tx.Commit(ctx)
}
