BEGIN;
-- Date corrections can hide an already-materialized task until its new due date.
-- Wake connected task feeds even when no new system_task_record is inserted.
CREATE TRIGGER escrow_review_changed AFTER INSERT OR UPDATE OR DELETE ON escrow_release_reviews
FOR EACH ROW EXECUTE FUNCTION notify_task_change();
COMMIT;
