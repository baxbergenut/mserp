BEGIN;

CREATE TABLE maintenance_configuration (
    singleton BOOLEAN PRIMARY KEY DEFAULT true CHECK (singleton),
    category_id UUID NOT NULL REFERENCES expense_settings(id) ON DELETE RESTRICT
);
INSERT INTO maintenance_configuration(category_id)
SELECT c.id FROM expense_settings c WHERE c.kind='category' AND (lower(c.name)='maintenance' OR EXISTS(SELECT 1 FROM expenses e WHERE e.category_id=c.id AND lower(e.category)='maintenance')) ORDER BY (lower(c.name)='maintenance') DESC,c.active DESC,c.id LIMIT 1;

CREATE TABLE maintenance_types (
    code TEXT PRIMARY KEY CHECK (code ~ '^[a-z][a-z0-9_]{0,39}$'),
    name TEXT NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 100),
    active BOOLEAN NOT NULL DEFAULT true,
    version INTEGER NOT NULL DEFAULT 1
);
CREATE UNIQUE INDEX maintenance_type_name ON maintenance_types(lower(name));
INSERT INTO maintenance_types(code,name) VALUES
 ('preventive','Preventive maintenance'),('engine','Engine & drivetrain'),
 ('electrical','Electrical'),('brakes','Brakes'),('tires','Tires & wheels'),
 ('fluids','Cooling & fluid systems'),('body','Body & chassis'),
 ('recovery','Towing & recovery'),('cleaning','Cleaning & detailing'),('other','Other maintenance');

CREATE TABLE maintenance_vendors (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 200),
    address TEXT NOT NULL DEFAULT '', phone TEXT NOT NULL DEFAULT '', contact TEXT NOT NULL DEFAULT '',
    active BOOLEAN NOT NULL DEFAULT true, version INTEGER NOT NULL DEFAULT 1
);
CREATE UNIQUE INDEX maintenance_vendor_name ON maintenance_vendors(lower(btrim(name)));

CREATE TABLE maintenance_records (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    expense_id UUID UNIQUE REFERENCES expenses(id) ON DELETE SET NULL,
    truck_id UUID REFERENCES trucks(id) ON DELETE RESTRICT,
    vendor_id UUID REFERENCES maintenance_vendors(id) ON DELETE RESTRICT,
    vendor_name TEXT NOT NULL DEFAULT '', vendor_address TEXT NOT NULL DEFAULT '',
    vendor_phone TEXT NOT NULL DEFAULT '', vendor_contact TEXT NOT NULL DEFAULT '',
    service_date DATE, odometer INTEGER CHECK (odometer >= 0),
    location TEXT NOT NULL DEFAULT 'shop' CHECK (location IN ('shop','roadside','internal','unknown')),
    status TEXT NOT NULL DEFAULT 'needs_review' CHECK (status IN ('completed','needs_review','voided')),
    work_order TEXT NOT NULL DEFAULT '', notes TEXT NOT NULL DEFAULT '',
    services JSONB NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(services)='array'),
    version INTEGER NOT NULL DEFAULT 1,
    created_by UUID REFERENCES app_users(id) ON DELETE SET NULL,
    created_by_name TEXT NOT NULL DEFAULT '', updated_by_name TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(), updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    void_reason TEXT NOT NULL DEFAULT '', voided_expense JSONB,
    CHECK (status<>'completed' OR (truck_id IS NOT NULL AND service_date IS NOT NULL))
);
CREATE INDEX maintenance_truck_history ON maintenance_records(truck_id,service_date DESC,id);
CREATE INDEX maintenance_record_services ON maintenance_records USING gin(services);

CREATE TABLE maintenance_changes (
    id BIGSERIAL PRIMARY KEY, record_id UUID NOT NULL REFERENCES maintenance_records(id) ON DELETE RESTRICT,
    actor_name TEXT NOT NULL, happened_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    before_value JSONB, after_value JSONB NOT NULL
);
CREATE TABLE maintenance_documents (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(), record_id UUID NOT NULL REFERENCES maintenance_records(id) ON DELETE RESTRICT,
    file_name TEXT NOT NULL, content_type TEXT NOT NULL, size_bytes BIGINT NOT NULL CHECK(size_bytes BETWEEN 1 AND 20971520),
    sha256 TEXT NOT NULL, data BYTEA NOT NULL, uploaded_by TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX maintenance_documents_record ON maintenance_documents(record_id);
CREATE UNIQUE INDEX maintenance_document_retry ON maintenance_documents(record_id,sha256,file_name);

CREATE TABLE maintenance_odometer_readings (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(), truck_id UUID NOT NULL REFERENCES trucks(id) ON DELETE RESTRICT,
    record_id UUID UNIQUE REFERENCES maintenance_records(id) ON DELETE RESTRICT,
    reading_date DATE NOT NULL, mileage INTEGER NOT NULL CHECK(mileage>=0),
    source TEXT NOT NULL CHECK(source IN ('manual','service')), active BOOLEAN NOT NULL DEFAULT true,
    version INTEGER NOT NULL DEFAULT 1, actor_name TEXT NOT NULL, updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX maintenance_odometer_truck ON maintenance_odometer_readings(truck_id,reading_date DESC) WHERE active;
CREATE TABLE maintenance_reading_changes (
 id BIGSERIAL PRIMARY KEY, reading_id UUID NOT NULL REFERENCES maintenance_odometer_readings(id), actor_name TEXT NOT NULL,
 happened_at TIMESTAMPTZ NOT NULL DEFAULT now(),before_value JSONB,after_value JSONB NOT NULL
);
CREATE TABLE maintenance_schedules (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(), truck_id UUID REFERENCES trucks(id) ON DELETE RESTRICT,
    service_code TEXT NOT NULL CHECK(service_code IN ('pm','oil')),
    interval_miles INTEGER CHECK(interval_miles>0), interval_days INTEGER CHECK(interval_days>0),
    warning_miles INTEGER NOT NULL DEFAULT 1000 CHECK(warning_miles>=0),
    warning_days INTEGER NOT NULL DEFAULT 14 CHECK(warning_days>=0),
    enabled BOOLEAN NOT NULL DEFAULT true, version INTEGER NOT NULL DEFAULT 1,
    CHECK(interval_miles IS NOT NULL OR interval_days IS NOT NULL OR NOT enabled)
);
CREATE UNIQUE INDEX maintenance_schedule_scope ON maintenance_schedules(coalesce(truck_id,'00000000-0000-0000-0000-000000000000'::uuid),service_code);

CREATE FUNCTION maintenance_name_type(label TEXT) RETURNS TEXT LANGUAGE sql IMMUTABLE AS $$
 SELECT CASE lower(btrim(label))
 WHEN 'pm service' THEN 'preventive' WHEN 'oil change' THEN 'preventive'
 WHEN 'electrical issue' THEN 'electrical' WHEN 'electrical repair' THEN 'electrical'
 WHEN 'brake issue' THEN 'brakes' WHEN 'tire issue' THEN 'tires' WHEN 'tire replacement' THEN 'tires'
 WHEN 'leakage' THEN 'fluids' WHEN 'body work' THEN 'body' WHEN 'towing' THEN 'recovery'
 WHEN 'truck wash' THEN 'cleaning' WHEN 'detailing' THEN 'cleaning' ELSE 'other' END
$$;
CREATE FUNCTION maintenance_legacy_services(label TEXT, costs JSONB) RETURNS JSONB LANGUAGE sql IMMUTABLE AS $$
 SELECT CASE WHEN jsonb_array_length(costs)>0 THEN
 (SELECT jsonb_agg(jsonb_build_object('typeCode',maintenance_name_type(row->>'name'),'name',row->>'name','amount',row->>'amount','trackingCode','')) FROM jsonb_array_elements(costs) row)
 ELSE jsonb_build_array(jsonb_build_object('typeCode',maintenance_name_type(label),'name',coalesce(nullif(label,''),'Historical maintenance'),'amount',NULL,'trackingCode','')) END
$$;
INSERT INTO maintenance_records(expense_id,truck_id,location,services,notes,created_by_name,created_at,created_by)
SELECT e.id,e.truck_id,'unknown',maintenance_legacy_services(e.expense_type,e.cost_breakdown),coalesce(e.description,''),coalesce(e.created_by_name,''),e.created_at,e.created_by
FROM expenses e JOIN maintenance_configuration c ON e.category_id=c.category_id;

-- Compatible clients still produce a review record; an invoice alone is never proof of completed work.
CREATE FUNCTION maintenance_capture_expense() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF EXISTS(SELECT 1 FROM maintenance_configuration WHERE category_id=NEW.category_id) THEN
  INSERT INTO maintenance_records(expense_id,truck_id,location,services,notes,created_by_name,created_by)
  VALUES(NEW.id,NEW.truck_id,'unknown',maintenance_legacy_services(NEW.expense_type,NEW.cost_breakdown),coalesce(NEW.description,''),coalesce(NEW.created_by_name,''),NEW.created_by);
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER maintenance_expense_capture AFTER INSERT ON expenses FOR EACH ROW EXECUTE FUNCTION maintenance_capture_expense();

-- Validate linked work costs at commit, after atomic service + ledger updates.
CREATE FUNCTION maintenance_validate_record(record_id UUID) RETURNS VOID LANGUAGE plpgsql AS $$
DECLARE r maintenance_records; e expenses; item JSONB; total NUMERIC:=0; priced INTEGER:=0; n INTEGER:=0;
BEGIN
 SELECT * INTO r FROM maintenance_records WHERE id=record_id;
 IF NOT FOUND OR r.status='voided' THEN RETURN; END IF;
 IF r.expense_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM expenses ledger JOIN maintenance_configuration c ON c.category_id=ledger.category_id WHERE ledger.id=r.expense_id) THEN
  RAISE EXCEPTION 'Maintenance invoices retain their classification' USING ERRCODE='23514',CONSTRAINT='maintenance_work_total';
 END IF;
 IF r.status='needs_review' THEN RETURN; END IF;
 IF jsonb_array_length(r.services) NOT BETWEEN 1 AND 100 THEN
  RAISE EXCEPTION 'Add completed work items' USING ERRCODE='23514',CONSTRAINT='maintenance_work_total';
 END IF;
 FOR item IN SELECT value FROM jsonb_array_elements(r.services) LOOP
  n:=n+1;
  IF jsonb_typeof(item) IS DISTINCT FROM 'object' OR jsonb_typeof(item->'name') IS DISTINCT FROM 'string'
   OR length(btrim(item->>'name')) NOT BETWEEN 1 AND 100
   OR NOT EXISTS(SELECT 1 FROM maintenance_types WHERE code=item->>'typeCode')
   OR coalesce(item->>'trackingCode','') NOT IN ('','pm','oil')
   OR (coalesce(item->>'trackingCode','')<>'' AND item->>'typeCode'<>'preventive') THEN
   RAISE EXCEPTION 'Invalid maintenance work' USING ERRCODE='23514',CONSTRAINT='maintenance_work_total';
  END IF;
  IF item->>'amount' IS NOT NULL THEN
   IF (item->>'amount') !~ '^-?[0-9]{1,12}(\.[0-9]{1,2})?$' THEN
    RAISE EXCEPTION 'Invalid maintenance cost' USING ERRCODE='23514',CONSTRAINT='maintenance_work_total';
   END IF;
   priced:=priced+1; total:=total+(item->>'amount')::numeric;
  END IF;
 END LOOP;
 IF priced<>0 AND priced<>n THEN
  RAISE EXCEPTION 'Price all work items or leave all unallocated' USING ERRCODE='23514',CONSTRAINT='maintenance_work_total';
 END IF;
 IF r.expense_id IS NOT NULL THEN
  SELECT * INTO e FROM expenses WHERE id=r.expense_id;
  IF e.truck_id IS DISTINCT FROM r.truck_id OR NOT EXISTS(SELECT 1 FROM maintenance_configuration WHERE category_id=e.category_id)
   OR (priced>0 AND total<>coalesce(e.base_amount,e.amount)) THEN
   RAISE EXCEPTION 'Maintenance work must match its financial record' USING ERRCODE='23514',CONSTRAINT='maintenance_work_total';
  END IF;
  IF (priced=0 AND jsonb_array_length(e.cost_breakdown)<>0) OR (priced>0 AND
 (jsonb_array_length(e.cost_breakdown)<>n OR EXISTS(SELECT 1 FROM jsonb_array_elements(r.services) WITH ORDINALITY s(value,ord)
 WHERE s.value->>'name' IS DISTINCT FROM e.cost_breakdown->(s.ord::integer-1)->>'name'
 OR (s.value->>'amount')::numeric IS DISTINCT FROM (e.cost_breakdown->(s.ord::integer-1)->>'amount')::numeric))) THEN
   RAISE EXCEPTION 'Update itemized service costs in Maintenance' USING ERRCODE='23514',CONSTRAINT='maintenance_work_total';
  END IF;
 ELSIF priced>0 AND total<>0 THEN
  RAISE EXCEPTION 'Paid work needs a financial record' USING ERRCODE='23514',CONSTRAINT='maintenance_work_total';
 END IF;
END $$;
CREATE FUNCTION maintenance_record_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN PERFORM maintenance_validate_record(NEW.id); RETURN NEW; END $$;
CREATE CONSTRAINT TRIGGER maintenance_record_cost_guard AFTER INSERT OR UPDATE ON maintenance_records
 DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION maintenance_record_guard();
CREATE FUNCTION maintenance_expense_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE record_id UUID;
BEGIN
 FOR record_id IN SELECT id FROM maintenance_records WHERE expense_id=NEW.id LOOP
  PERFORM maintenance_validate_record(record_id);
 END LOOP;
 RETURN NEW;
END $$;
CREATE CONSTRAINT TRIGGER maintenance_expense_cost_guard AFTER UPDATE ON expenses
 DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION maintenance_expense_guard();

CREATE FUNCTION maintenance_legacy_update() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.truck_id IS DISTINCT FROM OLD.truck_id OR NEW.cost_breakdown IS DISTINCT FROM OLD.cost_breakdown OR NEW.expense_type IS DISTINCT FROM OLD.expense_type THEN
  UPDATE maintenance_records SET truck_id=NEW.truck_id,services=maintenance_legacy_services(NEW.expense_type,NEW.cost_breakdown),version=version+1,updated_at=now() WHERE expense_id=NEW.id AND status='needs_review';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER maintenance_legacy_update AFTER UPDATE ON expenses FOR EACH ROW EXECUTE FUNCTION maintenance_legacy_update();
CREATE FUNCTION maintenance_protect_ledger_delete() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF EXISTS(SELECT 1 FROM maintenance_records WHERE expense_id=OLD.id) THEN
  RAISE EXCEPTION 'Void the maintenance visit to remove its unpaid invoice' USING ERRCODE='23514',CONSTRAINT='maintenance_work_total';
 END IF;
 RETURN OLD;
END $$;
CREATE TRIGGER maintenance_ledger_delete_guard BEFORE DELETE ON expenses FOR EACH ROW EXECUTE FUNCTION maintenance_protect_ledger_delete();

DO $$ DECLARE t TEXT; BEGIN
 IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
  FOREACH t IN ARRAY ARRAY['maintenance_configuration','maintenance_types','maintenance_vendors','maintenance_records','maintenance_changes','maintenance_documents','maintenance_odometer_readings','maintenance_schedules','maintenance_reading_changes'] LOOP
   EXECUTE format('ALTER TABLE %I OWNER TO mserp_app',t);
  END LOOP;
 END IF;
END $$;
COMMIT;
