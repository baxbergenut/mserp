package repository

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type PayDayStatus struct {
	Date   string `json:"date"`
	Status string `json:"status"`
	// Custom planning is display-only and must never acquire fixed-status semantics.
	Text string `json:"text,omitempty"`
	Kind string `json:"kind,omitempty"`
}

// Require an explicit Home record on every day of the selected week, with no
// conflicting statuses or loads. Missing days never imply time at home.
func payHomeWeek(week string, d DriverPayDriver) bool {
	if len(d.Loads) > 0 || d.Summary != nil && d.Summary.Loads > 0 {
		return false
	}
	start, err := time.Parse(time.DateOnly, week)
	if err != nil {
		return false
	}
	days := map[string]bool{}
	for _, s := range d.DayStatuses {
		if s.Date < week || s.Date >= start.AddDate(0, 0, 7).Format(time.DateOnly) {
			continue
		}
		if s.Status != "HOME" {
			return false
		}
		days[s.Date] = true
	}
	return len(days) == 7
}

// Decorate only the selected no-load page rows, retaining compact summaries.
// The existing day-status projection supplies dated truck/team routing and
// preserves frozen statement labels. No financial values are changed.
func decoratePayHomeWeeks(ctx context.Context, tx pgx.Tx, kind string, report *DriverPayWeek) error {
	metadata := DriverPayWeek{WeekStart: report.WeekStart}
	for _, d := range report.Drivers {
		if d.Summary != nil && d.Settlement != nil && d.Settlement.Finalized {
			continue
		}
		if len(d.Loads) == 0 && (d.Summary == nil || d.Summary.Loads == 0) {
			metadata.Drivers = append(metadata.Drivers, d)
		}
	}
	if err := decoratePayDayStatuses(ctx, tx, kind, &metadata); err != nil {
		return err
	}
	home := map[string]bool{}
	for _, d := range metadata.Drivers {
		home[d.ID] = payHomeWeek(report.WeekStart, d)
	}
	for i := range report.Drivers {
		if report.Drivers[i].Summary != nil && report.Drivers[i].Settlement != nil && report.Drivers[i].Settlement.Finalized {
			continue
		}
		report.Drivers[i].HomeWeek = home[report.Drivers[i].ID]
	}
	return nil
}

// Day labels are display metadata, never loads or payable amounts. Resolve the
// truck at the recorded day, rather than using today's operator or assignment.
func decoratePayDayStatuses(ctx context.Context, tx pgx.Tx, kind string, report *DriverPayWeek) error {
	defer func() {
		for i := range report.Drivers {
			for j := range report.Drivers[i].DayStatuses {
				report.Drivers[i].DayStatuses[j].Status = LegacyGrossBoardDayStatus(report.Drivers[i].DayStatuses[j].Status)
			}
		}
	}()
	ids := []string{}
	for _, d := range report.Drivers {
		if d.Settlement == nil || !d.Settlement.Finalized {
			ids = append(ids, d.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	byID, err := readPreparedPayDayStatuses(ctx, tx, kind, report.WeekStart, ids)
	if err != nil {
		return err
	}
	for i := range report.Drivers {
		d := &report.Drivers[i]
		if d.Settlement != nil && d.Settlement.Finalized {
			continue
		}
		d.DayStatuses = append([]PayDayStatus{}, byID[d.ID]...)
	}
	return decoratePayPlanningStatuses(ctx, tx, kind, report)
}

func readPreparedPayDayStatuses(ctx context.Context, tx pgx.Tx, kind, week string, ids []string) (map[string][]PayDayStatus, error) {
	facts, _ := ctx.Value(payrollRoutingFactsKey{}).(*payrollRoutingFactsState)
	if facts == nil || !payrollNormalizedInputs(ctx) || (kind != "driver" && kind != "investor") {
		return readPayDayStatuses(ctx, tx, kind, week, ids)
	}
	// This source-only query deliberately reads every destination. Its empty map
	// is complete, and callers attach only their own unfrozen rows below.
	factCtx := context.WithValue(ctx, payrollSelectionKey{}, (*payrollSelection)(nil))
	return payrollRoutingFact(factCtx, tx, kind+"_days", week, func() (map[string][]PayDayStatus, error) {
		return readPayDayStatuses(ctx, tx, kind, week, nil)
	})
}

func readPayDayStatuses(ctx context.Context, tx pgx.Tx, kind, week string, ids []string) (map[string][]PayDayStatus, error) {
	destination := `SELECT driver_id::text id,service_date,day_status FROM routed`
	if kind == "investor" {
		destination = `SELECT assignment.truck_id id,e.service_date,e.day_status FROM routed e
 JOIN LATERAL (
 SELECT min(a.truck_id::text) truck_id FROM truck_driver_assignments a
 WHERE a.driver_id=e.driver_id
 AND a.assigned_at < ((e.service_date+1)::timestamp AT TIME ZONE 'America/New_York')
 AND (a.unassigned_at IS NULL OR a.unassigned_at >= ((e.service_date+1)::timestamp AT TIME ZONE 'America/New_York'))
 HAVING count(DISTINCT a.truck_id)=1
 ) assignment ON true`
	}
	rows, err := tx.Query(ctx, `WITH statuses AS (
 SELECT driver_id,service_date,day_status FROM `+grossBoardEntriesSQL+` e
 WHERE NOT deleted AND day_status<>'' AND service_date >= $1::date AND service_date < $1::date+7
 UNION ALL SELECT driver_id,service_date,'LEFT' FROM (`+terminationDaysSQL+`) termination_days
 ), routed AS (
 SELECT coalesce(team.main_driver_id,e.driver_id) driver_id,e.service_date,e.day_status
 FROM statuses e LEFT JOIN driver_teams team ON team.team_driver_id=e.driver_id
 AND team.start_week <= $1::date AND (team.end_week IS NULL OR $1::date < team.end_week)
 ), destinations AS (`+destination+`)
 SELECT DISTINCT id,service_date::text,day_status FROM destinations WHERE id IS NOT NULL AND ($2::text[] IS NULL OR id=ANY($2::text[])) ORDER BY 1,2,3`, week, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byID := map[string][]PayDayStatus{}
	for rows.Next() {
		var id string
		var status PayDayStatus
		if err = rows.Scan(&id, &status.Date, &status.Status); err != nil {
			return nil, err
		}
		byID[id] = append(byID[id], status)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return byID, nil
}

// Financial cache identities deliberately exclude planning. Refresh this small
// display projection in the caller's snapshot after every cache hit or fill.
func (r *DriverPayRepository) cachedPayrollWeek(ctx context.Context, tx pgx.Tx, cacheKind string, week time.Time, read func() (DriverPayWeek, error)) (DriverPayWeek, error) {
	report, err := r.cachedWeek(ctx, tx, cacheKind, week, read)
	if err != nil {
		return report, err
	}
	kind, _, _ := strings.Cut(strings.TrimPrefix(strings.TrimPrefix(cacheKind, "detail:"), "summary:"), ":")
	if err = decoratePayPlanningStatuses(ctx, tx, kind, &report); err != nil {
		return report, err
	}
	if !strings.HasPrefix(report.Revision, "inputs:") {
		report.Revision = payrollRevision(report)
	}
	return report, nil
}

// Overlay live custom text only onto open statements. Native day facts,
// financial caches and prepared statement revisions remain independent of this
// mutable display metadata. In particular, nil legacy frozen metadata is not an
// invitation to reconstruct history from today's Gross Board. Compact rows also
// carry custom labels, letting clients refresh expanded details without changing
// their financial revision or including ordinary load detail in summary reads.
func decoratePayPlanningStatuses(ctx context.Context, tx pgx.Tx, kind string, report *DriverPayWeek) error {
	ids := []string{}
	for _, d := range report.Drivers {
		if d.Settlement == nil || !d.Settlement.Finalized {
			ids = append(ids, d.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	destination := `SELECT driver_id::text id,service_date,kind,text FROM routed`
	if kind == "investor" {
		destination = `SELECT assignment.truck_id id,e.service_date,e.kind,e.text FROM routed e
 JOIN LATERAL (
 SELECT min(a.truck_id::text) truck_id FROM truck_driver_assignments a
 WHERE a.driver_id=e.driver_id
 AND a.assigned_at < ((e.service_date+1)::timestamp AT TIME ZONE 'America/New_York')
 AND (a.unassigned_at IS NULL OR a.unassigned_at >= ((e.service_date+1)::timestamp AT TIME ZONE 'America/New_York'))
 HAVING count(DISTINCT a.truck_id)=1
 ) assignment ON true`
	}
	rows, err := tx.Query(ctx, `WITH routed AS (
 SELECT coalesce(team.main_driver_id,e.driver_id) driver_id,e.service_date,e.kind,e.text
 FROM gross_board_planning_entries e LEFT JOIN driver_teams team ON team.team_driver_id=e.driver_id
 AND team.start_week <= $1::date AND (team.end_week IS NULL OR $1::date < team.end_week)
 WHERE e.text<>'' AND e.service_date >= $1::date AND e.service_date < $1::date+7
 ), destinations AS (`+destination+`)
 SELECT DISTINCT id,service_date::text,kind,text FROM destinations WHERE id=ANY($2::text[]) ORDER BY 1,2,3,4`, report.WeekStart, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	byID := map[string][]PayDayStatus{}
	for rows.Next() {
		var id string
		status := PayDayStatus{Status: "CUSTOM"}
		if err = rows.Scan(&id, &status.Date, &status.Kind, &status.Text); err != nil {
			return err
		}
		byID[id] = append(byID[id], status)
	}
	if err = rows.Err(); err != nil {
		return err
	}
	for i := range report.Drivers {
		d := &report.Drivers[i]
		if d.Settlement != nil && d.Settlement.Finalized {
			continue
		}
		// Always remove earlier overlay rows, including when the new result is empty.
		// Allocate separately so a cached/calculated sibling cannot be mutated.
		var statuses []PayDayStatus
		if d.DayStatuses != nil {
			statuses = make([]PayDayStatus, 0, len(d.DayStatuses)+len(byID[d.ID]))
		}
		for _, status := range d.DayStatuses {
			if status.Status != "CUSTOM" {
				statuses = append(statuses, status)
			}
		}
		statuses = append(statuses, byID[d.ID]...)
		sort.SliceStable(statuses, func(i, j int) bool {
			a, b := statuses[i], statuses[j]
			if a.Date != b.Date {
				return a.Date < b.Date
			}
			if a.Status != b.Status {
				return a.Status < b.Status
			}
			if a.Kind != b.Kind {
				return a.Kind < b.Kind
			}
			return a.Text < b.Text
		})
		d.DayStatuses = statuses
	}
	return nil
}
