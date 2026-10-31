package repository

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type payAssignmentDay struct {
	DriverID string `json:"driver"`
	Date     string `json:"date"`
}

type payAssignmentReference struct {
	payAssignmentDay
	credited   bool
	dispatcher PayPerson
}

type payAssignment struct {
	DailyAssignment
	employmentType string
}

func payAssignmentReferences(d DriverPayDriver, kind string, week time.Time) []payAssignmentReference {
	refs := []payAssignmentReference{}
	for _, l := range d.Loads {
		id, date := l.SourceDriverID, l.SourceDate
		if id == "" && kind == "driver" {
			id = d.ID
		}
		if date == "" {
			date = l.Date
		}
		if id != "" && date != "" {
			refs = append(refs, payAssignmentReference{payAssignmentDay{id, date}, l.TransferVersion > 0, PayPerson{l.CreditedDispatcherID, l.CreditedDispatcherName}})
		}
	}
	if len(d.Loads) > 0 {
		return refs
	}
	people := d.OperatingDrivers
	if kind == "driver" {
		people = []PayPerson{{ID: d.ID}}
	}
	for _, p := range people {
		for day := 0; day < 7; day++ {
			refs = append(refs, payAssignmentReference{payAssignmentDay: payAssignmentDay{p.ID, week.AddDate(0, 0, day).Format(time.DateOnly)}})
		}
	}
	return refs
}

// Labels describe the work's original dates, after financial routing is complete.
// Current profiles and the Sunday roster must not erase earlier assignments.
// Empty statements use the selected week's dated assignments; frozen labels stay frozen.
func decoratePayAssignments(ctx context.Context, tx pgx.Tx, kind string, report *DriverPayWeek) error {
	week, err := time.Parse(time.DateOnly, report.WeekStart)
	if err != nil {
		return err
	}
	references := make([][]payAssignmentReference, len(report.Drivers))
	requested := map[payAssignmentDay]bool{}
	days := []payAssignmentDay{}
	for i, d := range report.Drivers {
		if d.Settlement != nil && d.Settlement.Finalized {
			continue
		}
		references[i] = payAssignmentReferences(d, kind, week)
		for _, ref := range references[i] {
			if !requested[ref.payAssignmentDay] {
				requested[ref.payAssignmentDay] = true
				days = append(days, ref.payAssignmentDay)
			}
		}
	}
	if len(days) == 0 {
		return nil
	}
	body, err := json.Marshal(days)
	if err != nil {
		return err
	}
	query := payrollDatedAssignmentsSQL
	if full, _ := ctx.Value(payrollAuthoritativeKey{}).(bool); full {
		query = payrollOriginalAssignmentsSQL
	}
	rows, err := tx.Query(ctx, query, string(body))
	if err != nil {
		return err
	}
	assignments := map[payAssignmentDay]payAssignment{}
	for rows.Next() {
		var key payAssignmentDay
		var a payAssignment
		if err = rows.Scan(&key.DriverID, &key.Date, &a.TruckID, &a.TruckUnit, &a.DispatcherID, &a.DispatcherName, &a.employmentType); err != nil {
			rows.Close()
			return err
		}
		assignments[key] = a
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for i := range report.Drivers {
		d := &report.Drivers[i]
		if d.Settlement != nil && d.Settlement.Finalized {
			continue
		}
		trucks, dispatchers := map[string]string{}, map[string]string{}
		employmentTypes := map[string]bool{}
		for _, ref := range references[i] {
			a := assignments[ref.payAssignmentDay]
			if kind == "investor" && len(d.Loads) == 0 && a.TruckID != d.ID {
				continue
			}
			if ref.credited {
				a.DispatcherID, a.DispatcherName = ref.dispatcher.ID, ref.dispatcher.Name
			}
			if a.TruckID != "" || len(d.Loads) > 0 {
				trucks[a.TruckID] = a.TruckUnit
				employmentTypes[a.employmentType] = true
			}
			if a.DispatcherID != "" || len(d.Loads) > 0 {
				dispatchers[a.DispatcherID] = a.DispatcherName
			}
		}
		d.DispatcherID, d.DispatcherName = payAssignmentLabel(dispatchers)
		d.Dispatchers = payAssignmentPeople(dispatchers)
		if kind == "driver" {
			d.DisplayEmploymentTypes = nil
			for value := range employmentTypes {
				d.DisplayEmploymentTypes = append(d.DisplayEmploymentTypes, value)
			}
			sort.Strings(d.DisplayEmploymentTypes)
			d.TruckID, d.TruckUnit = payAssignmentLabel(trucks)
			if len(trucks) == 0 {
				d.TruckUnit = ""
			}
		}
	}
	return nil
}

func payAssignmentPeople(values map[string]string) []PayPerson {
	people := []PayPerson{}
	for id, name := range values {
		if name == "" {
			name = "Unassigned"
		}
		people = append(people, PayPerson{id, name})
	}
	sort.Slice(people, func(i, j int) bool {
		if people[i].Name != people[j].Name {
			return people[i].Name < people[j].Name
		}
		return people[i].ID < people[j].ID
	})
	return people
}

func payAssignmentLabel(values map[string]string) (string, string) {
	people := payAssignmentPeople(values)
	if len(people) == 0 {
		return "", "Unassigned"
	}
	if len(people) == 1 {
		return people[0].ID, people[0].Name
	}
	names := make([]string, 0, len(people))
	for _, p := range people {
		names = append(names, p.Name)
	}
	return "", strings.Join(names, " / ")
}

// Resolve dated assignments once per driver/day. The original SQL functions are
// retained below as an independent parity oracle, including their date boundaries.
const payrollDatedAssignmentsSQL = `WITH requested AS MATERIALIZED (
 SELECT r.driver,r.date,coalesce(t.main_driver_id,r.driver) operator
 FROM jsonb_to_recordset($1::jsonb) r(driver uuid,date date)
 LEFT JOIN driver_teams t ON t.team_driver_id=r.driver AND t.start_week<=r.date AND (t.end_week IS NULL OR r.date<t.end_week)
), days AS MATERIALIZED (SELECT DISTINCT date FROM requested), operators AS MATERIALIZED (SELECT DISTINCT operator,date FROM requested),
 ownership AS MATERIALIZED (
 SELECT DISTINCT ON(d.date,h.truck_id) d.date,h.truck_id,h.owner_id FROM days d JOIN truck_ownership_history h
 ON (NOT h.start_known OR h.assigned_at<((d.date+1)::timestamp AT TIME ZONE 'America/New_York'))
 AND (h.unassigned_at IS NULL OR h.unassigned_at>=((d.date+1)::timestamp AT TIME ZONE 'America/New_York')) ORDER BY d.date,h.truck_id,h.assigned_at DESC
), employment AS MATERIALIZED (
 SELECT DISTINCT ON(r.operator,r.date) r.operator,r.date,i.is_company,i.driver_id owner_driver
 FROM operators r JOIN truck_driver_assignments a ON a.driver_id=r.operator
 AND a.assigned_at<((r.date+1)::timestamp AT TIME ZONE 'America/New_York')
 AND (a.unassigned_at IS NULL OR a.unassigned_at>=((r.date+1)::timestamp AT TIME ZONE 'America/New_York'))
 JOIN ownership h ON h.truck_id=a.truck_id AND h.date=r.date JOIN investors i ON i.id=h.owner_id
 ORDER BY r.operator,r.date,a.assigned_at DESC
), assigned AS MATERIALIZED (
 SELECT r.operator,r.date,min(a.truck_id::text)::uuid truck_id FROM operators r JOIN truck_driver_assignments a ON a.driver_id=r.operator
 AND a.assigned_at<((r.date+1)::timestamp AT TIME ZONE 'America/New_York')
 AND (a.unassigned_at IS NULL OR a.unassigned_at>=((r.date+1)::timestamp AT TIME ZONE 'America/New_York'))
 AND (a.unassigned_at IS NULL OR a.assigned_at<a.unassigned_at)
 GROUP BY r.operator,r.date HAVING count(DISTINCT a.truck_id)=1
), dispatch AS MATERIALIZED (
 SELECT DISTINCT ON(r.operator,r.date) r.operator,r.date,h.dispatcher_id,h.dispatcher_name FROM operators r JOIN driver_dispatcher_assignments h ON h.driver_id=r.operator
 AND (NOT h.start_known OR h.assigned_at<((r.date+1)::timestamp AT TIME ZONE 'America/New_York'))
 AND (h.unassigned_at IS NULL OR h.unassigned_at>=((r.date+1)::timestamp AT TIME ZONE 'America/New_York'))
 AND (NOT h.start_known OR h.unassigned_at IS NULL OR h.assigned_at<h.unassigned_at)
 ORDER BY r.operator,r.date,h.assigned_at DESC
) SELECT r.driver::text,r.date::text,coalesce(a.truck_id::text,''),coalesce(t.unit_number,''),
 coalesce(h.dispatcher_id::text,''),coalesce(p.full_name,h.dispatcher_name,'Unassigned'),
 CASE WHEN a.truck_id IS NULL OR e.operator IS NULL THEN 'unassigned' WHEN e.is_company THEN 'company' WHEN e.owner_driver=r.driver THEN 'owner_operator' ELSE 'investor_driver' END
 FROM requested r LEFT JOIN assigned a USING(operator,date) LEFT JOIN trucks t ON t.id=a.truck_id
 LEFT JOIN dispatch h USING(operator,date) LEFT JOIN dispatchers p ON p.id=h.dispatcher_id LEFT JOIN employment e USING(operator,date)`

const payrollOriginalAssignmentsSQL = `WITH requested AS (
 SELECT driver,date,payroll_driver_at(driver,date) operator FROM jsonb_to_recordset($1::jsonb) AS r(driver uuid,date date)
 ) SELECT r.driver::text,r.date::text,coalesce(a.truck_id::text,''),coalesce(t.unit_number,''),
 coalesce(h.dispatcher_id::text,''),coalesce(p.full_name,h.dispatcher_name,'Unassigned'),
 CASE WHEN a.truck_id IS NULL THEN 'unassigned' ELSE driver_employment_type(r.driver,r.date) END
 FROM requested r LEFT JOIN LATERAL (
 SELECT min(a.truck_id::text)::uuid truck_id FROM truck_driver_assignments a WHERE a.driver_id=r.operator
 AND a.assigned_at<((r.date+1)::timestamp AT TIME ZONE 'America/New_York')
 AND (a.unassigned_at IS NULL OR a.unassigned_at>=((r.date+1)::timestamp AT TIME ZONE 'America/New_York'))
 AND (a.unassigned_at IS NULL OR a.assigned_at<a.unassigned_at)
 HAVING count(DISTINCT a.truck_id)=1
 ) a ON true LEFT JOIN trucks t ON t.id=a.truck_id
 LEFT JOIN LATERAL (
 SELECT h.dispatcher_id,h.dispatcher_name FROM driver_dispatcher_assignments h WHERE h.driver_id=r.operator
 AND (NOT h.start_known OR h.assigned_at<((r.date+1)::timestamp AT TIME ZONE 'America/New_York'))
 AND (h.unassigned_at IS NULL OR h.unassigned_at>=((r.date+1)::timestamp AT TIME ZONE 'America/New_York'))
 AND (NOT h.start_known OR h.unassigned_at IS NULL OR h.assigned_at<h.unassigned_at)
 ORDER BY h.assigned_at DESC LIMIT 1
 ) h ON true LEFT JOIN dispatchers p ON p.id=h.dispatcher_id`
