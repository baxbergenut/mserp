package repository

import (
	"encoding/json"
	"testing"

	"mserp/internal/prepass"
)

func TestMapPrePassToll(t *testing.T) {
	value, err := mapPrePassToll("production", prepass.Transaction{
		TollID:            42,
		AccountNumber:     json.Number("123456"),
		PostDateTime:      "2026-07-20T02:21:01Z",
		InvoiceDateTime:   "2026-07-31T12:00:00Z",
		DeviceNumber:      "00409740958",
		VehicleNumber:     " 2000 ",
		PPDeviceID:        "1234567",
		TollAgencyCode:    "OTC",
		BillingAgencyCode: "EZPass",
		EntryDateTime:     "2026-07-19T09:29:17Z",
		EntryPlazaCode:    "161",
		ReadType:          "DEVICE",
		ExitDateTime:      "2026-07-19T10:16:04Z",
		ExitPlazaCode:     "211",
		TollClass:         "5",
		TollCharge:        json.Number("12.30"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if value.AmountCents != 1230 ||
		value.EquipmentUnit != "2000" ||
		value.Agency != "OTC" ||
		value.Source != "EZPass" ||
		value.ExitTime != "10:16:04" {
		t.Fatalf("mapped toll = %+v", value)
	}
}

func TestDecimalToCentsRejectsFractionalCents(t *testing.T) {
	if _, err := decimalToCents("12.345"); err == nil {
		t.Fatal("decimalToCents() error = nil, want an error")
	}
}
