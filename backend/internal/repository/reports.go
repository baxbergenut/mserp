package repository

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	ReportInvestor   = "investor"
	ReportCompany    = "company"
	ReportOperator   = "owner-operator"
	ReportDispatcher = "dispatcher"
	ReportDriver     = "driver"
	ReportsFirstWeek = "2026-10-19"
)

func ResolveReportsWeek(week string, now time.Time) (DashboardPeriod, error) {
	latest, err := ResolveDashboardPeriod("lastWeek", "", "", now)
	if err != nil {
		return latest, err
	}
	if week == "" {
		week = latest.DateFrom
	}
	day, err := time.Parse(time.DateOnly, week)
	if err != nil || day.Weekday() != time.Monday || week < ReportsFirstWeek || week > latest.DateFrom {
		return DashboardPeriod{}, fmt.Errorf("Choose a completed week from %s through %s", ReportsFirstWeek, latest.DateFrom)
	}
	return ResolveReportsPeriod(week, reportAddDays(week, 6), "", "", now)
}

type ReportsQuery struct {
	View, Truck, Driver, Dispatcher string
	Period                          DashboardPeriod
	CurrentOnly                     bool
}

func ResolveReportsPeriod(from, to, compareFrom, compareTo string, now time.Time) (DashboardPeriod, error) {
	preset := "lastWeek"
	if from != "" || to != "" {
		preset = "custom"
	}
	period, err := ResolveDashboardPeriod(preset, from, to, now)
	if err != nil {
		return period, err
	}
	if compareFrom != "" || compareTo != "" {
		comparison, err := ResolveDashboardPeriod("custom", compareFrom, compareTo, now)
		if err != nil {
			return period, fmt.Errorf("comparison: %w", err)
		}
		period.CompareFrom, period.CompareTo = comparison.DateFrom, comparison.DateTo
	}
	return period, nil
}

// Monetary amounts are signed integer cents. Missing metrics are omitted.
type ReportEvidence struct {
	Kind              string             `json:"kind"`
	ID                string             `json:"id"`
	Label             string             `json:"label"`
	Date              string             `json:"date"`
	Truck             string             `json:"truck,omitempty"`
	Driver            string             `json:"driver,omitempty"`
	Dispatcher        string             `json:"dispatcher,omitempty"`
	PickupActual      string             `json:"pickupActual,omitempty"`
	PickupScheduled   string             `json:"pickupScheduled,omitempty"`
	DeliveryActual    string             `json:"deliveryActual,omitempty"`
	DeliveryScheduled string             `json:"deliveryScheduled,omitempty"`
	Note              string             `json:"note,omitempty"`
	State             string             `json:"state,omitempty"`
	Values            map[string]float64 `json:"values,omitempty"`
}

type ReportRow struct {
	ID              string             `json:"id"`
	Name            string             `json:"name"`
	AssignedDrivers []PayPerson        `json:"assignedDrivers,omitempty"`
	Metrics         map[string]float64 `json:"metrics"`
	Estimated       map[string]bool    `json:"estimated,omitempty"`
	Sources         []ReportEvidence   `json:"sources,omitempty"`
	SourceCount     int                `json:"sourceCount"`
	ReviewCount     int                `json:"reviewCount"`
}

type ReportOptions struct {
	Trucks      []PayPerson `json:"trucks"`
	Drivers     []PayPerson `json:"drivers"`
	Dispatchers []PayPerson `json:"dispatchers"`
}

type ReportsResult struct {
	Revision   string          `json:"revision"`
	Period     DashboardPeriod `json:"period"`
	Current    []ReportRow     `json:"current"`
	Comparison []ReportRow     `json:"comparison"`
	Options    ReportOptions   `json:"options"`
}

type reportsCacheEntry struct {
	rows []byte
	used time.Time
}

type ReportsRepository struct {
	pool     *pgxpool.Pool
	mu       sync.Mutex
	cache    map[string]reportsCacheEntry
	inFlight map[string]chan struct{}
}

func NewReportsRepository(pool *pgxpool.Pool) *ReportsRepository {
	return &ReportsRepository{pool: pool, cache: map[string]reportsCacheEntry{}, inFlight: map[string]chan struct{}{}}
}

// Reports shares payroll's audited operational exclusions, except maintenance
// category configuration, which determines whether an expense is a P&L cost.
var reportsUnrelatedTables = func() []string {
	var result []string
	for _, table := range payrollUnrelatedTables {
		if table != "maintenance_configuration" {
			result = append(result, table)
		}
	}
	return result
}()

func (r *ReportsRepository) Read(ctx context.Context, q ReportsQuery) (ReportsResult, error) {
	result := ReportsResult{Period: q.Period}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	// The reporting queries join bounded source cohorts. Avoid paying PostgreSQL
	// JIT compilation costs for each interactive period and its comparison.
	if _, err = tx.Exec(ctx, "SET LOCAL jit = off; SET LOCAL join_collapse_limit = 1; SET LOCAL from_collapse_limit = 1"); err != nil {
		return result, err
	}
	state := reportRead{tx: tx, query: q, trucks: map[string]string{}, drivers: map[string]string{}, dispatchers: map[string]string{}, payroll: map[string][]DriverPayDriver{}}
	// Include source counters and maintenance classification, excluding audited
	// operational state such as presence and provider delivery bookkeeping. Resource
	// finalizations carry their committed financial epochs. Read in this snapshot.
	var revision string
	err = tx.QueryRow(ctx, `SELECT md5(payroll_financial_snapshot(coalesce(string_agg(table_name||':'||revision::text,',' ORDER BY table_name) FILTER (WHERE NOT table_name=ANY($1::text[])),''))
	 || (SELECT coalesce(first_week::text,'') FROM payroll_projection_control WHERE singleton)
     || (SELECT coalesce(string_agg(load_id::text,',' ORDER BY load_id),'') FROM load_source_availability WHERE NOT present)) FROM payroll_cache_revisions`, reportsUnrelatedTables).Scan(&revision)
	if err != nil {
		return result, err
	}
	result.Revision = revision
	readPeriod := func(from, to string) ([]ReportRow, error) {
		key := fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join([]string{revision, q.View, q.Truck, q.Driver, q.Dispatcher, from, to}, "/"))))
		var cached reportsCacheEntry
		var hit bool
		for {
			r.mu.Lock()
			cached, hit = r.cache[key]
			if hit && time.Since(cached.used) < 5*time.Minute {
				r.mu.Unlock()
				break
			}
			if flight := r.inFlight[key]; flight != nil {
				r.mu.Unlock()
				select {
				case <-flight:
					continue
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}
			r.inFlight[key] = make(chan struct{})
			r.mu.Unlock()
			defer func() { r.mu.Lock(); close(r.inFlight[key]); delete(r.inFlight, key); r.mu.Unlock() }()
			break
		}
		if hit && time.Since(cached.used) < 5*time.Minute {
			var saved struct {
				Rows    []ReportRow
				Options ReportOptions
			}
			if json.Unmarshal(cached.rows, &saved) == nil {
				for _, o := range saved.Options.Trucks {
					state.trucks[o.ID] = o.Name
				}
				for _, o := range saved.Options.Drivers {
					state.drivers[o.ID] = o.Name
				}
				for _, o := range saved.Options.Dispatchers {
					state.dispatchers[o.ID] = o.Name
				}
				return saved.Rows, nil
			}
		}
		rows, err := state.period(ctx, from, to)
		if err != nil {
			return nil, err
		}
		body, err := json.Marshal(struct {
			Rows    []ReportRow
			Options ReportOptions
		}{rows, ReportOptions{reportOptions(state.trucks), reportOptions(state.drivers), reportOptions(state.dispatchers)}})
		if err == nil && len(body) <= 4*1024*1024 {
			r.mu.Lock()
			bytes := len(body)
			for k, v := range r.cache {
				if time.Since(v.used) > 5*time.Minute {
					delete(r.cache, k)
				} else {
					bytes += len(v.rows)
				}
			}
			for len(r.cache) >= 32 || bytes > 16*1024*1024 {
				oldest := ""
				var stamp time.Time
				for k, v := range r.cache {
					if oldest == "" || v.used.Before(stamp) {
						oldest, stamp = k, v.used
					}
				}
				if oldest == "" {
					break
				}
				bytes -= len(r.cache[oldest].rows)
				delete(r.cache, oldest)
			}
			r.cache[key] = reportsCacheEntry{body, time.Now()}
			r.mu.Unlock()
		}
		return rows, nil
	}
	result.Current, err = readPeriod(q.Period.DateFrom, q.Period.DateTo)
	if err != nil {
		return result, fmt.Errorf("report period: %w", err)
	}
	result.Comparison = []ReportRow{}
	if !q.CurrentOnly {
		result.Comparison, err = readPeriod(q.Period.CompareFrom, q.Period.CompareTo)
	}
	if err != nil {
		return result, fmt.Errorf("report comparison: %w", err)
	}
	result.Options = ReportOptions{reportOptions(state.trucks), reportOptions(state.drivers), reportOptions(state.dispatchers)}
	return result, tx.Commit(ctx)
}

type reportRead struct {
	tx                           pgx.Tx
	query                        ReportsQuery
	trucks, drivers, dispatchers map[string]string
	payroll                      map[string][]DriverPayDriver
}

type reportIdentity struct {
	truck, unit, driver, name, dispatcher, dispatcherName, employment string
}

func (s *reportRead) includes(i reportIdentity) bool {
	for _, pair := range []struct {
		options  map[string]string
		id, name string
	}{{s.trucks, i.truck, i.unit}, {s.drivers, i.driver, i.name}, {s.dispatchers, i.dispatcher, i.dispatcherName}} {
		if pair.id != "" && pair.name != "" {
			pair.options[pair.id] = pair.name
		}
	}
	q := s.query
	return (q.Truck == "" || q.Truck == i.truck) &&
		(q.Driver == "" || q.Driver == i.driver) &&
		(q.Dispatcher == "" || q.Dispatcher == i.dispatcher)
}

func reportOptions(options map[string]string) []PayPerson {
	people := make([]PayPerson, 0, len(options))
	for id, name := range options {
		people = append(people, PayPerson{ID: id, Name: name})
	}
	sort.Slice(people, func(i, j int) bool {
		if people[i].Name == people[j].Name {
			return people[i].ID < people[j].ID
		}
		return strings.ToLower(people[i].Name) < strings.ToLower(people[j].Name)
	})
	return people
}

type reportGroup struct {
	ReportRow
	loads, days, truckWeeks, timingLoads                         map[string]bool
	pickupOnTime, deliveryOnTime, pickupCovered, deliveryCovered int
	loadedGross, loadedMiles, pairedLoaded, pairedEmpty          float64
	milesHundredths                                              int64
	missingGross, missingMiles, missingPay, missingCosts         bool
	missingActiveDays                                            bool
}

type reportGroups map[string]*reportGroup

func (groups reportGroups) add(id, name string, i reportIdentity, source ReportEvidence) *reportGroup {
	if id == "" {
		id, name = "unassigned", "Unassigned"
	}
	group := groups[id]
	if group == nil {
		group = &reportGroup{
			ReportRow: ReportRow{ID: id, Name: name, Metrics: map[string]float64{}, Estimated: map[string]bool{}},
			loads:     map[string]bool{}, days: map[string]bool{}, truckWeeks: map[string]bool{}, timingLoads: map[string]bool{},
		}
		groups[id] = group
	}
	source.Truck, source.Driver, source.Dispatcher = i.unit, i.name, i.dispatcherName
	group.Sources = append(group.Sources, source)
	return group
}

func reportMonday(date string) string {
	t, _ := time.Parse(time.DateOnly, date)
	return t.AddDate(0, 0, -(int(t.Weekday())+6)%7).Format(time.DateOnly)
}

func (s *reportRead) groupIdentity(i reportIdentity) (string, string, bool) {
	switch s.query.View {
	case ReportCompany, ReportOperator, ReportInvestor:
		if s.query.View == ReportInvestor && i.employment != "investor_driver" {
			return "", "", false
		}
		if i.employment != "" && i.employment != "unassigned" &&
			!(s.query.View == ReportCompany && i.employment == "company") &&
			!(s.query.View == ReportOperator && i.employment == "owner_operator") &&
			!(s.query.View == ReportInvestor && i.employment == "investor_driver") {
			return "", "", false
		}
		if i.employment == "unassigned" {
			return "", "", true
		}
		return i.truck, i.unit, true
	case ReportDispatcher:
		return i.dispatcher, i.dispatcherName, true
	case ReportDriver:
		return i.driver, i.name, true
	}
	return "", "", false
}

func (s *reportRead) period(ctx context.Context, from, to string) ([]ReportRow, error) {
	groups := reportGroups{}
	loads, err := readReportLoads(ctx, s.tx, s.query.View, from, to)
	if err != nil {
		return nil, err
	}
	financial := s.query.View == ReportCompany || s.query.View == ReportOperator || s.query.View == ReportInvestor
	if financial {
		if err = s.addFinancial(ctx, groups, loads, from, to); err != nil {
			return nil, err
		}
	}
	for _, l := range loads {
		id, name, include := s.groupIdentity(l.reportIdentity)
		if !include || !s.includes(l.reportIdentity) {
			continue
		}
		evidence := l.evidence()
		g := groups.add(id, name, l.reportIdentity, evidence)
		if evidence.State == "review" {
			g.missingGross, g.missingMiles, g.missingPay = true, true, true
		}
		if l.state == "confirmed" || l.note == "Missing actual delivery time" {
			g.addTiming(l)
		}
		if l.state != "confirmed" && l.state != "provisional" {
			continue
		}
		loadKey := l.parent
		if loadKey == "" {
			loadKey = evidence.ID
		}
		g.loads[loadKey] = true
		if l.gross != nil {
			g.Metrics["gross"] += *l.gross
		} else {
			g.missingGross = true
		}
		if l.miles != nil {
			g.milesHundredths += int64(math.Round(*l.miles * 100))
		} else {
			g.missingMiles = true
		}
		if l.loaded != nil && *l.loaded > 0 && l.gross != nil {
			g.loadedGross += *l.gross
			g.loadedMiles += *l.loaded
		}
		if l.loaded != nil && l.empty != nil {
			g.pairedLoaded += *l.loaded
			g.pairedEmpty += *l.empty
		}
		if l.truck != "" {
			g.truckWeeks[l.truck+"/"+reportMonday(l.date)] = true
		}
		if l.pickupDate == "" || l.deliveryDate < l.pickupDate {
			g.missingActiveDays = true
		} else {
			first, last := l.pickupDate, l.deliveryDate
			if first < from {
				first = from
			}
			if last > to {
				last = to
			}
			for day := first; day <= last; day = reportAddDays(day, 1) {
				g.days[day] = true
			}
		}
	}
	if financial {
		if err := s.addWeeklyDrivers(ctx, groups, from, to); err != nil {
			return nil, err
		}
	}
	result := make([]ReportRow, 0, len(groups))
	for _, g := range groups {
		g.finish(s.query.View)
		g.SourceCount = len(g.Sources)
		for _, source := range g.Sources {
			if source.State == "review" || source.State == "provisional" {
				g.ReviewCount++
			}
		}
		sort.Slice(g.Sources, func(i, j int) bool {
			a, b := g.Sources[i], g.Sources[j]
			if a.Date != b.Date {
				return a.Date < b.Date
			}
			if a.Kind != b.Kind {
				return a.Kind < b.Kind
			}
			return a.ID < b.ID
		})
		result = append(result, g.ReportRow)
	}
	if err := s.reportFirstNames(ctx, result); err != nil {
		return nil, err
	}
	sort.Slice(result, func(i, j int) bool {
		a, b := result[i], result[j]
		if a.ID == "unassigned" || b.ID == "unassigned" {
			return a.ID != "unassigned"
		}
		if a.Name == b.Name {
			return a.ID < b.ID
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})
	return result, nil
}

// Label existing truck rows from native dated assignments, including team members
// and changes within the selected week. This never substitutes today's crew or
// changes source attribution. One bounded query serves the entire report.
func (s *reportRead) addWeeklyDrivers(ctx context.Context, groups reportGroups, from, to string) error {
	trucks := make([]string, 0, len(groups))
	for id := range groups {
		if id != "unassigned" {
			trucks = append(trucks, id)
		}
	}
	if len(trucks) == 0 {
		return nil
	}
	rows, err := s.tx.Query(ctx, `WITH days AS (
 SELECT $2::date+n AS service_day FROM generate_series(0,$3::date-$2::date) n
), assigned AS MATERIALIZED (
 SELECT DISTINCT a.truck_id,a.driver_id,d.service_day FROM days d JOIN truck_driver_assignments a
 ON a.truck_id=ANY($1::uuid[]) AND a.assigned_at<((d.service_day+1)::timestamp AT TIME ZONE 'America/New_York')
 AND (a.unassigned_at IS NULL OR a.unassigned_at>=((d.service_day+1)::timestamp AT TIME ZONE 'America/New_York'))
 AND (a.unassigned_at IS NULL OR a.assigned_at<a.unassigned_at)
), crew AS (
 SELECT truck_id,driver_id FROM assigned
 UNION SELECT a.truck_id,t.team_driver_id FROM assigned a JOIN driver_teams t
 ON t.main_driver_id=a.driver_id AND t.start_week<=a.service_day AND (t.end_week IS NULL OR a.service_day<t.end_week)
) SELECT c.truck_id::text,d.id::text,d.full_name FROM crew c JOIN drivers d ON d.id=c.driver_id
 ORDER BY lower(d.full_name),d.id`, trucks, from, to)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var truck string
		var driver PayPerson
		if err := rows.Scan(&truck, &driver.ID, &driver.Name); err != nil {
			return err
		}
		groups[truck].AssignedDrivers = append(groups[truck].AssignedDrivers, driver)
	}
	return rows.Err()
}

func (g *reportGroup) addTiming(l reportLoad) {
	if g.timingLoads[l.parent] {
		return
	}
	g.timingLoads[l.parent] = true
	if l.pickupOnTime != nil {
		g.pickupCovered++
		if *l.pickupOnTime {
			g.pickupOnTime++
		}
	}
	if l.deliveryOnTime != nil {
		g.deliveryCovered++
		if *l.deliveryOnTime {
			g.deliveryOnTime++
		}
	}
}

func (g *reportGroup) finish(view string) {
	m := g.Metrics
	if !g.missingMiles {
		m["miles"] = float64(g.milesHundredths) / 100
	}
	if g.missingGross {
		delete(m, "gross")
	}
	count := float64(len(g.loads))
	m["completedLoads"] = count
	switch view {
	case ReportDispatcher:
		if len(g.truckWeeks) > 0 {
			m["truckWeeks"] = float64(len(g.truckWeeks))
			if gross, ok := m["gross"]; ok {
				m["grossPerTruckWeek"] = gross / m["truckWeeks"]
			}
		}
		if g.loadedMiles > 0 {
			m["loadedRPM"] = g.loadedGross / g.loadedMiles
		}
		if paired := g.pairedLoaded + g.pairedEmpty; paired > 0 {
			m["deadheadPct"] = 100 * g.pairedEmpty / paired
		}
	case ReportDriver:
		if count > 0 && !g.missingActiveDays {
			m["activeDays"] = float64(len(g.days))
		}
		if timing := float64(len(g.timingLoads)); timing > 0 {
			m["pickupCoverage"] = 100 * float64(g.pickupCovered) / timing
			m["deliveryCoverage"] = 100 * float64(g.deliveryCovered) / timing
		}
		if g.pickupCovered > 0 {
			m["pickupOnTime"] = 100 * float64(g.pickupOnTime) / float64(g.pickupCovered)
		}
		if g.deliveryCovered > 0 {
			m["deliveryOnTime"] = 100 * float64(g.deliveryOnTime) / float64(g.deliveryCovered)
		}
	case ReportCompany, ReportOperator, ReportInvestor:
		g.finishFinancial(view)
	}
}
