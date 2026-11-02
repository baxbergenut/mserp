# Status Board operational save and opening performance

Validated locally on November 2, 2026. These changes are not deployed. Migration
188 and the matching deployment helper must ship together; install the helper
before applying the migration. It stops writers during the classification change
and prevents rollback to a release that lacks migration 188.

## Findings and changes

Production request logs contained Status Board saves of 7.86 and 11.41 seconds,
with roughly 690 database calls and six payroll calculations. Board edits now
commit their operational rows without calculating payroll. Dispatch-only history
does not invalidate payroll. Actual driver availability or truck condition changes
still capture durable payroll work in the same transaction for background
preparation. A guarded retry takes canonical source locks only when needed and
rechecks the original caller versions.

The browser acknowledges the committed save without awaiting the subsequent load
detail refresh. Stale refresh responses cannot replace newer edits. Save responses
include the committed canonical fleet statuses; the board shows truck restrictions
and clearing a load cannot hide an existing Home, Vacation, Shop or STOP state.

Initial reads repeatedly calculated fleet-wide truck status and rescanned VINs.
The board now derives the equivalent restriction state for its directly assigned
active drivers and computes VIN ambiguity counts once per query.

Route-changing edits postpone ETA work until five seconds after the latest edit.
Notes do not restart that pause. Switching from Dispatched to Enroute resets the
pause and changes the target. The existing minute worker cadence still applies;
five seconds is the minimum settling period, not a promise of an ETA at five seconds.

## Measurements

The existing read-only board diagnostic was run against production data with the
old and updated query code. No production schema or application release changed.

| Server board read | Duration |
| --- | --- |
| Before, one diagnostic sample | 2,584 ms |
| Updated, three diagnostic samples | 762 / 635 / 572 ms |

The isolated local PostgreSQL 17 test used 20 simultaneous writers, 1,000 source
loads and 20 gross plans. Each fresh/incremental schema run included 100 committed
notes/status saves and 20 simultaneous load selections. Timings include repository
transactions and connection contention, but exclude HTTP, browser rendering and
network latency.

| Schema / operation | p50 | p95 | Maximum |
| --- | --- | --- | --- |
| Fresh / notes and status | 57 ms | 309 ms | 332 ms |
| Incremental / notes and status | 58 ms | 304 ms | 329 ms |
| Fresh / load selection | 214 ms | 316 ms | 328 ms |
| Incremental / load selection | 219 ms | 330 ms | 344 ms |

These measurements demonstrate the removed bottlenecks, not an end-to-end
production latency guarantee. Fleet synchronization still serializes related
changes. Real availability/condition changes still require the payroll source
gate, though they no longer calculate payroll synchronously.

## Validation

- `TestStatusBoardOperationalDatabase`: fresh and incremental schemas, 20-writer
  saves, progress while an unrelated payroll gate is held, no synchronous payroll
  publication, durable canonical status capture, financial marker misuse rejected,
  stale version rejection, status/profile parity, route deadline behavior and
  missing-guard detection.
- Database regressions: Driver Board (including automation/provider deferral),
  Driver Teams, Fleet Status, Driver Pay, Payroll Projection, Payroll Vacations and Vacation
  Start Correction, with fresh/upgrade coverage where supplied by their fixtures.
- Browser fixtures: Status Board saves, loads, layout and dialog loading. The save
  fixture holds load-detail requests open, checks immediate saved state and another
  edit, verifies committed truck status, and preserves drafts after a conflict.
- `go test ./...`, `go vet ./...`, frontend lint, production build and the Gross
  Board, Driver Board, Driver Pay and phone calculation checks passed.

Live browser timing and the 20-user production workload remain deployment checks.
