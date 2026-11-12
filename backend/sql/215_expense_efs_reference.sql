-- Historical EFS expenses have no reliable payment reference to backfill.
CREATE EXTENSION IF NOT EXISTS pg_trgm WITH SCHEMA public;

ALTER TABLE expenses ADD COLUMN IF NOT EXISTS efs_reference_number text;

CREATE INDEX IF NOT EXISTS expenses_efs_reference_trgm_idx
    ON expenses USING gin (efs_reference_number public.gin_trgm_ops)
    WHERE efs_reference_number IS NOT NULL AND lower(btrim(payment_type)) = 'efs';
