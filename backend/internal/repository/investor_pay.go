package repository

import (
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type PayAutoCharge struct {
	Name   string `json:"name"`
	Amount string `json:"amount"`
	Source string `json:"source"`
}
type truckPayGroup struct {
	term        TruckTerm
	card        DriverPayDriver
	ownerDriver string
	drivers     map[string]bool
	ownerOnly   bool
}

func applicableTruckTerms(data TruckChargeData, week string) map[string]TruckTerm {
	result := map[string]TruckTerm{}
	for _, t := range data.Terms {
		if t.WeekStart <= week {
			old, ok := result[t.TruckID]
			if !ok || t.WeekStart > old.WeekStart {
				result[t.TruckID] = t
			}
		}
	}
	return result
}
func truckRecurringCharges(data TruckChargeData, types []ChargeType, truck, week string, hasLoads bool) []PayAutoCharge {
	latest := map[string]TruckChargePhase{}
	for _, p := range data.Phases {
		if p.TruckID == truck && p.WeekStart <= week {
			old, ok := latest[p.TypeID]
			if !ok || old.WeekStart < p.WeekStart {
				latest[p.TypeID] = p
			}
		}
	}
	result := []PayAutoCharge{}
	for _, t := range types {
		p, ok := latest[t.ID]
		if !ok || !p.Included {
			continue
		}
		eligibility := t.Eligibility
		if len(t.Rules) > 0 {
			eligibility = t.Rules[0].Eligibility
		}
		for _, r := range t.Rules {
			if r.WeekStart <= week {
				eligibility = r.Eligibility
			}
		}
		if eligibility == "loads" && !hasLoads || eligibility == "no_loads" && hasLoads {
			continue
		}
		n, _ := chargeCents(p.Amount)
		if t.Direction == "charge" {
			n = -n
		}
		result = append(result, PayAutoCharge{t.Name, chargeMoney(n), "truck_charge:" + truck + ":" + t.ID})
	}
	return result
}

// Source unit is authoritative for matched loads. Unmatched plans require a
// unique assignment on the board date. Never use today's driver assignment.
func truckPayLoadMap(ctx context.Context, tx pgx.Tx, week string) (map[string]string, error) {
	rows, err := tx.Query(ctx, `SELECT e.driver_id::text,e.service_date::text,e.slot,lower(btrim(e.load_number)),coalesce(m.truck_id::text,'')
 FROM `+grossBoardEntriesSQL+` e `+grossBoardResolvedLoad+`
 LEFT JOIN LATERAL (SELECT (array_agg(DISTINCT t.id))[1] truck_id FROM trucks t WHERE
 (l.id IS NOT NULL AND upper(btrim(t.unit_number))=upper(btrim(l.truck_unit))) OR
 (l.id IS NULL AND EXISTS(SELECT 1 FROM truck_driver_assignments a WHERE a.truck_id=t.id AND a.driver_id=e.driver_id
 AND (a.assigned_at AT TIME ZONE 'America/New_York')::date<=e.service_date
 AND (a.unassigned_at IS NULL OR e.service_date<(a.unassigned_at AT TIME ZONE 'America/New_York')::date))) HAVING count(DISTINCT t.id)=1) m ON true
 WHERE NOT e.deleted AND e.day_status='' AND btrim(e.load_number)<>'' AND e.service_date>=$1::date AND e.service_date<$1::date+7`, week)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string]string{}
	for rows.Next() {
		var driver, date, number, truck string
		var slot int
		if err = rows.Scan(&driver, &date, &slot, &number, &truck); err != nil {
			return nil, err
		}
		result[driver+":"+payCommentKey(date, slot, number)] = truck
	}
	return result, rows.Err()
}
func payCommentKey(date string, slot int, number string) string { // same stable identity as Driver Pay
	return date + ":" + strconv.Itoa(slot) + ":" + strings.ToLower(strings.TrimSpace(number))
}

func routeTruckPay(ctx context.Context, tx pgx.Tx, source DriverPayWeek) (DriverPayWeek, DriverPayWeek, error) {
	investor := DriverPayWeek{WeekStart: source.WeekStart, Drivers: []DriverPayDriver{}}
	config, err := truckChargeData(ctx, tx)
	if err != nil {
		return source, investor, err
	}
	terms := applicableTruckTerms(config, source.WeekStart)
	if len(terms) == 0 {
		return source, investor, nil
	}
	charges, _, err := chargeData(ctx, tx, "")
	if err != nil {
		return source, investor, err
	}
	loadMap, err := truckPayLoadMap(ctx, tx, source.WeekStart)
	if err != nil {
		return source, investor, err
	}
	costs, err := readTruckCostAllocations(ctx, tx, source.WeekStart)
	if err != nil {
		return source, investor, err
	}
	groups := map[string]*truckPayGroup{}
	frozenDrivers, err := frozenDriverPay(ctx, tx, source.WeekStart)
	if err != nil {
		return source, investor, err
	}
	currentLoads := map[string]bool{}
	for _, d := range source.Drivers {
		for _, l := range d.Loads {
			currentLoads[d.ID+":"+l.CommentKey] = true
		}
	}
	for id, d := range frozenDrivers {
		for _, l := range d.Loads {
			if !currentLoads[id+":"+l.CommentKey] {
				investor.Issues = append(investor.Issues, "A load in "+d.FullName+"'s finalized paycheck is absent from Gross Board; reconcile the source before finalizing Investor Pay")
			}
		}
	}
	for id, term := range terms {
		g := &truckPayGroup{term: term, drivers: map[string]bool{}}
		err = tx.QueryRow(ctx, `SELECT t.unit_number,coalesce(d.full_name,i.full_name),coalesce(i.driver_id::text,'') FROM trucks t JOIN investors i ON i.id=$2 LEFT JOIN drivers d ON d.id=i.driver_id WHERE t.id=$1`, id, term.OwnerID).Scan(&g.card.TruckUnit, &g.card.FullName, &g.ownerDriver)
		if err != nil {
			return source, investor, err
		}
		g.card.ID = id
		g.card.TruckID = id
		g.card.InvestorID = term.OwnerID
		g.card.ProfileDriverID = g.ownerDriver
		g.card.IsOwnerOperator = true
		g.card.PayType = "gross_percentage"
		g.card.PayRate = term.SharePercent
		g.card.Loads = []DriverPayLoad{}
		g.card.FuelTotal = "0.00"
		g.card.TollTotal = "0.00"
		g.card.Edits = DriverPayEdits{DriverID: id, WeekStart: source.WeekStart, Comments: map[string]string{}, Adjustments: []DriverPayAdjustment{}}
		var ownershipConflict bool
		err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM truck_ownership_history WHERE truck_id=$1 AND owner_id<>$2 AND (assigned_at AT TIME ZONE 'America/New_York')::date<$3::date+7 AND (unassigned_at IS NULL OR (unassigned_at AT TIME ZONE 'America/New_York')::date>$3::date))`, id, term.OwnerID, source.WeekStart).Scan(&ownershipConflict)
		if err != nil {
			return source, investor, err
		}
		if ownershipConflict {
			g.card.Issues = append(g.card.Issues, "Truck ownership changed during or before this week; review the dated investor agreement")
		}
		groups[id] = g
	}
	// Build the complete truck week before choosing its destination. A mixed week
	// uses one investor statement and deducts hired labor only.
	for _, d := range source.Drivers {
		for _, l := range d.Loads {
			if loadMap[d.ID+":"+l.CommentKey] == "" {
				investor.Issues = append(investor.Issues, "Cannot attribute load "+l.LoadNumber+" ("+d.FullName+") to a unique historical truck. Correct its source truck or assignment before finalizing Investor Pay.")
			}
			g := groups[loadMap[d.ID+":"+l.CommentKey]]
			if g == nil {
				continue
			}
			g.drivers[d.ID] = true
			l.SourceDriverID = d.ID
			l.DriverFee = l.Fee
			if frozen, ok := frozenDrivers[d.ID]; ok {
				matched := false
				for _, f := range frozen.Loads {
					if f.CommentKey == l.CommentKey {
						l.DriverFee = f.Fee
						matched = true
						break
					}
				}
				if !matched {
					l.Issues = append(l.Issues, "Load differs from the finalized driver paycheck; review driver settlement")
				}
			}
			if d.ID == g.ownerDriver {
				l.DriverFee = "0.00"
			}
			l.Fee = driverPayFee("gross_percentage", g.term.SharePercent, l.DriverGross)
			l.CommentKey = l.CommentKey + ":" + d.ID
			if l.Fee == "" {
				l.Issues = append(l.Issues, "Investor share requires Gross Board gross")
			}
			if len(g.card.Loads) == 0 {
				g.card.DispatcherID = d.DispatcherID
				g.card.DispatcherName = d.DispatcherName
			} else if g.card.DispatcherID != d.DispatcherID {
				g.card.DispatcherID = "mixed"
				g.card.DispatcherName = "Multiple dispatchers"
			}
			g.card.Loads = append(g.card.Loads, l)
		}
	}
	for id, g := range groups {
		g.ownerOnly = len(g.drivers) == 1 && g.drivers[g.ownerDriver]
		if len(g.drivers) == 0 && g.ownerDriver != "" {
			// No-load weeks belong to the owner-operator only with a unique, full-week
			// historical assignment. Unassigned trucks remain visible in Investor Pay.
			err = tx.QueryRow(ctx, `SELECT count(*)=1 AND bool_and(driver_id::text=$2) FROM truck_driver_assignments WHERE truck_id=$1 AND (assigned_at AT TIME ZONE 'America/New_York')::date<=$3::date AND (unassigned_at IS NULL OR (unassigned_at AT TIME ZONE 'America/New_York')::date>=$3::date+7)`, id, g.ownerDriver, source.WeekStart).Scan(&g.ownerOnly)
			if err != nil {
				return source, investor, err
			}
		}
		var savedInvestor bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM investor_pay_weeks WHERE truck_id=$1 AND week_start=$2::date)`, id, source.WeekStart).Scan(&savedInvestor); err != nil {
			return source, investor, err
		}
		if savedInvestor {
			g.ownerOnly = false
		}
		if !g.ownerOnly && g.drivers[g.ownerDriver] {
			if _, ok := frozenDrivers[g.ownerDriver]; ok {
				g.card.Issues = append(g.card.Issues, "Owner driving earnings already appear in a finalized Driver Pay statement; reopen and reconcile before settling this truck")
			}
		}
		g.card.AutoCharges = truckRecurringCharges(config, charges.Types, id, source.WeekStart, len(g.card.Loads) > 0)
		seenLoads := map[int]bool{}
		for _, l := range g.card.Loads {
			if l.LoadRecordID != nil {
				if seenLoads[*l.LoadRecordID] {
					g.card.Issues = append(g.card.Issues, "A system load is placed more than once on this truck; resolve duplicate Gross Board entries")
				}
				seenLoads[*l.LoadRecordID] = true
			}
		}
		for _, d := range source.Drivers {
			var total int64
			for _, l := range g.card.Loads {
				if l.SourceDriverID == d.ID && d.ID != g.ownerDriver {
					n, e := chargeCents(l.DriverFee)
					if e != nil {
						g.card.Issues = append(g.card.Issues, "Driver earnings are incomplete for "+d.FullName)
					}
					total += n
				}
			}
			if g.drivers[d.ID] && d.ID != g.ownerDriver {
				g.card.AutoCharges = append(g.card.AutoCharges, PayAutoCharge{"Driver earnings · " + d.FullName, chargeMoney(-total), "driver:" + d.ID})
			}
		}
		if c := costs[id]; c != nil {
			g.card.FuelTotal = chargeMoney(c.fuel)
			g.card.TollTotal = chargeMoney(c.toll)
			if c.unlinkedFuel {
				g.card.Issues = append(g.card.Issues, "Resolve unlinked Relay fuel identities for this truck")
			}
			if !g.ownerOnly {
				for driver, frozen := range frozenDrivers {
					if driver == g.ownerDriver {
						continue
					}
					fuel, toll := int64(0), int64(0)
					if frozen.IsOwnerOperator && frozen.PayType == "gross_percentage" {
						fuel, _ = chargeCents(frozen.FuelTotal)
						toll, _ = chargeCents(frozen.TollTotal)
					}
					if frozen.Edits.FuelOverride != nil {
						n, _ := chargeCents(*frozen.Edits.FuelOverride)
						fuel = -n
					}
					if frozen.Edits.TollOverride != nil {
						n, _ := chargeCents(*frozen.Edits.TollOverride)
						toll = -n
					}
					if c.driverFuel[driver] != 0 && fuel > 0 || c.driverToll[driver] != 0 && toll > 0 {
						g.card.Issues = append(g.card.Issues, "Truck costs also appear in "+frozen.FullName+"'s finalized paycheck; reopen and reconcile that paycheck first")
					}
				}
			}
		}
		g.card.Edits.ExpenseDeductions, err = investorExpenseDeductions(ctx, tx, id, g.term.OwnerID, source.WeekStart, g.ownerOnly)
		if err != nil {
			return source, investor, err
		}
		if !g.ownerOnly {
			if err = readInvestorEdits(ctx, tx, &g.card); err != nil {
				return source, investor, err
			}
			investor.Drivers = append(investor.Drivers, g.card)
		}
	}
	// Remove only truck-responsible deductions from the driver's statement.
	for i := range source.Drivers {
		d := &source.Drivers[i]
		kept := []DriverPayLoad{}
		for _, l := range d.Loads {
			g := groups[loadMap[d.ID+":"+l.CommentKey]]
			if g != nil && d.ID == g.ownerDriver && !g.ownerOnly {
				continue
			}
			kept = append(kept, l)
		}
		d.Loads = kept
		filtered := []ExpenseDeduction{}
		for _, e := range d.Edits.ExpenseDeductions {
			routed := false
			for _, g := range groups {
				if !g.ownerOnly {
					for _, x := range g.card.Edits.ExpenseDeductions {
						if x.ExpenseID == e.ExpenseID {
							routed = true
						}
					}
				}
			}
			if !routed {
				filtered = append(filtered, e)
			}
		}
		d.Edits.ExpenseDeductions = filtered
		for _, g := range groups {
			if g.ownerOnly && g.ownerDriver == d.ID {
				d.AutoCharges = append(d.AutoCharges, g.card.AutoCharges...)
				d.Issues = append(d.Issues, g.card.Issues...)
			}
		}
		sort.Slice(d.AutoCharges, func(i, j int) bool { return d.AutoCharges[i].Source < d.AutoCharges[j].Source })
	}
	// Costs allocated to an investor truck are removed by transaction source,
	// not by subtracting an entire driver's week after a truck change.
	routeTruckDriverCosts(&source, groups, costs)
	for _, g := range groups {
		if !g.ownerOnly {
			continue
		}
		found := false
		for i := range source.Drivers {
			if source.Drivers[i].ID == g.ownerDriver {
				found = true
			}
		}
		if !found && (len(g.card.AutoCharges) > 0 || g.card.FuelTotal != "0.00" || g.card.TollTotal != "0.00" || len(g.card.Edits.ExpenseDeductions) > 0) {
			var d DriverPayDriver
			err = tx.QueryRow(ctx, `SELECT id::text,full_name,pay_type,pay_rate::text,is_owner_operator FROM drivers WHERE id=$1`, g.ownerDriver).Scan(&d.ID, &d.FullName, &d.PayType, &d.PayRate, &d.IsOwnerOperator)
			if err != nil {
				return source, investor, err
			}
			d.TruckUnit = g.card.TruckUnit
			d.TruckID = g.card.TruckID
			d.Loads = []DriverPayLoad{}
			d.Edits = DriverPayEdits{DriverID: d.ID, WeekStart: source.WeekStart, Comments: map[string]string{}, Adjustments: []DriverPayAdjustment{}}
			d.AutoCharges = g.card.AutoCharges
			d.Edits.ExpenseDeductions = g.card.Edits.ExpenseDeductions
			d.Issues = g.card.Issues
			d.FuelTotal = g.card.FuelTotal
			d.TollTotal = g.card.TollTotal
			source.Drivers = append(source.Drivers, d)
		}
	}
	sort.Slice(investor.Drivers, func(i, j int) bool {
		a, b := investor.Drivers[i], investor.Drivers[j]
		if a.FullName != b.FullName {
			return a.FullName < b.FullName
		}
		if a.InvestorID != b.InvestorID {
			return a.InvestorID < b.InvestorID
		}
		return a.TruckUnit < b.TruckUnit
	})
	keptDrivers := []DriverPayDriver{}
	for _, d := range source.Drivers {
		if len(d.Loads) > 0 || d.Edits.Version > 0 || len(d.Edits.GeneratedCharges) > 0 || len(d.Edits.ExpenseDeductions) > 0 || len(d.AutoCharges) > 0 || len(d.Issues) > 0 || (d.IsOwnerOperator && d.PayType == "gross_percentage" && (d.FuelTotal != "0.00" || d.TollTotal != "0.00")) {
			keptDrivers = append(keptDrivers, d)
		}
	}
	source.Drivers = keptDrivers
	return source, investor, nil
}

func frozenDriverPay(ctx context.Context, tx pgx.Tx, week string) (map[string]DriverPayDriver, error) {
	rows, err := tx.Query(ctx, `SELECT report FROM payroll_settlements WHERE week_start=$1::date AND finalized`, week)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string]DriverPayDriver{}
	for rows.Next() {
		var body []byte
		if err = rows.Scan(&body); err != nil {
			return nil, err
		}
		var d DriverPayDriver
		if err = json.Unmarshal(body, &d); err != nil {
			return nil, err
		}
		result[d.ID] = d
	}
	return result, rows.Err()
}

func readInvestorEdits(ctx context.Context, tx pgx.Tx, d *DriverPayDriver) error {
	var body, frozen []byte
	var finalized bool
	var meta PayrollSettlement
	err := tx.QueryRow(ctx, `SELECT w.edits,w.version,w.finalized,w.report,coalesce(w.finalized_at,now()),coalesce(u.username,'') FROM investor_pay_weeks w LEFT JOIN app_users u ON u.id=w.finalized_by WHERE w.truck_id=$1 AND w.week_start=$2::date`, d.ID, d.Edits.WeekStart).Scan(&body, &meta.Version, &finalized, &frozen, &meta.FinalizedAt, &meta.FinalizedBy)
	if err == pgx.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	if finalized {
		if err = json.Unmarshal(frozen, d); err != nil {
			return err
		}
		meta.Finalized = true
		d.Settlement = &meta
		return nil
	}
	expenses := d.Edits.ExpenseDeductions
	if err = json.Unmarshal(body, &d.Edits); err != nil {
		return err
	}
	d.Edits.Version = meta.Version
	d.Edits.ExpenseDeductions = expenses
	return nil
}
func (r *DriverPayRepository) InvestorPay(ctx context.Context, week time.Time) (DriverPayWeek, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return DriverPayWeek{}, err
	}
	defer tx.Rollback(ctx)
	result, err := readInvestorPay(ctx, tx, week)
	if err != nil {
		return result, err
	}
	return result, tx.Commit(ctx)
}
func readInvestorPay(ctx context.Context, tx pgx.Tx, week time.Time) (DriverPayWeek, error) {
	source, err := readDriverPaySourceWeek(ctx, tx, week, "")
	if err != nil {
		return source, err
	}
	_, result, err := routeTruckPay(ctx, tx, source)
	if err != nil {
		return result, err
	}
	// Retain frozen investor weeks even if source corrections change routing.
	rows, err := tx.Query(ctx, `SELECT report FROM investor_pay_weeks WHERE week_start=$1::date AND finalized`, result.WeekStart)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var body []byte
		if err = rows.Scan(&body); err != nil {
			rows.Close()
			return result, err
		}
		var d DriverPayDriver
		if err = json.Unmarshal(body, &d); err != nil {
			rows.Close()
			return result, err
		}
		found := false
		for _, v := range result.Drivers {
			if v.ID == d.ID {
				found = true
			}
		}
		if !found {
			result.Drivers = append(result.Drivers, d)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	result.Revision = payrollRevision(result)
	return result, nil
}
