BEGIN;

-- Pending hires enter/leave the Unassigned section as setup progresses.
CREATE TRIGGER status_board_changed AFTER INSERT OR UPDATE OR DELETE ON fleetscope_driver_intake
FOR EACH STATEMENT EXECUTE FUNCTION notify_status_board_change();

COMMIT;
