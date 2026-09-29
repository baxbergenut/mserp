BEGIN;

CREATE TABLE driver_charge_types (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 200),
    direction TEXT NOT NULL CHECK (direction IN ('charge','reimbursement')),
    amount NUMERIC(12,2) NOT NULL CHECK (amount > 0),
    archived BOOLEAN NOT NULL DEFAULT false,
    version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0)
);
CREATE TABLE driver_charge_schedules (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    driver_id UUID NOT NULL REFERENCES drivers(id) ON DELETE RESTRICT,
    type_id UUID REFERENCES driver_charge_types(id) ON DELETE RESTRICT,
    kind TEXT NOT NULL CHECK (kind IN ('recurring','installment')),
    name TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 200),
    direction TEXT NOT NULL CHECK (direction IN ('charge','reimbursement')),
    start_week DATE NOT NULL CHECK (extract(isodow FROM start_week)=1),
    end_week DATE CHECK (extract(isodow FROM end_week)=1 AND end_week >= start_week-7),
    eligibility TEXT NOT NULL CHECK (eligibility IN ('calendar','loads')),
    total NUMERIC(12,2) CHECK (total > 0),
    installment_count INTEGER NOT NULL DEFAULT 0 CHECK (installment_count BETWEEN 0 AND 5200),
    version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
    CHECK ((kind='recurring' AND type_id IS NOT NULL AND total IS NULL) OR
           (kind='installment' AND type_id IS NULL AND total IS NOT NULL AND direction='charge'))
);
CREATE INDEX driver_charge_schedules_driver_idx ON driver_charge_schedules(driver_id);
CREATE TABLE driver_charge_phases (
    schedule_id UUID NOT NULL REFERENCES driver_charge_schedules(id) ON DELETE RESTRICT,
    week_start DATE NOT NULL CHECK (extract(isodow FROM week_start)=1),
    amount NUMERIC(12,2) NOT NULL CHECK (amount > 0),
    paused BOOLEAN NOT NULL DEFAULT false,
    PRIMARY KEY(schedule_id,week_start)
);
CREATE TABLE driver_charge_occurrences (
    schedule_id UUID NOT NULL REFERENCES driver_charge_schedules(id) ON DELETE RESTRICT,
    week_start DATE NOT NULL CHECK (extract(isodow FROM week_start)=1),
    name TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 200),
    scheduled_amount NUMERIC(12,2) NOT NULL,
    amount NUMERIC(12,2) NOT NULL,
    overridden BOOLEAN NOT NULL DEFAULT false,
    confirmed_at TIMESTAMPTZ,
    confirmed_by UUID REFERENCES app_users(id) ON DELETE SET NULL,
    version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
    PRIMARY KEY(schedule_id,week_start)
);
CREATE TABLE driver_charge_events (
    id BIGSERIAL PRIMARY KEY,
    schedule_id UUID REFERENCES driver_charge_schedules(id) ON DELETE RESTRICT,
    type_id UUID REFERENCES driver_charge_types(id) ON DELETE RESTRICT,
    actor_id UUID REFERENCES app_users(id) ON DELETE SET NULL,
    action TEXT NOT NULL,
    details JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX driver_charge_events_schedule_idx ON driver_charge_events(schedule_id,id);
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
        ALTER TABLE driver_charge_types OWNER TO mserp_app;
        ALTER TABLE driver_charge_schedules OWNER TO mserp_app;
        ALTER TABLE driver_charge_phases OWNER TO mserp_app;
        ALTER TABLE driver_charge_occurrences OWNER TO mserp_app;
        ALTER TABLE driver_charge_events OWNER TO mserp_app;
    END IF;
END $$;
COMMIT;
