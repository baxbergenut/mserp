package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
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
 ), costs AS (
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
 AND (EXISTS(SELECT 1 FROM maintenance_configuration m WHERE m.category_id=e.category_id)
 OR lower(btrim(e.expense_type)) IN ('equipment','equipment lease','equipment rental','truck lease','truck rental','trailer lease','trailer rental','truck payment','trailer payment'))
 )
 SELECT c.kind,c.id,c.label,c.cost_day::text,(c.amount*100)::float8,coalesce(c.truck::text,''),coalesce(t.unit_number,''),
 coalesce(coalesce(c.driver,active.driver)::text,''),coalesce(d.full_name,''),
 coalesce(dispatch.dispatcher_id::text,''),coalesce(dispatch.dispatcher_name,''),
 CASE WHEN own.is_company THEN 'company' WHEN own.driver_id=active.driver THEN 'owner_operator'
  WHEN own.owner_id IS NOT NULL AND active.driver IS NOT NULL THEN 'investor_driver' ELSE 'unassigned' END,
 CASE WHEN c.payer<>'' THEN c.payer WHEN own.is_company THEN 'company'
  WHEN own.owner_id IS NOT NULL AND active.driver IS NOT NULL THEN 'operator' ELSE 'review' END
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
 ) dispatch ON true` + reportsOwnershipSQL("c.truck", "c.ownership_day")

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
		id, name, ok := s.groupIdentity(c.reportIdentity)
		if !ok || !s.includes(c.reportIdentity) {
			continue
		}
		state := c.payer
		note := ""
		if c.truck == "" || c.employment == "unassigned" || c.payer == "review" || c.amount == nil {
			id, name, state = "", "", "review"
			switch {
			case c.truck == "":
				note = "No verified truck"
			case c.employment == "unassigned":
				note = "Truck ownership or operator needs review"
			case c.amount == nil:
				note = "No recorded amount"
			default:
				note = "Cost responsibility or source needs review"
			}
			if c.truck != "" && c.employment != "unassigned" && c.payer != "operator" {
				groups.add(c.truck, c.unit, c.reportIdentity, ReportEvidence{Kind: c.kind, ID: c.id, Label: c.label, Date: c.date, State: "review", Note: "Cost needs review"}).missingCosts = true
			}
		}
		values := map[string]float64{}
		if c.amount != nil {
			values["amount"] = *c.amount
		}
		g := groups.add(id, name, c.reportIdentity, ReportEvidence{Kind: c.kind, ID: c.id, Label: c.label, Date: c.date, State: state, Note: note, Values: values})
		if state == "company" {
			g.Metrics[strings.ToLower(c.kind)] += *c.amount
		}
		if state == "review" && c.amount != nil {
			g.Metrics["unassignedCost"] += *c.amount
		}
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
		if l.state != "confirmed" || !ok || l.truck == "" || l.employment == "unassigned" || !s.includes(l.reportIdentity) {
			continue
		}
		required[l.parent+"/"+l.trip] = l
		weeks[reportMonday(l.serviceDate)] = true
		weeks[reportMonday(l.date)] = true
		if l.note != "" && strings.HasPrefix(l.note, "Gross Board transfer to ") {
			weeks[strings.TrimPrefix(l.note, "Gross Board transfer to ")] = true
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
		parents = append(parents, l.parent)
	}
	if len(parents) > 0 {
		rows, err := s.tx.Query(ctx, `SELECT f.week_start::text FROM payroll_settlements f
   WHERE f.finalized AND EXISTS(SELECT 1 FROM jsonb_array_elements(coalesce(f.report->'loads','[]')) l
    WHERE l->>'loadRecordId'=ANY($1::text[]))
   UNION SELECT p.week_start::text FROM payroll_load_placements p
   WHERE p.included AND p.load_record_id::text=ANY($1::text[])`, parents)
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
			for day := 0; day < 7; day++ {
				keys[reportAssignmentKey{card.ID, reportAddDays(week, day)}] = true
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
			if pay.LoadRecordID == nil {
				continue
			}
			key := fmt.Sprint(*pay.LoadRecordID) + "/" + reportTripID(pay.LoadTripID)
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
			if feeErr != nil || paid[key] > 1 {
				g.missingPay = true
				continue
			}
			g.Metrics["earnedPay"] += float64(fee)
		}
		if week >= reportMonday(from) && week <= to {
			s.addReportStatement(groups, d, week, from, to, assigned)
		}
	}
	for key, l := range required {
		if paid[key] == 0 {
			groups.add(l.truck, l.unit, l.reportIdentity, ReportEvidence{Kind: "Loads", ID: l.parent, Label: l.number, Date: l.date, State: "review", Note: "No matching finalized Driver Pay load"}).missingPay = true
		}
	}
	return nil
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
		values = map[string]float64{"settlementEarnings": float64(earned), "reimbursements": float64(reimb),
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
		m["carrierFees"] = gross - m["earnedPay"]
		if !g.missingCosts {
			m["contribution"] = m["carrierFees"] - costs
		}
	}
}
