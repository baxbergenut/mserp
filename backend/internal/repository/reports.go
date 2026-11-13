package repository

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	ReportCompany    = "company"
	ReportOperator   = "owner-operator"
	ReportDispatcher = "dispatcher"
	ReportDriver     = "driver"
)

type ReportsQuery struct {
	View, Truck, Driver, Dispatcher string
	Period                          DashboardPeriod
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
	ID      string             `json:"id"`
	Name    string             `json:"name"`
	Metrics map[string]float64 `json:"metrics"`
	Sources []ReportEvidence   `json:"sources"`
}

type ReportOptions struct {
	Trucks      []PayPerson `json:"trucks"`
	Drivers     []PayPerson `json:"drivers"`
	Dispatchers []PayPerson `json:"dispatchers"`
}

type ReportsResult struct {
	Period     DashboardPeriod `json:"period"`
	Current    []ReportRow     `json:"current"`
	Comparison []ReportRow     `json:"comparison"`
	Options    ReportOptions   `json:"options"`
}

type ReportsRepository struct{ pool *pgxpool.Pool }

func NewReportsRepository(pool *pgxpool.Pool) *ReportsRepository {
	return &ReportsRepository{pool: pool}
}

func (r *ReportsRepository) Read(ctx context.Context, q ReportsQuery) (ReportsResult, error) {
	result := ReportsResult{Period: q.Period}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	// The reporting queries join bounded source cohorts. Avoid paying PostgreSQL
	// JIT compilation costs for each interactive period and its comparison.
	if _, err = tx.Exec(ctx, "SET LOCAL jit = off"); err != nil {
		return result, err
	}
	state := reportRead{tx: tx, query: q, trucks: map[string]string{}, drivers: map[string]string{}, dispatchers: map[string]string{}, payroll: map[string][]DriverPayDriver{}}
	result.Current, err = state.period(ctx, q.Period.DateFrom, q.Period.DateTo)
	if err != nil {
		return result, fmt.Errorf("report period: %w", err)
	}
	result.Comparison, err = state.period(ctx, q.Period.CompareFrom, q.Period.CompareTo)
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
}

type reportGroups map[string]*reportGroup

func (groups reportGroups) add(id, name string, i reportIdentity, source ReportEvidence) *reportGroup {
	if id == "" {
		id, name = "unassigned", "Unassigned"
	}
	group := groups[id]
	if group == nil {
		group = &reportGroup{
			ReportRow: ReportRow{ID: id, Name: name, Metrics: map[string]float64{}},
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
	case ReportCompany, ReportOperator:
		if i.employment != "" && i.employment != "unassigned" &&
			!(s.query.View == ReportCompany && i.employment == "company") &&
			!(s.query.View == ReportOperator && i.employment == "owner_operator") {
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
	financial := s.query.View == ReportCompany || s.query.View == ReportOperator
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
		if evidence.State == "provisional" {
			g.Metrics["provisionalLoads"]++
			if l.gross != nil {
				g.Metrics["provisionalGross"] += *l.gross
			}
		}
		if l.state == "confirmed" || l.note == "Missing actual delivery time" {
			g.addTiming(l)
		}
		if l.state != "confirmed" {
			continue
		}
		g.loads[l.parent] = true
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
		for _, date := range []string{l.pickupDate, l.deliveryDate} {
			if date >= from && date <= to {
				g.days[date] = true
			}
		}
	}
	result := make([]ReportRow, 0, len(groups))
	for _, g := range groups {
		g.finish(s.query.View)
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
	if !g.missingMiles && len(g.loads) > 0 {
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
		if count > 0 {
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
	case ReportCompany, ReportOperator:
		g.finishFinancial(view)
	}
}
