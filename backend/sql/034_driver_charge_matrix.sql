BEGIN;

ALTER TABLE driver_charge_types ADD COLUMN amounts NUMERIC(12,2)[];
ALTER TABLE driver_charge_types ADD COLUMN eligibility TEXT NOT NULL DEFAULT 'calendar'
    CHECK (eligibility IN ('calendar','loads','no_loads'));
-- Retain every previously assigned amount as an available choice.
UPDATE driver_charge_types t SET amounts = ARRAY[t.amount] || ARRAY(
    SELECT DISTINCT p.amount FROM driver_charge_phases p
    JOIN driver_charge_schedules s ON s.id=p.schedule_id
    WHERE s.type_id=t.id AND p.amount<>t.amount ORDER BY p.amount
), eligibility = CASE WHEN EXISTS(SELECT 1 FROM driver_charge_schedules s WHERE s.type_id=t.id)
    AND NOT EXISTS(SELECT 1 FROM driver_charge_schedules s WHERE s.type_id=t.id AND s.eligibility<>'loads')
    THEN 'loads' ELSE 'calendar' END;
ALTER TABLE driver_charge_types ALTER COLUMN amounts SET NOT NULL;
ALTER TABLE driver_charge_types ADD CHECK (cardinality(amounts)>0 AND 0 < ALL(amounts));

-- Rules take effect from a Monday; older weeks retain their original rules.
CREATE TABLE driver_charge_type_rules (
    type_id UUID NOT NULL REFERENCES driver_charge_types(id) ON DELETE RESTRICT,
    week_start DATE NOT NULL CHECK (extract(isodow FROM week_start)=1),
    eligibility TEXT NOT NULL CHECK (eligibility IN ('calendar','loads','no_loads')),
    PRIMARY KEY(type_id,week_start)
);
INSERT INTO driver_charge_type_rules(type_id,week_start,eligibility)
SELECT id,date_trunc('week',now() AT TIME ZONE 'America/New_York')::date,eligibility FROM driver_charge_types;
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
        ALTER TABLE driver_charge_type_rules OWNER TO mserp_app;
    END IF;
END $$;
COMMIT;
