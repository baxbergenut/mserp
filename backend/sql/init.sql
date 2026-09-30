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
