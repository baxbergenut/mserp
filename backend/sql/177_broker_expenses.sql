BEGIN;
ALTER TABLE expenses ADD COLUMN broker_load_number text;
ALTER TABLE expenses ADD CONSTRAINT expenses_broker_load_length CHECK (
    broker_load_number IS NULL OR char_length(btrim(broker_load_number)) BETWEEN 1 AND 100
);
-- Imported historical Broker rows have no reviewed load number. Retain them and
-- allow unrelated bookkeeping updates, but never create another missing link or
-- erase a reviewed number while the broker remains responsible.
CREATE FUNCTION require_broker_expense_load() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF lower(btrim(NEW.covered_by))='broker' AND NEW.broker_load_number IS NULL THEN
        IF TG_OP='UPDATE' THEN
            IF lower(btrim(OLD.covered_by))='broker' AND OLD.broker_load_number IS NULL THEN
                RETURN NEW;
            END IF;
        END IF;
        RAISE EXCEPTION 'Broker-covered expenses require a load number' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER expenses_broker_load BEFORE INSERT OR UPDATE ON expenses
    FOR EACH ROW EXECUTE FUNCTION require_broker_expense_load();
CREATE INDEX expenses_broker_load_idx ON expenses(lower(btrim(broker_load_number)))
    WHERE lower(btrim(covered_by))='broker';
COMMIT;
