BEGIN;
CREATE FUNCTION notify_gross_board_change() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN PERFORM pg_notify('mserp_gross_board',''); RETURN NULL; END $$;
DO $$ DECLARE name text; BEGIN
    FOREACH name IN ARRAY ARRAY['gross_board_entries','gross_board_extra_entries','loads','drivers','trucks',
      'dispatchers','truck_driver_assignments','driver_teams','driver_dispatcher_assignments'] LOOP
        EXECUTE format('CREATE TRIGGER gross_board_changed AFTER INSERT OR UPDATE OR DELETE ON %I FOR EACH STATEMENT EXECUTE FUNCTION notify_gross_board_change()',name);
    END LOOP;
END $$;
CREATE TABLE gross_board_presence (
    client_id uuid PRIMARY KEY,
    user_id uuid NOT NULL REFERENCES app_users(id) ON DELETE CASCADE,
    session_hash char(64) NOT NULL REFERENCES auth_sessions(token_hash) ON DELETE CASCADE,
    driver_id uuid NOT NULL REFERENCES drivers(id) ON DELETE CASCADE,
    week_start date NOT NULL CHECK(extract(isodow FROM week_start)=1 AND week_start BETWEEN '2000-01-03' AND '2100-12-27'),
    cell_date date,
    slot integer NOT NULL CHECK(slot BETWEEN -1 AND 99),
    column_key text NOT NULL CHECK(column_key IN ('driver','truck','loadNumber','originalRate','driverRate','miles','note','originalTotal','driverTotal','milesTotal','balance')),
    expires_at timestamptz NOT NULL,
    CHECK((column_key IN ('driver','truck','originalTotal','driverTotal','milesTotal','balance') AND cell_date IS NULL AND slot=0)
       OR (column_key IN ('loadNumber','originalRate','driverRate','miles','note') AND cell_date BETWEEN week_start AND week_start+6 AND cell_date IS NOT NULL))
);
CREATE INDEX gross_board_presence_expiry ON gross_board_presence(expires_at);
CREATE INDEX gross_board_presence_week ON gross_board_presence(week_start);
CREATE FUNCTION notify_gross_presence_change() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='UPDATE' AND OLD.driver_id=NEW.driver_id AND OLD.week_start=NEW.week_start
      AND OLD.cell_date IS NOT DISTINCT FROM NEW.cell_date AND OLD.slot=NEW.slot AND OLD.column_key=NEW.column_key THEN RETURN NULL; END IF;
    PERFORM pg_notify('mserp_gross_presence',''); RETURN NULL;
END $$;
CREATE TRIGGER gross_presence_changed AFTER INSERT OR UPDATE OR DELETE ON gross_board_presence
FOR EACH ROW EXECUTE FUNCTION notify_gross_presence_change();
INSERT INTO payroll_cache_revisions(table_name) VALUES('gross_board_presence');
DO $$ BEGIN
    IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN ALTER TABLE gross_board_presence OWNER TO mserp_app; END IF;
END $$;
COMMIT;
