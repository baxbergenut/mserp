package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

type reportCost struct {
	reportIdentity
	kind, id, label, date, payer string
	amount                       *float64
}

var reportsCostsSQL = `
WITH aliases AS (
 SELECT unit_key,(array_agg(DISTINCT truck_id))[1] truck FROM truck_unit_aliases GROUP BY unit_key HAVING count(DISTINCT truck_id)=1
 ), costs AS MATERIALIZED (
 SELECT 'Fuel' kind,f.id::text id,coalesce(nullif(f.relay_transaction_id,''),f.id::text) label,
 f.purchased_on cost_day,f.purchased_on ownership_day,a.truck,f.driver_id driver,sum(i.total_amount_paid) amount,'' payer
 FROM fuel_transactions f JOIN fuel_transaction_items i ON i.fuel_transaction_id=f.id AND i.item_kind='fuel'
 LEFT JOIN aliases a ON cardinality(f.reported_truck_units)=1 AND a.unit_key=f.reported_truck_units[1]
 WHERE f.relay_environment='production' AND f.currency_code='USD' AND f.purchased_on BETWEEN $1::date AND $2::date
 GROUP BY f.id,a.truck
 UNION ALL
 SELECT 'Tolls',t.id::text,coalesce(nullif(t.equipment_unit,''),t.id::text),t.posting_date,t.exit_date,t.truck_id,NULL,t.amount,''
 FROM tolls t WHERE t.posting_date BETWEEN $1::date AND $2::date AND (t.prepass_environment IS NULL OR t.prepass_environment='production')
 UNION ALL
 SELECT 'Scales',t.id::text,t.ticket_number,t.transaction_date,t.transaction_date,t.truck_id,t.driver_id,t.amount,
 CASE WHEN t.source_conflict OR b.payer='review' THEN 'review' WHEN b.payer='company' THEN 'company' ELSE 'operator' END
 FROM weighmytruck_transactions t` + wmtPayerJoins + ` WHERE t.transaction_date BETWEEN $1::date AND $2::date
 UNION ALL
 SELECT CASE WHEN EXISTS(SELECT 1 FROM maintenance_configuration m WHERE m.category_id=e.category_id) THEN 'Maintenance' ELSE 'Equipment' END,
 e.id::text,coalesce(nullif(e.expense_type,''),e.category),e.expense_date,e.expense_date,e.truck_id,
 coalesce(e.charge_driver_id,e.driver_id),e.amount,
 CASE WHEN lower(btrim(e.covered_by))='company' THEN 'company'
 WHEN lower(btrim(e.covered_by)) IN ('driver','truck owner','broker') THEN lower(btrim(e.covered_by)) ELSE 'review' END
 FROM expenses e WHERE e.expense_date BETWEEN $1::date AND $2::date
 AND lower(btrim(e.covered_by))='company' AND e.charge_driver_id IS NULL
 AND (EXISTS(SELECT 1 FROM maintenance_configuration m WHERE m.category_id=e.category_id)
 OR lower(btrim(e.expense_type)) IN ('equipment','equipment lease','equipment rental','truck lease','truck rental','trailer lease','trailer rental'))
 ), ownership_days AS (SELECT DISTINCT truck,ownership_day FROM costs WHERE truck IS NOT NULL),
 ownership AS MATERIALIZED (
 SELECT c.truck,c.ownership_day,(array_agg(DISTINCT h.owner_id))[1] owner_id
 FROM ownership_days c JOIN truck_ownership_history h ON h.truck_id=c.truck
 AND (NOT h.start_known OR h.assigned_at<((c.ownership_day+1)::timestamp AT TIME ZONE 'America/New_York'))
 AND (h.unassigned_at IS NULL OR h.unassigned_at>=((c.ownership_day+1)::timestamp AT TIME ZONE 'America/New_York'))
 AND (NOT h.start_known OR h.unassigned_at IS NULL OR h.assigned_at<h.unassigned_at)
 GROUP BY c.truck,c.ownership_day HAVING count(DISTINCT h.owner_id)=1
 )
 SELECT c.kind,c.id,c.label,c.cost_day::text,(c.amount*100)::float8,coalesce(c.truck::text,''),coalesce(t.unit_number,''),
 coalesce(coalesce(c.driver,active.driver)::text,''),coalesce(d.full_name,''),
 coalesce(dispatch.dispatcher_id::text,''),coalesce(dispatch.dispatcher_name,''),
 CASE WHEN own.is_company THEN 'company' WHEN own.driver_id=active.driver THEN 'owner_operator'
  WHEN own.id IS NOT NULL AND active.driver IS NOT NULL THEN 'investor_driver' ELSE 'unassigned' END,
 CASE WHEN c.payer<>'' THEN c.payer WHEN own.is_company THEN 'company'
  WHEN own.id IS NOT NULL AND active.driver IS NOT NULL THEN 'operator' ELSE 'review' END
 FROM costs c LEFT JOIN trucks t ON t.id=c.truck
 LEFT JOIN LATERAL (
 SELECT (array_agg(DISTINCT a.driver_id))[1] driver FROM truck_driver_assignments a
 WHERE a.truck_id=c.truck AND a.assigned_at<((c.ownership_day+1)::timestamp AT TIME ZONE 'America/New_York')
 AND (a.unassigned_at IS NULL OR (a.unassigned_at>=((c.ownership_day+1)::timestamp AT TIME ZONE 'America/New_York') AND a.assigned_at<a.unassigned_at))
 HAVING count(DISTINCT a.driver_id)=1
 ) active ON true
 LEFT JOIN drivers d ON d.id=coalesce(c.driver,active.driver)
 LEFT JOIN LATERAL (
 SELECT h.dispatcher_id,h.dispatcher_name FROM driver_dispatcher_assignments h
 WHERE h.driver_id=payroll_driver_at(d.id,c.ownership_day)
 AND (NOT h.start_known OR h.assigned_at<((c.ownership_day+1)::timestamp AT TIME ZONE 'America/New_York'))
 AND (h.unassigned_at IS NULL OR h.unassigned_at>=((c.ownership_day+1)::timestamp AT TIME ZONE 'America/New_York'))
 ORDER BY h.assigned_at DESC LIMIT 1
 ) dispatch ON true
 LEFT JOIN ownership h ON h.truck=c.truck AND h.ownership_day=c.ownership_day
 LEFT JOIN investors own ON own.id=h.owner_id`

func (s *reportRead) addFinancial(ctx context.Context, groups reportGroups, loads []reportLoad, from, to string) error {
	rows, err := s.tx.Query(ctx, reportsCostsSQL, from, to)
	if err != nil {
		return err
	}
	for rows.Next() {
		var c reportCost
		if err = rows.Scan(&c.kind, &c.id, &c.label, &c.date, &c.amount, &c.truck, &c.unit, &c.driver, &c.name, &c.dispatcher, &c.dispatcherName, &c.employment, &c.payer); err != nil {
			rows.Close()
			return err
		}
		if c.dispatcher == "" && c.dispatcherName != "" {
			c.dispatcher = "historical:" + c.dispatcherName
		}
		// Reports contains attributable company costs only. Driver/operator charges
		// and unresolved cost identities must not appear as company expenses.
		if c.payer != "company" || c.truck == "" || c.employment == "unassigned" || c.amount == nil {
			continue
		}
		id, name, ok := s.groupIdentity(c.reportIdentity)
		if !ok || !s.includes(c.reportIdentity) {
			continue
		}
		g := groups.add(id, name, c.reportIdentity, ReportEvidence{Kind: c.kind, ID: c.id, Label: c.label, Date: c.date, State: "company", Values: map[string]float64{"amount": *c.amount}})
		g.Metrics[strings.ToLower(c.kind)] += *c.amount
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	return s.addReportPayroll(ctx, groups, loads, from, to)
}

type reportPayCard struct {
	card DriverPayDriver
	week string
}

func (s *reportRead) reportPayrollWeek(ctx context.Context, week string) ([]DriverPayDriver, error) {
	if cards, ok := s.payroll[week]; ok {
		return cards, nil
	}
	rows, err := s.tx.Query(ctx, `SELECT driver_id::text,report
 FROM payroll_settlements WHERE week_start=$1::date AND finalized`, week)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var cards []DriverPayDriver
	for rows.Next() {
		var card DriverPayDriver
		var id string
		var body []byte
		if err = rows.Scan(&id, &body); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(body, &card); err != nil {
			return nil, err
		}
		card.ID = id
		cards = append(cards, card)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	s.payroll[week] = cards
	return cards, nil
}
func (s *reportRead) addReportPayroll(ctx context.Context, groups reportGroups, loads []reportLoad, from, to string) error {
	weeks := map[string]bool{}
	required := map[string]reportLoad{}
	for _, l := range loads {
		_, _, ok := s.groupIdentity(l.reportIdentity)
		if (l.state != "confirmed" && l.state != "provisional") || !ok || l.truck == "" || l.employment == "unassigned" || !s.includes(l.reportIdentity) {
			continue
		}
		required[l.payKey()] = l
		weeks[reportMonday(l.serviceDate)] = true
		weeks[reportMonday(l.date)] = true
		if l.note != "" && strings.HasPrefix(l.note, "Gross Board transfer to ") {
			weeks[reportMonday(strings.TrimPrefix(l.note, "Gross Board transfer to "))] = true
		}
	}
	start, _ := time.Parse(time.DateOnly, reportMonday(from))
	for week := start; week.Format(time.DateOnly) <= to; week = week.AddDate(0, 0, 7) {
		weeks[week.Format(time.DateOnly)] = true
	}
	cards := []reportPayCard{}
	keys := map[reportAssignmentKey]bool{}
	// Include frozen placements outside the service week by exact Loads identity.
	parents := make([]string, 0, len(required))
	for _, l := range required {
		if l.parent != "" {
			parents = append(parents, l.parent)
		}
	}
	if len(parents) > 0 {
		rows, err := s.tx.Query(ctx, reportsFrozenWeeksSQL, parents)
		if err != nil {
			return err
		}
		for rows.Next() {
			var week string
			if err = rows.Scan(&week); err != nil {
				rows.Close()
				return err
			}
			weeks[week] = true
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
	}
	var firstWeek string
	if err := s.tx.QueryRow(ctx, `SELECT coalesce(first_week::text,'') FROM payroll_projection_control WHERE singleton`).Scan(&firstWeek); err != nil {
		return err
	}
	for week := range weeks {
		if week > PayrollLatestWeek() || week < firstWeek {
			continue
		}
		drivers, err := s.reportPayrollWeek(ctx, week)
		if err != nil {
			return err
		}
		for _, card := range drivers {
			if len(card.Loads) == 0 && card.Edits.Version == 0 && card.NetPayCents() == 0 {
				continue
			}
			cards = append(cards, reportPayCard{card, week})
			// Load earnings already have dated identity. Only whole-week settlements
			// and additional earnings need the seven-day assignment graph.
			if s.query.View == ReportOperator || len(card.Layovers) > 0 || len(card.Edits.Adjustments) > 0 {
				for day := 0; day < 7; day++ {
					keys[reportAssignmentKey{card.ID, reportAddDays(week, day)}] = true
				}
			}
		}
	}
	assigned, err := reportAssignments(ctx, s.tx, keys)
	if err != nil {
		return err
	}
	paid := map[string]int{}
	for _, record := range cards {
		d, week := record.card, record.week
		for _, pay := range d.Loads {
			key := reportFrozenPayKey(d.ID, pay)
			l, ok := required[key]
			if !ok {
				continue
			}
			paid[key]++
			fee, feeErr := chargeCents(pay.Fee)
			metrics := map[string]float64{}
			if feeErr == nil {
				metrics["earnedPay"] = float64(fee)
			}
			g := groups.add(l.truck, l.unit, l.reportIdentity, ReportEvidence{Kind: "Driver Pay", ID: d.ID + "/" + week + "/" + key, Label: l.number, Date: l.date,
				State: "finalized", Note: "Statement week " + week, Values: metrics})
			if feeErr != nil || pay.Fee == "" || paid[key] > 1 {
				g.missingPay = true
				continue
			}
			g.Metrics["earnedPay"] += float64(fee)
			if s.query.View == ReportOperator {
				basis, basisErr := chargeCents(pay.DriverGross)
				if basisErr != nil || pay.DriverGross == "" {
					g.missingPay = true
				} else {
					g.Metrics["carrierFees"] += float64(basis - fee)
					if l.gross != nil {
						g.Metrics["dispatchBalance"] += *l.gross - float64(basis)
					}
					metrics["carrierFees"] = float64(basis - fee)
				}
			}
		}
		if week >= reportMonday(from) && week <= to {
			s.addReportStatement(groups, d, week, from, to, assigned)
			if s.query.View == ReportOperator && week >= from {
				s.addReportRecurring(groups, d, week, assigned)
			}
		}
	}
	for key, l := range required {
		if paid[key] == 0 {
			basis := l.driverGross
			if l.payType == "cpm" {
				basis = l.payMiles
			}
			amount := driverPayFee(l.payType, l.payRate, basis)
			fee, err := chargeCents(amount)
			evidence := ReportEvidence{Kind: "Driver Pay", ID: l.payDriver + "/" + reportMonday(l.serviceDate) + "/tariff/" + key,
				Label: l.number, Date: l.date, State: "calculated", Note: "Calculated from Driver Pay tariff", Values: map[string]float64{}}
			g := groups.add(l.truck, l.unit, l.reportIdentity, evidence)
			if amount == "" || err != nil {
				g.Sources[len(g.Sources)-1].Note = "Pay needs a valid tariff and Gross Board " + map[bool]string{true: "mileage", false: "driver gross"}[l.payType == "cpm"]
				g.missingPay = true
				continue
			}
			evidence.Values["earnedPay"] = float64(fee)
			g.Metrics["earnedPay"] += float64(fee)
			if s.query.View == ReportOperator {
				gross, err := chargeCents(l.driverGross)
				if l.driverGross == "" || err != nil {
					g.missingPay = true
					continue
				}
				evidence.Values["carrierFees"] = float64(gross - fee)
				g.Metrics["carrierFees"] += float64(gross - fee)
				if l.gross != nil {
					g.Metrics["dispatchBalance"] += *l.gross - float64(gross)
				}
			}
		}
	}
	return nil
}

func (l reportLoad) payKey() string {
	if l.parent != "" {
		return l.parent + "/" + l.trip
	}
	return "board:" + l.driver + "/" + l.serviceDate + "/" + l.boardSlot
}

func reportFrozenPayKey(driver string, pay DriverPayLoad) string {
	if pay.LoadRecordID != nil {
		return fmt.Sprint(*pay.LoadRecordID) + "/" + reportTripID(pay.LoadTripID)
	}
	date, slot := pay.SourceDate, pay.SourceSlot
	if date == "" {
		date, slot = pay.Date, pay.Slot
	}
	if pay.SourceDriverID != "" {
		driver = pay.SourceDriverID
	}
	return "board:" + driver + "/" + date + "/" + strconv.Itoa(slot)
}

// Recurring revenue is the amount actually deducted in a frozen statement,
// including partial/zero source choices, never the scheduled base or due balance.
func (s *reportRead) addReportRecurring(groups reportGroups, d DriverPayDriver, week string, assigned map[reportAssignmentKey]reportIdentity) {
	identities := map[string]reportIdentity{}
	for day := 0; day < 7; day++ {
		i := assigned[reportAssignmentKey{d.ID, reportAddDays(week, day)}]
		if i.truck != "" && i.employment == "owner_operator" {
			i.driver, i.name = d.ID, d.FullName
			identities[i.truck] = i
		}
	}
	choices := map[string]string{}
	for _, choice := range d.Edits.SourceCharges {
		choices[choice.Source] = choice.Amount
	}
	seen := map[string]bool{}
	add := func(source, label, amount, truck string) {
		if seen[source] {
			return
		}
		seen[source] = true
		i, ok := identities[truck]
		if !ok || !s.includes(i) {
			return
		}
		paid, err := chargeCents(amount)
		if err != nil || paid >= 0 {
			return
		}
		g := groups.add(i.truck, i.unit, i, ReportEvidence{Kind: "Driver Pay", ID: d.ID + "/" + week + "/" + source, Label: label, Date: week,
			State: "finalized", Note: "Recurring charge deducted", Values: map[string]float64{"recurringCharges": float64(-paid)}})
		g.Metrics["recurringCharges"] += float64(-paid)
	}
	for _, charge := range d.AutoCharges {
		parts := strings.Split(charge.Source, ":")
		if len(parts) != 3 || parts[0] != "truck_charge" {
			continue
		}
		amount := charge.Amount
		if selected, exists := choices[charge.Source]; exists {
			amount = selected
		}
		add(charge.Source, charge.Name, amount, parts[1])
	}
	// Older frozen statements may contain driver-based recurring occurrences.
	// Without one dated owner-operated truck, those cannot be assigned safely.
	if len(identities) == 1 {
		for truck := range identities {
			for _, charge := range d.Edits.GeneratedCharges {
				if charge.Kind == "recurring" {
					add("recurring:"+charge.ScheduleID, charge.Name, charge.Amount, truck)
				}
			}
		}
	}
}

func reportAddDays(date string, days int) string {
	t, _ := time.Parse(time.DateOnly, date)
	return t.AddDate(0, 0, days).Format(time.DateOnly)
}

func (s *reportRead) addReportStatement(groups reportGroups, d DriverPayDriver, week, from, to string, assigned map[reportAssignmentKey]reportIdentity) {
	var i reportIdentity
	mixed, mixedDispatcher, relevant, matched := false, false, false, false
	for day := 0; day < 7; day++ {
		a := assigned[reportAssignmentKey{d.ID, reportAddDays(week, day)}]
		if a.truck == "" {
			continue
		}
		if i.truck != "" && (i.truck != a.truck || i.employment != a.employment) {
			mixed = true
		}
		if i.truck != "" && i.dispatcher != a.dispatcher {
			mixedDispatcher = true
		}
		a.name = d.FullName
		_, _, valid := s.groupIdentity(a)
		if valid && a.employment != "unassigned" {
			relevant = true
			if s.includes(a) {
				matched = true
			}
		}
		i = a
	}
	i.driver, i.name = d.ID, d.FullName
	if mixedDispatcher {
		i.dispatcher, i.dispatcherName = "", ""
	}
	id, name, _ := s.groupIdentity(i)
	if mixed {
		id, name = "", ""
	}
	values := map[string]float64{}
	note := ""
	if s.query.View == ReportCompany {
		for _, row := range d.Layovers {
			if row.Date < from || row.Date > to || slices.Contains(d.Edits.LayoverExcludedDates, row.Date) {
				continue
			}
			a := assigned[reportAssignmentKey{d.ID, row.Date}]
			if a.employment != "company" || !s.includes(a) {
				continue
			}
			n, err := chargeCents(row.Amount)
			if err != nil {
				continue
			}
			g := groups.add(a.truck, a.unit, a, ReportEvidence{Kind: "Driver Pay", ID: d.ID + "/" + week + "/layover/" + row.Date, Label: d.FullName, Date: row.Date,
				State: "finalized", Note: "Layover earnings", Values: map[string]float64{"earnedPay": float64(n)}})
			g.Metrics["earnedPay"] += float64(n)
		}
		// Weekly manual earnings adjustments carry no service date.
		if week >= from && week <= to {
			for _, a := range d.Edits.Adjustments {
				if a.Kind != "addition" {
					continue
				}
				n, err := chargeCents(a.Amount)
				if err == nil {
					values["earnedPay"] += float64(n)
				}
			}
		}
		if _, ok := values["earnedPay"]; !ok || (!matched && i.truck != "") || (i.truck == "" && !s.includes(i)) {
			return
		}
		note = "Weekly earnings adjustment"
	} else {
		if week < from || !relevant || !matched {
			return
		}
		var earned int64
		for _, l := range d.Loads {
			n, _ := chargeCents(l.Fee)
			earned += n
		}
		earned += driverLayoverTotal(d)
		reimb := d.ReimbursedCents()
		for _, a := range d.Edits.Adjustments {
			n, _ := chargeCents(a.Amount)
			if a.Kind == "addition" {
				earned += n
			} else if a.Kind == "reimbursement" {
				reimb += n
			}
		}
		returned, applied := d.EscrowMovementCents()
		var withheld int64
		for _, a := range d.Edits.ExpenseDeductions {
			if a.Source == "escrow" {
				n, _ := chargeCents(a.Amount)
				withheld += n
			}
		}
		for _, a := range d.AutoCharges {
			if strings.HasPrefix(a.Source, "escrow:") {
				n, _ := chargeCents(a.Amount)
				withheld -= n
			}
		}
		net := d.NetPayCents()
		values = map[string]float64{"recurringCharges": 0, "settlementEarnings": float64(earned), "reimbursements": float64(reimb),
			"deductions": float64(earned + reimb + returned - withheld - net), "escrowReturned": float64(returned),
			"escrowApplied": float64(applied), "escrowWithheld": float64(withheld), "netPayable": float64(net)}
		note = "Whole statement week"
	}
	if mixed {
		note += " · Multiple dated truck assignments"
	}
	g := groups.add(id, name, i, ReportEvidence{Kind: "Driver Pay", ID: d.ID + "/" + week, Label: d.FullName, Date: week,
		State: "finalized", Note: note, Values: values})
	for key, value := range values {
		g.Metrics[key] += value
	}
}

func (g *reportGroup) finishFinancial(view string) {
	if g.ID == "unassigned" {
		if len(g.loads) == 0 && !g.missingGross {
			g.Metrics["gross"] = 0
		}
		return
	}
	m := g.Metrics
	var costs float64
	for _, key := range []string{"fuel", "tolls", "scales", "maintenance", "equipment"} {
		m[key] += 0
		costs += m[key]
	}
	if !g.missingCosts {
		m["carrierCosts"] = costs
	}
	if len(g.loads) == 0 && !g.missingGross && !g.missingPay && m["provisionalLoads"] == 0 {
		m["gross"] = 0
	}
	gross, known := m["gross"]
	if g.missingPay {
		delete(m, "earnedPay")
		delete(m, "carrierFees")
		delete(m, "dispatchBalance")
	}
	if view == ReportCompany {
		if miles := m["miles"]; miles > 0 {
			if known {
				m["revenuePerMile"] = gross / miles
			}
			if !g.missingPay && !g.missingCosts {
				m["costPerMile"] = (costs + m["earnedPay"]) / miles
			}
		}
		if !g.missingPay && !g.missingCosts && known {
			m["contribution"] = gross - costs - m["earnedPay"]
			if gross > 0 {
				m["margin"] = 100 * m["contribution"] / gross
			}
		}
	} else if !g.missingPay && known {
		m["carrierFees"] += 0
		if !g.missingCosts {
			m["contribution"] = m["carrierFees"] + m["recurringCharges"] - costs
		}
	}
}

const reportsFrozenWeeksSQL = `SELECT f.week_start::text FROM payroll_settlements f
   WHERE f.finalized AND reports_statement_load_ids(f.report) && $1::text[]::bigint[]
   UNION SELECT p.week_start::text FROM payroll_load_placements p
   WHERE p.included AND p.load_record_id::text=ANY($1::text[])`
