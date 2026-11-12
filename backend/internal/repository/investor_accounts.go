package repository

import (
	"context"
	"github.com/jackc/pgx/v5"
	"time"
)

type InvestorAccountTruck struct {
	ID         string `json:"id"`
	UnitNumber string `json:"unitNumber"`
	OwnerName  string `json:"ownerName"`
}

func investorAccountTrucks(ctx context.Context, q chargeQuery) ([]InvestorAccountTruck, error) {
	rows, err := q.Query(ctx, `SELECT t.id::text,t.unit_number,i.full_name FROM trucks t JOIN investors i ON i.id=t.owner_id
 WHERE t.active AND i.active AND investor_truck_account_eligible(t.id,i.id,(now() AT TIME ZONE 'America/New_York')::date,(now() AT TIME ZONE 'America/New_York')::date+1)
 ORDER BY lower(i.full_name),t.unit_number,t.id`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[InvestorAccountTruck])
}

func investorAccountEligible(ctx context.Context, q chargeQuery, truck, owner, week string) (bool, error) {
	var eligible bool
	err := q.QueryRow(ctx, `SELECT investor_truck_account_eligible($1,$2,$3::date,$3::date+7)`, truck, owner, week).Scan(&eligible)
	return eligible, err
}

func payrollAccountIDs(ctx context.Context) []string {
	if payrollDriverIDs(ctx) == nil {
		return nil
	}
	return append(append([]string{}, payrollDriverIDs(ctx)...), payrollTruckIDs(ctx)...)
}

func addTruckInstallmentLoads(ctx context.Context, q chargeQuery, loads map[string]map[string]bool, trucks []string) error {
	if len(trucks) == 0 {
		return nil
	}
	rows, err := q.Query(ctx, `SELECT DISTINCT coalesce(e.transfer_truck_id,a.truck_id)::text,date_trunc('week',e.service_date)::date::text
 FROM `+payrollBoardEntriesSQL+` e LEFT JOIN LATERAL (
 SELECT (array_agg(DISTINCT truck_id))[1] truck_id FROM truck_driver_assignments
 WHERE driver_id=e.driver_id AND assigned_at<((e.source_date+1)::timestamp AT TIME ZONE 'America/New_York')
 AND (unassigned_at IS NULL OR unassigned_at>=((e.source_date+1)::timestamp AT TIME ZONE 'America/New_York'))
 HAVING count(DISTINCT truck_id)=1) a ON true
 WHERE NOT e.deleted AND e.day_status='' AND btrim(e.load_number)<>''
 AND coalesce(e.transfer_truck_id,a.truck_id) IS NOT NULL
 AND ($1::uuid[] IS NULL OR coalesce(e.transfer_truck_id,a.truck_id)=ANY($1::uuid[]))`, trucks)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, week string
		if err = rows.Scan(&id, &week); err != nil {
			return err
		}
		if loads[id] == nil {
			loads[id] = map[string]bool{}
		}
		loads[id][week] = true
	}
	return rows.Err()
}

func truckInstallmentOwner(ctx context.Context, q chargeQuery, truck, week string) (string, error) {
	var owner string
	err := q.QueryRow(ctx, `SELECT s.owner_id::text FROM trucks t JOIN LATERAL (`+latestTruckTermForWeekSQL+`) s ON true
 JOIN investors i ON i.id=s.owner_id WHERE t.id=$2 AND t.active AND i.active AND NOT i.is_company
 AND investor_truck_account_eligible(t.id,i.id,$1::date,$1::date+7)
 AND `+truckTermCoversWeekSQL("$1::date")+`
 AND NOT EXISTS(SELECT 1 FROM investor_pay_weeks w WHERE w.truck_id=t.id AND w.week_start>=$1::date AND w.finalized)`, week, truck).Scan(&owner)
	if err == pgx.ErrNoRows {
		return "", chargeInvalid("Select an active investor truck operated by someone other than its owner, with an agreement and open statements")
	}
	return owner, err
}

func createTruckInstallment(ctx context.Context, tx pgx.Tx, c ChargeCreate, actor string) ([]string, error) {
	owner, err := truckInstallmentOwner(ctx, tx, c.TruckID, c.StartWeek)
	if err != nil {
		return nil, err
	}
	var id string
	err = tx.QueryRow(ctx, `INSERT INTO driver_charge_schedules(truck_id,owner_id,kind,name,direction,start_week,end_week,eligibility,total,installment_count)
 VALUES($1,$2,'installment',$3,'charge',$4::date,$5::date,$6,$7::numeric,$8) RETURNING id::text`, c.TruckID, owner, c.Name, c.StartWeek, c.EndWeek, c.Eligibility, c.Total, c.Installments).Scan(&id)
	if err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO driver_charge_phases(schedule_id,week_start,amount) VALUES($1,$2::date,$3::numeric)`, id, c.StartWeek, c.Amount); err != nil {
		return nil, err
	}
	if err = chargeAudit(ctx, tx, id, "", actor, "created", c); err != nil {
		return nil, err
	}
	return []string{id}, commitPayrollMutation(ctx, tx)
}

func previewTruckInstallment(ctx context.Context, q chargeQuery, c ChargeCreate) ([]ChargeOccurrence, error) {
	if _, err := truckInstallmentOwner(ctx, q, c.TruckID, c.StartWeek); err != nil {
		return nil, err
	}
	loads := map[string]map[string]bool{}
	if err := addTruckInstallmentLoads(ctx, q, loads, []string{c.TruckID}); err != nil {
		return nil, err
	}
	s := ChargeSchedule{ID: "preview", TruckID: c.TruckID, DriverID: c.TruckID, Kind: "installment", Name: c.Name, Direction: "charge", StartWeek: c.StartWeek, EndWeek: c.EndWeek, Eligibility: c.Eligibility, Total: &c.Total, InstallmentCount: c.Installments, Phases: []ChargePhase{{WeekStart: c.StartWeek, Amount: c.Amount}}}
	return projectCharges(s, "2100-12-27", loads[c.TruckID], true)
}

func investorInstallments(ctx context.Context, tx pgx.Tx, truck, owner, week string) ([]ChargeOccurrence, error) {
	data, loads, err := chargeData(ctx, tx, truck)
	if err != nil {
		return nil, err
	}
	filtered := data
	filtered.Schedules = nil
	for _, s := range data.Schedules {
		if s.TruckID == truck && s.OwnerID == owner {
			filtered.Schedules = append(filtered.Schedules, s)
		}
	}
	all, err := generatedForWeek(filtered, loads, week)
	return all[truck], err
}

func finalizeInvestorInstallments(ctx context.Context, tx pgx.Tx, d *DriverPayDriver, actor string) error {
	data, loads, err := chargeData(ctx, tx, d.ID)
	if err != nil {
		return err
	}
	for i, row := range d.Edits.GeneratedCharges {
		if row.ConfirmedAt != nil {
			continue
		}
		found := false
		for j := range data.Schedules {
			s := &data.Schedules[j]
			if s.ID == row.ScheduleID && s.TruckID == d.ID && s.OwnerID == d.InvestorID {
				found = true
				if err = freezeChargesBefore(ctx, tx, s, d.Edits.WeekStart, loads[d.ID]); err != nil {
					return err
				}
			}
		}
		if !found {
			return ErrChargeConflict
		}
		row.Overridden = true
		n, e := chargeCents(row.Amount)
		if e != nil || n > 0 {
			return chargeInvalid("Invalid installment deduction")
		}
		if n < 0 {
			now := time.Now()
			row.ConfirmedAt = &now
			row.ConfirmedBy = &actor
		}
		if err = storeOccurrence(ctx, tx, row); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE driver_charge_schedules SET version=version+1 WHERE id=$1`, row.ScheduleID); err != nil {
			return err
		}
		d.Edits.GeneratedCharges[i] = row
	}
	return nil
}

func reopenInvestorInstallments(ctx context.Context, tx pgx.Tx, truck, week string) error {
	_, err := tx.Exec(ctx, `WITH changed AS (UPDATE driver_charge_occurrences o SET confirmed_at=NULL,confirmed_by=NULL,version=o.version+1
 FROM driver_charge_schedules s WHERE s.id=o.schedule_id AND s.truck_id=$1 AND o.week_start=$2::date AND o.confirmed_at IS NOT NULL RETURNING s.id)
 UPDATE driver_charge_schedules s SET version=s.version+1 FROM changed c WHERE s.id=c.id`, truck, week)
	return err
}

func truckOwnerSchedules(schedules []ChargeSchedule, truck, owner string) []ChargeSchedule {
	out := []ChargeSchedule{}
	for _, s := range schedules {
		if s.TruckID == truck && s.OwnerID == owner {
			out = append(out, s)
		}
	}
	return out
}
