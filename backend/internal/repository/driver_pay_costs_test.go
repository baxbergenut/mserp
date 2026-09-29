package repository

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"testing"
)

func seedDriverPayCosts(t *testing.T, ctx context.Context, pool *pgxpool.Pool, driver string) {
	t.Helper()
	// Week boundaries must use merchant-local dates, not UTC. Staging, unlinked
	// identities, DEF and products must not enter diesel deductions; credits do.
	_, err := pool.Exec(ctx, `WITH fixtures(id,stamp,amount,env,linked,category,kind) AS (VALUES
 ('included','2026-09-29 03:00+00',100.10,'production',true,'diesel','fuel'),
 ('credit','2026-09-30 16:00+00',-10.00,'production',true,'diesel','fuel'),
 ('sunday','2026-10-05 03:59+00',0.20,'production',true,'diesel','fuel'),
 ('before','2026-09-28 03:59+00',999.00,'production',true,'diesel','fuel'),
 ('after','2026-10-05 04:00+00',999.00,'production',true,'diesel','fuel'),
 ('staging','2026-09-29 12:00+00',999.00,'staging',true,'diesel','fuel'),
 ('unassigned','2026-09-29 12:00+00',999.00,'production',false,'diesel','fuel'),
 ('def','2026-09-29 12:00+00',999.00,'production',true,'def','fuel'),
 ('product','2026-09-29 12:00+00',999.00,'production',true,'other','product')
 ), inserted AS (
 INSERT INTO fuel_transactions(relay_environment,relay_transaction_id,driver_id,relay_driver_id,purchased_at,total_amount_paid,total_retail_price,total_amount_saved,is_direct_bill,currency_code,merchant_id,merchant_name,merchant_number,location_id,location_name,merchant_location_id,address,city,state,postal_code,latitude,longitude,timezone,raw_payload)
 SELECT env,id,CASE WHEN linked THEN $1::uuid END,'identity',stamp::timestamptz,amount,amount,0,false,'USD','','','','','','','','','','',0,0,'US/Eastern','{}' FROM fixtures
 RETURNING id,relay_transaction_id
 ) INSERT INTO fuel_transaction_items(fuel_transaction_id,line_number,item_kind,category,total_amount_paid)
 SELECT i.id,0,f.kind,f.category,f.amount FROM inserted i JOIN fixtures f ON f.id=i.relay_transaction_id`, driver)
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO trucks(id,unit_number) VALUES ('00000000-0000-0000-0000-000000000010','TEST-10');
 INSERT INTO drivers(id,full_name,normalized_name,pay_type,pay_rate) VALUES ('00000000-0000-0000-0000-000000000020','Other Driver','other driver','cpm',0.7);`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO truck_driver_assignments(truck_id,driver_id,assigned_at,unassigned_at) VALUES
 ('00000000-0000-0000-0000-000000000010',$1,'2026-09-01 04:00+00','2026-10-01 04:00+00'),
 ('00000000-0000-0000-0000-000000000010','00000000-0000-0000-0000-000000000020','2026-09-30 04:00+00','2026-10-01 04:00+00'),
 ('00000000-0000-0000-0000-000000000010','00000000-0000-0000-0000-000000000020','2026-10-01 04:00+00',NULL);
 `, driver)
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO tolls(truck_id,posting_date,invoice_date,customer_id,source,read_type,transponder_or_plate,equipment_unit,agency,exit_plaza,exit_date,exit_time,toll_class,amount,row_fingerprint,prepass_environment)
 SELECT '00000000-0000-0000-0000-000000000010','2026-10-10','2026-10-10','','','','','TEST-10','','',day::date,'12:00','',amount,md5(label)||md5(label),env
 FROM (VALUES ('included','2026-09-28',15.50,'production'),('credit','2026-09-29',-3.25,NULL),('ambiguous','2026-09-30',999.00,'production'),('changed-driver','2026-10-02',999.00,'production'),('before','2026-09-27',999.00,'production'),('test','2026-09-29',999.00,'nonproduction')) v(label,day,amount,env)`)
	if err != nil {
		t.Fatal(err)
	}
}
