BEGIN;

-- Keep identity/name snapshots after a driver or account is removed.
CREATE TABLE driver_board_history (
    id BIGSERIAL PRIMARY KEY,
    driver_id UUID NOT NULL,
    driver_name TEXT NOT NULL,
    actor_id TEXT NOT NULL DEFAULT '',
    actor_name TEXT NOT NULL,
    source TEXT NOT NULL,
    undo_of BIGINT REFERENCES driver_board_history(id),
    before_values JSONB NOT NULL,
    after_values JSONB NOT NULL,
    transaction_id BIGINT NOT NULL DEFAULT txid_current(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (driver_id, transaction_id)
);
CREATE INDEX driver_board_history_driver_idx ON driver_board_history(driver_id,id DESC);

CREATE FUNCTION audit_driver_board_change() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    previous JSONB; following JSONB; changed_before JSONB; changed_after JSONB;
    driver UUID; driver_name_snapshot TEXT; actor TEXT; actor_name_snapshot TEXT;
BEGIN
    IF TG_TABLE_NAME = 'drivers' THEN
        IF OLD.driver_home IS NOT DISTINCT FROM NEW.driver_home THEN RETURN NEW; END IF;
        driver := NEW.id;
        previous := jsonb_build_object('driverHome',OLD.driver_home);
        following := jsonb_build_object('driverHome',NEW.driver_home);
    ELSE
        driver := NEW.driver_id;
        following := jsonb_build_object('currentLoad',NEW.current_load,'trailerNumber',NEW.trailer_number,
          'status',NEW.status,'destination',NEW.destination,'eta',NEW.eta,'notes',NEW.notes,'homeTime',NEW.home_time);
        IF TG_OP = 'INSERT' THEN
            SELECT jsonb_object_agg(key,''::text) INTO previous FROM jsonb_each(following);
        ELSE
            previous := jsonb_build_object('currentLoad',OLD.current_load,'trailerNumber',OLD.trailer_number,
              'status',OLD.status,'destination',OLD.destination,'eta',OLD.eta,'notes',OLD.notes,'homeTime',OLD.home_time);
        END IF;
    END IF;
    SELECT jsonb_object_agg(key,previous->key), jsonb_object_agg(key,value)
      INTO changed_before,changed_after FROM jsonb_each(following)
      WHERE previous->key IS DISTINCT FROM value;
    IF changed_after IS NULL THEN RETURN NEW; END IF;
    SELECT full_name INTO driver_name_snapshot FROM drivers WHERE id=driver;
    actor := coalesce(current_setting('mserp.board_actor',true),'');
    SELECT username INTO actor_name_snapshot FROM app_users WHERE id::text=actor;
    INSERT INTO driver_board_history(driver_id,driver_name,actor_id,actor_name,source,undo_of,before_values,after_values)
    VALUES(driver,driver_name_snapshot,actor,coalesce(actor_name_snapshot,'System'),
      coalesce(nullif(current_setting('mserp.board_source',true),''),'database'),
      nullif(current_setting('mserp.board_undo',true),'')::bigint,changed_before,changed_after)
    ON CONFLICT(driver_id,transaction_id) DO UPDATE SET
      before_values=EXCLUDED.before_values || driver_board_history.before_values,
      after_values=driver_board_history.after_values || EXCLUDED.after_values;
    RETURN NEW;
END $$;
CREATE TRIGGER audit_driver_board AFTER INSERT OR UPDATE ON driver_board
FOR EACH ROW EXECUTE FUNCTION audit_driver_board_change();
CREATE TRIGGER audit_driver_home AFTER UPDATE ON drivers
FOR EACH ROW EXECUTE FUNCTION audit_driver_board_change();

-- Only the transaction creating an event may combine its field changes.
CREATE FUNCTION protect_driver_board_history() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='DELETE' OR OLD.transaction_id<>txid_current() THEN
        RAISE EXCEPTION 'driver board history is append-only';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER protect_driver_board_history BEFORE UPDATE OR DELETE ON driver_board_history
FOR EACH ROW EXECUTE FUNCTION protect_driver_board_history();
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
        ALTER TABLE driver_board_history OWNER TO mserp_app;
        ALTER SEQUENCE driver_board_history_id_seq OWNER TO mserp_app;
    END IF;
END $$;
COMMIT;
