BEGIN;

CREATE EXTENSION IF NOT EXISTS pgcrypto;

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

COMMIT;
