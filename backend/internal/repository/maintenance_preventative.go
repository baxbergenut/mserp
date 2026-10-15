package repository

import "context"

// Every detail comes from the same latest completed visit. Unknown historical
// prices stay unknown; a mixed invoice never becomes the price of just its PM.
type PreventativeTruck struct {
	TruckID      string  `json:"truckId"`
	UnitNumber   string  `json:"unitNumber"`
	Active       bool    `json:"active"`
	RecordID     *string `json:"recordId"`
	LastDate     *string `json:"lastDate"`
	VendorName   string  `json:"vendorName"`
	LastMiles    *int    `json:"lastMiles"`
	CurrentMiles *int    `json:"currentMiles"`
	CurrentDate  *string `json:"currentDate"`
	Cost         *string `json:"cost"`
	OilType      string  `json:"oilType"`
	Notes        string  `json:"notes"`
}
type PreventativePage struct {
	Items      []PreventativeTruck `json:"items"`
	Total      int                 `json:"total"`
	Page       int                 `json:"page"`
	PageSize   int                 `json:"pageSize"`
	TotalPages int                 `json:"totalPages"`
}

func (r *MaintenanceRepository) Preventative(ctx context.Context, service, search string, inactive bool, page, size int) (PreventativePage, error) {
	const filter = ` WHERE (t.active OR $1) AND ($2='' OR t.unit_number ILIKE '%'||$2||'%')`
	var count int
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM trucks t`+filter, inactive, search).Scan(&count); err != nil {
		return PreventativePage{}, err
	}
	p := Pagination{Page: page, PageSize: size}.Normalize(count)
	rows, err := r.pool.Query(ctx, `SELECT t.id,t.unit_number,t.active,m.id,m.service_date::text,coalesce(m.vendor_name,''),m.odometer,r.mileage,r.reading_date::text,
 CASE WHEN m.id IS NULL THEN NULL
 WHEN NOT EXISTS(SELECT 1 FROM jsonb_array_elements(m.services) s WHERE s->>'trackingCode'=$3 AND (s->>'amount') IS NULL)
 THEN (SELECT round(sum((s->>'amount')::numeric),2)::text FROM jsonb_array_elements(m.services) s WHERE s->>'trackingCode'=$3)
 WHEN jsonb_array_length(m.services)=1 THEN coalesce(e.base_amount,CASE WHEN lower(trim(coalesce(e.payment_type,'')))<>'efs' THEN e.amount END)::text
 ELSE NULL END,coalesce(m.oil_type,''),coalesce(m.notes,'')
 FROM trucks t
 LEFT JOIN LATERAL (SELECT * FROM maintenance_records v WHERE v.truck_id=t.id AND v.status='completed' AND EXISTS(SELECT 1 FROM jsonb_array_elements(v.services) s WHERE s->>'trackingCode'=$3) ORDER BY v.service_date DESC,v.created_at DESC,v.id DESC LIMIT 1) m ON true
 LEFT JOIN expenses e ON e.id=m.expense_id
 LEFT JOIN LATERAL (SELECT mileage,reading_date FROM maintenance_odometer_readings o WHERE o.truck_id=t.id AND o.active ORDER BY reading_date DESC,updated_at DESC,id DESC LIMIT 1) r ON true
 `+filter+` ORDER BY t.unit_number,t.id LIMIT $4 OFFSET $5`, inactive, search, service, p.PageSize, p.Offset())
	if err != nil {
		return PreventativePage{}, err
	}
	defer rows.Close()
	result := PreventativePage{Items: []PreventativeTruck{}, Total: count, Page: p.Page, PageSize: p.PageSize, TotalPages: max(1, (count+p.PageSize-1)/p.PageSize)}
	for rows.Next() {
		var v PreventativeTruck
		if err := rows.Scan(&v.TruckID, &v.UnitNumber, &v.Active, &v.RecordID, &v.LastDate, &v.VendorName, &v.LastMiles, &v.CurrentMiles, &v.CurrentDate, &v.Cost, &v.OilType, &v.Notes); err != nil {
			return result, err
		}
		result.Items = append(result.Items, v)
	}
	return result, rows.Err()
}
