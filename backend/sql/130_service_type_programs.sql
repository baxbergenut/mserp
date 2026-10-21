BEGIN;

ALTER TABLE maintenance_types ADD COLUMN program_id UUID REFERENCES service_programs(id) ON DELETE RESTRICT;
UPDATE maintenance_types SET name='Retired oil service (oil)',version=version+1 WHERE code='oil';
UPDATE maintenance_types SET name='Oil change',program_id='00000000-0000-4000-8000-000000000020',version=version+1 WHERE code='preventive';

INSERT INTO access_audit(action,target_id,details)
SELECT 'service_type_program_link',program_id,jsonb_build_object('typeCode',code,'name',name,'programId',program_id) FROM maintenance_types WHERE code='preventive';

COMMIT;
