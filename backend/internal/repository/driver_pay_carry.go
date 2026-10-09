package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
)

// Costs are positive obligations; payroll amounts are signed deductions/credits.
// A read projects a balance but never records a collection.
type DriverPayCosts struct {
	Revision  string `json:"revision"`
	FuelCarry string `json:"fuelCarry"`
	TollCarry string `json:"tollCarry"`
	FuelDue   string `json:"fuelDue"`
	TollDue   string `json:"tollDue"`
}

type payCostRecord struct {
	Driver     string
	Week       string
	Fuel       string
	Toll       string
	FuelAmount string
	TollAmount string
}

func driverCostBase(d DriverPayDriver) (int64, int64) {
	if d.PayType != "gross_percentage" || !d.IsOwnerOperator {
		return 0, 0
	}
	f, _ := chargeCents(d.FuelTotal)
	t, _ := chargeCents(d.TollTotal)
	return f, t
}

func payrollCostAmount(override *string, due int64, cpm bool) int64 {
	if override != nil && !cpm {
		n, _ := chargeCents(*override)
		return n
	}
	return -due
}

// Source-backed payroll rows can reduce or defer collection, never reverse its
// direction or collect more than the authoritative amount currently available.
func validatePaySourceAmount(name, amount string, source int64) error {
	n, err := chargeCents(amount)
	if err != nil || n < min(0, source) || n > max(0, source) {
		return chargeInvalid("%s must stay between zero and %s, including carry; use a separate adjustment for a bonus or reimbursement", name, chargeMoney(source))
	}
	return nil
}

func readPayCostRecords(ctx context.Context, tx pgx.Tx) ([]payCostRecord, error) {
	memo := payrollReadMemo(ctx, tx)
	if memo != nil && memo.costsLoaded {
		return memo.costs, nil
	}
	rows, err := tx.Query(ctx, `SELECT driver_id::text,week_start::text,fuel_base::text,toll_base::text,fuel_amount::text,toll_amount::text FROM driver_pay_cost_collections ORDER BY week_start,driver_id`)
	if err != nil {
		return nil, err
	}
	out := []payCostRecord{}
	for rows.Next() {
		var r payCostRecord
		if err = rows.Scan(&r.Driver, &r.Week, &r.Fuel, &r.Toll, &r.FuelAmount, &r.TollAmount); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, r)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	// Existing overrides predate the collection ledger. Resolve their actual
	// routed source report (or frozen settlement), preserving historical amounts.
	rows, err = tx.Query(ctx, `SELECT w.driver_id::text,w.week_start::text FROM driver_pay_weeks w WHERE (w.fuel_override IS NOT NULL OR w.toll_override IS NOT NULL) AND NOT EXISTS(SELECT 1 FROM driver_pay_cost_collections c WHERE c.driver_id=w.driver_id AND c.week_start=w.week_start) ORDER BY w.week_start,w.driver_id`)
	if err != nil {
		return nil, err
	}
	legacy := map[string]map[string]bool{}
	for rows.Next() {
		var driver, week string
		if err = rows.Scan(&driver, &week); err != nil {
			rows.Close()
			return nil, err
		}
		if legacy[week] == nil {
			legacy[week] = map[string]bool{}
		}
		legacy[week][driver] = true
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for week, drivers := range legacy {
		w, _ := chargeWeek(week)
		report, e := readDriverPaySourceWeek(ctx, tx, w, "")
		if e != nil {
			return nil, e
		}
		report, _, e = routeTruckPay(ctx, tx, report)
		if e != nil {
			return nil, e
		}
		report, e = overlayPayrollSettlements(ctx, tx, report, "")
		if e != nil {
			return nil, e
		}
		for _, d := range report.Drivers {
			if !drivers[d.ID] {
				continue
			}
			f, t := driverCostBase(d)
			out = append(out, payCostRecord{d.ID, week, chargeMoney(f), chargeMoney(t), chargeMoney(payrollCostAmount(d.Edits.FuelOverride, f, d.PayType == "cpm")), chargeMoney(payrollCostAmount(d.Edits.TollOverride, t, d.PayType == "cpm"))})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Week == out[j].Week {
			return out[i].Driver < out[j].Driver
		}
		return out[i].Week < out[j].Week
	})
	if memo != nil {
		memo.costs = out
		memo.costsLoaded = true
	}
	return out, nil
}

func costBalance(records []payCostRecord, driver, week string) (int64, int64) {
	f, t := int64(0), int64(0)
	for _, r := range records {
		if r.Driver != driver || r.Week >= week {
			continue
		}
		fb, _ := chargeCents(r.Fuel)
		tb, _ := chargeCents(r.Toll)
		fa, _ := chargeCents(r.FuelAmount)
		ta, _ := chargeCents(r.TollAmount)
		f = max(0, f+fb+fa)
		t = max(0, t+tb+ta)
	}
	return f, t
}

func applyDriverPayCarry(ctx context.Context, tx pgx.Tx, report *DriverPayWeek) error {
	records, err := readPayCostRecords(ctx, tx)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, d := range report.Drivers {
		seen[d.ID] = true
	}
	for _, r := range records {
		if seen[r.Driver] {
			continue
		}
		f, t := costBalance(records, r.Driver, report.WeekStart)
		if f == 0 && t == 0 {
			continue
		}
		var d DriverPayDriver
		if err = tx.QueryRow(ctx, `SELECT d.id::text,d.full_name,d.pay_type,d.pay_rate::text,d.is_owner_operator,
 coalesce(t.id::text,''),coalesce(t.unit_number,''),coalesce(dp.id::text,''),coalesce(dp.full_name,historical_dispatcher.dispatcher_name,'Unassigned')
 FROM drivers d `+weeklyAssignmentJoins+` WHERE d.id=$2 AND d.active`, report.WeekStart, r.Driver).Scan(&d.ID, &d.FullName, &d.PayType, &d.PayRate, &d.IsOwnerOperator, &d.TruckID, &d.TruckUnit, &d.DispatcherID, &d.DispatcherName); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				seen[r.Driver] = true
				continue
			}
			return err
		}
		d.FuelTotal = "0.00"
		d.TollTotal = "0.00"
		d.Loads = []DriverPayLoad{}
		d.Edits = DriverPayEdits{DriverID: d.ID, WeekStart: report.WeekStart, Comments: map[string]string{}, Adjustments: []DriverPayAdjustment{}}
		report.Drivers = append(report.Drivers, d)
		seen[d.ID] = true
	}
	for i := range report.Drivers {
		d := &report.Drivers[i]
		f, t := costBalance(records, d.ID, report.WeekStart)
		baseF, baseT := driverCostBase(*d)
		state := DriverPayCosts{FuelCarry: chargeMoney(f), TollCarry: chargeMoney(t), FuelDue: chargeMoney(baseF + f), TollDue: chargeMoney(baseT + t)}
		relevant := []payCostRecord{}
		for _, r := range records {
			if r.Driver == d.ID {
				relevant = append(relevant, r)
			}
		}
		body, _ := json.Marshal(struct {
			State   DriverPayCosts
			Records []payCostRecord
		}{state, relevant})
		hash := sha256.Sum256(body)
		state.Revision = hex.EncodeToString(hash[:])
		d.Edits.Costs = &state
	}
	return nil
}

func saveDriverPayCosts(ctx context.Context, tx pgx.Tx, d DriverPayDriver, edits DriverPayEdits, actor string) error {
	state := d.Edits.Costs
	if state == nil {
		return ErrDriverPayConflict
	}
	if edits.Costs != nil && edits.Costs.Revision != state.Revision {
		return ErrDriverPayConflict
	}
	if edits.Costs == nil && (state.FuelCarry != "0.00" || state.TollCarry != "0.00") {
		return ErrDriverPayConflict
	}
	f, t := driverCostBase(d)
	fd, _ := chargeCents(state.FuelDue)
	td, _ := chargeCents(state.TollDue)
	// Old CPM clients cannot submit a cost override. Existing debt remains visible
	// and editable after a tariff change, without assigning new CPM fuel costs.
	fuelCPM := d.PayType == "cpm" && state.FuelCarry == "0.00"
	tollCPM := d.PayType == "cpm" && state.TollCarry == "0.00"
	if edits.FuelOverride != nil && !fuelCPM {
		if err := validatePaySourceAmount("Fuel", *edits.FuelOverride, -fd); err != nil {
			return err
		}
	}
	if edits.TollOverride != nil && !tollCPM {
		if err := validatePaySourceAmount("Toll", *edits.TollOverride, -td); err != nil {
			return err
		}
	}
	r := payCostRecord{d.ID, edits.WeekStart, chargeMoney(f), chargeMoney(t), chargeMoney(payrollCostAmount(edits.FuelOverride, fd, fuelCPM)), chargeMoney(payrollCostAmount(edits.TollOverride, td, tollCPM))}
	records, err := readPayCostRecords(ctx, tx)
	if err != nil {
		return err
	}
	unchanged := false
	later := false
	for _, old := range records {
		if old.Driver == d.ID {
			if old.Week == r.Week && old == r {
				unchanged = true
			}
			if old.Week > r.Week && (old.FuelAmount != "0.00" || old.TollAmount != "0.00") {
				later = true
			}
		}
	}
	if unchanged {
		var stored bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM driver_pay_cost_collections WHERE driver_id=$1 AND week_start=$2::date)`, r.Driver, r.Week).Scan(&stored); err != nil {
			return err
		}
		if stored {
			return nil
		}
	}
	if !unchanged && later && (f != 0 || t != 0 || fd != 0 || td != 0 || r.FuelAmount != "0.00" || r.TollAmount != "0.00") {
		return chargeInvalid("Set later saved Fuel/Toll deductions to zero before correcting this week, then reapply them")
	}
	// Pin pre-ledger sources when a new collection relies on them. Frozen legacy
	// settlements already supply immutable source/amount snapshots on reads.
	for _, old := range records {
		if old.Driver != d.ID || old.Week >= r.Week {
			continue
		}
		if _, err = tx.Exec(ctx, `INSERT INTO driver_pay_cost_collections(driver_id,week_start,fuel_base,toll_base,fuel_amount,toll_amount,updated_by)
 SELECT $1::uuid,$2::date,$3::numeric,$4::numeric,$5::numeric,$6::numeric,nullif($7,'')::uuid
 WHERE NOT EXISTS(SELECT 1 FROM payroll_settlements WHERE driver_id=$1::uuid AND week_start=$2::date AND finalized)
 AND NOT EXISTS(SELECT 1 FROM driver_pay_cost_collections WHERE driver_id=$1::uuid AND week_start=$2::date)
 ON CONFLICT DO NOTHING`, old.Driver, old.Week, old.Fuel, old.Toll, old.FuelAmount, old.TollAmount, actor); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(ctx, `INSERT INTO driver_pay_cost_collections(driver_id,week_start,fuel_base,toll_base,fuel_amount,toll_amount,updated_by) VALUES($1,$2::date,$3::numeric,$4::numeric,$5::numeric,$6::numeric,nullif($7,'')::uuid) ON CONFLICT(driver_id,week_start) DO UPDATE SET fuel_base=excluded.fuel_base,toll_base=excluded.toll_base,fuel_amount=excluded.fuel_amount,toll_amount=excluded.toll_amount,updated_by=excluded.updated_by,updated_at=now()`, r.Driver, r.Week, r.Fuel, r.Toll, r.FuelAmount, r.TollAmount, actor); err != nil {
		return err
	}
	return nil
}

func currentDriverPayCosts(ctx context.Context, tx pgx.Tx, edits DriverPayEdits) (DriverPayDriver, error) {
	w, _ := time.Parse(time.DateOnly, edits.WeekStart)
	report, err := readDriverPayWeek(ctx, tx, w, edits.DriverID)
	if err != nil {
		return DriverPayDriver{}, err
	}
	for _, d := range report.Drivers {
		if d.ID == edits.DriverID {
			return d, nil
		}
	}
	// Notes can be added before a driver has loads or costs.
	var d DriverPayDriver
	err = tx.QueryRow(ctx, `SELECT id::text,pay_type,pay_rate::text,is_owner_operator FROM drivers WHERE id=$1`, edits.DriverID).Scan(&d.ID, &d.PayType, &d.PayRate, &d.IsOwnerOperator)
	if err != nil {
		return d, err
	}
	report = DriverPayWeek{WeekStart: edits.WeekStart, Drivers: []DriverPayDriver{d}}
	err = applyDriverPayCarry(ctx, tx, &report)
	return report.Drivers[0], err
}
