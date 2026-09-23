BEGIN;

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

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'mserp_app') THEN
        ALTER TABLE expenses OWNER TO mserp_app;
    END IF;
END
$$;

COMMIT;
