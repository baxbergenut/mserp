package repository

import (
	"context"
	"time"
)

// Both legacy completion tags identify the same PM service cycle.
type PreventativeTruck struct {
	RecordVersion     int        `json:"recordVersion"`
	BaselineVersion   int        `json:"baselineVersion"`
	CurrentReportedAt *time.Time `json:"currentReportedAt"`
	CurrentSource     string     `json:"currentSource"`
	NextMiles         *int       `json:"nextMiles"`
	RemainingMiles    *int       `json:"remainingMiles"`
	NextDate          *string    `json:"nextDate"`
	Status            string     `json:"status"`
	Reason            string     `json:"reason"`
	TruckID           string     `json:"truckId"`
	UnitNumber        string     `json:"unitNumber"`
	Active            bool       `json:"active"`
	RecordID          *string    `json:"recordId"`
	LastDate          *string    `json:"lastDate"`
	LastMiles         *int       `json:"lastMiles"`
	CurrentMiles      *int       `json:"currentMiles"`
	CurrentDate       *string    `json:"currentDate"`
	OilType           string     `json:"oilType"`
}
type PreventativePage struct {
	Items      []PreventativeTruck `json:"items"`
	Total      int                 `json:"total"`
	Page       int                 `json:"page"`
	PageSize   int                 `json:"pageSize"`
	TotalPages int                 `json:"totalPages"`
}

func (r *MaintenanceRepository) Preventative(ctx context.Context, search string, inactive bool, page, size int) (PreventativePage, error) {
	const filter = ` WHERE (t.active OR $1) AND ($2='' OR t.unit_number ILIKE '%'||$2||'%')`
	var count int
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM trucks t`+filter, inactive, search).Scan(&count); err != nil {
		return PreventativePage{}, err
	}
	p := Pagination{Page: page, PageSize: size}.Normalize(count)
	rows, err := r.pool.Query(ctx, `SELECT t.id,t.unit_number,t.active,m.id,m.service_date::text,m.odometer,r.mileage,r.reading_date::text,
 coalesce(m.oil_type,''),r.reported_at,coalesce(r.source,''),m.record_version,m.baseline_version
 FROM trucks t
 `+maintenancePMJoin+`
 `+maintenanceCurrentReadingJoin+`
 `+filter+` ORDER BY t.unit_number,t.id LIMIT $3 OFFSET $4`, inactive, search, p.PageSize, p.Offset())
	if err != nil {
		return PreventativePage{}, err
	}
	defer rows.Close()
	result := PreventativePage{Items: []PreventativeTruck{}, Total: count, Page: p.Page, PageSize: p.PageSize, TotalPages: max(1, (count+p.PageSize-1)/p.PageSize)}
	for rows.Next() {
		var v PreventativeTruck
		if err := rows.Scan(&v.TruckID, &v.UnitNumber, &v.Active, &v.RecordID, &v.LastDate, &v.LastMiles, &v.CurrentMiles, &v.CurrentDate, &v.OilType, &v.CurrentReportedAt, &v.CurrentSource, &v.RecordVersion, &v.BaselineVersion); err != nil {
			return result, err
		}
		result.Items = append(result.Items, v)
	}
	if err := rows.Err(); err != nil {
		return result, err
	}
	rows.Close()
	schedules, err := r.Schedules(ctx)
	if err != nil {
		return result, err
	}
	zone, _ := time.LoadLocation("America/New_York")
	today, _ := time.Parse(time.DateOnly, time.Now().In(zone).Format(time.DateOnly))
	for i := range result.Items {
		v := &result.Items[i]
		d := MaintenanceDue{TruckID: v.TruckID, ServiceCode: "pm", LastDate: v.LastDate, LastMiles: v.LastMiles, CurrentDate: v.CurrentDate, CurrentMiles: v.CurrentMiles, CurrentReportedAt: v.CurrentReportedAt}
		for _, s := range schedules {
			if s.ServiceCode == "pm" && (s.TruckID == nil || *s.TruckID == v.TruckID) && (d.Schedule == nil || s.TruckID != nil) {
				copy := s
				d.Schedule = &copy
			}
		}
		calculateMaintenanceDue(&d, today)
		v.NextMiles, v.NextDate, v.Status, v.Reason = d.NextMiles, d.NextDate, d.Status, d.Reason
		if v.NextMiles != nil && v.CurrentMiles != nil {
			remaining := *v.NextMiles - *v.CurrentMiles
			v.RemainingMiles = &remaining
		}
	}
	return result, nil
}
