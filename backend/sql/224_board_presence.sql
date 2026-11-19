BEGIN;
SELECT pg_advisory_xact_lock(736281940);
LOCK TABLE driver_board_presence, gross_board_presence IN ACCESS EXCLUSIVE MODE;

CREATE TABLE board_presence (
 board text NOT NULL CHECK (board IN ('status','gross')),
 client_id uuid NOT NULL,
 user_id uuid NOT NULL REFERENCES app_users ON DELETE CASCADE,
 session_hash char(64) NOT NULL REFERENCES auth_sessions(token_hash) ON DELETE CASCADE,
 driver_id uuid NOT NULL REFERENCES drivers ON DELETE CASCADE,
 column_key text NOT NULL,
 expires_at timestamptz NOT NULL,
 week_start date,
 cell_date date,
 slot integer,
 PRIMARY KEY (board,client_id),
 CHECK (
  (board='status' AND week_start IS NULL AND cell_date IS NULL AND slot IS NULL
   AND column_key IN ('currentLoad','driver','truck','trailerNumber','originalGross','driverGross','phone','status','destination','notes','eta','loadPlan','homeTime'))
  OR
  (board='gross' AND week_start IS NOT NULL AND extract(isodow FROM week_start)=1
   AND week_start BETWEEN DATE '2000-01-03' AND DATE '2100-12-27' AND slot IS NOT NULL AND slot BETWEEN -1 AND 99
   AND ((column_key IN ('driver','truck','originalTotal','driverTotal','milesTotal','balance') AND cell_date IS NULL AND slot=0)
     OR (column_key IN ('loadNumber','originalRate','driverRate','miles','note') AND cell_date IS NOT NULL AND cell_date BETWEEN week_start AND week_start+6))))
);
CREATE INDEX board_presence_expiry ON board_presence(board,expires_at);
CREATE INDEX board_presence_gross_week ON board_presence(week_start) WHERE board='gross';

INSERT INTO board_presence(board,client_id,user_id,session_hash,driver_id,column_key,expires_at)
 SELECT 'status',client_id,user_id,session_hash,driver_id,column_key,expires_at FROM driver_board_presence;
INSERT INTO board_presence(board,client_id,user_id,session_hash,driver_id,column_key,expires_at,week_start,cell_date,slot)
 SELECT 'gross',client_id,user_id,session_hash,driver_id,column_key,expires_at,week_start,cell_date,slot FROM gross_board_presence;

CREATE TRIGGER status_presence_changed AFTER INSERT OR UPDATE ON board_presence
 FOR EACH ROW WHEN (NEW.board='status') EXECUTE FUNCTION notify_board_presence_change();
CREATE TRIGGER status_presence_deleted AFTER DELETE ON board_presence
 FOR EACH ROW WHEN (OLD.board='status') EXECUTE FUNCTION notify_board_presence_change();
CREATE TRIGGER gross_presence_changed AFTER INSERT OR UPDATE ON board_presence
 FOR EACH ROW WHEN (NEW.board='gross') EXECUTE FUNCTION notify_gross_presence_change();
CREATE TRIGGER gross_presence_deleted AFTER DELETE ON board_presence
 FOR EACH ROW WHEN (OLD.board='gross') EXECUTE FUNCTION notify_gross_presence_change();

DROP TABLE driver_board_presence, gross_board_presence RESTRICT;
INSERT INTO payroll_cache_revisions(table_name) VALUES('board_presence');
INSERT INTO payroll_projection_sources(table_name,classification) VALUES('board_presence','unrelated');
DO $$ BEGIN
 IF EXISTS (SELECT FROM pg_roles WHERE rolname='mserp_app') THEN
  ALTER TABLE board_presence OWNER TO mserp_app;
 END IF;
END $$;
COMMIT;
