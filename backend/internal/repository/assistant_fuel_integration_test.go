package repository

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestDriverFuelReportIntegration(t *testing.T) {
	databaseURL := os.Getenv("ASSISTANT_INTEGRATION_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("assistant integration database is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var driverID string
	if err := pool.QueryRow(ctx, `INSERT INTO drivers(full_name,normalized_name,is_owner_operator,pay_type,pay_rate) VALUES('Fuel Test Driver','fuel test driver',false,'cpm',0.65) RETURNING id::text`).Scan(&driverID); err != nil {
		t.Fatal(err)
	}
	var transactionID string
	err = pool.QueryRow(ctx, `INSERT INTO fuel_transactions(
		relay_environment,relay_transaction_id,driver_id,relay_driver_id,purchased_at,
		total_amount_paid,total_retail_price,total_amount_saved,cash_advance,is_direct_bill,currency_code,
		merchant_id,merchant_name,merchant_number,location_id,location_name,merchant_location_id,
		address,city,state,postal_code,latitude,longitude,timezone,raw_payload
	)VALUES('production','assistant-test-1',$1,'relay-driver',TIMESTAMPTZ '2026-08-25 02:00:00+00',
		137.00,150.00,13.00,10.00,false,'USD','merchant','Test Fuel','1','location','Test Fuel Stop','ml',
		'1 Main St','New York','NY','10001',40.0,-74.0,'America/New_York','{}') RETURNING id::text`, driverID).Scan(&transactionID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO fuel_transaction_items(fuel_transaction_id,line_number,item_kind,category,quantity,unit_of_measure,total_amount_paid)VALUES
		($1,1,'fuel','diesel',25.000,'gallons',100.00),($1,2,'fuel','def',4.000,'gallons',20.00),($1,3,'product','oil',1.000,'each',5.00)`, transactionID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO fuel_transaction_fees(fuel_transaction_id,fee_type,amount)VALUES($1,'service',2.00)`, transactionID); err != nil {
		t.Fatal(err)
	}
	report, err := NewFuelRepository(pool).GetDriverFuelReport(ctx, driverID, time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC), time.Date(2026, 8, 30, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if report.TransactionCount != 1 || report.TotalCharged != "137.00" || report.FuelAmount != "100.00" || report.FuelGallons != "25.000" || report.DEFAmount != "20.00" || report.DEFGallons != "4.000" || report.OtherAmount != "5.00" || report.Fees != "2.00" {
		t.Fatalf("unexpected report totals: %+v", report)
	}
	if len(report.Transactions) != 1 || report.Transactions[0].Date != "2026-08-24" || !report.Transactions[0].Reconciles {
		t.Fatalf("merchant-local transaction did not reconcile: %+v", report.Transactions)
	}
	assistantRepo := NewAssistantRepository(pool)
	if _, err := assistantRepo.ListAudit(ctx, 50); err != nil {
		t.Fatalf("list combined assistant audit: %v", err)
	}
	if err := assistantRepo.Cleanup(ctx); err != nil {
		t.Fatalf("assistant cleanup: %v", err)
	}
}
