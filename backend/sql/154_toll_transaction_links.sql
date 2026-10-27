BEGIN;
CREATE TABLE toll_transaction_links (
 id uuid PRIMARY KEY,
 truck_id uuid NOT NULL REFERENCES trucks(id) ON DELETE RESTRICT,
 actor_id uuid REFERENCES app_users(id) ON DELETE SET NULL,
 revision text NOT NULL,
 transactions_before jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
ALTER TABLE tolls ADD COLUMN transaction_link_id uuid REFERENCES toll_transaction_links(id);
CREATE OR REPLACE FUNCTION apply_prepass_truck_mapping() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE matched prepass_truck_mappings%ROWTYPE;
BEGIN
 IF NEW.transaction_link_id IS NOT NULL THEN RETURN NEW; END IF;
 IF TG_OP='UPDATE' THEN
  IF OLD.truck_id IS NOT NULL THEN RETURN NEW; END IF;
 END IF;
 SELECT * INTO matched FROM resolve_prepass_truck_mapping(coalesce(NEW.prepass_environment,'production'),NEW.customer_id,NEW.prepass_tag_id,NEW.equipment_unit,NEW.exit_date);
 IF FOUND THEN NEW.truck_id=matched.truck_id; NEW.truck_mapping_id=matched.id; END IF;
 RETURN NEW;
END $$;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
  ALTER TABLE toll_transaction_links OWNER TO mserp_app;
 END IF;
END $$;
COMMIT;
