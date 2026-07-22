BEGIN;

CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- Internal users are provisioned directly by an administrator. Passwords are
-- bcrypt hashes; plaintext passwords are never stored by the application.
CREATE TABLE app_users (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    username      TEXT NOT NULL CHECK (username = btrim(username) AND username <> ''),
    password_hash TEXT NOT NULL CHECK (password_hash LIKE '$2%'),
    active        BOOLEAN NOT NULL DEFAULT true,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX app_users_username_idx ON app_users (lower(username));

-- Only a SHA-256 digest of the opaque browser session token is persisted.
CREATE TABLE auth_sessions (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID NOT NULL REFERENCES app_users(id) ON DELETE CASCADE,
    token_hash CHAR(64) NOT NULL UNIQUE,
    csrf_token CHAR(43) NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX auth_sessions_user_id_idx ON auth_sessions (user_id);
CREATE INDEX auth_sessions_expires_at_idx ON auth_sessions (expires_at);

-- Binary assets are kept in Postgres so application records can reference a
-- durable file without depending on a server-local upload directory.
CREATE TABLE files (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    file_name    TEXT NOT NULL,
    content_type TEXT NOT NULL,
    size_bytes   BIGINT NOT NULL CHECK (size_bytes >= 0),
    sha256       CHAR(64) NOT NULL,
    data         BYTEA NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (octet_length(data) = size_bytes)
);

CREATE INDEX files_sha256_idx ON files (sha256);

CREATE TABLE dispatchers (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    full_name       TEXT NOT NULL,
    normalized_name TEXT NOT NULL,
    email           TEXT,
    phone           TEXT,
    pay_percentage  NUMERIC(5,2) CHECK (pay_percentage BETWEEN 0 AND 100),
    active          BOOLEAN NOT NULL DEFAULT true,
    notes           TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX dispatchers_normalized_name_idx ON dispatchers (normalized_name);

CREATE TABLE drivers (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    full_name           TEXT NOT NULL,
    normalized_name     TEXT NOT NULL,
    is_owner_operator   BOOLEAN NOT NULL DEFAULT false,
    pay_type            TEXT NOT NULL CHECK (pay_type IN ('cpm', 'gross_percentage')),
    pay_rate            NUMERIC(10,4) NOT NULL CHECK (pay_rate >= 0),
    phone               TEXT,
    email               TEXT,
    license_number      TEXT,
    license_state       TEXT,
    license_expires     DATE,
    hire_date           DATE,
    address             TEXT,
    city                TEXT,
    state               TEXT,
    postal_code         TEXT,
    emergency_contact   TEXT,
    dispatcher_id       UUID REFERENCES dispatchers(id) ON DELETE SET NULL,
    active              BOOLEAN NOT NULL DEFAULT true,
    notes               TEXT,
    cdl_file_id         UUID REFERENCES files(id) ON DELETE SET NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX drivers_normalized_name_idx ON drivers (normalized_name);
CREATE INDEX drivers_dispatcher_id_idx ON drivers (dispatcher_id);

CREATE TABLE trucks (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    unit_number          TEXT NOT NULL UNIQUE,
    vin                  TEXT UNIQUE,
    year                 INTEGER CHECK (year BETWEEN 1900 AND 2200),
    make                 TEXT,
    model                TEXT,
    license_plate        TEXT,
    license_state        TEXT,
    is_company_owned     BOOLEAN NOT NULL DEFAULT true,
    status               TEXT NOT NULL DEFAULT 'available'
                         CHECK (status IN ('available', 'assigned', 'maintenance', 'out_of_service')),
    mileage              INTEGER CHECK (mileage >= 0),
    registration_expires DATE,
    insurance_expires    DATE,
    last_service_date    DATE,
    next_service_miles   INTEGER CHECK (next_service_miles >= 0),
    active               BOOLEAN NOT NULL DEFAULT true,
    notes                TEXT,
    irp_file_id          UUID REFERENCES files(id) ON DELETE SET NULL,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Assignment rows retain history. Partial unique indexes enforce one current
-- truck per driver and one current driver per truck.
CREATE TABLE truck_driver_assignments (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    truck_id      UUID NOT NULL REFERENCES trucks(id) ON DELETE CASCADE,
    driver_id     UUID NOT NULL REFERENCES drivers(id) ON DELETE CASCADE,
    assigned_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    unassigned_at TIMESTAMPTZ,
    CHECK (unassigned_at IS NULL OR unassigned_at >= assigned_at)
);

CREATE UNIQUE INDEX truck_driver_assignments_current_truck_idx
    ON truck_driver_assignments (truck_id) WHERE unassigned_at IS NULL;
CREATE UNIQUE INDEX truck_driver_assignments_current_driver_idx
    ON truck_driver_assignments (driver_id) WHERE unassigned_at IS NULL;
CREATE INDEX truck_driver_assignments_driver_history_idx
    ON truck_driver_assignments (driver_id, assigned_at DESC);

CREATE TABLE loads (
    id              INTEGER PRIMARY KEY, -- DataTruck API record ID, used for upserts
    load_id         TEXT NOT NULL,        -- business-facing ID; DataTruck may reuse it
    driver_id       UUID REFERENCES drivers(id) ON DELETE SET NULL,
    dispatcher_id   UUID REFERENCES dispatchers(id) ON DELETE SET NULL,
    shipment_id     TEXT,
    status          TEXT NOT NULL,

    load_pay        NUMERIC(10,2) NOT NULL,
    total_other_pay NUMERIC(10,2) DEFAULT 0,
    total_pay       NUMERIC(10,2) NOT NULL,
    total_miles     NUMERIC(10,2),
    per_mile_revenue NUMERIC(10,4),

    dispatcher_name TEXT,
    driver_name     TEXT,
    team_driver_name TEXT,
    truck_unit      TEXT,
    customer_name   TEXT,

    pickup_time     TIMESTAMPTZ,
    delivery_time   TIMESTAMPTZ,
    pickup_appointment_time   TIMESTAMPTZ,
    delivery_appointment_time TIMESTAMPTZ,

    created_datetime TIMESTAMPTZ,
    synced_at        TIMESTAMPTZ DEFAULT now(),
    raw_payload      JSONB
);

CREATE INDEX loads_load_id_idx ON loads (load_id);

-- Relay identities are kept separate from local drivers. The integration ID
-- is Relay's TMS-facing card number; the explicit mapping makes transaction
-- attribution stable even when a local driver's name or contact details change.
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

-- One row is stored per Relay transaction, with reporting dimensions copied
-- from the source payload and the complete payload retained for forward
-- compatibility. Monetary values remain numeric throughout the data layer.
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

-- Fuel, DEF, and non-fuel products share a line-item table so future reports
-- can group every purchase category without schema changes.
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

-- Completed UTC dates are recorded even when Relay returns no transactions.
-- The current UTC date is deliberately never marked complete, so later presses
-- re-check it for transactions created after an earlier same-day sync.
CREATE TABLE relay_fuel_sync_days (
    relay_environment TEXT NOT NULL CHECK (relay_environment IN ('staging', 'production')),
    sync_date         DATE NOT NULL,
    transaction_count INTEGER NOT NULL CHECK (transaction_count >= 0),
    fetched_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (relay_environment, sync_date)
);

-- Every upload is recorded for auditability. Re-uploading a report is allowed:
-- toll-level fingerprints prevent duplicate charges while allowing rows whose
-- truck was initially missing to be picked up on a later attempt.
CREATE TABLE toll_reports (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    file_name           TEXT NOT NULL,
    file_sha256         CHAR(64) NOT NULL,
    row_count           INTEGER NOT NULL CHECK (row_count >= 0),
    imported_count      INTEGER NOT NULL CHECK (imported_count >= 0),
    duplicate_count     INTEGER NOT NULL CHECK (duplicate_count >= 0),
    unmatched_count     INTEGER NOT NULL CHECK (unmatched_count >= 0),
    unmatched_units     JSONB NOT NULL DEFAULT '[]'::jsonb,
    total_amount        NUMERIC(12,2) NOT NULL,
    imported_amount     NUMERIC(12,2) NOT NULL,
    posting_date_start  DATE,
    posting_date_end    DATE,
    imported_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX toll_reports_imported_at_idx ON toll_reports (imported_at DESC);
CREATE INDEX toll_reports_file_sha256_idx ON toll_reports (file_sha256);

CREATE TABLE tolls (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    report_id               UUID NOT NULL REFERENCES toll_reports(id) ON DELETE RESTRICT,
    truck_id                UUID NOT NULL REFERENCES trucks(id) ON DELETE RESTRICT,
    posting_date            DATE NOT NULL,
    invoice_date            DATE NOT NULL,
    customer_id             TEXT NOT NULL,
    source                  TEXT NOT NULL,
    read_type               TEXT NOT NULL,
    prepass_tag_id          TEXT,
    transponder_or_plate    TEXT NOT NULL,
    equipment_unit          TEXT NOT NULL,
    agency                  TEXT NOT NULL,
    entry_plaza             TEXT,
    entry_date              DATE,
    entry_time              TIME,
    exit_plaza              TEXT NOT NULL,
    exit_date               DATE NOT NULL,
    exit_time               TIME NOT NULL,
    toll_class              TEXT NOT NULL,
    miles                   NUMERIC(10,2),
    amount                  NUMERIC(12,2) NOT NULL,
    row_fingerprint         CHAR(64) NOT NULL UNIQUE,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX tolls_posting_date_idx ON tolls (posting_date DESC);
CREATE INDEX tolls_truck_posting_date_idx ON tolls (truck_id, posting_date DESC);
CREATE INDEX tolls_report_id_idx ON tolls (report_id);

COMMIT;
