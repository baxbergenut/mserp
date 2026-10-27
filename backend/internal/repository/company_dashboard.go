package repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

type DashboardPeriod struct {
	Preset      string `json:"preset"`
	DateFrom    string `json:"dateFrom"`
	DateTo      string `json:"dateTo"`
	CompareFrom string `json:"compareFrom"`
	CompareTo   string `json:"compareTo"`
	Bucket      string `json:"bucket"`
}

// Calendar arithmetic uses UTC date values after resolving the business date in New York.
func ResolveDashboardPeriod(preset, from, to string, now time.Time) (DashboardPeriod, error) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		return DashboardPeriod{}, err
	}
	today, _ := time.Parse(time.DateOnly, now.In(loc).Format(time.DateOnly))
	start, end := today, today
	if preset == "" {
		if from != "" || to != "" {
			preset = "custom"
		} else {
			preset = "thisWeek"
		}
	}
	month := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, time.UTC)
	week := today.AddDate(0, 0, -(int(today.Weekday())+6)%7)
	switch preset {
	case "week":
		start = week
		if from != "" {
			start, err = time.Parse(time.DateOnly, from)
			if err != nil {
				return DashboardPeriod{}, errors.New("provide a valid week date")
			}
			start = start.AddDate(0, 0, -(int(start.Weekday())+6)%7)
		}
		end = start.AddDate(0, 0, 6)
	case "thisWeek":
		start = week
	case "lastWeek":
		start, end = week.AddDate(0, 0, -7), week.AddDate(0, 0, -1)
	case "thisMonth":
		start = month
	case "lastMonth":
		start, end = month.AddDate(0, -1, 0), month.AddDate(0, 0, -1)
	case "custom":
		start, err = time.Parse(time.DateOnly, from)
		if err != nil {
			return DashboardPeriod{}, errors.New("provide a valid start date")
		}
		end, err = time.Parse(time.DateOnly, to)
		if err != nil {
			return DashboardPeriod{}, errors.New("provide a valid end date")
		}
	default:
		return DashboardPeriod{}, errors.New("choose a valid reporting period")
	}
	days := int(end.Sub(start).Hours()/24) + 1
	if days < 1 || days > 366 || start.Year() < 2000 || end.Year() > 2100 {
		return DashboardPeriod{}, errors.New("choose an ordered date range of at most 366 days between 2000 and 2100")
	}
	previousStart, previousEnd := start.AddDate(0, 0, -days), start.AddDate(0, 0, -1)
	switch preset {
	case "thisWeek", "lastWeek":
		previousStart, previousEnd = start.AddDate(0, 0, -7), end.AddDate(0, 0, -7)
	case "thisMonth", "lastMonth":
		previousStart = start.AddDate(0, -1, 0)
		previousEnd = previousStart.AddDate(0, 0, days-1)
		if previousEnd.After(start.AddDate(0, 0, -1)) {
			previousEnd = start.AddDate(0, 0, -1)
		}
		if preset == "lastMonth" {
			previousEnd = start.AddDate(0, 0, -1)
		}
	}
	bucket := "day"
	if days > 90 {
		bucket = "week"
	}
	return DashboardPeriod{preset, start.Format(time.DateOnly), end.Format(time.DateOnly), previousStart.Format(time.DateOnly), previousEnd.Format(time.DateOnly), bucket}, nil
}

type DashboardMetric struct {
	Gross              string  `json:"gross"`
	DriverGross        string  `json:"driverGross"`
	Miles              string  `json:"miles"`
	RPM                *string `json:"rpm"`
	Loads              int     `json:"loads"`
	MissingGross       int     `json:"missingGross"`
	MissingMiles       int     `json:"missingMiles"`
	MissingDriverGross int     `json:"missingDriverGross"`
	PairedLoads        int     `json:"pairedLoads"`
	Provisional        int     `json:"provisional"`
	ProvisionalGross   string  `json:"provisionalGross"`
	Conflicts          int     `json:"conflicts"`
	ConflictGross      string  `json:"conflictGross"`
	Repeats            int     `json:"repeats"`
	RepeatedGross      string  `json:"repeatedGross"`
	RawGross           string  `json:"rawGross"`
}
type DashboardBreakdown struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	DashboardMetric
}
type DashboardPerformance struct {
	Current     DashboardMetric      `json:"current"`
	Previous    DashboardMetric      `json:"previous"`
	Trend       []DashboardBreakdown `json:"trend"`
	Dispatchers []DashboardBreakdown `json:"dispatchers"`
}
type DashboardItem struct {
	ID             string  `json:"id"`
	Label          string  `json:"label"`
	Count          int     `json:"count"`
	Amount         *string `json:"amount,omitempty"`
	PreviousAmount *string `json:"previousAmount,omitempty"`
	Incomplete     int     `json:"incomplete,omitempty"`
	Href           string  `json:"href"`
	Note           string  `json:"note,omitempty"`
}
type DashboardSection struct {
	Kind        string                `json:"kind"`
	Title       string                `json:"title"`
	UpdatedAt   string                `json:"updatedAt"`
	Error       string                `json:"error,omitempty"`
	Items       []DashboardItem       `json:"items"`
	Performance *DashboardPerformance `json:"performance,omitempty"`
	Waterfall   *DashboardWaterfall   `json:"waterfall,omitempty"`
}
type CompanyDashboard struct {
	Period   DashboardPeriod             `json:"period"`
	Sections map[string]DashboardSection `json:"sections"`
}

// Resolve exact identity and effective values exactly as Gross Board does. Rank
// before period filtering so a repeat in another week never creates new revenue.
// Conflicts are evaluated through the requested end date (not future plans).
const companyDashboardCTE = `WITH resolved AS MATERIALIZED (
 SELECT e.driver_id,e.service_date,e.slot,e.load_number,l.id AS matched_id,
 coalesce(e.entered_original_rate,CASE WHEN l.id IS NOT NULL THEN l.total_pay ELSE e.original_rate END) AS gross,
 e.driver_rate,coalesce(e.entered_miles,CASE WHEN l.id IS NOT NULL THEN l.total_miles ELSE e.miles END) AS miles
 FROM ` + grossBoardEntriesSQL + ` e ` + grossBoardResolvedLoad + `
 WHERE NOT e.deleted AND e.day_status='' AND btrim(e.load_number)<>'' AND e.service_date<=$2::date
), identities AS (
 SELECT matched_id,count(DISTINCT driver_id)>1 OR count(DISTINCT gross)>1 OR
 count(DISTINCT driver_rate)>1 OR count(DISTINCT miles)>1 OR
 (count(gross)>0 AND count(gross)<count(*)) OR (count(miles)>0 AND count(miles)<count(*)) OR
 (count(driver_rate)>0 AND count(driver_rate)<count(*)) AS conflict
 FROM resolved WHERE matched_id IS NOT NULL GROUP BY matched_id
), ranked AS (
 SELECT r.*,coalesce(i.conflict,false) AS conflict,
 row_number() OVER (PARTITION BY r.matched_id ORDER BY service_date,slot,driver_id) AS occurrence
 FROM resolved r LEFT JOIN identities i USING(matched_id)
), classified AS (
 SELECT r.*,CASE WHEN matched_id IS NULL THEN 'provisional' WHEN conflict THEN 'conflict'
 WHEN occurrence>1 THEN 'repeat' ELSE 'included' END AS inclusion
 FROM ranked r
), dated AS MATERIALIZED (
 SELECT c.*,coalesce(h.dispatcher_id::text,'') AS dispatcher_id,
 coalesce(dp.full_name,h.dispatcher_name,'Unassigned') AS dispatcher_name,d.full_name AS driver_name
 FROM classified c JOIN drivers d ON d.id=c.driver_id
 LEFT JOIN LATERAL (
 SELECT dispatcher_id,dispatcher_name FROM driver_dispatcher_assignments h WHERE h.driver_id=c.driver_id
 AND (NOT h.start_known OR h.assigned_at<((c.service_date+1)::timestamp AT TIME ZONE 'America/New_York'))
 AND (h.unassigned_at IS NULL OR h.unassigned_at>=((c.service_date+1)::timestamp AT TIME ZONE 'America/New_York'))
 AND (NOT h.start_known OR h.unassigned_at IS NULL OR h.assigned_at<h.unassigned_at)
 ORDER BY h.assigned_at DESC LIMIT 1
 ) h ON true LEFT JOIN dispatchers dp ON dp.id=h.dispatcher_id
 WHERE c.service_date BETWEEN $1::date AND $2::date
) `

const companyDashboardMetricsSQL = `jsonb_build_object(
 'gross',coalesce(sum(gross) FILTER(WHERE inclusion='included'),0)::text,
 'driverGross',coalesce(sum(driver_rate) FILTER(WHERE inclusion='included'),0)::text,
 'miles',coalesce(sum(miles) FILTER(WHERE inclusion='included'),0)::text,
 'rpm',round(sum(gross) FILTER(WHERE inclusion='included' AND miles>0 AND gross IS NOT NULL)/
 nullif(sum(miles) FILTER(WHERE inclusion='included' AND miles>0 AND gross IS NOT NULL),0),2)::text,
 'loads',count(*) FILTER(WHERE inclusion='included'),
 'missingGross',count(*) FILTER(WHERE inclusion='included' AND gross IS NULL),
 'missingDriverGross',count(*) FILTER(WHERE inclusion='included' AND driver_rate IS NULL),
 'missingMiles',count(*) FILTER(WHERE inclusion='included' AND (miles IS NULL OR miles<=0)),
 'pairedLoads',count(*) FILTER(WHERE inclusion='included' AND miles>0 AND gross IS NOT NULL),
 'provisional',count(*) FILTER(WHERE inclusion='provisional'),
 'provisionalGross',coalesce(sum(gross) FILTER(WHERE inclusion='provisional'),0)::text,
 'conflicts',count(*) FILTER(WHERE inclusion='conflict'),
 'conflictGross',coalesce(sum(gross) FILTER(WHERE inclusion='conflict'),0)::text,
 'repeats',count(*) FILTER(WHERE inclusion='repeat'),
 'repeatedGross',coalesce(sum(gross) FILTER(WHERE inclusion='repeat'),0)::text,
 'rawGross',coalesce(sum(gross),0)::text)`

func (r *DashboardRepository) Performance(ctx context.Context, p DashboardPeriod) (DashboardPerformance, error) {
	value := DashboardPerformance{Trend: []DashboardBreakdown{}, Dispatchers: []DashboardBreakdown{}}
	// One statement keeps all visible reporting aggregates on the same snapshot.
	query := companyDashboardCTE + `, summary AS (
 SELECT 'current' AS kind,'' AS id,'' AS name,` + companyDashboardMetricsSQL + ` AS metrics FROM dated WHERE service_date BETWEEN $3::date AND $2::date
 UNION ALL SELECT 'previous','','',` + companyDashboardMetricsSQL + ` FROM dated WHERE service_date BETWEEN $1::date AND $4::date
 UNION ALL SELECT 'dispatcher',dispatcher_id,dispatcher_name,` + companyDashboardMetricsSQL + ` FROM dated WHERE service_date BETWEEN $3::date AND $2::date GROUP BY dispatcher_id,dispatcher_name
 UNION ALL SELECT 'trend',date_trunc($5,service_date)::date::text,'',` + companyDashboardMetricsSQL + ` FROM dated WHERE service_date BETWEEN $3::date AND $2::date GROUP BY date_trunc($5,service_date)
 ) SELECT kind,id,name,metrics FROM summary ORDER BY kind,id`
	rows, err := r.pool.Query(ctx, query, p.CompareFrom, p.DateTo, p.DateFrom, p.CompareTo, p.Bucket)
	if err != nil {
		return value, err
	}
	defer rows.Close()
	for rows.Next() {
		var kind string
		var row DashboardBreakdown
		var raw []byte
		if err = rows.Scan(&kind, &row.ID, &row.Name, &raw); err != nil {
			return value, err
		}
		if err = json.Unmarshal(raw, &row.DashboardMetric); err != nil {
			return value, err
		}
		switch kind {
		case "current":
			value.Current = row.DashboardMetric
		case "previous":
			value.Previous = row.DashboardMetric
		case "dispatcher":
			value.Dispatchers = append(value.Dispatchers, row)
		case "trend":
			value.Trend = append(value.Trend, row)
		}
	}
	return value, rows.Err()
}

type DashboardLoad struct {
	DriverID       string  `json:"driverId"`
	DriverName     string  `json:"driverName"`
	Date           string  `json:"date"`
	Slot           int     `json:"slot"`
	LoadNumber     string  `json:"loadNumber"`
	LoadRecordID   *int    `json:"loadRecordId"`
	Gross          *string `json:"gross"`
	DriverGross    *string `json:"driverGross"`
	Miles          *string `json:"miles"`
	DispatcherID   string  `json:"dispatcherId"`
	DispatcherName string  `json:"dispatcherName"`
	Inclusion      string  `json:"inclusion"`
}

func (r *DashboardRepository) DashboardLoads(ctx context.Context, p DashboardPeriod, dispatcher, inclusion string, pagination Pagination) (Page[DashboardLoad], error) {
	items := []DashboardLoad{}
	var total int
	// Count and page in one snapshot; lateral pagination still returns the count for an empty page.
	q := companyDashboardCTE + `, filtered AS (SELECT * FROM dated WHERE ($3='' OR dispatcher_id=CASE WHEN $3='unassigned' THEN '' ELSE $3 END) AND ($4='' OR inclusion=$4)),
 count_rows AS (SELECT count(*)::int AS total FROM filtered)
 SELECT total,coalesce((SELECT jsonb_agg(row_to_json(v)) FROM (
 SELECT driver_id AS "driverId",driver_name AS "driverName",service_date::text AS date,slot,load_number AS "loadNumber",matched_id AS "loadRecordId",
 gross::text AS gross,driver_rate::text AS "driverGross",miles::text AS miles,dispatcher_id AS "dispatcherId",dispatcher_name AS "dispatcherName",inclusion
 FROM filtered ORDER BY service_date DESC,driver_id,slot LIMIT $5 OFFSET least($6,greatest(0,((total-1)/$5)*$5))
 ) v),'[]'::jsonb) FROM count_rows`
	var raw []byte
	err := r.pool.QueryRow(ctx, q, p.DateFrom, p.DateTo, dispatcher, inclusion, pagination.PageSize, pagination.Offset()).Scan(&total, &raw)
	if err == nil {
		err = json.Unmarshal(raw, &items)
	}
	return NewPage(items, total, pagination), err
}
