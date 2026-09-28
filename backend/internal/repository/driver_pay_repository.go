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
	DriverID    string                `json:"driverId"`
	WeekStart   string                `json:"weekStart"`
	Notes       string                `json:"notes"`
	Comments    map[string]string     `json:"comments"`
	Adjustments []DriverPayAdjustment `json:"adjustments"`
	Version     int                   `json:"version"`
}
type DriverPayLoad struct {
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
	ID              string          `json:"id"`
	FullName        string          `json:"fullName"`
	TruckUnit       string          `json:"truckUnit"`
	DispatcherID    string          `json:"dispatcherId"`
	DispatcherName  string          `json:"dispatcherName"`
	IsOwnerOperator bool            `json:"isOwnerOperator"`
	PayType         string          `json:"payType"`
	PayRate         string          `json:"payRate"`
	Loads           []DriverPayLoad `json:"loads"`
	Edits           DriverPayEdits  `json:"edits"`
}
type DriverPayWeek struct {
	WeekStart string            `json:"weekStart"`
	Drivers   []DriverPayDriver `json:"drivers"`
}

// One statement provides a consistent snapshot across placement, source data,
// profile tariffs, and accounting edits. No load status or active-driver filter.
func (r *DriverPayRepository) Get(ctx context.Context, week time.Time) (DriverPayWeek, error) {
	result := DriverPayWeek{WeekStart: week.Format(time.DateOnly), Drivers: []DriverPayDriver{}}
	rows, err := r.pool.Query(ctx, `SELECT d.id,d.full_name,coalesce(t.unit_number,''),
 coalesce(dp.id::text,''),coalesce(dp.full_name,'Unassigned'),d.pay_type,d.pay_rate::text,d.is_owner_operator,
 e.service_date::text,e.slot,e.load_number,l.id,
 coalesce((coalesce(l.pickup_time,l.pickup_appointment_time) AT TIME ZONE 'UTC')::date::text,''),
 coalesce(l.total_pay::text,''),coalesce(e.driver_rate::text,''),coalesce(l.total_miles::text,''),l.raw_payload,
 coalesce(w.notes,''),coalesce(w.comments,'{}'::jsonb),coalesce(w.adjustments,'[]'::jsonb),coalesce(w.version,0)
 FROM `+grossBoardEntriesSQL+` e JOIN drivers d ON d.id=e.driver_id
 LEFT JOIN dispatchers dp ON dp.id=d.dispatcher_id
 LEFT JOIN truck_driver_assignments a ON a.driver_id=d.id AND a.unassigned_at IS NULL
 LEFT JOIN trucks t ON t.id=a.truck_id
 `+grossBoardResolvedLoad+`
 LEFT JOIN driver_pay_weeks w ON w.driver_id=d.id AND w.week_start=$1::date
 WHERE e.service_date >= $1::date AND e.service_date < $1::date+7
 AND NOT e.deleted AND btrim(e.load_number)<>''
 ORDER BY d.full_name,d.id,e.service_date,e.slot`, week)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	index := map[string]int{}
	for rows.Next() {
		var d DriverPayDriver
		var l DriverPayLoad
		var raw, comments, adjustments []byte
		if err := rows.Scan(&d.ID, &d.FullName, &d.TruckUnit, &d.DispatcherID, &d.DispatcherName, &d.PayType, &d.PayRate, &d.IsOwnerOperator,
			&l.Date, &l.Slot, &l.LoadNumber, &l.LoadRecordID, &l.PickupDate, &l.OriginalRate, &l.DriverGross, &l.TotalMiles, &raw,
			&d.Edits.Notes, &comments, &adjustments, &d.Edits.Version); err != nil {
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
		result.Drivers[i].Loads = append(result.Drivers[i].Loads, l)
	}
	return result, rows.Err()
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

func (r *DriverPayRepository) Save(ctx context.Context, edits DriverPayEdits) (DriverPayEdits, error) {
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
		err = r.pool.QueryRow(ctx, `INSERT INTO driver_pay_weeks(driver_id,week_start,notes,comments,adjustments)
 VALUES($1,$2::date,$3,$4,$5) ON CONFLICT DO NOTHING RETURNING version`, edits.DriverID, edits.WeekStart, edits.Notes, comments, adjustments).Scan(&version)
	} else {
		err = r.pool.QueryRow(ctx, `UPDATE driver_pay_weeks SET notes=$3,comments=$4,adjustments=$5,version=version+1,updated_at=now()
 WHERE driver_id=$1 AND week_start=$2::date AND version=$6 RETURNING version`, edits.DriverID, edits.WeekStart, edits.Notes, comments, adjustments, edits.Version).Scan(&version)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return edits, ErrDriverPayConflict
	}
	edits.Version = version
	return edits, err
}
