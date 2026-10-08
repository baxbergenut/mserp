BEGIN;
ALTER TABLE app_users ADD COLUMN theme TEXT NOT NULL DEFAULT 'default'
    CHECK (theme IN ('default', 'solarized-light', 'solarized-dark', 'monokai', 'monokai-dimmed', 'dark-modern', 'default-light'));
COMMIT;
