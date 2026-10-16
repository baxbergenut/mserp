package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrMaintenanceConflict = errors.New("maintenance changed; reload before saving")
var ErrMaintenanceInvalid = errors.New("invalid maintenance record")

type MaintenanceRepository struct{ pool *pgxpool.Pool }

func NewMaintenanceRepository(pool *pgxpool.Pool) *MaintenanceRepository {
	return &MaintenanceRepository{pool: pool}
}

type MaintenanceType struct {
	Code    string `json:"code"`
	Name    string `json:"name"`
	Active  bool   `json:"active"`
	Version int    `json:"version"`
}
type MaintenanceVendor struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Address string `json:"address"`
	Phone   string `json:"phone"`
	Contact string `json:"contact"`
	Active  bool   `json:"active"`
	Version int    `json:"version"`
}
type MaintenanceWork struct {
	TypeCode     string  `json:"typeCode"`
	Name         string  `json:"name"`
	Amount       *string `json:"amount"`
	TrackingCode string  `json:"trackingCode"`
}
type MaintenanceDocument struct {
	ID          string    `json:"id"`
	FileName    string    `json:"fileName"`
	ContentType string    `json:"contentType"`
	SizeBytes   int64     `json:"sizeBytes"`
	CreatedAt   time.Time `json:"createdAt"`
}
type MaintenanceRecord struct {
	OilType       string                `json:"oilType"`
	ID            string                `json:"id"`
	ExpenseID     *string               `json:"expenseId"`
	TruckID       *string               `json:"truckId"`
	UnitNumber    string                `json:"unitNumber"`
	DriverID      *string               `json:"driverId"`
	DriverName    string                `json:"driverName"`
	VendorID      *string               `json:"vendorId"`
	VendorName    string                `json:"vendorName"`
	VendorAddress string                `json:"vendorAddress"`
	VendorPhone   string                `json:"vendorPhone"`
	VendorContact string                `json:"vendorContact"`
	ServiceDate   *string               `json:"serviceDate"`
	Odometer      *int                  `json:"odometer"`
	Location      string                `json:"location"`
	Status        string                `json:"status"`
	WorkOrder     string                `json:"workOrder"`
	Notes         string                `json:"notes"`
	Services      []MaintenanceWork     `json:"services"`
	Version       int                   `json:"version"`
	CreatedByName string                `json:"createdByName"`
	UpdatedByName string                `json:"updatedByName"`
	CreatedAt     time.Time             `json:"createdAt"`
	UpdatedAt     time.Time             `json:"updatedAt"`
	VoidReason    string                `json:"voidReason"`
	Expense       *Expense              `json:"expense"`
	Documents     []MaintenanceDocument `json:"documents"`
}
type MaintenanceInput struct {
	OilType                                               string
	ActorID                                               string
	ID                                                    string
	Version                                               int
	TruckID                                               *string
	DriverID                                              *string
	SourceUnitNumber                                      string
	VendorID                                              *string
	VendorName, VendorAddress, VendorPhone, VendorContact string
	ServiceDate                                           *time.Time
	Odometer                                              *int
	Location, Status, WorkOrder, Notes, Actor             string
	Services                                              []MaintenanceWork
	Expense                                               *ExpenseInput
}
type MaintenanceQuery struct {
	TruckID, VendorID, TypeCode, Status, Search, From, To, ExpenseID string
	Page, PageSize                                                   int
}
type MaintenancePage struct {
	Items      []MaintenanceRecord `json:"items"`
	Total      int                 `json:"total"`
	Page       int                 `json:"page"`
	PageSize   int                 `json:"pageSize"`
	TotalPages int                 `json:"totalPages"`
}

func (r *MaintenanceRepository) CategoryID(ctx context.Context) (string, error) {
	var id string
	err := r.pool.QueryRow(ctx, "SELECT category_id FROM maintenance_configuration").Scan(&id)
	return id, err
}
func (r *MaintenanceRepository) Types(ctx context.Context) ([]MaintenanceType, error) {
	rows, err := r.pool.Query(ctx, "SELECT code,name,active,version FROM maintenance_types WHERE code<>'oil' ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []MaintenanceType{}
	for rows.Next() {
		var v MaintenanceType
		if err := rows.Scan(&v.Code, &v.Name, &v.Active, &v.Version); err != nil {
			return nil, err
		}
		items = append(items, v)
	}
	return items, rows.Err()
}
func (r *MaintenanceRepository) Vendors(ctx context.Context) ([]MaintenanceVendor, error) {
	rows, err := r.pool.Query(ctx, "SELECT id,name,address,phone,contact,active,version FROM maintenance_vendors WHERE superseded_by IS NULL ORDER BY lower(name)")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []MaintenanceVendor{}
	for rows.Next() {
		var v MaintenanceVendor
		if err := rows.Scan(&v.ID, &v.Name, &v.Address, &v.Phone, &v.Contact, &v.Active, &v.Version); err != nil {
			return nil, err
		}
		items = append(items, v)
	}
	return items, rows.Err()
}
func (r *MaintenanceRepository) SaveVendor(ctx context.Context, v MaintenanceVendor) (MaintenanceVendor, error) {
	var row pgx.Row
	if v.ID == "" {
		row = r.pool.QueryRow(ctx, `INSERT INTO maintenance_vendors(name,address,phone,contact,active) VALUES($1,$2,$3,$4,$5) RETURNING id,name,address,phone,contact,active,version`, v.Name, v.Address, v.Phone, v.Contact, v.Active)
	} else {
		row = r.pool.QueryRow(ctx, `UPDATE maintenance_vendors SET name=$2,address=$3,phone=$4,contact=$5,active=$6,version=version+1 WHERE id=$1 AND version=$7 AND superseded_by IS NULL RETURNING id,name,address,phone,contact,active,version`, v.ID, v.Name, v.Address, v.Phone, v.Contact, v.Active, v.Version)
	}
	var result MaintenanceVendor
	err := row.Scan(&result.ID, &result.Name, &result.Address, &result.Phone, &result.Contact, &result.Active, &result.Version)
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrMaintenanceConflict
	}
	return result, err
}
func (r *MaintenanceRepository) SaveType(ctx context.Context, v MaintenanceType) (MaintenanceType, error) {
	if v.Code == "oil" {
		return v, ErrMaintenanceInvalid
	}
	var row pgx.Row
	if v.Version == 0 {
		row = r.pool.QueryRow(ctx, `INSERT INTO maintenance_types(code,name,active) VALUES($1,$2,$3) RETURNING code,name,active,version`, v.Code, v.Name, v.Active)
	} else {
		row = r.pool.QueryRow(ctx, `UPDATE maintenance_types SET name=$2,active=$3,version=version+1 WHERE code=$1 AND version=$4 RETURNING code,name,active,version`, v.Code, v.Name, v.Active, v.Version)
	}
	var result MaintenanceType
	err := row.Scan(&result.Code, &result.Name, &result.Active, &result.Version)
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrMaintenanceConflict
	}
	return result, err
}

const maintenanceSelect = `SELECT m.id,m.expense_id,m.truck_id,coalesce(t.unit_number,e.unit_number,nullif(m.source_unit_number,''),''),m.vendor_id,m.vendor_name,m.vendor_address,m.vendor_phone,m.vendor_contact,m.service_date::text,m.odometer,m.location,m.status,m.work_order,m.notes,m.services,m.version,m.created_by_name,m.updated_by_name,m.created_at,m.updated_at,m.void_reason,m.driver_id,m.driver_name,m.oil_type FROM maintenance_records m LEFT JOIN trucks t ON t.id=m.truck_id LEFT JOIN expenses e ON e.id=m.expense_id`

func scanMaintenance(row rowScanner) (MaintenanceRecord, error) {
	var m MaintenanceRecord
	err := row.Scan(&m.ID, &m.ExpenseID, &m.TruckID, &m.UnitNumber, &m.VendorID, &m.VendorName, &m.VendorAddress, &m.VendorPhone, &m.VendorContact, &m.ServiceDate, &m.Odometer, &m.Location, &m.Status, &m.WorkOrder, &m.Notes, &m.Services, &m.Version, &m.CreatedByName, &m.UpdatedByName, &m.CreatedAt, &m.UpdatedAt, &m.VoidReason, &m.DriverID, &m.DriverName, &m.OilType)
	// Present historical aliases with the current terminology without rewriting ledger snapshots.
	for i := range m.Services {
		if m.Services[i].TypeCode == "oil" || m.Services[i].TypeCode == "preventive" {
			m.Services[i].TypeCode = "preventive"
			m.Services[i].Name = "PM Service"
			if m.Services[i].TrackingCode == "oil" {
				m.Services[i].TrackingCode = "pm"
			}
		}
	}
	m.Documents = []MaintenanceDocument{}
	return m, err
}
func (r *MaintenanceRepository) Get(ctx context.Context, id string) (MaintenanceRecord, error) {
	m, err := scanMaintenance(r.pool.QueryRow(ctx, maintenanceSelect+" WHERE m.id=$1", id))
	if err != nil {
		return m, mapNotFound(err)
	}
	if m.ExpenseID != nil {
		e, err := NewExpenseRepository(r.pool).GetExpense(ctx, *m.ExpenseID)
		if err != nil {
			return m, err
		}
		m.Expense = &e
	}
	rows, err := r.pool.Query(ctx, `SELECT id,file_name,content_type,size_bytes,created_at FROM maintenance_documents WHERE record_id=$1 ORDER BY created_at`, id)
	if err != nil {
		return m, err
	}
	defer rows.Close()
	for rows.Next() {
		var d MaintenanceDocument
		if err := rows.Scan(&d.ID, &d.FileName, &d.ContentType, &d.SizeBytes, &d.CreatedAt); err != nil {
			return m, err
		}
		m.Documents = append(m.Documents, d)
	}
	return m, rows.Err()
}
func (r *MaintenanceRepository) List(ctx context.Context, q MaintenanceQuery) (MaintenancePage, error) {
	where := ` WHERE ($1='' OR m.truck_id=NULLIF($1,'')::uuid) AND ($2='' OR m.vendor_id=NULLIF($2,'')::uuid) AND ($3='' OR EXISTS(SELECT 1 FROM jsonb_array_elements(m.services) s WHERE CASE WHEN s->>'typeCode'='oil' THEN 'preventive' ELSE s->>'typeCode' END=$3)) AND ($4='' OR m.status=$4) AND ($5='' OR concat_ws(' ',m.vendor_name,t.unit_number,e.unit_number,m.source_unit_number,m.work_order,m.notes,m.services::text,e.reference_number) ILIKE '%'||$5||'%') AND ($6='' OR coalesce(m.service_date,e.expense_date)>=NULLIF($6,'')::date) AND ($7='' OR coalesce(m.service_date,e.expense_date)<=NULLIF($7,'')::date) AND ($8='' OR m.expense_id=NULLIF($8,'')::uuid)`
	args := []any{q.TruckID, q.VendorID, q.TypeCode, q.Status, q.Search, q.From, q.To, q.ExpenseID}
	var count int
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM maintenance_records m LEFT JOIN trucks t ON t.id=m.truck_id LEFT JOIN expenses e ON e.id=m.expense_id`+where, args...).Scan(&count); err != nil {
		return MaintenancePage{}, err
	}
	p := Pagination{Page: q.Page, PageSize: q.PageSize}.Normalize(count)
	rows, err := r.pool.Query(ctx, maintenanceSelect+where+` ORDER BY coalesce(m.service_date,e.expense_date) DESC NULLS LAST,m.created_at DESC,m.id LIMIT $9 OFFSET $10`, append(args, p.PageSize, p.Offset())...)
	if err != nil {
		return MaintenancePage{}, err
	}
	items := []MaintenanceRecord{}
	for rows.Next() {
		m, err := scanMaintenance(rows)
		if err != nil {
			rows.Close()
			return MaintenancePage{}, err
		}
		items = append(items, m)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return MaintenancePage{}, err
	}
	// Load only displayed records, leaving invoice data under the same category access boundary.
	for i := range items {
		m, err := r.Get(ctx, items[i].ID)
		if err != nil {
			return MaintenancePage{}, err
		}
		items[i] = m
	}
	pages := (count + p.PageSize - 1) / p.PageSize
	if pages < 1 {
		pages = 1
	}
	return MaintenancePage{Items: items, Total: count, Page: p.Page, PageSize: p.PageSize, TotalPages: pages}, nil
}

func (r *MaintenanceRepository) Save(ctx context.Context, in MaintenanceInput) (MaintenanceRecord, error) {
	in.OilType = NormalizeMaintenanceOilType(in.OilType)
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return MaintenanceRecord{}, err
	}
	defer tx.Rollback(ctx)
	var old MaintenanceRecord
	var before []byte
	if in.Version > 0 {
		old, err = scanMaintenance(tx.QueryRow(ctx, maintenanceSelect+" WHERE m.id=$1 AND m.version=$2 FOR UPDATE OF m", in.ID, in.Version))
		if errors.Is(err, pgx.ErrNoRows) {
			return old, ErrMaintenanceConflict
		}
		if err != nil {
			return old, err
		}
		if old.Status == "voided" {
			return old, ErrMaintenanceInvalid
		}
		if old.ExpenseID != nil {
			e, err := scanExpense(tx.QueryRow(ctx, selectExpensesSQL+" WHERE e.id=$1", *old.ExpenseID))
			if err != nil {
				return old, err
			}
			old.Expense = &e
		}
		before, _ = json.Marshal(old)
	} else {
		var exists bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM maintenance_records WHERE id=$1)", in.ID).Scan(&exists); err != nil {
			return old, err
		}
		if exists {
			return old, ErrMaintenanceConflict
		}
	}
	// Lock the truck to serialize new service readings, manual readings and corrections.
	if in.TruckID != nil {
		var id string
		if err := tx.QueryRow(ctx, "SELECT id FROM trucks WHERE id=$1 FOR UPDATE", *in.TruckID).Scan(&id); err != nil {
			return old, mapNotFound(err)
		}
	}
	names := []string{}
	costs := []ExpenseCostItem{}
	if len(in.Services) == 1 && in.Expense != nil && in.Expense.BaseAmount != nil {
		in.Services[0].Amount = in.Expense.BaseAmount
	}
	for index, work := range in.Services {
		work.TypeCode = MaintenanceTypeCode(work.TypeCode)
		in.Services[index].TypeCode = work.TypeCode
		var active bool
		var name string
		if err := tx.QueryRow(ctx, "SELECT active,name FROM maintenance_types WHERE code=$1", work.TypeCode).Scan(&active, &name); err != nil {
			return old, ErrMaintenanceInvalid
		}
		if !active {
			unchanged := false
			for _, previous := range old.Services {
				if previous.TypeCode == work.TypeCode {
					unchanged = true
				}
			}
			if !unchanged {
				return old, ErrMaintenanceInvalid
			}
		}
		in.Services[index].Name = name
		in.Services[index].TrackingCode = MaintenanceTrackingCode(work.TypeCode)
		names = append(names, name)
		if work.Amount != nil {
			costs = append(costs, ExpenseCostItem{Name: name, Amount: *work.Amount})
		}
	}
	var driverName string
	if in.DriverID != nil {
		if err := tx.QueryRow(ctx, "SELECT full_name FROM drivers WHERE id=$1", in.DriverID).Scan(&driverName); err != nil {
			return old, mapNotFound(err)
		}
	}
	if in.VendorID != nil && (old.VendorID == nil || *old.VendorID != *in.VendorID) {
		var vendorName, vendorAddress, vendorPhone, vendorContact string
		if err := tx.QueryRow(ctx, `SELECT name,address,phone,contact FROM maintenance_vendors WHERE id=$1 AND active`, *in.VendorID).Scan(&vendorName, &vendorAddress, &vendorPhone, &vendorContact); err != nil {
			return old, ErrMaintenanceInvalid
		}
		if in.VendorName == "" {
			in.VendorName = vendorName
		}
		if in.VendorAddress == "" {
			in.VendorAddress = vendorAddress
		}
		if in.VendorPhone == "" {
			in.VendorPhone = vendorPhone
		}
		if in.VendorContact == "" {
			in.VendorContact = vendorContact
		}
	}
	expenseID := old.ExpenseID
	if in.Expense != nil {
		in.Expense.TruckID = in.TruckID
		in.Expense.DriverID = in.DriverID
		in.Expense.DriverName = &driverName
		if in.Expense.CostBreakdown == nil {
			in.Expense.CostBreakdown = &costs
		}
		title := []rune(strings.Join(names, ", "))
		title = title[:min(100, len(title))]
		titleText := string(title)
		in.Expense.ExpenseType = &titleText
		if expenseID != nil {
			if in.Expense.UpdatedAt == nil {
				return old, ErrMaintenanceConflict
			}
			if err := updateExpenseRow(ctx, tx, *expenseID, *in.Expense); err != nil {
				return old, err
			}
		} else {
			id, err := insertExpense(ctx, tx, *in.Expense)
			if err != nil {
				return old, err
			}
			expenseID = &id
			// The compatibility trigger creates a placeholder; this write owns the complete visit.
			if _, err := tx.Exec(ctx, "DELETE FROM maintenance_records WHERE expense_id=$1", id); err != nil {
				return old, err
			}
		}
	} else if expenseID != nil {
		return old, fmt.Errorf("%w: use Void to remove an unpaid charge", ErrMaintenanceInvalid)
	}
	services, err := json.Marshal(in.Services)
	if err != nil {
		return old, err
	}
	if in.Version == 0 {
		_, err = tx.Exec(ctx, `INSERT INTO maintenance_records(id,expense_id,truck_id,vendor_id,vendor_name,vendor_address,vendor_phone,vendor_contact,service_date,odometer,location,status,work_order,notes,services,created_by_name,updated_by_name,created_by) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$16,NULLIF($17,'')::uuid)`, in.ID, expenseID, in.TruckID, in.VendorID, in.VendorName, in.VendorAddress, in.VendorPhone, in.VendorContact, in.ServiceDate, in.Odometer, in.Location, in.Status, in.WorkOrder, in.Notes, services, in.Actor, in.ActorID)
	} else {
		_, err = tx.Exec(ctx, `UPDATE maintenance_records SET expense_id=$2,truck_id=$3,vendor_id=$4,vendor_name=$5,vendor_address=$6,vendor_phone=$7,vendor_contact=$8,service_date=$9,odometer=$10,location=$11,status=$12,work_order=$13,notes=$14,services=$15,updated_by_name=$16,version=version+1,updated_at=now() WHERE id=$1`, in.ID, expenseID, in.TruckID, in.VendorID, in.VendorName, in.VendorAddress, in.VendorPhone, in.VendorContact, in.ServiceDate, in.Odometer, in.Location, in.Status, in.WorkOrder, in.Notes, services, in.Actor)
	}
	if err != nil {
		return old, err
	}
	if _, err = tx.Exec(ctx, "UPDATE maintenance_records SET driver_id=$2,driver_name=$3,source_unit_number=CASE WHEN $4<>'' THEN $4 ELSE source_unit_number END,oil_type=$5 WHERE id=$1", in.ID, in.DriverID, driverName, in.SourceUnitNumber, in.OilType); err != nil {
		return old, err
	}
	if _, err := tx.Exec(ctx, "UPDATE maintenance_odometer_readings SET active=false WHERE record_id=$1", in.ID); err != nil {
		return old, err
	}
	if in.Status == "completed" && in.Odometer != nil {
		if err := checkMaintenanceReading(ctx, tx, *in.TruckID, *in.ServiceDate, *in.Odometer, in.ID, ""); err != nil {
			return old, err
		}
		_, err = tx.Exec(ctx, `INSERT INTO maintenance_odometer_readings(truck_id,record_id,reading_date,mileage,source,actor_name) VALUES($1,$2,$3,$4,'service',$5) ON CONFLICT(record_id) DO UPDATE SET truck_id=excluded.truck_id,reading_date=excluded.reading_date,mileage=excluded.mileage,active=true,actor_name=excluded.actor_name,version=maintenance_odometer_readings.version+1,updated_at=now()`, in.TruckID, in.ID, in.ServiceDate, in.Odometer, in.Actor)
		if err != nil {
			return old, err
		}
	}
	after, err := scanMaintenance(tx.QueryRow(ctx, maintenanceSelect+" WHERE m.id=$1", in.ID))
	if err != nil {
		return old, err
	}
	if after.ExpenseID != nil {
		e, err := scanExpense(tx.QueryRow(ctx, selectExpensesSQL+" WHERE e.id=$1", *after.ExpenseID))
		if err != nil {
			return old, err
		}
		after.Expense = &e
	}
	afterJSON, _ := json.Marshal(after)
	if _, err := tx.Exec(ctx, "INSERT INTO maintenance_changes(record_id,actor_name,before_value,after_value) VALUES($1,$2,$3,$4)", in.ID, in.Actor, before, afterJSON); err != nil {
		return old, err
	}
	if err := tx.Commit(ctx); err != nil {
		return old, err
	}
	return r.Get(ctx, in.ID)
}

// MaintenanceTypeCode accepts old clients and import commands using the retired type.
func MaintenanceTypeCode(code string) string {
	if code == "oil" {
		return "preventive"
	}
	return code
}

// Schedule completion follows the selected service type, independent of payer.
func MaintenanceTrackingCode(code string) string {
	switch code {
	case "preventive", "oil":
		return "pm"
	default:
		return ""
	}
}

func (r *MaintenanceRepository) Void(ctx context.Context, id string, version int, reason, actor string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	old, err := scanMaintenance(tx.QueryRow(ctx, maintenanceSelect+" WHERE m.id=$1 AND m.version=$2 AND m.status<>'voided' FOR UPDATE OF m", id, version))
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrMaintenanceConflict
	}
	if err != nil {
		return err
	}
	before, _ := json.Marshal(old)
	var expenseJSON []byte
	if old.ExpenseID != nil {
		var paid bool
		if err := tx.QueryRow(ctx, `SELECT driver_settled OR EXISTS(SELECT 1 FROM expense_payments WHERE expense_id=e.id) OR manager_verified OR accounting_verified FROM expenses e WHERE id=$1 FOR UPDATE`, *old.ExpenseID).Scan(&paid); err != nil {
			return err
		}
		if paid {
			return fmt.Errorf("%w: paid or verified maintenance charges cannot be voided", ErrMaintenanceInvalid)
		}
		if err := tx.QueryRow(ctx, "SELECT to_jsonb(e) FROM expenses e WHERE id=$1", *old.ExpenseID).Scan(&expenseJSON); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE maintenance_records SET status='voided',void_reason=$3,voided_expense=$4,expense_id=NULL,version=version+1,updated_by_name=$5,updated_at=now() WHERE id=$1 AND version=$2`, id, version, reason, expenseJSON, actor); err != nil {
		return err
	}
	if old.ExpenseID != nil {
		if _, err := tx.Exec(ctx, "DELETE FROM expenses WHERE id=$1", *old.ExpenseID); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, "UPDATE maintenance_odometer_readings SET active=false WHERE record_id=$1", id); err != nil {
		return err
	}
	after, err := scanMaintenance(tx.QueryRow(ctx, maintenanceSelect+" WHERE m.id=$1", id))
	if err != nil {
		return err
	}
	afterJSON, _ := json.Marshal(after)
	if _, err := tx.Exec(ctx, "INSERT INTO maintenance_changes(record_id,actor_name,before_value,after_value) VALUES($1,$2,$3,$4)", id, actor, before, afterJSON); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (r *MaintenanceRepository) History(ctx context.Context, id string) ([]json.RawMessage, error) {
	rows, err := r.pool.Query(ctx, "SELECT jsonb_build_object('actorName',actor_name,'happenedAt',happened_at,'before',before_value,'after',after_value) FROM maintenance_changes WHERE record_id=$1 ORDER BY id DESC LIMIT 100", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []json.RawMessage{}
	for rows.Next() {
		var v json.RawMessage
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		items = append(items, v)
	}
	return items, rows.Err()
}

type MaintenanceReportRow struct {
	Group     string `json:"group"`
	Amount    string `json:"amount"`
	WorkItems int    `json:"workItems"`
	Visits    int    `json:"visits"`
}
type MaintenanceReport struct {
	ServiceTypes []MaintenanceReportRow `json:"serviceTypes"`
	Trucks       []MaintenanceReportRow `json:"trucks"`
	Vendors      []MaintenanceReportRow `json:"vendors"`
	RepeatedWork []MaintenanceReportRow `json:"repeatedWork"`
	Total        string                 `json:"total"`
	Fees         string                 `json:"fees"`
	Unallocated  string                 `json:"unallocated"`
}

func (r *MaintenanceRepository) Reports(ctx context.Context, q MaintenanceQuery) (MaintenanceReport, error) {
	args := []any{q.TruckID, q.From, q.To, q.VendorID, q.TypeCode, q.Status, q.Search}
	where := `m.status<>'voided' AND ($1='' OR m.truck_id=NULLIF($1,'')::uuid) AND ($2='' OR coalesce(m.service_date,e.expense_date)>=NULLIF($2,'')::date) AND ($3='' OR coalesce(m.service_date,e.expense_date)<=NULLIF($3,'')::date) AND ($4='' OR m.vendor_id=NULLIF($4,'')::uuid) AND ($5='' OR EXISTS(SELECT 1 FROM jsonb_array_elements(m.services) filter_work WHERE CASE WHEN filter_work->>'typeCode'='oil' THEN 'preventive' ELSE filter_work->>'typeCode' END=$5)) AND ($6='' OR m.status=$6) AND ($7='' OR concat_ws(' ',m.vendor_name,(SELECT unit_number FROM trucks WHERE id=m.truck_id),e.unit_number,m.source_unit_number,m.work_order,m.notes,m.services::text,e.reference_number) ILIKE '%'||$7||'%')`
	out := MaintenanceReport{ServiceTypes: []MaintenanceReportRow{}, Trucks: []MaintenanceReportRow{}, Vendors: []MaintenanceReportRow{}, RepeatedWork: []MaintenanceReportRow{}}
	err := r.pool.QueryRow(ctx, `SELECT coalesce(sum(coalesce(e.base_amount,e.amount)),0)::text,coalesce(sum(e.payment_fee),0)::text,coalesce(sum(coalesce(e.base_amount,e.amount)) FILTER(WHERE NOT EXISTS(SELECT 1 FROM jsonb_array_elements(m.services) s WHERE s->>'amount' IS NOT NULL)),0)::text FROM maintenance_records m LEFT JOIN expenses e ON e.id=m.expense_id WHERE `+where, args...).Scan(&out.Total, &out.Fees, &out.Unallocated)
	if err != nil {
		return out, err
	}
	for _, group := range []string{"types", "trucks", "vendors", "repeated"} {
		selectGroup := `coalesce(t.unit_number,'Unlinked truck')`
		joins := `LEFT JOIN trucks t ON t.id=m.truck_id`
		amount := `coalesce(e.base_amount,e.amount,0)`
		work := `coalesce(sum(jsonb_array_length(m.services)),0)`
		having := ""
		if group == "vendors" {
			selectGroup = `coalesce(vendor.name,nullif(m.vendor_name,''),'Unknown vendor')`
			joins += ` LEFT JOIN maintenance_vendors vendor ON vendor.id=m.vendor_id`
		}
		if group == "types" || group == "repeated" {
			joins += ` CROSS JOIN LATERAL jsonb_array_elements(m.services) s LEFT JOIN maintenance_types st ON st.code=CASE WHEN s->>'typeCode'='oil' THEN 'preventive' ELSE s->>'typeCode' END`
			selectGroup = `coalesce(st.name,'Other maintenance')`
			amount = `coalesce((s->>'amount')::numeric,0)`
			work = `count(*)`
			if group == "repeated" {
				selectGroup = `concat(coalesce(t.unit_number,'Unlinked truck'),' · ',coalesce(st.name,'Other maintenance'))`
				having = ` HAVING count(DISTINCT m.id)>1`
			}
		}
		reportWhere := where
		if group == "repeated" {
			reportWhere += ` AND s->>'typeCode' NOT IN ('preventive','oil','cleaning','other')`
		}
		rows, err := r.pool.Query(ctx, `SELECT `+selectGroup+`,sum(`+amount+`)::text,`+work+`,count(DISTINCT m.id) FROM maintenance_records m LEFT JOIN expenses e ON e.id=m.expense_id `+joins+` WHERE `+reportWhere+` GROUP BY 1`+having+` ORDER BY sum(`+amount+`) DESC,1`, args...)
		if err != nil {
			return out, err
		}
		values := []MaintenanceReportRow{}
		for rows.Next() {
			var v MaintenanceReportRow
			if err := rows.Scan(&v.Group, &v.Amount, &v.WorkItems, &v.Visits); err != nil {
				rows.Close()
				return out, err
			}
			values = append(values, v)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return out, err
		}
		switch group {
		case "types":
			out.ServiceTypes = values
		case "trucks":
			out.Trucks = values
		case "vendors":
			out.Vendors = values
		case "repeated":
			out.RepeatedWork = values
		}
	}
	return out, nil
}

func cleanMaintenanceText(s string) string { return strings.TrimSpace(s) }

var maintenanceOilGrade = regexp.MustCompile(`^(?:SAE\s*)?(\d{1,2})\s*W\s*[- ]?\s*(\d{2})$`)

// Canonicalize a stated viscosity grade, while preserving custom source names.
func NormalizeMaintenanceOilType(value string) string {
	value = strings.TrimSpace(value)
	if match := maintenanceOilGrade.FindStringSubmatch(strings.ToUpper(value)); match != nil {
		return match[1] + "W-" + match[2]
	}
	return value
}
