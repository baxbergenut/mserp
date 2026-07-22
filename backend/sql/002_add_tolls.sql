BEGIN;

CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE IF NOT EXISTS toll_reports (
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

CREATE INDEX IF NOT EXISTS toll_reports_imported_at_idx ON toll_reports (imported_at DESC);
CREATE INDEX IF NOT EXISTS toll_reports_file_sha256_idx ON toll_reports (file_sha256);

CREATE TABLE IF NOT EXISTS tolls (
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

CREATE INDEX IF NOT EXISTS tolls_posting_date_idx ON tolls (posting_date DESC);
CREATE INDEX IF NOT EXISTS tolls_truck_posting_date_idx ON tolls (truck_id, posting_date DESC);
CREATE INDEX IF NOT EXISTS tolls_report_id_idx ON tolls (report_id);

COMMIT;
