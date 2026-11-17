package repository

import (
	"context"
	"encoding/json"
)

// Read only the selected week's configuration. Report reads never prepare or
// save payroll. Scheduled amounts are estimates; frozen deductions replace them.
func (s *reportRead) addReportRecurringWeek(ctx context.Context, groups reportGroups, week string, assigned map[reportAssignmentKey]reportIdentity) error {
	cards, err := s.reportPayrollWeek(ctx, week)
	if err != nil {
		return err
	}
	frozen := map[string]bool{}
	for _, card := range cards {
		frozen[card.ID] = true
	}
	owners := map[string]map[string]reportIdentity{}
	keys := map[reportAssignmentKey]bool{}
	rows, err := s.tx.Query(ctx, `SELECT t.id::text,t.unit_number,d.id::text,d.full_name
 FROM trucks t `+truckOwnershipForWeekSQL+`
 JOIN drivers d ON d.id=i.driver_id
 LEFT JOIN LATERAL (`+latestTruckTermForWeekSQL+`) s ON true
 WHERE NOT i.is_company AND (s.truck_id IS NULL OR NOT `+truckTermCoversWeekSQL("$1::date")+`)
 AND NOT EXISTS(SELECT 1 FROM investor_pay_weeks w WHERE w.truck_id=t.id AND w.week_start=$1::date AND w.owner_id=i.id)
 AND (d.active OR EXISTS(SELECT 1 FROM driver_pay_weeks w WHERE w.driver_id=d.id AND w.week_start=$1::date)
 OR EXISTS(SELECT 1 FROM payroll_settlements p WHERE p.driver_id=d.id AND p.week_start=$1::date))`, week)
	if err != nil {
		return err
	}
	for rows.Next() {
		var i reportIdentity
		if err = rows.Scan(&i.truck, &i.unit, &i.driver, &i.name); err != nil {
			rows.Close()
			return err
		}
		i.employment = "owner_operator"
		if owners[i.driver] == nil {
			owners[i.driver] = map[string]reportIdentity{}
		}
		owners[i.driver][i.truck] = i
		keys[reportAssignmentKey{i.driver, reportAddDays(week, 6)}] = true
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	identities, err := reportAssignments(ctx, s.tx, keys)
	if err != nil {
		return err
	}
	for driver, trucks := range owners {
		for truck, i := range trucks {
			a := identities[reportAssignmentKey{driver, reportAddDays(week, 6)}]
			i.dispatcher, i.dispatcherName = a.dispatcher, a.dispatcherName
			trucks[truck] = i
			if g := groups[truck]; g != nil && s.includes(i) {
				g.Metrics["recurringCharges"] += 0
			}
		}
	}
	for _, card := range cards {
		trucks := owners[card.ID]
		if trucks == nil {
			trucks = map[string]reportIdentity{}
		}
		s.addReportRecurring(groups, card, week, assigned, trucks)
	}
	phases, err := payrollTruckPhases(ctx, s.tx, week)
	if err != nil {
		return err
	}
	data, err := truckChargesForEmploymentWeek(ctx, s.tx, TruckChargeData{Phases: phases}, week)
	if err != nil {
		return err
	}
	index := indexEffectiveTruckPhases(data.Phases, week)
	if len(index) == 0 {
		return nil
	}
	// Small configuration query; do not load recurring schedules or payroll cards.
	rows, err = s.tx.Query(ctx, `SELECT t.id::text,t.name,t.direction,t.eligibility,
 coalesce((SELECT jsonb_agg(jsonb_build_object('weekStart',r.week_start::text,'eligibility',r.eligibility) ORDER BY r.week_start)
 FROM driver_charge_type_rules r WHERE r.type_id=t.id),'[]'::jsonb) FROM driver_charge_types t ORDER BY t.id`)
	if err != nil {
		return err
	}
	var types []ChargeType
	for rows.Next() {
		var typ ChargeType
		var body []byte
		if err = rows.Scan(&typ.ID, &typ.Name, &typ.Direction, &typ.Eligibility, &body); err != nil {
			rows.Close()
			return err
		}
		if err = json.Unmarshal(body, &typ.Rules); err != nil {
			rows.Close()
			return err
		}
		types = append(types, typ)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	// Eligibility follows native weekly Gross Board placement, even when a load
	// delivers in another reporting week. Provider labels are never ownership.
	loadMap, err := truckPayLoadMap(ctx, s.tx, week)
	if err != nil {
		return err
	}
	hasLoads := map[string]bool{}
	for _, load := range loadMap {
		hasLoads[load.TruckID] = true
	}
	for driver, trucks := range owners {
		if frozen[driver] {
			continue
		}
		for truck, i := range trucks {
			if !s.includes(i) {
				continue
			}
			for _, charge := range effectiveTruckRecurringCharges(index[truck], types, truck, week, hasLoads[truck]) {
				amount, err := chargeCents(charge.Amount)
				if err != nil || amount > 0 {
					continue
				}
				g := groups.add(truck, i.unit, i, ReportEvidence{Kind: "Driver Pay", ID: driver + "/" + week + "/" + charge.Source, Label: charge.Name, Date: week,
					State: "calculated", Note: "Estimated recurring charge for the selected week", Values: map[string]float64{"recurringCharges": float64(-amount)}})
				g.Metrics["recurringCharges"] += float64(-amount)
				g.Estimated["recurringCharges"] = true
			}
		}
	}
	return nil
}
