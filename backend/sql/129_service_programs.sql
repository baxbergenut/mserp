BEGIN;

CREATE TABLE service_programs (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
 name TEXT NOT NULL CHECK(length(btrim(name)) BETWEEN 1 AND 100),
 instructions TEXT NOT NULL DEFAULT '' CHECK(length(instructions)<=5000),
 legacy_pm BOOLEAN NOT NULL DEFAULT false,
 interval_miles INTEGER CHECK(interval_miles BETWEEN 1 AND 2000000000),
 interval_days INTEGER CHECK(interval_days BETWEEN 1 AND 36500),
 interval_months INTEGER CHECK(interval_months BETWEEN 1 AND 1200),
 warning_miles INTEGER NOT NULL DEFAULT 1000 CHECK(warning_miles BETWEEN 0 AND 2000000000),
 warning_days INTEGER NOT NULL DEFAULT 14 CHECK(warning_days BETWEEN 0 AND 36500),
 active BOOLEAN NOT NULL DEFAULT true,
 use_default_assignees BOOLEAN NOT NULL DEFAULT true,
 assignee_ids UUID[] NOT NULL DEFAULT '{}',
 assignee_role_ids UUID[] NOT NULL DEFAULT '{}',
 version INTEGER NOT NULL DEFAULT 1,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 CHECK(interval_miles IS NOT NULL OR interval_days IS NOT NULL OR interval_months IS NOT NULL),
 CHECK(interval_days IS NULL OR interval_months IS NULL)
);
CREATE UNIQUE INDEX service_programs_pm_idx ON service_programs(legacy_pm) WHERE legacy_pm;
CREATE UNIQUE INDEX service_programs_name_idx ON service_programs(lower(btrim(name)));
INSERT INTO service_programs(id,name,legacy_pm,interval_miles,interval_days,warning_miles,warning_days,active)
SELECT '00000000-0000-4000-8000-000000000020','Oil change 20k',true,
 CASE WHEN s.id IS NULL THEN 20000 ELSE s.interval_miles END,s.interval_days,
 coalesce(s.warning_miles,1000),coalesce(s.warning_days,14),coalesce(s.enabled,true)
FROM (SELECT 1) seed LEFT JOIN maintenance_schedules s ON s.truck_id IS NULL AND s.service_code='pm';

CREATE TABLE service_program_trucks (
 program_id UUID NOT NULL REFERENCES service_programs(id) ON DELETE RESTRICT,
 truck_id UUID NOT NULL REFERENCES trucks(id) ON DELETE CASCADE,
 active BOOLEAN NOT NULL DEFAULT true,
 override_interval BOOLEAN NOT NULL DEFAULT false,
 interval_miles INTEGER CHECK(interval_miles BETWEEN 1 AND 2000000000),
 interval_days INTEGER CHECK(interval_days BETWEEN 1 AND 36500),
 interval_months INTEGER CHECK(interval_months BETWEEN 1 AND 1200),
 baseline_date DATE,
 baseline_odometer INTEGER CHECK(baseline_odometer BETWEEN 0 AND 2000000000),
 baseline_source_id UUID REFERENCES maintenance_records(id) ON DELETE RESTRICT,
 baseline_source_version INTEGER NOT NULL DEFAULT 0,
 baseline_cycle_id UUID NOT NULL DEFAULT gen_random_uuid(),
 version INTEGER NOT NULL DEFAULT 1,
 PRIMARY KEY(program_id,truck_id),
 CHECK(NOT override_interval OR interval_miles IS NOT NULL OR interval_days IS NOT NULL OR interval_months IS NOT NULL),
 CHECK(interval_days IS NULL OR interval_months IS NULL),
 CHECK(baseline_odometer IS NULL OR baseline_date IS NOT NULL)
);
INSERT INTO service_program_trucks(program_id,truck_id)
SELECT '00000000-0000-4000-8000-000000000020',id FROM trucks;

-- NULL retains the historical type-based PM meaning for older writers. Explicit
-- selections, including an empty array, control completion for new clients.
ALTER TABLE maintenance_records ADD COLUMN program_ids UUID[];
CREATE INDEX maintenance_records_programs_idx ON maintenance_records USING gin(program_ids);
ALTER TABLE maintenance_schedules ADD COLUMN interval_months INTEGER CHECK(interval_months BETWEEN 1 AND 1200);
ALTER TABLE system_task_assignments DROP CONSTRAINT system_task_assignments_kind_check;
ALTER TABLE system_task_assignments ADD CONSTRAINT system_task_assignments_kind_check CHECK(kind IN ('driver_onboarding','driver_offboarding','relay_review','escrow_release','relay_setup','weighmytruck_setup','datatruck_setup','five_eld_truck','maintenance_pm','maintenance_program'));
INSERT INTO system_task_assignments(kind) VALUES('maintenance_program');
ALTER TABLE system_task_records ADD COLUMN maintenance_program_id UUID REFERENCES service_programs(id) ON DELETE RESTRICT;
UPDATE system_task_records SET maintenance_program_id='00000000-0000-4000-8000-000000000020' WHERE kind='maintenance_pm';
DROP INDEX system_task_maintenance_open_idx;
CREATE UNIQUE INDEX system_task_maintenance_open_idx ON system_task_records(maintenance_truck_id,maintenance_program_id)
WHERE kind IN ('maintenance_pm','maintenance_program') AND completed_at IS NULL;

CREATE TRIGGER service_program_task_changed AFTER INSERT OR UPDATE OR DELETE ON service_programs FOR EACH ROW EXECUTE FUNCTION notify_task_change();
CREATE TRIGGER service_program_truck_task_changed AFTER INSERT OR UPDATE OR DELETE ON service_program_trucks FOR EACH ROW EXECUTE FUNCTION notify_task_change();
DO $$ DECLARE t TEXT; BEGIN
 IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
  FOREACH t IN ARRAY ARRAY['service_programs','service_program_trucks'] LOOP
   EXECUTE format('ALTER TABLE %I OWNER TO mserp_app',t);
  END LOOP;
 END IF;
END $$;
INSERT INTO access_audit(action,target_id,details)
SELECT 'service_program_migration',id,jsonb_build_object('program',to_jsonb(p),'enrolledTrucks',(SELECT count(*) FROM service_program_trucks)) FROM service_programs p WHERE legacy_pm;
COMMIT;
