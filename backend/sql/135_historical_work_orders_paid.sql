BEGIN;

-- The user confirmed that work recorded before the fleet workflow was paid.
-- Keep this classification separate from service/identity review and from
-- driver/owner recovery. No payment transaction, method or amount is invented.
ALTER TABLE maintenance_records ADD COLUMN historical_paid BOOLEAN NOT NULL DEFAULT false;

WITH legacy AS MATERIALIZED (
 SELECT m.id,to_jsonb(m) AS before_value
 FROM maintenance_records m
 WHERE m.work_status='' AND m.dispatch_status=''
  AND m.status IN ('completed','needs_review')
  AND m.created_at < (SELECT applied_at FROM schema_migrations WHERE version='132_fleet_workflow.sql')
), changed AS (
 UPDATE maintenance_records m
 SET historical_paid=true,version=m.version+1,updated_at=now(),updated_by_name='User confirmed historical paid'
 FROM legacy l WHERE m.id=l.id
 RETURNING m.*
)
INSERT INTO maintenance_changes(record_id,actor_name,before_value,after_value)
SELECT c.id,'User confirmed historical paid',
 l.before_value || jsonb_build_object('workStatus',coalesce(nullif(l.before_value->>'work_status',''),l.before_value->>'status'),'historicalPaid',false),
 to_jsonb(c) || jsonb_build_object('workStatus','closed','historicalPaid',true,'serviceDate',c.service_date,'vendorName',c.vendor_name)
FROM changed c JOIN legacy l ON l.id=c.id;

-- Only the frozen migration population can carry this marker. New records
-- always follow the normal work and explicit payment-confirmation workflow.
CREATE FUNCTION maintenance_historical_paid_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF (TG_OP='INSERT' AND NEW.historical_paid)
  OR (TG_OP='UPDATE' AND NEW.historical_paid IS DISTINCT FROM OLD.historical_paid)
  OR (NEW.historical_paid AND NEW.dispatch_status<>'') THEN
  RAISE EXCEPTION 'Historical paid work is archived and cannot create a dispatch hold' USING ERRCODE='23514',CONSTRAINT='maintenance_historical_paid';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER maintenance_historical_paid_guard BEFORE INSERT OR UPDATE ON maintenance_records
 FOR EACH ROW EXECUTE FUNCTION maintenance_historical_paid_guard();

COMMIT;
