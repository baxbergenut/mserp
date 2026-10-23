package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// Maintenance is an extension of an ordinary expense, under its category access.
// The existing service rows remain the source for odometer and program history.
type ExpenseMaintenance struct {
	RecordID      string  `json:"recordId,omitempty"`
	Version       int     `json:"version,omitempty"`
	Odometer      *int    `json:"odometer"`
	VendorID      *string `json:"vendorId"`
	VendorName    string  `json:"vendorName"`
	VendorAddress string  `json:"vendorAddress"`
	VendorPhone   string  `json:"vendorPhone"`
	VendorContact string  `json:"vendorContact"`
	OilType       string  `json:"oilType"`
}

func expenseMaintenanceEnabled(ctx context.Context, tx pgx.Tx) (bool, error) {
	var enabled bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_attribute WHERE attrelid=to_regclass('maintenance_records') AND attname='expense_entry' AND NOT attisdropped)`).Scan(&enabled)
	return enabled, err
}

func prepareExpenseMaintenance(ctx context.Context, tx pgx.Tx, in ExpenseInput) error {
	enabled, err := expenseMaintenanceEnabled(ctx, tx)
	if err != nil || !enabled {
		return err
	}
	if err = setBoardActor(ctx, tx, in.CreatedBy, "expense"); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock_shared(736281940)`)
	return err
}

func syncExpenseMaintenance(ctx context.Context, tx pgx.Tx, id string, in ExpenseInput, previous *Expense) error {
	enabled, err := expenseMaintenanceEnabled(ctx, tx)
	if err != nil || !enabled {
		return err
	}
	var category string
	if err = tx.QueryRow(ctx, `SELECT category_id::text FROM maintenance_configuration`).Scan(&category); err != nil {
		return err
	}
	if in.CategoryID != category {
		if in.Maintenance != nil {
			return fmt.Errorf("%w: shop details require the Maintenance category", ErrMaintenanceInvalid)
		}
		return nil
	}
	if _, err = tx.Exec(ctx, `INSERT INTO maintenance_records(expense_id,truck_id,location,services,notes,created_by_name,created_by) SELECT id,truck_id,'unknown',maintenance_legacy_services(expense_type,'[]'::jsonb),coalesce(description,''),coalesce(created_by_name,''),created_by FROM expenses WHERE id=$1 ON CONFLICT(expense_id) DO NOTHING`, id); err != nil {
		return err
	}
	old, err := scanMaintenance(tx.QueryRow(ctx, maintenanceSelect+` WHERE m.expense_id=$1 FOR UPDATE OF m`, id))
	if err != nil {
		return err
	}
	if old.Status == "voided" {
		return ErrMaintenanceInvalid
	}
	details := in.Maintenance
	if details == nil {
		details = &ExpenseMaintenance{Odometer: old.Odometer, VendorID: old.VendorID, VendorName: old.VendorName, VendorAddress: old.VendorAddress, VendorPhone: old.VendorPhone, VendorContact: old.VendorContact, OilType: old.OilType}
	} else if details.Version != 0 && details.Version != old.Version {
		return ErrExpenseSettingConflict
	}
	if details.RecordID != "" && details.RecordID != old.ID {
		return ErrExpenseSettingConflict
	}
	if in.TruckID != nil {
		if _, err = tx.Exec(ctx, `SELECT id FROM trucks WHERE id=$1 FOR UPDATE`, *in.TruckID); err != nil {
			return err
		}
	}
	before, _ := json.Marshal(old)
	// The original confirmation is kept; only an explicit edit appends a correction.
	// updateExpenseRow has already saved the new invoice, so read the last confirmed
	// snapshot here rather than silently replacing the original payment evidence.
	if len(old.PaymentConfirmation) > 0 {
		_, err = tx.Exec(ctx, `UPDATE maintenance_records m SET payment_confirmation=jsonb_set(payment_confirmation,'{invoiceCorrections}',coalesce(payment_confirmation->'invoiceCorrections','[]'::jsonb)||jsonb_build_array(jsonb_build_object('expenseId',e.id::text,'amount',e.amount::text,'method',coalesce(e.payment_type,''),'reference',coalesce(e.reference_number,''),'paidBy',coalesce(e.paid_by,''),'correctedBy',$2::text,'correctedAt',now()))) FROM expenses e WHERE m.id=$1 AND e.id=m.expense_id AND (coalesce(m.payment_confirmation->'invoiceCorrections'->-1,m.payment_confirmation)->>'amount' IS DISTINCT FROM e.amount::text OR coalesce(coalesce(m.payment_confirmation->'invoiceCorrections'->-1,m.payment_confirmation)->>'method','') IS DISTINCT FROM coalesce(e.payment_type,'') OR coalesce(coalesce(m.payment_confirmation->'invoiceCorrections'->-1,m.payment_confirmation)->>'reference','') IS DISTINCT FROM coalesce(e.reference_number,'') OR coalesce(coalesce(m.payment_confirmation->'invoiceCorrections'->-1,m.payment_confirmation)->>'paidBy','') IS DISTINCT FROM coalesce(e.paid_by,''))`, old.ID, in.CreatedByName)
		if err != nil {
			return err
		}
	}
	services := old.Services
	// Preserve legacy multi-type/program evidence until the expense type changes.
	var title string
	if in.ExpenseType != nil {
		title = strings.TrimSpace(*in.ExpenseType)
	}
	programs := old.ProgramIDs
	if previous == nil || previous.CategoryID != category || previous.ExpenseType == nil || title != strings.TrimSpace(*previous.ExpenseType) {
		var code, name string
		var program *string
		err = tx.QueryRow(ctx, `SELECT code,name,program_id::text FROM maintenance_types WHERE lower(btrim(name))=lower(btrim($1)) ORDER BY active DESC LIMIT 1`, title).Scan(&code, &name, &program)
		if errors.Is(err, pgx.ErrNoRows) {
			code, name, err = "other", title, nil
		}
		if err != nil {
			return err
		}
		services = []MaintenanceWork{{TypeCode: code, Name: name, TrackingCode: MaintenanceTrackingCode(code)}}
		programs = []string{}
		if program != nil {
			programs = append(programs, *program)
		}
	}
	for i := range services {
		services[i].Amount = nil
	}
	if services == nil {
		services = []MaintenanceWork{}
	}
	if programs == nil {
		programs = []string{}
	}
	work, err := json.Marshal(services)
	if err != nil {
		return err
	}
	// Existing incomplete work never becomes a program completion merely because
	// it was migrated. New expense entries are dated service history when linked.
	status := old.Status
	if previous == nil || previous.CategoryID != category {
		if in.TruckID != nil {
			status = "completed"
		}
	}
	if in.TruckID == nil && status == "completed" {
		status = "needs_review"
	}
	_, err = tx.Exec(ctx, `UPDATE maintenance_records SET expense_entry=true,truck_id=$2,driver_id=$3,driver_name=coalesce($4,''),source_unit_number=coalesce($5,''),service_date=$6,odometer=$7,vendor_id=$8,vendor_name=$9,vendor_address=$10,vendor_phone=$11,vendor_contact=$12,oil_type=$13,notes=coalesce($14,''),services=$15,program_ids=$16,status=$17,updated_by_name=$18,version=version+1,updated_at=now() WHERE id=$1`, old.ID, in.TruckID, in.DriverID, in.DriverName, in.UnitNumber, in.ExpenseDate, details.Odometer, details.VendorID, details.VendorName, details.VendorAddress, details.VendorPhone, details.VendorContact, NormalizeMaintenanceOilType(details.OilType), in.Description, work, programs, status, in.CreatedByName)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE maintenance_odometer_readings SET active=false WHERE record_id=$1`, old.ID); err != nil {
		return err
	}
	if status == "completed" && in.TruckID != nil && details.Odometer != nil {
		if err = checkMaintenanceReading(ctx, tx, *in.TruckID, in.ExpenseDate, *details.Odometer, old.ID, ""); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO maintenance_odometer_readings(truck_id,record_id,reading_date,mileage,source,actor_name) VALUES($1,$2,$3,$4,'service',$5) ON CONFLICT(record_id) DO UPDATE SET truck_id=excluded.truck_id,reading_date=excluded.reading_date,mileage=excluded.mileage,active=true,actor_name=excluded.actor_name,version=maintenance_odometer_readings.version+1,updated_at=now()`, in.TruckID, old.ID, in.ExpenseDate, details.Odometer, in.CreatedByName)
		if err != nil {
			return err
		}
	}
	_, err = tx.Exec(ctx, `INSERT INTO maintenance_changes(record_id,actor_name,before_value,after_value) SELECT id,$2,$3,to_jsonb(m) FROM maintenance_records m WHERE id=$1`, old.ID, in.CreatedByName, before)
	return err
}
