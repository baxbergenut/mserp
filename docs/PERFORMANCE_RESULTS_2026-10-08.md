# Performance implementation and production comparison

October 8, 2026. Baseline source: `493f362d16d55815da96d428e18435b580fc9088`.

## Implemented

- Bulk payroll expenses, escrow deductions/releases, truck ownership/routing, operators and investor edits. Charge/truck configuration and legacy cost records are reused within a repeatable-read calculation.
- Histories share a transaction and calculation inputs; finalized driver statements use their stored snapshots directly.
- Driver autosave reuses the validated routed deductions and only refreshes its changed carry state. Whole-week finalization calculates the updated fleet once before freezing rows atomically instead of recalculating the fleet inside each driver iteration.
- Bounded report cache keyed by PostgreSQL MVCC snapshot, kind/week and current charge week. Any committed input mutation changes the visibility token, including commits from transactions already running during a previous fill. There are no write-hook gaps. Cache entries are isolated JSON copies; writers bypass them. Cache limits: 32 reports / 8 MiB / 30 seconds.
- Migration 068 persists merchant-local fuel date, reporting timezone validity and normalized source truck evidence. Historical backfill materializes the timezone catalog once; a database trigger maintains corrections and older writers. Date/assignment/report/load indexes support reporting. MSERP connections disable JIT; shared PostgreSQL settings remain unchanged.
- Browser pending GET deduplication with independent cancellation and object copies. Payroll, histories and global search cancel obsolete requests. Mutations clear reuse and logout aborts reads. Collapsed payroll cards avoid constructing adjustment editors.
- Evaluated opt-in compact Status Board transport, preserving distinct historical/current snapshots. Production measurements found only 2.3% less raw JSON and 1.8% more gzip bytes; the frontend therefore retains the original transport by default. The optional contract and regression coverage remain available for later measurements.
- Immutable cache headers for content-hashed Next assets; HTML/route freshness and security headers remain enforced.
- Request Server-Timing and structured route-duration/query/pool/byte metrics; safe bounded read-only diagnostic CLI.
- DataTruck discovery, reconciliation and refresh update imported loads only. They never create/enrich fleet profiles or manage dispatcher/truck assignments. Relay retains its existing unmatched-identity review flow without creating drivers. Existing master records/assignments are not retroactively changed.
- Included the user's other local work: unified task board/history/statuses, assignments, live task event updates, onboarding/offboarding and Relay task handling. Regression fixtures were updated for those changes and the existing Appearance settings access.

## Validation

- Full backend tests against disposable PostgreSQL databases and `go vet`; CI repeats these on PostgreSQL 16/Linux (local PostgreSQL is 17/Windows). CI caught a legacy timezone alias difference; the backfill now applies exactly the original explicit alias mapping before timezone-catalog fallback.
- Frontend lint and static production build with `/api`; calculation/autosave, phone, cancellation, compact transport, task board, charge view and theme checks.
- Full Investor browser suite passed: access restrictions, authentication/revocation, ownership and histories, payroll finalization/reopening, Status Board queues/progression/undo/conflicts, onboarding/offboarding, and task events/retries. The Driver Charges browser suite passed, including carry, finalization and returning to the previous filtered payroll view.
- Database regression tests cover fleet records/assignments remaining unchanged across DataTruck import paths, snapshot-cache freshness after concurrent commits, isolated cache objects, preserved fuel reporting semantics, and bounded payroll query growth from 1 to 41 drivers.
- A pre-existing payroll Back-navigation filter reset exposed by the browser suite was corrected; financial drafts and save-before-navigation behavior are retained.

## Comparison method

Use `backend/cmd/perf-audit` from the baseline and optimized source against the same production database. All diagnostic connections use database read-only mode, the application role, one connection and bounded statement/run timeouts. Only sizes, hashes, counts and times leave the server. Runs are sequential observations, not concurrency tests or p95 estimates; repository times exclude HTTP/auth/network/browser rendering. Compare adjacent old/new reads after deployment as well as the pre-deployment capture because normal users and sync jobs can update data between captures.

The server remains the same 1-vCPU / approximately 458-MiB host. The user will discuss a resize with their manager later. No provider resize or global database restart is included.

## Remaining boundaries

This release retains full dependency groups for open historical statements: arbitrarily filtering to one driver before mixed-truck routing would change settlements. Summary/detail API splitting, fine-grained dependency generations, background jobs for manual sync and additional catalog/search endpoints remain growth work to justify from the new timings. There is no Redis service or shared proxy cache of financial data. No legacy financial balances were silently backfilled from today's recomputed values. The snapshot cache invalidates conservatively on unrelated database commits, which can reduce its hit rate but preserves financial freshness.

The later driver termination / escrow release project (migration 069 and its API/UI changes) is explicitly excluded by the user. The release is assembled in an isolated checkout; that active workspace remains untouched.

## Production results

Application release `6a9343c93a91069b111fde1068b3a7104b26c3a5` deployed successfully at approximately 18:44 UTC on October 8. [CI and deployment](https://github.com/baxbergenut/mserp/actions/runs/37826243016). A follow-up commit records these results and disables the default compact Status Board request after the measured gzip regression; it does not change payroll calculations.

The original source was compiled as a separate diagnostic and compared with the optimized source. “First” means an empty application report cache in a fresh diagnostic process, not a cold PostgreSQL or OS cache. Repeats run immediately in the same process. Times below are milliseconds, rounded.

| Read | Before deployment, two samples | After, first | After, repeat | DB calls before → first / repeat |
| --- | ---: | ---: | ---: | ---: |
| Driver Pay | 1,389–1,574 | 638 | 7 | 984 → 55 / 3 |
| Investor Pay | 458–543 | 216 | 3 | 342 → 27 / 3 |
| Driver history | 9,674–9,679 | 1,411 | 47 | 6,512 → 138 / 11 |
| Investor history | 4,212–4,617 | 1,398 | 8 | 2,528 → 182 / 14 |
| Fuel overview | 876–972 | 615 | 421 | 3 → 3 / 3 |
| Financial dashboard | 870–1,122 | 341 | 294 | 4 → 4 / 4 |
| Status Board calculation | 145–174 | 249 | 193 | 7 → 7 / 7 |
| Gross Board | 56–69 | 83 | 82 | 5 → 5 / 5 |

Relative to the mean of the two pre-deployment samples, uncached Driver Pay and Investor Pay were about 2.3 times faster; driver history about 6.9 times and investor history about 3.2 times faster. The latter two non-payroll boards showed no improvement; these small sequential samples do not establish a regression or a capacity guarantee. Fuel and financial queries benefit from less work per call, rather than fewer calls. Warm-cache payroll timings are best-case reuse and will increase whenever committed database changes invalidate the snapshot.

To distinguish changing production data from calculation changes, the old binary was also run immediately before the optimized binary on the migrated production database:

| Read | Adjacent old-code samples | Optimized samples | Exact serialized result |
| --- | ---: | ---: | --- |
| Driver Pay | 2,225–2,358 ms | 638 / 7 ms | Identical SHA-256 |
| Investor Pay | 1,072–1,140 ms | 216 / 3 ms | Identical SHA-256 |
| Driver history | 11,834–15,510 ms | 1,411 / 47 ms | Identical SHA-256 |
| Investor history | 6,569–6,628 ms | 1,398 / 8 ms | Identical SHA-256 |
| Fuel overview | 1,084–1,140 ms | 615 / 421 ms | Identical SHA-256 |
| Financial dashboard | 600–765 ms | 341 / 294 ms | Identical SHA-256 |
| Status Board | 202–227 ms | 249 / 193 ms | Identical SHA-256 |
| Gross Board | 57–91 ms | 83 / 82 ms | Identical SHA-256 |

All eight adjacent comparisons matched exactly, including complete financial payloads and histories. Live data changed between the earlier pre-deployment and later captures for some reports; those earlier hashes therefore are not used to assert equality. Server load also varied, as the old-code timings demonstrate. The adjacent baseline benefits from the newly installed indexes, so it is not a pure comparison of the old deployed schema.

Raw sanitized observations: [before](diagnostics/2026-10-08/pre.jsonl) and [adjacent old/new](diagnostics/2026-10-08/paired.jsonl). No financial rows, names, credentials or account identifiers are included.

### Actual API observations and rollout checks

During the first few minutes after deployment, structured logs captured 12 successful Driver Pay requests at 10–806 ms, with 5–57 database calls including authentication. The observed Investor Pay request took 196 ms and 29 calls. Maximum pool wait on those routes was 0.35 ms and 1.15 ms respectively. These are naturally occurring requests, not an authenticated synthetic browser benchmark. The observation window also included diagnostic CPU load. No application ERROR logs appeared in that window.

- API health/readiness, PostgreSQL, Nginx and the unrelated fuelbot service remained healthy.
- Schema migration history ends at 068; 066/067/068 applied successfully. Migration 069 was not deployed. Every fuel record has populated derived reporting fields.
- Driver Pay, Investor Pay and login HTML return HTTP 200 with `no-cache`. Hashed JavaScript returns HTTP 200 with a one-year immutable cache policy. Existing security headers remain present. Unauthenticated financial API requests still return HTTP 401 and expose timing metadata without report data.
- Post-deployment host observation: 458 MiB usable RAM, about 160 MiB available, about 205 MiB swap occupied, and root filesystem 74% used. These point-in-time values include deployment/cache effects; they are not evidence that additional RAM is unnecessary. A brief idle sample had no active swap-in/out. No provider resize was performed.
- Compact board transport measured 901,266 → 880,195 raw bytes but 161,312 → 164,190 gzip bytes. The default frontend request is restored to the original format; no transfer improvement is claimed.

### Next capacity decisions

Discuss the proposed 2-vCPU / 4-GB resize with the manager as requested. The application changes substantially reduce work, but one CPU and under half a gigabyte of usable RAM remain a shared-service constraint. Use the new request/query/pool metrics during normal busy periods to decide whether remaining latency is database calculation, queuing or network/browser work. A future concurrency test should run on a representative staging copy; this task intentionally did not stress production or mutate financial records for benchmarking.

If busy-period measurements justify further work, prioritize targeted historical calculation dependencies, a genuinely smaller Status Board summary/detail contract, lightweight global-search/catalog APIs, and durable manual-sync jobs. These are follow-up architecture options, not hidden unfinished migration steps in this release.
