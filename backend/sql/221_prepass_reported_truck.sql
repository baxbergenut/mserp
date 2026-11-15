BEGIN;

CREATE OR REPLACE FUNCTION apply_prepass_truck_mapping() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
 reported_truck uuid;
 matched prepass_truck_mappings%ROWTYPE;
BEGIN
 IF NEW.transaction_link_id IS NOT NULL THEN RETURN NEW; END IF;
 IF TG_OP='UPDATE' THEN
  IF OLD.truck_id IS NOT NULL THEN RETURN NEW; END IF;
 END IF;

 -- PrePass reports a vehicle on the toll itself. A dated tag rule is only a
 -- fallback when that reported vehicle cannot identify one Clustie truck.
 IF NEW.prepass_toll_id IS NOT NULL THEN
  reported_truck := resolve_load_truck(NEW.equipment_unit);
  IF reported_truck IS NOT NULL THEN
   NEW.truck_id := reported_truck;
   NEW.truck_mapping_id := NULL;
   RETURN NEW;
  END IF;
 END IF;

 SELECT * INTO matched FROM resolve_prepass_truck_mapping(coalesce(NEW.prepass_environment,'production'),NEW.customer_id,NEW.prepass_tag_id,NEW.equipment_unit,NEW.exit_date);
 IF FOUND THEN NEW.truck_id=matched.truck_id; NEW.truck_mapping_id=matched.id; END IF;
 RETURN NEW;
END $$;

COMMIT;
