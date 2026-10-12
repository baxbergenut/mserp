package repository

import (
	"context"
)

type ChargeCell struct {
	DriverID    string `json:"driverId"`
	TypeID      string `json:"typeId"`
	WeekStart   string `json:"weekStart"`
	Included    bool   `json:"included"`
	Amount      string `json:"amount"`
	ScheduleID  string `json:"scheduleId"`
	Version     int    `json:"version"`
	TypeVersion int    `json:"typeVersion"`
}

func (r *DriverChargeRepository) SaveCell(ctx context.Context, c ChargeCell, actor string) error {
	return chargeInvalid("Recurring charges are managed on trucks")
}
