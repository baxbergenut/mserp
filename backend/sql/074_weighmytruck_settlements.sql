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
