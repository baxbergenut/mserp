# Gross Board reliability and performance — November 3, 2026

The change fixes native rows disappearing when an older live/read snapshot arrives
just after a save. Preserving acknowledged inserts also prevents the next edit from
mistakenly trying to create that row again. Untouched focused fields now receive
updates, including another page using the same account. Dirty fields and open
Daily loads editors keep their pending work.

Following the requested save behavior, the interactive API merges explicitly
changed fields under its existing financial write lock. Same-field edits use the
last submitted value, without stale-row-version errors. Current-row counters remain
only for ordering snapshots and compatibility; this adds no version history.
Source/split review, transfers and finalized-payroll checks still apply.

Typing is debounced for 300 ms. Requested navigation flushes the queue immediately.
Acknowledgements consume only submitted edits; newer typing is rebased as field
deltas. User/week/tab-scoped session storage recovers pending drafts after reload.
Identical retried patches do not rewrite rows or recalculate payroll. Gross Board
planning no longer checks driver/truck operational availability.

## Read-only production query comparison

Compared both implementations in one read-only repeatable database transaction,
using the current week, November 2–8. No production records were changed.

- Current-week entry query before: 925 ms with JIT, 1,134 ms in the initial
  JIT-disabled sample under concurrent production load.
- Current-week entry query after: 74 ms with JIT disabled.
- Opening balances before/after in the later comparison: 277 ms / 53 ms.
- Symmetric EXCEPT ALL comparisons: zero differing current-week entries and zero
  differing opening balances (both directions).

The new query ranks all required historical duplicates first, then expands trip
JSON only for the selected rows. Save responses restrict that ranking to the
selected driver. Reads/writes use transaction-local JIT-off/custom-plan settings.
These are individual query samples, not an end-to-end save latency guarantee.

## Verification

- Backend unit tests and vet; API field validation including note-only patches.
- Database tests: Gross Board calculations, field merges/clears, same-value retry,
  batch rollback, resting-driver planning and load-trip rules.
- Fresh and incremental payroll schemas: field saves publish both pay reports
  with exact reference parity; injected publication failure rolls back the plan.
- Browser fixtures: typing coalescing, stale insert snapshots, queued typing,
  same-account pages, reload during save failure, last-write field updates,
  independent-field preservation, decimal caret behavior, Daily loads, split
  review, notes and layout.
- Real API/PostgreSQL live E2E passed: two users plus an identical-cookie account,
  no follow-up board GET, week isolation, presence, read-only access, reconnect,
  queued writes and Daily loads field patches. Five edit-to-other-screen samples
  were 493/504/509/462/472 ms locally and 659/662/645/646/643 ms at 178 ms simulated
  network RTT, including the 300 ms debounce. A first run during a concurrent
  production build had a 1,610 ms outlier; the isolated rerun above passed.
