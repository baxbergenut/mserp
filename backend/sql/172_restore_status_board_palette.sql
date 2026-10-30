BEGIN;

-- Restore the pre-palette Status Board vocabulary without rewriting source
-- records, immutable history, payroll snapshots or saved color audit receipts.
SELECT fleet_status_lock();
DROP TRIGGER IF EXISTS a_normalize_live_status ON driver_board;
DROP FUNCTION IF EXISTS normalize_live_board_status();
DROP TRIGGER IF EXISTS a_normalize_driver_rest ON drivers;
DROP FUNCTION IF EXISTS normalize_driver_rest();

CREATE OR REPLACE FUNCTION fleet_status_personal_label(availability text) RETURNS text LANGUAGE sql IMMUTABLE AS $$
 SELECT CASE availability WHEN 'home' THEN 'HOME' WHEN 'vacation' THEN 'VACATION' WHEN 'rest' THEN 'REST' WHEN 'reset' THEN '34 RESET'
 WHEN 'family_issue' THEN 'Family issue' WHEN 'health_issue' THEN 'Health issue' WHEN 'new_driver' THEN 'NEW DRIVER' ELSE '' END
$$;

INSERT INTO fleet_status_rules(board_status,availability,truck_condition,requires_active,requires_ready,ordinal) VALUES
 ('At PU','','',true,true,17),
 ('At DEL','','',true,true,19),
 ('HOME (YARD)','home','',false,false,20),
 ('34 RESET','reset','',false,false,22)
ON CONFLICT(board_status) DO UPDATE SET availability=excluded.availability,
 truck_condition=excluded.truck_condition,requires_active=excluded.requires_active,
 requires_ready=excluded.requires_ready,ordinal=excluded.ordinal;

-- Migration 171's unused settings/audit tables and compatibility constraints
-- intentionally remain. Removing them would discard durable records or make
-- restoration from an earlier backup depend on skipping a deployed migration.
COMMIT;
