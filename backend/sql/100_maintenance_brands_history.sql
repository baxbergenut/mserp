BEGIN;
ALTER TABLE maintenance_vendors ADD COLUMN superseded_by uuid REFERENCES maintenance_vendors(id);
-- Consolidate only untouched seeded chain names; custom edits remain authoritative.
UPDATE maintenance_vendors SET name=CASE name WHEN 'Love''s Truck Care' THEN 'Love''s' WHEN 'TA Truck Service' THEN 'TA' WHEN 'Petro Truck Service' THEN 'Petro' WHEN 'Blue Beacon Truck Wash' THEN 'Blue Beacon' END,version=version+1
WHERE version=1 AND name IN ('Love''s Truck Care','TA Truck Service','Petro Truck Service','Blue Beacon Truck Wash')
AND NOT EXISTS(SELECT 1 FROM maintenance_vendors other WHERE lower(other.name)=lower(CASE maintenance_vendors.name WHEN 'Love''s Truck Care' THEN 'Love''s' WHEN 'TA Truck Service' THEN 'TA' WHEN 'Petro Truck Service' THEN 'Petro' WHEN 'Blue Beacon Truck Wash' THEN 'Blue Beacon' END));
INSERT INTO maintenance_vendors(name,source_url) VALUES
('Love''s','https://www.loves.com/'),
('Speedco','https://www.loves.com/'),
('TA','https://www.ta-petro.com/'),
('Petro','https://www.ta-petro.com/'),
('Goodyear Commercial Tire & Service Centers','https://www.goodyearctsc.com/'),
('Blue Beacon','https://bluebeacon.com/'),
('Boss Truck Shops','https://bosstruckshops.com/'),
('Southern Tire Mart','https://stmtires.com/'),
('Southern Tire Mart at Pilot','https://pilotcompany.com/truck-care'),
('Rush Truck Centers','https://www.rushtruckcenters.com/service/our-services'),
('MHC Kenworth','https://www.mhc.com/'),
('Velocity Truck Centers','https://www.velocitytruckcenters.com/locations')
ON CONFLICT DO NOTHING;
-- Branch identities and their original source details remain available as history.
UPDATE maintenance_vendors v SET active=false,superseded_by=b.id,version=v.version+1 FROM maintenance_vendors b
WHERE v.version=1 AND v.source_url<>'' AND v.name LIKE '% — %' AND b.name=CASE
 WHEN v.source_url LIKE 'https://www.loves.com/%' AND v.name LIKE 'Speedco%' THEN 'Speedco'
 WHEN v.source_url LIKE 'https://www.loves.com/%' THEN 'Love''s'
 WHEN v.source_url LIKE 'https://www.ta-petro.com/%' AND v.name LIKE 'Petro%' THEN 'Petro'
 WHEN v.source_url LIKE 'https://www.ta-petro.com/%' THEN 'TA'
 WHEN v.source_url LIKE 'https://bluebeacon.com/%' THEN 'Blue Beacon' END;

-- Customer-confirmed historical imports: the sheet's saved payment date is the
-- service date and these work orders were completed, even if a truck is unlinked.
ALTER TABLE maintenance_records ADD COLUMN legacy_import boolean NOT NULL DEFAULT false;
ALTER TABLE maintenance_records DROP CONSTRAINT maintenance_records_check;
ALTER TABLE maintenance_records ADD CONSTRAINT maintenance_records_completed_check
 CHECK(status<>'completed' OR (service_date IS NOT NULL AND (truck_id IS NOT NULL OR legacy_import)));
WITH before AS MATERIALIZED (
 SELECT m.id,to_jsonb(m)||jsonb_build_object('serviceDate',m.service_date,'vendorName',m.vendor_name,'oilType',m.oil_type,'truckId',m.truck_id) value,e.expense_date,e.unit_number FROM maintenance_records m JOIN expenses e ON e.id=m.expense_id
 WHERE e.source_spreadsheet_id IS NOT NULL AND e.source_sheet='Maintenance_and_other_expenses'
 AND m.status='needs_review' AND m.service_date IS NULL AND m.version=1 AND e.expense_date IS NOT NULL
), changed AS (
 UPDATE maintenance_records m SET service_date=b.expense_date,status='completed',legacy_import=true,
 source_unit_number=coalesce(nullif(m.source_unit_number,''),b.unit_number,''),version=m.version+1,updated_at=now(),updated_by_name='Historical import correction',
 services=(SELECT jsonb_agg(CASE WHEN lower(btrim(s->>'name'))='pm service' THEN s||'{"typeCode":"preventive","trackingCode":"pm"}'::jsonb WHEN lower(btrim(s->>'name'))='oil change' THEN s||'{"typeCode":"oil","trackingCode":"oil"}'::jsonb ELSE s END ORDER BY ord) FROM jsonb_array_elements(m.services) WITH ORDINALITY a(s,ord))
 FROM before b WHERE m.id=b.id RETURNING m.*
) INSERT INTO maintenance_changes(record_id,actor_name,before_value,after_value)
 SELECT c.id,'Historical import correction',b.value,to_jsonb(c)||jsonb_build_object('serviceDate',c.service_date,'vendorName',c.vendor_name,'oilType',c.oil_type,'truckId',c.truck_id) FROM changed c JOIN before b ON b.id=c.id;
UPDATE maintenance_records m SET vendor_id=v.superseded_by,version=m.version+1,updated_at=now()
FROM maintenance_vendors v WHERE m.vendor_id=v.id AND v.superseded_by IS NOT NULL;
COMMIT;
