BEGIN;

-- Truck fees and investor operating costs share one exact weekly collection ledger.
-- Owner identity keeps an existing balance from transferring on a truck sale.
CREATE TABLE pay_source_collections (
    source TEXT NOT NULL,
    owner_id UUID NOT NULL REFERENCES investors(id),
    week_start DATE NOT NULL CHECK (extract(isodow FROM week_start)=1),
    kind TEXT NOT NULL CHECK (kind IN ('driver','investor')),
    statement_id UUID NOT NULL,
    truck_id UUID NOT NULL REFERENCES trucks(id),
    name TEXT NOT NULL,
    base NUMERIC(14,2) NOT NULL,
    amount NUMERIC(14,2) NOT NULL,
    statement JSONB NOT NULL,
    version INTEGER NOT NULL DEFAULT 1,
    updated_by UUID REFERENCES app_users(id),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY(source,owner_id,week_start)
);
CREATE INDEX pay_source_statement ON pay_source_collections(kind,statement_id,week_start);
CREATE FUNCTION protect_pay_source_collection() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE v RECORD;
BEGIN
    IF TG_OP='UPDATE' THEN
        IF NEW.source<>OLD.source OR NEW.owner_id<>OLD.owner_id OR NEW.week_start<>OLD.week_start THEN
            RAISE EXCEPTION 'Collection identity cannot be changed' USING ERRCODE='23514';
        END IF;
        IF EXISTS(SELECT 1 FROM payroll_settlements WHERE OLD.kind='driver' AND driver_id=OLD.statement_id AND week_start=OLD.week_start AND finalized)
           OR EXISTS(SELECT 1 FROM investor_pay_weeks WHERE OLD.kind='investor' AND truck_id=OLD.statement_id AND week_start=OLD.week_start AND finalized) THEN
            RAISE EXCEPTION 'Reopen this statement before changing its collections' USING ERRCODE='23514';
        END IF;
    END IF;
    IF TG_OP='DELETE' THEN v:=OLD; ELSE v:=NEW; END IF;
    IF EXISTS(SELECT 1 FROM payroll_settlements WHERE v.kind='driver' AND driver_id=v.statement_id AND week_start=v.week_start AND finalized)
       OR EXISTS(SELECT 1 FROM investor_pay_weeks WHERE v.kind='investor' AND truck_id=v.statement_id AND week_start=v.week_start AND finalized) THEN
        RAISE EXCEPTION 'Reopen this statement before changing its collections' USING ERRCODE='23514';
    END IF;
    RETURN v;
END $$;
CREATE TRIGGER pay_source_guard BEFORE INSERT OR UPDATE OR DELETE ON pay_source_collections FOR EACH ROW EXECUTE FUNCTION protect_pay_source_collection();
INSERT INTO payroll_cache_revisions(table_name) VALUES('pay_source_collections');
CREATE TRIGGER payroll_cache_insert AFTER INSERT ON pay_source_collections REFERENCING NEW TABLE AS changed_rows FOR EACH STATEMENT EXECUTE FUNCTION invalidate_payroll_cache();
CREATE TRIGGER payroll_cache_update AFTER UPDATE ON pay_source_collections REFERENCING NEW TABLE AS changed_rows FOR EACH STATEMENT EXECUTE FUNCTION invalidate_payroll_cache();
CREATE TRIGGER payroll_cache_delete AFTER DELETE ON pay_source_collections REFERENCING OLD TABLE AS changed_rows FOR EACH STATEMENT EXECUTE FUNCTION invalidate_payroll_cache();
CREATE TRIGGER payroll_cache_truncate AFTER TRUNCATE ON pay_source_collections FOR EACH STATEMENT EXECUTE FUNCTION invalidate_payroll_cache();
DO $$ BEGIN
    IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
        ALTER TABLE pay_source_collections OWNER TO mserp_app;
    END IF;
END $$;
COMMIT;
