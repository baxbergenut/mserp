# Performance implementation and production comparison

October 8, 2026. Baseline source: `493f362d16d55815da96d428e18435b580fc9088`.

## Implemented

- Bulk payroll expenses, escrow deductions/releases, truck ownership/routing, operators and investor edits. Charge/truck configuration and legacy cost records are reused within a repeatable-read calculation.
- Histories share a transaction and calculation inputs; finalized driver statements use their stored snapshots directly.
- Driver autosave reuses the validated routed deductions and only refreshes its changed carry state. Whole-week finalization calculates the updated fleet once before freezing rows atomically instead of recalculating the fleet inside each driver iteration.
- Bounded report cache keyed by PostgreSQL MVCC snapshot, kind/week and current charge week. Any committed input mutation changes the visibility token, including commits from transactions already running during a previous fill. There are no write-hook gaps. Cache entries are isolated JSON copies; writers bypass them. Cache limits: 32 reports / 8 MiB / 30 seconds.
- Migration 068 persists merchant-local fuel date, reporting timezone validity and normalized source truck evidence. Historical backfill materializes the timezone catalog once; a database trigger maintains corrections and older writers. Date/assignment/report/load indexes support reporting. MSERP connections disable JIT; shared PostgreSQL settings remain unchanged.
- Browser pending GET deduplication with independent cancellation and object copies. Payroll, histories and global search cancel obsolete requests. Mutations clear reuse and logout aborts reads. Collapsed payroll cards avoid constructing adjustment editors.
- Opt-in compact Status Board transport shares identical full plan values. Historical/current snapshots with the same plan ID stay distinct. The client restores the existing domain contract, preserving all queues and stops.
- Immutable cache headers for content-hashed Next assets; HTML/route freshness and security headers remain enforced.
- Request Server-Timing and structured route-duration/query/pool/byte metrics; safe bounded read-only diagnostic CLI.
- DataTruck discovery, reconciliation and refresh update imported loads only. They never create/enrich fleet profiles or manage dispatcher/truck assignments. Relay retains its existing unmatched-identity review flow without creating drivers. Existing master records/assignments are not retroactively changed.
- Included the user's other local work: unified task board/history/statuses, assignments, live task event updates, onboarding/offboarding and Relay task handling. Regression fixtures were updated for those changes and the existing Appearance settings access.

## Validation

Full backend tests with all configured disposable PostgreSQL suites and `go vet`; frontend lint, static production build, calculation tests, request cancellation/compact transport tests, task board view tests. Browser suites and production checks are recorded below when complete.

## Comparison method

Use `backend/cmd/perf-audit` from the baseline and optimized source against the same production database. All diagnostic connections use database read-only mode, the application role, one connection and bounded statement/run timeouts. Only sizes, hashes, counts and times leave the server. Runs are sequential observations, not concurrency tests or p95 estimates; repository times exclude HTTP/auth/network/browser rendering. Compare adjacent old/new reads after deployment as well as the pre-deployment capture because normal users and sync jobs can update data between captures.

The server remains the same 1-vCPU / approximately 458-MiB host. The user will discuss a resize with their manager later. No provider resize or global database restart is included.

## Remaining boundaries

This release retains full dependency groups for open historical statements: arbitrarily filtering to one driver before mixed-truck routing would change settlements. Summary/detail API splitting, fine-grained dependency generations, background jobs for manual sync and additional catalog/search endpoints remain growth work to justify from the new timings. There is no Redis service or shared proxy cache of financial data. No legacy financial balances were silently backfilled from today's recomputed values. The snapshot cache invalidates conservatively on unrelated database commits, which can reduce its hit rate but preserves financial freshness.

The later driver termination / escrow release project (migration 069 and its API/UI changes) is explicitly excluded by the user. The release is assembled in an isolated checkout; that active workspace remains untouched.

Production results: pending deployment and the paired comparison.
