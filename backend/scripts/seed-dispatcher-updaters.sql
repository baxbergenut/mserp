-- User-requested import from Driver Board / TODAY, read October 4, 2026.
-- Source: 1mDQM6jUWGgagvI-hHT4WbNhDiOYsOvyiOIDqvhUIhk0, gid 1851742271.
-- Blank extensions remain unknown. Will Dauterive maps to William Dauterive.
-- Run after migration 049. Atomic and repeatable; no driver rosters are changed.
BEGIN;
CREATE TEMP TABLE updater_import(dispatcher_name text PRIMARY KEY, extension int, main_name text, after_name text) ON COMMIT DROP;
INSERT INTO updater_import VALUES
 ('Ryan Johnson',714,NULL,'Tim'),
 ('Edward Mironov',111,'Roy','Mika'),
 ('Wayne Dispatch',888,'Braden','Mika'),
 ('Jay Anderson',707,'Braden','Tim'),
 ('Mark Andrews',555,'Braden','Tim'),
 ('Brian Isaac',777,'Stella','Mika'),
 ('Jonathan Hensley',107,'Braden','Tim'),
 ('Alex Smith',444,'Stella','Justin'),
 ('Travis Walker',77,'Stella','Tim'),
 ('Simon Dispatch',NULL,'Roy','Justin'),
 ('William Dauterive',202,'Roy','Justin'),
 ('Cameron Davis',105,'Roy','Justin');
DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM updater_import s WHERE (SELECT count(*) FROM dispatchers d WHERE d.full_name=s.dispatcher_name)<>1) THEN
  RAISE EXCEPTION 'Expected exactly one existing dispatcher for every source heading';
 END IF;
END $$;
INSERT INTO updaters(full_name,normalized_name,shift,extension) VALUES
 ('Roy','roy','main',106),('Mika','mika','after_hours',NULL),
 ('Tim','tim','after_hours',NULL),('Braden','braden','main',707),
 ('Stella','stella','main',222),('Justin','justin','after_hours',NULL)
ON CONFLICT(normalized_name) DO UPDATE SET shift=excluded.shift,
 extension=coalesce(excluded.extension,updaters.extension),version=updaters.version+1;
UPDATE dispatchers d SET extension=s.extension,updated_at=now()
FROM updater_import s WHERE d.full_name=s.dispatcher_name AND s.extension IS NOT NULL AND d.extension IS DISTINCT FROM s.extension;
DELETE FROM dispatcher_updaters a USING dispatchers d,updater_import s
WHERE a.dispatcher_id=d.id AND d.full_name=s.dispatcher_name;
INSERT INTO dispatcher_updaters(dispatcher_id,shift,updater_id)
SELECT d.id,u.shift,u.id FROM updater_import s JOIN dispatchers d ON d.full_name=s.dispatcher_name
JOIN updaters u ON (u.full_name=s.main_name AND u.shift='main') OR (u.full_name=s.after_name AND u.shift='after_hours');
DO $$ BEGIN
 IF (SELECT count(*) FROM dispatcher_updaters a JOIN dispatchers d ON d.id=a.dispatcher_id JOIN updater_import s ON s.dispatcher_name=d.full_name)<>23 THEN
  RAISE EXCEPTION 'Expected 23 updater assignments';
 END IF;
END $$;
COMMIT;
