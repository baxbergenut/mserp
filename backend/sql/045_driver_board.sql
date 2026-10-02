BEGIN;

ALTER TABLE drivers ADD COLUMN driver_home TEXT NOT NULL DEFAULT '' CHECK (char_length(driver_home) <= 300);
ALTER TABLE drivers ADD COLUMN driver_home_version INTEGER NOT NULL DEFAULT 0;

CREATE FUNCTION version_driver_home() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.driver_home IS DISTINCT FROM OLD.driver_home THEN
        NEW.driver_home_version := OLD.driver_home_version + 1;
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER drivers_home_version BEFORE UPDATE ON drivers
FOR EACH ROW EXECUTE FUNCTION version_driver_home();

-- A live dispatch board, independent of weekly Gross Board load entries.
CREATE TABLE driver_board (
    driver_id UUID PRIMARY KEY REFERENCES drivers(id) ON DELETE CASCADE,
    current_load TEXT NOT NULL DEFAULT '' CHECK (char_length(current_load) <= 300),
    trailer_number TEXT NOT NULL DEFAULT '' CHECK (char_length(trailer_number) <= 100),
    status TEXT NOT NULL DEFAULT '' CHECK (status IN ('','ENROUTE','DISPATCHED','RESERVED','HOME','VACATION','SHOP','RESET','NO LOAD','STUCK','LATE DEL','TRUCK ISSUE','LEFT','NEW DRIVER','DEADHEAD','LOAD CANCELLED','REJECTED')),
    destination TEXT NOT NULL DEFAULT '' CHECK (char_length(destination) <= 500),
    eta TEXT NOT NULL DEFAULT '' CHECK (char_length(eta) <= 500),
    notes TEXT NOT NULL DEFAULT '' CHECK (char_length(notes) <= 5000),
    home_time TEXT NOT NULL DEFAULT '' CHECK (char_length(home_time) <= 500),
    version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
        ALTER TABLE driver_board OWNER TO mserp_app;
    END IF;
END $$;
COMMIT;
