-- Disposable synthetic workload: 120 trucks, 960 current loads and 12,000
-- historical loads/board links. Never run against an operational database.
INSERT INTO drivers(id,full_name,normalized_name,pay_type,pay_rate,hire_date)
SELECT md5('reports-driver-'||n)::uuid,'Review Driver '||lpad(n::text,3,'0'),
 'review driver '||lpad(n::text,3,'0'),'cpm',0.75,'2026-01-01' FROM generate_series(1,120)n;
INSERT INTO trucks(id,unit_number,owner_id)
SELECT md5('reports-truck-'||n)::uuid,'R'||lpad(n::text,3,'0'),(SELECT id FROM investors WHERE is_company) FROM generate_series(1,120)n;
UPDATE truck_ownership_history SET start_known=false WHERE truck_id IN (SELECT md5('reports-truck-'||n)::uuid FROM generate_series(1,120)n);
INSERT INTO truck_driver_assignments(truck_id,driver_id,assigned_at)
SELECT md5('reports-truck-'||n)::uuid,md5('reports-driver-'||n)::uuid,'2026-01-01' FROM generate_series(1,120)n;
DELETE FROM driver_dispatcher_assignments WHERE driver_id IN (SELECT md5('reports-driver-'||n)::uuid FROM generate_series(1,120)n);
INSERT INTO driver_dispatcher_assignments(driver_id,dispatcher_name,assigned_at)
SELECT md5('reports-driver-'||n)::uuid,'Review Dispatcher '||(n%5+1),'2026-01-01' FROM generate_series(1,120)n;
INSERT INTO loads(id,load_id,driver_id,truck_id,status,total_pay,load_pay,total_miles,pickup_time,delivery_time,pickup_appointment_time,delivery_appointment_time,raw_payload)
SELECT 700000+n*10+k,'RPT-'||n||'-'||k,md5('reports-driver-'||n)::uuid,md5('reports-truck-'||n)::uuid,
 'delivered',1800+k*50,1800+k*50,600,'2026-10-12 15:00Z'::timestamptz+(k%6)*interval '1 day',
 '2026-10-13 15:00Z'::timestamptz+(k%6)*interval '1 day','2026-10-12 16:00Z'::timestamptz+(k%6)*interval '1 day',
 '2026-10-13 16:00Z'::timestamptz+(k%6)*interval '1 day','{"trip":{"mile":"540","empty_mile":"60"}}'
FROM generate_series(1,120)n CROSS JOIN generate_series(1,8)k;
INSERT INTO gross_board_extra_entries(driver_id,service_date,slot,load_number,load_record_id,driver_rate,original_rate,miles)
SELECT driver_id,'2026-10-12',id%10,load_id,id,total_pay,total_pay,total_miles FROM loads WHERE id BETWEEN 700000 AND 710000;
INSERT INTO payroll_settlements(driver_id,week_start,report)
SELECT l.driver_id,'2026-10-12',jsonb_build_object('id',l.driver_id,'fullName',max(d.full_name),'payType','cpm',
 'loads',jsonb_agg(jsonb_build_object('loadRecordId',l.id,'loadNumber',l.load_id,'date','2026-10-12','fee','450.00','driverGross',l.total_pay::text)),
 'edits',jsonb_build_object('weekStart','2026-10-12'))
FROM loads l JOIN drivers d ON d.id=l.driver_id WHERE l.id BETWEEN 700000 AND 710000 GROUP BY l.driver_id;
INSERT INTO loads(id,load_id,driver_id,status,total_pay,load_pay,total_miles,pickup_time,delivery_time,raw_payload)
SELECT 800000+n,'OLD-RPT-'||n,md5('reports-driver-'||(n%120+1))::uuid,'delivered',2000,2000,600,
 '2025-01-01 15:00Z'::timestamptz+(n%300)*interval '1 day','2025-01-02 15:00Z'::timestamptz+(n%300)*interval '1 day','{}'
FROM generate_series(1,12000)n;
INSERT INTO gross_board_extra_entries(driver_id,service_date,slot,load_number,load_record_id,driver_rate,original_rate,miles)
SELECT driver_id,'2025-01-01'::date+(id-800001)/960,((id-800001)/120)%8+1,load_id,id,total_pay,total_pay,total_miles FROM loads WHERE id BETWEEN 800001 AND 812000;
ANALYZE loads;
ANALYZE gross_board_extra_entries;
ANALYZE payroll_settlements;
