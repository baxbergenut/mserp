-- Each updater has one shift; each dispatcher has at most one updater per shift.
CREATE TABLE updaters (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    full_name TEXT NOT NULL CHECK (btrim(full_name) <> ''),
    normalized_name TEXT NOT NULL UNIQUE,
    shift TEXT NOT NULL CHECK (shift IN ('main', 'after_hours')),
    extension INTEGER CHECK (extension BETWEEN 0 AND 999999),
    version INTEGER NOT NULL DEFAULT 1,
    UNIQUE (id, shift)
);

ALTER TABLE dispatchers ADD COLUMN extension INTEGER CHECK (extension BETWEEN 0 AND 999999);

CREATE TABLE dispatcher_updaters (
    dispatcher_id UUID NOT NULL REFERENCES dispatchers(id) ON DELETE CASCADE,
    shift TEXT NOT NULL CHECK (shift IN ('main', 'after_hours')),
    updater_id UUID NOT NULL,
    PRIMARY KEY (dispatcher_id, shift),
    FOREIGN KEY (updater_id, shift) REFERENCES updaters(id, shift) ON DELETE CASCADE
);
CREATE INDEX dispatcher_updaters_updater_idx ON dispatcher_updaters(updater_id);

DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'mserp_app') THEN
        ALTER TABLE updaters OWNER TO mserp_app;
        ALTER TABLE dispatcher_updaters OWNER TO mserp_app;
    END IF;
END $$;
