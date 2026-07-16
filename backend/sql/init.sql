CREATE TABLE drivers (
    id              SERIAL PRIMARY KEY,
    full_name       TEXT NOT NULL,
    normalized_name TEXT NOT NULL,   -- lowercase, trimmed, for matching
    pay_type        TEXT NOT NULL,   -- 'per_mile', 'percentage', 'flat'
    rate            NUMERIC(10,4),   -- interpretation depends on pay_type
    active          BOOLEAN DEFAULT true,
    created_at      TIMESTAMPTZ DEFAULT now()
);

CREATE TABLE dispatchers (
    id              SERIAL PRIMARY KEY,
    full_name       TEXT NOT NULL,
    normalized_name TEXT NOT NULL,
    pay_percentage  NUMERIC(5,4),    -- e.g. 0.03 for 3%
    active          BOOLEAN DEFAULT true,
    created_at      TIMESTAMPTZ DEFAULT now()
);

CREATE TABLE loads (
    id              INTEGER PRIMARY KEY,        -- DataTruck's own id, use as your PK or unique key
    load_id         TEXT UNIQUE NOT NULL,        -- e.g. "T-111QT34XS"
    driver_id        INTEGER REFERENCES drivers(id),
    dispatcher_id    INTEGER REFERENCES dispatchers(id),
    shipment_id     TEXT,                        -- e.g. "DT-002489"
    status          TEXT NOT NULL,               -- dispatched, delivered, etc.

    load_pay        NUMERIC(10,2) NOT NULL,
    total_other_pay NUMERIC(10,2) DEFAULT 0,
    total_pay       NUMERIC(10,2) NOT NULL,       -- gross for this load
    total_miles     NUMERIC(10,2),
    per_mile_revenue NUMERIC(10,4),

    dispatcher_name TEXT,                         -- from dispatcher__full_name
    driver_name     TEXT,                         -- from trip.driver__full_name
    team_driver_name TEXT,                        -- from trip.team_driver__full_name
    truck_unit      TEXT,                         -- from trip.truck__unit_number
    customer_name   TEXT,                         -- customer__company_name

    pickup_time     TIMESTAMPTZ,
    delivery_time   TIMESTAMPTZ,
    pickup_appointment_time   TIMESTAMPTZ,
    delivery_appointment_time TIMESTAMPTZ,

    created_datetime TIMESTAMPTZ,
    synced_at        TIMESTAMPTZ DEFAULT now(),   -- when YOU last pulled this record

    raw_payload      JSONB                        -- keep the full original response, just in case
);