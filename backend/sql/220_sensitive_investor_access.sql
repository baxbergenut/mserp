BEGIN;
-- One-time deployment policy. Later explicit role edits use the normal catalog.
CREATE TEMP TABLE investor_access_before ON COMMIT DROP AS
SELECT id,permissions FROM app_roles;
UPDATE app_roles SET permissions=CASE
 WHEN lower(btrim(name)) IN ('accountant','accountants') THEN
   ARRAY(SELECT DISTINCT p FROM unnest(permissions || ARRAY['investors.read','investor_pay.read']) p ORDER BY p)
 ELSE ARRAY(SELECT p FROM unnest(permissions) p
   WHERE p NOT LIKE 'investors.%' AND p NOT LIKE 'investor_pay.%' AND p NOT LIKE 'investor_terms.%')
 END,version=version+1 WHERE NOT system_role;
INSERT INTO access_audit(actor_id,action,target_id,details)
SELECT NULL,'sensitive_investor_access',r.id,
 jsonb_build_object('before',b.permissions,'after',r.permissions,'reason','Investor access restricted to Administrators and Accountants at deployment')
FROM app_roles r JOIN investor_access_before b USING(id);
-- Drop cached browser data through a fresh sign-in at the privacy cutover.
DELETE FROM auth_sessions;
-- A durable marker also fences rollback if migration-ledger recording fails.
CREATE OR REPLACE FUNCTION investor_identity_access_v1() RETURNS boolean
LANGUAGE sql IMMUTABLE AS $$ SELECT true $$;
COMMIT;
