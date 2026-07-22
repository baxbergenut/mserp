BEGIN;

CREATE TABLE IF NOT EXISTS files (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    file_name    TEXT NOT NULL,
    content_type TEXT NOT NULL,
    size_bytes   BIGINT NOT NULL CHECK (size_bytes >= 0),
    sha256       CHAR(64) NOT NULL,
    data         BYTEA NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (octet_length(data) = size_bytes)
);

CREATE INDEX IF NOT EXISTS files_sha256_idx ON files (sha256);

ALTER TABLE trucks
    ADD COLUMN IF NOT EXISTS irp_file_id UUID REFERENCES files(id) ON DELETE SET NULL;

COMMIT;
