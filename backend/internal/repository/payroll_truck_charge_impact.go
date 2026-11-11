package repository

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/jackc/pgx/v5"
)

type truckChargeImpactPhase struct {
	Truck string `json:"truck"`
	Type  string `json:"type"`
	Week  string `json:"week"`
}

type truckChargeImpactAudit struct {
	xid, truck, week string
}

type truckChargeImpactProof struct {
	valid  bool
	phases map[truckChargeImpactPhase]map[string]bool
	audits map[int64]truckChargeImpactAudit
}

func newTruckChargeImpactProof() truckChargeImpactProof {
	return truckChargeImpactProof{valid: true, phases: map[truckChargeImpactPhase]map[string]bool{}, audits: map[int64]truckChargeImpactAudit{}}
}

func (p *truckChargeImpactProof) capture(table, operation, xid string, routes []map[string]json.RawMessage) {
	if !p.valid {
		return
	}
	if len(routes) == 0 || xid == "" || (table != "truck_charge_phases" && (table != "truck_settlement_events" || operation != "INSERT")) {
		p.valid = false
		return
	}
	for _, route := range routes {
		value := func(key string) string {
			var s string
			_ = json.Unmarshal(route[key], &s)
			return s
		}
		truck, week := value("truck_id"), value("week_start")
		if _, err := chargeWeek(week); truck == "" || err != nil {
			p.valid = false
			return
		}
		if table == "truck_settlement_events" {
			id, err := strconv.ParseInt(string(route["id"]), 10, 64)
			if err != nil || id <= 0 {
				p.valid = false
				return
			}
			p.audits[id] = truckChargeImpactAudit{xid, truck, week}
			continue
		}
		typ := value("type_id")
		if typ == "" {
			p.valid = false
			return
		}
		phase := truckChargeImpactPhase{truck, typ, week}
		if p.phases[phase] == nil {
			p.phases[phase] = map[string]bool{}
		}
		p.phases[phase][xid] = true
	}
}

// Both dispatch and synchronous publication use the captured old/new phase
// identities. An audit is incidental only when it matches a real phase change
// in the same transaction. Other truck audits retain their normal routing.
func (p truckChargeImpactProof) affectedWeeks(ctx context.Context, tx pgx.Tx) (map[string]bool, error) {
	if len(p.audits) > 0 {
		ids := make([]int64, 0, len(p.audits))
		for id := range p.audits {
			ids = append(ids, id)
		}
		rows, err := tx.Query(ctx, `SELECT id,truck_id::text,week_start::text,coalesce(details->>'typeId','')
 FROM truck_settlement_events WHERE id=ANY($1::bigint[]) AND action='charge'`, ids)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		seen := 0
		for rows.Next() {
			var id int64
			var phase truckChargeImpactPhase
			if err = rows.Scan(&id, &phase.Truck, &phase.Week, &phase.Type); err != nil {
				return nil, err
			}
			audit := p.audits[id]
			if phase.Truck != audit.truck || phase.Week != audit.week || !p.phases[phase][audit.xid] {
				return nil, nil
			}
			seen++
		}
		if err = rows.Err(); err != nil {
			return nil, err
		}
		if seen != len(ids) {
			return nil, nil
		}
	}

	phases := make([]truckChargeImpactPhase, 0, len(p.phases))
	for phase := range p.phases {
		phases = append(phases, phase)
	}
	body, err := json.Marshal(phases)
	if err != nil {
		return nil, err
	}
	// A recurring base is selected as of the statement's Monday. Prior weeks
	// cannot see a later phase. After a successor, this edit cannot contribute
	// to membership or carry if there is no saved collection or saved statement
	// for its destinations. Include both current and historical owner routes.
	rows, err := tx.Query(ctx, `WITH phases AS MATERIALIZED (
 SELECT * FROM jsonb_to_recordset($1::jsonb) x(truck uuid,type uuid,week date)
), windows AS MATERIALIZED (
 SELECT x.week,CASE WHEN n.week IS NOT NULL
 AND NOT EXISTS(SELECT 1 FROM pay_source_collections c WHERE c.source='truck_charge:'||x.truck::text||':'||x.type::text)
 AND NOT EXISTS(SELECT 1 FROM investor_pay_weeks w WHERE w.truck_id=x.truck AND w.week_start>=x.week)
 AND NOT EXISTS(
  SELECT 1 FROM investors i WHERE i.driver_id IS NOT NULL
  AND (EXISTS(SELECT 1 FROM trucks t WHERE t.id=x.truck AND t.owner_id=i.id)
    OR EXISTS(SELECT 1 FROM truck_ownership_history h WHERE h.truck_id=x.truck AND h.owner_id=i.id)
    OR EXISTS(SELECT 1 FROM truck_settlement_terms t WHERE t.truck_id=x.truck AND t.owner_id=i.id))
  AND (EXISTS(SELECT 1 FROM driver_pay_weeks d WHERE d.driver_id=i.driver_id AND d.week_start>=x.week)
    OR EXISTS(SELECT 1 FROM payroll_settlements d WHERE d.driver_id=i.driver_id AND d.week_start>=x.week))
 ) THEN n.week END AS ends_before
 FROM phases x LEFT JOIN LATERAL (SELECT p.week_start AS week FROM truck_charge_phases p
 WHERE p.truck_id=x.truck AND p.type_id=x.type AND p.week_start>x.week ORDER BY p.week_start LIMIT 1) n ON true
), periods AS (
 SELECT week_start FROM payroll_projection_weeks UNION SELECT week_start FROM payroll_projection_jobs
)
 SELECT p.week_start::text FROM periods p WHERE EXISTS(
 SELECT 1 FROM windows w WHERE p.week_start>=w.week AND (w.ends_before IS NULL OR p.week_start<w.ends_before)
 )`, body)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	weeks := map[string]bool{}
	for rows.Next() {
		var week string
		if err = rows.Scan(&week); err != nil {
			return nil, err
		}
		weeks[week] = true
	}
	return weeks, rows.Err()
}
