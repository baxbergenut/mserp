# Board opening performance

The target is a usable visible board within 400 ms where practical. API response
time, cold document loading, viewport readiness and off-screen editor mounting
are separate measurements. Do not present a fast query or skeleton as a complete
page-load measurement.

## November 12, 2026 measurements

Before this change, two hours of production logs contained 672 Status Board GETs
(median 886 ms, p95 3,183 ms) and 61 Gross Board GETs (median 970 ms, p95 2,649 ms).
These are API durations, not browser timings. Database execution dominated;
connection-pool waits were negligible in the inspected samples.

A local Chromium benchmark uses 160 synthetic drivers and 640 weekly load entries
at 1800 x 1000, with the production frontend build. Before progressive row mounting,
Gross Board navigation took 1,747–2,409 ms even with zero API delay. Warm Status
Board navigation took 531–899 ms. The old grid mounted all off-screen editors
before it could paint the visible rows.

With the optimization, a sequential local run measured:

| Scenario | Status Board | Gross Board |
| --- | --- | --- |
| Warm route, zero API delay | 210–247 ms | 186 ms |
| Warm route, simulated 150 ms API delay | 315–371 ms | 302–308 ms |
| First Gross Board route, simulated 150 ms API delay | — | 390 ms |
| Cold Status Board document, simulated 150 ms API delay | 519 ms | — |

These small synthetic samples establish a browser improvement, not a production
p95 guarantee. Off-screen editors finish mounting incrementally; the full data and
totals are already loaded. Mounted editors remain mounted. Scrolling and explicit
deep links prioritize their rows, and spreadsheet navigation/copy synchronously
finishes any pending rows before reading the logical grid.

## Database and refresh changes

- Migration 213 adds a partial expression index for the latest departure from an
  exact driver/source pair. It retains the original query and immutable history.
- Explicit source IDs use a primary-key branch; number-only entries use a separate
  unique normalized-number branch. Missing explicit IDs cannot fall back to names.
- Trip decisions and source status read typed columns rather than converting an
  entire imported order to JSON. Stop readers fetch only stops and appointment
  fallbacks. No balance, assignment, split or duplicate rule changes.
- Status Board uses a custom plan for its current request scope, compact overview
  responses, stream updates while connected, and retained snapshots on focusout.
  Disconnected polling and focus/reconnect recovery remain. Older HTTP refreshes
  cannot replace a newer stream snapshot.
- Truck dropdown choices use the existing live bulk fleet read projection instead
  of recalculating the complete operator graph separately for every truck. A live
  read-only comparison returned zero status differences across 147 trucks; query
  time fell from 256 ms to 8 ms in that sample. Reviewed handovers and assignment
  saves retain their authoritative status functions and revision checks.

A later pre-release observation found the production CPU fully occupied with
concurrent board and truck-choice reads. Under that contention, even the optimized
standalone board reader took seconds. The dropdown improvement and removal of
redundant connected refreshes address this shared load; the synthetic browser
results alone do not establish a sub-400 ms production guarantee.

Read-only repeatable-snapshot `EXCEPT ALL` comparisons against production returned
zero differences in both directions for current Gross Board entries, opening
balances and Status Board gross entries. Repository tests cover fresh and migrated
schemas, missing/ambiguous sources, reviewed splits, historical duplicates, stop
progress, and existing financial guards.

## Reproduce and verify

### Connection establishment

Opening previously calculated the board once for HTTP and again for the initial
event-stream snapshot. The stream did not flush its headers until that calculation
and any shared snapshot mutex wait finished, so Connecting could last seconds.
Streams now flush the authorized connection first. New clients request
`initial=none`. When the authenticated session advertises parallel opening,
the browser starts the stream and grid GET together with one tab-specific ID.
The API starts the snapshot after registering the matching viewer, board and
Gross Board week. If the requests reach different processes or the stream is
blocked, the opening attempt returns 204 after at most 150 ms of registration
wait without building a grid. The ordinary GET and reconnect then cover the
missed interval. Older clients retain their browser-side wait. Run
`node scripts/test-investors-e2e.mjs --board-opening-only` against the local
PostgreSQL fixture to compare both paths at 178 ms round-trip latency.
Invalidations received while loading remain pending until the board can apply them.
Legacy clients still receive an initial snapshot, and commit snapshots retain
per-subscriber permissions and existing draft/version checks.

Build with `NEXT_PUBLIC_API_URL=/api`, then run from `frontend/`:

```powershell
node scripts/benchmark-board-opening.mjs
$env:BOARD_API_DELAY_MS = '150'
node scripts/benchmark-board-opening.mjs
node scripts/test-status-board-refresh.mjs
node scripts/test-status-board-layout.mjs
node scripts/test-board-grid.mjs
node scripts/test-gross-board-reliability.mjs
node scripts/test-status-board-loads.mjs
```

Do not run the benchmark concurrently with builds or other browser suites. Compare
production API logs over separate release windows; retain sample counts and failures.
Use the existing `Server-Timing` response header to distinguish app, database and
pool time. After deployment, verify migration 213 and the indexed history query on
the live database before claiming its production speedup.

## Gross Board writes and weekly reads

Single-entry saves roll back the whole transaction on a rejected edit. Multi-entry
saves keep their savepoints for independent groups. Prior load identity, trip and
transfer protection are read together. The weekly balance query feeds only
complex allocations to the trip validator. Board grids use the same trip-field
parsing for ordinary single-trip loads and retain the full split validation
for selected trips or reviewed allocations.

Status Board requests can use `overview=1&compact=grid` for weekly totals and
current-load suggestions. Full Gross Board editing data stays on its own
endpoint. The route and stop-city workers omit the display-only commission,
weekly gross, ELD summary and pending-driver reads. The ETA sweep checks READY
location freshness before taking a driver lock, then rechecks due work under
that lock. It writes arrival evidence and ETA context only when needed.

Single-row version conflicts and overlapping text edits return the board values
already read under the driver lock. The browser uses them to rebase independent
edits or show the saved value next to the local draft. A version check still
applies to the next submission. Accepting the saved values replaces only the
conflicting field groups. Independent draft edits on the same driver still save.
Older responses use the board refresh fallback.

The automated DataTruck replacement job yields to an interactive board command
during an uncommitted repair. A retry after the board is quiet checks provider
availability, current load identity, board versions and financial protection
again. Publication stays in the same transaction. The resumed repair runs to
completion, and an unknown commit result never triggers this retry.

On a disposable PostgreSQL database, the 250-load reconciliation fixture went
from 1,009 SQL calls to 15. The 253-observation existing-only refresh went from
1,021 to 16. Both database layouts also forced a dispatcher save during a
blocked repair publication and checked the posted result after the repair resumed.

Live cursor frames update the presence overlay independently of both editable grids.
The Gross Board computes visible row totals and carried balances once for each
data or filter change. Autosave credits time already spent rendering a typing
burst. Browser timing logs include pre-request, request-through-decoding and
response-to-paint durations when the client has all three timestamps. They use
the next ordinary request. The board read logs time the roster, weekly entries,
load views, stop cities, row ordering and Gross Board openings.

## References

- [PostgreSQL 16: EXPLAIN](https://www.postgresql.org/docs/16/using-explain.html)
  supports measuring actual plan nodes and buffer work before changing queries.
- [Expression indexes](https://www.postgresql.org/docs/16/indexes-expressional.html)
  avoid recomputing a JSON-derived lookup key during each search;
  [partial indexes](https://www.postgresql.org/docs/16/indexes-partial.html) require
  the query to imply the indexed predicate. The history lookup uses that exact predicate.
- [Google: optimizing INP](https://web.dev/articles/optimize-inp) recommends at most
  200 ms at the 75th percentile for interactions and reducing blocking main-thread work.
- [Google: optimizing TTFB](https://web.dev/articles/optimize-ttfb) gives 800 ms as
  a general TTFB guideline. That is distinct from this stricter 400 ms board-opening target.

The Status Board caches the resolved manual queue using the same committed
load and availability revision as its native plans. Each key also includes the
driver, queue order, entered text and local plan identity. A missing revision
uses the transaction's direct read. Cached stops retain the internal geocoding
evidence used to fill city labels. Each caller owns its copy of the stop and
schedule data. TestBoardPlanCacheKeepsStopEvidence and
TestBoardManualSourcesDatabase cover repeated reads and source changes.

Weekly vacation eligibility groups corrected availability history by driver
before applying the existing Monday and later-week rules. The live read-only
comparison covered ten weeks and found identical driver membership.
