BEGIN;

-- Full releases need no explanatory reason. Keep the audit reason mandatory
-- when any funds are retained, and preserve the completion-state invariant.
DO $$
DECLARE constraint_name text;
BEGIN
 FOR constraint_name IN
  SELECT conname FROM pg_constraint
  WHERE conrelid='escrow_release_reviews'::regclass AND contype='c'
    AND pg_get_constraintdef(oid) LIKE '%reason%'
 LOOP
  EXECUTE format('ALTER TABLE escrow_release_reviews DROP CONSTRAINT %I',constraint_name);
 END LOOP;
END $$;
ALTER TABLE escrow_release_reviews ADD CONSTRAINT escrow_review_completion_reason_check
 CHECK ((decision IS NULL AND completed_at IS NULL) OR
 (decision IS NOT NULL AND completed_at IS NOT NULL AND length(btrim(reason))<=5000
  AND (decision='released' OR length(btrim(reason))>=1)));

COMMIT;
