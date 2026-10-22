package repository

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// Retain the original paid confirmation and append the invoice values explicitly
// corrected by this save. The ordinary invoice/settlement guards remain in force.
func recordMaintenanceInvoiceCorrection(ctx context.Context, tx pgx.Tx, old MaintenanceRecord, actor string) error {
	if len(old.PaymentConfirmation) == 0 || old.Expense == nil {
		return nil
	}
	_, err := tx.Exec(ctx, `UPDATE maintenance_records m SET payment_confirmation=jsonb_set(payment_confirmation,'{invoiceCorrections}',
 coalesce(payment_confirmation->'invoiceCorrections','[]'::jsonb)||jsonb_build_array(jsonb_build_object(
 'expenseId',e.id::text,'amount',e.amount::text,'method',coalesce(e.payment_type,''),'reference',coalesce(e.reference_number,''),
 'paidBy',coalesce(e.paid_by,''),'correctedBy',$2::text,'correctedAt',now())))
 FROM expenses e WHERE m.id=$1 AND e.id=m.expense_id AND
 (e.amount IS DISTINCT FROM $3::numeric OR coalesce(e.payment_type,'') IS DISTINCT FROM coalesce($4::text,'')
 OR coalesce(e.reference_number,'') IS DISTINCT FROM coalesce($5::text,'') OR coalesce(e.paid_by,'') IS DISTINCT FROM coalesce($6::text,''))`,
		old.ID, actor, old.Expense.Amount, old.Expense.PaymentType, old.Expense.ReferenceNumber, old.Expense.PaidBy)
	return err
}
