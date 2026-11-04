# DataTruck load refresh

Scheduled jobs share one client and one paced request gate (15 attempts/minute,
including retries). Run only one scheduled API instance per provider token.

| Job | Default interval | Purpose |
| --- | --- | --- |
| Modified loads | 30 seconds | Refresh existing orders whose `modified_datetime` changed, regardless of service date |
| New IDs | 1 minute | Import all IDs above the local discovery watermark |
| Operational reconciliation | 15 minutes | Recheck existing IDs, including missing/replaced loads |
| Service-date reconciliation | Daily, configured load sync time | Recover missing historical IDs and recheck a rolling 21-day pickup/delivery window |

`DATATRUCK_MODIFIED_SYNC_INTERVAL` accepts 30s–24h. Existing
`DATATRUCK_NEW_LOADS_INTERVAL` and `DATATRUCK_OPERATIONAL_SYNC_INTERVAL` accept
1m–24h. All jobs obey `SCHEDULED_SYNCS_ENABLED`. Explicit environment overrides
take precedence over defaults; update an old operational `1m`/`5m` override when
activating the slower reconciliation cadence.

## Incremental contract

Live edits verified that trip driver gross (`total_load_pay`), loaded miles and
empty miles advance the parent order's `modified_datetime`, and that the public
orders endpoint returns the edited order through the `after` filter. This does
not establish that every possible provider edit does so; slower checks remain.

Each sweep uses a fixed timestamp lower bound and ascending **ID keyset** pages
of 25. Every request starts at page one with `id greater_than lastID+1` (the
numeric operator is inclusive). Offset `next` links are not followed: edits or
deletions while paginating must not shift an unprocessed order past an offset.
Returned timestamps, IDs, ordering and page size are validated before saving.

Migration 196 stores a checkpoint per provider base URL. First activation starts
24 hours back; restarts and outages resume the stored checkpoint without an age
cutoff. A successful sweep advances to its **start time**, with a two-minute
overlap on the next query. Edits to already-passed IDs are therefore replayed in
the following sweep. Small timing/visibility delays are covered by the overlap;
slow reconciliation remains the fallback for changes outside that guarantee.

Pages commit before the checkpoint advances. Any HTTP/validation/database failure
retains the old checkpoint; retries replay safely. A checkpoint compare-and-swap
rejects concurrent movement or regression. New IDs returned by a modified query
are left for discovery: modified refresh cannot skip undiscovered orders by
advancing `MAX(loads.id)`. Filtered omissions never imply deletion.

Modified pages and discovery take priority over queued reconciliation requests.
Sweeps do not overlap. Thirty seconds is the polling cadence, not a hard delivery
deadline: multiple pages, provider latency, shared traffic and 429 cooldowns can
take longer. An empty sweep needs one provider request (normally two per minute).

## Source writes and accounting

Provider modification timestamps are stored in `datatruck_load_versions`, outside
canonical raw payloads and payroll capture. Every import, reconciliation and
payroll detail refresh rejects older provider versions under the existing source
lock. A slow response cannot overwrite a newer modified-feed value. Metadata
tables are explicitly unrelated to payroll; canonical loads retain their normal
financial capture and finalization rules.

Identical source values skip load-row writes during overlap replay. Slower checks
can renew `synced_at` after 15 minutes so data-quality freshness remains meaningful;
that timestamp alone does not invalidate financial revisions. Provider timestamps
changing without canonical values also do not trigger recalculation.

Manual driver gross, total gross and miles remain saved and discrepancies remain
visible. Only the existing explicitly pending blank driver gross can initialize
once. Finalized financial values stay frozen. Missing-source detection and the
reviewed duplicate-load repair workflow remain in place; this feed never merges
loads by their displayed number or automatically reassigns an old selection.

## Validation and operations

- Client tests reject ignored filters, absent/old timestamps, duplicate or
  unordered IDs and malformed pagination; metadata stays out of raw payloads.
- Job tests cover partial-page failure, save/checkpoint failure, cancellation,
  overlap replay and independence from slow reconciliation.
- `TestModifiedLoadsDatabase` uses local HTTP plus a disposable PostgreSQL database
  (`MSERP_GROSS_BOARD_TEST_DATABASE_URL`) through the production client, job and
  repository. It checks migration, pagination, saved manual values versus updated
  Gross Board comparisons, stale-writer rejection, no-op row/revision stability,
  unseen-ID handling, restart checkpoints and empty-query omission semantics.
- Existing driver-gross, source-availability, duplicate-resolution and payroll
  regressions continue to apply. Install the matching deployment helper before
  migration 196; it quiesces writers for schema installation.

Verify `modified load sync complete` journal events and increasing
`datatruck_load_sync_state.through_at`/`completed_at` after deployment. A process
start or an interval start alone does not establish successful refresh. Check
source values and preserved board values for a known edited order as well.
