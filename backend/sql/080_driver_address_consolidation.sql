BEGIN;

-- Keep the original values for audit and application rollback. The current API
-- no longer exposes or writes these legacy columns.
COMMENT ON COLUMN drivers.driver_home IS 'Retired in migration 080. Original address evidence only; use address/city/state/postal_code.';
COMMENT ON COLUMN drivers.driver_home_version IS 'Retired in migration 080; not an application concurrency token.';

CREATE TEMP TABLE driver_address_transfer ON COMMIT DROP AS
WITH parsed AS (
 SELECT id,driver_home,city,state,
   regexp_match(btrim(driver_home), '^([[:alpha:]][[:alpha:] ''’-]*?)[[:space:],.]+([[:alpha:]]{2})$') AS parts,
   ARRAY['AL','AK','AZ','AR','CA','CO','CT','DE','DC','FL','GA','HI','ID','IL','IN','IA','KS','KY','LA','ME','MD','MA','MI','MN','MS','MO','MT','NE','NV','NH','NJ','NM','NY','NC','ND','OH','OK','OR','PA','RI','SC','SD','TN','TX','UT','VT','VA','WA','WV','WI','WY','AB','BC','MB','NB','NL','NS','NT','NU','ON','PE','QC','SK','YT'] AS regions
 FROM drivers WHERE btrim(driver_home)<>''
), candidates AS (
 SELECT *, CASE WHEN upper(parts[2])=ANY(regions) THEN btrim(parts[1]) END AS new_city,
   CASE WHEN upper(parts[2])=ANY(regions) THEN upper(parts[2])
        WHEN upper(btrim(driver_home))=ANY(regions) THEN upper(btrim(driver_home)) END AS new_state
 FROM parsed
)
SELECT *, new_state IS NOT NULL
 AND (nullif(btrim(state),'') IS NULL OR upper(btrim(state))=new_state)
 AND (new_city IS NULL OR nullif(btrim(city),'') IS NULL OR lower(btrim(city))=lower(new_city)) AS transferable
FROM candidates;

UPDATE drivers d SET city=coalesce(nullif(btrim(d.city),''),t.new_city),
 state=coalesce(nullif(btrim(d.state),''),t.new_state),updated_at=now()
FROM driver_address_transfer t WHERE t.id=d.id AND t.transferable
 AND (nullif(btrim(d.state),'') IS NULL OR (t.new_city IS NOT NULL AND nullif(btrim(d.city),'') IS NULL));

-- Preserve ambiguous or conflicting evidence visibly, without inventing a city
-- or replacing a populated mailing address.
INSERT INTO profile_notes(id,driver_id,body,actor_name)
SELECT gen_random_uuid(),id,'Former home entry: '||driver_home||'. Review the city and state on the driver profile.','System'
FROM driver_address_transfer WHERE NOT transferable;

COMMIT;
