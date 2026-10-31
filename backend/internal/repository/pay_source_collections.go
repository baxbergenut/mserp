package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"mserp/internal/db"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
)

// Amounts are signed payroll values. Due includes only saved unpaid balances;
// browsing an intervening week never collects or duplicates a balance.
type PaySourceCharge struct {
	Source   string `json:"source"`
	OwnerID  string `json:"ownerId"`
	Name     string `json:"name"`
	Base     string `json:"base"`
	Carry    string `json:"carry"`
	Due      string `json:"due"`
	Amount   string `json:"amount"`
	Revision string `json:"revision"`
	Saved    bool   `json:"saved"`
}
type paySourceRecord struct {
	Source, Owner, Week, Kind, ID, Truck, Name, Base, Amount string
	Version                                                  int
	Statement                                                DriverPayDriver
}

func sourceKey(source, owner string) string { return source + "|" + owner }
func readPaySources(ctx context.Context, tx pgx.Tx) ([]paySourceRecord, error) {
	defer db.Phase(ctx, "source_ledger")()
	rows, err := tx.Query(ctx, `WITH relevant AS (
 SELECT *,CASE WHEN NOT $3::boolean AND week_start<nullif($2,'')::date THEN '' ELSE week_start::text END bucket
 FROM pay_source_collections WHERE accounting_live_date(week_start) AND (source NOT LIKE 'investor_cost:%' OR payroll_cost_in_scope(week_start,nullif($2,'')::date)) AND ($1::uuid[] IS NULL OR owner_id=ANY($1::uuid[]))
 ), grouped AS (SELECT *,sum(base) OVER(PARTITION BY source,owner_id,bucket) total_base,
 sum(amount) OVER(PARTITION BY source,owner_id,bucket) total_amount,
 sum(version) OVER(PARTITION BY source,owner_id,bucket) total_version FROM relevant), latest AS (
 SELECT DISTINCT ON(source,owner_id,bucket) * FROM grouped ORDER BY source,owner_id,bucket,week_start DESC)
 SELECT source,owner_id::text,bucket,kind,statement_id::text,truck_id::text,name,total_base::text,total_amount::text,total_version::integer,statement
 FROM latest ORDER BY bucket,source,owner_id`, payrollOwnerIDs(ctx), payrollContextWeek(ctx), payrollCostDetails(ctx))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []paySourceRecord{}
	for rows.Next() {
		var r paySourceRecord
		var body []byte
		if err = rows.Scan(&r.Source, &r.Owner, &r.Week, &r.Kind, &r.ID, &r.Truck, &r.Name, &r.Base, &r.Amount, &r.Version, &body); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(body, &r.Statement); err != nil {
			return nil, err
		}
		result = append(result, r)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	return readLegacyInvestorSources(ctx, tx, result)
}
func projectPaySource(source, owner, name, base, week string, records []paySourceRecord) PaySourceCharge {
	row := PaySourceCharge{Source: source, OwnerID: owner, Name: name, Base: base, Carry: "0.00"}
	var carry int64
	relevant := []paySourceRecord{}
	for _, r := range records {
		if r.Source != source || r.Owner != owner {
			continue
		}
		relevant = append(relevant, r)
		if r.Week < week {
			b, _ := chargeCents(r.Base)
			a, _ := chargeCents(r.Amount)
			carry += b - a
		}
		if r.Week == week {
			row.Saved = true
			row.Amount = r.Amount
		}
	}
	b, _ := chargeCents(base)
	row.Carry = chargeMoney(carry)
	row.Due = chargeMoney(b + carry)
	if !row.Saved {
		row.Amount = row.Due
	}
	body, _ := json.Marshal(struct {
		Row     PaySourceCharge
		Records []paySourceRecord
	}{row, relevant})
	hash := sha256.Sum256(body)
	row.Revision = hex.EncodeToString(hash[:])
	return row
}

func routeTruckPay(ctx context.Context, tx pgx.Tx, source DriverPayWeek) (DriverPayWeek, DriverPayWeek, error) {
	drivers, investors, err := routeTruckPaySources(ctx, tx, source)
	if err != nil {
		return drivers, investors, err
	}
	if err = routeCorrectedInvestorExpenses(ctx, tx, &investors); err != nil {
		return drivers, investors, err
	}
	err = applyPaySources(ctx, tx, &drivers, &investors)
	return drivers, investors, err
}

func applyPaySources(ctx context.Context, tx pgx.Tx, drivers, investors *DriverPayWeek) error {
	records, err := readPaySources(ctx, tx)
	if err != nil {
		return err
	}
	week := drivers.WeekStart
	vacation, err := readVacationWeek(ctx, tx, week)
	if err != nil {
		return err
	}
	// One explicit owner identity per driver, including owners with no current truck.
	owners := map[string]string{}
	activeDrivers := map[string]bool{}
	rows, err := tx.Query(ctx, `SELECT i.driver_id::text,i.id::text,d.active FROM investors i JOIN drivers d ON d.id=i.driver_id`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var d, o string
		var active bool
		if err = rows.Scan(&d, &o, &active); err != nil {
			rows.Close()
			return err
		}
		owners[d] = o
		activeDrivers[d] = active
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	found := map[string]bool{}
	reports := []*DriverPayWeek{drivers, investors}
	for k, report := range reports {
		kind := []string{"driver", "investor"}[k]
		for i := range report.Drivers {
			d := &report.Drivers[i]
			if d.Settlement != nil && d.Settlement.Finalized {
				continue
			}
			d.Edits.SourceCharges = nil
			owner := d.InvestorID
			if kind == "driver" {
				owner = owners[d.ID]
			}
			if owner == "" {
				continue
			}
			if d.Settlement != nil && d.Settlement.Finalized {
				continue
			}
			for _, a := range d.AutoCharges {
				if !strings.HasPrefix(a.Source, "truck_charge:") {
					continue
				}
				row := projectPaySource(a.Source, owner, a.Name, a.Amount, week, records)
				d.Edits.SourceCharges = append(d.Edits.SourceCharges, row)
				found[sourceKey(a.Source, owner)] = true
			}
			if kind == "investor" {
				for _, cost := range []struct{ name, base string }{{"Fuel", d.FuelTotal}, {"Toll", d.TollTotal}} {
					n, _ := chargeCents(cost.base)
					s := "investor_cost:" + d.ID + ":" + strings.ToLower(cost.name)
					row := projectPaySource(s, owner, cost.name, chargeMoney(-n), week, records)
					// Preserve a pre-ledger explicit override on its original week.
					override := d.Edits.FuelOverride
					if cost.name == "Toll" {
						override = d.Edits.TollOverride
					}
					if !row.Saved && override != nil {
						row.Amount = *override
					}
					d.Edits.SourceCharges = append(d.Edits.SourceCharges, row)
					found[sourceKey(s, owner)] = true
				}
			}
		}
	}
	// Pausing/excluding a fee or moving between owner-driver and investor routing
	// must not hide saved debt. A new owner never inherits the previous owner's debt.
	latest := map[string]paySourceRecord{}
	for _, r := range records {
		if r.Week <= week {
			latest[sourceKey(r.Source, r.Owner)] = r
		}
	}
	keys := []string{}
	for key := range latest {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		r := latest[key]
		// Leave suspends collection as well as generation. Keep the ledger intact
		// for return, but never resurrect a paused fee from a saved source or carry.
		if strings.HasPrefix(r.Source, "truck_charge:") && vacation.trucks[r.Truck] {
			continue
		}
		if _, ok := found[key]; ok {
			continue
		}
		base := "0.00"
		if r.Week == week {
			base = r.Base
		}
		row := projectPaySource(r.Source, r.Owner, r.Name, base, week, records)
		if row.Due == "0.00" && !row.Saved {
			continue
		}
		report := drivers
		if r.Kind == "investor" {
			report = investors
		}
		index := -1
		// Prefer this owner's current statement for the same truck.
		for k, p := range reports {
			for i, d := range p.Drivers {
				o := d.InvestorID
				if k == 0 {
					o = owners[d.ID]
				}
				if d.TruckID == r.Truck && o == r.Owner {
					report = p
					index = i
				}
			}
		}
		if index < 0 {
			for i, d := range report.Drivers {
				if d.ID == r.ID {
					if r.Kind == "investor" && d.InvestorID != r.Owner {
						d.Issues = append(d.Issues, "An unpaid balance belongs to the previous truck owner; reconcile their earlier statement")
						report.Drivers[i] = d
						index = -2
						break
					}
					index = i
					break
				}
			}
		}
		if index == -2 {
			continue
		}
		if index < 0 {
			if r.Kind == "driver" && !activeDrivers[r.ID] {
				continue
			}
			d := r.Statement
			d.Loads = []DriverPayLoad{}
			d.AutoCharges = nil
			d.Issues = nil
			d.Settlement = nil
			d.Layovers = nil
			d.Summary = nil
			d.FuelTotal = "0.00"
			d.TollTotal = "0.00"
			d.Edits = DriverPayEdits{DriverID: d.ID, WeekStart: week, Comments: map[string]string{}, Adjustments: []DriverPayAdjustment{}}
			if r.Kind == "investor" {
				if err = readInvestorEdits(ctx, tx, &d); err != nil {
					return err
				}
			}
			report.Drivers = append(report.Drivers, d)
			index = len(report.Drivers) - 1
		}
		d := &report.Drivers[index]
		if d.Settlement != nil && d.Settlement.Finalized {
			continue
		}
		if report == drivers && strings.HasPrefix(row.Source, "investor_cost:") && !strings.Contains(row.Name, "truck balance") {
			row.Name += " · truck balance"
		}
		d.Edits.SourceCharges = append(d.Edits.SourceCharges, row)
		if strings.HasPrefix(r.Source, "truck_charge:") || report == drivers {
			d.AutoCharges = append(d.AutoCharges, PayAutoCharge{Name: r.Name, Amount: row.Amount, Source: r.Source})
		}
	}
	for k, report := range reports {
		for i := range report.Drivers {
			d := &report.Drivers[i]
			if d.Settlement != nil && d.Settlement.Finalized {
				continue
			}
			for j, a := range d.AutoCharges {
				for _, row := range d.Edits.SourceCharges {
					if row.Source == a.Source {
						d.AutoCharges[j].Amount = row.Amount
					}
				}
			}
			if k == 1 {
				setInvestorSourceCosts(d)
			}
		}
	}
	return nil
}

func setInvestorSourceCosts(d *DriverPayDriver) {
	state := &DriverPayCosts{FuelCarry: "0.00", TollCarry: "0.00", FuelDue: d.FuelTotal, TollDue: d.TollTotal}
	for _, row := range d.Edits.SourceCharges {
		if !strings.HasPrefix(row.Source, "investor_cost:") {
			continue
		}
		carry, _ := chargeCents(row.Carry)
		due, _ := chargeCents(row.Due)
		amount := row.Amount
		state.Revision += row.Revision
		if strings.HasSuffix(row.Source, ":fuel") {
			state.FuelCarry = chargeMoney(-carry)
			state.FuelDue = chargeMoney(-due)
			if row.Saved {
				d.Edits.FuelOverride = &amount
			}
		}
		if strings.HasSuffix(row.Source, ":toll") {
			state.TollCarry = chargeMoney(-carry)
			state.TollDue = chargeMoney(-due)
			if row.Saved {
				d.Edits.TollOverride = &amount
			}
		}
	}
	d.Edits.Costs = state
}

func savePaySources(ctx context.Context, tx pgx.Tx, d DriverPayDriver, edits *DriverPayEdits, kind, actor string) error {
	ctx = payrollWeekContext(ctx, edits.WeekStart)
	defer db.Phase(ctx, "source_validation_write")()
	if len(d.Edits.SourceCharges) == 0 && len(edits.SourceCharges) == 0 {
		edits.SourceCharges = []PaySourceCharge{}
		if kind == "investor" {
			d.Edits = *edits
			setInvestorSourceCosts(&d)
			edits.Costs = d.Edits.Costs
		}
		return nil
	}
	inputs, _ := ctx.Value(payrollFinalizeKey{}).(*payrollFinalizeInputs)
	var records []paySourceRecord
	var err error
	if inputs != nil {
		records = inputs.sources
	} else {
		records, err = readPaySources(ctx, tx)
		if err != nil {
			return err
		}
	}
	supplied := map[string]PaySourceCharge{}
	for _, row := range edits.SourceCharges {
		key := sourceKey(row.Source, row.OwnerID)
		if _, ok := supplied[key]; ok {
			return chargeInvalid("Duplicate source charge")
		}
		supplied[key] = row
	}
	result := []PaySourceCharge{}
	for _, row := range d.Edits.SourceCharges {
		key := sourceKey(row.Source, row.OwnerID)
		input, ok := supplied[key]
		delete(supplied, key)
		if ok && input.Revision != row.Revision {
			return ErrDriverPayConflict
		}
		if !ok && row.Carry != "0.00" {
			return ErrDriverPayConflict
		}
		amount := row.Amount
		if ok {
			amount = input.Amount
		}
		if kind == "investor" && strings.HasPrefix(row.Source, "investor_cost:") {
			override := edits.FuelOverride
			if strings.HasSuffix(row.Source, ":toll") {
				override = edits.TollOverride
			}
			amount = row.Due
			if override != nil {
				amount = *override
			}
			if edits.Costs != nil && d.Edits.Costs != nil && edits.Costs.Revision != d.Edits.Costs.Revision {
				return ErrDriverPayConflict
			}
		}
		due, _ := chargeCents(row.Due)
		if err = validatePaySourceAmount(row.Name, amount, due); err != nil {
			return err
		}
		value, _ := chargeCents(amount)
		amount = chargeMoney(value)
		unchanged, later := false, false
		for _, old := range records {
			if old.Source != row.Source || old.Owner != row.OwnerID {
				continue
			}
			if old.Week == edits.WeekStart {
				if old.Amount != "0.00" && (old.Kind != kind || old.ID != d.ID) {
					return chargeInvalid("This charge was already saved on another statement; set that collection to zero before changing its destination")
				}
				unchanged = old.Base == row.Base && old.Amount == amount && old.Kind == kind && old.ID == d.ID
			}
			if old.Week > edits.WeekStart && old.Amount != "0.00" {
				later = true
			}
		}
		if unchanged {
			result = append(result, row)
			continue
		}
		if !row.Saved && row.Base == "0.00" && row.Due == "0.00" && amount == "0.00" {
			result = append(result, row)
			continue
		}
		if later {
			return chargeInvalid("Set later saved %s deductions to zero before correcting this week, then reapply them", row.Name)
		}
		truck := d.TruckID
		if strings.HasPrefix(row.Source, "truck_charge:") || strings.HasPrefix(row.Source, "investor_cost:") {
			truck = strings.Split(row.Source, ":")[1]
		}
		// Frozen future statements predate this ledger too; do not create new debt
		// behind a finalized collection or silently reroute a frozen historical row.
		var closed bool
		var checked bool
		if inputs != nil {
			closed, checked = inputs.closed[row.Source+"|"+truck]
		}
		if !checked {
			err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM investor_pay_weeks WHERE truck_id=$1 AND week_start>=$2::date AND finalized)
   OR EXISTS(SELECT 1 FROM payroll_settlements p WHERE p.week_start>=$2::date AND p.finalized AND EXISTS(SELECT 1 FROM jsonb_array_elements(coalesce(p.report->'autoCharges','[]')) a WHERE a->>'source'=$3))`, truck, edits.WeekStart, row.Source).Scan(&closed)
		}
		if err != nil {
			return err
		}
		if closed {
			return chargeInvalid("Reopen affected finalized statements before correcting %s", row.Name)
		}
		snapshot := DriverPayDriver{ID: d.ID, FullName: d.FullName, TruckID: truck, TruckUnit: d.TruckUnit, InvestorID: d.InvestorID, ProfileDriverID: d.ProfileDriverID, IsOwnerOperator: d.IsOwnerOperator, EmploymentType: d.EmploymentType, PayType: d.PayType, PayRate: d.PayRate, DispatcherID: d.DispatcherID, DispatcherName: d.DispatcherName}
		body, _ := json.Marshal(snapshot)
		// Pin any pre-ledger override whose unpaid principal this save relies on.
		for _, old := range records {
			if old.Version != 0 || old.Source != row.Source || old.Owner != row.OwnerID || old.Week >= edits.WeekStart {
				continue
			}
			legacyBody, _ := json.Marshal(old.Statement)
			_, err = tx.Exec(ctx, `INSERT INTO pay_source_collections(source,owner_id,week_start,kind,statement_id,truck_id,name,base,amount,statement,updated_by)
			 SELECT $1,$2,$3::date,$4,$5,$6,$7,$8::numeric,$9::numeric,$10,nullif($11,'')::uuid
			 WHERE NOT EXISTS(SELECT 1 FROM investor_pay_weeks WHERE truck_id=$5 AND week_start=$3::date AND finalized)
			 ON CONFLICT DO NOTHING`, old.Source, old.Owner, old.Week, old.Kind, old.ID, old.Truck, old.Name, old.Base, old.Amount, legacyBody, actor)
			if err != nil {
				return err
			}
		}
		_, err = tx.Exec(ctx, `INSERT INTO pay_source_collections(source,owner_id,week_start,kind,statement_id,truck_id,name,base,amount,statement,updated_by)
   VALUES($1,$2,$3::date,$4,$5,$6,$7,$8::numeric,$9::numeric,$10,nullif($11,'')::uuid)
   ON CONFLICT(source,owner_id,week_start) DO UPDATE SET kind=excluded.kind,statement_id=excluded.statement_id,name=excluded.name,base=excluded.base,amount=excluded.amount,statement=excluded.statement,version=pay_source_collections.version+1,updated_by=excluded.updated_by,updated_at=now()`, row.Source, row.OwnerID, edits.WeekStart, kind, d.ID, truck, row.Name, row.Base, amount, body, actor)
		if err != nil {
			return err
		}
		if err = truckAudit(ctx, tx, truck, edits.WeekStart, "source_collection", actor, map[string]any{"source": row.Source, "ownerId": row.OwnerID, "base": row.Base, "amount": amount, "kind": kind, "statementId": d.ID}); err != nil {
			return err
		}
		row.Amount = amount
		result = append(result, row)
	}
	if len(supplied) > 0 {
		return chargeInvalid("This charge does not belong to this statement; reload payroll")
	}
	if inputs != nil {
		edits.SourceCharges = result
		return nil
	}
	records, err = readPaySources(ctx, tx)
	if err != nil {
		return err
	}
	for i, row := range result {
		result[i] = projectPaySource(row.Source, row.OwnerID, row.Name, row.Base, edits.WeekStart, records)
	}
	edits.SourceCharges = result
	if kind == "investor" {
		d.Edits = *edits
		setInvestorSourceCosts(&d)
		edits.Costs = d.Edits.Costs
	}
	return nil
}
