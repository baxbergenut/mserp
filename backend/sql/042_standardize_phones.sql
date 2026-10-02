BEGIN;

-- A NULL result means missing or irrecoverable, never guessed or padded digits.
CREATE FUNCTION canonical_phone(value TEXT) RETURNS TEXT
LANGUAGE plpgsql IMMUTABLE AS $$
DECLARE digits TEXT;
BEGIN
    value := btrim(value, E' \t\r\n');
    IF value IS NULL OR value = '' OR value ~ '[^0-9+(). -]' THEN
        RETURN NULL;
    END IF;
    digits := regexp_replace(value, '[^0-9]', '', 'g');
    IF length(digits) = 11 AND left(digits, 1) = '1' THEN
        digits := substring(digits FROM 2);
    END IF;
    IF digits ~ '^[0-9]{10}$' THEN RETURN digits; END IF;
    RETURN NULL;
END $$;

-- Legacy contacts sometimes list a primary and secondary number separated by
-- slash. Keep the first only when every listed number is independently valid;
-- the audit retains the complete original. New writes remain single-number.
CREATE FUNCTION backfill_phone(value TEXT) RETURNS TEXT
LANGUAGE plpgsql IMMUTABLE AS $$
DECLARE parts TEXT[]; part TEXT; first_phone TEXT;
BEGIN
    IF canonical_phone(value) IS NOT NULL THEN RETURN canonical_phone(value); END IF;
    IF value IS NULL OR position('/' IN value)=0 THEN RETURN NULL; END IF;
    parts := string_to_array(value, '/');
    FOREACH part IN ARRAY parts LOOP
        IF canonical_phone(part) IS NULL THEN RETURN NULL; END IF;
        first_phone := coalesce(first_phone, canonical_phone(part));
    END LOOP;
    RETURN first_phone;
END $$;

-- Preserve the first FleetScope snapshot and receipt hashes. The application
-- exposes this canonical contact column instead of the raw source JSON phone.
ALTER TABLE fleetscope_driver_intake ADD COLUMN phone TEXT;

CREATE TABLE phone_normalization_audit (
    source_table TEXT NOT NULL,
    record_id UUID NOT NULL,
    original_value TEXT NOT NULL,
    normalized_value TEXT CHECK (normalized_value ~ '^[0-9]{10}$'),
    reason TEXT NOT NULL CHECK (reason IN ('normalized', 'primary_selected', 'invalid', 'blank')),
    normalized_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (source_table, record_id)
);

-- Capture all originals before updates, including driver-linked investor copies
-- which the existing identity trigger will update when the driver changes.
INSERT INTO phone_normalization_audit(source_table,record_id,original_value,normalized_value,reason)
SELECT source_table,id,value,backfill_phone(value),
    CASE WHEN btrim(value, E' \t\r\n') = '' THEN 'blank'
         WHEN backfill_phone(value) IS NULL THEN 'invalid'
         WHEN canonical_phone(value) IS NULL THEN 'primary_selected' ELSE 'normalized' END
FROM (
    SELECT 'drivers' AS source_table,id,phone AS value FROM drivers
    UNION ALL SELECT 'dispatchers',id,phone FROM dispatchers
    UNION ALL SELECT 'investors',id,phone FROM investors
    UNION ALL SELECT 'relay_driver_links',id,relay_phone FROM relay_driver_links
    UNION ALL SELECT 'fleetscope_driver_intake',id,driver_data->>'phone' FROM fleetscope_driver_intake
) contacts WHERE value IS NOT NULL AND value IS DISTINCT FROM backfill_phone(value);

UPDATE drivers SET phone=backfill_phone(phone) WHERE phone IS DISTINCT FROM backfill_phone(phone);
UPDATE dispatchers SET phone=backfill_phone(phone) WHERE phone IS DISTINCT FROM backfill_phone(phone);
UPDATE investors SET phone=backfill_phone(phone) WHERE phone IS DISTINCT FROM backfill_phone(phone);
UPDATE relay_driver_links SET relay_phone=backfill_phone(relay_phone) WHERE relay_phone IS DISTINCT FROM backfill_phone(relay_phone);
UPDATE fleetscope_driver_intake SET phone=backfill_phone(driver_data->>'phone');
DROP FUNCTION backfill_phone(TEXT);

ALTER TABLE drivers ADD CONSTRAINT drivers_phone_check CHECK (phone ~ '^[0-9]{10}$');
ALTER TABLE dispatchers ADD CONSTRAINT dispatchers_phone_check CHECK (phone ~ '^[0-9]{10}$');
ALTER TABLE investors ADD CONSTRAINT investors_phone_check CHECK (phone ~ '^[0-9]{10}$');
ALTER TABLE relay_driver_links ADD CONSTRAINT relay_driver_links_phone_check CHECK (relay_phone ~ '^[0-9]{10}$');
ALTER TABLE fleetscope_driver_intake ADD CONSTRAINT fleetscope_intake_phone_check CHECK (phone ~ '^[0-9]{10}$');

-- Normalize valid writes from older binaries too, preserving rollback support.
CREATE FUNCTION normalize_contact_phone() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE value TEXT; normalized TEXT;
BEGIN
    value := to_jsonb(NEW)->>TG_ARGV[0];
    normalized := canonical_phone(value);
    IF nullif(btrim(value, E' \t\r\n'), '') IS NOT NULL AND normalized IS NULL THEN
        RAISE EXCEPTION 'phone must contain exactly 10 digits' USING ERRCODE='23514';
    END IF;
    NEW := jsonb_populate_record(NEW, jsonb_build_object(TG_ARGV[0], normalized));
    RETURN NEW;
END $$;
CREATE TRIGGER drivers_phone_normalize BEFORE INSERT OR UPDATE OF phone ON drivers
FOR EACH ROW EXECUTE FUNCTION normalize_contact_phone('phone');
CREATE TRIGGER dispatchers_phone_normalize BEFORE INSERT OR UPDATE OF phone ON dispatchers
FOR EACH ROW EXECUTE FUNCTION normalize_contact_phone('phone');
CREATE TRIGGER investors_phone_normalize BEFORE INSERT OR UPDATE OF phone ON investors
FOR EACH ROW EXECUTE FUNCTION normalize_contact_phone('phone');
CREATE TRIGGER relay_phone_normalize BEFORE INSERT OR UPDATE OF relay_phone ON relay_driver_links
FOR EACH ROW EXECUTE FUNCTION normalize_contact_phone('relay_phone');

-- Old webhook binaries still populate only driver_data; derive the contact
-- without changing that snapshot or blocking receipt for malformed source data.
CREATE FUNCTION set_intake_phone() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    NEW.phone := canonical_phone(NEW.driver_data->>'phone');
    RETURN NEW;
END $$;
CREATE TRIGGER intake_phone_from_source BEFORE INSERT OR UPDATE OF driver_data ON fleetscope_driver_intake
FOR EACH ROW EXECUTE FUNCTION set_intake_phone();
CREATE TRIGGER intake_phone_normalize BEFORE UPDATE OF phone ON fleetscope_driver_intake
FOR EACH ROW EXECUTE FUNCTION normalize_contact_phone('phone');

DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
        ALTER TABLE phone_normalization_audit OWNER TO mserp_app;
    END IF;
END $$;
COMMIT;
