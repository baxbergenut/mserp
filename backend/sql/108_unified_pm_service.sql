BEGIN;

-- Keep the former configuration for review, including any conflicting intervals.
INSERT INTO access_audit(action,target_id,details)
SELECT 'maintenance_pm_consolidation',id,jsonb_build_object('schedule',to_jsonb(s))
FROM maintenance_schedules s;
INSERT INTO access_audit(action,target_id,details)
SELECT 'maintenance_pm_consolidation',id,jsonb_build_object('assignment',to_jsonb(a))
FROM system_task_assignments a WHERE kind IN ('maintenance_pm','maintenance_oil');

-- Existing PM configuration wins at each scope. An oil-only override becomes PM.
DELETE FROM maintenance_schedules oil USING maintenance_schedules pm
WHERE oil.service_code='oil' AND pm.service_code='pm'
AND oil.truck_id IS NOT DISTINCT FROM pm.truck_id;
UPDATE maintenance_schedules SET service_code='pm',version=version+1 WHERE service_code='oil';
ALTER TABLE maintenance_schedules DROP CONSTRAINT maintenance_schedules_service_code_check;
ALTER TABLE maintenance_schedules ADD CONSTRAINT maintenance_schedules_service_code_check CHECK(service_code='pm');

-- Retain the legacy type for immutable records and ledger validation. Application
-- reads present this alias as PM Service; new writes use preventive/pm only.
UPDATE maintenance_types SET name='PM Service',version=version+1 WHERE code='preventive';
UPDATE maintenance_types SET active=false,version=version+1 WHERE code='oil';

-- Every former assignee continues receiving the single reminder category.
WITH merged AS (
 SELECT ARRAY(SELECT DISTINCT person FROM system_task_assignments a,
 unnest(a.assignee_ids || CASE WHEN a.assignee_id IS NULL THEN '{}'::uuid[] ELSE ARRAY[a.assignee_id] END) person
 WHERE a.kind IN ('maintenance_pm','maintenance_oil') ORDER BY person) people,
 ARRAY(SELECT DISTINCT role_id FROM system_task_assignments a,unnest(a.assignee_role_ids) role_id
 WHERE a.kind IN ('maintenance_pm','maintenance_oil') ORDER BY role_id) roles
)
UPDATE system_task_assignments SET assignee_ids=merged.people,
 assignee_id=coalesce(assignee_id,merged.people[1]),assignee_role_ids=merged.roles,
 version=version+1,updated_at=now() FROM merged WHERE kind='maintenance_pm';

-- Preserve one open reminder with the strongest observed mileage evidence.
WITH ranked AS (
 SELECT id,row_number() OVER(PARTITION BY maintenance_truck_id ORDER BY maintenance_observed_miles DESC NULLS LAST,created_at,id) position
 FROM system_task_records WHERE kind IN ('maintenance_pm','maintenance_oil') AND completed_at IS NULL
)
UPDATE system_task_records SET completed_at=now(),completed_by_name='System',outcome='Consolidated into PM Service reminder'
WHERE id IN (SELECT id FROM ranked WHERE position>1);
UPDATE system_task_records SET kind='maintenance_pm',maintenance_service='pm',
 title=regexp_replace(title,'^(Oil change|PM service)','PM Service')
WHERE kind IN ('maintenance_pm','maintenance_oil');
UPDATE custom_tasks SET system_task_kind='maintenance_pm' WHERE system_task_kind='maintenance_oil';
DELETE FROM system_task_assignments WHERE kind='maintenance_oil';
ALTER TABLE system_task_assignments DROP CONSTRAINT system_task_assignments_kind_check;
ALTER TABLE system_task_assignments ADD CONSTRAINT system_task_assignments_kind_check
CHECK(kind IN ('driver_onboarding','driver_offboarding','relay_review','escrow_release','relay_setup','weighmytruck_setup','five_eld_truck','maintenance_pm'));
DROP INDEX system_task_maintenance_open_idx;
CREATE UNIQUE INDEX system_task_maintenance_open_idx ON system_task_records(maintenance_truck_id)
WHERE kind='maintenance_pm' AND completed_at IS NULL;

COMMIT;
