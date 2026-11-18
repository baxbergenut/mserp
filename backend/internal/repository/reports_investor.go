package repository

import (
	"context"
	"encoding/json"
	"strings"
)

type reportInvestorWeek struct {
	truck, owner, week, unit string
	card                     DriverPayDriver
}

type reportInvestorLoad struct {
	pay       DriverPayLoad
	week      string
	ambiguous bool
}

// Investor Pay splits driver gross using the dated investor agreement. Hired
// driver wages are paid out of the investor's share, not the carrier's fee.
func (s *reportRead) addInvestorReport(ctx context.Context, groups reportGroups, loads []reportLoad, from, to string) error {
	terms, err := truckTermData(ctx, s.tx)
	if err != nil {
		return err
	}
	config := TruckChargeData{Terms: terms}
	byWeek := map[string]map[string]TruckTerm{}
	trucks, parents := []string{}, []string{}
	for _, l := range loads {
		if l.employment != "investor_driver" {
			continue
		}
		trucks = append(trucks, l.truck)
		if l.parent != "" {
			parents = append(parents, l.parent)
		}
	}
	rows, err := s.tx.Query(ctx, `SELECT w.truck_id::text,w.owner_id::text,w.week_start::text,t.unit_number,w.report
 FROM investor_pay_weeks w JOIN trucks t ON t.id=w.truck_id WHERE w.finalized AND
 (w.week_start BETWEEN $1::date AND $2::date OR (w.truck_id=ANY($3::uuid[]) AND
 (reports_statement_load_ids(w.report) && $4::text[]::bigint[] OR w.week_start=ANY($5::text[]::date[]))))`, from, to, trucks, parents, investorServiceWeeks(loads))
	if err != nil {
		return err
	}
	type loadKey struct{ truck, owner, key string }
	frozen := map[loadKey]reportInvestorLoad{}
	weeks := map[string]map[string]reportInvestorWeek{}
	for rows.Next() {
		var w reportInvestorWeek
		var body []byte
		if err = rows.Scan(&w.truck, &w.owner, &w.week, &w.unit, &body); err != nil {
			rows.Close()
			return err
		}
		if err = json.Unmarshal(body, &w.card); err != nil {
			rows.Close()
			return err
		}
		if weeks[w.week] == nil {
			weeks[w.week] = map[string]reportInvestorWeek{}
		}
		weeks[w.week][w.truck] = w
		for _, pay := range w.card.Loads {
			key := loadKey{w.truck, w.owner, reportFrozenPayKey(pay.SourceDriverID, pay)}
			_, exists := frozen[key]
			frozen[key] = reportInvestorLoad{pay, w.week, exists}
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, l := range loads {
		if l.employment != "investor_driver" || (l.state != "confirmed" && l.state != "provisional") || !s.includes(l.reportIdentity) {
			continue
		}
		key := l.payKey()
		basis, share, state := l.driverGross, "", "calculated"
		week := reportMonday(l.settlementDate)
		if hit, ok := frozen[loadKey{l.truck, l.owner, key}]; ok {
			week, basis, state = hit.week, hit.pay.DriverGross, "finalized"
			if !hit.ambiguous {
				share = hit.pay.Fee
			}
		} else {
			if byWeek[week] == nil {
				byWeek[week] = applicableTruckTerms(config, week)
			}
			if term, ok := byWeek[week][l.truck]; ok && term.OwnerID == l.owner {
				share = driverPayFee("gross_percentage", term.SharePercent, basis)
			}
		}
		values := map[string]float64{}
		g := groups.add(l.truck, l.unit, l.reportIdentity, ReportEvidence{Kind: "Investor Pay", ID: l.truck + "/" + week + "/" + key, Label: l.number, Date: l.date, State: state, Values: values})
		gross, e1 := chargeCents(basis)
		fee, e2 := chargeCents(share)
		if share == "" || basis == "" || e1 != nil || e2 != nil {
			g.missingPay = true
			g.Sources[len(g.Sources)-1].Note = "Investor share or driver gross unavailable"
			continue
		}
		values["carrierFees"] = float64(gross - fee)
		g.Metrics["carrierFees"] += values["carrierFees"]
		if state == "calculated" {
			g.Estimated["carrierFees"] = true
		}
	}
	for week := reportMonday(from); week <= to; week = reportAddDays(week, 7) {
		if week < from {
			continue
		}
		if err = s.addInvestorReportCharges(ctx, groups, week, weeks[week], applicableTruckTerms(config, week)); err != nil {
			return err
		}
	}
	return nil
}

func investorServiceWeeks(loads []reportLoad) []string {
	var result []string
	for _, l := range loads {
		if l.employment == "investor_driver" {
			result = append(result, reportMonday(l.settlementDate))
		}
	}
	return result
}

func (s *reportRead) addInvestorReportCharges(ctx context.Context, groups reportGroups, week string, frozen map[string]reportInvestorWeek, terms map[string]TruckTerm) error {
	// Route weekly charges by dated ownership. Attribute driver/dispatcher filters
	// only where the truck has one unambiguous operating assignment at week end.
	rows, err := s.tx.Query(ctx, `SELECT t.id::text,t.unit_number,i.id::text,coalesce(i.driver_id::text,''),coalesce(a.driver::text,''),coalesce(d.full_name,''),
 coalesce(h.dispatcher_id::text,''),coalesce(h.dispatcher_name,'')
 FROM trucks t `+truckOwnershipForWeekSQL+`
 LEFT JOIN LATERAL (SELECT (array_agg(DISTINCT driver_id))[1] driver FROM truck_driver_assignments
 WHERE truck_id=t.id AND assigned_at<(($1::date+7)::timestamp AT TIME ZONE 'America/New_York')
 AND (unassigned_at IS NULL OR unassigned_at>=(($1::date+7)::timestamp AT TIME ZONE 'America/New_York')) HAVING count(DISTINCT driver_id)=1) a ON true
 LEFT JOIN drivers d ON d.id=a.driver
 LEFT JOIN LATERAL (SELECT dispatcher_id,dispatcher_name FROM driver_dispatcher_assignments
 WHERE driver_id=payroll_driver_at(a.driver,$1::date+6) AND (NOT start_known OR assigned_at<(($1::date+7)::timestamp AT TIME ZONE 'America/New_York'))
 AND (unassigned_at IS NULL OR unassigned_at>=(($1::date+7)::timestamp AT TIME ZONE 'America/New_York')) ORDER BY assigned_at DESC LIMIT 1) h ON true
 WHERE NOT i.is_company`, week)
	if err != nil {
		return err
	}
	type candidate struct {
		i                  reportIdentity
		owner, ownerDriver string
	}
	var candidates []candidate
	for rows.Next() {
		var c candidate
		i := &c.i
		if err = rows.Scan(&i.truck, &i.unit, &c.owner, &c.ownerDriver, &i.driver, &i.name, &i.dispatcher, &i.dispatcherName); err != nil {
			rows.Close()
			return err
		}
		candidates = append(candidates, c)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	charges, err := s.scheduledReportCharges(ctx, week)
	if err != nil {
		return err
	}
	for _, c := range candidates {
		i := c.i
		w, final := frozen[i.truck]
		if final && w.owner != c.owner {
			continue
		}
		term, hasTerm := terms[i.truck]
		// Without an investor agreement the owner-driver pays these fees through Driver Pay.
		if !final && c.ownerDriver != "" && (!hasTerm || term.OwnerID != c.owner || i.driver == "" || c.ownerDriver == i.driver) {
			continue
		}
		if i.dispatcher == "" && i.dispatcherName != "" && i.dispatcherName != "Unassigned" {
			i.dispatcher = "historical:" + i.dispatcherName
		}
		list := charges[i.truck]
		amounts := map[string]string{}
		state := "calculated"
		if final {
			state = "finalized"
			list = w.card.AutoCharges
			for _, choice := range w.card.Edits.SourceCharges {
				amounts[choice.Source] = choice.Amount
			}
		}
		if !s.includes(i) {
			continue
		}
		seen := map[string]bool{}
		for _, charge := range list {
			if seen[charge.Source] || !strings.HasPrefix(charge.Source, "truck_charge:"+i.truck+":") {
				continue
			}
			seen[charge.Source] = true
			amount := charge.Amount
			if v, ok := amounts[charge.Source]; ok {
				amount = v
			}
			cents, err := chargeCents(amount)
			if err != nil || cents > 0 {
				continue
			}
			value := float64(-cents)
			g := groups.add(i.truck, i.unit, i, ReportEvidence{Kind: "Investor Pay", ID: i.truck + "/" + week + "/" + charge.Source, Date: week, Label: charge.Name, State: state, Values: map[string]float64{"recurringCharges": value}})
			g.Metrics["recurringCharges"] += value
			if !final {
				g.Estimated["recurringCharges"] = true
			}
		}
	}
	return nil
}
