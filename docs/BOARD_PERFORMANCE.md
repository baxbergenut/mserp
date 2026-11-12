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

Read-only repeatable-snapshot `EXCEPT ALL` comparisons against production returned
zero differences in both directions for current Gross Board entries, opening
balances and Status Board gross entries. Repository tests cover fresh and migrated
schemas, missing/ambiguous sources, reviewed splits, historical duplicates, stop
progress, and existing financial guards.

## Reproduce and verify

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
