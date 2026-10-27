package repository

import (
	"context"
	"fmt"
	"net/url"
)

func (r *DashboardRepository) DashboardCounts(ctx context.Context, kind string) ([]DashboardItem, error) {
	q, path, param := "", "", "status"
	switch kind {
	case "trucks":
		q = "SELECT fleet_truck_operating_status(id),count(*) FROM trucks WHERE status<>'terminated' GROUP BY 1"
		path = "/trucks"
	case "trailers":
		q = "SELECT data->>'status',count(*) FROM fleet_trailers WHERE data->>'status'<>'terminated' GROUP BY 1"
		path = "/fleet/trailers"
	case "drivers":
		q = "SELECT CASE WHEN availability_status='new_driver' THEN 'active' ELSE availability_status END,count(*) FROM drivers WHERE active AND availability_status IN ('active','new_driver','home','vacation') GROUP BY 1"
		path = "/drivers"
		param = "availability"
	case "gps":
		q = "SELECT gps_health.alert,count(*) FROM trucks t " + truckGPSJoinSQL + " WHERE gps_health.alert<>'' GROUP BY 1"
		path = "/trucks"
		param = "gpsAlert"
	default:
		return nil, fmt.Errorf("unknown dashboard count")
	}
	rows, err := r.pool.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	counts := map[string]int{}
	for rows.Next() {
		var key string
		var count int
		if err = rows.Scan(&key, &count); err != nil {
			return nil, err
		}
		counts[key] = count
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	keys := []string{"ready", "in_use", "shop", "out_of_service", "terminated"}
	if kind == "trucks" || kind == "trailers" {
		keys = []string{"ready", "in_use", "shop", "out_of_service"}
	}
	labels := map[string]string{"ready": "Ready", "in_use": "In use", "shop": "Shop", "out_of_service": "OOS", "terminated": "Terminated", "active": "Active", "home": "Home", "vacation": "Vacation", "rest": "Rest", "reset": "Reset", "family_issue": "Family issue", "health_issue": "Health issue", "new_driver": "New driver", "unassigned": "Without truck", "overdue": "GPS update overdue", "missing": "No GPS data"}
	if kind == "drivers" {
		keys = []string{"active", "home", "vacation"}
	}
	if kind == "gps" {
		keys = []string{"overdue", "missing"}
	}
	items := []DashboardItem{}
	for _, key := range keys {
		v := url.Values{"dashboard": {"1"}, param: {key}}
		if kind == "drivers" {
			v.Set("employment", "active")
			if key == "active" {
				v.Set(param, "active_or_new")
			}
		}
		items = append(items, DashboardItem{ID: key, Label: labels[key], Count: counts[key], Href: path + "?" + v.Encode()})
	}
	return items, nil
}

func (r *DashboardRepository) DashboardCost(ctx context.Context, kind string, p DashboardPeriod, categories []string) ([]DashboardItem, error) {
	base, date, amount, table, where, path, label := "", "", "", "", "true", "", ""
	args := []any{p.DateFrom, p.DateTo, p.CompareFrom, p.CompareTo}
	switch kind {
	case "diesel":
		base = fuelDashboardBaseSQL
		date = "purchased_on"
		amount = "spend"
		table = "fuel_purchase_totals"
		path = "/fuel"
		label = "Diesel"
	case "tolls":
		date = "posting_date"
		amount = "amount"
		table = "tolls"
		where = "(prepass_environment IS NULL OR prepass_environment='production')"
		path = "/tolls"
		label = "Tolls"
	case "scale":
		date = "transaction_date"
		amount = "amount"
		table = "weighmytruck_transactions"
		path = "/weighmytruck"
		label = "Scale tickets"
	case "expenses":
		date = "expense_date"
		amount = "amount"
		table = "expenses"
		where = "category_id=ANY($5::uuid[])"
		path = "/expenses"
		args = append(args, categories)
	default:
		return nil, fmt.Errorf("unknown dashboard cost")
	}
	id, name, group := "''", "''", ""
	if kind == "expenses" {
		id = "category_id::text"
		name = "coalesce((SELECT name FROM expense_settings s WHERE s.id=expenses.category_id),'Expenses')"
		group = " GROUP BY category_id"
	}
	query := base + " SELECT " + id + "," + name + ",coalesce(sum(" + amount + ") FILTER(WHERE " + date + " BETWEEN $1::date AND $2::date),0)::text,coalesce(sum(" + amount + ") FILTER(WHERE " + date + " BETWEEN $3::date AND $4::date),0)::text,count(*) FILTER(WHERE " + date + " BETWEEN $1::date AND $2::date),count(*) FILTER(WHERE " + date + " IS NULL OR (" + date + " BETWEEN $1::date AND $2::date AND " + amount + " IS NULL)) FROM " + table + " WHERE " + where + " AND (" + date + " BETWEEN $3::date AND $2::date OR " + date + " IS NULL)" + group
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []DashboardItem{}
	for rows.Next() {
		var item DashboardItem
		var current, previous string
		if err = rows.Scan(&item.ID, &item.Label, &current, &previous, &item.Count, &item.Incomplete); err != nil {
			return nil, err
		}
		if kind != "expenses" {
			item.ID = kind
			item.Label = label
		}
		item.Amount = &current
		item.PreviousAmount = &previous
		v := url.Values{"dashboard": {"1"}, "dateFrom": {p.DateFrom}, "dateTo": {p.DateTo}}
		if kind == "expenses" {
			v.Set("categoryId", item.ID)
		}
		if kind == "diesel" {
			v.Set("tab", "transactions")
			v.Set("category", "fuel")
		}
		if kind == "scale" {
			v.Set("tab", "transactions")
		}
		if kind == "tolls" {
			v.Set("tab", "transactions")
			v.Set("productionOnly", "true")
		}
		item.Href = path + "?" + v.Encode()
		item.Note = "Recorded transactions; not company payment responsibility."
		if kind == "diesel" {
			item.Note = "Diesel line items only. Linked transactions can also contain DEF or other purchases."
		}
		if item.Count == 0 {
			item.Note = "No recorded transactions in this period; source coverage is not confirmed."
		}
		if kind == "expenses" {
			item.Note = "Recorded expense dates; missing dates are excluded from period totals."
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *CustomTaskRepository) DashboardTasks(ctx context.Context, driverRead, truckRead bool) ([]DashboardItem, error) {
	admin, user := taskViewerArgs(ctx)
	rows, err := r.pool.Query(ctx, boardTaskCTE+` SELECT CASE WHEN in_process THEN 'in_process' ELSE 'open' END,count(*) FROM visible WHERE completed_at IS NULL GROUP BY 1`, admin, user, taskDirectoryKinds(driverRead, truckRead))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []DashboardItem{}
	for rows.Next() {
		var item DashboardItem
		if err = rows.Scan(&item.ID, &item.Count); err != nil {
			return nil, err
		}
		item.Label = "Open tasks"
		if item.ID == "in_process" {
			item.Label = "Tasks in process"
		}
		item.Href = "/tasks?dashboard=1&status=" + item.ID
		items = append(items, item)
	}
	return items, rows.Err()
}
