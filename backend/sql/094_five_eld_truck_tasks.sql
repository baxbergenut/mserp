BEGIN;
ALTER TABLE system_task_assignments DROP CONSTRAINT system_task_assignments_kind_check;
ALTER TABLE system_task_assignments ADD CONSTRAINT system_task_assignments_kind_check CHECK(kind IN ('driver_onboarding','driver_offboarding','relay_review','escrow_release','relay_setup','weighmytruck_setup','five_eld_truck'));
-- Initial responsibility requested for Nate; later edits use Settings and are preserved.
INSERT INTO system_task_assignments(kind,assignee_id,assignee_ids)
SELECT 'five_eld_truck',id,CASE WHEN id IS NULL THEN '{}'::uuid[] ELSE ARRAY[id] END
FROM (SELECT (SELECT min(id::text)::uuid FROM app_users WHERE active AND lower(username)='nate finesse' HAVING count(*)=1) AS id) initial;

ALTER TABLE system_task_records ADD COLUMN eld_vin text;
ALTER TABLE system_task_records ADD COLUMN eld_unit text NOT NULL DEFAULT '';
ALTER TABLE system_task_records ADD COLUMN eld_version integer NOT NULL DEFAULT 1;
-- Deliberately retained after truck deletion as a durable review receipt.
ALTER TABLE system_task_records ADD COLUMN eld_truck_id uuid;
CREATE UNIQUE INDEX system_task_eld_vin_idx ON system_task_records(eld_vin) WHERE kind='five_eld_truck';

CREATE FUNCTION reconcile_five_eld_tasks() RETURNS void LANGUAGE plpgsql AS $$
BEGIN
 INSERT INTO system_task_records(id,kind,title,notes,created_at,eld_vin,eld_unit)
 SELECT gen_random_uuid(),'five_eld_truck','Review Five ELD truck '||l.provider_truck_number,
 'Five ELD reports this truck working for MS Express. Review its details before adding it to MSERP.',
 now(),l.vin,l.provider_truck_number FROM five_eld_locations l
 WHERE EXISTS(SELECT 1 FROM five_eld_sync_state s WHERE l.fetched_at=s.last_success_at)
 AND NOT EXISTS(SELECT 1 FROM trucks t WHERE upper(regexp_replace(t.vin,'[^A-Za-z0-9]','','g'))=l.vin)
 AND NOT EXISTS(SELECT 1 FROM five_eld_sync_state s WHERE l.vin=ANY(s.ambiguous_vins))
 ON CONFLICT(eld_vin) WHERE kind='five_eld_truck' DO UPDATE SET
 eld_unit=excluded.eld_unit,title=excluded.title,eld_version=system_task_records.eld_version+1
 WHERE system_task_records.completed_at IS NULL AND system_task_records.eld_unit IS DISTINCT FROM excluded.eld_unit;

 UPDATE system_task_records s SET completed_at=now(),completed_by_name='System',outcome='Truck already in MSERP',eld_truck_id=t.id
 FROM trucks t WHERE s.kind='five_eld_truck' AND s.completed_at IS NULL
 AND upper(regexp_replace(t.vin,'[^A-Za-z0-9]','','g'))=s.eld_vin;
END $$;
SELECT reconcile_five_eld_tasks();
COMMIT;
