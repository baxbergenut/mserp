-- Notifications only wake the exporter; the immutable outbox remains authoritative.
CREATE OR REPLACE FUNCTION notify_expense_sheet_export() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    PERFORM pg_notify('mserp_expense_sheet_export', '');
    RETURN NEW;
END;
$$;
CREATE TRIGGER expense_sheet_export_notify
AFTER INSERT ON expense_sheet_exports
FOR EACH ROW EXECUTE FUNCTION notify_expense_sheet_export();
