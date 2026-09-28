package repository

import "context"

func (r *LoadRepository) UpdateLoadDetails(ctx context.Context, l LoadRecord) error {
	result, err := r.pool.Exec(ctx, `UPDATE loads SET status=$2,load_pay=$3,total_other_pay=$4,total_pay=$5,
 total_miles=$6,per_mile_revenue=$7,pickup_time=$8,delivery_time=$9,pickup_appointment_time=$10,
 delivery_appointment_time=$11,raw_payload=$12,synced_at=$13 WHERE id=$1`,
		l.ID, l.Status, l.LoadPay, l.TotalOtherPay, l.TotalPay, l.TotalMiles, l.PerMileRevenue,
		l.PickupTime, l.DeliveryTime, l.PickupAppointmentTime, l.DeliveryAppointmentTime, l.RawPayload, l.SyncedAt)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
