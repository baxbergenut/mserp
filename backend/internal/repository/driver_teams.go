package repository

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
)

var ErrDriverTeamConflict = errors.New("this team was changed elsewhere; reload before saving")

type DriverTeamPeriod struct {
	ID                  string  `json:"id"`
	MainDriverID        string  `json:"mainDriverId"`
	MainDriverName      string  `json:"mainDriverName"`
	TeamDriverID        string  `json:"teamDriverId"`
	TeamDriverName      string  `json:"teamDriverName"`
	MainActive          bool    `json:"mainActive"`
	TeamActive          bool    `json:"teamActive"`
	MainTerminationDate *string `json:"mainTerminationDate,omitempty"`
	TeamTerminationDate *string `json:"teamTerminationDate,omitempty"`
	StartWeek           string  `json:"startWeek"`
	EndWeek             *string `json:"endWeek"`
}
type DriverTeam struct {
	Version int                `json:"version"`
	Periods []DriverTeamPeriod `json:"periods"`
}
type DriverTeamInput struct {
	TeamDriverID string `json:"teamDriverId"`
	WeekStart    string `json:"weekStart"`
	Version      int    `json:"version"`
}

func readDriverTeams(ctx context.Context, q chargeQuery, driver string) (DriverTeam, error) {
	value := DriverTeam{Periods: []DriverTeamPeriod{}}
	if driver != "" {
		if err := q.QueryRow(ctx, `SELECT team_version FROM drivers WHERE id=$1`, driver).Scan(&value.Version); err != nil {
			return value, mapNotFound(err)
		}
	}
	rows, err := q.Query(ctx, `SELECT t.id::text,t.main_driver_id::text,m.full_name,t.team_driver_id::text,d.full_name,m.active,d.active,to_jsonb(m)->>'termination_date',to_jsonb(d)->>'termination_date',t.start_week::text,t.end_week::text
 FROM driver_teams t JOIN drivers m ON m.id=t.main_driver_id JOIN drivers d ON d.id=t.team_driver_id
 WHERE ($1='' OR t.main_driver_id::text=$1 OR t.team_driver_id::text=$1) AND t.start_week<coalesce(t.end_week,'infinity'::date)
 ORDER BY t.start_week,t.id`, driver)
	if err != nil {
		return value, err
	}
	defer rows.Close()
	for rows.Next() {
		var p DriverTeamPeriod
		if err = rows.Scan(&p.ID, &p.MainDriverID, &p.MainDriverName, &p.TeamDriverID, &p.TeamDriverName, &p.MainActive, &p.TeamActive, &p.MainTerminationDate, &p.TeamTerminationDate, &p.StartWeek, &p.EndWeek); err != nil {
			return value, err
		}
		value.Periods = append(value.Periods, p)
	}
	return value, rows.Err()
}
func (r *FleetRepository) GetDriverTeam(ctx context.Context, driver string) (DriverTeam, error) {
	return readDriverTeams(ctx, r.pool, driver)
}

func (r *FleetRepository) SetDriverTeam(ctx context.Context, main string, input DriverTeamInput, actor string) (DriverTeam, error) {
	empty := DriverTeam{}
	if _, err := chargeWeek(input.WeekStart); err != nil {
		return empty, err
	}
	if main == input.TeamDriverID {
		return empty, chargeInvalid("Choose a different team driver")
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback(ctx)
	// Exclusive transaction lock prevents concurrent payroll/fleet changes while
	// membership checks and the effective history update commit together.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(736281940)`); err != nil {
		return empty, err
	}
	before, err := readDriverTeams(ctx, tx, main)
	if err != nil {
		return empty, err
	}
	if input.Version != before.Version {
		return empty, ErrDriverTeamConflict
	}
	current := teamPeriodFor(before.Periods, main, input.WeekStart)
	noLater := true
	for _, p := range before.Periods {
		if p.StartWeek > input.WeekStart {
			noLater = false
		}
	}
	if noLater && ((current == nil && input.TeamDriverID == "") || (current != nil && current.MainDriverID == main && current.TeamDriverID == input.TeamDriverID)) {
		return before, nil
	}
	affected := map[string]bool{main: true}
	for _, p := range before.Periods {
		if p.EndWeek != nil && *p.EndWeek <= input.WeekStart {
			continue
		}
		if p.TeamDriverID == main {
			return empty, chargeInvalid("Change this team from the main driver's profile (%s)", p.MainDriverName)
		}
		affected[p.TeamDriverID] = true
	}
	if input.TeamDriverID != "" {
		affected[input.TeamDriverID] = true
		var active bool
		if err = tx.QueryRow(ctx, `SELECT active FROM drivers WHERE id=$1`, input.TeamDriverID).Scan(&active); err != nil {
			return empty, mapNotFound(err)
		}
		if !active {
			return empty, chargeInvalid("Select an active team driver")
		}
		if err = tx.QueryRow(ctx, `SELECT active FROM drivers WHERE id=$1`, main).Scan(&active); err != nil {
			return empty, err
		}
		if !active {
			return empty, chargeInvalid("Select an active main driver")
		}
	}
	ids := []string{}
	for id := range affected {
		ids = append(ids, id)
	}
	var blocked bool
	if input.TeamDriverID != "" {
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM driver_teams WHERE
 (main_driver_id=$1 OR team_driver_id=$1 OR team_driver_id=$2) AND main_driver_id<>$2
 AND (end_week IS NULL OR end_week>$3::date))`, input.TeamDriverID, main, input.WeekStart).Scan(&blocked); err != nil {
			return empty, err
		}
		if blocked {
			return empty, chargeInvalid("This driver already belongs to another team; end that team first")
		}
	}
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM payroll_settlements WHERE driver_id::text=ANY($1::text[]) AND week_start>=$2::date AND finalized)
 OR EXISTS(SELECT 1 FROM investor_pay_weeks p WHERE p.week_start>=$2::date AND p.finalized AND
 EXISTS(SELECT 1 FROM truck_driver_assignments a WHERE a.truck_id=p.truck_id AND a.driver_id::text=ANY($1::text[])))
 OR EXISTS(SELECT 1 FROM driver_pay_cost_collections WHERE driver_id::text=ANY($1::text[]) AND week_start>$2::date)`, ids, input.WeekStart).Scan(&blocked); err != nil {
		return empty, err
	}
	if blocked {
		return empty, chargeInvalid("Reopen affected driver/investor statements and choose a week after later saved Fuel/Toll collections before changing the team")
	}
	if input.TeamDriverID != "" {
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM `+grossBoardEntriesSQL+` WHERE driver_id=$1 AND service_date>=$2::date AND NOT deleted AND btrim(load_number)<>'')
 OR EXISTS(SELECT 1 FROM truck_driver_assignments WHERE driver_id=$1 AND (unassigned_at IS NULL OR (unassigned_at AT TIME ZONE 'America/New_York')::date>$2::date))
 OR EXISTS(SELECT 1 FROM driver_pay_weeks WHERE driver_id=$1 AND week_start>=$2::date)
 OR EXISTS(SELECT 1 FROM driver_escrow_payments p JOIN driver_escrows e ON e.id=p.escrow_id WHERE e.driver_id=$1 AND p.week_start>=$2::date)`, input.TeamDriverID, input.WeekStart).Scan(&blocked); err != nil {
			return empty, err
		}
		if blocked {
			return empty, chargeInvalid("Move the team driver's loads and truck assignment to the main driver and choose a week after their saved personal payroll")
		}
		records, e := readPayCostRecords(ctx, tx)
		if e != nil {
			return empty, e
		}
		fuel, toll := costBalance(records, input.TeamDriverID, input.WeekStart)
		if fuel != 0 || toll != 0 {
			return empty, chargeInvalid("Settle the team driver's existing Fuel/Toll balance before pairing")
		}
		data, loads, e := chargeData(ctx, tx, input.TeamDriverID)
		if e != nil {
			return empty, e
		}
		generated, e := generatedForWeek(data, loads, input.WeekStart)
		if e != nil {
			return empty, e
		}
		for _, row := range generated[input.TeamDriverID] {
			if row.Kind == "recurring" {
				n, _ := chargeCents(row.Amount)
				if n != 0 {
					return empty, chargeInvalid("Settle or pause the team driver's recurring charges before pairing; configure recurring charges on the main driver")
				}
			}
		}
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM (SELECT p.*,lead(p.week_start) OVER(PARTITION BY p.schedule_id ORDER BY p.week_start) next_week
 FROM driver_charge_phases p) p JOIN driver_charge_schedules s ON s.id=p.schedule_id
 WHERE s.driver_id=$1 AND s.kind='recurring' AND NOT p.paused AND (p.next_week IS NULL OR p.next_week>$2::date)
 AND (s.end_week IS NULL OR s.end_week>=$2::date))`, input.TeamDriverID, input.WeekStart).Scan(&blocked); err != nil {
			return empty, err
		}
		if blocked {
			return empty, chargeInvalid("Pause the team driver's recurring charges from this week, including future phases; configure recurring charges on the main driver")
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE driver_teams SET end_week=greatest(start_week,$2::date) WHERE main_driver_id=$1 AND (end_week IS NULL OR end_week>$2::date)`, main, input.WeekStart); err != nil {
		return empty, err
	}
	if input.TeamDriverID != "" {
		if _, err = tx.Exec(ctx, `INSERT INTO driver_teams(main_driver_id,team_driver_id,start_week) VALUES($1,$2,$3::date)`, main, input.TeamDriverID, input.WeekStart); err != nil {
			return empty, err
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE drivers SET team_version=team_version+1 WHERE id::text=ANY($1::text[])`, ids); err != nil {
		return empty, err
	}
	// A same-week open statement can already have collections. Rebase its
	// automatic sources atomically; explicit deductions must still fit the new
	// available amount. Later saved payments above were checked before mutation.
	for _, id := range ids {
		var saved bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM driver_pay_cost_collections WHERE driver_id=$1 AND week_start=$2::date)`, id, input.WeekStart).Scan(&saved); err != nil {
			return empty, err
		}
		if !saved {
			continue
		}
		card, e := currentDriverPayCosts(ctx, tx, DriverPayEdits{DriverID: id, WeekStart: input.WeekStart})
		if e != nil {
			return empty, e
		}
		if e = saveDriverPayCosts(ctx, tx, card, card.Edits, actor); e != nil {
			return empty, e
		}
	}
	after, err := readDriverTeams(ctx, tx, main)
	if err != nil {
		return empty, err
	}
	b, _ := json.Marshal(before)
	a, _ := json.Marshal(after)
	if _, err = tx.Exec(ctx, `INSERT INTO driver_team_events(driver_id,actor_id,effective_week,before_value,after_value) VALUES($1,nullif($2,'')::uuid,$3::date,$4,$5)`, main, actor, input.WeekStart, b, a); err != nil {
		return empty, err
	}
	return after, tx.Commit(ctx)
}

func teamPeriodFor(periods []DriverTeamPeriod, driver, week string) *DriverTeamPeriod {
	for i := range periods {
		p := &periods[i]
		if (p.MainDriverID == driver || p.TeamDriverID == driver) && p.StartWeek <= week && (p.EndWeek == nil || week < *p.EndWeek) {
			return p
		}
	}
	return nil
}

// Fleet/Relay identities and personal principals remain unchanged. Only the
// weekly statement destination follows dated team membership.
func routeTeamPay(ctx context.Context, tx pgx.Tx, report *DriverPayWeek) error {
	teams, err := readDriverTeams(ctx, tx, "")
	if err != nil {
		return err
	}
	index := map[string]int{}
	for i, d := range report.Drivers {
		index[d.ID] = i
	}
	for _, p := range teams.Periods {
		if p.StartWeek > report.WeekStart || (p.EndWeek != nil && *p.EndWeek <= report.WeekStart) {
			continue
		}
		mainIndex, exists := index[p.MainDriverID]
		teamIndex, hasTeam := index[p.TeamDriverID]
		if !exists && hasTeam {
			var d DriverPayDriver
			err = tx.QueryRow(ctx, `SELECT d.id::text,d.full_name,d.pay_type,d.pay_rate::text,d.is_owner_operator,
 coalesce(t.id::text,''),coalesce(t.unit_number,''),coalesce(dp.id::text,''),coalesce(dp.full_name,historical_dispatcher.dispatcher_name,'Unassigned')
 FROM drivers d `+weeklyAssignmentJoins+` WHERE d.id=$2`, report.WeekStart, p.MainDriverID).Scan(&d.ID, &d.FullName, &d.PayType, &d.PayRate, &d.IsOwnerOperator, &d.TruckID, &d.TruckUnit, &d.DispatcherID, &d.DispatcherName)
			if err != nil {
				return err
			}
			d.Loads = []DriverPayLoad{}
			d.FuelTotal = "0.00"
			d.TollTotal = "0.00"
			d.Edits = DriverPayEdits{DriverID: d.ID, WeekStart: report.WeekStart, Comments: map[string]string{}, Adjustments: []DriverPayAdjustment{}}
			mainIndex = len(report.Drivers)
			report.Drivers = append(report.Drivers, d)
			index[d.ID] = mainIndex
			exists = true
		}
		if !exists {
			continue
		}
		main := &report.Drivers[mainIndex]
		main.TeamDrivers = []PayPerson{{ID: p.TeamDriverID, Name: p.TeamDriverName}}
		weekEnd, _ := chargeWeek(report.WeekStart)
		lastDay := weekEnd.AddDate(0, 0, 6).Format("2006-01-02")
		if (!p.MainActive && (p.MainTerminationDate == nil || *p.MainTerminationDate <= lastDay)) || (!p.TeamActive && (p.TeamTerminationDate == nil || *p.TeamTerminationDate <= lastDay)) {
			main.TeamNotice = "A team member is terminated; review the team end week"
		}
		if !hasTeam {
			continue
		}
		member := report.Drivers[teamIndex]
		if len(member.Loads) > 0 || member.Edits.Version > 0 {
			return chargeInvalid("Review separate payroll or loads for team driver %s", member.FullName)
		}
		main.Edits.GeneratedCharges = append(main.Edits.GeneratedCharges, member.Edits.GeneratedCharges...)
		// Only the main driver's escrow is collected while paired. The team member's
		// individual escrow ledger remains intact for earlier/later solo weeks.
		for _, deduction := range member.Edits.ExpenseDeductions {
			if deduction.Source != "escrow" {
				main.Edits.ExpenseDeductions = append(main.Edits.ExpenseDeductions, deduction)
			}
		}
		main.AutoCharges = append(main.AutoCharges, member.AutoCharges...)
		main.Issues = append(main.Issues, member.Issues...)
	}
	kept := []DriverPayDriver{}
	for _, d := range report.Drivers {
		p := teamPeriodFor(teams.Periods, d.ID, report.WeekStart)
		if p == nil || p.TeamDriverID != d.ID {
			kept = append(kept, d)
		}
	}
	report.Drivers = kept
	return nil
}

func teamPayMembers(ctx context.Context, q chargeQuery, driver, week string) ([]string, error) {
	rows, err := q.Query(ctx, `SELECT id::text FROM drivers WHERE payroll_driver_at(id,$2::date)=$1::uuid ORDER BY id`, driver, week)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil, chargeInvalid("Edit the main driver's statement for this week")
	}
	return ids, rows.Err()
}

func saveTeamExpenses(ctx context.Context, tx pgx.Tx, driver, week, actor string, input []ExpenseDeduction) ([]ExpenseDeduction, error) {
	members, err := teamPayMembers(ctx, tx, driver, week)
	if err != nil {
		return nil, err
	}
	if len(members) == 1 {
		return saveExpenseDeductions(ctx, tx, driver, week, actor, input)
	}
	current, err := expenseDeductionsBulk(ctx, tx, members, week)
	if err != nil {
		return nil, err
	}
	owners := map[string]string{}
	for id, rows := range current {
		for _, row := range rows {
			owners[row.ExpenseID] = id
		}
	}
	grouped := map[string][]ExpenseDeduction{}
	for _, row := range input {
		owner := owners[row.ExpenseID]
		if row.Source == "escrow" && owner != driver && row.Apply {
			return nil, ErrDriverPayConflict
		}
		if row.Apply && owner == "" {
			return nil, ErrDriverPayConflict
		}
		if owner != "" {
			grouped[owner] = append(grouped[owner], row)
		}
	}
	out := []ExpenseDeduction{}
	for _, id := range members {
		rows, e := saveExpenseDeductions(ctx, tx, id, week, actor, grouped[id])
		if e != nil {
			return nil, e
		}
		out = append(out, rows...)
	}
	return out, nil
}

func saveTeamCharges(ctx context.Context, tx pgx.Tx, driver, week, actor string, input []ChargeOccurrence) ([]ChargeOccurrence, error) {
	members, err := teamPayMembers(ctx, tx, driver, week)
	if err != nil {
		return nil, err
	}
	if len(members) == 1 {
		return saveGeneratedCharges(ctx, tx, driver, week, actor, input)
	}
	data, _, err := chargeData(ctx, tx, "")
	if err != nil {
		return nil, err
	}
	allowed := map[string]bool{}
	for _, id := range members {
		allowed[id] = true
	}
	owners := map[string]string{}
	for _, s := range data.Schedules {
		if allowed[s.DriverID] {
			owners[s.ID] = s.DriverID
		}
	}
	groups := map[string][]ChargeOccurrence{}
	for _, row := range input {
		id := owners[row.ScheduleID]
		if id == "" {
			return nil, ErrChargeConflict
		}
		groups[id] = append(groups[id], row)
	}
	out := []ChargeOccurrence{}
	for _, id := range members {
		rows, e := saveGeneratedCharges(ctx, tx, id, week, actor, groups[id])
		if e != nil {
			return nil, e
		}
		out = append(out, rows...)
	}
	return out, nil
}

// Load-eligible personal charges use the main driver's work in team weeks.
func addTeamChargeLoads(ctx context.Context, q chargeQuery, loads map[string]map[string]bool) error {
	rows, err := q.Query(ctx, `SELECT DISTINCT t.team_driver_id::text,date_trunc('week',e.service_date)::date::text
 FROM driver_teams t JOIN `+grossBoardEntriesSQL+` e ON e.driver_id=t.main_driver_id
 WHERE e.service_date>=t.start_week AND (t.end_week IS NULL OR e.service_date<t.end_week)
 AND NOT e.deleted AND e.day_status='' AND btrim(e.load_number)<>''`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, w string
		if err = rows.Scan(&id, &w); err != nil {
			return err
		}
		if loads[id] == nil {
			loads[id] = map[string]bool{}
		}
		loads[id][w] = true
	}
	return rows.Err()
}
