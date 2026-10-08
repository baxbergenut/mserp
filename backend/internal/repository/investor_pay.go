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

type truckPayLoadMatch struct {
	TruckID    string
	Candidates []string
}

// Imported source loads retain an indexed truck ID across renames. Only blank
// source units and unmatched plans fall back to the unique dated assignment.
// Unresolved source labels supply candidates for scoped review, never a guess.
func truckPayLoadMap(ctx context.Context, tx pgx.Tx, week string) (map[string]truckPayLoadMatch, error) {
	rows, err := tx.Query(ctx, `SELECT e.driver_id::text,e.service_date::text,e.slot,lower(btrim(e.load_number)),
 coalesce(l.truck_id::text,''),(l.id IS NULL OR btrim(coalesce(l.truck_unit,''))=''),
 ARRAY(SELECT DISTINCT a.truck_id::text FROM truck_driver_assignments a WHERE a.driver_id=e.driver_id
 AND (a.assigned_at AT TIME ZONE 'America/New_York')::date<=e.service_date
 AND (a.unassigned_at IS NULL OR e.service_date<(a.unassigned_at AT TIME ZONE 'America/New_York')::date)),
 ARRAY(SELECT a.truck_id::text FROM truck_unit_aliases a WHERE l.truck_id IS NULL AND a.unit_key=upper(btrim(l.truck_unit)))
 FROM `+grossBoardEntriesSQL+` e `+grossBoardResolvedLoad+`
 WHERE NOT e.deleted AND e.day_status='' AND btrim(e.load_number)<>'' AND e.service_date>=$1::date AND e.service_date<$1::date+7`, week)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string]truckPayLoadMatch{}
	for rows.Next() {
		var driver, date, number string
		var assigned, aliases []string
		var value truckPayLoadMatch
		var fallback bool
		var slot int
		if err = rows.Scan(&driver, &date, &slot, &number, &value.TruckID, &fallback, &assigned, &aliases); err != nil {
			return nil, err
		}
		value.Candidates = append(assigned, aliases...)
		if value.TruckID == "" && fallback && len(assigned) == 1 {
			value.TruckID = assigned[0]
		}
		result[driver+":"+payCommentKey(date, slot, number)] = value
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
	missingFrozen := map[string][]string{}
	for id, d := range frozenDrivers {
		for _, l := range d.Loads {
			if !currentLoads[id+":"+l.CommentKey] {
				missingFrozen[id] = append(missingFrozen[id], l.LoadNumber)
			}
		}
	}
	unresolved := map[string][]DriverPayLoad{}
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
			if loadMap[d.ID+":"+l.CommentKey].TruckID == "" {
				unresolved[d.ID] = append(unresolved[d.ID], l)
			}
			g := groups[loadMap[d.ID+":"+l.CommentKey].TruckID]
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
		driverIDs := make([]string, 0, len(g.drivers))
		for driverID := range g.drivers {
			driverIDs = append(driverIDs, driverID)
		}
		people, e := tx.Query(ctx, `SELECT d.id::text,d.full_name,coalesce(p.id::text,''),coalesce(p.full_name,'Unassigned') FROM drivers d LEFT JOIN dispatchers p ON p.id=d.dispatcher_id
        WHERE d.id=ANY($2::uuid[]) OR (cardinality($2::uuid[])=0 AND EXISTS(SELECT 1 FROM truck_driver_assignments a WHERE a.driver_id=d.id AND a.truck_id=$1 AND (a.assigned_at AT TIME ZONE 'America/New_York')::date<$3::date+7 AND (a.unassigned_at IS NULL OR (a.unassigned_at AT TIME ZONE 'America/New_York')::date>$3::date))) ORDER BY d.full_name,d.id`, id, driverIDs, source.WeekStart)
		if e != nil {
			return source, investor, e
		}
		dispatcherNames := []string{}
		dispatcherIDs := map[string]bool{}
		for people.Next() {
			var person PayPerson
			var dispatcherID, dispatcherName string
			if e = people.Scan(&person.ID, &person.Name, &dispatcherID, &dispatcherName); e != nil {
				people.Close()
				return source, investor, e
			}
			g.card.OperatingDrivers = append(g.card.OperatingDrivers, person)
			if !dispatcherIDs[dispatcherID] {
				dispatcherIDs[dispatcherID] = true
				dispatcherNames = append(dispatcherNames, dispatcherName)
			}
			g.card.DispatcherID = dispatcherID
		}
		e = people.Err()
		people.Close()
		if e != nil {
			return source, investor, e
		}
		if len(dispatcherIDs) != 1 {
			g.card.DispatcherID = ""
		}
		g.card.DispatcherName = strings.Join(dispatcherNames, ", ")
		for _, d := range source.Drivers {
			numbers := []string{}
			for _, l := range unresolved[d.ID] {
				relevant := g.drivers[d.ID]
				for _, candidate := range loadMap[d.ID+":"+l.CommentKey].Candidates {
					if candidate == id {
						relevant = true
					}
				}
				if relevant {
					numbers = append(numbers, l.LoadNumber)
				}
			}
			if len(numbers) > 0 {
				g.card.Issues = append(g.card.Issues, "Review truck assignment for "+d.FullName+": "+strings.Join(numbers, ", ")+".")
			}
		}
		for _, person := range g.card.OperatingDrivers {
			if numbers := missingFrozen[person.ID]; len(numbers) > 0 {
				g.card.Issues = append(g.card.Issues, "Loads in "+person.Name+"'s finalized paycheck are missing from Gross Board: "+strings.Join(numbers, ", ")+".")
			}
		}

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
			g := groups[loadMap[d.ID+":"+l.CommentKey].TruckID]
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
func (r *DriverPayRepository) InvestorPay(ctx context.Context, week time.Time, targetTruck ...string) (DriverPayWeek, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return DriverPayWeek{}, err
	}
	defer tx.Rollback(ctx)
	result, err := readInvestorPay(ctx, tx, week)
	if err != nil {
		return result, err
	}
	target := ""
	if len(targetTruck) > 0 {
		target = targetTruck[0]
	}
	return investorPayView(result, target), tx.Commit(ctx)
}

func investorPayView(report DriverPayWeek, target string) DriverPayWeek {
	visible := []DriverPayDriver{}
	for _, d := range report.Drivers {
		if !d.TruckInactive || d.ID == target {
			visible = append(visible, d)
		}
	}
	report.Drivers = visible
	return report
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
	// Configuration gaps are informational current-fleet rows, never zero-value
	// statements or inferred historical ownership/rates.
	setup, err := tx.Query(ctx, `SELECT t.id::text,t.unit_number,i.id::text,coalesce(d.full_name,i.full_name),coalesce(operator.id::text,''),coalesce(operator.full_name,''),coalesce(dispatcher.id::text,''),coalesce(dispatcher.full_name,'')
	FROM trucks t JOIN investors i ON i.id=t.owner_id LEFT JOIN drivers d ON d.id=i.driver_id
    LEFT JOIN truck_driver_assignments assignment ON assignment.truck_id=t.id AND assignment.unassigned_at IS NULL
    LEFT JOIN drivers operator ON operator.id=assignment.driver_id
    LEFT JOIN dispatchers dispatcher ON dispatcher.id=operator.dispatcher_id
	WHERE t.active AND `+investorTruckEligibility+` AND NOT EXISTS(SELECT 1 FROM truck_settlement_terms s WHERE s.truck_id=t.id AND s.week_start<=$1::date)
	ORDER BY coalesce(d.full_name,i.full_name),t.unit_number`, result.WeekStart)
	if err != nil {
		return result, err
	}
	for setup.Next() {
		var v InvestorTruckSetup
		if err = setup.Scan(&v.TruckID, &v.TruckUnit, &v.OwnerID, &v.OwnerName, &v.DriverID, &v.DriverName, &v.DispatcherID, &v.DispatcherName); err != nil {
			setup.Close()
			return result, err
		}
		result.SetupRequired = append(result.SetupRequired, v)
	}
	err = setup.Err()
	setup.Close()
	if err != nil {
		return result, err
	}
	activeRows, err := tx.Query(ctx, `SELECT id::text FROM trucks WHERE active`)
	if err != nil {
		return result, err
	}
	active := map[string]bool{}
	for activeRows.Next() {
		var id string
		if err = activeRows.Scan(&id); err != nil {
			activeRows.Close()
			return result, err
		}
		active[id] = true
	}
	err = activeRows.Err()
	activeRows.Close()
	if err != nil {
		return result, err
	}
	for i := range result.Drivers {
		result.Drivers[i].TruckInactive = !active[result.Drivers[i].ID]
	}
	result.Revision = payrollRevision(result)
	return result, nil
}
