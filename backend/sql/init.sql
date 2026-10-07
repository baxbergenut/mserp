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
