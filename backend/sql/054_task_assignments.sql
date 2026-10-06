BEGIN;

CREATE TABLE system_task_assignments (
    id UUID NOT NULL UNIQUE DEFAULT gen_random_uuid(),
    kind TEXT PRIMARY KEY CHECK (kind IN ('driver_onboarding','driver_offboarding','relay_review')),
    assignee_id UUID REFERENCES app_users(id) ON DELETE SET NULL,
    version INTEGER NOT NULL DEFAULT 1,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by UUID REFERENCES app_users(id) ON DELETE SET NULL
);
INSERT INTO system_task_assignments(kind) VALUES ('driver_onboarding'),('driver_offboarding'),('relay_review');

ALTER TABLE custom_tasks ADD COLUMN assigned_to UUID REFERENCES app_users(id) ON DELETE SET NULL;
ALTER TABLE custom_tasks ADD COLUMN assigned_by UUID REFERENCES app_users(id) ON DELETE SET NULL;
ALTER TABLE custom_tasks ADD COLUMN system_task_kind TEXT REFERENCES system_task_assignments(kind);
-- Use the durable integration link, never task title/name matching.
DO $$ BEGIN
    IF to_regclass('fleetscope_driver_terminations') IS NOT NULL THEN
        UPDATE custom_tasks c SET system_task_kind='driver_offboarding'
        FROM fleetscope_driver_terminations t WHERE t.task_id=c.id;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
        ALTER TABLE system_task_assignments OWNER TO mserp_app;
    END IF;
END $$;
COMMIT;
