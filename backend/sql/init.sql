BEGIN;

CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE schema_migrations (
    version    TEXT PRIMARY KEY,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

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

CREATE TABLE custom_tasks (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    title TEXT NOT NULL CHECK (char_length(title) BETWEEN 1 AND 200 AND title = btrim(title)),
    notes TEXT NOT NULL DEFAULT '' CHECK (char_length(notes) <= 5000),
    completed_at TIMESTAMPTZ,
    created_by UUID REFERENCES app_users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX custom_tasks_status_created_idx ON custom_tasks ((completed_at IS NOT NULL), created_at DESC, id);

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
    color                TEXT CONSTRAINT trucks_color_length CHECK (color IS NULL OR char_length(color) BETWEEN 1 AND 30),
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
    driver_id            UUID REFERENCES drivers(id) ON DELETE CASCADE,
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
    driver_id             UUID REFERENCES drivers(id) ON DELETE RESTRICT,
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

CREATE INDEX relay_driver_links_pending_idx ON relay_driver_links(created_at, id) WHERE driver_id IS NULL;
CREATE INDEX fuel_transactions_relay_identity_idx ON fuel_transactions(relay_environment, relay_driver_id);
CREATE TABLE relay_identity_reviews (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    identity_id UUID NOT NULL REFERENCES relay_driver_links(id) ON DELETE RESTRICT,
    driver_id UUID NOT NULL REFERENCES drivers(id) ON DELETE RESTRICT,
    action TEXT NOT NULL CHECK (action IN ('link', 'reject')),
    reviewed_by UUID REFERENCES app_users(id) ON DELETE SET NULL,
    reviewed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(identity_id, driver_id, action)
);

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

-- Historical CSV uploads remain for auditability after toll ingestion moved
-- to the PrePass API.
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
    report_id               UUID REFERENCES toll_reports(id) ON DELETE RESTRICT,
    truck_id                UUID REFERENCES trucks(id) ON DELETE RESTRICT,
    posting_date            DATE NOT NULL,
    invoice_date            DATE NOT NULL,
    customer_id             TEXT NOT NULL,
    source                  TEXT NOT NULL,
    read_type               TEXT NOT NULL,
    prepass_tag_id          TEXT,
    transponder_or_plate    TEXT NOT NULL,
    equipment_unit          TEXT NOT NULL,
    agency                  TEXT NOT NULL,
    toll_agency_state        TEXT,
    toll_agency_name         TEXT,
    entry_plaza_name         TEXT,
    exit_plaza_name          TEXT,
    location_synced_at       TIMESTAMPTZ,
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
    prepass_environment     TEXT CHECK (
        prepass_environment IS NULL
        OR prepass_environment IN ('nonproduction', 'production')
    ),
    prepass_toll_id         BIGINT,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX tolls_posting_date_idx ON tolls (posting_date DESC);
CREATE INDEX tolls_truck_posting_date_idx ON tolls (truck_id, posting_date DESC);
CREATE INDEX tolls_report_id_idx ON tolls (report_id);
CREATE UNIQUE INDEX tolls_prepass_transaction_idx
    ON tolls (prepass_environment, prepass_toll_id);

-- Completed UTC posting dates are recorded even when PrePass returns no
-- transactions. The current UTC date remains open for same-day additions.
CREATE TABLE prepass_toll_sync_days (
    prepass_environment TEXT NOT NULL
        CHECK (prepass_environment IN ('nonproduction', 'production')),
    sync_date          DATE NOT NULL,
    transaction_count INTEGER NOT NULL CHECK (transaction_count >= 0),
    fetched_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (prepass_environment, sync_date)
);

CREATE TABLE expenses (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    truck_id              UUID REFERENCES trucks(id) ON DELETE SET NULL,
    driver_id             UUID REFERENCES drivers(id) ON DELETE SET NULL,
    company               TEXT NOT NULL,
    category              TEXT NOT NULL CHECK (
        category IN ('Maintenance', 'Other', 'Safety', 'HR', 'Administrative')
    ),
    expense_date          DATE,
    unit_number           TEXT,
    driver_name           TEXT,
    amount                NUMERIC(14,2),
    payment_type          TEXT,
    expense_type          TEXT,
    reference_number      TEXT,
    description           TEXT,
    covered_by            TEXT,
    paid_by               TEXT,
    manager_verified      BOOLEAN NOT NULL DEFAULT false,
    accounting_verified   BOOLEAN NOT NULL DEFAULT false,
    source_spreadsheet_id TEXT,
    source_sheet          TEXT,
    source_row            INTEGER CHECK (source_row IS NULL OR source_row >= 2),
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (
        (source_spreadsheet_id IS NULL AND source_sheet IS NULL AND source_row IS NULL)
        OR
        (source_spreadsheet_id IS NOT NULL AND source_sheet IS NOT NULL AND source_row IS NOT NULL)
    )
);

CREATE INDEX expenses_date_idx ON expenses (expense_date DESC);
CREATE INDEX expenses_truck_date_idx ON expenses (truck_id, expense_date DESC);
CREATE INDEX expenses_driver_date_idx ON expenses (driver_id, expense_date DESC);
CREATE INDEX expenses_category_date_idx ON expenses (category, expense_date DESC);
CREATE INDEX expenses_company_date_idx ON expenses (company, expense_date DESC);
CREATE UNIQUE INDEX expenses_source_row_idx
    ON expenses (source_spreadsheet_id, source_sheet, source_row)
    WHERE source_spreadsheet_id IS NOT NULL;

CREATE TABLE gross_board_entries (
    driver_id UUID NOT NULL REFERENCES drivers(id) ON DELETE CASCADE,
    service_date DATE NOT NULL,
    load_number TEXT NOT NULL DEFAULT '' CHECK (length(load_number) <= 200),
    day_status TEXT NOT NULL DEFAULT '' CHECK (day_status IN ('', 'SHOP', 'HOME', 'RESET', 'IN TRANSIT', 'REJECTED', 'LOAD CANCELLED', 'NO LOAD', 'STUCK', 'LATE DEL', 'TRUCK ISSUE', 'LEFT', 'NEW DRIVER', 'DEADHEAD')),
    load_record_id INTEGER REFERENCES loads(id) ON DELETE SET NULL,
    original_rate NUMERIC(12,2),
    entered_original_rate NUMERIC(12,2),
    entered_miles NUMERIC(12,2) CHECK (entered_miles >= 0),
    driver_rate NUMERIC(12,2),
    miles NUMERIC(12,2) CHECK (miles >= 0),
    version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (driver_id, service_date),
    CHECK (day_status = '' OR (load_number = '' AND load_record_id IS NULL
      AND original_rate IS NULL AND driver_rate IS NULL AND miles IS NULL
      AND entered_original_rate IS NULL AND entered_miles IS NULL))
);
CREATE INDEX gross_board_entries_date_idx ON gross_board_entries(service_date);
CREATE INDEX loads_gross_board_number_idx ON loads(lower(btrim(load_id)));

-- New hires are staged separately until accounting confirms identity and pay.
CREATE TABLE fleetscope_driver_intake (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id UUID NOT NULL,
    fleetscope_driver_id UUID NOT NULL,
    driver_data JSONB NOT NULL,
    normalized_name TEXT NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL,
    received_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ,
    completed_by UUID REFERENCES app_users(id) ON DELETE SET NULL,
    driver_id UUID REFERENCES drivers(id) ON DELETE SET NULL,
    UNIQUE(company_id, fleetscope_driver_id)
);
CREATE INDEX fleetscope_driver_intake_pending_idx
    ON fleetscope_driver_intake(received_at, id) WHERE completed_at IS NULL;

CREATE TABLE fleetscope_webhook_receipts (
    event_id UUID PRIMARY KEY,
    body_sha256 TEXT NOT NULL,
    intake_id UUID NOT NULL REFERENCES fleetscope_driver_intake(id),
    received_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'mserp_app') THEN
        ALTER TABLE fleetscope_driver_intake OWNER TO mserp_app;
        ALTER TABLE fleetscope_webhook_receipts OWNER TO mserp_app;
        ALTER TABLE relay_identity_reviews OWNER TO mserp_app;
        ALTER TABLE custom_tasks OWNER TO mserp_app;
    END IF;
END $$;


-- Keep the original one-entry table readable/writable by the previous release.
-- Additional slots have independent versions and retain tombstones on removal.
CREATE TABLE gross_board_extra_entries (
    driver_id UUID NOT NULL REFERENCES drivers(id) ON DELETE CASCADE,
    service_date DATE NOT NULL,
    slot INTEGER NOT NULL CHECK (slot BETWEEN 1 AND 99),
    load_number TEXT NOT NULL DEFAULT '' CHECK (length(load_number) <= 200),
    load_record_id INTEGER REFERENCES loads(id) ON DELETE SET NULL,
    original_rate NUMERIC(12,2),
    driver_rate NUMERIC(12,2),
    miles NUMERIC(12,2) CHECK (miles >= 0),
    entered_original_rate NUMERIC(12,2),
    entered_miles NUMERIC(12,2) CHECK (entered_miles >= 0),
    day_status TEXT NOT NULL DEFAULT '' CHECK (day_status IN ('', 'SHOP', 'HOME', 'RESET', 'IN TRANSIT', 'REJECTED', 'LOAD CANCELLED', 'NO LOAD', 'STUCK', 'LATE DEL', 'TRUCK ISSUE', 'LEFT', 'NEW DRIVER', 'DEADHEAD')),
    deleted BOOLEAN NOT NULL DEFAULT false,
    version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (driver_id, service_date, slot)
);
CREATE INDEX gross_board_extra_entries_date_idx ON gross_board_extra_entries(service_date);

CREATE TABLE driver_pay_weeks (
    driver_id UUID NOT NULL REFERENCES drivers(id) ON DELETE CASCADE,
    week_start DATE NOT NULL CHECK (extract(isodow FROM week_start) = 1),
    notes TEXT NOT NULL DEFAULT '' CHECK (char_length(notes) <= 5000),
    comments JSONB NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(comments) = 'object'),
    adjustments JSONB NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(adjustments) = 'array'),
    version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (driver_id, week_start)
);

DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'mserp_app') THEN
        ALTER TABLE gross_board_extra_entries OWNER TO mserp_app;
        ALTER TABLE driver_pay_weeks OWNER TO mserp_app;
    END IF;
END $$;


-- NULL follows live weekly costs; zero and signed amounts are explicit overrides.
-- Additive: the previous binary preserves these columns when saving other edits.
ALTER TABLE driver_pay_weeks
    ADD COLUMN fuel_override NUMERIC(12,2),
    ADD COLUMN toll_override NUMERIC(12,2);

ALTER TABLE truck_driver_assignments ADD COLUMN source TEXT NOT NULL DEFAULT '';

CREATE TABLE driver_dispatcher_assignments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    driver_id UUID NOT NULL REFERENCES drivers(id) ON DELETE CASCADE,
    dispatcher_id UUID REFERENCES dispatchers(id) ON DELETE SET NULL,
    dispatcher_name TEXT NOT NULL,
    assigned_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    unassigned_at TIMESTAMPTZ,
    start_known BOOLEAN NOT NULL DEFAULT true,
    source TEXT NOT NULL DEFAULT '',
    CHECK (unassigned_at IS NULL OR unassigned_at >= assigned_at)
);
CREATE UNIQUE INDEX driver_dispatcher_assignments_current_idx
    ON driver_dispatcher_assignments(driver_id) WHERE unassigned_at IS NULL;
CREATE INDEX driver_dispatcher_assignments_history_idx
    ON driver_dispatcher_assignments(driver_id, assigned_at DESC);

-- Existing links have an observation date, not a known historical start date.
LOCK TABLE drivers IN SHARE ROW EXCLUSIVE MODE;
INSERT INTO driver_dispatcher_assignments(driver_id,dispatcher_id,dispatcher_name,start_known,source)
SELECT d.id,d.dispatcher_id,coalesce(dp.full_name,'Unassigned'),false,'Existing assignment when history tracking started'
FROM drivers d LEFT JOIN dispatchers dp ON dp.id=d.dispatcher_id;

CREATE FUNCTION record_driver_dispatcher_assignment() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE changed_at timestamptz := clock_timestamp();
BEGIN
    IF TG_OP='UPDATE' AND NEW.dispatcher_id IS NOT DISTINCT FROM OLD.dispatcher_id THEN
        RETURN NEW;
    END IF;
    UPDATE driver_dispatcher_assignments SET unassigned_at=changed_at
    WHERE driver_id=NEW.id AND unassigned_at IS NULL;
    INSERT INTO driver_dispatcher_assignments(driver_id,dispatcher_id,dispatcher_name,assigned_at,source)
    VALUES(NEW.id,NEW.dispatcher_id,
        coalesce((SELECT full_name FROM dispatchers WHERE id=NEW.dispatcher_id),'Unassigned'),
        changed_at,coalesce(current_setting('mserp.assignment_source',true),''));
    RETURN NEW;
END $$;
CREATE TRIGGER drivers_dispatcher_history
AFTER INSERT OR UPDATE OF dispatcher_id ON drivers
FOR EACH ROW EXECUTE FUNCTION record_driver_dispatcher_assignment();

DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
        ALTER TABLE driver_dispatcher_assignments OWNER TO mserp_app;
    END IF;
END $$;


CREATE TABLE investors (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    full_name TEXT NOT NULL CHECK (length(trim(full_name)) BETWEEN 1 AND 200),
    driver_id UUID UNIQUE REFERENCES drivers(id) ON DELETE SET NULL,
    is_company BOOLEAN NOT NULL DEFAULT false,
    email TEXT,
    phone TEXT,
    notes TEXT,
    active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (NOT is_company OR (driver_id IS NULL AND active))
);
CREATE UNIQUE INDEX investors_company_idx ON investors(is_company) WHERE is_company;
INSERT INTO investors(id,full_name,is_company)
VALUES ('00000000-0000-0000-0000-000000000001','MS Express Inc.',true);

-- This is a one-time ownership initialization, not a rule tying ownership to operation.
LOCK TABLE trucks, truck_driver_assignments, drivers IN SHARE ROW EXCLUSIVE MODE;
INSERT INTO investors(full_name,driver_id,email,phone)
SELECT DISTINCT d.full_name,d.id,d.email,d.phone FROM drivers d
JOIN truck_driver_assignments a ON a.driver_id=d.id AND a.unassigned_at IS NULL
WHERE d.is_owner_operator;
ALTER TABLE trucks ADD COLUMN owner_id UUID NOT NULL
    DEFAULT '00000000-0000-0000-0000-000000000001' REFERENCES investors(id);
CREATE INDEX trucks_owner_idx ON trucks(owner_id);
UPDATE trucks t SET owner_id=i.id FROM truck_driver_assignments a
JOIN investors i ON i.driver_id=a.driver_id
WHERE a.truck_id=t.id AND a.unassigned_at IS NULL;
UPDATE trucks SET is_company_owned=(owner_id='00000000-0000-0000-0000-000000000001');

CREATE TABLE truck_ownership_history (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    truck_id UUID REFERENCES trucks(id) ON DELETE SET NULL,
    truck_unit TEXT NOT NULL,
    owner_id UUID NOT NULL REFERENCES investors(id),
    assigned_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    unassigned_at TIMESTAMPTZ,
    start_known BOOLEAN NOT NULL DEFAULT true,
    CHECK (unassigned_at IS NULL OR unassigned_at >= assigned_at)
);
CREATE UNIQUE INDEX truck_ownership_current_idx ON truck_ownership_history(truck_id)
    WHERE unassigned_at IS NULL;
CREATE INDEX truck_ownership_owner_idx ON truck_ownership_history(owner_id,assigned_at);
INSERT INTO truck_ownership_history(truck_id,truck_unit,owner_id,start_known)
SELECT id,unit_number,owner_id,false FROM trucks;

CREATE FUNCTION record_truck_ownership() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE changed_at timestamptz := clock_timestamp();
BEGIN
    IF TG_OP='DELETE' THEN
        UPDATE truck_ownership_history SET unassigned_at=changed_at
        WHERE truck_id=OLD.id AND unassigned_at IS NULL;
        RETURN OLD;
    END IF;
    -- Keep the legacy flag consistent even for older clients.
    NEW.is_company_owned := (NEW.owner_id='00000000-0000-0000-0000-000000000001');
    IF TG_OP='UPDATE' AND NEW.owner_id IS NOT DISTINCT FROM OLD.owner_id THEN
        RETURN NEW;
    END IF;
    UPDATE truck_ownership_history SET unassigned_at=changed_at
    WHERE truck_id=NEW.id AND unassigned_at IS NULL;
    INSERT INTO truck_ownership_history(truck_id,truck_unit,owner_id,assigned_at)
    VALUES(NEW.id,NEW.unit_number,NEW.owner_id,changed_at);
    RETURN NEW;
END $$;
-- Deferred FK permits history to be recorded in the BEFORE INSERT trigger.
ALTER TABLE truck_ownership_history ALTER CONSTRAINT truck_ownership_history_truck_id_fkey
    DEFERRABLE INITIALLY DEFERRED;
CREATE TRIGGER trucks_ownership BEFORE INSERT OR UPDATE OR DELETE ON trucks
FOR EACH ROW EXECUTE FUNCTION record_truck_ownership();

-- Preserve the most recent identity if the driver profile is later removed.
CREATE FUNCTION sync_investor_driver_identity() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    UPDATE investors SET full_name=NEW.full_name,email=NEW.email,phone=NEW.phone,updated_at=now()
    WHERE driver_id=NEW.id;
    RETURN NEW;
END $$;
CREATE TRIGGER investor_driver_identity AFTER UPDATE OF full_name,email,phone ON drivers
FOR EACH ROW EXECUTE FUNCTION sync_investor_driver_identity();

DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
        ALTER TABLE investors OWNER TO mserp_app;
        ALTER TABLE truck_ownership_history OWNER TO mserp_app;
    END IF;
END $$;
COMMIT;
BEGIN;

CREATE TABLE driver_charge_types (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 200),
    direction TEXT NOT NULL CHECK (direction IN ('charge','reimbursement')),
    amount NUMERIC(12,2) NOT NULL CHECK (amount > 0),
    archived BOOLEAN NOT NULL DEFAULT false,
    version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0)
);
CREATE TABLE driver_charge_schedules (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    driver_id UUID NOT NULL REFERENCES drivers(id) ON DELETE RESTRICT,
    type_id UUID REFERENCES driver_charge_types(id) ON DELETE RESTRICT,
    kind TEXT NOT NULL CHECK (kind IN ('recurring','installment')),
    name TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 200),
    direction TEXT NOT NULL CHECK (direction IN ('charge','reimbursement')),
    start_week DATE NOT NULL CHECK (extract(isodow FROM start_week)=1),
    end_week DATE CHECK (extract(isodow FROM end_week)=1 AND end_week >= start_week-7),
    eligibility TEXT NOT NULL CHECK (eligibility IN ('calendar','loads')),
    total NUMERIC(12,2) CHECK (total > 0),
    installment_count INTEGER NOT NULL DEFAULT 0 CHECK (installment_count BETWEEN 0 AND 5200),
    version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
    CHECK ((kind='recurring' AND type_id IS NOT NULL AND total IS NULL) OR
           (kind='installment' AND type_id IS NULL AND total IS NOT NULL AND direction='charge'))
);
CREATE INDEX driver_charge_schedules_driver_idx ON driver_charge_schedules(driver_id);
CREATE TABLE driver_charge_phases (
    schedule_id UUID NOT NULL REFERENCES driver_charge_schedules(id) ON DELETE RESTRICT,
    week_start DATE NOT NULL CHECK (extract(isodow FROM week_start)=1),
    amount NUMERIC(12,2) NOT NULL CHECK (amount > 0),
    paused BOOLEAN NOT NULL DEFAULT false,
    PRIMARY KEY(schedule_id,week_start)
);
CREATE TABLE driver_charge_occurrences (
    schedule_id UUID NOT NULL REFERENCES driver_charge_schedules(id) ON DELETE RESTRICT,
    week_start DATE NOT NULL CHECK (extract(isodow FROM week_start)=1),
    name TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 200),
    scheduled_amount NUMERIC(12,2) NOT NULL,
    amount NUMERIC(12,2) NOT NULL,
    overridden BOOLEAN NOT NULL DEFAULT false,
    confirmed_at TIMESTAMPTZ,
    confirmed_by UUID REFERENCES app_users(id) ON DELETE SET NULL,
    version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
    PRIMARY KEY(schedule_id,week_start)
);
CREATE TABLE driver_charge_events (
    id BIGSERIAL PRIMARY KEY,
    schedule_id UUID REFERENCES driver_charge_schedules(id) ON DELETE RESTRICT,
    type_id UUID REFERENCES driver_charge_types(id) ON DELETE RESTRICT,
    actor_id UUID REFERENCES app_users(id) ON DELETE SET NULL,
    action TEXT NOT NULL,
    details JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX driver_charge_events_schedule_idx ON driver_charge_events(schedule_id,id);
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
        ALTER TABLE driver_charge_types OWNER TO mserp_app;
        ALTER TABLE driver_charge_schedules OWNER TO mserp_app;
        ALTER TABLE driver_charge_phases OWNER TO mserp_app;
        ALTER TABLE driver_charge_occurrences OWNER TO mserp_app;
        ALTER TABLE driver_charge_events OWNER TO mserp_app;
    END IF;
END $$;
COMMIT;

BEGIN;

ALTER TABLE driver_charge_types ADD COLUMN amounts NUMERIC(12,2)[];
ALTER TABLE driver_charge_types ADD COLUMN eligibility TEXT NOT NULL DEFAULT 'calendar'
    CHECK (eligibility IN ('calendar','loads','no_loads'));
-- Retain every previously assigned amount as an available choice.
UPDATE driver_charge_types t SET amounts = ARRAY[t.amount] || ARRAY(
    SELECT DISTINCT p.amount FROM driver_charge_phases p
    JOIN driver_charge_schedules s ON s.id=p.schedule_id
    WHERE s.type_id=t.id AND p.amount<>t.amount ORDER BY p.amount
), eligibility = CASE WHEN EXISTS(SELECT 1 FROM driver_charge_schedules s WHERE s.type_id=t.id)
    AND NOT EXISTS(SELECT 1 FROM driver_charge_schedules s WHERE s.type_id=t.id AND s.eligibility<>'loads')
    THEN 'loads' ELSE 'calendar' END;
ALTER TABLE driver_charge_types ALTER COLUMN amounts SET NOT NULL;
ALTER TABLE driver_charge_types ADD CHECK (cardinality(amounts)>0 AND 0 < ALL(amounts));

-- Rules take effect from a Monday; older weeks retain their original rules.
CREATE TABLE driver_charge_type_rules (
    type_id UUID NOT NULL REFERENCES driver_charge_types(id) ON DELETE RESTRICT,
    week_start DATE NOT NULL CHECK (extract(isodow FROM week_start)=1),
    eligibility TEXT NOT NULL CHECK (eligibility IN ('calendar','loads','no_loads')),
    PRIMARY KEY(type_id,week_start)
);
INSERT INTO driver_charge_type_rules(type_id,week_start,eligibility)
SELECT id,date_trunc('week',now() AT TIME ZONE 'America/New_York')::date,eligibility FROM driver_charge_types;
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
        ALTER TABLE driver_charge_type_rules OWNER TO mserp_app;
    END IF;
END $$;
COMMIT;

BEGIN;
-- Backdated recurring assignments keep the type's original eligibility before
-- its first dated rule. Installment eligibility remains calendar/load weeks.
ALTER TABLE driver_charge_schedules DROP CONSTRAINT driver_charge_schedules_eligibility_check;
ALTER TABLE driver_charge_schedules ADD CONSTRAINT driver_charge_schedules_eligibility_check
    CHECK (eligibility IN ('calendar','loads') OR (kind='recurring' AND eligibility='no_loads'));
COMMIT;

BEGIN;

ALTER TABLE expenses DROP CONSTRAINT expenses_category_check;
ALTER TABLE expenses ADD CONSTRAINT expenses_category_check CHECK
    (category IN ('Maintenance', 'Other', 'Safety', 'HR', 'Administrative', 'Penalties'));
ALTER TABLE expenses ADD COLUMN driver_settled BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE expenses ADD COLUMN balance_version INTEGER NOT NULL DEFAULT 1;
-- One-time opening balance: existing driver expenses have already been paid.
UPDATE expenses SET driver_settled = true WHERE lower(btrim(covered_by)) = 'driver';

CREATE TABLE expense_payments (
    expense_id UUID NOT NULL REFERENCES expenses(id) ON DELETE RESTRICT,
    week_start DATE NOT NULL CHECK (extract(isodow FROM week_start) = 1),
    amount NUMERIC(14,2) NOT NULL CHECK (amount >= 0),
    updated_by UUID REFERENCES app_users(id) ON DELETE SET NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (expense_id, week_start)
);
CREATE INDEX expense_payments_week_idx ON expense_payments(week_start);

-- Preserve the expense identity and payroll history, including from older clients.
CREATE FUNCTION protect_expense_balance() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'UPDATE' THEN
        NEW.balance_version := OLD.balance_version + 1;
        IF EXISTS (SELECT 1 FROM expense_payments WHERE expense_id = OLD.id)
           AND (NEW.driver_id IS DISTINCT FROM OLD.driver_id
             OR NEW.expense_date IS DISTINCT FROM OLD.expense_date
             OR NEW.amount IS DISTINCT FROM OLD.amount
             OR lower(btrim(NEW.covered_by)) IS DISTINCT FROM lower(btrim(OLD.covered_by))) THEN
            RAISE EXCEPTION 'An expense used in Driver Pay cannot change its driver, date, amount or responsibility' USING ERRCODE = '23514';
        END IF;
        IF OLD.driver_settled AND (NEW.amount IS DISTINCT FROM OLD.amount
             OR lower(btrim(NEW.covered_by)) IS DISTINCT FROM lower(btrim(OLD.covered_by))) THEN
            RAISE EXCEPTION 'Previously paid driver expenses retain their original amount and responsibility; create a new expense' USING ERRCODE = '23514';
        END IF;
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER expenses_balance_guard BEFORE UPDATE ON expenses
FOR EACH ROW EXECUTE FUNCTION protect_expense_balance();

DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'mserp_app') THEN
        ALTER TABLE expense_payments OWNER TO mserp_app;
        GRANT SELECT, INSERT, UPDATE, DELETE ON expenses TO mserp_app;
    END IF;
END $$;
COMMIT;

BEGIN;
ALTER TABLE expenses ADD COLUMN owner_id UUID REFERENCES investors(id);
ALTER TABLE expenses ADD COLUMN charge_driver_id UUID REFERENCES drivers(id) ON DELETE SET NULL;
UPDATE expenses SET charge_driver_id=driver_id WHERE lower(btrim(covered_by))='driver';
-- Historical owner expenses have no reliable ownership snapshot. Do not infer
-- their debtor from today's assignment or introduce old payroll deductions.
UPDATE expenses SET driver_settled=true WHERE lower(btrim(covered_by))='truck owner';
CREATE INDEX expenses_charge_driver_idx ON expenses(charge_driver_id,expense_date);

CREATE FUNCTION resolve_expense_responsibility() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='UPDATE' THEN
        IF NEW.owner_id IS NOT DISTINCT FROM OLD.owner_id
           AND NEW.driver_id IS NOT DISTINCT FROM OLD.driver_id
           AND NEW.covered_by IS NOT DISTINCT FROM OLD.covered_by THEN
            RETURN NEW;
        END IF;
        IF EXISTS(SELECT 1 FROM expense_payments WHERE expense_id=OLD.id)
           AND NEW.owner_id IS DISTINCT FROM OLD.owner_id THEN
            RAISE EXCEPTION 'An expense used in Driver Pay cannot change its owner' USING ERRCODE='23514';
        END IF;
    END IF;
    NEW.charge_driver_id := NULL;
    IF lower(btrim(NEW.covered_by))='driver' THEN
        NEW.owner_id := NULL;
        NEW.charge_driver_id := NEW.driver_id;
    ELSIF lower(btrim(NEW.covered_by))='truck owner' THEN
        IF NEW.owner_id IS NULL THEN
            RAISE EXCEPTION 'Select the responsible truck owner' USING ERRCODE='23514';
        END IF;
        SELECT driver_id INTO NEW.charge_driver_id FROM investors WHERE id=NEW.owner_id AND NOT is_company;
        IF NEW.owner_id='00000000-0000-0000-0000-000000000001' THEN
            NEW.covered_by := 'Company';
            NEW.owner_id := NULL;
        END IF;
    ELSE
        NEW.owner_id := NULL;
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER expenses_00_responsibility BEFORE INSERT OR UPDATE ON expenses
FOR EACH ROW EXECUTE FUNCTION resolve_expense_responsibility();
COMMIT;

BEGIN;
CREATE TABLE payroll_settlements (
    driver_id UUID NOT NULL REFERENCES drivers(id) ON DELETE RESTRICT,
    week_start DATE NOT NULL CHECK (extract(isodow FROM week_start)=1),
    version INTEGER NOT NULL DEFAULT 1,
    finalized BOOLEAN NOT NULL DEFAULT true,
    report JSONB NOT NULL,
    confirmed_schedules UUID[] NOT NULL DEFAULT '{}',
    finalized_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    finalized_by UUID REFERENCES app_users(id) ON DELETE SET NULL,
    reopened_at TIMESTAMPTZ,
    reopened_by UUID REFERENCES app_users(id) ON DELETE SET NULL,
    reason TEXT NOT NULL DEFAULT '',
    PRIMARY KEY(driver_id,week_start)
);
CREATE TABLE payroll_settlement_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    driver_id UUID NOT NULL REFERENCES drivers(id) ON DELETE RESTRICT,
    week_start DATE NOT NULL,
    version INTEGER NOT NULL,
    action TEXT NOT NULL CHECK(action IN ('finalized','reopened')),
    actor_id UUID REFERENCES app_users(id) ON DELETE SET NULL,
    reason TEXT NOT NULL DEFAULT '',
    report JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX payroll_settlement_events_driver_week_idx ON payroll_settlement_events(driver_id,week_start,created_at);

-- Frozen settlements cannot be changed through old API versions either.
CREATE FUNCTION protect_finalized_payroll() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE d uuid; w date;
BEGIN
    IF TG_TABLE_NAME='driver_pay_weeks' THEN
        IF TG_OP='DELETE' THEN d:=OLD.driver_id;w:=OLD.week_start; ELSE d:=NEW.driver_id;w:=NEW.week_start; END IF;
    ELSIF TG_TABLE_NAME='expense_payments' THEN
        IF TG_OP='DELETE' THEN
            SELECT charge_driver_id INTO d FROM expenses WHERE id=OLD.expense_id;w:=OLD.week_start;
        ELSE SELECT charge_driver_id INTO d FROM expenses WHERE id=NEW.expense_id;w:=NEW.week_start; END IF;
    ELSE
        IF TG_OP='DELETE' THEN
            SELECT driver_id INTO d FROM driver_charge_schedules WHERE id=OLD.schedule_id;w:=OLD.week_start;
        ELSE SELECT driver_id INTO d FROM driver_charge_schedules WHERE id=NEW.schedule_id;w:=NEW.week_start; END IF;
        -- Schedule maintenance may re-store an unchanged frozen occurrence.
        IF TG_OP='UPDATE' AND NEW.amount=OLD.amount AND NEW.name=OLD.name
           AND NEW.overridden=OLD.overridden AND NEW.confirmed_at IS NOT DISTINCT FROM OLD.confirmed_at THEN RETURN NEW; END IF;
    END IF;
    IF EXISTS(SELECT 1 FROM payroll_settlements WHERE driver_id=d AND week_start=w AND finalized) THEN
        RAISE EXCEPTION 'Reopen this driver settlement before editing payroll' USING ERRCODE='23514';
    END IF;
    IF TG_OP='DELETE' THEN RETURN OLD; END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER payroll_week_guard BEFORE INSERT OR UPDATE OR DELETE ON driver_pay_weeks FOR EACH ROW EXECUTE FUNCTION protect_finalized_payroll();
CREATE TRIGGER payroll_expense_guard BEFORE INSERT OR UPDATE OR DELETE ON expense_payments FOR EACH ROW EXECUTE FUNCTION protect_finalized_payroll();
CREATE TRIGGER payroll_charge_guard BEFORE INSERT OR UPDATE OR DELETE ON driver_charge_occurrences FOR EACH ROW EXECUTE FUNCTION protect_finalized_payroll();
DO $$ BEGIN
    IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
        ALTER TABLE payroll_settlements OWNER TO mserp_app;
        ALTER TABLE payroll_settlement_events OWNER TO mserp_app;
    END IF;
END $$;
COMMIT;

BEGIN;

CREATE TABLE expense_settings (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    kind TEXT NOT NULL CHECK (kind IN ('category','name','payment_method','payer')),
    category_id UUID REFERENCES expense_settings(id) ON DELETE RESTRICT,
    name TEXT NOT NULL CHECK (name = btrim(name) AND length(name) BETWEEN 1 AND 100),
    active BOOLEAN NOT NULL DEFAULT true,
    version INTEGER NOT NULL DEFAULT 1,
    CHECK ((kind = 'name') = (category_id IS NOT NULL))
);
CREATE UNIQUE INDEX expense_settings_unique_name ON expense_settings
    (kind, coalesce(category_id, '00000000-0000-0000-0000-000000000000'::uuid), lower(name));

INSERT INTO expense_settings(kind,name) VALUES
    ('category','Maintenance'),('category','Penalties'),('category','Other'),
    ('category','Safety'),('category','HR'),('category','Administrative');
INSERT INTO expense_settings(kind,category_id,name)
SELECT 'name',c.id,n.name FROM (VALUES
    ('Maintenance','Tire replacement'),('Maintenance','Electrical repair'),('Maintenance','Oil change'),
    ('Penalties','Parking violation'),('Penalties','Speeding ticket'),('Penalties','Late delivery penalty'),
    ('Safety','Inspection'),('Safety','Permit'),('Safety','Drug test'),
    ('HR','Recruiting'),('HR','Driver lodging'),
    ('Administrative','Software subscription'),('Administrative','Office supplies'),('Administrative','Bank fee')
) n(category,name) JOIN expense_settings c ON c.kind='category' AND c.name=n.category;
INSERT INTO expense_settings(kind,name)
SELECT 'payment_method',name FROM (SELECT unnest(ARRAY['Cash','ACH','Check','Card']) AS name
    UNION SELECT btrim(payment_type) FROM expenses WHERE nullif(btrim(payment_type),'') IS NOT NULL) v
WHERE length(name)<=100 ON CONFLICT DO NOTHING;
INSERT INTO expense_settings(kind,name)
SELECT 'payer',btrim(paid_by) FROM expenses WHERE length(btrim(paid_by)) BETWEEN 1 AND 100
ON CONFLICT DO NOTHING;

CREATE FUNCTION validate_expense_setting() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='UPDATE' AND NEW.kind<>OLD.kind THEN
        RAISE EXCEPTION 'Setting kind cannot change' USING ERRCODE='23514';
    END IF;
    IF NEW.kind='name' THEN
        PERFORM 1 FROM expense_settings WHERE id=NEW.category_id AND kind='category'
            AND (active OR NOT NEW.active) FOR SHARE;
        IF NOT FOUND THEN
            RAISE EXCEPTION 'Select an active expense category' USING ERRCODE='23514';
        END IF;
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER expense_setting_guard BEFORE INSERT OR UPDATE ON expense_settings
    FOR EACH ROW EXECUTE FUNCTION validate_expense_setting();

ALTER TABLE expenses DROP CONSTRAINT expenses_category_check;
ALTER TABLE expenses ADD CONSTRAINT expenses_category_check CHECK (length(btrim(category)) BETWEEN 1 AND 100);
CREATE FUNCTION validate_expense_category() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    -- Existing records keep their original category even after a rename/archive.
    IF TG_OP='UPDATE' AND NEW.category=OLD.category THEN RETURN NEW; END IF;
    PERFORM 1 FROM expense_settings WHERE kind='category' AND name=NEW.category AND active FOR SHARE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'Select an active expense category' USING ERRCODE='23514', CONSTRAINT='expense_active_category';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER expense_category_guard BEFORE INSERT OR UPDATE OF category ON expenses
    FOR EACH ROW EXECUTE FUNCTION validate_expense_category();

DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
        ALTER TABLE expense_settings OWNER TO mserp_app;
    END IF;
END $$;
COMMIT;

BEGIN;

-- Terms are explicit, effective-dated agreements. No rate or historical owner
-- is inferred from a driver's current profile or a current truck assignment.
CREATE TABLE truck_settlement_terms (
    truck_id UUID NOT NULL REFERENCES trucks(id) ON DELETE RESTRICT,
    week_start DATE NOT NULL CHECK (extract(isodow FROM week_start)=1),
    owner_id UUID NOT NULL REFERENCES investors(id) ON DELETE RESTRICT,
    share_percent NUMERIC(7,4) NOT NULL CHECK (share_percent>0 AND share_percent<=100),
    version INTEGER NOT NULL DEFAULT 1,
    updated_by UUID REFERENCES app_users(id),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY(truck_id,week_start)
);
CREATE TABLE truck_charge_phases (
    truck_id UUID NOT NULL REFERENCES trucks(id) ON DELETE RESTRICT,
    type_id UUID NOT NULL REFERENCES driver_charge_types(id) ON DELETE RESTRICT,
    week_start DATE NOT NULL CHECK (extract(isodow FROM week_start)=1),
    amount NUMERIC(14,2) NOT NULL CHECK(amount>=0),
    included BOOLEAN NOT NULL DEFAULT true,
    version INTEGER NOT NULL DEFAULT 1,
    updated_by UUID REFERENCES app_users(id),
    PRIMARY KEY(truck_id,type_id,week_start)
);
CREATE TABLE investor_pay_weeks (
    truck_id UUID NOT NULL REFERENCES trucks(id) ON DELETE RESTRICT,
    week_start DATE NOT NULL CHECK(extract(isodow FROM week_start)=1),
    owner_id UUID NOT NULL REFERENCES investors(id) ON DELETE RESTRICT,
    edits JSONB NOT NULL,
    version INTEGER NOT NULL DEFAULT 1,
    finalized BOOLEAN NOT NULL DEFAULT false,
    report JSONB,
    finalized_at TIMESTAMPTZ,
    finalized_by UUID REFERENCES app_users(id),
    PRIMARY KEY(truck_id,week_start)
);
CREATE TABLE truck_settlement_events (
    id BIGSERIAL PRIMARY KEY,
    truck_id UUID NOT NULL REFERENCES trucks(id) ON DELETE RESTRICT,
    week_start DATE NOT NULL,
    action TEXT NOT NULL,
    details JSONB NOT NULL,
    actor_id UUID REFERENCES app_users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- A payment belongs to only one settlement. Existing driver payments keep
-- their original identity and continue to reserve the expense principal.
ALTER TABLE expense_payments ADD COLUMN investor_truck_id UUID REFERENCES trucks(id) ON DELETE RESTRICT;

CREATE FUNCTION protect_investor_expense_payment() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE tid uuid; wk date;
BEGIN
    IF TG_OP='DELETE' THEN tid:=OLD.investor_truck_id;wk:=OLD.week_start;
    ELSE tid:=NEW.investor_truck_id;wk:=NEW.week_start; END IF;
    IF TG_OP='INSERT' AND NEW.investor_truck_id IS NULL AND EXISTS(SELECT 1 FROM expense_payments WHERE expense_id=NEW.expense_id AND week_start=NEW.week_start AND investor_truck_id IS NOT NULL) THEN
        RAISE EXCEPTION 'This expense payment belongs to Investor Pay' USING ERRCODE='23514';
    END IF;
    IF TG_OP='UPDATE' AND NEW.investor_truck_id IS DISTINCT FROM OLD.investor_truck_id THEN
        RAISE EXCEPTION 'A payment cannot change settlement destination' USING ERRCODE='23514';
    END IF;
    IF EXISTS(SELECT 1 FROM investor_pay_weeks WHERE truck_id=tid AND week_start=wk AND finalized) THEN
        RAISE EXCEPTION 'Reopen the investor settlement before editing its expenses' USING ERRCODE='23514';
    END IF;
    IF TG_OP='DELETE' THEN RETURN OLD; END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER investor_expense_guard BEFORE INSERT OR UPDATE OR DELETE ON expense_payments
FOR EACH ROW EXECUTE FUNCTION protect_investor_expense_payment();

CREATE OR REPLACE FUNCTION protect_finalized_payroll() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE d uuid; w date;
BEGIN
    IF TG_TABLE_NAME='driver_pay_weeks' THEN
        IF TG_OP='DELETE' THEN d:=OLD.driver_id;w:=OLD.week_start; ELSE d:=NEW.driver_id;w:=NEW.week_start; END IF;
    ELSIF TG_TABLE_NAME='expense_payments' THEN
        IF TG_OP='DELETE' THEN
            IF OLD.investor_truck_id IS NOT NULL THEN RETURN OLD; END IF;
        ELSE
            IF NEW.investor_truck_id IS NOT NULL THEN RETURN NEW; END IF;
        END IF;
        IF TG_OP='DELETE' THEN
            SELECT charge_driver_id INTO d FROM expenses WHERE id=OLD.expense_id;w:=OLD.week_start;
        ELSE SELECT charge_driver_id INTO d FROM expenses WHERE id=NEW.expense_id;w:=NEW.week_start; END IF;
    ELSE
        IF TG_OP='DELETE' THEN
            SELECT driver_id INTO d FROM driver_charge_schedules WHERE id=OLD.schedule_id;w:=OLD.week_start;
        ELSE SELECT driver_id INTO d FROM driver_charge_schedules WHERE id=NEW.schedule_id;w:=NEW.week_start; END IF;
        -- Schedule maintenance may re-store an unchanged frozen occurrence.
        IF TG_OP='UPDATE' AND NEW.amount=OLD.amount AND NEW.name=OLD.name
           AND NEW.overridden=OLD.overridden AND NEW.confirmed_at IS NOT DISTINCT FROM OLD.confirmed_at THEN RETURN NEW; END IF;
    END IF;
    IF EXISTS(SELECT 1 FROM payroll_settlements WHERE driver_id=d AND week_start=w AND finalized) THEN
        RAISE EXCEPTION 'Reopen this driver settlement before editing payroll' USING ERRCODE='23514';
    END IF;
    IF TG_OP='DELETE' THEN RETURN OLD; END IF;
    RETURN NEW;
END $$;

CREATE FUNCTION protect_truck_expense_identity() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF (NEW.truck_id IS DISTINCT FROM OLD.truck_id OR NEW.owner_id IS DISTINCT FROM OLD.owner_id)
       AND EXISTS(SELECT 1 FROM expense_payments WHERE expense_id=OLD.id) THEN
        RAISE EXCEPTION 'An expense used in a settlement cannot change its truck or owner' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER truck_expense_identity_guard BEFORE UPDATE ON expenses FOR EACH ROW EXECUTE FUNCTION protect_truck_expense_identity();

DO $$ BEGIN
    IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
        ALTER TABLE truck_settlement_terms OWNER TO mserp_app;
        ALTER TABLE truck_charge_phases OWNER TO mserp_app;
        ALTER TABLE investor_pay_weeks OWNER TO mserp_app;
        ALTER TABLE truck_settlement_events OWNER TO mserp_app;
        ALTER SEQUENCE truck_settlement_events_id_seq OWNER TO mserp_app;
    END IF;
END $$;
COMMIT;

BEGIN;

-- A NULL result means missing or irrecoverable, never guessed or padded digits.
CREATE FUNCTION canonical_phone(value TEXT) RETURNS TEXT
LANGUAGE plpgsql IMMUTABLE AS $$
DECLARE digits TEXT;
BEGIN
    value := btrim(value, E' \t\r\n');
    IF value IS NULL OR value = '' OR value ~ '[^0-9+(). -]' THEN
        RETURN NULL;
    END IF;
    digits := regexp_replace(value, '[^0-9]', '', 'g');
    IF length(digits) = 11 AND left(digits, 1) = '1' THEN
        digits := substring(digits FROM 2);
    END IF;
    IF digits ~ '^[0-9]{10}$' THEN RETURN digits; END IF;
    RETURN NULL;
END $$;

-- Legacy contacts sometimes list a primary and secondary number separated by
-- slash. Keep the first only when every listed number is independently valid;
-- the audit retains the complete original. New writes remain single-number.
CREATE FUNCTION backfill_phone(value TEXT) RETURNS TEXT
LANGUAGE plpgsql IMMUTABLE AS $$
DECLARE parts TEXT[]; part TEXT; first_phone TEXT;
BEGIN
    IF canonical_phone(value) IS NOT NULL THEN RETURN canonical_phone(value); END IF;
    IF value IS NULL OR position('/' IN value)=0 THEN RETURN NULL; END IF;
    parts := string_to_array(value, '/');
    FOREACH part IN ARRAY parts LOOP
        IF canonical_phone(part) IS NULL THEN RETURN NULL; END IF;
        first_phone := coalesce(first_phone, canonical_phone(part));
    END LOOP;
    RETURN first_phone;
END $$;

-- Preserve the first FleetScope snapshot and receipt hashes. The application
-- exposes this canonical contact column instead of the raw source JSON phone.
ALTER TABLE fleetscope_driver_intake ADD COLUMN phone TEXT;

CREATE TABLE phone_normalization_audit (
    source_table TEXT NOT NULL,
    record_id UUID NOT NULL,
    original_value TEXT NOT NULL,
    normalized_value TEXT CHECK (normalized_value ~ '^[0-9]{10}$'),
    reason TEXT NOT NULL CHECK (reason IN ('normalized', 'primary_selected', 'invalid', 'blank')),
    normalized_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (source_table, record_id)
);

-- Capture all originals before updates, including driver-linked investor copies
-- which the existing identity trigger will update when the driver changes.
INSERT INTO phone_normalization_audit(source_table,record_id,original_value,normalized_value,reason)
SELECT source_table,id,value,backfill_phone(value),
    CASE WHEN btrim(value, E' \t\r\n') = '' THEN 'blank'
         WHEN backfill_phone(value) IS NULL THEN 'invalid'
         WHEN canonical_phone(value) IS NULL THEN 'primary_selected' ELSE 'normalized' END
FROM (
    SELECT 'drivers' AS source_table,id,phone AS value FROM drivers
    UNION ALL SELECT 'dispatchers',id,phone FROM dispatchers
    UNION ALL SELECT 'investors',id,phone FROM investors
    UNION ALL SELECT 'relay_driver_links',id,relay_phone FROM relay_driver_links
    UNION ALL SELECT 'fleetscope_driver_intake',id,driver_data->>'phone' FROM fleetscope_driver_intake
) contacts WHERE value IS NOT NULL AND value IS DISTINCT FROM backfill_phone(value);

UPDATE drivers SET phone=backfill_phone(phone) WHERE phone IS DISTINCT FROM backfill_phone(phone);
UPDATE dispatchers SET phone=backfill_phone(phone) WHERE phone IS DISTINCT FROM backfill_phone(phone);
UPDATE investors SET phone=backfill_phone(phone) WHERE phone IS DISTINCT FROM backfill_phone(phone);
UPDATE relay_driver_links SET relay_phone=backfill_phone(relay_phone) WHERE relay_phone IS DISTINCT FROM backfill_phone(relay_phone);
UPDATE fleetscope_driver_intake SET phone=backfill_phone(driver_data->>'phone');
DROP FUNCTION backfill_phone(TEXT);

ALTER TABLE drivers ADD CONSTRAINT drivers_phone_check CHECK (phone ~ '^[0-9]{10}$');
ALTER TABLE dispatchers ADD CONSTRAINT dispatchers_phone_check CHECK (phone ~ '^[0-9]{10}$');
ALTER TABLE investors ADD CONSTRAINT investors_phone_check CHECK (phone ~ '^[0-9]{10}$');
ALTER TABLE relay_driver_links ADD CONSTRAINT relay_driver_links_phone_check CHECK (relay_phone ~ '^[0-9]{10}$');
ALTER TABLE fleetscope_driver_intake ADD CONSTRAINT fleetscope_intake_phone_check CHECK (phone ~ '^[0-9]{10}$');

-- Normalize valid writes from older binaries too, preserving rollback support.
CREATE FUNCTION normalize_contact_phone() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE value TEXT; normalized TEXT;
BEGIN
    value := to_jsonb(NEW)->>TG_ARGV[0];
    normalized := canonical_phone(value);
    IF nullif(btrim(value, E' \t\r\n'), '') IS NOT NULL AND normalized IS NULL THEN
        RAISE EXCEPTION 'phone must contain exactly 10 digits' USING ERRCODE='23514';
    END IF;
    NEW := jsonb_populate_record(NEW, jsonb_build_object(TG_ARGV[0], normalized));
    RETURN NEW;
END $$;
CREATE TRIGGER drivers_phone_normalize BEFORE INSERT OR UPDATE OF phone ON drivers
FOR EACH ROW EXECUTE FUNCTION normalize_contact_phone('phone');
CREATE TRIGGER dispatchers_phone_normalize BEFORE INSERT OR UPDATE OF phone ON dispatchers
FOR EACH ROW EXECUTE FUNCTION normalize_contact_phone('phone');
CREATE TRIGGER investors_phone_normalize BEFORE INSERT OR UPDATE OF phone ON investors
FOR EACH ROW EXECUTE FUNCTION normalize_contact_phone('phone');
CREATE TRIGGER relay_phone_normalize BEFORE INSERT OR UPDATE OF relay_phone ON relay_driver_links
FOR EACH ROW EXECUTE FUNCTION normalize_contact_phone('relay_phone');

-- Old webhook binaries still populate only driver_data; derive the contact
-- without changing that snapshot or blocking receipt for malformed source data.
CREATE FUNCTION set_intake_phone() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    NEW.phone := canonical_phone(NEW.driver_data->>'phone');
    RETURN NEW;
END $$;
CREATE TRIGGER intake_phone_from_source BEFORE INSERT OR UPDATE OF driver_data ON fleetscope_driver_intake
FOR EACH ROW EXECUTE FUNCTION set_intake_phone();
CREATE TRIGGER intake_phone_normalize BEFORE UPDATE OF phone ON fleetscope_driver_intake
FOR EACH ROW EXECUTE FUNCTION normalize_contact_phone('phone');

DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
        ALTER TABLE phone_normalization_audit OWNER TO mserp_app;
    END IF;
END $$;
COMMIT;

BEGIN;

-- Called by assignment and recurring-charge writes, and once for existing
-- assignments. Recurring operating fees move; personal installments do not.
CREATE FUNCTION handoff_investor_driver_charges(p_driver UUID, p_actor UUID DEFAULT NULL)
RETURNS void LANGUAGE plpgsql AS $$
DECLARE
    assignment RECORD;
    schedule RECORD;
    phase RECORD;
    effective DATE;
    cutoff DATE;
    amount_at_start NUMERIC;
    paused_at_start BOOLEAN;
    detail JSONB;
BEGIN
    PERFORM pg_advisory_xact_lock_shared(736281940);
    PERFORM 1 FROM drivers WHERE id=p_driver FOR UPDATE;
    SELECT a.truck_id, date_trunc('week',greatest(a.assigned_at,CASE WHEN h.start_known THEN h.assigned_at END) AT TIME ZONE 'America/New_York')::date AS week
      INTO assignment
      FROM truck_driver_assignments a JOIN trucks t ON t.id=a.truck_id
      JOIN investors i ON i.id=t.owner_id
      LEFT JOIN truck_ownership_history h ON h.truck_id=t.id AND h.owner_id=i.id AND h.unassigned_at IS NULL
      WHERE a.driver_id=p_driver AND a.unassigned_at IS NULL AND NOT i.is_company
        AND (i.driver_id IS NULL OR (i.driver_id<>p_driver AND
          (SELECT count(*) FROM trucks owned WHERE owned.owner_id=i.id)>=2));
    IF NOT FOUND THEN RETURN; END IF;
    -- Serialize fee seeding when replacement drivers share a truck/week.
    PERFORM 1 FROM trucks WHERE id=assignment.truck_id FOR UPDATE;
    FOR schedule IN SELECT * FROM driver_charge_schedules
      WHERE driver_id=p_driver AND kind='recurring' AND direction='charge'
      ORDER BY start_week,id FOR UPDATE
    LOOP
      effective := greatest(assignment.week,schedule.start_week);
      IF schedule.end_week IS NOT NULL AND schedule.end_week<effective THEN CONTINUE; END IF;
      SELECT amount,paused INTO amount_at_start,paused_at_start FROM driver_charge_phases
        WHERE schedule_id=schedule.id AND week_start<=effective ORDER BY week_start DESC LIMIT 1;
      IF amount_at_start IS NULL THEN CONTINUE; END IF;
      IF paused_at_start AND NOT EXISTS(SELECT 1 FROM driver_charge_phases
           WHERE schedule_id=schedule.id AND week_start>effective AND NOT paused)
         AND NOT EXISTS(SELECT 1 FROM driver_charge_occurrences WHERE schedule_id=schedule.id AND week_start>=effective)
      THEN CONTINUE; END IF;
      IF EXISTS(SELECT 1 FROM payroll_settlements WHERE driver_id=p_driver AND week_start>=effective AND finalized)
        OR EXISTS(SELECT 1 FROM investor_pay_weeks WHERE truck_id=assignment.truck_id AND week_start>=effective AND finalized)
        OR EXISTS(SELECT 1 FROM driver_charge_occurrences WHERE schedule_id=schedule.id AND week_start>=effective AND confirmed_at IS NOT NULL)
      THEN RAISE EXCEPTION 'Reopen affected settlements before moving driver fees to an investor truck' USING ERRCODE='23514'; END IF;
      SELECT jsonb_build_object('truckId',assignment.truck_id,'weekStart',effective,
        'phases',coalesce((SELECT jsonb_agg(to_jsonb(p)) FROM driver_charge_phases p WHERE schedule_id=schedule.id),'[]'::jsonb),
        'occurrences',coalesce((SELECT jsonb_agg(to_jsonb(o)) FROM driver_charge_occurrences o WHERE schedule_id=schedule.id AND week_start>=effective),'[]'::jsonb)) INTO detail;
      -- Existing truck selections win, including explicit pauses. Only fill
      -- the gap before the first configured phase; never stack the same type.
      IF NOT EXISTS(SELECT 1 FROM truck_charge_phases WHERE truck_id=assignment.truck_id AND type_id=schedule.type_id AND week_start<=effective) THEN
        SELECT min(week_start) INTO cutoff FROM truck_charge_phases WHERE truck_id=assignment.truck_id AND type_id=schedule.type_id;
        INSERT INTO truck_charge_phases(truck_id,type_id,week_start,amount,included,updated_by)
          VALUES(assignment.truck_id,schedule.type_id,effective,amount_at_start,NOT paused_at_start,p_actor);
        FOR phase IN SELECT * FROM driver_charge_phases WHERE schedule_id=schedule.id AND week_start>effective
            AND (cutoff IS NULL OR week_start<cutoff) AND (schedule.end_week IS NULL OR week_start<=schedule.end_week) ORDER BY week_start
        LOOP
          INSERT INTO truck_charge_phases(truck_id,type_id,week_start,amount,included,updated_by)
            VALUES(assignment.truck_id,schedule.type_id,phase.week_start,phase.amount,NOT phase.paused,p_actor);
        END LOOP;
        IF schedule.end_week IS NOT NULL AND (cutoff IS NULL OR schedule.end_week+7<cutoff) THEN
          INSERT INTO truck_charge_phases(truck_id,type_id,week_start,amount,included,updated_by)
            VALUES(assignment.truck_id,schedule.type_id,schedule.end_week+7,amount_at_start,false,p_actor) ON CONFLICT DO NOTHING;
        END IF;
      END IF;
      DELETE FROM driver_charge_occurrences WHERE schedule_id=schedule.id AND week_start>=effective;
      DELETE FROM driver_charge_phases WHERE schedule_id=schedule.id AND week_start>=effective;
      INSERT INTO driver_charge_phases(schedule_id,week_start,amount,paused) VALUES(schedule.id,effective,amount_at_start,true);
      UPDATE driver_charge_schedules SET version=version+1 WHERE id=schedule.id;
      INSERT INTO driver_charge_events(schedule_id,type_id,actor_id,action,details)
        VALUES(schedule.id,schedule.type_id,p_actor,'investor_truck_handoff',detail);
      INSERT INTO truck_settlement_events(truck_id,week_start,actor_id,action,details)
        VALUES(assignment.truck_id,effective,p_actor,'driver_fee_handoff',detail);
    END LOOP;
END $$;

DO $$ DECLARE driver UUID; BEGIN
  FOR driver IN SELECT driver_id FROM truck_driver_assignments WHERE unassigned_at IS NULL ORDER BY driver_id
  LOOP PERFORM handoff_investor_driver_charges(driver); END LOOP;
END $$;

-- Access control (migration 044).

CREATE TABLE app_roles (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL CHECK (name = btrim(name) AND char_length(name) BETWEEN 1 AND 80),
    permissions TEXT[] NOT NULL DEFAULT '{}',
    system_role BOOLEAN NOT NULL DEFAULT false,
    version INTEGER NOT NULL DEFAULT 1
);
CREATE UNIQUE INDEX app_roles_name_idx ON app_roles(lower(name));
CREATE UNIQUE INDEX app_roles_system_idx ON app_roles(system_role) WHERE system_role;
INSERT INTO app_roles(name, system_role) VALUES ('Administrator', true);

ALTER TABLE app_users ADD COLUMN email TEXT;
ALTER TABLE app_users ADD COLUMN role_id UUID REFERENCES app_roles(id);
ALTER TABLE app_users ADD COLUMN version INTEGER NOT NULL DEFAULT 1;
ALTER TABLE app_users ADD CONSTRAINT app_users_email_check CHECK
    (email IS NULL OR (email = lower(btrim(email)) AND email ~ '^[^[:space:]@]+@[^[:space:]@]+\.[^[:space:]@]+$'));
CREATE UNIQUE INDEX app_users_email_idx ON app_users(lower(email)) WHERE email IS NOT NULL;
-- Existing users already had unrestricted access. Preserve it explicitly.
-- Legacy username login works only until an administrator attaches a real email.
UPDATE app_users SET role_id=(SELECT id FROM app_roles WHERE system_role);

CREATE TABLE access_audit (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    actor_id UUID REFERENCES app_users(id) ON DELETE SET NULL,
    action TEXT NOT NULL,
    target_id UUID NOT NULL,
    details JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
DELETE FROM auth_sessions;

DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
        ALTER TABLE app_roles OWNER TO mserp_app;
        ALTER TABLE access_audit OWNER TO mserp_app;
    END IF;
END $$;
COMMIT;

BEGIN;

ALTER TABLE drivers ADD COLUMN driver_home TEXT NOT NULL DEFAULT '' CHECK (char_length(driver_home) <= 300);
ALTER TABLE drivers ADD COLUMN driver_home_version INTEGER NOT NULL DEFAULT 0;

CREATE FUNCTION version_driver_home() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.driver_home IS DISTINCT FROM OLD.driver_home THEN
        NEW.driver_home_version := OLD.driver_home_version + 1;
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER drivers_home_version BEFORE UPDATE ON drivers
FOR EACH ROW EXECUTE FUNCTION version_driver_home();

-- A live dispatch board, independent of weekly Gross Board load entries.
CREATE TABLE driver_board (
    driver_id UUID PRIMARY KEY REFERENCES drivers(id) ON DELETE CASCADE,
    current_load TEXT NOT NULL DEFAULT '' CHECK (char_length(current_load) <= 300),
    trailer_number TEXT NOT NULL DEFAULT '' CHECK (char_length(trailer_number) <= 100),
    status TEXT NOT NULL DEFAULT '' CHECK (status IN ('','ENROUTE','DISPATCHED','RESERVED','HOME','VACATION','SHOP','RESET','NO LOAD','STUCK','LATE DEL','TRUCK ISSUE','LEFT','NEW DRIVER','DEADHEAD','LOAD CANCELLED','REJECTED')),
    destination TEXT NOT NULL DEFAULT '' CHECK (char_length(destination) <= 500),
    eta TEXT NOT NULL DEFAULT '' CHECK (char_length(eta) <= 500),
    notes TEXT NOT NULL DEFAULT '' CHECK (char_length(notes) <= 5000),
    home_time TEXT NOT NULL DEFAULT '' CHECK (char_length(home_time) <= 500),
    version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
        ALTER TABLE driver_board OWNER TO mserp_app;
    END IF;
END $$;
COMMIT;
BEGIN;

-- Keep identity/name snapshots after a driver or account is removed.
CREATE TABLE driver_board_history (
    id BIGSERIAL PRIMARY KEY,
    driver_id UUID NOT NULL,
    driver_name TEXT NOT NULL,
    actor_id TEXT NOT NULL DEFAULT '',
    actor_name TEXT NOT NULL,
    source TEXT NOT NULL,
    undo_of BIGINT REFERENCES driver_board_history(id),
    before_values JSONB NOT NULL,
    after_values JSONB NOT NULL,
    transaction_id BIGINT NOT NULL DEFAULT txid_current(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (driver_id, transaction_id)
);
CREATE INDEX driver_board_history_driver_idx ON driver_board_history(driver_id,id DESC);

CREATE FUNCTION audit_driver_board_change() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    previous JSONB; following JSONB; changed_before JSONB; changed_after JSONB;
    driver UUID; driver_name_snapshot TEXT; actor TEXT; actor_name_snapshot TEXT;
BEGIN
    IF TG_TABLE_NAME = 'drivers' THEN
        IF OLD.driver_home IS NOT DISTINCT FROM NEW.driver_home THEN RETURN NEW; END IF;
        driver := NEW.id;
        previous := jsonb_build_object('driverHome',OLD.driver_home);
        following := jsonb_build_object('driverHome',NEW.driver_home);
    ELSE
        driver := NEW.driver_id;
        following := jsonb_build_object('currentLoad',NEW.current_load,'trailerNumber',NEW.trailer_number,
          'status',NEW.status,'destination',NEW.destination,'eta',NEW.eta,'notes',NEW.notes,'homeTime',NEW.home_time);
        IF TG_OP = 'INSERT' THEN
            SELECT jsonb_object_agg(key,''::text) INTO previous FROM jsonb_each(following);
        ELSE
            previous := jsonb_build_object('currentLoad',OLD.current_load,'trailerNumber',OLD.trailer_number,
              'status',OLD.status,'destination',OLD.destination,'eta',OLD.eta,'notes',OLD.notes,'homeTime',OLD.home_time);
        END IF;
    END IF;
    SELECT jsonb_object_agg(key,previous->key), jsonb_object_agg(key,value)
      INTO changed_before,changed_after FROM jsonb_each(following)
      WHERE previous->key IS DISTINCT FROM value;
    IF changed_after IS NULL THEN RETURN NEW; END IF;
    SELECT full_name INTO driver_name_snapshot FROM drivers WHERE id=driver;
    actor := coalesce(current_setting('mserp.board_actor',true),'');
    SELECT username INTO actor_name_snapshot FROM app_users WHERE id::text=actor;
    INSERT INTO driver_board_history(driver_id,driver_name,actor_id,actor_name,source,undo_of,before_values,after_values)
    VALUES(driver,driver_name_snapshot,actor,coalesce(actor_name_snapshot,'System'),
      coalesce(nullif(current_setting('mserp.board_source',true),''),'database'),
      nullif(current_setting('mserp.board_undo',true),'')::bigint,changed_before,changed_after)
    ON CONFLICT(driver_id,transaction_id) DO UPDATE SET
      before_values=EXCLUDED.before_values || driver_board_history.before_values,
      after_values=driver_board_history.after_values || EXCLUDED.after_values;
    RETURN NEW;
END $$;
CREATE TRIGGER audit_driver_board AFTER INSERT OR UPDATE ON driver_board
FOR EACH ROW EXECUTE FUNCTION audit_driver_board_change();
CREATE TRIGGER audit_driver_home AFTER UPDATE ON drivers
FOR EACH ROW EXECUTE FUNCTION audit_driver_board_change();

-- Only the transaction creating an event may combine its field changes.
CREATE FUNCTION protect_driver_board_history() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='DELETE' OR OLD.transaction_id<>txid_current() THEN
        RAISE EXCEPTION 'driver board history is append-only';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER protect_driver_board_history BEFORE UPDATE OR DELETE ON driver_board_history
FOR EACH ROW EXECUTE FUNCTION protect_driver_board_history();
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
        ALTER TABLE driver_board_history OWNER TO mserp_app;
        ALTER SEQUENCE driver_board_history_id_seq OWNER TO mserp_app;
    END IF;
END $$;
COMMIT;

BEGIN;

-- Keep the old trigger contract for imports and older binaries. Fleet forms set
-- this transaction-local week explicitly (New York midnight, including DST).
CREATE OR REPLACE FUNCTION record_driver_dispatcher_assignment() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE changed_at timestamptz := clock_timestamp();
DECLARE effective_week date := nullif(current_setting('mserp.assignment_week',true),'')::date;
BEGIN
    IF TG_OP='UPDATE' AND NEW.dispatcher_id IS NOT DISTINCT FROM OLD.dispatcher_id THEN
        RETURN NEW;
    END IF;
    IF effective_week IS NOT NULL THEN
        changed_at := effective_week::timestamp AT TIME ZONE 'America/New_York';
        IF EXISTS (SELECT 1 FROM driver_dispatcher_assignments
            WHERE driver_id=NEW.id AND start_known
            AND assigned_at >= ((effective_week+7)::timestamp AT TIME ZONE 'America/New_York')) THEN
            RAISE EXCEPTION 'The selected week precedes a later dispatcher assignment; choose that week or a later one' USING ERRCODE='23514';
        END IF;
        -- Preserve multiple changes in one week as zero-length history records,
        -- moving both ends together so the previous week remains untouched.
        UPDATE driver_dispatcher_assignments SET
            assigned_at=least(assigned_at,changed_at),
            unassigned_at=CASE WHEN unassigned_at IS NULL THEN NULL ELSE least(unassigned_at,changed_at) END
        WHERE driver_id=NEW.id AND (assigned_at>=changed_at OR unassigned_at>=changed_at);
    END IF;
    UPDATE driver_dispatcher_assignments SET unassigned_at=changed_at
    WHERE driver_id=NEW.id AND unassigned_at IS NULL;
    INSERT INTO driver_dispatcher_assignments(driver_id,dispatcher_id,dispatcher_name,assigned_at,source)
    VALUES(NEW.id,NEW.dispatcher_id,
        coalesce((SELECT full_name FROM dispatchers WHERE id=NEW.dispatcher_id),'Unassigned'),
        changed_at,coalesce(current_setting('mserp.assignment_source',true),''));
    RETURN NEW;
END $$;

COMMIT;

BEGIN;

-- Current Five ELD telemetry is operational cache data. It is matched to the
-- assigned truck by normalized VIN and never changes fleet assignments.
CREATE TABLE five_eld_locations (
    vin TEXT PRIMARY KEY CHECK (vin ~ '^[A-Z0-9]{17}$'),
    provider_truck_number TEXT NOT NULL DEFAULT '',
    address TEXT NOT NULL DEFAULT '',
    latitude DOUBLE PRECISION NOT NULL CHECK (latitude BETWEEN -90 AND 90),
    longitude DOUBLE PRECISION NOT NULL CHECK (longitude BETWEEN -180 AND 180),
    reported_at TIMESTAMPTZ NOT NULL,
    fetched_at TIMESTAMPTZ NOT NULL,
    address_updated_at TIMESTAMPTZ,
    address_reported_at TIMESTAMPTZ
);

CREATE TABLE five_eld_sync_state (
    singleton BOOLEAN PRIMARY KEY DEFAULT true CHECK (singleton),
    last_attempt_at TIMESTAMPTZ,
    last_success_at TIMESTAMPTZ,
    last_error TEXT NOT NULL DEFAULT '',
    stale_after_seconds INTEGER NOT NULL DEFAULT 900 CHECK (stale_after_seconds BETWEEN 60 AND 86400),
    unmatched_vins TEXT[] NOT NULL DEFAULT '{}',
    ambiguous_vins TEXT[] NOT NULL DEFAULT '{}',
    invalid_unit_count INTEGER NOT NULL DEFAULT 0 CHECK (invalid_unit_count >= 0)
);

DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'mserp_app') THEN
        ALTER TABLE five_eld_locations OWNER TO mserp_app;
        ALTER TABLE five_eld_sync_state OWNER TO mserp_app;
    END IF;
END $$;

COMMIT;

BEGIN;

ALTER TABLE five_eld_locations ADD COLUMN heading DOUBLE PRECISION
    CHECK (heading >= 0 AND heading < 360);

COMMIT;
BEGIN;

-- A plan identity survives rate/date edits, but never follows a reused load slot.
ALTER TABLE gross_board_entries ADD COLUMN plan_id UUID NOT NULL DEFAULT gen_random_uuid();
ALTER TABLE gross_board_extra_entries ADD COLUMN plan_id UUID NOT NULL DEFAULT gen_random_uuid();
CREATE UNIQUE INDEX gross_board_plan_id_idx ON gross_board_entries(plan_id);
CREATE UNIQUE INDEX gross_board_extra_plan_id_idx ON gross_board_extra_entries(plan_id);
CREATE FUNCTION version_gross_board_plan() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF lower(btrim(NEW.load_number)) IS DISTINCT FROM lower(btrim(OLD.load_number))
       OR NEW.day_status IS DISTINCT FROM OLD.day_status
       OR (OLD.load_record_id IS NOT NULL AND NEW.load_record_id IS NOT NULL AND OLD.load_record_id<>NEW.load_record_id)
       OR (TG_TABLE_NAME='gross_board_extra_entries' AND (to_jsonb(NEW)->'deleted') IS DISTINCT FROM (to_jsonb(OLD)->'deleted')) THEN
        NEW.plan_id := gen_random_uuid();
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER gross_board_plan_identity BEFORE UPDATE ON gross_board_entries FOR EACH ROW EXECUTE FUNCTION version_gross_board_plan();
CREATE TRIGGER gross_board_extra_plan_identity BEFORE UPDATE ON gross_board_extra_entries FOR EACH ROW EXECUTE FUNCTION version_gross_board_plan();

-- Operational order/selection is independent of financial board placement.
CREATE TABLE driver_board_load_state (
    driver_id UUID PRIMARY KEY REFERENCES drivers(id) ON DELETE CASCADE,
    payload JSONB NOT NULL DEFAULT '{}' CHECK(jsonb_typeof(payload)='object')
);
CREATE FUNCTION audit_driver_board_loads() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE previous TEXT := '{}'; actor TEXT; actor_name_snapshot TEXT; driver_name_snapshot TEXT;
BEGIN
    IF TG_OP='UPDATE' THEN previous := OLD.payload::text; END IF;
    IF previous=NEW.payload::text THEN RETURN NEW; END IF;
    SELECT full_name INTO driver_name_snapshot FROM drivers WHERE id=NEW.driver_id;
    actor := coalesce(current_setting('mserp.board_actor',true),'');
    SELECT username INTO actor_name_snapshot FROM app_users WHERE id::text=actor;
    INSERT INTO driver_board_history(driver_id,driver_name,actor_id,actor_name,source,undo_of,before_values,after_values)
    VALUES(NEW.driver_id,driver_name_snapshot,actor,coalesce(actor_name_snapshot,'System'),
      coalesce(nullif(current_setting('mserp.board_source',true),''),'database'),
      nullif(current_setting('mserp.board_undo',true),'')::bigint,
      jsonb_build_object('loadPlan',previous),jsonb_build_object('loadPlan',NEW.payload::text))
    ON CONFLICT(driver_id,transaction_id) DO UPDATE SET
      before_values=EXCLUDED.before_values || driver_board_history.before_values,
      after_values=driver_board_history.after_values || EXCLUDED.after_values;
    RETURN NEW;
END $$;
CREATE TRIGGER audit_driver_board_load_state AFTER INSERT OR UPDATE ON driver_board_load_state
FOR EACH ROW EXECUTE FUNCTION audit_driver_board_loads();
DO $$ BEGIN
    IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
        ALTER TABLE driver_board_load_state OWNER TO mserp_app;
    END IF;
END $$;
COMMIT;

BEGIN;

-- Each updater has one shift; each dispatcher has at most one updater per shift.
CREATE TABLE updaters (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    full_name TEXT NOT NULL CHECK (btrim(full_name) <> ''),
    normalized_name TEXT NOT NULL UNIQUE,
    shift TEXT NOT NULL CHECK (shift IN ('main', 'after_hours')),
    extension INTEGER CHECK (extension BETWEEN 0 AND 999999),
    version INTEGER NOT NULL DEFAULT 1,
    UNIQUE (id, shift)
);

ALTER TABLE dispatchers ADD COLUMN extension INTEGER CHECK (extension BETWEEN 0 AND 999999);

CREATE TABLE dispatcher_updaters (
    dispatcher_id UUID NOT NULL REFERENCES dispatchers(id) ON DELETE CASCADE,
    shift TEXT NOT NULL CHECK (shift IN ('main', 'after_hours')),
    updater_id UUID NOT NULL,
    PRIMARY KEY (dispatcher_id, shift),
    FOREIGN KEY (updater_id, shift) REFERENCES updaters(id, shift) ON DELETE CASCADE
);
CREATE INDEX dispatcher_updaters_updater_idx ON dispatcher_updaters(updater_id);

DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'mserp_app') THEN
        ALTER TABLE updaters OWNER TO mserp_app;
        ALTER TABLE dispatcher_updaters OWNER TO mserp_app;
    END IF;
END $$;

COMMIT;

BEGIN;

-- FleetScope termination receipts survive task/driver deletion and late hire delivery.
ALTER TABLE fleetscope_driver_intake ADD COLUMN terminated_at TIMESTAMPTZ;
CREATE TABLE fleetscope_driver_terminations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id UUID NOT NULL,
    fleetscope_driver_id UUID NOT NULL,
    driver_name TEXT NOT NULL,
    termination_date DATE NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL,
    received_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    driver_id UUID REFERENCES drivers(id) ON DELETE SET NULL,
    task_id UUID REFERENCES custom_tasks(id) ON DELETE SET NULL,
    UNIQUE(company_id, fleetscope_driver_id)
);
ALTER TABLE fleetscope_webhook_receipts ALTER COLUMN intake_id DROP NOT NULL;
ALTER TABLE fleetscope_webhook_receipts ADD COLUMN termination_id UUID REFERENCES fleetscope_driver_terminations(id);
ALTER TABLE fleetscope_webhook_receipts ADD CONSTRAINT fleetscope_receipt_target
    CHECK (num_nonnulls(intake_id, termination_id) = 1);

DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'mserp_app') THEN
        ALTER TABLE fleetscope_driver_terminations OWNER TO mserp_app;
    END IF;
END $$;
COMMIT;
BEGIN;
ALTER TABLE driver_charge_occurrences ADD COLUMN base_amount NUMERIC(14,2);
ALTER TABLE driver_charge_occurrences ADD COLUMN waive_remainder BOOLEAN NOT NULL DEFAULT false;

CREATE TABLE driver_pay_cost_collections (
    driver_id UUID NOT NULL REFERENCES drivers(id) ON DELETE RESTRICT,
    week_start DATE NOT NULL CHECK (extract(isodow FROM week_start)=1),
    fuel_base NUMERIC(14,2) NOT NULL,
    toll_base NUMERIC(14,2) NOT NULL,
    fuel_amount NUMERIC(14,2) NOT NULL,
    toll_amount NUMERIC(14,2) NOT NULL,
    updated_by UUID REFERENCES app_users(id) ON DELETE SET NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY(driver_id,week_start)
);

CREATE FUNCTION protect_payroll_remainders() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE d uuid; w date;
BEGIN
    IF TG_TABLE_NAME='driver_pay_cost_collections' THEN
        IF TG_OP='DELETE' THEN d:=OLD.driver_id; w:=OLD.week_start;
        ELSE d:=NEW.driver_id; w:=NEW.week_start; END IF;
    ELSE
        IF TG_OP<>'UPDATE' OR (NEW.base_amount IS NOT DISTINCT FROM OLD.base_amount AND NEW.waive_remainder=OLD.waive_remainder) THEN RETURN NEW; END IF;
        SELECT driver_id INTO d FROM driver_charge_schedules WHERE id=NEW.schedule_id;
        w:=NEW.week_start;
    END IF;
    IF EXISTS(SELECT 1 FROM payroll_settlements WHERE driver_id=d AND week_start=w AND finalized) THEN
        RAISE EXCEPTION 'Reopen this driver settlement before editing payroll' USING ERRCODE='23514';
    END IF;
    IF TG_OP='DELETE' THEN RETURN OLD; END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER payroll_cost_collection_guard BEFORE INSERT OR UPDATE OR DELETE ON driver_pay_cost_collections FOR EACH ROW EXECUTE FUNCTION protect_payroll_remainders();
CREATE TRIGGER payroll_charge_remainder_guard BEFORE UPDATE ON driver_charge_occurrences FOR EACH ROW EXECUTE FUNCTION protect_payroll_remainders();
DO $$ BEGIN
    IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
        ALTER TABLE driver_pay_cost_collections OWNER TO mserp_app;
    END IF;
END $$;
COMMIT;
BEGIN;

CREATE TABLE system_task_assignments (
    id UUID NOT NULL UNIQUE DEFAULT gen_random_uuid(),
    kind TEXT PRIMARY KEY CHECK (kind IN ('driver_onboarding','driver_offboarding','relay_review')),
    assignee_id UUID REFERENCES app_users(id) ON DELETE SET NULL,
    version INTEGER NOT NULL DEFAULT 1,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by UUID REFERENCES app_users(id) ON DELETE SET NULL
);
INSERT INTO system_task_assignments(kind) VALUES ('driver_onboarding'),('driver_offboarding'),('relay_review');

ALTER TABLE custom_tasks ADD COLUMN assigned_to UUID REFERENCES app_users(id) ON DELETE SET NULL;
ALTER TABLE custom_tasks ADD COLUMN assigned_by UUID REFERENCES app_users(id) ON DELETE SET NULL;
ALTER TABLE custom_tasks ADD COLUMN system_task_kind TEXT REFERENCES system_task_assignments(kind);
-- Use the durable integration link, never task title/name matching.
DO $$ BEGIN
    IF to_regclass('fleetscope_driver_terminations') IS NOT NULL THEN
        UPDATE custom_tasks c SET system_task_kind='driver_offboarding'
        FROM fleetscope_driver_terminations t WHERE t.task_id=c.id;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
        ALTER TABLE system_task_assignments OWNER TO mserp_app;
    END IF;
END $$;
COMMIT;
BEGIN;
-- Audit snapshots survive deletion of unused catalog entries.
ALTER TABLE driver_charge_events DROP CONSTRAINT driver_charge_events_type_id_fkey;
ALTER TABLE driver_charge_events ADD CONSTRAINT driver_charge_events_type_id_fkey
    FOREIGN KEY(type_id) REFERENCES driver_charge_types(id) ON DELETE SET NULL;
COMMIT;
BEGIN;

-- Durable expense categories, category-scoped role access and creator attribution
-- (migration 056).
INSERT INTO expense_settings(kind,name,active)
SELECT 'category', min(btrim(e.category)), false
FROM expenses e
WHERE NOT EXISTS (
    SELECT 1 FROM expense_settings s
    WHERE s.kind='category' AND lower(s.name)=lower(btrim(e.category))
)
GROUP BY lower(btrim(e.category));

ALTER TABLE expenses ADD COLUMN category_id UUID REFERENCES expense_settings(id);
UPDATE expenses e SET category_id=s.id
FROM expense_settings s
WHERE s.kind='category' AND lower(s.name)=lower(btrim(e.category));
ALTER TABLE expenses ALTER COLUMN category_id SET NOT NULL;
CREATE INDEX expenses_category_id_date_idx ON expenses(category_id,expense_date DESC);
ALTER TABLE expenses ADD COLUMN created_by UUID REFERENCES app_users(id) ON DELETE SET NULL;
ALTER TABLE expenses ADD COLUMN created_by_name TEXT;

CREATE TABLE role_expense_category_access (
    role_id UUID NOT NULL REFERENCES app_roles(id) ON DELETE CASCADE,
    category_id UUID NOT NULL REFERENCES expense_settings(id) ON DELETE CASCADE,
    can_view BOOLEAN NOT NULL DEFAULT false,
    can_create BOOLEAN NOT NULL DEFAULT false,
    can_edit BOOLEAN NOT NULL DEFAULT false,
    can_delete BOOLEAN NOT NULL DEFAULT false,
    PRIMARY KEY(role_id,category_id),
    CHECK (can_view OR can_create OR can_edit OR can_delete)
);
INSERT INTO role_expense_category_access(role_id,category_id,can_view,can_create,can_edit,can_delete)
SELECT r.id,c.id,
       'expenses.read'=ANY(r.permissions),
       'expenses.write'=ANY(r.permissions),
       'expenses.write'=ANY(r.permissions),
       'expenses.write'=ANY(r.permissions)
FROM app_roles r CROSS JOIN expense_settings c
WHERE NOT r.system_role AND c.kind='category'
  AND ('expenses.read'=ANY(r.permissions) OR 'expenses.write'=ANY(r.permissions));
UPDATE app_roles
SET permissions = CASE
    WHEN 'expenses.write'=ANY(permissions)
      AND NOT 'expense_settings.manage'=ANY(permissions)
      THEN array_append(array_remove(array_remove(permissions,'expenses.read'),'expenses.write'),'expense_settings.manage')
    ELSE array_remove(array_remove(permissions,'expenses.read'),'expenses.write')
END;

DROP TRIGGER expense_category_guard ON expenses;
CREATE OR REPLACE FUNCTION validate_expense_category() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE current_name TEXT;
BEGIN
    IF TG_OP='UPDATE' AND NEW.category_id=OLD.category_id THEN
        IF NEW.category IS DISTINCT FROM OLD.category THEN
            RAISE EXCEPTION 'Expense category text cannot change without its category id' USING ERRCODE='23514', CONSTRAINT='expense_active_category';
        END IF;
        RETURN NEW;
    END IF;
    SELECT name INTO current_name FROM expense_settings
    WHERE id=NEW.category_id AND kind='category' AND active FOR SHARE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'Select an active expense category' USING ERRCODE='23514', CONSTRAINT='expense_active_category';
    END IF;
    NEW.category:=current_name;
    RETURN NEW;
END $$;
CREATE TRIGGER expense_category_guard BEFORE INSERT OR UPDATE OF category,category_id ON expenses
    FOR EACH ROW EXECUTE FUNCTION validate_expense_category();
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
        ALTER TABLE role_expense_category_access OWNER TO mserp_app;
    END IF;
END $$;
COMMIT;
BEGIN;

-- Tables created by earlier migrations are owned by the runtime role. PostgreSQL
-- checks their foreign keys with that owner's privileges, including while this
-- migration is run by an administrator in a private test schema.
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
        EXECUTE format('GRANT USAGE ON SCHEMA %I TO mserp_app', current_schema());
    END IF;
END $$;

-- Driver escrow is a normal driver-covered Safety expense. The singleton
-- setting supplies the default for new-driver setup while each created expense
-- keeps its original principal independently.
UPDATE expense_settings
SET active=true, version=version+1
WHERE kind='category' AND lower(name)='safety' AND NOT active;

INSERT INTO expense_settings(kind,name,active)
SELECT 'category','Safety',true
WHERE NOT EXISTS (
    SELECT 1 FROM expense_settings WHERE kind='category' AND lower(name)='safety'
);

INSERT INTO expense_settings(kind,category_id,name,active)
SELECT 'name',id,'Escrow payment',true
FROM expense_settings
WHERE kind='category' AND lower(name)='safety'
ON CONFLICT (kind, (coalesce(category_id, '00000000-0000-0000-0000-000000000000'::uuid)), (lower(name)))
DO UPDATE SET active=true, version=expense_settings.version+1;

CREATE TABLE driver_escrow_settings (
    singleton BOOLEAN PRIMARY KEY DEFAULT true CHECK (singleton),
    category_id UUID NOT NULL REFERENCES expense_settings(id) ON DELETE RESTRICT,
    default_amount NUMERIC(14,2) NOT NULL CHECK (default_amount > 0),
    version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
INSERT INTO driver_escrow_settings(singleton,category_id,default_amount)
SELECT true,id,2500
FROM expense_settings
WHERE kind='category' AND lower(name)='safety';

ALTER TABLE expenses ADD COLUMN system_kind TEXT
    CHECK (system_kind IS NULL OR system_kind='driver_escrow');
CREATE UNIQUE INDEX expenses_driver_escrow_unique
    ON expenses(driver_id) WHERE system_kind='driver_escrow';

-- Preserve a pre-existing canonical escrow instead of creating a duplicate.
WITH existing AS (
    SELECT DISTINCT ON (e.driver_id) e.id
    FROM expenses e
    JOIN driver_escrow_settings s ON s.category_id=e.category_id
    WHERE e.driver_id IS NOT NULL
      AND lower(btrim(coalesce(e.covered_by,'')))='driver'
      AND lower(btrim(coalesce(e.expense_type,'')))='escrow payment'
    ORDER BY e.driver_id,e.created_at,e.id
)
UPDATE expenses e SET system_kind='driver_escrow'
FROM existing x WHERE e.id=x.id;

INSERT INTO expenses(
    company,category_id,category,expense_date,driver_id,driver_name,amount,
    expense_type,description,covered_by,paid_by,system_kind
)
SELECT 'MS Express',s.category_id,c.name,
       coalesce(d.hire_date,(now() AT TIME ZONE 'America/New_York')::date),
       d.id,d.full_name,s.default_amount,'Escrow payment',
       'Driver safety escrow','Driver','MS Express','driver_escrow'
FROM drivers d
CROSS JOIN driver_escrow_settings s
JOIN expense_settings c ON c.id=s.category_id
WHERE NOT EXISTS (
    SELECT 1 FROM expenses e
    WHERE e.driver_id=d.id AND e.system_kind='driver_escrow'
);

CREATE OR REPLACE FUNCTION validate_expense_setting() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='UPDATE' AND NEW.kind<>OLD.kind THEN
        RAISE EXCEPTION 'Setting kind cannot change' USING ERRCODE='23514';
    END IF;
    IF TG_OP='UPDATE'
       AND OLD.id=(SELECT category_id FROM driver_escrow_settings WHERE singleton)
       AND (NEW.name<>'Safety' OR NOT NEW.active) THEN
        RAISE EXCEPTION 'The Safety category is required for driver escrow'
            USING ERRCODE='23514', CONSTRAINT='driver_escrow_category';
    END IF;
    IF NEW.kind='name' THEN
        PERFORM 1 FROM expense_settings WHERE id=NEW.category_id AND kind='category'
            AND (active OR NOT NEW.active) FOR SHARE;
        IF NOT FOUND THEN
            RAISE EXCEPTION 'Select an active expense category' USING ERRCODE='23514';
        END IF;
    END IF;
    RETURN NEW;
END $$;

DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
        ALTER TABLE driver_escrow_settings OWNER TO mserp_app;
    END IF;
END $$;
COMMIT;

BEGIN;

UPDATE expense_settings
SET name='Escrow', version=version+1
WHERE kind='name' AND lower(btrim(name))='escrow payment';

UPDATE expenses
SET expense_type='Escrow', updated_at=now()
WHERE system_kind='driver_escrow' AND expense_type IS DISTINCT FROM 'Escrow';

COMMIT;

BEGIN;

-- Retain the original IDs and a full source snapshot for imported balances and
-- frozen payroll reports. Escrow principal and collections now have their own ledger.
CREATE TABLE driver_escrows (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    driver_id UUID REFERENCES drivers(id) ON DELETE SET NULL,
    driver_name TEXT NOT NULL,
    start_date DATE NOT NULL,
    amount NUMERIC(14,2) NOT NULL CHECK (amount >= 0),
    opening_paid NUMERIC(14,2) NOT NULL DEFAULT 0 CHECK (opening_paid >= 0 AND opening_paid <= amount),
    balance_version INTEGER NOT NULL DEFAULT 1,
    created_by UUID REFERENCES app_users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    legacy_expense JSONB,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX driver_escrows_driver_idx ON driver_escrows(driver_id);
CREATE TABLE driver_escrow_payments (
    escrow_id UUID NOT NULL REFERENCES driver_escrows(id) ON DELETE RESTRICT,
    week_start DATE NOT NULL CHECK (extract(isodow FROM week_start)=1),
    amount NUMERIC(14,2) NOT NULL CHECK (amount >= 0),
    updated_by UUID REFERENCES app_users(id) ON DELETE SET NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY(escrow_id,week_start)
);
CREATE INDEX driver_escrow_payments_week_idx ON driver_escrow_payments(week_start);

-- Include older manually entered escrow records, not only the system-tagged
-- onboarding entries. Settled imports keep their paid opening balance.
LOCK TABLE expenses, expense_payments IN ACCESS EXCLUSIVE MODE;
INSERT INTO driver_escrows(id,driver_id,driver_name,start_date,amount,opening_paid,
    balance_version,created_by,created_at,updated_at,legacy_expense)
SELECT e.id,coalesce(e.charge_driver_id,e.driver_id),coalesce(e.driver_name,'Unknown driver'),
    greatest(coalesce(d.hire_date,DATE '2026-09-28'),DATE '2026-09-28'),coalesce(e.amount,0),
    CASE WHEN e.driver_settled THEN greatest(0,coalesce(e.amount,0)-coalesce(p.paid,0)) ELSE 0 END,
    e.balance_version,e.created_by,e.created_at,e.updated_at,to_jsonb(e)
FROM expenses e
LEFT JOIN drivers d ON d.id=coalesce(e.charge_driver_id,e.driver_id)
LEFT JOIN LATERAL (SELECT sum(amount) paid FROM expense_payments WHERE expense_id=e.id) p ON true
WHERE e.system_kind='driver_escrow'
   OR (lower(btrim(coalesce(e.expense_type,''))) IN ('escrow','escrow payment')
       AND lower(btrim(coalesce(e.covered_by,'')))='driver');
INSERT INTO driver_escrow_payments(escrow_id,week_start,amount,updated_by,updated_at)
SELECT p.expense_id,p.week_start,p.amount,p.updated_by,p.updated_at
FROM expense_payments p JOIN driver_escrows e ON e.id=p.expense_id;

-- This is a ledger transfer, not a payroll correction. Frozen report JSON and
-- audit snapshots remain untouched. The locks prevent writes during transfer.
ALTER TABLE expense_payments DISABLE TRIGGER USER;
DELETE FROM expense_payments p USING driver_escrows e WHERE p.expense_id=e.id;
ALTER TABLE expense_payments ENABLE TRIGGER USER;
DELETE FROM expenses e USING driver_escrows s WHERE e.id=s.id;

-- Keep the old column for schema compatibility, but it no longer controls escrow.
ALTER TABLE driver_escrow_settings ALTER COLUMN category_id DROP NOT NULL;
UPDATE driver_escrow_settings SET category_id=NULL;
UPDATE expense_settings SET active=false,version=version+1
WHERE kind='name' AND lower(btrim(name)) IN ('escrow','escrow payment');

CREATE FUNCTION protect_escrow_payment() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE e driver_escrows%ROWTYPE; target uuid; target_week date; paid numeric;
BEGIN
    IF TG_OP='DELETE' THEN target:=OLD.escrow_id; target_week:=OLD.week_start;
    ELSE target:=NEW.escrow_id; target_week:=NEW.week_start; END IF;
    IF TG_OP='UPDATE' AND (NEW.escrow_id<>OLD.escrow_id OR NEW.week_start<>OLD.week_start) THEN
        RAISE EXCEPTION 'Escrow payment identity cannot change' USING ERRCODE='23514';
    END IF;
    SELECT * INTO e FROM driver_escrows WHERE id=target FOR UPDATE;
    IF EXISTS(SELECT 1 FROM payroll_settlements WHERE driver_id=e.driver_id AND week_start=target_week AND finalized) THEN
        RAISE EXCEPTION 'Reopen this driver settlement before editing escrow' USING ERRCODE='23514';
    END IF;
    IF TG_OP<>'DELETE' THEN
        SELECT coalesce(sum(amount),0) INTO paid FROM driver_escrow_payments WHERE escrow_id=target AND week_start<>target_week;
        IF NEW.amount+paid+e.opening_paid>e.amount OR (target_week+6<e.start_date AND NOT EXISTS(SELECT 1 FROM driver_escrow_payments WHERE escrow_id=target AND week_start=target_week)) THEN
            RAISE EXCEPTION 'Escrow payment exceeds the available balance or precedes its start' USING ERRCODE='23514';
        END IF;
    END IF;
    UPDATE driver_escrows SET balance_version=balance_version+1,updated_at=now() WHERE id=target;
    IF TG_OP='DELETE' THEN RETURN OLD; END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER escrow_payment_guard BEFORE INSERT OR UPDATE OR DELETE ON driver_escrow_payments
FOR EACH ROW EXECUTE FUNCTION protect_escrow_payment();

-- Older application releases must not recreate expense-backed escrow after the
-- transfer. Failing their transaction is safer than creating a second balance.
CREATE FUNCTION prevent_legacy_escrow() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.system_kind='driver_escrow' OR (lower(btrim(coalesce(NEW.expense_type,''))) IN ('escrow','escrow payment') AND lower(btrim(coalesce(NEW.covered_by,'')))='driver') THEN
        RAISE EXCEPTION 'Use Accounting Escrow for driver escrow balances' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER expenses_legacy_escrow_guard BEFORE INSERT OR UPDATE ON expenses
FOR EACH ROW EXECUTE FUNCTION prevent_legacy_escrow();

DO $$ BEGIN
    IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
        ALTER TABLE driver_escrows OWNER TO mserp_app;
        ALTER TABLE driver_escrow_payments OWNER TO mserp_app;
    END IF;
END $$;
COMMIT;

BEGIN;

CREATE TABLE driver_escrow_releases (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    escrow_id UUID NOT NULL REFERENCES driver_escrows(id) ON DELETE RESTRICT,
    week_start DATE NOT NULL CHECK (extract(isodow FROM week_start)=1),
    amount NUMERIC(14,2) NOT NULL CHECK (amount > 0),
    cancelled BOOLEAN NOT NULL DEFAULT false,
    version INTEGER NOT NULL DEFAULT 1,
    updated_by UUID REFERENCES app_users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX driver_escrow_releases_escrow_idx ON driver_escrow_releases(escrow_id,week_start);
CREATE INDEX driver_escrow_releases_week_idx ON driver_escrow_releases(week_start) WHERE NOT cancelled;
CREATE TABLE driver_escrow_release_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    release_id UUID NOT NULL REFERENCES driver_escrow_releases(id),
    actor_id UUID REFERENCES app_users(id) ON DELETE SET NULL,
    before_value JSONB,
    after_value JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- All scheduled credits reserve funds, and none may spend a later collection.
CREATE FUNCTION assert_escrow_release_funding(target uuid) RETURNS void LANGUAGE plpgsql AS $$
DECLARE initial_paid numeric;
BEGIN
    SELECT opening_paid INTO initial_paid FROM driver_escrows WHERE id=target FOR UPDATE;
    IF EXISTS (
        SELECT 1 FROM driver_escrow_releases r WHERE r.escrow_id=target AND NOT r.cancelled
        AND (SELECT coalesce(sum(x.amount),0) FROM driver_escrow_releases x WHERE x.escrow_id=target AND NOT x.cancelled AND x.week_start<=r.week_start)
          > initial_paid+(SELECT coalesce(sum(p.amount),0) FROM driver_escrow_payments p WHERE p.escrow_id=target AND p.week_start<=r.week_start)
    ) THEN
        RAISE EXCEPTION 'Release exceeds escrow funds available in the selected week' USING ERRCODE='23514';
    END IF;
END $$;

CREATE FUNCTION protect_escrow_release() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE driver uuid; current_week date := date_trunc('week',now() AT TIME ZONE 'America/New_York')::date;
BEGIN
    IF TG_OP='DELETE' THEN
        RAISE EXCEPTION 'Cancel escrow releases instead of deleting history' USING ERRCODE='23514';
    END IF;
    SELECT driver_id INTO driver FROM driver_escrows WHERE id=NEW.escrow_id FOR UPDATE;
    IF TG_OP='UPDATE' THEN
        IF NEW.id<>OLD.id OR NEW.escrow_id<>OLD.escrow_id OR OLD.cancelled OR OLD.week_start<current_week THEN
            RAISE EXCEPTION 'This release can no longer be changed' USING ERRCODE='23514';
        END IF;
        IF EXISTS(SELECT 1 FROM payroll_settlements WHERE driver_id=driver AND week_start=OLD.week_start AND finalized) THEN
            RAISE EXCEPTION 'Reopen this driver settlement before changing its release' USING ERRCODE='23514';
        END IF;
        NEW.version := OLD.version+1;
    END IF;
    IF driver IS NULL OR NEW.week_start<current_week THEN
        RAISE EXCEPTION 'Select an existing driver and the current or a future week' USING ERRCODE='23514';
    END IF;
    IF EXISTS(SELECT 1 FROM payroll_settlements WHERE driver_id=driver AND week_start=NEW.week_start AND finalized) THEN
        RAISE EXCEPTION 'Reopen this driver settlement before changing its release' USING ERRCODE='23514';
    END IF;
    UPDATE driver_escrows SET balance_version=balance_version+1,updated_at=now() WHERE id=NEW.escrow_id;
    NEW.updated_at := now();
    RETURN NEW;
END $$;
CREATE TRIGGER escrow_release_guard BEFORE INSERT OR UPDATE OR DELETE ON driver_escrow_releases
FOR EACH ROW EXECUTE FUNCTION protect_escrow_release();

CREATE FUNCTION audit_escrow_release() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    PERFORM assert_escrow_release_funding(NEW.escrow_id);
    INSERT INTO driver_escrow_release_events(release_id,actor_id,before_value,after_value)
    VALUES(NEW.id,NEW.updated_by,CASE WHEN TG_OP='UPDATE' THEN to_jsonb(OLD) ELSE NULL END,to_jsonb(NEW));
    RETURN NULL;
END $$;
CREATE TRIGGER escrow_release_audit AFTER INSERT OR UPDATE ON driver_escrow_releases
FOR EACH ROW EXECUTE FUNCTION audit_escrow_release();

CREATE FUNCTION protect_released_escrow_funds() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_TABLE_NAME='driver_escrows' THEN PERFORM assert_escrow_release_funding(NEW.id);
    ELSIF TG_OP='DELETE' THEN PERFORM assert_escrow_release_funding(OLD.escrow_id);
    ELSE PERFORM assert_escrow_release_funding(NEW.escrow_id); END IF;
    RETURN NULL;
END $$;
CREATE TRIGGER escrow_payment_release_funds AFTER INSERT OR UPDATE OR DELETE ON driver_escrow_payments
FOR EACH ROW EXECUTE FUNCTION protect_released_escrow_funds();
CREATE TRIGGER escrow_opening_release_funds AFTER UPDATE OF opening_paid ON driver_escrows
FOR EACH ROW EXECUTE FUNCTION protect_released_escrow_funds();

DO $$ BEGIN
    IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
        ALTER TABLE driver_escrow_releases OWNER TO mserp_app;
        ALTER TABLE driver_escrow_release_events OWNER TO mserp_app;
    END IF;
END $$;
COMMIT;

BEGIN;

-- A release reopens collection capacity starting the following week. Reserve
-- already saved later payments so editing an earlier week cannot overcollect.
CREATE FUNCTION escrow_collection_available(target uuid, target_week date) RETURNS numeric
LANGUAGE sql STABLE AS $$
    SELECT min(e.amount-e.opening_paid
        -coalesce((SELECT sum(p.amount) FROM driver_escrow_payments p
          WHERE p.escrow_id=target AND p.week_start<=w.week_start AND p.week_start<>target_week),0)
        +coalesce((SELECT sum(r.amount) FROM driver_escrow_releases r
          WHERE r.escrow_id=target AND NOT r.cancelled AND r.week_start<w.week_start),0))
    FROM driver_escrows e CROSS JOIN (
        SELECT target_week week_start UNION
        SELECT week_start FROM driver_escrow_payments WHERE escrow_id=target AND week_start>target_week
    ) w WHERE e.id=target
$$;

CREATE FUNCTION assert_escrow_collection_limit(target uuid) RETURNS void LANGUAGE plpgsql AS $$
BEGIN
    PERFORM 1 FROM driver_escrows WHERE id=target FOR UPDATE;
    IF EXISTS (SELECT 1 FROM driver_escrow_payments p WHERE p.escrow_id=target
        AND p.amount>escrow_collection_available(target,p.week_start)) THEN
        RAISE EXCEPTION 'Escrow repayments exceed the required balance; adjust later repayments before changing this release' USING ERRCODE='23514';
    END IF;
END $$;

CREATE OR REPLACE FUNCTION protect_escrow_payment() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE e driver_escrows%ROWTYPE; target uuid; target_week date;
BEGIN
    IF TG_OP='DELETE' THEN target:=OLD.escrow_id; target_week:=OLD.week_start;
    ELSE target:=NEW.escrow_id; target_week:=NEW.week_start; END IF;
    IF TG_OP='UPDATE' AND (NEW.escrow_id<>OLD.escrow_id OR NEW.week_start<>OLD.week_start) THEN
        RAISE EXCEPTION 'Escrow payment identity cannot change' USING ERRCODE='23514';
    END IF;
    SELECT * INTO e FROM driver_escrows WHERE id=target FOR UPDATE;
    IF EXISTS(SELECT 1 FROM payroll_settlements WHERE driver_id=e.driver_id AND week_start=target_week AND finalized) THEN
        RAISE EXCEPTION 'Reopen this driver settlement before editing escrow' USING ERRCODE='23514';
    END IF;
    IF TG_OP<>'DELETE' THEN
        IF NEW.amount>escrow_collection_available(target,target_week)
          OR (target_week+6<e.start_date AND NOT EXISTS(SELECT 1 FROM driver_escrow_payments WHERE escrow_id=target AND week_start=target_week)) THEN
            RAISE EXCEPTION 'Escrow payment exceeds the available balance or precedes its start' USING ERRCODE='23514';
        END IF;
    END IF;
    UPDATE driver_escrows SET balance_version=balance_version+1,updated_at=now() WHERE id=target;
    IF TG_OP='DELETE' THEN RETURN OLD; END IF;
    RETURN NEW;
END $$;

CREATE OR REPLACE FUNCTION audit_escrow_release() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    PERFORM assert_escrow_release_funding(NEW.escrow_id);
    PERFORM assert_escrow_collection_limit(NEW.escrow_id);
    INSERT INTO driver_escrow_release_events(release_id,actor_id,before_value,after_value)
    VALUES(NEW.id,NEW.updated_by,CASE WHEN TG_OP='UPDATE' THEN to_jsonb(OLD) ELSE NULL END,to_jsonb(NEW));
    RETURN NULL;
END $$;

CREATE OR REPLACE FUNCTION protect_released_escrow_funds() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE target uuid;
BEGIN
    IF TG_TABLE_NAME='driver_escrows' THEN target:=NEW.id;
    ELSIF TG_OP='DELETE' THEN target:=OLD.escrow_id;
    ELSE target:=NEW.escrow_id; END IF;
    PERFORM assert_escrow_release_funding(target);
    PERFORM assert_escrow_collection_limit(target);
    RETURN NULL;
END $$;
DROP TRIGGER escrow_opening_release_funds ON driver_escrows;
CREATE TRIGGER escrow_opening_release_funds AFTER UPDATE OF opening_paid,amount ON driver_escrows
FOR EACH ROW EXECUTE FUNCTION protect_released_escrow_funds();

COMMIT;

BEGIN;

-- A reactivated driver's current roster starts independently of old assignments.
ALTER TABLE drivers ADD COLUMN roster_start_week date
    CHECK (extract(isodow FROM roster_start_week)=1);
-- Only backfill known starts; migration observation dates are not start dates.
UPDATE drivers d SET roster_start_week=date_trunc('week',s.started_at AT TIME ZONE 'America/New_York')::date
FROM (
    SELECT driver_id,min(assigned_at) started_at FROM (
        SELECT driver_id,assigned_at FROM driver_dispatcher_assignments
        UNION ALL SELECT driver_id,assigned_at FROM truck_driver_assignments
    ) h GROUP BY driver_id
) s WHERE s.driver_id=d.id AND NOT EXISTS (
    SELECT 1 FROM driver_dispatcher_assignments h WHERE h.driver_id=d.id AND NOT h.start_known
);

-- Assignment changes may use any Monday, but cannot rewrite settled periods.
CREATE OR REPLACE FUNCTION assert_assignment_payroll_open(d uuid, t uuid, effective_week date)
RETURNS void LANGUAGE plpgsql AS $$
BEGIN
    IF EXISTS (SELECT 1 FROM payroll_settlements
        WHERE driver_id=d AND week_start>=effective_week AND finalized) THEN
        RAISE EXCEPTION 'Reopen finalized payroll for this driver from the selected assignment week onward before changing assignments' USING ERRCODE='23514';
    END IF;
    IF EXISTS (SELECT 1 FROM investor_pay_weeks
        WHERE truck_id=t AND week_start>=effective_week AND finalized) THEN
        RAISE EXCEPTION 'Reopen finalized investor payroll for this truck from the selected assignment week onward before changing assignments' USING ERRCODE='23514';
    END IF;
END $$;

CREATE FUNCTION record_driver_roster_start() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE effective_week date := nullif(current_setting('mserp.assignment_week',true),'')::date;
BEGIN
    IF effective_week IS NULL THEN RETURN NEW; END IF;
    IF TG_OP='INSERT' THEN
        NEW.roster_start_week := effective_week;
    ELSIF NEW.active AND NOT OLD.active THEN
        PERFORM assert_assignment_payroll_open(NEW.id,NULL,effective_week);
        NEW.roster_start_week := effective_week;
    ELSIF NEW.dispatcher_id IS DISTINCT FROM OLD.dispatcher_id AND NEW.roster_start_week>effective_week THEN
        -- An explicit earlier assignment also brings the current roster forward.
        NEW.roster_start_week := effective_week;
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER drivers_roster_start BEFORE INSERT OR UPDATE OF active,dispatcher_id ON drivers
FOR EACH ROW EXECUTE FUNCTION record_driver_roster_start();

-- Imports without an explicit accounting week retain their observed timestamps.
CREATE OR REPLACE FUNCTION record_driver_dispatcher_assignment() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE changed_at timestamptz := clock_timestamp();
DECLARE effective_week date := nullif(current_setting('mserp.assignment_week',true),'')::date;
BEGIN
    IF TG_OP='UPDATE' AND NEW.dispatcher_id IS NOT DISTINCT FROM OLD.dispatcher_id THEN
        RETURN NEW;
    END IF;
    IF effective_week IS NOT NULL THEN
        changed_at := effective_week::timestamp AT TIME ZONE 'America/New_York';
        PERFORM assert_assignment_payroll_open(NEW.id,NULL,effective_week);
        -- Superseded changes remain as zero-length history records. Earlier
        -- periods keep their assignments up to the newly selected boundary.
        UPDATE driver_dispatcher_assignments SET
            assigned_at=least(assigned_at,changed_at),
            unassigned_at=CASE WHEN unassigned_at IS NULL THEN NULL ELSE least(unassigned_at,changed_at) END
        WHERE driver_id=NEW.id AND (assigned_at>=changed_at OR unassigned_at>=changed_at);
    END IF;
    UPDATE driver_dispatcher_assignments SET unassigned_at=changed_at
    WHERE driver_id=NEW.id AND unassigned_at IS NULL;
    INSERT INTO driver_dispatcher_assignments(driver_id,dispatcher_id,dispatcher_name,assigned_at,source)
    VALUES(NEW.id,NEW.dispatcher_id,
        coalesce((SELECT full_name FROM dispatchers WHERE id=NEW.dispatcher_id),'Unassigned'),
        changed_at,coalesce(current_setting('mserp.assignment_source',true),''));
    RETURN NEW;
END $$;

COMMIT;

BEGIN;

-- Existing freeform notes stay on profiles as undated legacy context.
CREATE TABLE profile_notes (
    id uuid PRIMARY KEY,
    driver_id uuid REFERENCES drivers(id) ON DELETE CASCADE,
    truck_id uuid REFERENCES trucks(id) ON DELETE CASCADE,
    body text NOT NULL CHECK (length(btrim(body)) BETWEEN 1 AND 5000),
    actor_id uuid REFERENCES app_users(id) ON DELETE SET NULL,
    actor_name text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (num_nonnulls(driver_id, truck_id) = 1)
);
CREATE INDEX profile_notes_driver ON profile_notes(driver_id, created_at DESC);
CREATE INDEX profile_notes_truck ON profile_notes(truck_id, created_at DESC);
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'mserp_app') THEN
        ALTER TABLE profile_notes OWNER TO mserp_app;
    END IF;
END $$;

COMMIT;


-- Preserve source labels for display, but bind each imported load to a stable
-- local truck identity once. A reused/ambiguous label is never guessed.
CREATE TABLE truck_unit_aliases (
    unit_key text NOT NULL CHECK (unit_key <> '' AND unit_key=upper(btrim(unit_key))),
    truck_id uuid NOT NULL REFERENCES trucks(id) ON DELETE CASCADE,
    PRIMARY KEY(unit_key, truck_id)
);
CREATE INDEX truck_unit_aliases_truck_idx ON truck_unit_aliases(truck_id);
INSERT INTO truck_unit_aliases(unit_key,truck_id)
SELECT upper(btrim(unit_number)),id FROM trucks WHERE btrim(unit_number)<>''
UNION
SELECT upper(btrim(h.truck_unit)),h.truck_id FROM truck_ownership_history h
JOIN trucks t ON t.id=h.truck_id WHERE btrim(h.truck_unit)<>'';

ALTER TABLE loads ADD COLUMN truck_id uuid REFERENCES trucks(id) ON DELETE SET NULL;
CREATE INDEX loads_truck_id_idx ON loads(truck_id);
CREATE INDEX loads_unlinked_truck_label_idx ON loads(upper(btrim(truck_unit))) WHERE truck_id IS NULL;

CREATE FUNCTION resolve_load_truck(unit_label text) RETURNS uuid LANGUAGE sql STABLE AS $$
    SELECT CASE WHEN count(*)=1 THEN (array_agg(truck_id))[1] END
    FROM truck_unit_aliases WHERE unit_key=upper(btrim(unit_label))
$$;

UPDATE loads l SET truck_id=m.truck_id FROM (
    SELECT unit_key,(array_agg(truck_id))[1] truck_id FROM truck_unit_aliases
    GROUP BY unit_key HAVING count(*)=1
) m WHERE upper(btrim(l.truck_unit))=m.unit_key;

CREATE FUNCTION bind_load_truck() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='UPDATE' AND OLD.truck_id IS NOT NULL
       AND upper(btrim(NEW.truck_unit)) IS NOT DISTINCT FROM upper(btrim(OLD.truck_unit)) THEN
        NEW.truck_id := OLD.truck_id;
    ELSE
        NEW.truck_id := resolve_load_truck(NEW.truck_unit);
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER loads_bind_truck BEFORE INSERT OR UPDATE OF truck_unit ON loads
FOR EACH ROW EXECUTE FUNCTION bind_load_truck();

CREATE FUNCTION record_truck_unit_alias() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO truck_unit_aliases(unit_key,truck_id) VALUES(upper(btrim(NEW.unit_number)),NEW.id)
    ON CONFLICT DO NOTHING;
    IF TG_OP='UPDATE' THEN
        INSERT INTO truck_unit_aliases(unit_key,truck_id) VALUES(upper(btrim(OLD.unit_number)),OLD.id)
        ON CONFLICT DO NOTHING;
    END IF;
    -- A truck created after its source loads can resolve only previously
    -- unlinked records. Existing identities survive later label reuse.
    UPDATE loads SET truck_id=NEW.id
    WHERE truck_id IS NULL AND upper(btrim(truck_unit))=upper(btrim(NEW.unit_number))
      AND resolve_load_truck(truck_unit)=NEW.id;
    RETURN NEW;
END $$;
CREATE TRIGGER trucks_unit_alias AFTER INSERT OR UPDATE OF unit_number ON trucks
FOR EACH ROW EXECUTE FUNCTION record_truck_unit_alias();

DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
        ALTER TABLE truck_unit_aliases OWNER TO mserp_app;
    END IF;
END $$;

ALTER TABLE app_users ADD COLUMN theme TEXT NOT NULL DEFAULT 'default'
    CHECK (theme IN ('default', 'solarized-light', 'solarized-dark', 'monokai', 'monokai-dimmed', 'dark-modern', 'default-light'));

BEGIN;

ALTER TABLE custom_tasks ADD COLUMN completed_by UUID REFERENCES app_users(id) ON DELETE SET NULL;
ALTER TABLE custom_tasks ADD COLUMN completed_by_name TEXT NOT NULL DEFAULT '';

-- Source identities deliberately have no FK: completed work survives source deletion.
CREATE TABLE system_task_records (
    id UUID PRIMARY KEY,
    kind TEXT NOT NULL REFERENCES system_task_assignments(kind),
    title TEXT NOT NULL,
    notes TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL,
    completed_at TIMESTAMPTZ,
    completed_by_name TEXT NOT NULL DEFAULT '',
    outcome TEXT NOT NULL DEFAULT ''
);
CREATE INDEX system_task_records_status_idx ON system_task_records((completed_at IS NOT NULL),created_at DESC,id);

INSERT INTO system_task_records(id,kind,title,created_at,completed_at,completed_by_name,outcome)
SELECT i.id,'driver_onboarding','Set up ' || coalesce(i.driver_data->>'fullName','driver'),i.received_at,
 coalesce(i.completed_at,i.terminated_at),coalesce(u.username,CASE WHEN i.terminated_at IS NOT NULL AND i.completed_at IS NULL THEN 'System' ELSE '' END),
 CASE WHEN i.completed_at IS NULL AND i.terminated_at IS NOT NULL THEN 'Cancelled by termination' ELSE '' END
FROM fleetscope_driver_intake i LEFT JOIN app_users u ON u.id=i.completed_by;

INSERT INTO system_task_records(id,kind,title,created_at,completed_at,completed_by_name)
SELECT l.id,'relay_review','Review Relay account: ' || coalesce(nullif(concat_ws(' ',l.relay_first_name,l.relay_last_name),''),l.relay_driver_id),
 l.created_at,CASE WHEN l.driver_id IS NOT NULL THEN v.reviewed_at END,coalesce(u.username,'')
FROM relay_driver_links l
LEFT JOIN LATERAL (SELECT reviewed_at,reviewed_by FROM relay_identity_reviews WHERE identity_id=l.id AND action='link' ORDER BY reviewed_at DESC LIMIT 1) v ON true
LEFT JOIN app_users u ON u.id=v.reviewed_by
WHERE l.driver_id IS NULL OR v.reviewed_at IS NOT NULL;

CREATE FUNCTION sync_intake_task() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 INSERT INTO system_task_records(id,kind,title,created_at,completed_at,completed_by_name,outcome)
 VALUES(NEW.id,'driver_onboarding','Set up ' || coalesce(NEW.driver_data->>'fullName','driver'),NEW.received_at,
 coalesce(NEW.completed_at,NEW.terminated_at),coalesce((SELECT username FROM app_users WHERE id=NEW.completed_by),CASE WHEN NEW.terminated_at IS NOT NULL AND NEW.completed_at IS NULL THEN 'System' ELSE '' END),
 CASE WHEN NEW.completed_at IS NULL AND NEW.terminated_at IS NOT NULL THEN 'Cancelled by termination' ELSE '' END)
 ON CONFLICT(id) DO UPDATE SET title=excluded.title,completed_at=excluded.completed_at,
 completed_by_name=CASE WHEN system_task_records.completed_at IS NOT NULL THEN system_task_records.completed_by_name ELSE excluded.completed_by_name END,outcome=excluded.outcome;
 RETURN NEW;
END $$;
CREATE TRIGGER intake_task AFTER INSERT OR UPDATE ON fleetscope_driver_intake FOR EACH ROW EXECUTE FUNCTION sync_intake_task();

CREATE FUNCTION sync_relay_task() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.driver_id IS NULL THEN
  INSERT INTO system_task_records(id,kind,title,notes,created_at)
  VALUES(NEW.id,'relay_review','Review Relay account: ' || coalesce(nullif(concat_ws(' ',NEW.relay_first_name,NEW.relay_last_name),''),NEW.relay_driver_id),concat_ws(' · ',NEW.relay_email,NEW.relay_phone,NEW.relay_driver_id),NEW.created_at)
  ON CONFLICT(id) DO UPDATE SET title=excluded.title,notes=excluded.notes;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER relay_task AFTER INSERT OR UPDATE ON relay_driver_links FOR EACH ROW EXECUTE FUNCTION sync_relay_task();

CREATE FUNCTION complete_relay_task() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.action='link' THEN
  UPDATE system_task_records SET completed_at=NEW.reviewed_at,
   completed_by_name=coalesce((SELECT username FROM app_users WHERE id=NEW.reviewed_by),'')
   WHERE id=NEW.identity_id AND completed_at IS NULL;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER relay_task_completed AFTER INSERT ON relay_identity_reviews FOR EACH ROW EXECUTE FUNCTION complete_relay_task();

-- One committed notification per transaction wakes the shared browser stream.
CREATE FUNCTION notify_task_change() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='UPDATE' AND NEW IS NOT DISTINCT FROM OLD THEN RETURN NULL; END IF;
 PERFORM pg_notify('mserp_tasks','');
 RETURN NULL;
END $$;
CREATE TRIGGER custom_task_changed AFTER INSERT OR UPDATE OR DELETE ON custom_tasks FOR EACH ROW EXECUTE FUNCTION notify_task_change();
CREATE TRIGGER system_task_changed AFTER INSERT OR UPDATE OR DELETE ON system_task_records FOR EACH ROW EXECUTE FUNCTION notify_task_change();
CREATE TRIGGER task_assignment_changed AFTER UPDATE ON system_task_assignments FOR EACH ROW EXECUTE FUNCTION notify_task_change();
CREATE TRIGGER task_user_changed AFTER UPDATE OR DELETE ON app_users FOR EACH ROW EXECUTE FUNCTION notify_task_change();
CREATE TRIGGER task_role_changed AFTER UPDATE OR DELETE ON app_roles FOR EACH ROW EXECUTE FUNCTION notify_task_change();

DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
  ALTER TABLE system_task_records OWNER TO mserp_app;
 END IF;
END $$;
COMMIT;

BEGIN;
-- Completion remains authoritative for older clients using completed_at.
ALTER TABLE custom_tasks ADD COLUMN in_process BOOLEAN NOT NULL DEFAULT false;
COMMIT;

BEGIN;

-- Derived reporting evidence is maintained for every writer, including old
-- binaries during deployment. Source timestamps, prompts and payloads remain intact.
ALTER TABLE fuel_transactions ADD COLUMN reporting_timezone text;
ALTER TABLE fuel_transactions ADD COLUMN purchased_on date;
ALTER TABLE fuel_transactions ADD COLUMN reporting_timezone_valid boolean;
ALTER TABLE fuel_transactions ADD COLUMN reported_truck_units text[];

CREATE FUNCTION fuel_reporting_fields() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE zone text;
BEGIN
    IF TG_OP='UPDATE' AND NEW.purchased_at IS NOT DISTINCT FROM OLD.purchased_at
       AND NEW.timezone IS NOT DISTINCT FROM OLD.timezone AND NEW.prompts IS NOT DISTINCT FROM OLD.prompts
       AND OLD.purchased_on IS NOT NULL THEN
        NEW.reporting_timezone := OLD.reporting_timezone;
        NEW.purchased_on := OLD.purchased_on;
        NEW.reporting_timezone_valid := OLD.reporting_timezone_valid;
        NEW.reported_truck_units := OLD.reported_truck_units;
        RETURN NEW;
    END IF;
    zone := CASE NEW.timezone
        WHEN 'US/Eastern' THEN 'America/New_York' WHEN 'US/Central' THEN 'America/Chicago'
        WHEN 'US/Mountain' THEN 'America/Denver' WHEN 'US/Pacific' THEN 'America/Los_Angeles'
        WHEN 'US/Arizona' THEN 'America/Phoenix' WHEN 'US/Alaska' THEN 'America/Anchorage'
        WHEN 'US/Aleutian' THEN 'America/Adak' WHEN 'US/Hawaii' THEN 'Pacific/Honolulu'
        WHEN 'US/East-Indiana' THEN 'America/Indiana/Indianapolis' WHEN 'US/Indiana-Starke' THEN 'America/Indiana/Knox'
        WHEN 'US/Michigan' THEN 'America/Detroit' WHEN 'US/Samoa' THEN 'Pacific/Pago_Pago'
        ELSE coalesce((SELECT name FROM pg_timezone_names WHERE name=nullif(NEW.timezone,'') LIMIT 1),'America/New_York') END;
    NEW.reporting_timezone := zone;
    NEW.purchased_on := (NEW.purchased_at AT TIME ZONE zone)::date;
    NEW.reporting_timezone_valid := EXISTS(SELECT 1 FROM pg_timezone_names WHERE name=NEW.timezone);
    SELECT coalesce(array_agg(DISTINCT upper(btrim(p->>'value')) ORDER BY upper(btrim(p->>'value'))),'{}'::text[])
      INTO NEW.reported_truck_units FROM jsonb_array_elements(NEW.prompts) p
      WHERE lower(btrim(p->>'label'))='truck #' AND btrim(p->>'value')<>'';
    RETURN NEW;
END $$;
-- Materialize the timezone catalog once for the historical backfill.
WITH zones AS MATERIALIZED (SELECT name FROM pg_timezone_names), derived AS (
 SELECT f.id,CASE f.timezone
        WHEN 'US/Eastern' THEN 'America/New_York' WHEN 'US/Central' THEN 'America/Chicago'
        WHEN 'US/Mountain' THEN 'America/Denver' WHEN 'US/Pacific' THEN 'America/Los_Angeles'
        WHEN 'US/Arizona' THEN 'America/Phoenix' WHEN 'US/Alaska' THEN 'America/Anchorage'
        WHEN 'US/Aleutian' THEN 'America/Adak' WHEN 'US/Hawaii' THEN 'Pacific/Honolulu'
        WHEN 'US/East-Indiana' THEN 'America/Indiana/Indianapolis' WHEN 'US/Indiana-Starke' THEN 'America/Indiana/Knox'
        WHEN 'US/Michigan' THEN 'America/Detroit' WHEN 'US/Samoa' THEN 'Pacific/Pago_Pago'
        ELSE coalesce(z.name,'America/New_York') END zone,z.name IS NOT NULL valid,
 ARRAY(SELECT DISTINCT upper(btrim(p->>'value')) FROM jsonb_array_elements(f.prompts) p
 WHERE lower(btrim(p->>'label'))='truck #' AND btrim(p->>'value')<>'' ORDER BY 1) units
 FROM fuel_transactions f LEFT JOIN zones z ON z.name=nullif(f.timezone,'')
)
UPDATE fuel_transactions f SET reporting_timezone=d.zone,purchased_on=(f.purchased_at AT TIME ZONE d.zone)::date,
 reporting_timezone_valid=d.valid,reported_truck_units=d.units FROM derived d WHERE f.id=d.id;
CREATE TRIGGER fuel_reporting_fields_before BEFORE INSERT OR UPDATE ON fuel_transactions
FOR EACH ROW EXECUTE FUNCTION fuel_reporting_fields();
ALTER TABLE fuel_transactions ALTER COLUMN reporting_timezone SET NOT NULL;
ALTER TABLE fuel_transactions ALTER COLUMN purchased_on SET NOT NULL;
ALTER TABLE fuel_transactions ALTER COLUMN reporting_timezone_valid SET NOT NULL;
ALTER TABLE fuel_transactions ALTER COLUMN reported_truck_units SET NOT NULL;
CREATE INDEX fuel_transactions_reporting_date_idx ON fuel_transactions(purchased_on,driver_id);
CREATE INDEX truck_assignments_truck_history_idx ON truck_driver_assignments(truck_id,assigned_at);
DO $$ BEGIN IF to_regclass('payroll_settlements') IS NOT NULL THEN CREATE INDEX payroll_settlements_week_idx ON payroll_settlements(week_start); END IF; END $$;
DO $$ BEGIN IF to_regclass('investor_pay_weeks') IS NOT NULL THEN CREATE INDEX investor_pay_weeks_week_idx ON investor_pay_weeks(week_start); END IF; END $$;
CREATE INDEX loads_pickup_id_idx ON loads(pickup_time DESC,id DESC);
CREATE INDEX loads_driver_id_idx ON loads(driver_id);
CREATE INDEX loads_coverage_unit_idx ON loads(upper(regexp_replace(btrim(truck_unit),'[[:space:]]+',' ','g')));
CREATE INDEX loads_coverage_team_idx ON loads(lower(regexp_replace(btrim(team_driver_name),'[[:space:]]+',' ','g')));
ANALYZE fuel_transactions;
COMMIT;

BEGIN;

ALTER TABLE drivers ADD COLUMN status text NOT NULL DEFAULT 'active';
ALTER TABLE drivers ADD COLUMN termination_date date;
ALTER TABLE drivers ADD COLUMN termination_id uuid;
UPDATE drivers d SET status='terminated', termination_id=gen_random_uuid(),
 termination_date=coalesce((SELECT min(t.termination_date) FROM fleetscope_driver_terminations t WHERE t.driver_id=d.id),(now() AT TIME ZONE 'America/New_York')::date-30)
 WHERE NOT active;
ALTER TABLE drivers ADD CONSTRAINT drivers_status_check CHECK(status IN ('active','vacation','home','terminated'));

-- Older clients still write active; preserve named active statuses on those writes.
CREATE FUNCTION sync_driver_status() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='INSERT' THEN
  IF NOT NEW.active AND NEW.status='active' THEN NEW.status='terminated'; END IF;
 ELSIF NEW.status IS NOT DISTINCT FROM OLD.status AND NEW.active IS DISTINCT FROM OLD.active THEN
  NEW.status=CASE WHEN NEW.active THEN 'active' ELSE 'terminated' END;
 END IF;
 NEW.active=NEW.status<>'terminated';
 IF NEW.status='terminated' THEN
  IF TG_OP='INSERT' THEN
   NEW.termination_id=coalesce(NEW.termination_id,gen_random_uuid());
  ELSIF OLD.status<>'terminated' THEN
   NEW.termination_id=gen_random_uuid();
  END IF;
  NEW.termination_date=coalesce(NEW.termination_date,(now() AT TIME ZONE 'America/New_York')::date);
 ELSE
  NEW.termination_date=NULL;
  NEW.termination_id=NULL;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER driver_status BEFORE INSERT OR UPDATE ON drivers FOR EACH ROW EXECUTE FUNCTION sync_driver_status();

ALTER TABLE system_task_assignments DROP CONSTRAINT system_task_assignments_kind_check;
ALTER TABLE system_task_assignments ADD CONSTRAINT system_task_assignments_kind_check CHECK(kind IN ('driver_onboarding','driver_offboarding','relay_review','escrow_release'));
ALTER TABLE system_task_assignments ADD COLUMN assignee_ids uuid[] NOT NULL DEFAULT '{}';
INSERT INTO system_task_assignments(kind) VALUES('escrow_release');

CREATE TABLE escrow_release_reviews (
 id uuid PRIMARY KEY,
 driver_id uuid REFERENCES drivers(id) ON DELETE SET NULL,
 driver_name text NOT NULL,
 termination_date date NOT NULL,
 due_date date GENERATED ALWAYS AS (termination_date+30) STORED,
 decision text CHECK(decision IN ('released','partially_released','kept')),
 reason text NOT NULL DEFAULT '',
 balance_snapshot jsonb,
 completed_by uuid REFERENCES app_users(id) ON DELETE SET NULL,
 completed_at timestamptz,
 CHECK ((decision IS NULL AND completed_at IS NULL) OR
        (decision IS NOT NULL AND completed_at IS NOT NULL AND length(btrim(reason)) BETWEEN 1 AND 5000))
);
CREATE INDEX escrow_release_reviews_due_idx ON escrow_release_reviews(due_date) WHERE completed_at IS NULL;
CREATE FUNCTION sync_escrow_release_review() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.status='terminated' THEN
  INSERT INTO escrow_release_reviews(id,driver_id,driver_name,termination_date)
  VALUES(NEW.termination_id,NEW.id,NEW.full_name,NEW.termination_date)
  ON CONFLICT(id) DO UPDATE SET driver_name=excluded.driver_name,termination_date=excluded.termination_date
  WHERE escrow_release_reviews.completed_at IS NULL;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER driver_escrow_review AFTER INSERT OR UPDATE ON drivers FOR EACH ROW EXECUTE FUNCTION sync_escrow_release_review();
INSERT INTO escrow_release_reviews(id,driver_id,driver_name,termination_date)
 SELECT termination_id,id,full_name,termination_date FROM drivers WHERE status='terminated';

CREATE TABLE driver_escrow_opening_events (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 escrow_id uuid NOT NULL REFERENCES driver_escrows(id),
 actor_id uuid REFERENCES app_users(id) ON DELETE SET NULL,
 before_amount numeric(14,2) NOT NULL,
 after_amount numeric(14,2) NOT NULL,
 reason text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
  ALTER TABLE escrow_release_reviews OWNER TO mserp_app;
  ALTER TABLE driver_escrow_opening_events OWNER TO mserp_app;
 END IF;
END $$;
COMMIT;

BEGIN;
-- Date corrections can hide an already-materialized task until its new due date.
-- Wake connected task feeds even when no new system_task_record is inserted.
CREATE TRIGGER escrow_review_changed AFTER INSERT OR UPDATE OR DELETE ON escrow_release_reviews
FOR EACH ROW EXECUTE FUNCTION notify_task_change();
COMMIT;

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

BEGIN;
CREATE TABLE payroll_source_actions (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 actor_id text NOT NULL,
 driver_id uuid NOT NULL,
 service_date date NOT NULL,
 slot integer NOT NULL,
 applied_version integer NOT NULL,
 expected_version integer NOT NULL,
 before_values jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 undone_at timestamptz
);
DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
  ALTER TABLE payroll_source_actions OWNER TO mserp_app;
 END IF;
END $$;
COMMIT;

-- WeighMyTruck (migration 073).
BEGIN;
CREATE TABLE weighmytruck_memberships (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 driver_id uuid UNIQUE REFERENCES drivers(id) ON DELETE SET NULL,
 email text NOT NULL UNIQUE CHECK (email = lower(btrim(email)) AND email <> ''),
 first_name text NOT NULL,
 last_name text NOT NULL,
 phone text NOT NULL DEFAULT '',
 driver_code text NOT NULL DEFAULT '',
 enrolled boolean NOT NULL DEFAULT false,
 state text NOT NULL DEFAULT 'confirmed' CHECK(state IN ('confirmed','pending','review')),
 version integer NOT NULL DEFAULT 1,
 last_error text NOT NULL DEFAULT '',
 updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE weighmytruck_events (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 membership_id uuid NOT NULL REFERENCES weighmytruck_memberships(id),
 actor_id text NOT NULL,
 action text NOT NULL,
 detail text NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE weighmytruck_imports (
 digest text PRIMARY KEY,
 row_count integer NOT NULL,
 imported_at timestamptz NOT NULL DEFAULT now()
);
DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
  ALTER TABLE weighmytruck_memberships OWNER TO mserp_app;
  ALTER TABLE weighmytruck_events OWNER TO mserp_app;
  ALTER TABLE weighmytruck_imports OWNER TO mserp_app;
 END IF;
END $$;
COMMIT;

-- WeighMyTruck settlements (migration 074).
BEGIN;
CREATE TABLE weighmytruck_transactions (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 site_number text NOT NULL,
 ticket_number text NOT NULL,
 original_ticket_number text NOT NULL DEFAULT '',
 transaction_date date NOT NULL,
 approximate_time text NOT NULL DEFAULT '',
 amount numeric(12,2) NOT NULL,
 tractor_number text NOT NULL DEFAULT '',
 trailer_number text NOT NULL DEFAULT '',
 driver_code text NOT NULL DEFAULT '',
 location text NOT NULL DEFAULT '', city text NOT NULL DEFAULT '', state text NOT NULL DEFAULT '',
 driver_id uuid REFERENCES drivers(id) ON DELETE RESTRICT,
 truck_id uuid REFERENCES trucks(id) ON DELETE RESTRICT,
 match_warning text NOT NULL DEFAULT '',
 source_conflict boolean NOT NULL DEFAULT false,
 manually_linked boolean NOT NULL DEFAULT false,
 version integer NOT NULL DEFAULT 1,
 raw_payload jsonb NOT NULL,
 synced_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(site_number,ticket_number)
);
CREATE INDEX wmt_transaction_date ON weighmytruck_transactions(transaction_date);
CREATE INDEX wmt_transaction_driver ON weighmytruck_transactions(driver_id,transaction_date);
CREATE INDEX wmt_transaction_truck ON weighmytruck_transactions(truck_id,transaction_date);
CREATE TABLE weighmytruck_billing (
 driver_id uuid NOT NULL REFERENCES drivers(id) ON DELETE RESTRICT,
 week_start date NOT NULL CHECK(extract(isodow FROM week_start)=1),
 payer text NOT NULL CHECK(payer IN ('company','driver','investor')),
 version integer NOT NULL DEFAULT 1,
 actor_id text NOT NULL,
 updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(driver_id,week_start)
);
CREATE TABLE weighmytruck_sync_days (
 sync_date date PRIMARY KEY, synced_at timestamptz NOT NULL DEFAULT now(), ticket_count integer NOT NULL
);
CREATE TABLE weighmytruck_audit (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 actor_id text NOT NULL, action text NOT NULL, detail jsonb NOT NULL, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE weighmytruck_requests (
 driver_id uuid PRIMARY KEY REFERENCES drivers(id) ON DELETE CASCADE,
 actor_id text NOT NULL,
 state text NOT NULL DEFAULT 'queued' CHECK(state IN ('queued','running','done','review')),
 last_error text NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now()
);
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
  ALTER TABLE weighmytruck_transactions OWNER TO mserp_app;
  ALTER TABLE weighmytruck_billing OWNER TO mserp_app;
  ALTER TABLE weighmytruck_sync_days OWNER TO mserp_app;
  ALTER TABLE weighmytruck_audit OWNER TO mserp_app;
  ALTER TABLE weighmytruck_requests OWNER TO mserp_app;
 END IF;
END $$;
COMMIT;

-- Relay driver management (migration 075).
BEGIN;
CREATE TABLE relay_driver_rosters (
    environment TEXT PRIMARY KEY CHECK (environment IN ('production','staging')),
    snapshot JSONB NOT NULL,
    refreshed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE relay_driver_operations (
    id UUID PRIMARY KEY,
    environment TEXT NOT NULL CHECK (environment IN ('production','staging')),
    target TEXT NOT NULL,
    action TEXT NOT NULL CHECK (action IN ('add','edit','remove','enable','disable','assign','unassign')),
    relay_driver_id TEXT NOT NULL DEFAULT '',
    driver_id UUID REFERENCES drivers(id) ON DELETE SET NULL,
    policy_id TEXT NOT NULL DEFAULT '',
    state TEXT NOT NULL DEFAULT 'pending' CHECK (state IN ('pending','confirmed','failed','verified')),
    actor_id UUID REFERENCES app_users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at TIMESTAMPTZ,
    resolved_by UUID REFERENCES app_users(id) ON DELETE SET NULL
);
CREATE UNIQUE INDEX relay_driver_operation_pending ON relay_driver_operations(environment,target) WHERE state='pending';
DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
  ALTER TABLE relay_driver_rosters OWNER TO mserp_app;
  ALTER TABLE relay_driver_operations OWNER TO mserp_app;
 END IF;
END $$;
COMMIT;

-- Relay offboarding (migration 076).
BEGIN;
ALTER TABLE relay_driver_operations ADD COLUMN source TEXT NOT NULL DEFAULT 'manual' CHECK (source IN ('manual','termination'));
ALTER TABLE relay_driver_operations ADD COLUMN termination_date DATE;
COMMIT;

-- PrePass truck mappings (migration 077).
BEGIN;
CREATE FUNCTION prepass_truck_key(tag text,unit text) RETURNS text LANGUAGE sql IMMUTABLE AS $$
 SELECT CASE WHEN btrim(coalesce(tag,''))<>'' THEN 'tag:'||upper(btrim(tag))
 WHEN btrim(coalesce(unit,''))<>'' AND upper(btrim(unit)) NOT LIKE 'UNASSIGNED%' AND upper(btrim(unit))<>'UNKNOWN'
 THEN 'unit:'||upper(btrim(unit)) ELSE '' END
$$;
CREATE TABLE prepass_truck_mappings (
 id uuid PRIMARY KEY,
 environment text NOT NULL CHECK(environment IN ('production','nonproduction')),
 identity_key text NOT NULL CHECK(identity_key<>''),
 truck_id uuid NOT NULL REFERENCES trucks(id) ON DELETE RESTRICT,
 effective_from date NOT NULL,
 effective_to date CHECK(effective_to>=effective_from),
 active boolean NOT NULL DEFAULT true,
 version integer NOT NULL DEFAULT 1,
 actor_id uuid REFERENCES app_users(id) ON DELETE SET NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX prepass_truck_mappings_identity_idx ON prepass_truck_mappings(environment,identity_key) WHERE active;
ALTER TABLE tolls ADD COLUMN truck_mapping_id uuid REFERENCES prepass_truck_mappings(id) ON DELETE RESTRICT;
CREATE TABLE prepass_truck_mapping_events (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 mapping_id uuid NOT NULL REFERENCES prepass_truck_mappings(id),
 actor_id uuid REFERENCES app_users(id) ON DELETE SET NULL,
 action text NOT NULL CHECK(action IN ('link','undo')),
 transaction_ids jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
-- Confirmed mappings take precedence over unit-name guesses for new/unlinked rows.
-- Existing linked rows are retained on sync. Undo deactivates the mapping first.
CREATE FUNCTION apply_prepass_truck_mapping() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE matched prepass_truck_mappings%ROWTYPE;
BEGIN
 IF TG_OP='UPDATE' THEN
  IF OLD.truck_id IS NOT NULL THEN RETURN NEW; END IF;
 END IF;
 SELECT * INTO matched FROM prepass_truck_mappings m
 WHERE m.active AND m.environment=coalesce(NEW.prepass_environment,'production')
 AND m.identity_key=prepass_truck_key(NEW.prepass_tag_id,NEW.equipment_unit)
 AND NEW.exit_date>=m.effective_from AND (m.effective_to IS NULL OR NEW.exit_date<=m.effective_to);
 IF FOUND THEN NEW.truck_id=matched.truck_id; NEW.truck_mapping_id=matched.id; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER toll_prepass_truck_mapping BEFORE INSERT OR UPDATE OF truck_id,equipment_unit,prepass_tag_id,exit_date ON tolls
 FOR EACH ROW EXECUTE FUNCTION apply_prepass_truck_mapping();
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
  ALTER TABLE prepass_truck_mappings OWNER TO mserp_app;
  ALTER TABLE prepass_truck_mapping_events OWNER TO mserp_app;
 END IF;
END $$;
COMMIT;

BEGIN;
ALTER TABLE drivers ADD COLUMN team_version integer NOT NULL DEFAULT 0;
CREATE TABLE driver_teams (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 main_driver_id uuid NOT NULL REFERENCES drivers(id) ON DELETE RESTRICT,
 team_driver_id uuid NOT NULL REFERENCES drivers(id) ON DELETE RESTRICT,
 start_week date NOT NULL CHECK(extract(isodow FROM start_week)=1),
 end_week date CHECK(extract(isodow FROM end_week)=1 AND end_week>=start_week),
 CHECK(main_driver_id<>team_driver_id)
);
CREATE INDEX driver_teams_main_idx ON driver_teams(main_driver_id,start_week);
CREATE INDEX driver_teams_member_idx ON driver_teams(team_driver_id,start_week);
CREATE TABLE driver_team_events (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 driver_id uuid NOT NULL REFERENCES drivers(id) ON DELETE RESTRICT,
 actor_id uuid REFERENCES app_users(id) ON DELETE SET NULL,
 effective_week date NOT NULL,
 before_value jsonb NOT NULL,
 after_value jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE FUNCTION payroll_driver_at(person uuid, day date) RETURNS uuid LANGUAGE sql STABLE AS $$
 SELECT coalesce((SELECT main_driver_id FROM driver_teams WHERE team_driver_id=person
 AND start_week<=day AND (end_week IS NULL OR day<end_week)),person)
$$;
-- Serialize configuration with payroll, fleet writes and other team changes.
CREATE FUNCTION guard_driver_team() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE first_week date; last_week date;
BEGIN
 PERFORM pg_advisory_xact_lock(736281940);
 first_week:=NEW.start_week;last_week:=NEW.end_week;
 IF TG_OP='UPDATE' THEN
  IF NEW.main_driver_id=OLD.main_driver_id AND NEW.team_driver_id=OLD.team_driver_id AND NEW.start_week=OLD.start_week THEN
   IF NEW.end_week IS NOT DISTINCT FROM OLD.end_week THEN RETURN NEW; END IF;
   first_week:=least(NEW.end_week,OLD.end_week);
   IF NEW.end_week IS NULL OR OLD.end_week IS NULL THEN last_week:=NULL; ELSE last_week:=greatest(NEW.end_week,OLD.end_week); END IF;
  ELSE first_week:=least(NEW.start_week,OLD.start_week);last_week:=NULL;
  END IF;
 END IF;
 IF EXISTS(SELECT 1 FROM payroll_settlements s WHERE s.driver_id IN (NEW.main_driver_id,NEW.team_driver_id)
 AND s.week_start>=first_week AND (last_week IS NULL OR s.week_start<last_week) AND s.finalized) THEN
  RAISE EXCEPTION 'Reopen finalized payroll before changing this team' USING ERRCODE='23514';
 END IF;
 IF NEW.end_week=NEW.start_week THEN RETURN NEW; END IF;
 IF EXISTS(SELECT 1 FROM driver_teams t WHERE t.id<>NEW.id
 AND (t.main_driver_id IN (NEW.main_driver_id,NEW.team_driver_id) OR t.team_driver_id IN (NEW.main_driver_id,NEW.team_driver_id))
 AND daterange(t.start_week,t.end_week,'[)') && daterange(NEW.start_week,NEW.end_week,'[)')) THEN
  RAISE EXCEPTION 'A driver can belong to only one two-person team in a week' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER driver_team_guard BEFORE INSERT OR UPDATE ON driver_teams FOR EACH ROW EXECUTE FUNCTION guard_driver_team();

-- Team members keep their personal ledgers, protected by the main statement.
CREATE FUNCTION protect_team_payroll() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE d uuid; w date; value jsonb;
BEGIN
 IF TG_OP='DELETE' THEN value:=to_jsonb(OLD); ELSE value:=to_jsonb(NEW); END IF;
 w:=(value->>'week_start')::date;
 IF TG_TABLE_NAME IN ('driver_pay_weeks','driver_pay_cost_collections') THEN d:=(value->>'driver_id')::uuid;
 ELSIF TG_TABLE_NAME='expense_payments' THEN SELECT charge_driver_id INTO d FROM expenses WHERE id=(value->>'expense_id')::uuid;
 ELSIF TG_TABLE_NAME IN ('driver_escrow_payments','driver_escrow_releases') THEN SELECT driver_id INTO d FROM driver_escrows WHERE id=(value->>'escrow_id')::uuid;
 ELSE SELECT driver_id INTO d FROM driver_charge_schedules WHERE id=(value->>'schedule_id')::uuid;
 END IF;
 IF d IS NOT NULL AND payroll_driver_at(d,w)<>d AND EXISTS(SELECT 1 FROM payroll_settlements
 WHERE driver_id=payroll_driver_at(d,w) AND week_start=w AND finalized) THEN
  IF TG_OP='UPDATE' AND TG_TABLE_NAME='driver_charge_occurrences' AND
    (value - ARRAY['version'])=(to_jsonb(OLD) - ARRAY['version']) THEN RETURN NEW; END IF;
  RAISE EXCEPTION 'Reopen the main driver settlement before editing team deductions' USING ERRCODE='23514';
 END IF;
 IF TG_OP='DELETE' THEN RETURN OLD; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER team_payroll_guard BEFORE INSERT OR UPDATE OR DELETE ON driver_pay_weeks FOR EACH ROW EXECUTE FUNCTION protect_team_payroll();
CREATE TRIGGER team_cost_guard BEFORE INSERT OR UPDATE OR DELETE ON driver_pay_cost_collections FOR EACH ROW EXECUTE FUNCTION protect_team_payroll();
CREATE TRIGGER team_expense_guard BEFORE INSERT OR UPDATE OR DELETE ON expense_payments FOR EACH ROW EXECUTE FUNCTION protect_team_payroll();
CREATE TRIGGER team_charge_guard BEFORE INSERT OR UPDATE OR DELETE ON driver_charge_occurrences FOR EACH ROW EXECUTE FUNCTION protect_team_payroll();
CREATE TRIGGER team_escrow_guard BEFORE INSERT OR UPDATE OR DELETE ON driver_escrow_payments FOR EACH ROW EXECUTE FUNCTION protect_team_payroll();
CREATE TRIGGER team_release_guard BEFORE INSERT OR UPDATE OR DELETE ON driver_escrow_releases FOR EACH ROW EXECUTE FUNCTION protect_team_payroll();

CREATE FUNCTION guard_team_dispatch() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE day date;
BEGIN
 PERFORM pg_advisory_xact_lock_shared(736281940);
 IF TG_TABLE_NAME='truck_driver_assignments' THEN
  IF EXISTS(SELECT 1 FROM driver_teams t WHERE t.team_driver_id=NEW.driver_id
    AND daterange(t.start_week,t.end_week,'[)') && daterange((NEW.assigned_at AT TIME ZONE 'America/New_York')::date,
      (NEW.unassigned_at AT TIME ZONE 'America/New_York')::date,'[)')) THEN
   RAISE EXCEPTION 'Assign the truck to the main driver of this team' USING ERRCODE='23514';
  END IF;
 ELSE
  IF TG_TABLE_NAME='driver_board' THEN day:=(now() AT TIME ZONE 'America/New_York')::date;
  ELSE
   day:=NEW.service_date;
   IF btrim(NEW.load_number)='' THEN RETURN NEW; END IF;
   IF TG_TABLE_NAME='gross_board_extra_entries' THEN
    IF NEW.deleted THEN RETURN NEW; END IF;
   END IF;
  END IF;
  IF payroll_driver_at(NEW.driver_id,day)<>NEW.driver_id THEN
   RAISE EXCEPTION 'Enter dispatch work under the main driver of this team' USING ERRCODE='23514';
  END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER team_truck_guard BEFORE INSERT OR UPDATE ON truck_driver_assignments FOR EACH ROW EXECUTE FUNCTION guard_team_dispatch();
CREATE TRIGGER team_gross_guard BEFORE INSERT OR UPDATE ON gross_board_entries FOR EACH ROW EXECUTE FUNCTION guard_team_dispatch();
CREATE TRIGGER team_extra_gross_guard BEFORE INSERT OR UPDATE ON gross_board_extra_entries FOR EACH ROW EXECUTE FUNCTION guard_team_dispatch();
CREATE TRIGGER team_board_guard BEFORE INSERT OR UPDATE ON driver_board FOR EACH ROW EXECUTE FUNCTION guard_team_dispatch();

CREATE FUNCTION guard_team_recurring() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE d uuid; k text; first_week date; last_week date;
BEGIN
 PERFORM pg_advisory_xact_lock_shared(736281940);
 IF TG_TABLE_NAME='driver_charge_schedules' THEN
  IF TG_OP='UPDATE' AND NEW.driver_id=OLD.driver_id AND NEW.kind=OLD.kind AND NEW.start_week=OLD.start_week THEN RETURN NEW; END IF;
  d:=NEW.driver_id;k:=NEW.kind;first_week:=NEW.start_week;last_week:=NEW.end_week+7;
 ELSE
  IF NEW.paused THEN RETURN NEW; END IF;
  SELECT driver_id,kind,end_week+7 INTO d,k,last_week FROM driver_charge_schedules WHERE id=NEW.schedule_id;
  first_week:=NEW.week_start;
  SELECT least(last_week,min(week_start)) INTO last_week FROM driver_charge_phases WHERE schedule_id=NEW.schedule_id AND week_start>first_week;
 END IF;
 IF k='recurring' AND EXISTS(SELECT 1 FROM driver_teams t WHERE t.team_driver_id=d
 AND daterange(t.start_week,t.end_week,'[)') && daterange(first_week,last_week,'[)')) THEN
  RAISE EXCEPTION 'Configure recurring charges on the main driver, not the team driver' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER team_recurring_schedule_guard BEFORE INSERT OR UPDATE ON driver_charge_schedules FOR EACH ROW EXECUTE FUNCTION guard_team_recurring();
CREATE TRIGGER team_recurring_phase_guard BEFORE INSERT OR UPDATE ON driver_charge_phases FOR EACH ROW EXECUTE FUNCTION guard_team_recurring();

CREATE OR REPLACE FUNCTION assert_assignment_payroll_open(d uuid, t uuid, effective_week date)
RETURNS void LANGUAGE plpgsql AS $$
BEGIN
 IF EXISTS(SELECT 1 FROM payroll_settlements s WHERE
 (s.driver_id=d OR s.driver_id=payroll_driver_at(d,s.week_start)) AND s.week_start>=effective_week AND s.finalized) THEN
  RAISE EXCEPTION 'Reopen finalized payroll for this driver from the selected assignment week onward before changing assignments' USING ERRCODE='23514';
 END IF;
 IF EXISTS(SELECT 1 FROM investor_pay_weeks WHERE truck_id=t AND week_start>=effective_week AND finalized) THEN
  RAISE EXCEPTION 'Reopen finalized investor payroll for this truck from the selected assignment week onward before changing assignments' USING ERRCODE='23514';
 END IF;
END $$;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
  ALTER TABLE driver_teams OWNER TO mserp_app;
  ALTER TABLE driver_team_events OWNER TO mserp_app;
 END IF;
END $$;
COMMIT;

-- Driver service setup (migration 079).
BEGIN;
ALTER TABLE system_task_assignments DROP CONSTRAINT system_task_assignments_kind_check;
ALTER TABLE system_task_assignments ADD CONSTRAINT system_task_assignments_kind_check CHECK(kind IN ('driver_onboarding','driver_offboarding','relay_review','escrow_release','relay_setup','weighmytruck_setup'));
INSERT INTO system_task_assignments(kind,assignee_id,assignee_ids)
 SELECT v.kind,a.assignee_id,a.assignee_ids FROM system_task_assignments a CROSS JOIN (VALUES('relay_setup'),('weighmytruck_setup')) v(kind) WHERE a.kind='driver_onboarding';
ALTER TABLE system_task_records ADD COLUMN available_on date NOT NULL DEFAULT (now() AT TIME ZONE 'America/New_York')::date;
ALTER TABLE system_task_records ADD COLUMN service_driver_id uuid;
CREATE TABLE driver_service_requests (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 driver_id uuid NOT NULL REFERENCES drivers(id) ON DELETE CASCADE,
 service text NOT NULL CHECK(service IN ('relay','weighmytruck')),
 state text NOT NULL CHECK(state IN ('queued','running','review','deferred','done','canceled')),
 version integer NOT NULL DEFAULT 1,
 follow_up_on date NOT NULL DEFAULT (now() AT TIME ZONE 'America/New_York')::date,
 reminder_notified_on date,
 last_error text NOT NULL DEFAULT '',
 actor_id uuid REFERENCES app_users(id) ON DELETE SET NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(driver_id,service)
);
CREATE INDEX driver_service_requests_work_idx ON driver_service_requests(state,created_at);
CREATE TABLE driver_service_events (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 request_id uuid NOT NULL,
 actor_id uuid REFERENCES app_users(id) ON DELETE SET NULL,
 state text NOT NULL,
 follow_up_on date NOT NULL,
 detail text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE FUNCTION sync_driver_service_task() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE driver_name text; label text;
BEGIN
 IF TG_OP='DELETE' THEN
  UPDATE system_task_records SET completed_at=coalesce(completed_at,now()),completed_by_name='System',outcome='Driver deleted' WHERE id=OLD.id;
  RETURN OLD;
 END IF;
 SELECT full_name INTO driver_name FROM drivers WHERE id=NEW.driver_id;
 label=CASE NEW.service WHEN 'relay' THEN 'Relay' ELSE 'WeighMyTruck' END;
 INSERT INTO system_task_records(id,kind,title,notes,created_at,available_on,service_driver_id,completed_at,completed_by_name,outcome)
 VALUES(NEW.id,NEW.service||'_setup','Set up '||driver_name||' in '||label,
 concat_ws(E'\n',nullif(NEW.last_error,''),'Follow up: '||NEW.follow_up_on::text),NEW.created_at,NEW.follow_up_on,NEW.driver_id,
 CASE WHEN NEW.state IN ('done','canceled') THEN now() END,CASE WHEN NEW.state IN ('done','canceled') THEN 'System' ELSE '' END,
 CASE NEW.state WHEN 'done' THEN 'Enrollment confirmed' WHEN 'canceled' THEN 'Driver terminated; setup canceled' ELSE '' END)
 ON CONFLICT(id) DO UPDATE SET title=excluded.title,notes=excluded.notes,available_on=excluded.available_on,
 completed_at=CASE WHEN NEW.state IN ('done','canceled') THEN coalesce(system_task_records.completed_at,excluded.completed_at) ELSE NULL END,
 completed_by_name=excluded.completed_by_name,outcome=excluded.outcome;
 INSERT INTO driver_service_events(request_id,actor_id,state,follow_up_on,detail) VALUES(NEW.id,NEW.actor_id,NEW.state,NEW.follow_up_on,NEW.last_error);
 RETURN NEW;
END $$;
CREATE TRIGGER driver_service_task AFTER INSERT OR UPDATE OR DELETE ON driver_service_requests FOR EACH ROW EXECUTE FUNCTION sync_driver_service_task();
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
  ALTER TABLE driver_service_requests OWNER TO mserp_app;
  ALTER TABLE driver_service_events OWNER TO mserp_app;
 END IF;
END $$;
COMMIT;


-- Driver address consolidation (migration 080).
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

-- Truck-only recurring charges and automatic driver types (migration 081).
BEGIN;

-- Driver classification is a read of assignment and ownership, never user input.
-- A date selects historical relationships; NULL selects the current assignment.
CREATE OR REPLACE FUNCTION driver_employment_type(p_driver uuid, p_day date DEFAULT NULL)
RETURNS text LANGUAGE sql STABLE AS $$
 SELECT coalesce((
   SELECT CASE WHEN i.is_company THEN 'company' WHEN i.driver_id=p_driver THEN 'owner_operator' ELSE 'investor_driver' END
   FROM truck_driver_assignments a JOIN trucks t ON t.id=a.truck_id
   LEFT JOIN LATERAL (
     SELECT h.owner_id FROM truck_ownership_history h WHERE h.truck_id=t.id
       AND (NOT h.start_known OR (h.assigned_at AT TIME ZONE 'America/New_York')::date<=p_day)
       AND (h.unassigned_at IS NULL OR (h.unassigned_at AT TIME ZONE 'America/New_York')::date>p_day)
     ORDER BY h.assigned_at DESC LIMIT 1
   ) ownership ON p_day IS NOT NULL
   JOIN investors i ON i.id=CASE WHEN p_day IS NULL THEN t.owner_id ELSE ownership.owner_id END
   WHERE a.driver_id=payroll_driver_at(p_driver,coalesce(p_day,(now() AT TIME ZONE 'America/New_York')::date))
     AND CASE WHEN p_day IS NULL THEN a.unassigned_at IS NULL ELSE
       (a.assigned_at AT TIME ZONE 'America/New_York')::date<=p_day AND
       (a.unassigned_at IS NULL OR (a.unassigned_at AT TIME ZONE 'America/New_York')::date>p_day) END
   ORDER BY a.assigned_at DESC LIMIT 1
 ),'unassigned')
$$;

-- Explicitly authorized reset of driver recurring assignments. Installment
-- principal and frozen payroll snapshots are separate and remain intact.
ALTER TABLE driver_charge_events DROP CONSTRAINT driver_charge_events_schedule_id_fkey;
ALTER TABLE driver_charge_events ADD CONSTRAINT driver_charge_events_schedule_id_fkey
 FOREIGN KEY(schedule_id) REFERENCES driver_charge_schedules(id) ON DELETE SET NULL;
INSERT INTO driver_charge_events(schedule_id,type_id,action,details)
 SELECT id,type_id,'retired_driver_recurring',to_jsonb(s) FROM driver_charge_schedules s WHERE kind='recurring';
-- Keep frozen report JSON intact, but remove retired collection handles so a
-- later explicit reopen does not try to audit a deleted schedule.
UPDATE payroll_settlements w SET confirmed_schedules=ARRAY(
 SELECT kept.schedule_id FROM unnest(w.confirmed_schedules) AS kept(schedule_id)
 WHERE NOT EXISTS(SELECT 1 FROM driver_charge_schedules s WHERE s.id=kept.schedule_id AND s.kind='recurring'))
 WHERE confirmed_schedules && ARRAY(SELECT id FROM driver_charge_schedules WHERE kind='recurring');
-- This one-time authorized reset includes already-confirmed recurring rows.
-- Transactional DDL holds the table lock and restores the guard before commit.
ALTER TABLE driver_charge_occurrences DISABLE TRIGGER payroll_charge_guard;
DELETE FROM driver_charge_occurrences WHERE schedule_id IN (SELECT id FROM driver_charge_schedules WHERE kind='recurring');
ALTER TABLE driver_charge_occurrences ENABLE TRIGGER payroll_charge_guard;
DELETE FROM driver_charge_phases WHERE schedule_id IN (SELECT id FROM driver_charge_schedules WHERE kind='recurring');
DELETE FROM driver_charge_schedules WHERE kind='recurring';
ALTER TABLE driver_charge_schedules DROP CONSTRAINT IF EXISTS driver_charges_installments_only;
ALTER TABLE driver_charge_schedules ADD CONSTRAINT driver_charges_installments_only CHECK (kind='installment');

-- Remove the specifically requested catalog entry and all its truck selections.
INSERT INTO driver_charge_events(type_id,action,details)
 SELECT id,'type_deleted',to_jsonb(t) FROM driver_charge_types t WHERE lower(btrim(name))='charge';
DELETE FROM truck_charge_phases WHERE type_id IN (SELECT id FROM driver_charge_types WHERE lower(btrim(name))='charge');
DELETE FROM driver_charge_type_rules WHERE type_id IN (SELECT id FROM driver_charge_types WHERE lower(btrim(name))='charge');
DELETE FROM driver_charge_types WHERE lower(btrim(name))='charge';

-- Older binaries may still call this function during assignment changes.
CREATE OR REPLACE FUNCTION handoff_investor_driver_charges(p_driver uuid, p_actor uuid DEFAULT NULL)
RETURNS void LANGUAGE plpgsql AS $$ BEGIN RETURN; END $$;

COMMIT;

-- PrePass fleet management (migration 082).
BEGIN;
CREATE TABLE prepass_fleet_rosters (
 environment TEXT PRIMARY KEY CHECK(environment IN ('production','nonproduction')),
 snapshot JSONB NOT NULL,
 refreshed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE prepass_fleet_links (
 environment TEXT NOT NULL CHECK(environment IN ('production','nonproduction')),
 account_number TEXT NOT NULL,
 vin TEXT NOT NULL,
 truck_id UUID NOT NULL REFERENCES trucks(id) ON DELETE CASCADE,
 updated_by UUID REFERENCES app_users(id) ON DELETE SET NULL,
 updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 PRIMARY KEY(environment,account_number,vin),
 UNIQUE(environment,truck_id)
);
CREATE TABLE prepass_fleet_operations (
 id UUID PRIMARY KEY,
 environment TEXT NOT NULL CHECK(environment IN ('production','nonproduction')),
 account_number TEXT NOT NULL,
 vin TEXT NOT NULL,
 action TEXT NOT NULL CHECK(action IN ('add','edit','remove','assign','link')),
 request_hash TEXT NOT NULL,
 request JSONB NOT NULL,
 before_vehicle JSONB,
 state TEXT NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','confirmed','failed','verified')),
 actor_id UUID REFERENCES app_users(id) ON DELETE SET NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 resolved_at TIMESTAMPTZ,
 resolved_by UUID REFERENCES app_users(id) ON DELETE SET NULL
);
CREATE UNIQUE INDEX prepass_fleet_pending ON prepass_fleet_operations(environment) WHERE state='pending';
CREATE INDEX prepass_fleet_operation_history ON prepass_fleet_operations(environment,created_at DESC);
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
  ALTER TABLE prepass_fleet_rosters OWNER TO mserp_app;
  ALTER TABLE prepass_fleet_links OWNER TO mserp_app;
  ALTER TABLE prepass_fleet_operations OWNER TO mserp_app;
 END IF;
END $$;
COMMIT;

-- PrePass fleet toll linking (migration 083).
BEGIN;
ALTER TABLE prepass_truck_mappings ADD COLUMN account_number text;
ALTER TABLE prepass_truck_mappings ADD COLUMN fleet_vin text;
ALTER TABLE prepass_truck_mappings ADD COLUMN fleet_request_id uuid REFERENCES prepass_fleet_operations(id);
ALTER TABLE prepass_truck_mapping_events DROP CONSTRAINT prepass_truck_mapping_events_action_check;
ALTER TABLE prepass_truck_mapping_events ADD CONSTRAINT prepass_truck_mapping_events_action_check CHECK(action IN ('link','undo','close'));
CREATE INDEX prepass_truck_mappings_fleet_idx ON prepass_truck_mappings(environment,account_number,fleet_vin) WHERE active;

-- Legacy reviewed mappings retain their tag-first semantics. Fleet rules are
-- account scoped and also allow exact-unit fallback when no tag rule matches.
CREATE FUNCTION resolve_prepass_truck_mapping(env text, account text, tag text, unit text, travel date)
RETURNS SETOF prepass_truck_mappings LANGUAGE sql STABLE AS $$
 SELECT m.* FROM prepass_truck_mappings m
 WHERE m.active AND m.environment=env
 AND travel>=m.effective_from AND (m.effective_to IS NULL OR travel<=m.effective_to)
 AND ((m.account_number IS NULL AND m.identity_key=prepass_truck_key(tag,unit))
   OR (m.account_number=account AND m.identity_key IN
     (CASE WHEN btrim(coalesce(tag,''))<>'' THEN 'tag:'||upper(btrim(tag)) ELSE '' END,prepass_truck_key(NULL,unit))))
 ORDER BY (m.identity_key LIKE 'tag:%') DESC,(m.account_number IS NOT NULL) DESC,m.effective_from DESC,m.id
 LIMIT 1
$$;
CREATE OR REPLACE FUNCTION apply_prepass_truck_mapping() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE matched prepass_truck_mappings%ROWTYPE;
BEGIN
 IF TG_OP='UPDATE' THEN
  IF OLD.truck_id IS NOT NULL THEN RETURN NEW; END IF;
 END IF;
 SELECT * INTO matched FROM resolve_prepass_truck_mapping(coalesce(NEW.prepass_environment,'production'),NEW.customer_id,NEW.prepass_tag_id,NEW.equipment_unit,NEW.exit_date);
 IF FOUND THEN NEW.truck_id=matched.truck_id; NEW.truck_mapping_id=matched.id; END IF;
 RETURN NEW;
END $$;
DROP TRIGGER toll_prepass_truck_mapping ON tolls;
CREATE TRIGGER toll_prepass_truck_mapping BEFORE INSERT OR UPDATE OF truck_id,equipment_unit,prepass_tag_id,exit_date,customer_id,prepass_environment ON tolls
 FOR EACH ROW EXECUTE FUNCTION apply_prepass_truck_mapping();
COMMIT;

-- Gross Board load notes (084).
ALTER TABLE gross_board_entries ADD COLUMN note text NOT NULL DEFAULT '' CHECK (char_length(note) <= 5000);
ALTER TABLE gross_board_extra_entries ADD COLUMN note text NOT NULL DEFAULT '' CHECK (char_length(note) <= 5000);

-- Expense verification attribution and cancellation (migration 085).
BEGIN;

CREATE TABLE expense_verifications (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    expense_id uuid NOT NULL REFERENCES expenses(id) ON DELETE CASCADE,
    verifier_role text NOT NULL CHECK (verifier_role IN ('admin', 'accountant')),
    user_id uuid REFERENCES app_users(id) ON DELETE SET NULL,
    user_name text NOT NULL DEFAULT '',
    verified_at timestamptz,
    revoked_at timestamptz,
    revoked_by uuid REFERENCES app_users(id) ON DELETE SET NULL,
    revoked_by_name text
);
CREATE UNIQUE INDEX expense_verifications_active ON expense_verifications(expense_id, verifier_role) WHERE revoked_at IS NULL;

-- Historical checks have no recorded verifier or timestamp. Preserve them
-- without attributing the approval to the migration operator.
INSERT INTO expense_verifications(expense_id, verifier_role)
SELECT id, 'admin' FROM expenses WHERE manager_verified;
INSERT INTO expense_verifications(expense_id, verifier_role)
SELECT id, 'accountant' FROM expenses WHERE accounting_verified;

DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'mserp_app') THEN
        ALTER TABLE expense_verifications OWNER TO mserp_app;
    END IF;
END $$;

COMMIT;

-- Driver pay layover settings and exclusions (migration 086).
BEGIN;

CREATE TABLE driver_pay_settings (
 singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
 layover_daily_amount numeric(12,2) NOT NULL DEFAULT 100 CHECK (layover_daily_amount >= 0 AND layover_daily_amount <= 999999999.99),
 version integer NOT NULL DEFAULT 1 CHECK (version > 0),
 updated_at timestamptz NOT NULL DEFAULT now(),
 updated_by uuid REFERENCES app_users(id) ON DELETE SET NULL
);
INSERT INTO driver_pay_settings(singleton) VALUES(true);

ALTER TABLE driver_pay_weeks ADD COLUMN layover_excluded_dates text[] NOT NULL DEFAULT '{}';
ALTER TABLE driver_pay_weeks ADD CONSTRAINT driver_pay_layover_dates_limit CHECK (cardinality(layover_excluded_dates) <= 7);

DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
  ALTER TABLE driver_pay_settings OWNER TO mserp_app;
 END IF;
END $$;

COMMIT;

-- Retire expense company (compatibility bridge, migration 087).
BEGIN;
-- Compatibility bridge: deploy company-independent readers/writers before 088
-- removes the column. Older API binaries remain usable during this activation.
ALTER TABLE expenses ALTER COLUMN company SET DEFAULT '';
COMMIT;

-- Remove retired expense company data (migration 088).
BEGIN;
-- Deploy the company-independent API from migration 087 before applying this.
-- The immediately preceding release remains compatible with this schema.
-- Dropping the column also removes its company/date index and historical values.
ALTER TABLE expenses DROP COLUMN company;
COMMIT;

-- EFS expense fees (migration 089).
BEGIN;

-- Preserve historical totals; fee-aware writes store the entered principal separately.
ALTER TABLE expenses ADD COLUMN base_amount NUMERIC(14,2);
ALTER TABLE expenses ADD COLUMN payment_fee NUMERIC(14,2) NOT NULL DEFAULT 0;
ALTER TABLE expenses ADD CONSTRAINT expense_payment_fee CHECK (
    payment_fee >= 0 AND (base_amount IS NULL OR amount = base_amount + payment_fee)
);
INSERT INTO expense_settings(kind,name) VALUES('payment_method','EFS') ON CONFLICT DO NOTHING;
UPDATE expense_settings SET name='EFS', active=true, version=version+1 WHERE kind='payment_method' AND lower(name)='efs';

CREATE FUNCTION protect_efs_payment_method() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.kind='payment_method' AND lower(OLD.name)='efs' THEN
        IF TG_OP='DELETE' THEN
            RAISE EXCEPTION 'EFS is a permanent payment method' USING ERRCODE='23514';
        END IF;
        IF NEW.kind<>OLD.kind OR NEW.name<>'EFS' OR NOT NEW.active THEN
            RAISE EXCEPTION 'EFS is a permanent payment method' USING ERRCODE='23514';
        END IF;
    END IF;
    IF TG_OP='DELETE' THEN RETURN OLD; END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER expense_settings_protect_efs BEFORE UPDATE OR DELETE ON expense_settings
FOR EACH ROW EXECUTE FUNCTION protect_efs_payment_method();

COMMIT;

-- Transactional payroll cache revisions (migration 090).
BEGIN;

-- One transactional revision per table. Changes and invalidation commit together.
-- Authentication/session housekeeping is deliberately outside financial inputs.
CREATE TABLE payroll_cache_revisions (
    table_name TEXT PRIMARY KEY,
    revision BIGINT NOT NULL DEFAULT 0
);
CREATE FUNCTION invalidate_payroll_cache() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP<>'TRUNCATE' THEN
        IF NOT EXISTS (SELECT 1 FROM changed_rows LIMIT 1) THEN RETURN NULL; END IF;
    END IF;
    UPDATE payroll_cache_revisions SET revision=revision+1 WHERE table_name=TG_TABLE_NAME;
    RETURN NULL;
END $$;
DO $$
DECLARE item RECORD;
BEGIN
    -- Cover every existing application table, not a fragile subset of payroll joins.
    FOR item IN SELECT tablename FROM pg_tables WHERE schemaname=current_schema()
        AND tablename NOT IN ('payroll_cache_revisions','auth_sessions','schema_migrations')
        ORDER BY tablename
    LOOP
        INSERT INTO payroll_cache_revisions(table_name) VALUES(item.tablename);
        EXECUTE format('CREATE TRIGGER payroll_cache_insert AFTER INSERT ON %I REFERENCING NEW TABLE AS changed_rows FOR EACH STATEMENT EXECUTE FUNCTION invalidate_payroll_cache()',item.tablename);
        EXECUTE format('CREATE TRIGGER payroll_cache_update AFTER UPDATE ON %I REFERENCING NEW TABLE AS changed_rows FOR EACH STATEMENT EXECUTE FUNCTION invalidate_payroll_cache()',item.tablename);
        EXECUTE format('CREATE TRIGGER payroll_cache_delete AFTER DELETE ON %I REFERENCING OLD TABLE AS changed_rows FOR EACH STATEMENT EXECUTE FUNCTION invalidate_payroll_cache()',item.tablename);
        EXECUTE format('CREATE TRIGGER payroll_cache_truncate AFTER TRUNCATE ON %I FOR EACH STATEMENT EXECUTE FUNCTION invalidate_payroll_cache()',item.tablename);
    END LOOP;
    IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
        ALTER TABLE payroll_cache_revisions OWNER TO mserp_app;
    END IF;
END $$;
COMMIT;


BEGIN;
ALTER TABLE system_task_assignments DROP CONSTRAINT system_task_assignments_kind_check;
ALTER TABLE system_task_assignments ADD CONSTRAINT system_task_assignments_kind_check CHECK(kind IN ('driver_onboarding','driver_offboarding','relay_review','escrow_release','relay_setup','weighmytruck_setup','five_eld_truck'));
-- Initial responsibility requested for Nate; later edits use Settings and are preserved.
INSERT INTO system_task_assignments(kind,assignee_id,assignee_ids)
SELECT 'five_eld_truck',id,CASE WHEN id IS NULL THEN '{}'::uuid[] ELSE ARRAY[id] END
FROM (SELECT (SELECT min(id::text)::uuid FROM app_users WHERE active AND lower(username)='nate finesse' HAVING count(*)=1) AS id) initial;

ALTER TABLE system_task_records ADD COLUMN eld_vin text;
ALTER TABLE system_task_records ADD COLUMN eld_unit text NOT NULL DEFAULT '';
ALTER TABLE system_task_records ADD COLUMN eld_version integer NOT NULL DEFAULT 1;
-- Deliberately retained after truck deletion as a durable review receipt.
ALTER TABLE system_task_records ADD COLUMN eld_truck_id uuid;
CREATE UNIQUE INDEX system_task_eld_vin_idx ON system_task_records(eld_vin) WHERE kind='five_eld_truck';

CREATE FUNCTION reconcile_five_eld_tasks() RETURNS void LANGUAGE plpgsql AS $$
BEGIN
 INSERT INTO system_task_records(id,kind,title,notes,created_at,eld_vin,eld_unit)
 SELECT gen_random_uuid(),'five_eld_truck','Review Five ELD truck '||l.provider_truck_number,
 'Five ELD reports this truck working for MS Express. Review its details before adding it to MSERP.',
 now(),l.vin,l.provider_truck_number FROM five_eld_locations l
 WHERE EXISTS(SELECT 1 FROM five_eld_sync_state s WHERE l.fetched_at=s.last_success_at)
 AND NOT EXISTS(SELECT 1 FROM trucks t WHERE upper(regexp_replace(t.vin,'[^A-Za-z0-9]','','g'))=l.vin)
 AND NOT EXISTS(SELECT 1 FROM five_eld_sync_state s WHERE l.vin=ANY(s.ambiguous_vins))
 ON CONFLICT(eld_vin) WHERE kind='five_eld_truck' DO UPDATE SET
 eld_unit=excluded.eld_unit,title=excluded.title,eld_version=system_task_records.eld_version+1
 WHERE system_task_records.completed_at IS NULL AND system_task_records.eld_unit IS DISTINCT FROM excluded.eld_unit;

 UPDATE system_task_records s SET completed_at=now(),completed_by_name='System',outcome='Truck already in MSERP',eld_truck_id=t.id
 FROM trucks t WHERE s.kind='five_eld_truck' AND s.completed_at IS NULL
 AND upper(regexp_replace(t.vin,'[^A-Za-z0-9]','','g'))=s.eld_vin;
END $$;
SELECT reconcile_five_eld_tasks();
COMMIT;

BEGIN;

-- Truck fees and investor operating costs share one exact weekly collection ledger.
-- Owner identity keeps an existing balance from transferring on a truck sale.
CREATE TABLE pay_source_collections (
    source TEXT NOT NULL,
    owner_id UUID NOT NULL REFERENCES investors(id),
    week_start DATE NOT NULL CHECK (extract(isodow FROM week_start)=1),
    kind TEXT NOT NULL CHECK (kind IN ('driver','investor')),
    statement_id UUID NOT NULL,
    truck_id UUID NOT NULL REFERENCES trucks(id),
    name TEXT NOT NULL,
    base NUMERIC(14,2) NOT NULL,
    amount NUMERIC(14,2) NOT NULL,
    statement JSONB NOT NULL,
    version INTEGER NOT NULL DEFAULT 1,
    updated_by UUID REFERENCES app_users(id),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY(source,owner_id,week_start)
);
CREATE INDEX pay_source_statement ON pay_source_collections(kind,statement_id,week_start);
CREATE FUNCTION protect_pay_source_collection() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE v RECORD;
BEGIN
    IF TG_OP='UPDATE' THEN
        IF NEW.source<>OLD.source OR NEW.owner_id<>OLD.owner_id OR NEW.week_start<>OLD.week_start THEN
            RAISE EXCEPTION 'Collection identity cannot be changed' USING ERRCODE='23514';
        END IF;
        IF EXISTS(SELECT 1 FROM payroll_settlements WHERE OLD.kind='driver' AND driver_id=OLD.statement_id AND week_start=OLD.week_start AND finalized)
           OR EXISTS(SELECT 1 FROM investor_pay_weeks WHERE OLD.kind='investor' AND truck_id=OLD.statement_id AND week_start=OLD.week_start AND finalized) THEN
            RAISE EXCEPTION 'Reopen this statement before changing its collections' USING ERRCODE='23514';
        END IF;
    END IF;
    IF TG_OP='DELETE' THEN v:=OLD; ELSE v:=NEW; END IF;
    IF EXISTS(SELECT 1 FROM payroll_settlements WHERE v.kind='driver' AND driver_id=v.statement_id AND week_start=v.week_start AND finalized)
       OR EXISTS(SELECT 1 FROM investor_pay_weeks WHERE v.kind='investor' AND truck_id=v.statement_id AND week_start=v.week_start AND finalized) THEN
        RAISE EXCEPTION 'Reopen this statement before changing its collections' USING ERRCODE='23514';
    END IF;
    RETURN v;
END $$;
CREATE TRIGGER pay_source_guard BEFORE INSERT OR UPDATE OR DELETE ON pay_source_collections FOR EACH ROW EXECUTE FUNCTION protect_pay_source_collection();
INSERT INTO payroll_cache_revisions(table_name) VALUES('pay_source_collections');
CREATE TRIGGER payroll_cache_insert AFTER INSERT ON pay_source_collections REFERENCING NEW TABLE AS changed_rows FOR EACH STATEMENT EXECUTE FUNCTION invalidate_payroll_cache();
CREATE TRIGGER payroll_cache_update AFTER UPDATE ON pay_source_collections REFERENCING NEW TABLE AS changed_rows FOR EACH STATEMENT EXECUTE FUNCTION invalidate_payroll_cache();
CREATE TRIGGER payroll_cache_delete AFTER DELETE ON pay_source_collections REFERENCING OLD TABLE AS changed_rows FOR EACH STATEMENT EXECUTE FUNCTION invalidate_payroll_cache();
CREATE TRIGGER payroll_cache_truncate AFTER TRUNCATE ON pay_source_collections FOR EACH STATEMENT EXECUTE FUNCTION invalidate_payroll_cache();
DO $$ BEGIN
    IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
        ALTER TABLE pay_source_collections OWNER TO mserp_app;
    END IF;
END $$;
COMMIT;

BEGIN;

-- Names are historical snapshots; catalog changes never rewrite expense costs.
ALTER TABLE expenses ADD COLUMN cost_breakdown JSONB NOT NULL DEFAULT '[]'::jsonb;

CREATE FUNCTION valid_expense_cost_breakdown(items JSONB, total NUMERIC)
RETURNS BOOLEAN LANGUAGE plpgsql IMMUTABLE AS $$
DECLARE item JSONB; cost NUMERIC := 0;
BEGIN
    IF jsonb_typeof(items) IS DISTINCT FROM 'array' THEN RETURN false; END IF;
    IF jsonb_array_length(items) = 0 THEN RETURN true; END IF;
    IF jsonb_array_length(items) > 100 OR total IS NULL THEN RETURN false; END IF;
    FOR item IN SELECT value FROM jsonb_array_elements(items) LOOP
        IF jsonb_typeof(item) IS DISTINCT FROM 'object'
            OR jsonb_typeof(item->'name') IS DISTINCT FROM 'string'
            OR length(btrim(item->>'name')) NOT BETWEEN 1 AND 100
            OR item->>'name' <> btrim(item->>'name')
            OR jsonb_typeof(item->'amount') IS DISTINCT FROM 'string'
            OR (item->>'amount') !~ '^-?[0-9]{1,12}(\.[0-9]{1,2})?$'
        THEN RETURN false; END IF;
        cost := cost + (item->>'amount')::numeric;
    END LOOP;
    RETURN cost = total;
END $$;
ALTER TABLE expenses ADD CONSTRAINT expense_cost_breakdown_total
    CHECK (valid_expense_cost_breakdown(cost_breakdown, coalesce(base_amount, amount)));

INSERT INTO expense_settings(kind,category_id,name)
SELECT 'name',c.id,n.name FROM (VALUES
    ('Electrical issue'),('Towing'),('PM service'),('Truck wash'),('Detailing'),
    ('Road service'),('Body work'),('Leakage'),('Brake issue'),('Tire issue'),('Oil change')
) n(name) JOIN expense_settings c ON c.kind='category' AND lower(c.name)='maintenance' AND c.active
ON CONFLICT DO NOTHING;

COMMIT;

BEGIN;

CREATE TABLE maintenance_configuration (
    singleton BOOLEAN PRIMARY KEY DEFAULT true CHECK (singleton),
    category_id UUID NOT NULL REFERENCES expense_settings(id) ON DELETE RESTRICT
);
INSERT INTO maintenance_configuration(category_id)
SELECT c.id FROM expense_settings c WHERE c.kind='category' AND (lower(c.name)='maintenance' OR EXISTS(SELECT 1 FROM expenses e WHERE e.category_id=c.id AND lower(e.category)='maintenance')) ORDER BY (lower(c.name)='maintenance') DESC,c.active DESC,c.id LIMIT 1;

CREATE TABLE maintenance_types (
    code TEXT PRIMARY KEY CHECK (code ~ '^[a-z][a-z0-9_]{0,39}$'),
    name TEXT NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 100),
    active BOOLEAN NOT NULL DEFAULT true,
    version INTEGER NOT NULL DEFAULT 1
);
CREATE UNIQUE INDEX maintenance_type_name ON maintenance_types(lower(name));
INSERT INTO maintenance_types(code,name) VALUES
 ('preventive','Preventive maintenance'),('engine','Engine & drivetrain'),
 ('electrical','Electrical'),('brakes','Brakes'),('tires','Tires & wheels'),
 ('fluids','Cooling & fluid systems'),('body','Body & chassis'),
 ('recovery','Towing & recovery'),('cleaning','Cleaning & detailing'),('other','Other maintenance');

CREATE TABLE maintenance_vendors (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 200),
    address TEXT NOT NULL DEFAULT '', phone TEXT NOT NULL DEFAULT '', contact TEXT NOT NULL DEFAULT '',
    active BOOLEAN NOT NULL DEFAULT true, version INTEGER NOT NULL DEFAULT 1
);
CREATE UNIQUE INDEX maintenance_vendor_name ON maintenance_vendors(lower(btrim(name)));

CREATE TABLE maintenance_records (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    expense_id UUID UNIQUE REFERENCES expenses(id) ON DELETE SET NULL,
    truck_id UUID REFERENCES trucks(id) ON DELETE RESTRICT,
    vendor_id UUID REFERENCES maintenance_vendors(id) ON DELETE RESTRICT,
    vendor_name TEXT NOT NULL DEFAULT '', vendor_address TEXT NOT NULL DEFAULT '',
    vendor_phone TEXT NOT NULL DEFAULT '', vendor_contact TEXT NOT NULL DEFAULT '',
    service_date DATE, odometer INTEGER CHECK (odometer >= 0),
    location TEXT NOT NULL DEFAULT 'shop' CHECK (location IN ('shop','roadside','internal','unknown')),
    status TEXT NOT NULL DEFAULT 'needs_review' CHECK (status IN ('completed','needs_review','voided')),
    work_order TEXT NOT NULL DEFAULT '', notes TEXT NOT NULL DEFAULT '',
    services JSONB NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(services)='array'),
    version INTEGER NOT NULL DEFAULT 1,
    created_by UUID REFERENCES app_users(id) ON DELETE SET NULL,
    created_by_name TEXT NOT NULL DEFAULT '', updated_by_name TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(), updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    void_reason TEXT NOT NULL DEFAULT '', voided_expense JSONB,
    CHECK (status<>'completed' OR (truck_id IS NOT NULL AND service_date IS NOT NULL))
);
CREATE INDEX maintenance_truck_history ON maintenance_records(truck_id,service_date DESC,id);
CREATE INDEX maintenance_record_services ON maintenance_records USING gin(services);

CREATE TABLE maintenance_changes (
    id BIGSERIAL PRIMARY KEY, record_id UUID NOT NULL REFERENCES maintenance_records(id) ON DELETE RESTRICT,
    actor_name TEXT NOT NULL, happened_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    before_value JSONB, after_value JSONB NOT NULL
);
CREATE TABLE maintenance_documents (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(), record_id UUID NOT NULL REFERENCES maintenance_records(id) ON DELETE RESTRICT,
    file_name TEXT NOT NULL, content_type TEXT NOT NULL, size_bytes BIGINT NOT NULL CHECK(size_bytes BETWEEN 1 AND 20971520),
    sha256 TEXT NOT NULL, data BYTEA NOT NULL, uploaded_by TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX maintenance_documents_record ON maintenance_documents(record_id);
CREATE UNIQUE INDEX maintenance_document_retry ON maintenance_documents(record_id,sha256,file_name);

CREATE TABLE maintenance_odometer_readings (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(), truck_id UUID NOT NULL REFERENCES trucks(id) ON DELETE RESTRICT,
    record_id UUID UNIQUE REFERENCES maintenance_records(id) ON DELETE RESTRICT,
    reading_date DATE NOT NULL, mileage INTEGER NOT NULL CHECK(mileage>=0),
    source TEXT NOT NULL CHECK(source IN ('manual','service')), active BOOLEAN NOT NULL DEFAULT true,
    version INTEGER NOT NULL DEFAULT 1, actor_name TEXT NOT NULL, updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX maintenance_odometer_truck ON maintenance_odometer_readings(truck_id,reading_date DESC) WHERE active;
CREATE TABLE maintenance_reading_changes (
 id BIGSERIAL PRIMARY KEY, reading_id UUID NOT NULL REFERENCES maintenance_odometer_readings(id), actor_name TEXT NOT NULL,
 happened_at TIMESTAMPTZ NOT NULL DEFAULT now(),before_value JSONB,after_value JSONB NOT NULL
);
CREATE TABLE maintenance_schedules (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(), truck_id UUID REFERENCES trucks(id) ON DELETE RESTRICT,
    service_code TEXT NOT NULL CHECK(service_code IN ('pm','oil')),
    interval_miles INTEGER CHECK(interval_miles>0), interval_days INTEGER CHECK(interval_days>0),
    warning_miles INTEGER NOT NULL DEFAULT 1000 CHECK(warning_miles>=0),
    warning_days INTEGER NOT NULL DEFAULT 14 CHECK(warning_days>=0),
    enabled BOOLEAN NOT NULL DEFAULT true, version INTEGER NOT NULL DEFAULT 1,
    CHECK(interval_miles IS NOT NULL OR interval_days IS NOT NULL OR NOT enabled)
);
CREATE UNIQUE INDEX maintenance_schedule_scope ON maintenance_schedules(coalesce(truck_id,'00000000-0000-0000-0000-000000000000'::uuid),service_code);

CREATE FUNCTION maintenance_name_type(label TEXT) RETURNS TEXT LANGUAGE sql IMMUTABLE AS $$
 SELECT CASE lower(btrim(label))
 WHEN 'pm service' THEN 'preventive' WHEN 'oil change' THEN 'preventive'
 WHEN 'electrical issue' THEN 'electrical' WHEN 'electrical repair' THEN 'electrical'
 WHEN 'brake issue' THEN 'brakes' WHEN 'tire issue' THEN 'tires' WHEN 'tire replacement' THEN 'tires'
 WHEN 'leakage' THEN 'fluids' WHEN 'body work' THEN 'body' WHEN 'towing' THEN 'recovery'
 WHEN 'truck wash' THEN 'cleaning' WHEN 'detailing' THEN 'cleaning' ELSE 'other' END
$$;
CREATE FUNCTION maintenance_legacy_services(label TEXT, costs JSONB) RETURNS JSONB LANGUAGE sql IMMUTABLE AS $$
 SELECT CASE WHEN jsonb_array_length(costs)>0 THEN
 (SELECT jsonb_agg(jsonb_build_object('typeCode',maintenance_name_type(row->>'name'),'name',row->>'name','amount',row->>'amount','trackingCode','')) FROM jsonb_array_elements(costs) row)
 ELSE jsonb_build_array(jsonb_build_object('typeCode',maintenance_name_type(label),'name',coalesce(nullif(label,''),'Historical maintenance'),'amount',NULL,'trackingCode','')) END
$$;
INSERT INTO maintenance_records(expense_id,truck_id,location,services,notes,created_by_name,created_at,created_by)
SELECT e.id,e.truck_id,'unknown',maintenance_legacy_services(e.expense_type,e.cost_breakdown),coalesce(e.description,''),coalesce(e.created_by_name,''),e.created_at,e.created_by
FROM expenses e JOIN maintenance_configuration c ON e.category_id=c.category_id;

-- Compatible clients still produce a review record; an invoice alone is never proof of completed work.
CREATE FUNCTION maintenance_capture_expense() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF EXISTS(SELECT 1 FROM maintenance_configuration WHERE category_id=NEW.category_id) THEN
  INSERT INTO maintenance_records(expense_id,truck_id,location,services,notes,created_by_name,created_by)
  VALUES(NEW.id,NEW.truck_id,'unknown',maintenance_legacy_services(NEW.expense_type,NEW.cost_breakdown),coalesce(NEW.description,''),coalesce(NEW.created_by_name,''),NEW.created_by);
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER maintenance_expense_capture AFTER INSERT ON expenses FOR EACH ROW EXECUTE FUNCTION maintenance_capture_expense();

-- Validate linked work costs at commit, after atomic service + ledger updates.
CREATE FUNCTION maintenance_validate_record(record_id UUID) RETURNS VOID LANGUAGE plpgsql AS $$
DECLARE r maintenance_records; e expenses; item JSONB; total NUMERIC:=0; priced INTEGER:=0; n INTEGER:=0;
BEGIN
 SELECT * INTO r FROM maintenance_records WHERE id=record_id;
 IF NOT FOUND OR r.status='voided' THEN RETURN; END IF;
 IF r.expense_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM expenses ledger JOIN maintenance_configuration c ON c.category_id=ledger.category_id WHERE ledger.id=r.expense_id) THEN
  RAISE EXCEPTION 'Maintenance invoices retain their classification' USING ERRCODE='23514',CONSTRAINT='maintenance_work_total';
 END IF;
 IF r.status='needs_review' THEN RETURN; END IF;
 IF jsonb_array_length(r.services) NOT BETWEEN 1 AND 100 THEN
  RAISE EXCEPTION 'Add completed work items' USING ERRCODE='23514',CONSTRAINT='maintenance_work_total';
 END IF;
 FOR item IN SELECT value FROM jsonb_array_elements(r.services) LOOP
  n:=n+1;
  IF jsonb_typeof(item) IS DISTINCT FROM 'object' OR jsonb_typeof(item->'name') IS DISTINCT FROM 'string'
   OR length(btrim(item->>'name')) NOT BETWEEN 1 AND 100
   OR NOT EXISTS(SELECT 1 FROM maintenance_types WHERE code=item->>'typeCode')
   OR coalesce(item->>'trackingCode','') NOT IN ('','pm','oil')
   OR (coalesce(item->>'trackingCode','')<>'' AND item->>'typeCode'<>'preventive') THEN
   RAISE EXCEPTION 'Invalid maintenance work' USING ERRCODE='23514',CONSTRAINT='maintenance_work_total';
  END IF;
  IF item->>'amount' IS NOT NULL THEN
   IF (item->>'amount') !~ '^-?[0-9]{1,12}(\.[0-9]{1,2})?$' THEN
    RAISE EXCEPTION 'Invalid maintenance cost' USING ERRCODE='23514',CONSTRAINT='maintenance_work_total';
   END IF;
   priced:=priced+1; total:=total+(item->>'amount')::numeric;
  END IF;
 END LOOP;
 IF priced<>0 AND priced<>n THEN
  RAISE EXCEPTION 'Price all work items or leave all unallocated' USING ERRCODE='23514',CONSTRAINT='maintenance_work_total';
 END IF;
 IF r.expense_id IS NOT NULL THEN
  SELECT * INTO e FROM expenses WHERE id=r.expense_id;
  IF e.truck_id IS DISTINCT FROM r.truck_id OR NOT EXISTS(SELECT 1 FROM maintenance_configuration WHERE category_id=e.category_id)
   OR (priced>0 AND total<>coalesce(e.base_amount,e.amount)) THEN
   RAISE EXCEPTION 'Maintenance work must match its financial record' USING ERRCODE='23514',CONSTRAINT='maintenance_work_total';
  END IF;
  IF (priced=0 AND jsonb_array_length(e.cost_breakdown)<>0) OR (priced>0 AND
 (jsonb_array_length(e.cost_breakdown)<>n OR EXISTS(SELECT 1 FROM jsonb_array_elements(r.services) WITH ORDINALITY s(value,ord)
 WHERE s.value->>'name' IS DISTINCT FROM e.cost_breakdown->(s.ord::integer-1)->>'name'
 OR (s.value->>'amount')::numeric IS DISTINCT FROM (e.cost_breakdown->(s.ord::integer-1)->>'amount')::numeric))) THEN
   RAISE EXCEPTION 'Update itemized service costs in Maintenance' USING ERRCODE='23514',CONSTRAINT='maintenance_work_total';
  END IF;
 ELSIF priced>0 AND total<>0 THEN
  RAISE EXCEPTION 'Paid work needs a financial record' USING ERRCODE='23514',CONSTRAINT='maintenance_work_total';
 END IF;
END $$;
CREATE FUNCTION maintenance_record_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN PERFORM maintenance_validate_record(NEW.id); RETURN NEW; END $$;
CREATE CONSTRAINT TRIGGER maintenance_record_cost_guard AFTER INSERT OR UPDATE ON maintenance_records
 DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION maintenance_record_guard();
CREATE FUNCTION maintenance_expense_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE record_id UUID;
BEGIN
 FOR record_id IN SELECT id FROM maintenance_records WHERE expense_id=NEW.id LOOP
  PERFORM maintenance_validate_record(record_id);
 END LOOP;
 RETURN NEW;
END $$;
CREATE CONSTRAINT TRIGGER maintenance_expense_cost_guard AFTER UPDATE ON expenses
 DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION maintenance_expense_guard();

CREATE FUNCTION maintenance_legacy_update() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.truck_id IS DISTINCT FROM OLD.truck_id OR NEW.cost_breakdown IS DISTINCT FROM OLD.cost_breakdown OR NEW.expense_type IS DISTINCT FROM OLD.expense_type THEN
  UPDATE maintenance_records SET truck_id=NEW.truck_id,services=maintenance_legacy_services(NEW.expense_type,NEW.cost_breakdown),version=version+1,updated_at=now() WHERE expense_id=NEW.id AND status='needs_review';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER maintenance_legacy_update AFTER UPDATE ON expenses FOR EACH ROW EXECUTE FUNCTION maintenance_legacy_update();
CREATE FUNCTION maintenance_protect_ledger_delete() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF EXISTS(SELECT 1 FROM maintenance_records WHERE expense_id=OLD.id) THEN
  RAISE EXCEPTION 'Void the maintenance visit to remove its unpaid invoice' USING ERRCODE='23514',CONSTRAINT='maintenance_work_total';
 END IF;
 RETURN OLD;
END $$;
CREATE TRIGGER maintenance_ledger_delete_guard BEFORE DELETE ON expenses FOR EACH ROW EXECUTE FUNCTION maintenance_protect_ledger_delete();

DO $$ DECLARE t TEXT; BEGIN
 IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
  FOREACH t IN ARRAY ARRAY['maintenance_configuration','maintenance_types','maintenance_vendors','maintenance_records','maintenance_changes','maintenance_documents','maintenance_odometer_readings','maintenance_schedules','maintenance_reading_changes'] LOOP
   EXECUTE format('ALTER TABLE %I OWNER TO mserp_app',t);
  END LOOP;
 END IF;
END $$;
COMMIT;

BEGIN;

CREATE OR REPLACE FUNCTION maintenance_validate_record(record_id UUID) RETURNS VOID LANGUAGE plpgsql AS $$
DECLARE r maintenance_records; e expenses; item JSONB; total NUMERIC:=0; priced INTEGER:=0; n INTEGER:=0;
BEGIN
 SELECT * INTO r FROM maintenance_records WHERE id=record_id;
 IF NOT FOUND OR r.status='voided' THEN RETURN; END IF;
 IF r.expense_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM expenses ledger JOIN maintenance_configuration c ON c.category_id=ledger.category_id WHERE ledger.id=r.expense_id) THEN
  RAISE EXCEPTION 'Maintenance invoices retain their classification' USING ERRCODE='23514',CONSTRAINT='maintenance_work_total';
 END IF;
 IF r.status='needs_review' THEN RETURN; END IF;
 IF jsonb_array_length(r.services) NOT BETWEEN 1 AND 100 THEN
  RAISE EXCEPTION 'Add completed work items' USING ERRCODE='23514',CONSTRAINT='maintenance_work_total';
 END IF;
 FOR item IN SELECT value FROM jsonb_array_elements(r.services) LOOP
  n:=n+1;
  IF jsonb_typeof(item) IS DISTINCT FROM 'object' OR jsonb_typeof(item->'name') IS DISTINCT FROM 'string'
   OR length(btrim(item->>'name')) NOT BETWEEN 1 AND 100
   OR NOT EXISTS(SELECT 1 FROM maintenance_types WHERE code=item->>'typeCode')
   OR coalesce(item->>'trackingCode','') NOT IN ('','pm','oil')
   OR (item->>'trackingCode'='pm' AND item->>'typeCode'<>'preventive') OR (item->>'trackingCode'='oil' AND item->>'typeCode'<>'oil') THEN
   RAISE EXCEPTION 'Invalid maintenance work' USING ERRCODE='23514',CONSTRAINT='maintenance_work_total';
  END IF;
  IF item->>'amount' IS NOT NULL THEN
   IF (item->>'amount') !~ '^-?[0-9]{1,12}(\.[0-9]{1,2})?$' THEN
    RAISE EXCEPTION 'Invalid maintenance cost' USING ERRCODE='23514',CONSTRAINT='maintenance_work_total';
   END IF;
   priced:=priced+1; total:=total+(item->>'amount')::numeric;
  END IF;
 END LOOP;
 IF priced<>0 AND priced<>n THEN
  RAISE EXCEPTION 'Price all work items or leave all unallocated' USING ERRCODE='23514',CONSTRAINT='maintenance_work_total';
 END IF;
 IF r.expense_id IS NOT NULL THEN
  SELECT * INTO e FROM expenses WHERE id=r.expense_id;
  IF e.truck_id IS DISTINCT FROM r.truck_id OR NOT EXISTS(SELECT 1 FROM maintenance_configuration WHERE category_id=e.category_id)
   OR (priced>0 AND total<>coalesce(e.base_amount,e.amount)) THEN
   RAISE EXCEPTION 'Maintenance work must match its financial record' USING ERRCODE='23514',CONSTRAINT='maintenance_work_total';
  END IF;
  IF (priced=0 AND jsonb_array_length(e.cost_breakdown)<>0) OR (priced>0 AND
 (jsonb_array_length(e.cost_breakdown)<>n OR EXISTS(SELECT 1 FROM jsonb_array_elements(r.services) WITH ORDINALITY s(value,ord)
 WHERE s.value->>'name' IS DISTINCT FROM e.cost_breakdown->(s.ord::integer-1)->>'name'
 OR (s.value->>'amount')::numeric IS DISTINCT FROM (e.cost_breakdown->(s.ord::integer-1)->>'amount')::numeric))) THEN
   RAISE EXCEPTION 'Update itemized service costs in Maintenance' USING ERRCODE='23514',CONSTRAINT='maintenance_work_total';
  END IF;
 ELSIF priced>0 AND total<>0 THEN
  RAISE EXCEPTION 'Paid work needs a financial record' USING ERRCODE='23514',CONSTRAINT='maintenance_work_total';
 END IF;
END $$;

ALTER TABLE maintenance_records ADD COLUMN driver_id UUID REFERENCES drivers(id) ON DELETE SET NULL;
ALTER TABLE maintenance_records ADD COLUMN driver_name TEXT NOT NULL DEFAULT '';
ALTER TABLE maintenance_records ADD COLUMN source_unit_number TEXT NOT NULL DEFAULT '';
UPDATE maintenance_records m SET driver_id=e.driver_id,driver_name=coalesce(e.driver_name,'')
FROM expenses e WHERE e.id=m.expense_id;

UPDATE maintenance_types SET name='PM service',version=version+1 WHERE code='preventive' AND name='Preventive maintenance';
INSERT INTO maintenance_types(code,name) VALUES('oil','Oil change') ON CONFLICT DO NOTHING;
-- Preserve actual completion evidence and ledger names when separating oil from PM.
UPDATE maintenance_records m SET services=(SELECT jsonb_agg(CASE WHEN s->>'typeCode'='preventive' AND s->>'trackingCode'='oil' THEN jsonb_set(s,'{typeCode}','"oil"') ELSE s END ORDER BY ord) FROM jsonb_array_elements(m.services) WITH ORDINALITY a(s,ord)),version=version+1
WHERE EXISTS(SELECT 1 FROM jsonb_array_elements(m.services) s WHERE s->>'typeCode'='preventive' AND s->>'trackingCode'='oil');

INSERT INTO maintenance_vendors(name) VALUES
 ('Love''s Truck Care'),('Speedco'),('TA Truck Service'),('Petro Truck Service'),
 ('Goodyear Commercial Tire & Service Centers'),('Blue Beacon Truck Wash')
ON CONFLICT DO NOTHING;

COMMIT;

-- BEGIN 099_preventative_maintenance.sql
BEGIN;

-- Fleet preventative maintenance metadata and verified US vendor directory.
ALTER TABLE maintenance_records ADD COLUMN IF NOT EXISTS oil_type text NOT NULL DEFAULT '' CHECK (char_length(oil_type)<=100);
-- Backfill only an explicitly recorded oil grade from the historical import.
UPDATE maintenance_records SET oil_type=upper(trim(substring(notes from 'Type of oil: ([^\n\r]+)'))) WHERE oil_type='' AND work_order LIKE 'fleet-board:oil-change:row-%' AND char_length(trim(substring(notes from 'Type of oil: ([^\n\r]+)')))<=100 AND notes LIKE '%Type of oil: %' AND EXISTS (SELECT 1 FROM jsonb_array_elements(services) s WHERE s->>'typeCode'='oil');
UPDATE maintenance_records SET oil_type=regexp_replace(oil_type,'^(\d{1,2})W[ -]+(\d{2})$', '\1W-\2') WHERE oil_type ~ '^\d{1,2}W[ -]+\d{2}$';
ALTER TABLE maintenance_vendors ADD COLUMN IF NOT EXISTS source_url text NOT NULL DEFAULT '';
-- See docs/data/maintenance-vendors-us.json for selection and source URLs.
INSERT INTO maintenance_vendors(name,address,phone,source_url) VALUES
('Love''s Travel Stop #368 — Clanton, AL','Clanton, AL','','https://www.loves.com/locations/al/clanton/loves-travel-stop-clanton-368'),
('Love''s Travel Stop #635 — Alma, AR','Alma, AR','','https://www.loves.com/locations/ar/alma/loves-travel-stop-alma-635'),
('Love''s Travel Stop #460 — Benson, AZ','Benson, AZ','','https://www.loves.com/locations/az/benson/loves-travel-stop-benson-460'),
('Love''s Travel Stop #830 — Bakersfield, CA','Bakersfield, CA','','https://www.loves.com/locations/ca/bakersfield/loves-travel-stop-bakersfield-830'),
('Love''s Travel Stop #959 — Alamosa, CO','Alamosa, CO','','https://www.loves.com/locations/co/alamosa/loves-travel-stop-alamosa-959'),
('Love''s Travel Stop #646 — Willington, CT','Willington, CT','','https://www.loves.com/locations/ct/willington/loves-travel-stop-willington-646'),
('Love''s Travel Stop #228 — Auburndale, FL','Auburndale, FL','','https://www.loves.com/locations/fl/auburndale/loves-travel-stop-auburndale-228'),
('Love''s Travel Stop #808 — Albany, GA','Albany, GA','','https://www.loves.com/locations/ga/albany/loves-travel-stop-albany-808'),
('Love''s Travel Stop #411 — Clive/Des Moines, IA','Clive/Des Moines, IA','','https://www.loves.com/locations/ia/clive/loves-travel-stop-clive-411'),
('Love''s Travel Stop #812 — Bliss, ID','Bliss, ID','','https://www.loves.com/locations/id/bliss/loves-travel-stop-bliss-812'),
('Love''s Travel Stop #766 — Atkinson, IL','Atkinson, IL','','https://www.loves.com/locations/il/atkinson/loves-travel-stop-atkinson-766'),
('Love''s Travel Stop #551 — Angola, IN','Angola, IN','','https://www.loves.com/locations/in/angola/loves-travel-stop-angola-551'),
('Love''s Travel Stop #732 — Abilene, KS','Abilene, KS','','https://www.loves.com/locations/ks/abilene/loves-travel-stop-abilene-732'),
('Love''s Travel Stop #348 — Calvert City, KY','Calvert City, KY','','https://www.loves.com/locations/ky/calvert-city/loves-travel-stop-calvert-city-348'),
('Love''s Travel Stop #720 — Boyce, LA','Boyce, LA','','https://www.loves.com/locations/la/boyce/loves-travel-stop-boyce-720'),
('Love''s Travel Stop #537 — Cumberland, MD','Cumberland, MD','','https://www.loves.com/locations/md/cumberland/loves-travel-stop-cumberland-537'),
('Love''s Travel Stop #878 — Alamo, MI','Alamo, MI','','https://www.loves.com/locations/mi/alamo/loves-travel-stop-alamo-878'),
('Love''s Travel Stop #337 — Albert Lea, MN','Albert Lea, MN','','https://www.loves.com/locations/mn/albert-lea/loves-travel-stop-albert-lea-337'),
('Love''s Travel Stop #809 — Bates City, MO','Bates City, MO','','https://www.loves.com/locations/mo/bates-city/loves-travel-stop-bates-city-809'),
('Love''s Travel Stop #388 — Batesville, MS','Batesville, MS','','https://www.loves.com/locations/ms/batesville/loves-travel-stop-batesville-388'),
('Love''s Travel Stop #789 — Great Falls, MT','Great Falls, MT','','https://www.loves.com/locations/mt/great-falls/loves-travel-stop-great-falls-789'),
('Love''s Travel Stop #714 — Charlotte, NC','Charlotte, NC','','https://www.loves.com/locations/nc/charlotte/loves-travel-stop-charlotte-714'),
('Love''s Travel Stop #843 — Drayton, ND','Drayton, ND','','https://www.loves.com/locations/nd/drayton/loves-travel-stop-drayton-843'),
('Love''s Travel Stop #309 — Aurora, NE','Aurora, NE','','https://www.loves.com/locations/ne/aurora/loves-travel-stop-aurora-309'),
('Love''s Travel Stop #404 — Bordentown NJ','Bordentown NJ','','https://www.loves.com/locations/nj/bordentown/loves-travel-stop-bordentown-404'),
('Love''s Travel Stop #210 — Albuquerque, NM','Albuquerque, NM','','https://www.loves.com/locations/nm/albuquerque/loves-travel-stop-albuquerque-210'),
('Love''s Travel Stop #978 — Carlin, NV','Carlin, NV','','https://www.loves.com/locations/nv/carlin/loves-travel-stop-carlin-978'),
('Love''s Travel Stop #772 — Bath, NY','Bath, NY','','https://www.loves.com/locations/ny/bath/loves-travel-stop-bath-772'),
('Love''s Travel Stop #810 — Bellefontaine, OH','Bellefontaine, OH','','https://www.loves.com/locations/oh/bellefontaine/loves-travel-stop-bellefontaine-810'),
('Love''s Travel Stop #266 — Ardmore, OK','Ardmore, OK','','https://www.loves.com/locations/ok/ardmore/loves-travel-stop-ardmore-266'),
('Love''s Travel Stop #728 — Albany, OR','Albany, OR','','https://www.loves.com/locations/or/albany/loves-travel-stop-albany-728'),
('Love''s Travel Stop #829 — Brookville, PA','Brookville, PA','','https://www.loves.com/locations/pa/brookville/loves-travel-stop-brookville-829'),
('Love''s Travel Stop #397 — Blacksburg, SC','Blacksburg, SC','','https://www.loves.com/locations/sc/blacksburg/loves-travel-stop-blacksburg-397'),
('Love''s Travel Stop #602 — Box Elder, SD','Box Elder, SD','','https://www.loves.com/locations/sd/box-elder/loves-travel-stop-box-elder-602'),
('Love''s Travel Stop #330 — Baxter, TN','Baxter, TN','','https://www.loves.com/locations/tn/baxter/loves-travel-stop-baxter-330'),
('Love''s Travel Stop #721 — Alma, TX','Alma, TX','','https://www.loves.com/locations/tx/alma/loves-travel-stop-alma-721'),
('Love''s Travel Stop #686 — Brigham City, UT','Brigham City, UT','','https://www.loves.com/locations/ut/brigham-city/loves-travel-stop-brigham-city-686'),
('Love''s Travel Stop #715 — Bastian, VA','Bastian, VA','','https://www.loves.com/locations/va/bastian/loves-travel-stop-bastian-715'),
('Love''s Travel Stop #413 — Ellensburg, WA','Ellensburg, WA','','https://www.loves.com/locations/wa/ellensburg/loves-travel-stop-ellensburg-413'),
('Love''s Travel Stop #637 — Dekorra - Poynette, WI','Dekorra - Poynette, WI','','https://www.loves.com/locations/wi/dekorra---poynette/loves-travel-stop-dekorra---poynette-637'),
('Love''s Travel Stop #378 — Ripley, WV','Ripley, WV','','https://www.loves.com/locations/wv/ripley/loves-travel-stop-ripley-378'),
('Love''s Travel Stop #965 — Buffalo, WY','Buffalo, WY','','https://www.loves.com/locations/wy/buffalo/loves-travel-stop-buffalo-965'),
('Love''s Travel Stop #877 — Cullman, AL','Cullman, AL','','https://www.loves.com/locations/al/cullman/loves-travel-stop-cullman-877'),
('Love''s Travel Stop #671 — Blytheville, AR','Blytheville, AR','','https://www.loves.com/locations/ar/blytheville/loves-travel-stop-blytheville-671'),
('Love''s Travel Stop #280 — Buckeye, AZ','Buckeye, AZ','','https://www.loves.com/locations/az/buckeye/loves-travel-stop-buckeye-280'),
('Love''s Travel Stop #374 — Barstow, CA','Barstow, CA','','https://www.loves.com/locations/ca/barstow/loves-travel-stop-barstow-374'),
('Love''s Travel Stop #300 — Bennett, CO','Bennett, CO','','https://www.loves.com/locations/co/bennett/loves-travel-stop-bennett-300'),
('Love''s Travel Stop #708 — Bushnell, FL','Bushnell, FL','','https://www.loves.com/locations/fl/bushnell/loves-travel-stop-bushnell-708'),
('Love''s Travel Stop #405 — Brunswick, GA','Brunswick, GA','','https://www.loves.com/locations/ga/brunswick/loves-travel-stop-brunswick-405'),
('Love''s Travel Stop #476 — Davenport, IA','Davenport, IA','','https://www.loves.com/locations/ia/davenport/loves-travel-stop-davenport-476'),
('Love''s Travel Stop #334 — Heyburn, ID','Heyburn, ID','','https://www.loves.com/locations/id/heyburn/loves-travel-stop-heyburn-334'),
('Love''s Travel Stop #768 — Channahon, IL','Channahon, IL','','https://www.loves.com/locations/il/channahon/loves-travel-stop-channahon-768'),
('Love''s Travel Stop #394 — Demotte, IN','Demotte, IN','','https://www.loves.com/locations/in/demotte/loves-travel-stop-demotte-394'),
('Love''s Travel Stop #178 — Belle Plaine, KS','Belle Plaine, KS','','https://www.loves.com/locations/ks/belle-plaine/loves-travel-stop-belle-plaine-178'),
('Love''s Travel Stop #321 — Corbin KY','Corbin KY','','https://www.loves.com/locations/ky/corbin/loves-travel-stop-corbin-321'),
('Love''s Travel Stop #523 — Duson, LA','Duson, LA','','https://www.loves.com/locations/la/duson/loves-travel-stop-duson-523'),
('Love''s Travel Stop #682 — Hagerstown, MD','Hagerstown, MD','','https://www.loves.com/locations/md/hagerstown/loves-travel-stop-hagerstown-682'),
('Love''s Travel Stop #743 — Bridgeport, MI','Bridgeport, MI','','https://www.loves.com/locations/mi/bridgeport/loves-travel-stop-bridgeport-743'),
('Love''s Travel Stop #832 — Columbus, MN','Columbus, MN','','https://www.loves.com/locations/mn/columbus/loves-travel-stop-columbus-832'),
('Love''s Travel Stop #621 — Bevier, MO','Bevier, MO','','https://www.loves.com/locations/mo/bevier/loves-travel-stop-bevier-621'),
('Love''s Travel Stop #402 — Biloxi, MS','Biloxi, MS','','https://www.loves.com/locations/ms/biloxi/loves-travel-stop-biloxi-402'),
('Love''s Travel Stop #679 — Hardin, MT','Hardin, MT','','https://www.loves.com/locations/mt/hardin/loves-travel-stop-hardin-679'),
('Love''s Travel Stop #412 — Dunn, NC','Dunn, NC','','https://www.loves.com/locations/nc/dunn/loves-travel-stop-dunn-412'),
('Love''s Travel Stop #353 — Fargo, ND','Fargo, ND','','https://www.loves.com/locations/nd/fargo/loves-travel-stop-fargo-353'),
('Love''s Travel Stop #680 — Norfolk, NE','Norfolk, NE','','https://www.loves.com/locations/ne/norfolk/loves-travel-stop-norfolk-680'),
('Love''s Travel Stop #614 — Albuquerque, NM','Albuquerque, NM','','https://www.loves.com/locations/nm/albuquerque/loves-travel-stop-albuquerque-614'),
('Love''s Travel Stop #691 — Ely, NV','Ely, NV','','https://www.loves.com/locations/nv/ely/loves-travel-stop-ely-691'),
('Love''s Travel Stop #403 — Binghamton, NY','Binghamton, NY','','https://www.loves.com/locations/ny/binghamton/loves-travel-stop-binghamton-403'),
('Love''s Travel Stop #711 — Bellville, OH','Bellville, OH','','https://www.loves.com/locations/oh/bellville/loves-travel-stop-bellville-711'),
('Love''s Travel Stop #268 — Atoka, OK','Atoka, OK','','https://www.loves.com/locations/ok/atoka/loves-travel-stop-atoka-268'),
('Love''s Travel Stop #569 — Baker City, OR','Baker City, OR','','https://www.loves.com/locations/or/baker-city/loves-travel-stop-baker-city-569'),
('Love''s Travel Stop #407 — Carlisle, PA','Carlisle, PA','','https://www.loves.com/locations/pa/carlisle/loves-travel-stop-carlisle-407'),
('Love''s Travel Stop #657 — Cayce, SC','Cayce, SC','','https://www.loves.com/locations/sc/cayce/loves-travel-stop-cayce-657'),
('Love''s Travel Stop #816 — Kimball, SD','Kimball, SD','','https://www.loves.com/locations/sd/kimball/loves-travel-stop-kimball-816'),
('Love''s Travel Stop #364 — Charleston, TN','Charleston, TN','','https://www.loves.com/locations/tn/charleston/loves-travel-stop-charleston-364'),
('Love''s Travel Stop #200 — Amarillo, TX','Amarillo, TX','','https://www.loves.com/locations/tx/amarillo/loves-travel-stop-amarillo-200'),
('Love''s Travel Stop #335 — Cedar City, UT','Cedar City, UT','','https://www.loves.com/locations/ut/cedar-city/loves-travel-stop-cedar-city-335'),
('Love''s Travel Stop #707 — Low Moor, VA','Low Moor, VA','','https://www.loves.com/locations/va/covington/loves-travel-stop-covington-707'),
('Love''s Travel Stop #827 — Moses Lake, WA','Moses Lake, WA','','https://www.loves.com/locations/wa/moses-lake/loves-travel-stop-moses-lake-827'),
('Love''s Travel Stop #752 — Elkhorn, WI','Elkhorn, WI','','https://www.loves.com/locations/wi/elkhorn/loves-travel-stop-elkhorn-752'),
('Love''s Travel Stop #1060 — Casper, WY','Casper, WY','','https://www.loves.com/locations/wy/casper/loves-travel-stop-casper-1060'),
('Love''s Travel Stop #773 — Eutaw, AL','Eutaw, AL','','https://www.loves.com/locations/al/eutaw/loves-travel-stop-eutaw-773'),
('Love''s Travel Stop #822 — Clarksville, AR','Clarksville, AR','','https://www.loves.com/locations/ar/clarksville/loves-travel-stop-clarksville-822'),
('Love''s Travel Stop #328 — Chandler, AZ','Chandler, AZ','','https://www.loves.com/locations/az/chandler/loves-travel-stop-chandler-328'),
('Love''s Travel Stop #755 — Boron, CA','Boron, CA','','https://www.loves.com/locations/ca/boron/loves-travel-stop-boron-755'),
('Love''s Travel Stop #668 — Berthoud, CO','Berthoud, CO','','https://www.loves.com/locations/co/berthoud/loves-travel-stop-berthoud-668'),
('Love''s Travel Stop #453 — Cottondale, FL','Cottondale, FL','','https://www.loves.com/locations/fl/cottondale/loves-travel-stop-cottondale-453'),
('Love''s Travel Stop #735 — Calhoun, GA','Calhoun, GA','','https://www.loves.com/locations/ga/calhoun/loves-travel-stop-calhoun-735'),
('Love''s Travel Stop #684 — Ellsworth, IA','Ellsworth, IA','','https://www.loves.com/locations/ia/ellsworth/loves-travel-stop-ellsworth-684'),
('Love''s Travel Stop #478 — Idaho Falls, ID','Idaho Falls, ID','','https://www.loves.com/locations/id/idaho-falls/loves-travel-stop-idaho-falls-478'),
('Love''s Travel Stop #746 — Decatur, IL','Decatur, IL','','https://www.loves.com/locations/il/decatur/loves-travel-stop-decatur-746'),
('Love''s Travel Stop #417 — Gary, IN','Gary, IN','','https://www.loves.com/locations/in/gary/loves-travel-stop-gary-417'),
('Love''s Travel Stop #647 — Belleville, KS','Belleville, KS','','https://www.loves.com/locations/ks/belleville/loves-travel-stop-belleville-647'),
('Love''s Travel Stop #716 — Elizabethtown, KY','Elizabethtown, KY','','https://www.loves.com/locations/ky/elizabethtown/loves-travel-stop-elizabethtown-716'),
('Love''s Travel Stop #209 — Greenwood, LA','Greenwood, LA','','https://www.loves.com/locations/la/greenwood/loves-travel-stop-greenwood-209'),
('Love''s Travel Stop #701 — Capac, MI','Capac, MI','','https://www.loves.com/locations/mi/capac/loves-travel-stop-capac-701'),
('Love''s Travel Stop #842 — Rockville, MN','Rockville, MN','','https://www.loves.com/locations/mn/rockville/loves-travel-stop-rockville-842'),
('Love''s Travel Stop #488 — Bloomsdale, MO','Bloomsdale, MO','','https://www.loves.com/locations/mo/bloomsdale/loves-travel-stop-bloomsdale-488'),
('Love''s Travel Stop #208 — Canton, MS','Canton, MS','','https://www.loves.com/locations/ms/canton/loves-travel-stop-canton-208'),
('Love''s Travel Stop #1043 — Laurel, MT','Laurel, MT','','https://www.loves.com/locations/mt/laurel/loves-travel-stop-laurel-1043'),
('Speedco #911 — Cottondale, AL','Cottondale, AL','','https://www.loves.com/locations/al/cottondale/loves-speedco-cottondale-911'),
('Speedco #939 — N Little Rock, AR','N Little Rock, AR','','https://www.loves.com/locations/ar/n-little-rock/loves-speedco-n-little-rock-939'),
('Speedco #907 — Casa Grande, AZ','Casa Grande, AZ','','https://www.loves.com/locations/az/casa-grande/loves-speedco-casa-grande-907'),
('Speedco #910 — Corning, CA','Corning, CA','','https://www.loves.com/locations/ca/corning/loves-speedco-corning-910'),
('Speedco #908 — Commerce City, CO','Commerce City, CO','','https://www.loves.com/locations/co/commerce-city/loves-speedco-commerce-city-908'),
('Speedco #906 — Brunswick, GA','Brunswick, GA','','https://www.loves.com/locations/ga/brunswick/loves-speedco-brunswick-906'),
('Speedco #912 — Council Bluffs, IA','Council Bluffs, IA','','https://www.loves.com/locations/ia/council-bluffs/loves-speedco-council-bluffs-912'),
('Speedco #915 — Effingham, IL','Effingham, IL','','https://www.loves.com/locations/il/effingham/loves-speedco-effingham-915'),
('Speedco #920 — Gary, IN','Gary, IN','','https://www.loves.com/locations/in/gary/loves-speedco-gary-920'),
('Speedco #40175 — Topeka, KS','Topeka, KS','','https://www.loves.com/locations/ks/topeka/loves-speedco-topeka-40175'),
('Speedco #919 — Franklin, KY','Franklin, KY','','https://www.loves.com/locations/ky/franklin/loves-speedco-franklin-919'),
('Speedco #922 — Greenwood, LA','Greenwood, LA','','https://www.loves.com/locations/la/greenwood/loves-speedco-greenwood-922'),
('Speedco #929 — Joplin, MO','Joplin, MO','','https://www.loves.com/locations/mo/joplin/loves-speedco-joplin-929'),
('Speedco #918 — Flowood, MS','Flowood, MS','','https://www.loves.com/locations/ms/flowood/loves-speedco-flowood-918'),
('Speedco #925 — Haw River, NC','Haw River, NC','','https://www.loves.com/locations/nc/haw-river/loves-speedco-haw-river-925'),
('Speedco #938 — Milan, NM','Milan, NM','','https://www.loves.com/locations/nm/milan/loves-speedco-milan-938'),
('Speedco #917 — Fernley, NV','Fernley, NV','','https://www.loves.com/locations/nv/fernley/loves-speedco-fernley-917'),
('Speedco #903 — Austinburg, OH','Austinburg, OH','','https://www.loves.com/locations/oh/austinburg/loves-speedco-austinburg-903'),
('Speedco #941 — Oklahoma City, OK','Oklahoma City, OK','','https://www.loves.com/locations/ok/oklahoma-city/loves-speedco-oklahoma-city-941'),
('Speedco #902 — Aurora, OR','Aurora, OR','','https://www.loves.com/locations/or/aurora/loves-speedco-aurora-902'),
('Speedco #924 — Harrisburg, PA','Harrisburg, PA','','https://www.loves.com/locations/pa/harrisburg/loves-speedco-harrisburg-924'),
('Speedco #914 — Duncan, SC','Duncan, SC','','https://www.loves.com/locations/sc/duncan/loves-speedco-duncan-914'),
('Speedco #932 — Knoxville, TN','Knoxville, TN','','https://www.loves.com/locations/tn/knoxville/loves-speedco-knoxville-932'),
('Speedco #901 — Amarillo, TX','Amarillo, TX','','https://www.loves.com/locations/tx/amarillo/loves-speedco-amarillo-901'),
('Speedco #937 — Max Meadows, VA','Max Meadows, VA','','https://www.loves.com/locations/va/max-meadows/loves-speedco-max-meadows-937'),
('Speedco #948 — Spokane, WA','Spokane, WA','','https://www.loves.com/locations/wa/spokane/loves-speedco-spokane-948'),
('Speedco #951 — West Memphis, AR','West Memphis, AR','','https://www.loves.com/locations/ar/west-memphis/loves-speedco-west-memphis-951'),
('Speedco #931 — Kingman, AZ','Kingman, AZ','','https://www.loves.com/locations/az/kingman/loves-speedco-kingman-931'),
('Speedco #944 — Ripon, CA','Ripon, CA','','https://www.loves.com/locations/ca/ripon/loves-speedco-ripon-944'),
('Speedco #909 — Conley, GA','Conley, GA','','https://www.loves.com/locations/ga/conley/loves-speedco-conley-909'),
('Speedco #950 — Walcott, IA','Walcott, IA','','https://www.loves.com/locations/ia/walcott/loves-speedco-walcott-950'),
('Speedco #943 — Pontoon Beach, IL','Pontoon Beach, IL','','https://www.loves.com/locations/il/pontoon-beach/loves-speedco-pontoon-beach-943'),
('Speedco #926 — Indianapolis, IN','Indianapolis, IN','','https://www.loves.com/locations/in/indianapolis/loves-speedco-indianapolis-926'),
('Speedco #953 — Pendleton, KY','Pendleton, KY','','https://www.loves.com/locations/ky/pendleton/loves-speedco-pendleton-953'),
('Speedco #923 — Hammond, LA','Hammond, LA','','https://www.loves.com/locations/la/hammond/loves-speedco-hammond-923'),
('Speedco #940 — Oak Grove, MO','Oak Grove, MO','','https://www.loves.com/locations/mo/oak-grove/loves-speedco-oak-grove-940'),
('Speedco #930 — Kenly, NC','Kenly, NC','','https://www.loves.com/locations/nc/kenly/loves-speedco-kenly-930'),
('Speedco #936 — North Las Vegas, NV','North Las Vegas, NV','','https://www.loves.com/locations/nv/north-las-vegas/loves-speedco-north-las-vegas-936'),
('Speedco #905 — Beaverdam, OH','Beaverdam, OH','','https://www.loves.com/locations/oh/beaverdam/loves-speedco-beaverdam-905'),
('Speedco #904 — Baytown, TX','Baytown, TX','','https://www.loves.com/locations/tx/baytown/loves-speedco-baytown-904'),
('Speedco #945 — Ruther Glen, VA','Ruther Glen, VA','','https://www.loves.com/locations/va/ruther-glen/loves-speedco-ruther-glen-945'),
('Speedco #928 — Jackson, GA','Jackson, GA','','https://www.loves.com/locations/ga/jackson/loves-speedco-jackson-928'),
('Speedco #949 — Troy, IL','Troy, IL','','https://www.loves.com/locations/il/troy/loves-speedco-troy-949'),
('Speedco #952 — Whiteland, IN','Whiteland, IN','','https://www.loves.com/locations/in/whiteland/loves-speedco-whiteland-952'),
('Speedco #921 — Girard, OH','Girard, OH','','https://www.loves.com/locations/oh/girard/loves-speedco-girard-921'),
('Speedco #913 — Dallas, TX','Dallas, TX','','https://www.loves.com/locations/tx/dallas/loves-speedco-dallas-913'),
('Speedco #933 — Lake Park, GA','Lake Park, GA','','https://www.loves.com/locations/ga/lake-park/loves-speedco-lake-park-933'),
('Speedco #942 — Perrysburg, OH','Perrysburg, OH','','https://www.loves.com/locations/oh/perrysburg/loves-speedco-perrysburg-942'),
('Speedco #916 — El Paso, TX','El Paso, TX','','https://www.loves.com/locations/tx/el-paso/loves-speedco-el-paso-916'),
('Speedco #947 — Seville, OH','Seville, OH','','https://www.loves.com/locations/oh/seville/loves-speedco-seville-947'),
('TA Tuscaloosa #0016 — AL','AL','','https://www.ta-petro.com/location/al/ta-tuscaloosa/'),
('TA Earle #0033 — AR','AR','','https://www.ta-petro.com/location/ar/ta-earle/'),
('TA Eloy #0007 — AZ','AZ','','https://www.ta-petro.com/location/az/ta-eloy/'),
('Petro Ontario #0026 — CA','CA','','https://www.ta-petro.com/location/ca/petro-ontario/'),
('TA Commerce City #0148 — CO','CO','','https://www.ta-petro.com/location/co/ta-commerce-city/'),
('TA Willington #0022 — CT','CT','','https://www.ta-petro.com/location/ct/ta-willington/'),
('TA Wildwood #0053 — FL','FL','','https://www.ta-petro.com/location/fl/ta-wildwood/'),
('TA Madison #0045 — GA','GA','','https://www.ta-petro.com/location/ga/ta-madison/'),
('TA Council Bluffs #0066 — IA','IA','','https://www.ta-petro.com/location/ia/ta-council-bluffs/'),
('TA Boise #0167 — ID','ID','','https://www.ta-petro.com/location/id/ta-boise/'),
('TA Chicago North #0030 — IL','IL','','https://www.ta-petro.com/location/il/ta-chicago-north/'),
('TA Gary #0010 — IN','IN','','https://www.ta-petro.com/location/in/ta-gary/'),
('TA Oakley #0091 — KS','KS','','https://www.ta-petro.com/location/ks/ta-oakley/'),
('TA Florence #0093 — KY','KY','','https://www.ta-petro.com/location/ky/ta-florence/'),
('TA Tallulah #0046 — LA','LA','','https://www.ta-petro.com/location/la/ta-tallulah/'),
('TA Elkton #0019 — MD','MD','','https://www.ta-petro.com/location/md/ta-elkton/'),
('TA Monroe #0069 — MI','MI','','https://www.ta-petro.com/location/mi/ta-monroe/'),
('TA Rogers #0525 — MN','MN','','https://www.ta-petro.com/location/mn/ta-rogers/'),
('TA Concordia #0524 — MO','MO','','https://www.ta-petro.com/location/mo/ta-concordia/'),
('TA Meridian #0047 — MS','MS','','https://www.ta-petro.com/location/ms/ta-meridian/'),
('TA Missoula #0531 — MT','MT','','https://www.ta-petro.com/location/mt/ta-missoula/'),
('TA Greensboro #0002 — NC','NC','','https://www.ta-petro.com/location/nc/ta-greensboro/'),
('TA Express Alexander #0259 — ND','ND','','https://www.ta-petro.com/location/nd/ta-express-alexander/'),
('TA Ogallala #0090 — NE','NE','','https://www.ta-petro.com/location/ne/ta-ogallala/'),
('TA Greenland #0211 — NH','NH','','https://www.ta-petro.com/location/nh/ta-greenland/'),
('TA Columbia #0006 — NJ','NJ','','https://www.ta-petro.com/location/nj/ta-columbia/'),
('TA Gallup #0008 — NM','NM','','https://www.ta-petro.com/location/nm/ta-gallup/'),
('TA Las Vegas #0108 — NV','NV','','https://www.ta-petro.com/location/nv/ta-las-vegas/'),
('TA Pembroke #0194 — NY','NY','','https://www.ta-petro.com/location/ny/ta-pembroke/'),
('TA Dayton #0011 — OH','OH','','https://www.ta-petro.com/location/oh/ta-dayton/'),
('TA Oklahoma City East #0036 — OK','OK','','https://www.ta-petro.com/location/ok/ta-oklahoma-city-east/'),
('TA Aurora #0056 — OR','OR','','https://www.ta-petro.com/location/or/ta-aurora/'),
('TA Brookville #0003 — PA','PA','','https://www.ta-petro.com/location/pa/ta-brookville/'),
('TA West Greenwich #0253 — RI','RI','','https://www.ta-petro.com/location/ri/ta-west-greenwich/'),
('TA Spartanburg #0025 — SC','SC','','https://www.ta-petro.com/location/sc/ta-spartanburg/'),
('TA Express Hot Springs #0273 — SD','SD','','https://www.ta-petro.com/location/sd/ta-express-hot-springs/'),
('TA Knoxville East #0013 — TN','TN','','https://www.ta-petro.com/location/tn/ta-knoxville-east/'),
('TA Baytown #0017 — TX','TX','','https://www.ta-petro.com/location/tx/ta-baytown/'),
('TA Tooele #0060 — UT','UT','','https://www.ta-petro.com/location/ut/ta-tooele/'),
('TA Ashland #0001 — VA','VA','','https://www.ta-petro.com/location/va/ta-ashland/'),
('TA Seattle East #0176 — WA','WA','','https://www.ta-petro.com/location/wa/ta-seattle-east/'),
('TA Madison #0050 — WI','WI','','https://www.ta-petro.com/location/wi/ta-madison/'),
('TA Wheeling #0032 — WV','WV','','https://www.ta-petro.com/location/wv/ta-wheeling/'),
('TA Cheyenne #0187 — WY','WY','','https://www.ta-petro.com/location/wy/ta-cheyenne/'),
('TA Mobile #0054 — AL','AL','','https://www.ta-petro.com/location/al/ta-mobile/'),
('TA Prescott #0224 — AR','AR','','https://www.ta-petro.com/location/ar/ta-prescott/'),
('TA Kingman #0094 — AZ','AZ','','https://www.ta-petro.com/location/az/ta-kingman/'),
('TA Corning #0040 — CA','CA','','https://www.ta-petro.com/location/ca/ta-corning/'),
('TA Wheat Ridge #0174 — CO','CO','','https://www.ta-petro.com/location/co/ta-wheat-ridge/'),
('TA Southington #0154 — CT','CT','','https://www.ta-petro.com/location/ct/ta-southington/'),
('TA Express Tampa #0158 — FL','FL','','https://www.ta-petro.com/location/fl/ta-express-tampa/'),
('TA Cartersville #0146 — GA','GA','','https://www.ta-petro.com/location/ga/ta-cartersville/'),
('TA Walcott #0077 — IA','IA','','https://www.ta-petro.com/location/ia/ta-walcott/'),
('TA Express Dubois #0940 — ID','ID','','https://www.ta-petro.com/location/id/ta-express-dubois/'),
('TA Effingham #0035 — IL','IL','','https://www.ta-petro.com/location/il/ta-effingham/'),
('TA Seymour #0065 — IN','IN','','https://www.ta-petro.com/location/in/ta-seymour/'),
('TA Beto Junction #0252 — KS','KS','','https://www.ta-petro.com/location/ks/ta-beto-junction/'),
('Petro Glendale #0330 — KY','KY','','https://www.ta-petro.com/location/ky/petro-glendale/'),
('TA Lafayette #0161 — LA','LA','','https://www.ta-petro.com/location/la/ta-lafayette/'),
('TA Baltimore South #0151 — MD','MD','','https://www.ta-petro.com/location/md/ta-baltimore-south/'),
('TA Ann Arbor #0089 — MI','MI','','https://www.ta-petro.com/location/mi/ta-ann-arbor/'),
('Petro Clearwater #0389 — MN','MN','','https://www.ta-petro.com/location/mn/petro-clearwater/'),
('TA Matthews #0051 — MO','MO','','https://www.ta-petro.com/location/mo/ta-matthews/'),
('Petro Jackson #0506 — MS','MS','','https://www.ta-petro.com/location/ms/petro-jackson/'),
('TA Laurel #0261 — MT','MT','','https://www.ta-petro.com/location/mt/ta-laurel/'),
('TA Candler #0221 — NC','NC','','https://www.ta-petro.com/location/nc/ta-candler/'),
('TA Express Steele #0272 — ND','ND','','https://www.ta-petro.com/location/nd/ta-express-steele/'),
('TA Grand Island #0193 — NE','NE','','https://www.ta-petro.com/location/ne/ta-grand-island/'),
('TA Bloomsbury #0048 — NJ','NJ','','https://www.ta-petro.com/location/nj/ta-bloomsbury/'),
('TA Las Cruces #0014 — NM','NM','','https://www.ta-petro.com/location/nm/ta-las-cruces/'),
('TA Sparks #0172 — NV','NV','','https://www.ta-petro.com/location/nv/ta-sparks/'),
('TA Binghamton #0207 — NY','NY','','https://www.ta-petro.com/location/ny/ta-binghamton/'),
('TA Lodi #0015 — OH','OH','','https://www.ta-petro.com/location/oh/ta-lodi/'),
('TA Oklahoma City West #0059 — OK','OK','','https://www.ta-petro.com/location/ok/ta-oklahoma-city-west/'),
('TA Coburg #0482 — OR','OR','','https://www.ta-petro.com/location/or/ta-coburg/'),
('TA Harrisburg #0012 — PA','PA','','https://www.ta-petro.com/location/pa/ta-harrisburg/'),
('TA Manning #0179 — SC','SC','','https://www.ta-petro.com/location/sc/ta-manning/'),
('TA Express Summit #0274 — SD','SD','','https://www.ta-petro.com/location/sd/ta-express-summit/'),
('TA Antioch #0117 — TN','TN','','https://www.ta-petro.com/location/tn/ta-antioch/'),
('TA Rockwall #0049 — TX','TX','','https://www.ta-petro.com/location/tx/ta-rockwall/'),
('TA Express Parowan #0186 — UT','UT','','https://www.ta-petro.com/location/ut/ta-express-parowan/'),
('TA Richmond #0142 — VA','VA','','https://www.ta-petro.com/location/va/ta-richmond/'),
('Petro Spokane #0339 — WA','WA','','https://www.ta-petro.com/location/wa/petro-spokane/'),
('TA Janesville #0071 — WI','WI','','https://www.ta-petro.com/location/wi/ta-janesville/'),
('TA Hurricane #0149 — WV','WV','','https://www.ta-petro.com/location/wv/ta-hurricane/'),
('TA Ft. Bridger #0188 — WY','WY','','https://www.ta-petro.com/location/wy/ta-ft-bridger/'),
('TA Lincoln #0532 — AL','AL','','https://www.ta-petro.com/location/al/ta-lincoln/'),
('Petro W. Memphis #0311 — AR','AR','','https://www.ta-petro.com/location/ar/petro-w-memphis/'),
('TA Tonopah #0225 — AZ','AZ','','https://www.ta-petro.com/location/az/ta-tonopah/'),
('TA Coachella #0041 — CA','CA','','https://www.ta-petro.com/location/ca/ta-coachella/'),
('TA Limon #0228 — CO','CO','','https://www.ta-petro.com/location/co/ta-limon/'),
('TA Express New Haven #0171 — CT','CT','','https://www.ta-petro.com/location/ct/ta-express-new-haven/'),
('TA Marianna #0178 — FL','FL','','https://www.ta-petro.com/location/fl/ta-marianna/'),
('TA Commerce #0156 — GA','GA','','https://www.ta-petro.com/location/ga/ta-commerce/'),
('TA Brooklyn #0254 — IA','IA','','https://www.ta-petro.com/location/ia/ta-brooklyn/'),
('TA Mt Vernon #0043 — IL','IL','','https://www.ta-petro.com/location/il/ta-mt-vernon/'),
('TA Whitestown #0173 — IN','IN','','https://www.ta-petro.com/location/in/ta-whitestown/'),
('TA Express Edgerton #0284 — KS','KS','','https://www.ta-petro.com/location/ks/ta-express-edgerton/'),
('TA Truck Service Franklin #0347 — KY','KY','','https://www.ta-petro.com/location/ky/ta-truck-service-franklin/'),
('TA Slidell #0180 — LA','LA','','https://www.ta-petro.com/location/la/ta-slidell/'),
('Blue Beacon Truck Wash — Tuscaloosa, AL','Tuscaloosa, AL','(205) 462-0019','https://bluebeacon.com/locations/listing/tuscaloosa-al'),
('Blue Beacon Truck Wash — Little Rock, AR','Little Rock, AR','(501) 945-7023','https://bluebeacon.com/locations/listing/little-rock-ar'),
('Blue Beacon Truck Wash — Casa Grande, AZ','Casa Grande, AZ','(520) 426-9362','https://bluebeacon.com/locations/listing/casa-grande-az'),
('Blue Beacon Truck Wash — Barstow, CA','Barstow, CA','(760) 253-7395','https://bluebeacon.com/locations/listing/barstow-ca'),
('Blue Beacon Truck Wash — Commerce City, CO','Commerce City, CO','(303) 288-2990','https://bluebeacon.com/locations/listing/commerce-city-co'),
('Blue Beacon Truck Wash — Dade City, FL','Dade City, FL','(352) 588-0095','https://bluebeacon.com/locations/listing/dade-city-fl'),
('Blue Beacon Truck Wash — Atlanta West, GA','Atlanta West, GA','(404) 794-3621','https://bluebeacon.com/locations/listing/atlanta-west-ga'),
('Blue Beacon Truck Wash — Council Bluffs, IA','Council Bluffs, IA','(712) 322-7484','https://bluebeacon.com/locations/listing/council-bluffs'),
('Blue Beacon Truck Wash — Boise, ID','Boise, ID','(986) 249-0111','https://bluebeacon.com/locations/listing/boise-id'),
('Blue Beacon Truck Wash — Bloomington, IL','Bloomington, IL','(309) 828-2272','https://bluebeacon.com/locations/listing/bloomington-il'),
('Blue Beacon Truck Wash — Evansville, IN','Evansville, IN','(812) 768-5606','https://bluebeacon.com/locations/listing/evansville-in'),
('Blue Beacon Truck Wash — Dodge City, KS','Dodge City, KS','(620) 390-1024','https://bluebeacon.com/locations/listing/dodge-city-ks'),
('Blue Beacon Truck Wash — Corbin, KY','Corbin, KY','(606) 528-4461','https://bluebeacon.com/locations/listing/corbin-ky'),
('Blue Beacon Truck Wash — Hammond, LA','Hammond, LA','(985) 542-7853','https://bluebeacon.com/locations/listing/hammond-la'),
('Blue Beacon Truck Wash — Elkton, MD','Elkton, MD','(410) 398-9351','https://bluebeacon.com/locations/listing/elkton-md'),
('Blue Beacon Truck Wash — Benton Harbor, MI','Benton Harbor, MI','(269) 925-9555','https://bluebeacon.com/locations/listing/benton-harbor-mi'),
('Blue Beacon Truck Wash — Cuba, MO','Cuba, MO','(573) 885-0320','https://bluebeacon.com/locations/listing/cuba-mo'),
('Blue Beacon Truck Wash — Pearl, MS','Pearl, MS','(601) 352-9445','https://bluebeacon.com/locations/listing/pearl-ms'),
('Blue Beacon Truck Wash — Haw River, NC','Haw River, NC','(336) 578-3503','https://bluebeacon.com/locations/listing/haw-river-nc'),
('Blue Beacon Truck Wash — Fargo, ND','Fargo, ND','(701) 277-7123','https://bluebeacon.com/locations/listing/fargo-nd'),
('Blue Beacon Truck Wash — York, NE','York, NE','(402) 362-1655','https://bluebeacon.com/locations/listing/york-ne'),
('Blue Beacon Truck Wash — Bordentown, NJ','Bordentown, NJ','(609) 291-1812','https://bluebeacon.com/locations/listing/bordentown-nj'),
('Blue Beacon Truck Wash — Gallup, NM','Gallup, NM','(505) 722-2357','https://bluebeacon.com/locations/listing/gallup-nm'),
('Blue Beacon Truck Wash — Fernley, NV','Fernley, NV','(775) 575-4446','https://bluebeacon.com/locations/listing/fernley-nv'),
('Blue Beacon Truck Wash — Kirkwood, NY','Kirkwood, NY','(607) 778-1230','https://bluebeacon.com/locations/listing/kirkwood-ny'),
('Blue Beacon Truck Wash — Beaverdam, OH','Beaverdam, OH','(419) 643-8146','https://bluebeacon.com/locations/listing/beaverdam-oh'),
('Blue Beacon Truck Wash — Oklahoma City North, OK','Oklahoma City North, OK','(405) 478-0833','https://bluebeacon.com/locations/listing/oklahoma-city-north-ok'),
('Blue Beacon Truck Wash — Portland, OR','Portland, OR','(503) 283-3270','https://bluebeacon.com/locations/listing/portland-or'),
('Blue Beacon Truck Wash — Bentleyville, PA','Bentleyville, PA','(724) 918-1080','https://bluebeacon.com/locations/listing/bentleyville-pa'),
('Blue Beacon Truck Wash — Columbia, SC','Columbia, SC','(803) 691-1579','https://bluebeacon.com/locations/listing/columbia-sc'),
('Blue Beacon Truck Wash — Sioux Falls, SD','Sioux Falls, SD','(605) 335-0934','https://bluebeacon.com/locations/listing/sioux-falls'),
('Blue Beacon Truck Wash — Knoxville, TN','Knoxville, TN','(865) 690-5228','https://bluebeacon.com/locations/listing/knoxville-tn'),
('Blue Beacon Truck Wash — Amarillo East, TX','Amarillo East, TX','(806) 335-9033','https://bluebeacon.com/locations/listing/amarillo-east-tx'),
('Blue Beacon Truck Wash — Salt Lake City, UT','Salt Lake City, UT','(801) 975-1400','https://bluebeacon.com/locations/listing/salt-lake-city-ut'),
('Blue Beacon Truck Wash — Fort Chiswell, VA','Fort Chiswell, VA','(276) 637-3468','https://bluebeacon.com/locations/listing/fort-chiswell-va'),
('Blue Beacon Truck Wash — Spokane, WA','Spokane, WA','(509) 315-5018','https://bluebeacon.com/locations/listing/spokane-wa'),
('Blue Beacon Truck Wash — Milwaukee, WI','Milwaukee, WI','(414) 296-4008','https://bluebeacon.com/locations/listing/milwaukee-wi'),
('Blue Beacon Truck Wash — Cheyenne, WY','Cheyenne, WY','(307) 634-6531','https://bluebeacon.com/locations/listing/cheyenne-wy'),
('Blue Beacon Truck Wash — West Memphis, AR','West Memphis, AR','(870) 735-8068','https://bluebeacon.com/locations/listing/west-memphis-ar'),
('Blue Beacon Truck Wash — Eloy East, AZ','Eloy East, AZ','(520) 464-8011','https://bluebeacon.com/locations/listing/eloy-east-az'),
('Blue Beacon Truck Wash — Corning, CA','Corning, CA','(530) 824-0474','https://bluebeacon.com/locations/listing/corning-ca'),
('Blue Beacon Truck Wash — Ft. Pierce, FL','Ft. Pierce, FL','(772) 429-1459','https://bluebeacon.com/locations/listing/ft-pierce-fl'),
('Blue Beacon Truck Wash — Brunswick, GA','Brunswick, GA','(912) 261-9338','https://bluebeacon.com/locations/listing/brunswick-ga'),
('Blue Beacon Truck Wash — Des Moines, IA','Des Moines, IA','(515) 967-0141','https://bluebeacon.com/locations/listing/des-moines-ia'),
('Blue Beacon Truck Wash — Twin Falls, ID','Twin Falls, ID','(208) 324-1310','https://bluebeacon.com/locations/listing/twin-falls-id'),
('Blue Beacon Truck Wash — Effingham West, IL','Effingham West, IL','(217) 347-8930','https://bluebeacon.com/locations/listing/effingham-west-il'),
('Blue Beacon Truck Wash — Ft. Wayne, IN','Ft. Wayne, IN','(260) 471-3049','https://bluebeacon.com/locations/listing/ft-wayne-in'),
('Blue Beacon Truck Wash — Emporia, KS','Emporia, KS','(620) 591-3080','https://bluebeacon.com/locations/listing/emporia-ks'),
('Blue Beacon Truck Wash — Glendale, KY','Glendale, KY','(270) 369-0171','https://bluebeacon.com/locations/listing/glendale-ky'),
('Blue Beacon Truck Wash — Shreveport, LA','Shreveport, LA','(318) 671-6497','https://bluebeacon.com/locations/listing/shreveport-la')
ON CONFLICT DO NOTHING;

COMMIT;
-- END 099_preventative_maintenance.sql

BEGIN;
ALTER TABLE maintenance_vendors ADD COLUMN superseded_by uuid REFERENCES maintenance_vendors(id);
-- Consolidate only untouched seeded chain names; custom edits remain authoritative.
UPDATE maintenance_vendors SET name=CASE name WHEN 'Love''s Truck Care' THEN 'Love''s' WHEN 'TA Truck Service' THEN 'TA' WHEN 'Petro Truck Service' THEN 'Petro' WHEN 'Blue Beacon Truck Wash' THEN 'Blue Beacon' END,version=version+1
WHERE version=1 AND name IN ('Love''s Truck Care','TA Truck Service','Petro Truck Service','Blue Beacon Truck Wash')
AND NOT EXISTS(SELECT 1 FROM maintenance_vendors other WHERE lower(other.name)=lower(CASE maintenance_vendors.name WHEN 'Love''s Truck Care' THEN 'Love''s' WHEN 'TA Truck Service' THEN 'TA' WHEN 'Petro Truck Service' THEN 'Petro' WHEN 'Blue Beacon Truck Wash' THEN 'Blue Beacon' END));
INSERT INTO maintenance_vendors(name,source_url) VALUES
('Love''s','https://www.loves.com/'),
('Speedco','https://www.loves.com/'),
('TA','https://www.ta-petro.com/'),
('Petro','https://www.ta-petro.com/'),
('Goodyear Commercial Tire & Service Centers','https://www.goodyearctsc.com/'),
('Blue Beacon','https://bluebeacon.com/'),
('Boss Truck Shops','https://bosstruckshops.com/'),
('Southern Tire Mart','https://stmtires.com/'),
('Southern Tire Mart at Pilot','https://pilotcompany.com/truck-care'),
('Rush Truck Centers','https://www.rushtruckcenters.com/service/our-services'),
('MHC Kenworth','https://www.mhc.com/'),
('Velocity Truck Centers','https://www.velocitytruckcenters.com/locations')
ON CONFLICT DO NOTHING;
-- Branch identities and their original source details remain available as history.
UPDATE maintenance_vendors v SET active=false,superseded_by=b.id,version=v.version+1 FROM maintenance_vendors b
WHERE v.version=1 AND v.source_url<>'' AND v.name LIKE '% — %' AND b.name=CASE
 WHEN v.source_url LIKE 'https://www.loves.com/%' AND v.name LIKE 'Speedco%' THEN 'Speedco'
 WHEN v.source_url LIKE 'https://www.loves.com/%' THEN 'Love''s'
 WHEN v.source_url LIKE 'https://www.ta-petro.com/%' AND v.name LIKE 'Petro%' THEN 'Petro'
 WHEN v.source_url LIKE 'https://www.ta-petro.com/%' THEN 'TA'
 WHEN v.source_url LIKE 'https://bluebeacon.com/%' THEN 'Blue Beacon' END;

-- Customer-confirmed historical imports: the sheet's saved payment date is the
-- service date and these work orders were completed, even if a truck is unlinked.
ALTER TABLE maintenance_records ADD COLUMN legacy_import boolean NOT NULL DEFAULT false;
ALTER TABLE maintenance_records DROP CONSTRAINT maintenance_records_check;
ALTER TABLE maintenance_records ADD CONSTRAINT maintenance_records_completed_check
 CHECK(status<>'completed' OR (service_date IS NOT NULL AND (truck_id IS NOT NULL OR legacy_import)));
WITH before AS MATERIALIZED (
 SELECT m.id,to_jsonb(m)||jsonb_build_object('serviceDate',m.service_date,'vendorName',m.vendor_name,'oilType',m.oil_type,'truckId',m.truck_id) value,e.expense_date,e.unit_number FROM maintenance_records m JOIN expenses e ON e.id=m.expense_id
 WHERE e.source_spreadsheet_id IS NOT NULL AND e.source_sheet='Maintenance_and_other_expenses'
 AND m.status='needs_review' AND m.service_date IS NULL AND m.version=1 AND e.expense_date IS NOT NULL
), changed AS (
 UPDATE maintenance_records m SET service_date=b.expense_date,status='completed',legacy_import=true,
 source_unit_number=coalesce(nullif(m.source_unit_number,''),b.unit_number,''),version=m.version+1,updated_at=now(),updated_by_name='Historical import correction',
 services=(SELECT jsonb_agg(CASE WHEN lower(btrim(s->>'name'))='pm service' THEN s||'{"typeCode":"preventive","trackingCode":"pm"}'::jsonb WHEN lower(btrim(s->>'name'))='oil change' THEN s||'{"typeCode":"oil","trackingCode":"oil"}'::jsonb ELSE s END ORDER BY ord) FROM jsonb_array_elements(m.services) WITH ORDINALITY a(s,ord))
 FROM before b WHERE m.id=b.id RETURNING m.*
) INSERT INTO maintenance_changes(record_id,actor_name,before_value,after_value)
 SELECT c.id,'Historical import correction',b.value,to_jsonb(c)||jsonb_build_object('serviceDate',c.service_date,'vendorName',c.vendor_name,'oilType',c.oil_type,'truckId',c.truck_id) FROM changed c JOIN before b ON b.id=c.id;
UPDATE maintenance_records m SET vendor_id=v.superseded_by,version=m.version+1,updated_at=now()
FROM maintenance_vendors v WHERE m.vendor_id=v.id AND v.superseded_by IS NOT NULL;
COMMIT;

BEGIN;
ALTER TABLE system_task_assignments ADD COLUMN assignee_role_ids uuid[] NOT NULL DEFAULT '{}';
ALTER TABLE system_task_assignments DROP CONSTRAINT system_task_assignments_kind_check;
ALTER TABLE system_task_assignments ADD CONSTRAINT system_task_assignments_kind_check CHECK(kind IN ('driver_onboarding','driver_offboarding','relay_review','escrow_release','relay_setup','weighmytruck_setup','five_eld_truck','maintenance_pm','maintenance_oil'));
INSERT INTO system_task_assignments(kind) VALUES('maintenance_pm'),('maintenance_oil');
ALTER TABLE system_task_records ADD COLUMN maintenance_truck_id uuid;
ALTER TABLE system_task_records ADD COLUMN maintenance_service text NOT NULL DEFAULT '';
ALTER TABLE system_task_records ADD COLUMN maintenance_baseline_id uuid;
ALTER TABLE system_task_records ADD COLUMN maintenance_observed_miles integer;
ALTER TABLE system_task_records ADD COLUMN maintenance_observed_date date;
DROP TRIGGER task_user_changed ON app_users;
CREATE TRIGGER task_user_changed AFTER INSERT OR UPDATE OR DELETE ON app_users FOR EACH ROW EXECUTE FUNCTION notify_task_change();
CREATE UNIQUE INDEX system_task_maintenance_open_idx ON system_task_records(maintenance_truck_id,maintenance_service) WHERE kind IN ('maintenance_pm','maintenance_oil') AND completed_at IS NULL;
COMMIT;

-- BEGIN 102_work_order_workspace.sql
BEGIN;
CREATE TABLE maintenance_telemetry (
 truck_id uuid PRIMARY KEY REFERENCES trucks(id) ON DELETE CASCADE,
 vin text NOT NULL,
 mileage integer CHECK(mileage>=0),
 reported_at timestamptz,
 checked_at timestamptz NOT NULL DEFAULT now(),
 CHECK ((mileage IS NULL)=(reported_at IS NULL))
);
DO $$ BEGIN IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN GRANT SELECT,INSERT,UPDATE,DELETE ON maintenance_telemetry TO mserp_app; END IF; END $$;
-- Preserve category identity, permissions and financial snapshots.
UPDATE expense_settings SET name='Work orders',version=version+1
WHERE id=(SELECT category_id FROM maintenance_configuration) AND name='Maintenance'
AND NOT EXISTS(SELECT 1 FROM expense_settings WHERE kind='category' AND lower(name)='work orders');
COMMIT;

-- END 102_work_order_workspace.sql

-- FleetScope outbound fleet sync (103)
BEGIN;
-- Local capture is always durable. Credentials and network enablement are never stored here.
CREATE TABLE fleetscope_sync_state (
 singleton boolean PRIMARY KEY DEFAULT true CHECK(singleton),
 source_revision bigint NOT NULL DEFAULT 0,
 company_id uuid,
 environment text CHECK(environment IN ('production','staging')),
 epoch uuid,
 mode text NOT NULL DEFAULT 'off' CHECK(mode IN ('off','reconcile','live','paused')),
 version bigint NOT NULL DEFAULT 0,
 assignment_sequence bigint NOT NULL DEFAULT 0,
 assignment_hash text NOT NULL DEFAULT '',
 last_error text NOT NULL DEFAULT '',
 last_checked_at timestamptz,
 last_verified_at timestamptz,
 activated_at timestamptz
);
INSERT INTO fleetscope_sync_state(singleton) VALUES(true);
CREATE TABLE fleetscope_driver_identity (
 company_id uuid NOT NULL,
 fleetscope_driver_id uuid NOT NULL,
 driver_id uuid NOT NULL,
 source text NOT NULL CHECK(source IN ('intake','review')),
 confirmed boolean NOT NULL DEFAULT true,
 reviewed_by uuid,
 reviewed_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(company_id,fleetscope_driver_id),
 UNIQUE(company_id,driver_id)
);
-- Ambiguous old intake links remain review items rather than choosing a winner.
INSERT INTO fleetscope_driver_identity(company_id,fleetscope_driver_id,driver_id,source,reviewed_by)
SELECT i.company_id,i.fleetscope_driver_id,i.driver_id,'intake',i.completed_by
FROM fleetscope_driver_intake i WHERE i.completed_at IS NOT NULL AND i.driver_id IS NOT NULL
AND (SELECT count(*) FROM fleetscope_driver_intake x WHERE x.company_id=i.company_id AND x.driver_id=i.driver_id AND x.completed_at IS NOT NULL)=1;
CREATE TABLE fleetscope_sync_links (
 kind text NOT NULL CHECK(kind IN ('driver','truck')),
 local_id uuid NOT NULL,
 company_id uuid NOT NULL,
 environment text NOT NULL,
 remote_id uuid,
 remote_revision text NOT NULL DEFAULT '',
 assignment_revision text NOT NULL DEFAULT '',
 source_revision bigint NOT NULL DEFAULT 0,
 acknowledged jsonb,
 acknowledged_hash text NOT NULL DEFAULT '',
 provisioning jsonb,
 excluded_reason text NOT NULL DEFAULT '',
 last_error text NOT NULL DEFAULT '',
 last_synced_at timestamptz,
 version bigint NOT NULL DEFAULT 1,
 PRIMARY KEY(kind,local_id),
 UNIQUE(kind,company_id,environment,remote_id)
);
CREATE TABLE fleetscope_sync_snapshots (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 remote_id uuid NOT NULL,
 company_id uuid NOT NULL,
 environment text NOT NULL,
 epoch uuid NOT NULL,
 source_revision bigint NOT NULL,
 payload jsonb NOT NULL,
 local_payload jsonb NOT NULL,
 created_by uuid NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE fleetscope_sync_operations (
 id uuid PRIMARY KEY,
 entity_key text NOT NULL,
 kind text NOT NULL,
 company_id uuid NOT NULL,
 environment text NOT NULL,
 epoch uuid NOT NULL,
 payload bytea NOT NULL,
 body_sha256 text NOT NULL,
 desired_hash text NOT NULL DEFAULT '',
 bootstrap boolean NOT NULL DEFAULT false,
 review_delivery boolean NOT NULL DEFAULT false,
 state text NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','processing','applied','blocked','superseded')),
 attempts integer NOT NULL DEFAULT 0,
 next_attempt_at timestamptz NOT NULL DEFAULT now(),
 lease_token uuid,
 lease_until timestamptz,
 last_error text NOT NULL DEFAULT '',
 last_http_status integer NOT NULL DEFAULT 0,
 result jsonb,
 created_by uuid,
 created_at timestamptz NOT NULL DEFAULT now(),
 applied_at timestamptz
);
CREATE UNIQUE INDEX fleetscope_sync_one_pending ON fleetscope_sync_operations(entity_key) WHERE state IN ('pending','processing','blocked');
CREATE INDEX fleetscope_sync_due ON fleetscope_sync_operations(next_attempt_at) WHERE state IN ('pending','processing');
CREATE FUNCTION fleetscope_sync_immutable_operation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF ROW(NEW.id,NEW.entity_key,NEW.kind,NEW.company_id,NEW.environment,NEW.epoch,NEW.payload,NEW.body_sha256,NEW.desired_hash,NEW.bootstrap,NEW.created_by,NEW.created_at)
 IS DISTINCT FROM ROW(OLD.id,OLD.entity_key,OLD.kind,OLD.company_id,OLD.environment,OLD.epoch,OLD.payload,OLD.body_sha256,OLD.desired_hash,OLD.bootstrap,OLD.created_by,OLD.created_at) THEN
  RAISE EXCEPTION 'FleetScope operation identity and payload are immutable' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER fleetscope_sync_immutable BEFORE UPDATE ON fleetscope_sync_operations FOR EACH ROW EXECUTE FUNCTION fleetscope_sync_immutable_operation();
CREATE TABLE fleetscope_sync_audit (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 actor_id uuid,
 action text NOT NULL,
 details jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE FUNCTION fleetscope_sync_capture() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 UPDATE fleetscope_sync_state SET source_revision=source_revision+1 WHERE singleton;
 RETURN NULL;
END $$;
DO $$ DECLARE t text; BEGIN
 FOREACH t IN ARRAY ARRAY['trucks','drivers','investors','truck_driver_assignments','driver_teams','truck_ownership_history','fleetscope_driver_intake'] LOOP
 EXECUTE format('CREATE TRIGGER fleetscope_capture AFTER INSERT OR UPDATE OR DELETE ON %I FOR EACH STATEMENT EXECUTE FUNCTION fleetscope_sync_capture()',t);
 END LOOP;
END $$;
CREATE FUNCTION fleetscope_identity_from_intake() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.completed_at IS NOT NULL AND NEW.driver_id IS NOT NULL THEN
  IF EXISTS(SELECT 1 FROM fleetscope_driver_identity WHERE company_id=NEW.company_id AND (fleetscope_driver_id=NEW.fleetscope_driver_id OR driver_id=NEW.driver_id) AND (fleetscope_driver_id<>NEW.fleetscope_driver_id OR driver_id<>NEW.driver_id)) THEN
   RAISE EXCEPTION 'FleetScope identity is already linked' USING ERRCODE='23505';
  END IF;
  INSERT INTO fleetscope_driver_identity(company_id,fleetscope_driver_id,driver_id,source,reviewed_by)
  VALUES(NEW.company_id,NEW.fleetscope_driver_id,NEW.driver_id,'intake',NEW.completed_by)
  ON CONFLICT(company_id,fleetscope_driver_id) DO UPDATE SET confirmed=true;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER fleetscope_identity_intake AFTER INSERT OR UPDATE OF completed_at,driver_id ON fleetscope_driver_intake FOR EACH ROW EXECUTE FUNCTION fleetscope_identity_from_intake();
DO $$ DECLARE t text; BEGIN
 IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
 FOREACH t IN ARRAY ARRAY['fleetscope_sync_state','fleetscope_driver_identity','fleetscope_sync_links','fleetscope_sync_snapshots','fleetscope_sync_operations','fleetscope_sync_audit'] LOOP
 EXECUTE format('GRANT SELECT,INSERT,UPDATE,DELETE ON %I TO mserp_app',t);
 END LOOP;
 END IF;
END $$;
COMMIT;

BEGIN;

-- Existing full names are deliberately not split: token order does not prove
-- which words are given names or family names.
ALTER TABLE drivers ADD COLUMN first_name text;
ALTER TABLE drivers ADD COLUMN last_name text;
ALTER TABLE drivers ADD CONSTRAINT drivers_name_parts CHECK (
 (first_name IS NULL AND last_name IS NULL) OR
 (first_name IS NOT NULL AND last_name IS NOT NULL
  AND char_length(first_name) BETWEEN 1 AND 50
  AND char_length(last_name) BETWEEN 1 AND 50)
);

CREATE FUNCTION driver_name_parts_canonical() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.first_name IS NOT NULL THEN
  NEW.first_name := btrim(regexp_replace(NEW.first_name, '[[:space:]]+', ' ', 'g'));
 END IF;
 IF NEW.last_name IS NOT NULL THEN
  NEW.last_name := btrim(regexp_replace(NEW.last_name, '[[:space:]]+', ' ', 'g'));
 END IF;
 IF NEW.first_name IS NOT NULL AND NEW.last_name IS NOT NULL THEN
  -- Reviewed parts are authoritative even for older full-name-only writers.
  NEW.full_name := NEW.first_name || ' ' || NEW.last_name;
  NEW.normalized_name := lower(NEW.full_name);
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER driver_name_parts_canonical BEFORE INSERT OR UPDATE ON drivers
 FOR EACH ROW EXECUTE FUNCTION driver_name_parts_canonical();

COMMIT;

BEGIN;

-- Explicit operator activation is required. Deploying this migration sends nothing.
CREATE TABLE expense_sheet_export_config (
 singleton boolean PRIMARY KEY DEFAULT true CHECK(singleton),
 enabled boolean NOT NULL DEFAULT false,
 spreadsheet_id text NOT NULL DEFAULT '1VKSL8fyBRjwOWAUhHlHUrJtn2HOa53tpuTm95TNqll4',
 maintenance_sheet_id integer NOT NULL DEFAULT 377431143,
 hr_sheet_id integer NOT NULL DEFAULT 1713619712,
 safety_sheet_id integer NOT NULL DEFAULT 752928518,
 paid_before date NOT NULL DEFAULT '2026-09-28',
 hr_category_id uuid NOT NULL REFERENCES expense_settings(id),
 safety_category_id uuid NOT NULL REFERENCES expense_settings(id),
 penalties_category_id uuid NOT NULL REFERENCES expense_settings(id),
 other_category_id uuid NOT NULL REFERENCES expense_settings(id)
);
INSERT INTO expense_sheet_export_config(hr_category_id,other_category_id,safety_category_id,penalties_category_id)
SELECT h.id,o.id,s.id,p.id FROM expense_settings h CROSS JOIN expense_settings o CROSS JOIN expense_settings s CROSS JOIN expense_settings p
WHERE h.kind='category' AND lower(h.name)='hr' AND o.kind='category' AND lower(o.name)='other'
 AND s.kind='category' AND lower(s.name)='safety' AND p.kind='category' AND lower(p.name)='penalties';

-- Customer-confirmed transition cutoff: historical native entries are already paid.
-- This sets settlement only; verification/approval checkboxes retain their meaning.
CREATE FUNCTION settle_expense_sheet_history() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE c expense_sheet_export_config;
BEGIN
 SELECT * INTO c FROM expense_sheet_export_config WHERE singleton;
 IF FOUND AND c.enabled AND NEW.source_spreadsheet_id IS NULL AND NEW.system_kind IS NULL
 AND NEW.expense_date<c.paid_before AND (NEW.category_id IN(c.hr_category_id,c.safety_category_id,c.penalties_category_id,c.other_category_id)
 OR EXISTS(SELECT 1 FROM maintenance_configuration WHERE category_id=NEW.category_id)) THEN
  NEW.driver_settled:=true;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER expense_sheet_history_settled BEFORE INSERT ON expenses
 FOR EACH ROW EXECUTE FUNCTION settle_expense_sheet_history();

-- No foreign key to the source: deletion must not discard an already queued backup.
CREATE TABLE expense_sheet_exports (
 expense_id uuid PRIMARY KEY,
 spreadsheet_id text NOT NULL,
 sheet_id integer NOT NULL,
 payload jsonb NOT NULL CHECK(jsonb_typeof(payload)='object'),
 created_at timestamptz NOT NULL DEFAULT now(),
 attempts integer NOT NULL DEFAULT 0,
 last_attempt_at timestamptz,
 delivered_at timestamptz
);
CREATE INDEX expense_sheet_exports_pending ON expense_sheet_exports(created_at,expense_id) WHERE delivered_at IS NULL;

CREATE FUNCTION capture_expense_sheet_export() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE c expense_sheet_export_config; e expenses; m jsonb; target integer;
BEGIN
 SELECT * INTO c FROM expense_sheet_export_config WHERE singleton;
 IF NOT FOUND OR NOT c.enabled THEN RETURN NEW; END IF;
 SELECT * INTO e FROM expenses WHERE id=NEW.id;
 IF NOT FOUND OR e.source_spreadsheet_id IS NOT NULL OR e.system_kind IS NOT NULL THEN RETURN NEW; END IF;
 IF e.category_id=c.hr_category_id THEN target:=c.hr_sheet_id;
 ELSIF e.category_id IN(c.safety_category_id,c.penalties_category_id) THEN target:=c.safety_sheet_id;
 ELSIF e.category_id=c.other_category_id OR EXISTS(SELECT 1 FROM maintenance_configuration WHERE category_id=e.category_id) THEN target:=c.maintenance_sheet_id;
 ELSE RETURN NEW;
 END IF;
 -- Deferred capture sees service details and cost allocation committed with the invoice.
 SELECT to_jsonb(r) INTO m FROM maintenance_records r WHERE expense_id=e.id;
 INSERT INTO expense_sheet_exports(expense_id,spreadsheet_id,sheet_id,payload)
 VALUES(e.id,c.spreadsheet_id,target,jsonb_build_object('expense',to_jsonb(e),'maintenance',m))
 ON CONFLICT(expense_id) DO NOTHING;
 RETURN NEW;
END $$;
CREATE CONSTRAINT TRIGGER expense_sheet_export_capture AFTER INSERT ON expenses
 DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION capture_expense_sheet_export();

CREATE FUNCTION protect_expense_sheet_export() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.expense_id IS DISTINCT FROM OLD.expense_id OR NEW.spreadsheet_id IS DISTINCT FROM OLD.spreadsheet_id
 OR NEW.sheet_id IS DISTINCT FROM OLD.sheet_id OR NEW.payload IS DISTINCT FROM OLD.payload
 OR NEW.created_at IS DISTINCT FROM OLD.created_at
 OR (OLD.delivered_at IS NOT NULL AND NEW.delivered_at IS DISTINCT FROM OLD.delivered_at) THEN
  RAISE EXCEPTION 'Expense sheet export snapshots and delivery receipts are immutable' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER expense_sheet_export_immutable BEFORE UPDATE ON expense_sheet_exports
 FOR EACH ROW EXECUTE FUNCTION protect_expense_sheet_export();
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
  ALTER TABLE expense_sheet_export_config OWNER TO mserp_app;
  ALTER TABLE expense_sheet_exports OWNER TO mserp_app;
 END IF;
END $$;
COMMIT;

BEGIN;
-- Persist the rule on the category UUID so renaming Penalties does not remove it.
ALTER TABLE expense_settings ADD COLUMN driver_only boolean NOT NULL DEFAULT false;
UPDATE expense_settings SET driver_only=true WHERE kind='category' AND lower(btrim(name))='penalties';
ALTER TABLE expense_settings ADD CONSTRAINT expense_setting_driver_only_category CHECK(kind='category' OR NOT driver_only);
COMMIT;

-- Notifications only wake the exporter; the immutable outbox remains authoritative.
CREATE OR REPLACE FUNCTION notify_expense_sheet_export() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    PERFORM pg_notify('mserp_expense_sheet_export', '');
    RETURN NEW;
END;
$$;
CREATE TRIGGER expense_sheet_export_notify
AFTER INSERT ON expense_sheet_exports
FOR EACH ROW EXECUTE FUNCTION notify_expense_sheet_export();

BEGIN;

-- Keep the former configuration for review, including any conflicting intervals.
INSERT INTO access_audit(action,target_id,details)
SELECT 'maintenance_pm_consolidation',id,jsonb_build_object('schedule',to_jsonb(s))
FROM maintenance_schedules s;
INSERT INTO access_audit(action,target_id,details)
SELECT 'maintenance_pm_consolidation',id,jsonb_build_object('assignment',to_jsonb(a))
FROM system_task_assignments a WHERE kind IN ('maintenance_pm','maintenance_oil');

-- Existing PM configuration wins at each scope. An oil-only override becomes PM.
DELETE FROM maintenance_schedules oil USING maintenance_schedules pm
WHERE oil.service_code='oil' AND pm.service_code='pm'
AND oil.truck_id IS NOT DISTINCT FROM pm.truck_id;
UPDATE maintenance_schedules SET service_code='pm',version=version+1 WHERE service_code='oil';
ALTER TABLE maintenance_schedules DROP CONSTRAINT maintenance_schedules_service_code_check;
ALTER TABLE maintenance_schedules ADD CONSTRAINT maintenance_schedules_service_code_check CHECK(service_code='pm');

-- Retain the legacy type for immutable records and ledger validation. Application
-- reads present this alias as PM Service; new writes use preventive/pm only.
UPDATE maintenance_types SET name='PM Service',version=version+1 WHERE code='preventive';
UPDATE maintenance_types SET active=false,version=version+1 WHERE code='oil';

-- Every former assignee continues receiving the single reminder category.
WITH merged AS (
 SELECT ARRAY(SELECT DISTINCT person FROM system_task_assignments a,
 unnest(a.assignee_ids || CASE WHEN a.assignee_id IS NULL THEN '{}'::uuid[] ELSE ARRAY[a.assignee_id] END) person
 WHERE a.kind IN ('maintenance_pm','maintenance_oil') ORDER BY person) people,
 ARRAY(SELECT DISTINCT role_id FROM system_task_assignments a,unnest(a.assignee_role_ids) role_id
 WHERE a.kind IN ('maintenance_pm','maintenance_oil') ORDER BY role_id) roles
)
UPDATE system_task_assignments SET assignee_ids=merged.people,
 assignee_id=coalesce(assignee_id,merged.people[1]),assignee_role_ids=merged.roles,
 version=version+1,updated_at=now() FROM merged WHERE kind='maintenance_pm';

-- Preserve one open reminder with the strongest observed mileage evidence.
WITH ranked AS (
 SELECT id,row_number() OVER(PARTITION BY maintenance_truck_id ORDER BY maintenance_observed_miles DESC NULLS LAST,created_at,id) position
 FROM system_task_records WHERE kind IN ('maintenance_pm','maintenance_oil') AND completed_at IS NULL
)
UPDATE system_task_records SET completed_at=now(),completed_by_name='System',outcome='Consolidated into PM Service reminder'
WHERE id IN (SELECT id FROM ranked WHERE position>1);
UPDATE system_task_records SET kind='maintenance_pm',maintenance_service='pm',
 title=regexp_replace(title,'^(Oil change|PM service)','PM Service')
WHERE kind IN ('maintenance_pm','maintenance_oil');
UPDATE custom_tasks SET system_task_kind='maintenance_pm' WHERE system_task_kind='maintenance_oil';
DELETE FROM system_task_assignments WHERE kind='maintenance_oil';
ALTER TABLE system_task_assignments DROP CONSTRAINT system_task_assignments_kind_check;
ALTER TABLE system_task_assignments ADD CONSTRAINT system_task_assignments_kind_check
CHECK(kind IN ('driver_onboarding','driver_offboarding','relay_review','escrow_release','relay_setup','weighmytruck_setup','five_eld_truck','maintenance_pm'));
DROP INDEX system_task_maintenance_open_idx;
CREATE UNIQUE INDEX system_task_maintenance_open_idx ON system_task_records(maintenance_truck_id)
WHERE kind='maintenance_pm' AND completed_at IS NULL;

COMMIT;

BEGIN;

-- Legacy/imported EFS principal remains unchanged. Payroll computes the missing
-- $3 per started $500 once in the shared collectible balance. Once payments
-- exist, metadata must not silently change that balance under another week.
CREATE FUNCTION protect_expense_settlement_total() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE old_total numeric; new_total numeric;
BEGIN
    IF (NEW.amount,NEW.base_amount,NEW.payment_fee,NEW.payment_type,NEW.covered_by,NEW.category_id,NEW.driver_settled)
       IS NOT DISTINCT FROM
       (OLD.amount,OLD.base_amount,OLD.payment_fee,OLD.payment_type,OLD.covered_by,OLD.category_id,OLD.driver_settled) THEN
        RETURN NEW;
    END IF;
    IF NOT OLD.driver_settled AND NOT EXISTS(SELECT 1 FROM expense_payments WHERE expense_id=OLD.id) THEN
        RETURN NEW;
    END IF;
    SELECT CASE WHEN e.base_amount IS NULL AND e.payment_fee=0
      AND upper(btrim(e.payment_type))='EFS' AND lower(btrim(e.covered_by)) IN ('driver','truck owner')
      AND e.amount>0 AND NOT e.driver_settled
      AND NOT EXISTS(SELECT 1 FROM expense_settings c WHERE c.id=e.category_id AND c.driver_only)
      THEN e.amount+ceil(e.amount/500)*3 ELSE e.amount END
    INTO old_total FROM (SELECT OLD.*) e;
    SELECT CASE WHEN e.base_amount IS NULL AND e.payment_fee=0
      AND upper(btrim(e.payment_type))='EFS' AND lower(btrim(e.covered_by)) IN ('driver','truck owner')
      AND e.amount>0 AND NOT e.driver_settled
      AND NOT EXISTS(SELECT 1 FROM expense_settings c WHERE c.id=e.category_id AND c.driver_only)
      THEN e.amount+ceil(e.amount/500)*3 ELSE e.amount END
    INTO new_total FROM (SELECT NEW.*) e;
    IF old_total IS DISTINCT FROM new_total THEN
        RAISE EXCEPTION 'An expense used in a settlement cannot change its EFS fee or collectible total' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER expenses_settlement_total_guard BEFORE UPDATE ON expenses
FOR EACH ROW EXECUTE FUNCTION protect_expense_settlement_total();

COMMIT;

BEGIN;

-- Separate the work classification from freely named invoice cost rows.
-- No existing service, invoice, payment or approval records are rewritten.
CREATE OR REPLACE FUNCTION maintenance_validate_record(record_id UUID) RETURNS VOID LANGUAGE plpgsql AS $$
DECLARE r maintenance_records; e expenses; item JSONB; total NUMERIC:=0; priced INTEGER:=0; n INTEGER:=0;
BEGIN
 SELECT * INTO r FROM maintenance_records WHERE id=record_id;
 IF NOT FOUND OR r.status='voided' THEN RETURN; END IF;
 IF r.expense_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM expenses ledger JOIN maintenance_configuration c ON c.category_id=ledger.category_id WHERE ledger.id=r.expense_id) THEN
  RAISE EXCEPTION 'Maintenance invoices retain their classification' USING ERRCODE='23514',CONSTRAINT='maintenance_work_total';
 END IF;
 IF r.status='needs_review' THEN RETURN; END IF;
 IF jsonb_array_length(r.services) NOT BETWEEN 1 AND 100 THEN
  RAISE EXCEPTION 'Add completed work items' USING ERRCODE='23514',CONSTRAINT='maintenance_work_total';
 END IF;
 FOR item IN SELECT value FROM jsonb_array_elements(r.services) LOOP
  n:=n+1;
  IF jsonb_typeof(item) IS DISTINCT FROM 'object' OR jsonb_typeof(item->'name') IS DISTINCT FROM 'string'
   OR length(btrim(item->>'name')) NOT BETWEEN 1 AND 100
   OR NOT EXISTS(SELECT 1 FROM maintenance_types WHERE code=item->>'typeCode')
   OR coalesce(item->>'trackingCode','') NOT IN ('','pm','oil')
   OR (item->>'trackingCode'='pm' AND item->>'typeCode'<>'preventive') OR (item->>'trackingCode'='oil' AND item->>'typeCode'<>'oil') THEN
   RAISE EXCEPTION 'Invalid maintenance work' USING ERRCODE='23514',CONSTRAINT='maintenance_work_total';
  END IF;
  IF item->>'amount' IS NOT NULL THEN
   IF (item->>'amount') !~ '^-?[0-9]{1,12}(\.[0-9]{1,2})?$' THEN
    RAISE EXCEPTION 'Invalid maintenance cost' USING ERRCODE='23514',CONSTRAINT='maintenance_work_total';
   END IF;
   priced:=priced+1; total:=total+(item->>'amount')::numeric;
  END IF;
 END LOOP;
 IF priced<>0 AND priced<>n THEN
  RAISE EXCEPTION 'Price all work items or leave all unallocated' USING ERRCODE='23514',CONSTRAINT='maintenance_work_total';
 END IF;
 IF r.expense_id IS NOT NULL THEN
  SELECT * INTO e FROM expenses WHERE id=r.expense_id;
  IF e.truck_id IS DISTINCT FROM r.truck_id OR NOT EXISTS(SELECT 1 FROM maintenance_configuration WHERE category_id=e.category_id)
   OR (priced>0 AND total<>coalesce(e.base_amount,e.amount)) THEN
   RAISE EXCEPTION 'Maintenance work must match its financial record' USING ERRCODE='23514',CONSTRAINT='maintenance_work_total';
  END IF;
  -- Expense cost rows are independent invoice detail. Their exact base-amount
  -- sum is enforced by expense_cost_breakdown_total; typed work retains its own total.
 ELSIF priced>0 AND total<>0 THEN
  RAISE EXCEPTION 'Paid work needs a financial record' USING ERRCODE='23514',CONSTRAINT='maintenance_work_total';
 END IF;
END $$;

COMMIT;

BEGIN;

ALTER TABLE driver_board ADD COLUMN trailer_note TEXT NOT NULL DEFAULT '' CHECK (char_length(trailer_note) <= 5000);

CREATE OR REPLACE FUNCTION audit_driver_board_change() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    previous JSONB; following JSONB; changed_before JSONB; changed_after JSONB;
    driver UUID; driver_name_snapshot TEXT; actor TEXT; actor_name_snapshot TEXT;
BEGIN
    IF TG_TABLE_NAME = 'drivers' THEN
        IF OLD.driver_home IS NOT DISTINCT FROM NEW.driver_home THEN RETURN NEW; END IF;
        driver := NEW.id;
        previous := jsonb_build_object('driverHome',OLD.driver_home);
        following := jsonb_build_object('driverHome',NEW.driver_home);
    ELSE
        driver := NEW.driver_id;
        following := jsonb_build_object('currentLoad',NEW.current_load,'trailerNumber',NEW.trailer_number,
          'trailerNote',NEW.trailer_note,'status',NEW.status,'destination',NEW.destination,'eta',NEW.eta,'notes',NEW.notes,'homeTime',NEW.home_time);
        IF TG_OP = 'INSERT' THEN
            SELECT jsonb_object_agg(key,''::text) INTO previous FROM jsonb_each(following);
        ELSE
            previous := jsonb_build_object('currentLoad',OLD.current_load,'trailerNumber',OLD.trailer_number,
              'trailerNote',OLD.trailer_note,'status',OLD.status,'destination',OLD.destination,'eta',OLD.eta,'notes',OLD.notes,'homeTime',OLD.home_time);
        END IF;
    END IF;
    SELECT jsonb_object_agg(key,previous->key), jsonb_object_agg(key,value)
      INTO changed_before,changed_after FROM jsonb_each(following)
      WHERE previous->key IS DISTINCT FROM value;
    IF changed_after IS NULL THEN RETURN NEW; END IF;
    SELECT full_name INTO driver_name_snapshot FROM drivers WHERE id=driver;
    actor := coalesce(current_setting('mserp.board_actor',true),'');
    SELECT username INTO actor_name_snapshot FROM app_users WHERE id::text=actor;
    INSERT INTO driver_board_history(driver_id,driver_name,actor_id,actor_name,source,undo_of,before_values,after_values)
    VALUES(driver,driver_name_snapshot,actor,coalesce(actor_name_snapshot,'System'),
      coalesce(nullif(current_setting('mserp.board_source',true),''),'database'),
      nullif(current_setting('mserp.board_undo',true),'')::bigint,changed_before,changed_after)
    ON CONFLICT(driver_id,transaction_id) DO UPDATE SET
      before_values=EXCLUDED.before_values || driver_board_history.before_values,
      after_values=driver_board_history.after_values || EXCLUDED.after_values;
    RETURN NEW;
END $$;

COMMIT;

-- Permissions v2 (migration 112).
BEGIN;
-- Capability cutover: sensitive profile and document access requires explicit review.
CREATE TEMP TABLE permission_before ON COMMIT DROP AS SELECT r.id,r.name,r.permissions,coalesce((SELECT jsonb_agg(to_jsonb(a) ORDER BY a.category_id) FROM role_expense_category_access a WHERE a.role_id=r.id),'[]'::jsonb) AS categories FROM app_roles r WHERE NOT system_role;
UPDATE app_roles r SET permissions=ARRAY(SELECT DISTINCT expanded FROM unnest(r.permissions) old CROSS JOIN LATERAL unnest(CASE old
 WHEN 'fuel.read' THEN ARRAY['fuel.read','fuel.drivers.sync']::text[]
 WHEN 'tolls.read' THEN ARRAY['tolls.read','tolls.fleet.sync']::text[]
 WHEN 'fleet.read' THEN ARRAY['drivers.read','trucks.read','dispatchers.read','updaters.read','investors.read']::text[]
 WHEN 'fleet.write' THEN ARRAY['drivers.create','drivers.edit','drivers.delete','trucks.create','trucks.edit','trucks.delete','dispatchers.create','dispatchers.edit','dispatchers.delete','updaters.create','updaters.edit','updaters.delete','investors.create','investors.edit']::text[]
 WHEN 'payroll.read' THEN ARRAY['driver_pay.read','investor_pay.read','dispatcher_pay.read','driver_compensation.read','dispatcher_compensation.read']::text[]
 WHEN 'payroll.write' THEN ARRAY['driver_pay.edit','investor_pay.edit','driver_compensation.edit','dispatcher_compensation.edit']::text[]
 WHEN 'payroll.finalize' THEN ARRAY['driver_pay.finalize','driver_pay.reopen','investor_pay.finalize','investor_pay.reopen']::text[]
 WHEN 'charges.read' THEN ARRAY['charges.read','investor_terms.read']::text[]
 WHEN 'charges.write' THEN ARRAY['charges.create','charges.edit','charges.delete','investor_terms.edit']::text[]
 WHEN 'escrow.write' THEN ARRAY['escrow.create','escrow.edit','escrow.delete']::text[]
 WHEN 'tasks.write' THEN ARRAY['tasks.create','tasks.edit','tasks.delete','tasks.review']::text[]
 WHEN 'fuel.drivers.write' THEN ARRAY['fuel.drivers.create','fuel.drivers.edit','fuel.drivers.delete','fuel.drivers.recover']::text[]
 WHEN 'tolls.fleet.write' THEN ARRAY['tolls.fleet.create','tolls.fleet.edit','tolls.fleet.delete','tolls.fleet.recover']::text[]
 WHEN 'weighmytruck.write' THEN ARRAY['weighmytruck.create','weighmytruck.delete','weighmytruck.sync','weighmytruck.link','weighmytruck.recover']::text[]
 ELSE ARRAY[old] END) expanded WHERE expanded=ANY(ARRAY['fuel.drivers.sync','fuel.drivers.recover','tolls.fleet.sync','drivers.read','drivers.create','drivers.edit','drivers.delete','trucks.read','trucks.create','trucks.edit','trucks.delete','dispatchers.read','dispatchers.create','dispatchers.edit','dispatchers.delete','updaters.read','updaters.create','updaters.edit','updaters.delete','investors.read','investors.create','investors.edit','drivers.private.read','drivers.private.edit','drivers.documents.read','drivers.documents.edit','trucks.documents.read','trucks.documents.edit','driver_compensation.read','driver_compensation.edit','dispatcher_compensation.read','dispatcher_compensation.edit','investor_terms.read','investor_terms.edit','driver_pay.read','driver_pay.edit','driver_pay.finalize','driver_pay.reopen','investor_pay.read','investor_pay.edit','investor_pay.finalize','investor_pay.reopen','dispatcher_pay.read','driver_board.read','driver_board.write','board.read','board.write','loads.read','loads.sync','fuel.read','fuel.sync','fuel.drivers.create','fuel.drivers.edit','fuel.drivers.delete','tolls.read','tolls.sync','tolls.link','tolls.fleet.create','tolls.fleet.edit','tolls.fleet.delete','tolls.fleet.recover','weighmytruck.read','weighmytruck.create','weighmytruck.delete','weighmytruck.sync','weighmytruck.link','weighmytruck.recover','escrow.read','escrow.create','escrow.edit','escrow.delete','charges.read','charges.create','charges.edit','charges.delete','tasks.read','tasks.create','tasks.edit','tasks.delete','tasks.review','fleetscope.read','fleetscope.manage','fleetscope.reconcile','reports.read','expense_settings.manage','expenses.accounting.verify','access.manage']::text[]) ORDER BY expanded),version=version+1 WHERE NOT system_role;
UPDATE app_roles SET permissions=array_append(permissions,'expenses.accounting.verify') WHERE NOT system_role AND lower(trim(name))='accountant' AND NOT ('expenses.accounting.verify'=ANY(permissions));
-- Remove actions whose prerequisites were not granted; never add read access to repair old writes.
UPDATE app_roles SET permissions=array_remove(permissions,'drivers.create') WHERE NOT system_role AND NOT ARRAY['drivers.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'drivers.edit') WHERE NOT system_role AND NOT ARRAY['drivers.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'drivers.delete') WHERE NOT system_role AND NOT ARRAY['drivers.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'trucks.create') WHERE NOT system_role AND NOT ARRAY['trucks.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'trucks.edit') WHERE NOT system_role AND NOT ARRAY['trucks.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'trucks.delete') WHERE NOT system_role AND NOT ARRAY['trucks.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'dispatchers.create') WHERE NOT system_role AND NOT ARRAY['dispatchers.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'dispatchers.edit') WHERE NOT system_role AND NOT ARRAY['dispatchers.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'dispatchers.delete') WHERE NOT system_role AND NOT ARRAY['dispatchers.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'updaters.create') WHERE NOT system_role AND NOT ARRAY['updaters.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'updaters.edit') WHERE NOT system_role AND NOT ARRAY['updaters.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'updaters.delete') WHERE NOT system_role AND NOT ARRAY['updaters.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'investors.create') WHERE NOT system_role AND NOT ARRAY['investors.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'investors.edit') WHERE NOT system_role AND NOT ARRAY['investors.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'drivers.private.read') WHERE NOT system_role AND NOT ARRAY['drivers.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'drivers.private.edit') WHERE NOT system_role AND NOT ARRAY['drivers.read','drivers.private.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'drivers.documents.read') WHERE NOT system_role AND NOT ARRAY['drivers.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'drivers.documents.edit') WHERE NOT system_role AND NOT ARRAY['drivers.read','drivers.documents.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'trucks.documents.read') WHERE NOT system_role AND NOT ARRAY['trucks.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'trucks.documents.edit') WHERE NOT system_role AND NOT ARRAY['trucks.read','trucks.documents.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'driver_compensation.edit') WHERE NOT system_role AND NOT ARRAY['driver_compensation.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'dispatcher_compensation.edit') WHERE NOT system_role AND NOT ARRAY['dispatcher_compensation.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'investor_terms.edit') WHERE NOT system_role AND NOT ARRAY['investor_terms.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'driver_pay.edit') WHERE NOT system_role AND NOT ARRAY['driver_pay.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'driver_pay.finalize') WHERE NOT system_role AND NOT ARRAY['driver_pay.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'driver_pay.reopen') WHERE NOT system_role AND NOT ARRAY['driver_pay.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'investor_pay.edit') WHERE NOT system_role AND NOT ARRAY['investor_pay.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'investor_pay.finalize') WHERE NOT system_role AND NOT ARRAY['investor_pay.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'investor_pay.reopen') WHERE NOT system_role AND NOT ARRAY['investor_pay.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'driver_board.write') WHERE NOT system_role AND NOT ARRAY['driver_board.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'board.write') WHERE NOT system_role AND NOT ARRAY['board.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'loads.sync') WHERE NOT system_role AND NOT ARRAY['loads.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'fuel.sync') WHERE NOT system_role AND NOT ARRAY['fuel.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'fuel.drivers.create') WHERE NOT system_role AND NOT ARRAY['fuel.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'fuel.drivers.edit') WHERE NOT system_role AND NOT ARRAY['fuel.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'fuel.drivers.delete') WHERE NOT system_role AND NOT ARRAY['fuel.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'tolls.sync') WHERE NOT system_role AND NOT ARRAY['tolls.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'tolls.link') WHERE NOT system_role AND NOT ARRAY['tolls.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'tolls.fleet.create') WHERE NOT system_role AND NOT ARRAY['tolls.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'tolls.fleet.edit') WHERE NOT system_role AND NOT ARRAY['tolls.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'tolls.fleet.delete') WHERE NOT system_role AND NOT ARRAY['tolls.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'tolls.fleet.recover') WHERE NOT system_role AND NOT ARRAY['tolls.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'weighmytruck.create') WHERE NOT system_role AND NOT ARRAY['weighmytruck.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'weighmytruck.delete') WHERE NOT system_role AND NOT ARRAY['weighmytruck.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'weighmytruck.sync') WHERE NOT system_role AND NOT ARRAY['weighmytruck.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'weighmytruck.link') WHERE NOT system_role AND NOT ARRAY['weighmytruck.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'weighmytruck.recover') WHERE NOT system_role AND NOT ARRAY['weighmytruck.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'escrow.create') WHERE NOT system_role AND NOT ARRAY['escrow.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'escrow.edit') WHERE NOT system_role AND NOT ARRAY['escrow.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'escrow.delete') WHERE NOT system_role AND NOT ARRAY['escrow.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'charges.create') WHERE NOT system_role AND NOT ARRAY['charges.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'charges.edit') WHERE NOT system_role AND NOT ARRAY['charges.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'charges.delete') WHERE NOT system_role AND NOT ARRAY['charges.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'tasks.create') WHERE NOT system_role AND NOT ARRAY['tasks.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'tasks.edit') WHERE NOT system_role AND NOT ARRAY['tasks.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'tasks.delete') WHERE NOT system_role AND NOT ARRAY['tasks.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'tasks.review') WHERE NOT system_role AND NOT ARRAY['tasks.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'fleetscope.manage') WHERE NOT system_role AND NOT ARRAY['fleetscope.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(permissions,'fleetscope.reconcile') WHERE NOT system_role AND NOT ARRAY['fleetscope.read']::text[] <@ permissions;
UPDATE app_roles SET permissions=array_remove(array_remove(permissions,'fuel.drivers.sync'),'fuel.drivers.recover') WHERE NOT system_role AND NOT ('fuel.read'=ANY(permissions));
UPDATE app_roles SET permissions=array_remove(permissions,'tolls.fleet.sync') WHERE NOT system_role AND NOT ('tolls.read'=ANY(permissions));
UPDATE role_expense_category_access SET can_create=false,can_edit=false,can_delete=false WHERE NOT can_view;
INSERT INTO access_audit(actor_id,action,target_id,details) SELECT NULL,'permissions_v2_migration',r.id,jsonb_build_object('before',b.permissions,'after',r.permissions,'categoriesBefore',b.categories,'categoriesAfter',coalesce((SELECT jsonb_agg(to_jsonb(a) ORDER BY a.category_id) FROM role_expense_category_access a WHERE a.role_id=r.id),'[]'::jsonb),'reviewRequired',true,'reason','Review private details, documents and combined setup prerequisites') FROM app_roles r JOIN permission_before b USING(id);
DELETE FROM auth_sessions;
ALTER TABLE files ADD COLUMN IF NOT EXISTS uploaded_by uuid REFERENCES app_users(id);
COMMIT;

BEGIN;

-- Invalidations are delivered only after commit and never contain board data.
CREATE FUNCTION notify_status_board_change() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    PERFORM pg_notify('mserp_driver_board','');
    RETURN NULL;
END $$;
DO $$ DECLARE name text; BEGIN
    FOREACH name IN ARRAY ARRAY['driver_board','driver_board_load_state','drivers','trucks','investors',
      'dispatchers','updaters','dispatcher_updaters','truck_driver_assignments','driver_teams',
      'gross_board_entries','gross_board_extra_entries','loads','five_eld_locations'] LOOP
        EXECUTE format('CREATE TRIGGER status_board_changed AFTER INSERT OR UPDATE OR DELETE ON %I FOR EACH STATEMENT EXECUTE FUNCTION notify_status_board_change()',name);
    END LOOP;
END $$;

-- Tab-scoped, expiring cursors. Session deletion removes presence immediately.
CREATE TABLE driver_board_presence (
    client_id uuid PRIMARY KEY,
    user_id uuid NOT NULL REFERENCES app_users(id) ON DELETE CASCADE,
    session_hash char(64) NOT NULL REFERENCES auth_sessions(token_hash) ON DELETE CASCADE,
    driver_id uuid NOT NULL REFERENCES drivers(id) ON DELETE CASCADE,
    column_key text NOT NULL CHECK(column_key IN ('currentLoad','driver','truck','trailerNumber','originalGross','driverGross','phone','status','destination','notes','eta','loadPlan','homeTime')),
    expires_at timestamptz NOT NULL
);
CREATE INDEX driver_board_presence_expiry ON driver_board_presence(expires_at);
CREATE FUNCTION notify_board_presence_change() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='UPDATE' AND OLD.driver_id=NEW.driver_id AND OLD.column_key=NEW.column_key THEN RETURN NULL; END IF;
    PERFORM pg_notify('mserp_board_presence','');
    RETURN NULL;
END $$;
CREATE TRIGGER board_presence_changed AFTER INSERT OR UPDATE OR DELETE ON driver_board_presence
FOR EACH ROW EXECUTE FUNCTION notify_board_presence_change();
INSERT INTO payroll_cache_revisions(table_name) VALUES('driver_board_presence');
-- Presence is operational metadata and is excluded by payroll_cache.go.
DO $$ BEGIN
    IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
        ALTER TABLE driver_board_presence OWNER TO mserp_app;
    END IF;
END $$;
COMMIT;

BEGIN;
CREATE FUNCTION notify_gross_board_change() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN PERFORM pg_notify('mserp_gross_board',''); RETURN NULL; END $$;
DO $$ DECLARE name text; BEGIN
    FOREACH name IN ARRAY ARRAY['gross_board_entries','gross_board_extra_entries','loads','drivers','trucks',
      'dispatchers','truck_driver_assignments','driver_teams','driver_dispatcher_assignments'] LOOP
        EXECUTE format('CREATE TRIGGER gross_board_changed AFTER INSERT OR UPDATE OR DELETE ON %I FOR EACH STATEMENT EXECUTE FUNCTION notify_gross_board_change()',name);
    END LOOP;
END $$;
CREATE TABLE gross_board_presence (
    client_id uuid PRIMARY KEY,
    user_id uuid NOT NULL REFERENCES app_users(id) ON DELETE CASCADE,
    session_hash char(64) NOT NULL REFERENCES auth_sessions(token_hash) ON DELETE CASCADE,
    driver_id uuid NOT NULL REFERENCES drivers(id) ON DELETE CASCADE,
    week_start date NOT NULL CHECK(extract(isodow FROM week_start)=1 AND week_start BETWEEN '2000-01-03' AND '2100-12-27'),
    cell_date date,
    slot integer NOT NULL CHECK(slot BETWEEN -1 AND 99),
    column_key text NOT NULL CHECK(column_key IN ('driver','truck','loadNumber','originalRate','driverRate','miles','note','originalTotal','driverTotal','milesTotal','balance')),
    expires_at timestamptz NOT NULL,
    CHECK((column_key IN ('driver','truck','originalTotal','driverTotal','milesTotal','balance') AND cell_date IS NULL AND slot=0)
       OR (column_key IN ('loadNumber','originalRate','driverRate','miles','note') AND cell_date BETWEEN week_start AND week_start+6 AND cell_date IS NOT NULL))
);
CREATE INDEX gross_board_presence_expiry ON gross_board_presence(expires_at);
CREATE INDEX gross_board_presence_week ON gross_board_presence(week_start);
CREATE FUNCTION notify_gross_presence_change() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='UPDATE' AND OLD.driver_id=NEW.driver_id AND OLD.week_start=NEW.week_start
      AND OLD.cell_date IS NOT DISTINCT FROM NEW.cell_date AND OLD.slot=NEW.slot AND OLD.column_key=NEW.column_key THEN RETURN NULL; END IF;
    PERFORM pg_notify('mserp_gross_presence',''); RETURN NULL;
END $$;
CREATE TRIGGER gross_presence_changed AFTER INSERT OR UPDATE OR DELETE ON gross_board_presence
FOR EACH ROW EXECUTE FUNCTION notify_gross_presence_change();
INSERT INTO payroll_cache_revisions(table_name) VALUES('gross_board_presence');
DO $$ BEGIN
    IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN ALTER TABLE gross_board_presence OWNER TO mserp_app; END IF;
END $$;
COMMIT;

BEGIN;

-- Poll evidence is distinct from human edits/history and never changes progress.
CREATE TABLE driver_board_automation (
 driver_id uuid PRIMARY KEY REFERENCES drivers(id) ON DELETE CASCADE,
 evidence jsonb NOT NULL DEFAULT '{}'::jsonb,
 travel jsonb NOT NULL DEFAULT '{}'::jsonb,
 eta_context text NOT NULL DEFAULT '',
 updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TRIGGER status_board_changed AFTER INSERT OR UPDATE OR DELETE ON driver_board_automation
FOR EACH ROW EXECUTE FUNCTION notify_status_board_change();
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
  ALTER TABLE driver_board_automation OWNER TO mserp_app;
 END IF;
END $$;

COMMIT;

BEGIN;

-- Keep login history after sessions expire or are revoked. Activity deliberately
-- lives outside app_users so heartbeats do not invalidate access/payroll caches.
CREATE TABLE user_login_activity (
    user_id UUID PRIMARY KEY REFERENCES app_users(id) ON DELETE CASCADE,
    last_login_at TIMESTAMPTZ NOT NULL
);
INSERT INTO user_login_activity(user_id,last_login_at)
SELECT user_id,max(created_at) FROM auth_sessions GROUP BY user_id;

-- Existing sessions must send a browser heartbeat before being considered online.
ALTER TABLE auth_sessions ADD COLUMN last_seen_at TIMESTAMPTZ;

DO $$ BEGIN
    IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
        ALTER TABLE user_login_activity OWNER TO mserp_app;
    END IF;
END $$;

COMMIT;
