BEGIN;

CREATE TABLE relay_driver_links (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    relay_environment    TEXT NOT NULL CHECK (relay_environment IN ('staging', 'production')),
    relay_driver_id      TEXT NOT NULL,
    relay_integration_id TEXT,
    driver_id            UUID NOT NULL REFERENCES drivers(id) ON DELETE CASCADE,
    relay_first_name     TEXT,
    relay_last_name      TEXT,
    relay_phone          TEXT,
    relay_email          TEXT,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (relay_environment, relay_driver_id)
);

CREATE INDEX relay_driver_links_integration_id_idx
    ON relay_driver_links (relay_environment, relay_integration_id);

CREATE TABLE fuel_transactions (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    relay_environment     TEXT NOT NULL CHECK (relay_environment IN ('staging', 'production')),
    relay_transaction_id  TEXT NOT NULL,
    driver_id             UUID NOT NULL REFERENCES drivers(id) ON DELETE RESTRICT,
    relay_driver_id       TEXT NOT NULL,
    relay_integration_id  TEXT,
    purchased_at          TIMESTAMPTZ NOT NULL,
    relay_fuel_code       TEXT,
    fuel_code_type        TEXT,
    total_amount_paid     NUMERIC(12,2) NOT NULL,
    total_retail_price    NUMERIC(12,2) NOT NULL,
    total_amount_saved    NUMERIC(12,2) NOT NULL,
    cash_advance          NUMERIC(12,2),
    is_direct_bill        BOOLEAN NOT NULL,
    currency_code         CHAR(3) NOT NULL,
    merchant_id           TEXT NOT NULL,
    merchant_name         TEXT NOT NULL,
    merchant_number       TEXT NOT NULL,
    location_id           TEXT NOT NULL,
    location_name         TEXT NOT NULL,
    merchant_location_id  TEXT NOT NULL,
    address               TEXT NOT NULL,
    city                  TEXT NOT NULL,
    state                 TEXT NOT NULL,
    postal_code           TEXT NOT NULL,
    latitude              NUMERIC(10,7) NOT NULL,
    longitude             NUMERIC(10,7) NOT NULL,
    timezone              TEXT NOT NULL,
    fuel_policy_id        TEXT,
    fuel_policy_name      TEXT,
    prompts               JSONB NOT NULL DEFAULT '[]'::jsonb,
    raw_payload           JSONB NOT NULL,
    synced_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (relay_environment, relay_transaction_id)
);

CREATE INDEX fuel_transactions_purchased_at_idx
    ON fuel_transactions (purchased_at DESC);
CREATE INDEX fuel_transactions_driver_purchased_at_idx
    ON fuel_transactions (driver_id, purchased_at DESC);
CREATE INDEX fuel_transactions_state_purchased_at_idx
    ON fuel_transactions (state, purchased_at DESC);

CREATE TABLE fuel_transaction_items (
    id                         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    fuel_transaction_id        UUID NOT NULL REFERENCES fuel_transactions(id) ON DELETE CASCADE,
    line_number                INTEGER NOT NULL CHECK (line_number >= 0),
    item_kind                  TEXT NOT NULL CHECK (item_kind IN ('fuel', 'product')),
    category                   TEXT NOT NULL,
    description                TEXT,
    product_code               TEXT,
    quantity                   NUMERIC(12,3),
    unit_of_measure            TEXT,
    retail_price_per_unit      NUMERIC(12,4),
    discounted_price_per_unit  NUMERIC(12,4),
    total_retail_price         NUMERIC(12,2),
    total_amount_paid          NUMERIC(12,2) NOT NULL,
    UNIQUE (fuel_transaction_id, line_number)
);

CREATE INDEX fuel_transaction_items_category_idx
    ON fuel_transaction_items (category);

CREATE TABLE fuel_transaction_fees (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    fuel_transaction_id UUID NOT NULL REFERENCES fuel_transactions(id) ON DELETE CASCADE,
    item_line_number    INTEGER,
    fee_type            TEXT NOT NULL,
    amount              NUMERIC(12,2) NOT NULL
);

CREATE INDEX fuel_transaction_fees_transaction_idx
    ON fuel_transaction_fees (fuel_transaction_id);

CREATE TABLE relay_fuel_sync_days (
    relay_environment TEXT NOT NULL CHECK (relay_environment IN ('staging', 'production')),
    sync_date          DATE NOT NULL,
    transaction_count INTEGER NOT NULL CHECK (transaction_count >= 0),
    fetched_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (relay_environment, sync_date)
);

COMMIT;
