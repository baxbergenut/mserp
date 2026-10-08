// Synthetic records shared by the isolated preview and profile browser checks.
export const profileFixture = {
  owner: '63000000-0000-0000-0000-000000000010',
  hired: '63000000-0000-0000-0000-000000000011',
  dispatcher: '63000000-0000-0000-0000-000000000012',
  investor: '63000000-0000-0000-0000-000000000013',
  ownTruck: '63000000-0000-0000-0000-000000000014',
  extraTruck: '63000000-0000-0000-0000-000000000015',
  independent: '63000000-0000-0000-0000-000000000016',
};

export function seedProfileFixture(sql, schema) {
  const f = profileFixture;
  sql(`SET search_path TO ${schema},public;
    INSERT INTO dispatchers(id,full_name,normalized_name,phone,email,extension,pay_percentage) VALUES('${f.dispatcher}','Dana Mitchell','dana mitchell','2025550101','dana@example.test',201,2);
    INSERT INTO drivers(id,full_name,normalized_name,is_owner_operator,pay_type,pay_rate,dispatcher_id,phone,email,driver_home,notes,hire_date) VALUES
      ('${f.owner}','Morgan Hayes','morgan hayes',true,'gross_percentage',88,'${f.dispatcher}','2025550102','morgan@example.test','Dallas, TX','Legacy profile context retained without an invented date.','2026-09-28'),
      ('${f.hired}','Jamie Carter','jamie carter',false,'cpm',0.5,'${f.dispatcher}','2025550103','jamie@example.test','Columbus, OH',NULL,'2026-09-28');
    INSERT INTO investors(id,full_name,driver_id) VALUES('${f.investor}','Morgan Hayes','${f.owner}');
    INSERT INTO investors(id,full_name,email) VALUES('${f.independent}','River Fleet Partners','office@example.test');
    INSERT INTO trucks(id,unit_number,owner_id,year,make,model,vin) VALUES
      ('${f.ownTruck}','101','${f.investor}',2022,'Freightliner','Cascadia','1FUJGLDR0NL000101'),
      ('${f.extraTruck}','102','${f.investor}',2023,'Volvo','VNL','4V4NC9EH0PN000102');
    INSERT INTO trucks(unit_number,owner_id,year,make,model) VALUES('103','${f.independent}',2024,'Kenworth','T680');
    INSERT INTO truck_driver_assignments(truck_id,driver_id,assigned_at,unassigned_at,source) VALUES('${f.extraTruck}','${f.owner}','2026-09-07','2026-09-21','Initial sample assignment');
    INSERT INTO truck_driver_assignments(truck_id,driver_id,assigned_at,source) VALUES
      ('${f.ownTruck}','${f.owner}','2026-09-21','Sample fleet setup'),('${f.extraTruck}','${f.hired}','2026-09-21','Sample fleet setup');
    INSERT INTO truck_settlement_terms(truck_id,owner_id,week_start,share_percent) VALUES('${f.extraTruck}','${f.investor}','2026-09-28',88);
    INSERT INTO loads(id,load_id,status,load_pay,total_pay,total_miles,truck_unit,pickup_time,raw_payload) VALUES
      (63001,'REVIEW-101','delivered',3000,3000,800,'101','2026-09-28 00:01+00','{"trip":{"mile":750,"empty_mile":50}}'),
      (63002,'REVIEW-102','delivered',12000,12000,1000,'102','2026-09-28 00:01+00','{"trip":{"mile":900,"empty_mile":100}}'),
      (63003,'REVIEW-103','delivered',2500,2500,700,'101','2026-10-05 00:01+00','{"trip":{"mile":650,"empty_mile":50}}'),
      (63004,'REVIEW-104','delivered',6500,6500,900,'102','2026-10-05 00:01+00','{"trip":{"mile":850,"empty_mile":50}}');
    UPDATE loads SET raw_payload=raw_payload || '{"stops":[{"stop_type":"pickup","ordering":1,"location":{"city":"Dallas","state":"TX"}},{"stop_type":"delivery","ordering":2,"location":{"city":"Columbus","state":"OH"}}]}'::jsonb WHERE id BETWEEN 63001 AND 63004;
    INSERT INTO gross_board_entries(driver_id,service_date,load_number,load_record_id,driver_rate) VALUES
      ('${f.owner}','2026-09-28','REVIEW-101',63001,2800),('${f.hired}','2026-09-28','REVIEW-102',63002,10000),
      ('${f.owner}','2026-10-05','REVIEW-103',63003,2700),('${f.hired}','2026-10-05','REVIEW-104',63004,6000);
  `);
}
