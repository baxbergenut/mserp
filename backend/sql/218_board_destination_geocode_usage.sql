BEGIN;
ALTER TABLE board_route_usage DROP CONSTRAINT board_route_usage_provider_check;
ALTER TABLE board_route_usage ADD CONSTRAINT board_route_usage_provider_check
    CHECK (provider IN ('tomtom','google','tomtom_reverse','tomtom_geocode'));
COMMIT;
