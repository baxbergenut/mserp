BEGIN;

-- Derived reporting evidence is maintained for every writer, including old
-- binaries during deployment. Source timestamps, prompts and payloads remain intact.
ALTER TABLE fuel_transactions ADD COLUMN reporting_timezone text;
ALTER TABLE fuel_transactions ADD COLUMN purchased_on date;
ALTER TABLE fuel_transactions ADD COLUMN reporting_timezone_valid boolean;
ALTER TABLE fuel_transactions ADD COLUMN reported_truck_units text[];

CREATE FUNCTION fuel_reporting_fields() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE zone text;
BEGIN
    IF TG_OP='UPDATE' AND NEW.purchased_at IS NOT DISTINCT FROM OLD.purchased_at
       AND NEW.timezone IS NOT DISTINCT FROM OLD.timezone AND NEW.prompts IS NOT DISTINCT FROM OLD.prompts
       AND OLD.purchased_on IS NOT NULL THEN
        NEW.reporting_timezone := OLD.reporting_timezone;
        NEW.purchased_on := OLD.purchased_on;
        NEW.reporting_timezone_valid := OLD.reporting_timezone_valid;
        NEW.reported_truck_units := OLD.reported_truck_units;
        RETURN NEW;
    END IF;
    zone := CASE NEW.timezone
        WHEN 'US/Eastern' THEN 'America/New_York' WHEN 'US/Central' THEN 'America/Chicago'
        WHEN 'US/Mountain' THEN 'America/Denver' WHEN 'US/Pacific' THEN 'America/Los_Angeles'
        WHEN 'US/Arizona' THEN 'America/Phoenix' WHEN 'US/Alaska' THEN 'America/Anchorage'
        WHEN 'US/Aleutian' THEN 'America/Adak' WHEN 'US/Hawaii' THEN 'Pacific/Honolulu'
        WHEN 'US/East-Indiana' THEN 'America/Indiana/Indianapolis' WHEN 'US/Indiana-Starke' THEN 'America/Indiana/Knox'
        WHEN 'US/Michigan' THEN 'America/Detroit' WHEN 'US/Samoa' THEN 'Pacific/Pago_Pago'
        ELSE coalesce((SELECT name FROM pg_timezone_names WHERE name=nullif(NEW.timezone,'') LIMIT 1),'America/New_York') END;
    NEW.reporting_timezone := zone;
    NEW.purchased_on := (NEW.purchased_at AT TIME ZONE zone)::date;
    NEW.reporting_timezone_valid := EXISTS(SELECT 1 FROM pg_timezone_names WHERE name=NEW.timezone);
    SELECT coalesce(array_agg(DISTINCT upper(btrim(p->>'value')) ORDER BY upper(btrim(p->>'value'))),'{}'::text[])
      INTO NEW.reported_truck_units FROM jsonb_array_elements(NEW.prompts) p
      WHERE lower(btrim(p->>'label'))='truck #' AND btrim(p->>'value')<>'';
    RETURN NEW;
END $$;
-- Materialize the timezone catalog once for the historical backfill.
WITH zones AS MATERIALIZED (SELECT name FROM pg_timezone_names), derived AS (
 SELECT f.id,coalesce(z.name,'America/New_York') zone,z.name IS NOT NULL valid,
 ARRAY(SELECT DISTINCT upper(btrim(p->>'value')) FROM jsonb_array_elements(f.prompts) p
 WHERE lower(btrim(p->>'label'))='truck #' AND btrim(p->>'value')<>'' ORDER BY 1) units
 FROM fuel_transactions f LEFT JOIN zones z ON z.name=nullif(f.timezone,'')
)
UPDATE fuel_transactions f SET reporting_timezone=d.zone,purchased_on=(f.purchased_at AT TIME ZONE d.zone)::date,
 reporting_timezone_valid=d.valid,reported_truck_units=d.units FROM derived d WHERE f.id=d.id;
-- Canonical aliases have identical date rules; store the same canonical label as new imports.
UPDATE fuel_transactions SET reporting_timezone=CASE reporting_timezone
 WHEN 'US/Eastern' THEN 'America/New_York' WHEN 'US/Central' THEN 'America/Chicago'
 WHEN 'US/Mountain' THEN 'America/Denver' WHEN 'US/Pacific' THEN 'America/Los_Angeles'
 WHEN 'US/Arizona' THEN 'America/Phoenix' WHEN 'US/Alaska' THEN 'America/Anchorage'
 WHEN 'US/Aleutian' THEN 'America/Adak' WHEN 'US/Hawaii' THEN 'Pacific/Honolulu'
 WHEN 'US/East-Indiana' THEN 'America/Indiana/Indianapolis' WHEN 'US/Indiana-Starke' THEN 'America/Indiana/Knox'
 WHEN 'US/Michigan' THEN 'America/Detroit' WHEN 'US/Samoa' THEN 'Pacific/Pago_Pago' ELSE reporting_timezone END;
CREATE TRIGGER fuel_reporting_fields_before BEFORE INSERT OR UPDATE ON fuel_transactions
FOR EACH ROW EXECUTE FUNCTION fuel_reporting_fields();
ALTER TABLE fuel_transactions ALTER COLUMN reporting_timezone SET NOT NULL;
ALTER TABLE fuel_transactions ALTER COLUMN purchased_on SET NOT NULL;
ALTER TABLE fuel_transactions ALTER COLUMN reporting_timezone_valid SET NOT NULL;
ALTER TABLE fuel_transactions ALTER COLUMN reported_truck_units SET NOT NULL;
CREATE INDEX fuel_transactions_reporting_date_idx ON fuel_transactions(purchased_on,driver_id);
CREATE INDEX truck_assignments_truck_history_idx ON truck_driver_assignments(truck_id,assigned_at);
DO $$ BEGIN IF to_regclass('payroll_settlements') IS NOT NULL THEN CREATE INDEX payroll_settlements_week_idx ON payroll_settlements(week_start); END IF; END $$;
DO $$ BEGIN IF to_regclass('investor_pay_weeks') IS NOT NULL THEN CREATE INDEX investor_pay_weeks_week_idx ON investor_pay_weeks(week_start); END IF; END $$;
CREATE INDEX loads_pickup_id_idx ON loads(pickup_time DESC,id DESC);
CREATE INDEX loads_driver_id_idx ON loads(driver_id);
CREATE INDEX loads_coverage_unit_idx ON loads(upper(regexp_replace(btrim(truck_unit),'[[:space:]]+',' ','g')));
CREATE INDEX loads_coverage_team_idx ON loads(lower(regexp_replace(btrim(team_driver_name),'[[:space:]]+',' ','g')));
ANALYZE fuel_transactions;
COMMIT;
