BEGIN;

CREATE TABLE custom_tasks (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    title TEXT NOT NULL CHECK (char_length(title) BETWEEN 1 AND 200 AND title = btrim(title)),
    notes TEXT NOT NULL DEFAULT '' CHECK (char_length(notes) <= 5000),
    completed_at TIMESTAMPTZ,
    created_by UUID REFERENCES app_users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX custom_tasks_status_created_idx ON custom_tasks ((completed_at IS NOT NULL), created_at DESC, id);

DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'mserp_app') THEN
        ALTER TABLE custom_tasks OWNER TO mserp_app;
    END IF;
END $$;

COMMIT;
