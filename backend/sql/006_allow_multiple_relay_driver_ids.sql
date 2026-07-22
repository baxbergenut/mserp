BEGIN;

-- Relay may retain multiple external driver identities/card integrations for
-- the same person. Keep every Relay identity unique while allowing all of them
-- to attribute transactions to the same local driver.
ALTER TABLE relay_driver_links
    DROP CONSTRAINT IF EXISTS relay_driver_links_relay_environment_driver_id_key;

DROP INDEX IF EXISTS relay_driver_links_integration_id_idx;
CREATE INDEX relay_driver_links_integration_id_idx
    ON relay_driver_links (relay_environment, relay_integration_id);

COMMIT;
