BEGIN;

-- Keep the original one-entry table readable/writable by the previous release.
-- Additional slots have independent versions and retain tombstones on removal.
CREATE TABLE gross_board_extra_entries (
    driver_id UUID NOT NULL REFERENCES drivers(id) ON DELETE CASCADE,
    service_date DATE NOT NULL,
    slot INTEGER NOT NULL CHECK (slot BETWEEN 1 AND 99),
    load_number TEXT NOT NULL DEFAULT '' CHECK (length(load_number) <= 200),
    load_record_id INTEGER REFERENCES loads(id) ON DELETE SET NULL,
    original_rate NUMERIC(12,2),
    driver_rate NUMERIC(12,2),
    miles NUMERIC(12,2) CHECK (miles >= 0),
    entered_original_rate NUMERIC(12,2),
    entered_miles NUMERIC(12,2) CHECK (entered_miles >= 0),
    day_status TEXT NOT NULL DEFAULT '' CHECK (day_status IN ('', 'SHOP', 'HOME', 'RESET', 'IN TRANSIT', 'REJECTED', 'NO LOAD', 'STUCK', 'LATE DEL', 'TRUCK ISSUE', 'LEFT', 'NEW DRIVER', 'DEADHEAD')),
    deleted BOOLEAN NOT NULL DEFAULT false,
    version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (driver_id, service_date, slot)
);
CREATE INDEX gross_board_extra_entries_date_idx ON gross_board_extra_entries(service_date);

CREATE TABLE driver_pay_weeks (
    driver_id UUID NOT NULL REFERENCES drivers(id) ON DELETE CASCADE,
    week_start DATE NOT NULL CHECK (extract(isodow FROM week_start) = 1),
    notes TEXT NOT NULL DEFAULT '' CHECK (char_length(notes) <= 5000),
    comments JSONB NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(comments) = 'object'),
    adjustments JSONB NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(adjustments) = 'array'),
    version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (driver_id, week_start)
);

DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'mserp_app') THEN
        ALTER TABLE gross_board_extra_entries OWNER TO mserp_app;
        ALTER TABLE driver_pay_weeks OWNER TO mserp_app;
    END IF;
END $$;

COMMIT;
