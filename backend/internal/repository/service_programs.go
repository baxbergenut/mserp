package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

const OilChangeProgramID = "00000000-0000-4000-8000-000000000020"

func (r *MaintenanceRepository) ProgramAssignees(ctx context.Context) ([]TaskUser, []TaskUser, error) {
	people, err := NewAuthRepository(r.pool).TaskUsers(ctx)
	if err != nil {
		return nil, nil, err
	}
	rows, err := r.pool.Query(ctx, `SELECT id::text,name FROM app_roles ORDER BY lower(name),id`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	roles := []TaskUser{}
	for rows.Next() {
		var v TaskUser
		if err = rows.Scan(&v.ID, &v.Name); err != nil {
			return nil, nil, err
		}
		roles = append(roles, v)
	}
	return people, roles, rows.Err()
}

type ProgramEnrollment struct {
	ProgramID string `json:"programId"`
	TruckID   string `json:"truckId"`
}

func (r *MaintenanceRepository) ProgramEnrollments(ctx context.Context) ([]ProgramEnrollment, error) {
	rows, err := r.pool.Query(ctx, `SELECT program_id::text,truck_id::text FROM service_program_trucks WHERE active ORDER BY program_id,truck_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []ProgramEnrollment{}
	for rows.Next() {
		var v ProgramEnrollment
		if err = rows.Scan(&v.ProgramID, &v.TruckID); err != nil {
			return nil, err
		}
		result = append(result, v)
	}
	return result, rows.Err()
}

type ServiceProgram struct {
	ID                  string   `json:"id"`
	Name                string   `json:"name"`
	Instructions        string   `json:"instructions"`
	LegacyPM            bool     `json:"legacyPM"`
	IntervalMiles       *int     `json:"intervalMiles"`
	IntervalDays        *int     `json:"intervalDays"`
	IntervalMonths      *int     `json:"intervalMonths"`
	WarningMiles        int      `json:"warningMiles"`
	WarningDays         int      `json:"warningDays"`
	Active              bool     `json:"active"`
	UseDefaultAssignees bool     `json:"useDefaultAssignees"`
	AssigneeIDs         []string `json:"assigneeIds"`
	AssigneeRoleIDs     []string `json:"assigneeRoleIds"`
	Version             int      `json:"version"`
	TruckCount          int      `json:"truckCount"`
	UpcomingCount       int      `json:"upcomingCount"`
	OverdueCount        int      `json:"overdueCount"`
}

type ProgramTruck struct {
	MaintenanceDue
	ProgramID         string `json:"programId"`
	ProgramName       string `json:"programName"`
	Active            bool   `json:"active"`
	Enrolled          bool   `json:"enrolled"`
	MemberVersion     int    `json:"memberVersion"`
	RecordVersion     int    `json:"recordVersion"`
	BaselineVersion   int    `json:"baselineVersion"`
	CurrentSource     string `json:"currentSource"`
	MilesSinceService *int   `json:"milesSinceService"`
	OverrideInterval  bool   `json:"overrideInterval"`
	IntervalMiles     *int   `json:"intervalMiles"`
	IntervalDays      *int   `json:"intervalDays"`
	IntervalMonths    *int   `json:"intervalMonths"`
}

type ProgramTruckInput struct {
	TruckID          string `json:"truckId"`
	Version          int    `json:"version"`
	Active           bool   `json:"active"`
	OverrideInterval bool   `json:"overrideInterval"`
	IntervalMiles    *int   `json:"intervalMiles"`
	IntervalDays     *int   `json:"intervalDays"`
	IntervalMonths   *int   `json:"intervalMonths"`
}

func validProgramInterval(miles, days, months *int) bool {
	return (miles != nil || days != nil || months != nil) && !(days != nil && months != nil) &&
		(miles == nil || *miles >= 1 && *miles <= 2000000000) &&
		(days == nil || *days >= 1 && *days <= 36500) && (months == nil || *months >= 1 && *months <= 1200)
}

func (r *MaintenanceRepository) Programs(ctx context.Context) ([]ServiceProgram, error) {
	return servicePrograms(ctx, r.pool)
}

func servicePrograms(ctx context.Context, q chargeQuery) ([]ServiceProgram, error) {
	rows, err := q.Query(ctx, `SELECT p.id,p.name,p.instructions,p.legacy_pm,
 CASE WHEN p.legacy_pm AND s.id IS NOT NULL THEN s.interval_miles ELSE p.interval_miles END,
 CASE WHEN p.legacy_pm AND s.id IS NOT NULL THEN s.interval_days ELSE p.interval_days END,
 CASE WHEN p.legacy_pm AND s.id IS NOT NULL THEN s.interval_months ELSE p.interval_months END,
 CASE WHEN p.legacy_pm AND s.id IS NOT NULL THEN s.warning_miles ELSE p.warning_miles END,
 CASE WHEN p.legacy_pm AND s.id IS NOT NULL THEN s.warning_days ELSE p.warning_days END,
 p.active AND coalesce(s.enabled,true),p.use_default_assignees,p.assignee_ids::text[],p.assignee_role_ids::text[],p.version,
 (SELECT count(*) FROM service_program_trucks m WHERE m.program_id=p.id AND m.active)
 FROM service_programs p LEFT JOIN maintenance_schedules s ON p.legacy_pm AND s.truck_id IS NULL AND s.service_code='pm' ORDER BY p.legacy_pm DESC,lower(p.name),p.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []ServiceProgram{}
	for rows.Next() {
		var p ServiceProgram
		if err = rows.Scan(&p.ID, &p.Name, &p.Instructions, &p.LegacyPM, &p.IntervalMiles, &p.IntervalDays, &p.IntervalMonths, &p.WarningMiles, &p.WarningDays, &p.Active, &p.UseDefaultAssignees, &p.AssigneeIDs, &p.AssigneeRoleIDs, &p.Version, &p.TruckCount); err != nil {
			return nil, err
		}
		result = append(result, p)
	}
	return result, rows.Err()
}

func (r *MaintenanceRepository) SaveProgram(ctx context.Context, p ServiceProgram, actor string) (ServiceProgram, error) {
	p.Name = strings.Join(strings.Fields(p.Name), " ")
	p.Instructions = strings.TrimSpace(p.Instructions)
	if utf8.RuneCountInString(p.Name) < 1 || utf8.RuneCountInString(p.Name) > 100 || utf8.RuneCountInString(p.Instructions) > 5000 || !validProgramInterval(p.IntervalMiles, p.IntervalDays, p.IntervalMonths) || p.WarningMiles < 0 || p.WarningMiles > 2000000000 || p.WarningDays < 0 || p.WarningDays > 36500 {
		return p, fmt.Errorf("%w: enter a name, a positive interval and nonnegative reminder thresholds", ErrMaintenanceInvalid)
	}
	if p.AssigneeIDs == nil {
		p.AssigneeIDs = []string{}
	}
	if p.AssigneeRoleIDs == nil {
		p.AssigneeRoleIDs = []string{}
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return p, err
	}
	defer tx.Rollback(ctx)
	var before []byte
	if p.ID != "" {
		if err = tx.QueryRow(ctx, `SELECT legacy_pm,to_jsonb(p) FROM service_programs p WHERE id=$1 AND version=$2 FOR UPDATE`, p.ID, p.Version).Scan(&p.LegacyPM, &before); err != nil {
			if err == pgx.ErrNoRows {
				return p, ErrMaintenanceConflict
			}
			return p, err
		}
	} else {
		p.LegacyPM = false
	}
	var valid bool
	if err = tx.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM unnest($1::uuid[]) u(id) WHERE NOT EXISTS(SELECT 1 FROM app_users a WHERE a.id=u.id AND a.active)) AND NOT EXISTS(SELECT 1 FROM unnest($2::uuid[]) r(id) WHERE NOT EXISTS(SELECT 1 FROM app_roles a WHERE a.id=r.id))`, p.AssigneeIDs, p.AssigneeRoleIDs).Scan(&valid); err != nil {
		return p, err
	}
	if !valid {
		return p, fmt.Errorf("%w: choose active people and existing roles", ErrMaintenanceInvalid)
	}
	if p.ID == "" {
		err = tx.QueryRow(ctx, `INSERT INTO service_programs(name,instructions,interval_miles,interval_days,interval_months,warning_miles,warning_days,active,use_default_assignees,assignee_ids,assignee_role_ids) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING id,version`, p.Name, p.Instructions, p.IntervalMiles, p.IntervalDays, p.IntervalMonths, p.WarningMiles, p.WarningDays, p.Active, p.UseDefaultAssignees, p.AssigneeIDs, p.AssigneeRoleIDs).Scan(&p.ID, &p.Version)
	} else {
		_, err = tx.Exec(ctx, `UPDATE service_programs SET name=$2,instructions=$3,interval_miles=$4,interval_days=$5,interval_months=$6,warning_miles=$7,warning_days=$8,active=$9,use_default_assignees=$10,assignee_ids=$11,assignee_role_ids=$12,version=version+1 WHERE id=$1`, p.ID, p.Name, p.Instructions, p.IntervalMiles, p.IntervalDays, p.IntervalMonths, p.WarningMiles, p.WarningDays, p.Active, p.UseDefaultAssignees, p.AssigneeIDs, p.AssigneeRoleIDs)
		p.Version++
	}
	if err != nil {
		return p, err
	}
	if p.LegacyPM {
		_, err = tx.Exec(ctx, `INSERT INTO maintenance_schedules(service_code,interval_miles,interval_days,interval_months,warning_miles,warning_days,enabled) VALUES('pm',$1,$2,$3,$4,$5,$6) ON CONFLICT((coalesce(truck_id,'00000000-0000-0000-0000-000000000000'::uuid)),service_code) DO UPDATE SET interval_miles=excluded.interval_miles,interval_days=excluded.interval_days,interval_months=excluded.interval_months,warning_miles=excluded.warning_miles,warning_days=excluded.warning_days,enabled=excluded.enabled,version=maintenance_schedules.version+1`, p.IntervalMiles, p.IntervalDays, p.IntervalMonths, p.WarningMiles, p.WarningDays, p.Active)
		if err != nil {
			return p, err
		}
	}
	if err = accessAudit(ctx, tx, actor, "service_program_saved", p.ID, map[string]any{"before": json.RawMessage(before), "after": p}); err != nil {
		return p, err
	}
	return p, tx.Commit(ctx)
}

// The last work order is selected independently for each program. A manual
// baseline remains effective only while that exact source revision is unchanged.
const programCycleJoin = ` LEFT JOIN LATERAL (
 SELECT v.id,v.version,v.service_date,v.odometer FROM maintenance_records v
 WHERE v.truck_id=t.id AND v.status='completed' AND
 (p.id=ANY(v.program_ids) OR (p.legacy_pm AND v.program_ids IS NULL AND EXISTS(SELECT 1 FROM jsonb_array_elements(v.services) s WHERE s->>'trackingCode' IN ('pm','oil'))))
 ORDER BY v.service_date DESC,v.created_at DESC,v.id DESC LIMIT 1
 ) original ON true
 LEFT JOIN maintenance_pm_baselines b ON p.legacy_pm AND b.truck_id=t.id
 LEFT JOIN LATERAL (
 SELECT CASE WHEN p.legacy_pm THEN b.service_date ELSE member.baseline_date END AS service_date,
 CASE WHEN p.legacy_pm THEN b.odometer ELSE member.baseline_odometer END AS odometer,
 CASE WHEN p.legacy_pm THEN b.source_record_id ELSE member.baseline_source_id END AS source_id,
 CASE WHEN p.legacy_pm THEN b.source_version ELSE member.baseline_source_version END AS source_version,
 CASE WHEN p.legacy_pm THEN b.cycle_id ELSE member.baseline_cycle_id END AS cycle_id,
 CASE WHEN p.legacy_pm THEN coalesce(b.version,0) ELSE member.version END AS version
 ) baseline ON true
 LEFT JOIN LATERAL (
 SELECT original.id,coalesce(original.version,0) AS record_version,baseline.version AS baseline_version,
 CASE WHEN baseline.service_date IS NOT NULL AND baseline.source_id IS NOT DISTINCT FROM original.id AND baseline.source_version=coalesce(original.version,0) THEN baseline.service_date ELSE original.service_date END AS service_date,
 CASE WHEN baseline.service_date IS NOT NULL AND baseline.source_id IS NOT DISTINCT FROM original.id AND baseline.source_version=coalesce(original.version,0) THEN baseline.odometer ELSE original.odometer END AS odometer,
 CASE WHEN baseline.service_date IS NOT NULL AND baseline.source_id IS NOT DISTINCT FROM original.id AND baseline.source_version=coalesce(original.version,0) THEN baseline.cycle_id ELSE original.id END AS cycle_id
 ) m ON true `

func programTrucks(ctx context.Context, q chargeQuery, program, truck string, today time.Time) ([]ProgramTruck, error) {
	programs, err := servicePrograms(ctx, q)
	if err != nil {
		return nil, err
	}
	byID := map[string]ServiceProgram{}
	for _, p := range programs {
		byID[p.ID] = p
	}
	rows, err := q.Query(ctx, `SELECT p.id,p.name,t.id,t.unit_number,t.active,member.active,member.version,m.id::text,m.record_version,m.baseline_version,m.service_date::text,m.odometer,m.cycle_id::text,r.reading_date::text,r.mileage,r.reported_at,coalesce(r.source,''),
 CASE WHEN p.legacy_pm AND s.id IS NOT NULL THEN true ELSE member.override_interval END,
 CASE WHEN p.legacy_pm AND s.id IS NOT NULL THEN s.interval_miles ELSE member.interval_miles END,
 CASE WHEN p.legacy_pm AND s.id IS NOT NULL THEN s.interval_days ELSE member.interval_days END,
 CASE WHEN p.legacy_pm AND s.id IS NOT NULL THEN s.interval_months ELSE member.interval_months END,
 coalesce(s.enabled,true),s.warning_miles,s.warning_days
 FROM service_program_trucks member JOIN service_programs p ON p.id=member.program_id JOIN trucks t ON t.id=member.truck_id
 `+programCycleJoin+maintenanceCurrentReadingJoin+`
 LEFT JOIN maintenance_schedules s ON p.legacy_pm AND s.truck_id=t.id AND s.service_code='pm'
 WHERE ($1='' OR p.id=NULLIF($1,'')::uuid) AND ($2='' OR t.id=NULLIF($2,'')::uuid) ORDER BY lower(p.name),t.unit_number,t.id`, program, truck)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []ProgramTruck{}
	for rows.Next() {
		var v ProgramTruck
		var enabled bool
		var warningMiles, warningDays *int
		if err = rows.Scan(&v.ProgramID, &v.ProgramName, &v.TruckID, &v.UnitNumber, &v.Active, &v.Enrolled, &v.MemberVersion, &v.LastRecordID, &v.RecordVersion, &v.BaselineVersion, &v.LastDate, &v.LastMiles, &v.CycleID, &v.CurrentDate, &v.CurrentMiles, &v.CurrentReportedAt, &v.CurrentSource, &v.OverrideInterval, &v.IntervalMiles, &v.IntervalDays, &v.IntervalMonths, &enabled, &warningMiles, &warningDays); err != nil {
			return nil, err
		}
		p := byID[v.ProgramID]
		v.ServiceCode = "pm"
		v.Schedule = &MaintenanceSchedule{Enabled: p.Active && v.Enrolled && enabled && v.Active, IntervalMiles: p.IntervalMiles, IntervalDays: p.IntervalDays, IntervalMonths: p.IntervalMonths, WarningMiles: p.WarningMiles, WarningDays: p.WarningDays}
		if v.OverrideInterval {
			v.Schedule.IntervalMiles = v.IntervalMiles
			v.Schedule.IntervalDays = v.IntervalDays
			v.Schedule.IntervalMonths = v.IntervalMonths
		}
		if warningMiles != nil {
			v.Schedule.WarningMiles = *warningMiles
		}
		if warningDays != nil {
			v.Schedule.WarningDays = *warningDays
		}
		calculateMaintenanceDue(&v.MaintenanceDue, today)
		if v.CurrentMiles != nil && v.LastMiles != nil {
			n := *v.CurrentMiles - *v.LastMiles
			v.MilesSinceService = &n
		}
		result = append(result, v)
	}
	return result, rows.Err()
}

func maintenanceToday() time.Time {
	zone, _ := time.LoadLocation("America/New_York")
	day, _ := time.Parse(time.DateOnly, time.Now().In(zone).Format(time.DateOnly))
	return day
}
func (r *MaintenanceRepository) ProgramTrucks(ctx context.Context, program, truck string) ([]ProgramTruck, error) {
	return programTrucks(ctx, r.pool, program, truck, maintenanceToday())
}

func (r *MaintenanceRepository) SaveProgramTrucks(ctx context.Context, program string, items []ProgramTruckInput, actor string) error {
	if len(items) < 1 || len(items) > 500 {
		return ErrMaintenanceInvalid
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var legacy bool
	if err = tx.QueryRow(ctx, `SELECT legacy_pm FROM service_programs WHERE id=$1 FOR SHARE`, program).Scan(&legacy); err != nil {
		return mapNotFound(err)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].TruckID < items[j].TruckID })
	seen := map[string]bool{}
	for _, v := range items {
		if seen[v.TruckID] || v.Version < 0 || v.OverrideInterval && !validProgramInterval(v.IntervalMiles, v.IntervalDays, v.IntervalMonths) {
			return ErrMaintenanceInvalid
		}
		seen[v.TruckID] = true
		var id string
		if err = tx.QueryRow(ctx, `SELECT id FROM trucks WHERE id=$1 FOR UPDATE`, v.TruckID).Scan(&id); err != nil {
			return mapNotFound(err)
		}
		var before []byte
		var version int
		err = tx.QueryRow(ctx, `SELECT version,to_jsonb(m) FROM service_program_trucks m WHERE program_id=$1 AND truck_id=$2 FOR UPDATE`, program, v.TruckID).Scan(&version, &before)
		if err != nil && err != pgx.ErrNoRows {
			return err
		}
		if version != v.Version {
			return ErrMaintenanceConflict
		}
		_, err = tx.Exec(ctx, `INSERT INTO service_program_trucks(program_id,truck_id,active,override_interval,interval_miles,interval_days,interval_months) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(program_id,truck_id) DO UPDATE SET active=excluded.active,override_interval=excluded.override_interval,interval_miles=excluded.interval_miles,interval_days=excluded.interval_days,interval_months=excluded.interval_months,version=service_program_trucks.version+1`, program, v.TruckID, v.Active, v.OverrideInterval, v.IntervalMiles, v.IntervalDays, v.IntervalMonths)
		if err != nil {
			return err
		}
		if legacy {
			if v.OverrideInterval {
				_, err = tx.Exec(ctx, `INSERT INTO maintenance_schedules(truck_id,service_code,interval_miles,interval_days,interval_months,warning_miles,warning_days,enabled) SELECT $1,'pm',$2,$3,$4,p.warning_miles,p.warning_days,true FROM service_programs p WHERE p.id=$5 ON CONFLICT((coalesce(truck_id,'00000000-0000-0000-0000-000000000000'::uuid)),service_code) DO UPDATE SET interval_miles=excluded.interval_miles,interval_days=excluded.interval_days,interval_months=excluded.interval_months,enabled=true,version=maintenance_schedules.version+1`, v.TruckID, v.IntervalMiles, v.IntervalDays, v.IntervalMonths, program)
			} else {
				_, err = tx.Exec(ctx, `DELETE FROM maintenance_schedules WHERE truck_id=$1 AND service_code='pm'`, v.TruckID)
			}
			if err != nil {
				return err
			}
		}
		if err = accessAudit(ctx, tx, actor, "service_program_truck_saved", v.TruckID, map[string]any{"programId": program, "before": json.RawMessage(before), "after": v}); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (r *MaintenanceRepository) SaveProgramBaseline(ctx context.Context, program string, in MaintenancePMInput, memberVersion int) error {
	if program == OilChangeProgramID {
		in.MemberVersion = &memberVersion
		return r.SavePMBaseline(ctx, in)
	}
	date, err := time.Parse(time.DateOnly, in.LastDate)
	if err != nil || date.After(maintenanceToday()) || in.LastMiles != nil && (*in.LastMiles < 0 || *in.LastMiles > 2000000000) {
		return fmt.Errorf("%w: enter a service date through today and a nonnegative odometer", ErrMaintenanceInvalid)
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var truck string
	if err = tx.QueryRow(ctx, `SELECT id FROM trucks WHERE id=$1 FOR UPDATE`, in.TruckID).Scan(&truck); err != nil {
		return mapNotFound(err)
	}
	values, err := programTrucks(ctx, tx, program, truck, maintenanceToday())
	if err != nil {
		return err
	}
	if len(values) != 1 {
		return ErrNotFound
	}
	v := values[0]
	if v.MemberVersion != memberVersion || v.RecordVersion != in.RecordVersion || pmRecordID(v.LastRecordID) != pmRecordID(in.RecordID) {
		return ErrMaintenanceConflict
	}
	if in.LastMiles != nil {
		if err = checkMaintenanceReading(ctx, tx, truck, date, *in.LastMiles, pmRecordID(v.LastRecordID), ""); err != nil {
			return err
		}
	}
	_, err = tx.Exec(ctx, `UPDATE service_program_trucks SET baseline_date=$3,baseline_odometer=$4,baseline_source_id=$5,baseline_source_version=$6,baseline_cycle_id=gen_random_uuid(),version=version+1 WHERE program_id=$1 AND truck_id=$2`, program, truck, date, in.LastMiles, v.LastRecordID, v.RecordVersion)
	if err != nil {
		return err
	}
	if err = accessAudit(ctx, tx, in.ActorID, "service_program_baseline", truck, map[string]any{"programId": program, "before": v, "after": in}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
