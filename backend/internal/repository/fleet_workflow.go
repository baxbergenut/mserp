package repository

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

var ErrTruckUnavailable = fmt.Errorf("truck is unavailable; Fleet must release it before assigning another load")

// Lock the assigned truck before checking, sharing the work-order writer's fence.
// Existing load records and historical accounting corrections remain editable.
func requireDispatchableTruck(ctx context.Context, tx pgx.Tx, driver string) error {
	var available bool
	if err := tx.QueryRow(ctx, `SELECT NOT fleet_status_enabled() OR availability_status='active' FROM drivers WHERE id=$1`, driver).Scan(&available); err != nil {
		return err
	}
	if !available {
		return chargeInvalid("Set the driver availability to Active before dispatch")
	}
	var truck string
	err := tx.QueryRow(ctx, `SELECT t.id::text FROM trucks t WHERE t.id=fleet_status_driver_truck($1) FOR UPDATE OF t`, driver).Scan(&truck)
	if err == pgx.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	var state string
	var active bool
	if err = tx.QueryRow(ctx, `SELECT fleet_truck_operating_status(id),active FROM trucks WHERE id=$1`, truck).Scan(&state, &active); err != nil {
		return err
	}
	if !active || state != "ready" {
		return ErrTruckUnavailable
	}
	return nil
}

func validateMaintenanceWorkflow(in *MaintenanceInput, old MaintenanceRecord) error {
	// Legacy/import writers keep their existing service semantics and cannot
	// accidentally clear the operating hold or workflow of a newer client.
	if in.WorkStatus == nil {
		in.WorkStatus = &old.WorkStatus
		in.DispatchStatus = &old.DispatchStatus
		in.WaitingOn, in.ScheduledFor, in.ExpectedReady = old.WaitingOn, old.ScheduledFor, old.ExpectedReady
		in.Priority, in.NoCharge, in.CancelReason = old.Priority, old.NoCharge, old.CancelReason
		if old.WorkStatus == "" || old.WorkStatus == "completed" || old.WorkStatus == "needs_review" {
			v := in.Status
			in.WorkStatus = &v
		}
	}
	if in.Priority == "" {
		in.Priority = "normal"
	}
	if in.DispatchStatus == nil {
		v := old.DispatchStatus
		in.DispatchStatus = &v
	}
	invalid := func(message string) error { return fmt.Errorf("%w: %s", ErrMaintenanceInvalid, message) }
	if old.HistoricalPaid {
		if in.ConfirmPayment || *in.DispatchStatus != "" {
			return invalid("historical paid work cannot create a new payment or truck hold")
		}
		switch *in.WorkStatus {
		case "completed", "needs_review", "closed":
			// Historical payment classification does not confirm unresolved
			// service/truck identity. Legacy correction forms retain that status.
			serviceStatus := in.Status
			in.WorkStatus = &serviceStatus
		default:
			return invalid("historical paid work cannot become an ongoing work order")
		}
	}
	switch *in.WorkStatus {
	case "open", "scheduled", "in_progress", "canceled", "needs_review":
		in.Status = "needs_review"
	case "completed", "closed":
		in.Status = "completed"
	default:
		return invalid("choose a valid work stage")
	}
	switch *in.DispatchStatus {
	case "", "shop", "out_of_service", "hold":
	default:
		return invalid("choose a valid truck status")
	}
	switch in.WaitingOn {
	case "", "shop", "diagnosis", "approval", "parts", "pickup":
	default:
		return invalid("choose a waiting reason")
	}
	switch in.Priority {
	case "normal", "high", "urgent":
	default:
		return invalid("choose a priority")
	}
	if in.TruckID == nil && *in.WorkStatus != "needs_review" {
		return invalid("select a truck")
	}
	if in.TruckID == nil && *in.DispatchStatus != "" {
		return invalid("select the truck to hold")
	}
	if old.DispatchStatus != "" && pmRecordID(old.TruckID) != pmRecordID(in.TruckID) {
		return invalid("release the original truck before changing trucks")
	}
	if old.DispatchStatus != "" && *in.DispatchStatus == "" && *in.WorkStatus != "completed" && *in.WorkStatus != "closed" && *in.WorkStatus != "canceled" {
		return invalid("complete or cancel the work before releasing the truck")
	}
	if (*in.WorkStatus == "open" || *in.WorkStatus == "scheduled" || *in.WorkStatus == "in_progress") && strings.TrimSpace(in.Notes) == "" {
		return invalid("describe the work needed")
	}
	if *in.WorkStatus == "scheduled" && in.ScheduledFor == nil {
		return invalid("enter a scheduled date")
	}
	if in.Status == "completed" && (in.TruckID == nil || in.ServiceDate == nil || len(in.Services) == 0) {
		return invalid("completed work requires a truck, service date and service type")
	}
	if *in.WorkStatus == "canceled" && len(strings.TrimSpace(in.CancelReason)) < 3 {
		return invalid("enter a cancellation reason")
	}
	if *in.WorkStatus == "canceled" && old.Status == "completed" {
		return invalid("completed service cannot be canceled; reopen it to correct completion details")
	}
	if in.NoCharge && (in.Expense != nil || old.ExpenseID != nil) {
		return invalid("an invoiced order cannot be marked no charge")
	}
	if in.ConfirmPayment && *in.WorkStatus != "closed" {
		return invalid("payment is confirmed in the final step")
	}
	if *in.WorkStatus == "closed" {
		if old.Status != "completed" {
			return invalid("finish the work before the payment step")
		}
		if !in.NoCharge && len(old.PaymentConfirmation) == 0 && !in.ConfirmPayment {
			return invalid("confirm payment using the invoice payment fields before finishing")
		}
		if *in.DispatchStatus != "" {
			return invalid("release this work order's truck hold before closing")
		}
		if in.Expense == nil && old.ExpenseID == nil && !in.NoCharge {
			return invalid("add the final invoice or confirm no charge before closing")
		}
	}
	return nil
}
