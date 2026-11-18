INSERT INTO investors(id,full_name) VALUES ('64000000-0000-0000-0000-000000000001','Report Investor');
INSERT INTO drivers(id,full_name,normalized_name,pay_type,pay_rate,hire_date) VALUES
 ('64000000-0000-0000-0000-000000000002','Hired Driver','hired driver','cpm',0.65,'2026-01-01');
INSERT INTO trucks(id,unit_number,owner_id) VALUES
 ('64000000-0000-0000-0000-000000000003','INV-RPT','64000000-0000-0000-0000-000000000001');
UPDATE truck_ownership_history SET start_known=false WHERE truck_id='64000000-0000-0000-0000-000000000003';
INSERT INTO truck_driver_assignments(truck_id,driver_id,assigned_at) VALUES
 ('64000000-0000-0000-0000-000000000003','64000000-0000-0000-0000-000000000002','2026-01-01');
INSERT INTO truck_settlement_terms(truck_id,owner_id,week_start,share_percent) VALUES
 ('64000000-0000-0000-0000-000000000003','64000000-0000-0000-0000-000000000001','2026-10-12',85);
INSERT INTO gross_board_entries(driver_id,service_date,load_number,driver_rate,entered_original_rate,entered_miles) VALUES
 ('64000000-0000-0000-0000-000000000002','2026-10-14','INV-MANUAL',1000,1200,300);
INSERT INTO driver_charge_types(id,name,direction,amount,amounts,eligibility) VALUES
 ('64000000-0000-0000-0000-000000000004','Report RC','charge',100,ARRAY[100]::numeric[],'loads');
INSERT INTO truck_charge_phases(truck_id,type_id,week_start,amount) VALUES
 ('64000000-0000-0000-0000-000000000003','64000000-0000-0000-0000-000000000004','2026-10-12',100);
INSERT INTO expenses(truck_id,category,category_id,expense_date,amount,expense_type,payment_type,covered_by)
 SELECT '64000000-0000-0000-0000-000000000003',name,id,'2026-10-14',30,'Repair','Card','Company' FROM expense_settings WHERE kind='category' AND name='Maintenance';
