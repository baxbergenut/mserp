
  INSERT INTO drivers(id,full_name,normalized_name,pay_type,pay_rate,hire_date) VALUES
  ('61000000-0000-0000-0000-000000000001','Company Driver','company driver','cpm',0.75,'2026-01-01'),
  ('61000000-0000-0000-0000-000000000002','Owner Driver','owner driver','gross_percentage',80,'2026-01-01');
  INSERT INTO investors(id,full_name,driver_id) VALUES
  ('61000000-0000-0000-0000-000000000003','Owner Driver','61000000-0000-0000-0000-000000000002');
  INSERT INTO trucks(id,unit_number,owner_id) VALUES
  ('61000000-0000-0000-0000-000000000004','COMPANY-RPT',(SELECT id FROM investors WHERE is_company)),
  ('61000000-0000-0000-0000-000000000005','OWNER-RPT','61000000-0000-0000-0000-000000000003');
  UPDATE truck_ownership_history SET start_known=false WHERE truck_id IN ('61000000-0000-0000-0000-000000000004','61000000-0000-0000-0000-000000000005');
  INSERT INTO truck_driver_assignments(truck_id,driver_id,assigned_at) VALUES
  ('61000000-0000-0000-0000-000000000004','61000000-0000-0000-0000-000000000001','2026-01-01'),
  ('61000000-0000-0000-0000-000000000005','61000000-0000-0000-0000-000000000002','2026-01-01');
  DELETE FROM driver_dispatcher_assignments WHERE driver_id='61000000-0000-0000-0000-000000000001';
 INSERT INTO driver_dispatcher_assignments(driver_id,dispatcher_name,assigned_at) VALUES
  ('61000000-0000-0000-0000-000000000001','Former Dispatcher','2026-01-01');
  INSERT INTO loads(id,load_id,driver_id,status,load_pay,total_pay,total_miles,pickup_time,delivery_time,pickup_appointment_time,delivery_appointment_time,raw_payload) VALUES
  (61001,'REPORT-1','61000000-0000-0000-0000-000000000001','delivered',1000,1100,400,'2026-10-12 15:00Z','2026-10-13 15:00Z','2026-10-12 16:00Z','2026-10-13 14:00Z','{"trip":{"mile":"360","empty_mile":"40"}}'),
  (61002,'REPORT-2','61000000-0000-0000-0000-000000000002','completed',1700,1800,600,'2026-10-17 15:00Z','2026-10-18 15:00Z',NULL,NULL,'{}');
  INSERT INTO gross_board_entries(driver_id,service_date,load_number,load_record_id,driver_rate,entered_original_rate,entered_miles) VALUES
  ('61000000-0000-0000-0000-000000000001','2026-10-12','REPORT-1',61001,1000,999,300),
  ('61000000-0000-0000-0000-000000000002','2026-10-18','REPORT-2',61002,1700,1999,500),
  ('61000000-0000-0000-0000-000000000001','2026-10-14','MANUAL',NULL,100,200,100);
  INSERT INTO gross_board_extra_entries(driver_id,service_date,slot,load_number,load_record_id,driver_rate,original_rate,miles)
  VALUES('61000000-0000-0000-0000-000000000001','2026-10-12',1,'REPORT-1',61001,1000,1300,500);
  INSERT INTO gross_board_transfers(driver_id,source_date,source_slot,dispatch_date,dispatch_slot,settlement_date,dispatcher_id,dispatcher_name,truck_id)
  VALUES('61000000-0000-0000-0000-000000000002','2026-10-18',0,'2026-10-19',0,'2026-10-18','','Original Dispatcher','61000000-0000-0000-0000-000000000005');
  INSERT INTO expenses(truck_id,category,category_id,expense_date,amount,expense_type,payment_type,covered_by)
  SELECT '61000000-0000-0000-0000-000000000004'::uuid,name,id,'2026-10-13'::date,80,'Repair','Card','Company' FROM expense_settings WHERE kind='category' AND name='Maintenance'
  UNION ALL SELECT '61000000-0000-0000-0000-000000000005'::uuid,name,id,'2026-10-13'::date,50,'Truck Lease','Card','Company' FROM expense_settings WHERE kind='category' AND name='Other';
  INSERT INTO payroll_settlements(driver_id,week_start,report) VALUES
  ('61000000-0000-0000-0000-000000000001','2026-10-12','{"id":"61000000-0000-0000-0000-000000000001","fullName":"Company Driver","payType":"cpm","loads":[{"loadRecordId":61001,"loadNumber":"REPORT-1","date":"2026-10-12","fee":"300.00"}],"edits":{"weekStart":"2026-10-12"}}'),
  ('61000000-0000-0000-0000-000000000002','2026-10-12','{"id":"61000000-0000-0000-0000-000000000002","fullName":"Owner Driver","isOwnerOperator":true,"payType":"gross_percentage","loads":[{"loadRecordId":61002,"loadNumber":"REPORT-2","date":"2026-10-18","fee":"1360.00","driverGross":"1700.00"}],"edits":{"weekStart":"2026-10-12","expenseDeductions":[{"source":"reimburse","amount":"25.00"},{"source":"escrow","amount":"20.00"},{"source":"expense","amount":"50.00","escrowAmount":"10.00"}]},"autoCharges":[{"source":"escrow-release:receipt","amount":"5.00"}]}');

INSERT INTO fuel_transactions(id,relay_environment,relay_transaction_id,driver_id,relay_driver_id,purchased_at,total_amount_paid,total_retail_price,total_amount_saved,is_direct_bill,currency_code,merchant_id,merchant_name,merchant_number,location_id,location_name,merchant_location_id,address,city,state,postal_code,latitude,longitude,timezone,prompts,raw_payload)
VALUES('61000000-0000-0000-0000-000000000006','production','REPORT-FUEL','61000000-0000-0000-0000-000000000001','review','2026-10-13 16:00Z',200,200,0,false,'USD','','Review fuel stop','','','','','','','','',0,0,'America/New_York','[{"label":"Truck #","value":"COMPANY-RPT"}]','{}');
INSERT INTO fuel_transaction_items(fuel_transaction_id,line_number,item_kind,category,quantity,total_amount_paid)
VALUES('61000000-0000-0000-0000-000000000006',1,'fuel','Diesel',50,200);
INSERT INTO tolls(truck_id,posting_date,invoice_date,customer_id,source,read_type,transponder_or_plate,equipment_unit,agency,exit_plaza,exit_date,exit_time,toll_class,amount,row_fingerprint)
VALUES('61000000-0000-0000-0000-000000000004','2026-10-13','2026-10-13','','review','','','COMPANY-RPT','','','2026-10-13','12:00','',40,repeat('a',64));
INSERT INTO weighmytruck_transactions(site_number,ticket_number,transaction_date,amount,driver_id,truck_id,raw_payload)
VALUES('review','REPORT-SCALE','2026-10-13',14,'61000000-0000-0000-0000-000000000001','61000000-0000-0000-0000-000000000004','{}');
INSERT INTO expenses(truck_id,category,category_id,expense_date,amount,expense_type,payment_type,covered_by)
SELECT '61000000-0000-0000-0000-000000000004'::uuid,name,id,'2026-10-13'::date,60,'Equipment','Card','Company' FROM expense_settings WHERE kind='category' AND name='Other'
UNION ALL SELECT '61000000-0000-0000-0000-000000000004'::uuid,name,id,'2026-10-13'::date,900,'Truck Payment','Card','Company' FROM expense_settings WHERE kind='category' AND name='Other'
UNION ALL SELECT NULL::uuid,name,id,'2026-10-13'::date,30,'Repair','Card','Company' FROM expense_settings WHERE kind='category' AND name='Maintenance';
