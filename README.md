# mserp
ERP for MS Express Inc.

## Document extraction

Truck cab cards are stored directly in Postgres and can be uploaded from the
truck create/edit form. PDF, PNG, JPEG, and WEBP files up to 10 MB are
supported. The original file is retained; GROQ fills the truck fields for a
user to review before saving.

Driver CDLs use the same workflow from the driver create/edit form. GROQ fills
the driver's legal name, license number, issuing state, expiration date, and
address fields while retaining the original license document in Postgres.

Add the API key to the Git-ignored `backend/.env.local`:

```dotenv
GROQ_API_KEY=your-key-here
# Optional; defaults to the current GROQ vision model below.
GROQ_MODEL=qwen/qwen3.6-27b
```

Existing databases must apply `backend/sql/003_add_files_and_truck_irp.sql`
and `backend/sql/004_add_driver_cdl.sql`.

## Relay fuel integration

Relay fuel transactions are synced from the Fuel API and stored with normalized
driver, location, fuel/DEF/product, and fee data. The full Relay payload is also
retained for future reports. Add credentials to the Git-ignored
`backend/.env.relay.local`:

```dotenv
RELAY_ENVIRONMENT=production # or staging
RELAY_STAGING_API_KEY=your-staging-key
RELAY_PRODUCTION_API_KEY=your-production-key
# Optional initial backfill boundary; defaults to 30 days before startup.
RELAY_FUEL_SYNC_START_DATE=2026-01-01
```

The manual sync records every completed UTC date it successfully checks, even
when no transactions are returned. It rechecks the current date every time so
later same-day purchases are included. Existing databases must apply
`backend/sql/005_add_fuel.sql` and
`backend/sql/006_allow_multiple_relay_driver_ids.sql`.

## PrePass toll integration

Tolls are fetched directly from the PrePass Account and Toll Transaction APIs.
The sync discovers active accounts automatically, retrieves posting-date ranges
with pagination, and upserts by PrePass toll ID. Add credentials to the
Git-ignored `backend/.env.local`:

```dotenv
PREPASS_ENVIRONMENT=production # or nonproduction
PREPASS_PRODUCTION_CLIENT_ID=your-client-id
PREPASS_PRODUCTION_CLIENT_SECRET=your-client-secret
PREPASS_NONPRODUCTION_CLIENT_ID=your-nonproduction-client-id
PREPASS_NONPRODUCTION_CLIENT_SECRET=your-nonproduction-client-secret
# Optional; defaults to the maximum two-year PrePass history.
PREPASS_TOLL_SYNC_START_DATE=2024-07-30
```

Completed UTC posting dates are skipped on later runs. The current UTC date is
rechecked, and unmatched vehicle numbers remain stored until a matching truck is
added. Existing databases must apply
`backend/sql/010_add_prepass_toll_sync.sql` and
`backend/sql/011_fix_prepass_sync_table_owner.sql`.

## Scheduled data syncs

The API process runs the load, fuel, and toll sync jobs every day. By default,
loads sync at 6:00 AM, fuel at 6:30 AM, and tolls at 7:00 AM in
`America/New_York`. The scheduler uses the same in-process jobs as the manual
API actions, so it does not require an application user session.

The schedule can be customized in the backend environment:

```dotenv
SCHEDULED_SYNCS_ENABLED=true
SCHEDULED_SYNCS_TIMEZONE=America/New_York
SCHEDULED_LOADS_SYNC_TIME=06:00
SCHEDULED_FUEL_SYNC_TIME=06:30
SCHEDULED_TOLLS_SYNC_TIME=07:00
```

Times use 24-hour `HH:MM` format. Set `SCHEDULED_SYNCS_ENABLED=false` to disable
the scheduled jobs for a local or secondary API process.
