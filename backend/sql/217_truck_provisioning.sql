BEGIN;
ALTER TABLE trucks ADD COLUMN truck_class varchar(20), ADD COLUMN gvwr_lbs integer;
COMMIT;
