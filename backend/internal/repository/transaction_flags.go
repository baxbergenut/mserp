package repository

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const transactionLoadBufferDays = 1

// TransactionFlag is a review hint, not a finding of fraud. Flags are derived
// on every paginated read so new loads and corrections immediately reassess them.
type TransactionFlag struct {
	Status          string                   `json:"status"`
	Reason          string                   `json:"reason"`
	TruckUnit       string                   `json:"truckUnit"`
	TransactionDate string                   `json:"transactionDate"`
	BufferDays      int                      `json:"bufferDays"`
	PreviousLoad    *TransactionLoadEvidence `json:"previousLoad"`
	NextLoad        *TransactionLoadEvidence `json:"nextLoad"`
	RelatedLoad     *TransactionLoadEvidence `json:"relatedLoad"`
	LoadsSyncedAt   *time.Time               `json:"loadsSyncedAt"`
}

type TransactionLoadEvidence struct {
	ID                  int    `json:"id"`
	LoadID              string `json:"loadId"`
	TruckUnit           string `json:"truckUnit"`
	DriverName          string `json:"driverName"`
	PickupDate          string `json:"pickupDate"`
	DeliveryDate        string `json:"deliveryDate"`
	AppointmentFallback bool   `json:"appointmentFallback"`
}

type coverageTransaction struct {
	id, kind, driverID string
	units              []string
	date               time.Time
	dateValid          bool
	positiveCharge     bool
	production         bool
}

type coverageLoad struct {
	TransactionLoadEvidence
	driverID, teamName string
	start, end         time.Time
	uncertainStart     time.Time
	uncertainEnd       time.Time
	problem            bool
	excluded           bool
}

func coverageDate(value time.Time) time.Time {
	u := value.UTC()
	return time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, time.UTC)
}

func makeCoverageLoad(id int, number, unit, driverID, driverName, teamName, status string,
	pickup, delivery, pickupAppointment, deliveryAppointment *time.Time, now time.Time,
) coverageLoad {
	l := coverageLoad{TransactionLoadEvidence: TransactionLoadEvidence{
		ID: id, LoadID: number, TruckUnit: normalizeTruckUnit(unit), DriverName: formatPersonName(driverName),
	}, driverID: driverID, teamName: normalizeName(teamName)}
	if pickup == nil {
		pickup = pickupAppointment
		l.AppointmentFallback = true
	}
	if delivery == nil {
		delivery = deliveryAppointment
		l.AppointmentFallback = true
	}
	if pickup != nil {
		l.start = coverageDate(*pickup)
		l.PickupDate = l.start.Format(time.DateOnly)
	}
	if delivery != nil {
		l.end = coverageDate(*delivery)
		l.DeliveryDate = l.end.Format(time.DateOnly)
	}
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "canceled", "cancelled":
		l.excluded = true
	case "booked", "assigned", "dispatched", "in_transit", "delivered", "invoiced", "paid":
	default:
		l.problem = true
	}
	l.problem = l.problem || l.start.IsZero() || l.end.IsZero() || l.end.Before(l.start) ||
		l.end.After(l.start.AddDate(0, 0, 30)) || l.start.After(coverageDate(now).AddDate(0, 0, 30)) ||
		l.end.After(coverageDate(now).AddDate(0, 0, 30))
	for _, value := range []*time.Time{pickup, delivery, pickupAppointment, deliveryAppointment} {
		if value == nil {
			continue
		}
		d := coverageDate(*value)
		if l.uncertainStart.IsZero() || d.Before(l.uncertainStart) {
			l.uncertainStart = d
		}
		if l.uncertainEnd.IsZero() || d.After(l.uncertainEnd) {
			l.uncertainEnd = d
		}
	}
	// A missing endpoint is an open uncertainty interval, never a zero-day load.
	if pickup == nil {
		l.uncertainStart = time.Time{}
	}
	if delivery == nil {
		l.uncertainEnd = time.Time{}
	}
	return l
}

type coverageAssessment struct {
	matched, previous, next, uncertain *TransactionLoadEvidence
}

func assessLoadCoverage(date time.Time, loads []coverageLoad) coverageAssessment {
	result := coverageAssessment{}
	for i := range loads {
		l := &loads[i]
		if l.excluded {
			continue
		}
		if l.problem {
			if (l.uncertainStart.IsZero() || !date.Before(l.uncertainStart.AddDate(0, 0, -transactionLoadBufferDays))) &&
				(l.uncertainEnd.IsZero() || !date.After(l.uncertainEnd.AddDate(0, 0, transactionLoadBufferDays))) {
				result.uncertain = &l.TransactionLoadEvidence
			}
			continue
		}
		if !date.Before(l.start.AddDate(0, 0, -transactionLoadBufferDays)) && !date.After(l.end.AddDate(0, 0, transactionLoadBufferDays)) {
			result.matched = &l.TransactionLoadEvidence
		}
		if l.end.Before(date) && (result.previous == nil || l.DeliveryDate > result.previous.DeliveryDate) {
			result.previous = &l.TransactionLoadEvidence
		}
		if l.start.After(date) && (result.next == nil || l.PickupDate < result.next.PickupDate) {
			result.next = &l.TransactionLoadEvidence
		}
	}
	return result
}

func classifyTransaction(t coverageTransaction, byUnit, byDriver map[string][]coverageLoad, syncedAt *time.Time, now time.Time) *TransactionFlag {
	if !t.positiveCharge || !t.production {
		return nil // Refunds, zero charges, and test-environment data are not spending flags.
	}
	units := make(map[string]bool)
	for _, value := range t.units {
		if unit := normalizeTruckUnit(value); unit != "" {
			units[unit] = true
		}
	}
	f := &TransactionFlag{Status: "data_issue", BufferDays: transactionLoadBufferDays,
		TransactionDate: t.date.Format(time.DateOnly), LoadsSyncedAt: syncedAt}
	if len(units) != 1 {
		f.Reason = "The transaction has no single reported truck unit to match to loads."
		return f
	}
	for unit := range units {
		f.TruckUnit = unit
	}
	if !t.dateValid || t.date.After(coverageDate(now)) {
		f.Reason = "The transaction date or timezone needs verification."
		return f
	}
	a := assessLoadCoverage(t.date, byUnit[f.TruckUnit])
	f.PreviousLoad, f.NextLoad = a.previous, a.next
	if a.matched != nil {
		return nil
	}
	if syncedAt == nil || now.Sub(*syncedAt) > 48*time.Hour {
		f.Reason = "Load data is out of date. Sync loads before reviewing this transaction."
		return f
	}
	if a.uncertain != nil {
		f.Reason = "A nearby load has incomplete or inconsistent dates."
		f.RelatedLoad = a.uncertain
		return f
	}
	if t.kind == "fuel" {
		d := assessLoadCoverage(t.date, byDriver[t.driverID])
		if d.matched != nil {
			f.Reason = "The driver has a nearby load on a different or unspecified truck. Verify the reported unit."
			f.RelatedLoad = d.matched
			return f
		}
		if d.uncertain != nil {
			f.Reason = "A nearby driver load has incomplete or inconsistent dates."
			f.RelatedLoad = d.uncertain
			return f
		}
	}
	if a.previous == nil {
		f.Reason = "There is no earlier valid load history for this reported truck. Verify the unit and history."
		return f
	}
	f.Status = "review"
	f.Reason = "No recorded truck load covers this transaction, including one day before pickup and after delivery."
	return f
}

// Only the displayed IDs are assessed; existing table filters, pagination and
// financial totals remain server-side and unchanged. History is not date-clipped.
func transactionFlags(ctx context.Context, pool *pgxpool.Pool, kind string, ids []string, now time.Time) (map[string]*TransactionFlag, error) {
	flags := make(map[string]*TransactionFlag, len(ids))
	if len(ids) == 0 {
		return flags, nil
	}
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	var query string
	switch kind {
	case "fuel":
		query = `SELECT t.id::text, COALESCE((SELECT array_agg(p->>'value')
			FROM jsonb_array_elements(t.prompts) p WHERE lower(trim(p->>'label')) = 'truck #'), '{}'),
			t.driver_id::text, (t.purchased_at AT TIME ZONE ` + fuelTimezoneExpression("t.timezone") + `)::date,
			EXISTS(SELECT 1 FROM pg_timezone_names WHERE name = t.timezone),
			t.total_amount_paid > 0, t.relay_environment = 'production'
			FROM fuel_transactions t WHERE t.id = ANY($1::uuid[])`
	case "toll":
		query = `SELECT id::text, ARRAY[equipment_unit], '', exit_date,
			(entry_date IS NULL OR entry_date <= exit_date), amount > 0,
			(prepass_environment IS NULL OR prepass_environment = 'production')
			FROM tolls WHERE id = ANY($1::uuid[])`
	default:
		return nil, fmt.Errorf("unsupported transaction kind %q", kind)
	}
	rows, err := tx.Query(ctx, query, ids)
	if err != nil {
		return nil, err
	}
	transactions := make([]coverageTransaction, 0, len(ids))
	units, driverIDs := []string{}, []string{}
	for rows.Next() {
		t := coverageTransaction{kind: kind}
		if err := rows.Scan(&t.id, &t.units, &t.driverID, &t.date, &t.dateValid, &t.positiveCharge, &t.production); err != nil {
			rows.Close()
			return nil, err
		}
		transactions = append(transactions, t)
		for _, unit := range t.units {
			units = append(units, normalizeTruckUnit(unit))
		}
		if t.driverID != "" {
			driverIDs = append(driverIDs, t.driverID)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Team drivers use a name in DataTruck. Only unique exact canonical name
	// matches are allowed; today's truck assignments are never used here.
	nameIDs := map[string]string{}
	rows, err = tx.Query(ctx, `SELECT normalized_name, CASE WHEN count(*) = 1 THEN min(id::text) ELSE '' END FROM drivers GROUP BY normalized_name`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var name, id string
		if err := rows.Scan(&name, &id); err != nil {
			rows.Close()
			return nil, err
		}
		if id != "" {
			nameIDs[name] = id
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	driverNames := []string{}
	for name, id := range nameIDs {
		for _, driverID := range driverIDs {
			if id == driverID {
				driverNames = append(driverNames, name)
				break
			}
		}
	}
	rows, err = tx.Query(ctx, `SELECT id, load_id, COALESCE(truck_unit,''), COALESCE(driver_id::text,''),
		COALESCE(driver_name,''), COALESCE(team_driver_name,''), status,
		pickup_time, delivery_time, pickup_appointment_time, delivery_appointment_time
		FROM loads WHERE upper(regexp_replace(btrim(truck_unit),'[[:space:]]+',' ','g')) = ANY($1::text[])
		OR driver_id = ANY($2::uuid[])
		OR lower(regexp_replace(btrim(team_driver_name),'[[:space:]]+',' ','g')) = ANY($3::text[])
		ORDER BY id`, units, driverIDs, driverNames)
	if err != nil {
		return nil, err
	}
	byUnit, byDriver := map[string][]coverageLoad{}, map[string][]coverageLoad{}
	for rows.Next() {
		var id int
		var number, unit, driverID, driverName, teamName, status string
		var pickup, delivery, pickupAppointment, deliveryAppointment *time.Time
		if err := rows.Scan(&id, &number, &unit, &driverID, &driverName, &teamName, &status,
			&pickup, &delivery, &pickupAppointment, &deliveryAppointment); err != nil {
			rows.Close()
			return nil, err
		}
		l := makeCoverageLoad(id, number, unit, driverID, driverName, teamName, status,
			pickup, delivery, pickupAppointment, deliveryAppointment, now)
		if l.TruckUnit != "" {
			byUnit[l.TruckUnit] = append(byUnit[l.TruckUnit], l)
		}
		if driverID != "" {
			byDriver[driverID] = append(byDriver[driverID], l)
		}
		if teamID := nameIDs[l.teamName]; teamID != "" && teamID != driverID {
			byDriver[teamID] = append(byDriver[teamID], l)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var syncedAt *time.Time
	if err := tx.QueryRow(ctx, `SELECT max(synced_at) FROM loads`).Scan(&syncedAt); err != nil {
		return nil, err
	}
	for _, t := range transactions {
		flags[t.id] = classifyTransaction(t, byUnit, byDriver, syncedAt, now)
	}
	return flags, tx.Commit(ctx)
}
