# MSERP performance audit and implementation plan

Assessment: October 8, 2026. Local HEAD and the live release both matched `493f362d16d55815da96d428e18435b580fc9088`.

The largest confirmed problems are repeated payroll calculations, hundreds of small deduction queries, expensive fuel reporting expressions, and very limited server capacity. The pay pages read imported loads from PostgreSQL; opening them does not fetch each load from DataTruck. Caching can improve navigation, but eliminating redundant work should come first.

This section records the original read-only assessment. Implementation was subsequently authorized, including source-only DataTruck imports, all local changes on main, and deployment. See PERFORMANCE_RESULTS_2026-10-08.md for the implemented changes and production comparison.

## Measurements and limits

Diagnostics ran over operator SSH on the production VPS. A temporary binary compiled from the matching source called existing repository read methods, using one database connection, `SET ROLE mserp_app`, `default_transaction_read_only=on`, and bounded timeouts. A pgx tracer counted calls and attributed elapsed time to source locations. Only timings, aggregate counts, response sizes, hashes, and sanitized query plans were output. No report contents or credentials were exported. Diagnostic binaries were removed afterward.

These are individual observations, not p95 benchmarks. Each diagnostic process used a fresh connection; normal API connections can reuse prepared statements. Query timing includes result consumption and some Go processing. Repository times exclude authentication, pool contention in the live API, HTTP serialization, network transfer, and browser rendering. The diagnostic process itself consumes some memory on this very small host. No concurrent load test or production financial write was performed.

Weekly reports used October 5, 2026. Lists used their first 25 records without filters. Dashboard probes used 2026 through October 8 where applicable. History probes selected existing records internally and requested up to ten weeks. Expense timing includes approximately 18 ms to select authorized category IDs for the diagnostic.

| Read path | Observed server calculation time | Database calls | Uncompressed response |
| --- | ---: | ---: | ---: |
| Driver Pay | 1.78–3.35 s | 1,005 | 146 KB |
| Investor Pay | 0.73–0.92 s | 351 | 10.6 KB |
| Driver pay history, seven available weeks | 11.29 s | 6,642 | 13.7 KB |
| Investor history, ten weeks | 4.50 s | 2,580 | 21.4 KB |
| Fuel overview | 1.10 s | 3 | 8.2 KB |
| Financial dashboard repository | 0.97 s | 4 | 3.1 KB |
| Fuel transactions | 275 ms | 9 | 20 KB |
| Status Board | 170–289 ms | 7 | 911 KB |
| Toll overview | 127 ms | 1 | 5.7 KB |
| Toll transactions | 90 ms | 9 | 17.8 KB |
| Expenses | 72 ms | 5 including diagnostic setup | 31.7 KB |
| Gross Board | 59 ms | 5 | 81 KB |
| Driver charges | 42 ms | 8 | 344 KB |
| Loads | 42 ms | 3 | 22 KB |
| Unpaginated driver lookup | 10 ms | 1 | 130 KB |

Database call counts include transaction control where used. History counts include one diagnostic record-selection query. The financial dashboard repository remains callable through the API even though the old dashboard page redirects to Gross Board.

Production has an approximately 85 MB database: 5,478 loads, 11,216 fuel transactions, 16,880 fuel items, 16,340 tolls, 7,071 expenses, 177 drivers, and 98 active drivers. Most of these are PostgreSQL row estimates. This volume should not require a distributed architecture or a separate cache service.

## Confirmed payroll bottlenecks

**1. Driver Pay rebuilds unrelated historical weeks on every read.**

`driver_pay_repository.go:130` reads the complete source week, routes investor statements, applies carry, and only then filters an individual driver. `driver_pay_carry.go:52` reads the entire collection ledger and finds legacy overrides without ledger rows. Production has two such overrides in two different weeks. Each causes another full source-week calculation and investor routing, even for an unrelated driver.

The current-week probe executed the source report three times, fuel truck allocation three times, and each charge-data query six times. In the 1.78-second sample, source queries consumed about 532 ms and truck fuel allocation 781 ms together: roughly 74% of total elapsed time.

Fix: scope carry by relevant driver and required historical range; resolve legacy dependencies without calculating unrelated fleet reports. Reuse source/configuration data within the same transaction. A separate, explicitly reviewed backfill may eventually pin eligible legacy bases, but simply inserting today's recalculated amounts could change historical semantics. Ordinary reads must remain pure.

**2. Per-driver and per-truck queries multiply work.**

`readDriverPaySourceWeek` calls escrow releases, expenses, and escrow deductions separately for each candidate driver. Investor Pay called each of these 97 times; Driver Pay called each 285 times across current and legacy weeks. `routeTruckPay` separately reads truck/owner names, ownership conflicts, operating drivers, assignments, expenses, and saved edits for each truck. It also rereads all charge data that the source calculation already loaded.

Fix: bulk-read these datasets for the relevant driver/truck IDs and group them in memory. Pass one calculation context through source calculation, routing, carry, and snapshot overlay, under the existing repeatable-read transaction. Preserve all cross-driver truck routing dependencies; moving a driver filter earlier without those dependencies would produce incorrect statements.

**3. Profile histories perform full-fleet work once per week.**

`driver_pay_history.go:50` calls `GetDriverWeek` in a loop, but that function filters only after building fleet-wide payroll and carry. The sample seven-week history ran 21 source-week calculations. `investor_pay_history.go:54` similarly calculates the complete investor report for each week, then selects the owner.

Fix: a targeted history calculation over the requested identities and week range, bulk-loading common inputs once. Fetch frozen finalized snapshots directly. Compute only open weeks and their actual dependency groups. Return summary rows first and fetch a single expanded statement on demand. Driver history currently defaults to 25 weeks in the UI, so the scaling risk exceeds the ten-week diagnostic request.

**4. Autosave and finalization amplify the same cost.**

`DriverPayRepository.Save` calculates current costs before saving, may recalculate payroll to validate expenses, then calculates costs again for the response. `saveDriverPayCosts` separately reads legacy cost records. Whole-week finalization calls `readDriverPayWeek` inside the per-driver loop while holding the payroll coordination lock (`payroll_settlements.go:212`). This can delay other accounting/fleet writes.

These write-path findings come from code inspection; production saves/finalization were not benchmarked. Fix them with transaction-local shared inputs and targeted recomputation after writes. Never reuse a pre-write calculation after a mutation that affects it. Preserve lock order, optimistic versions, exact arithmetic, principal reservation, audit events, atomic whole-week finalization, and frozen reports.

## Fuel queries and indexes

`fuelTimezoneExpression` (`fuel_repository.go:1104`) looks up `pg_timezone_names` inside a row-dependent expression. Fuel reporting filters transform `purchased_at` to a local date, so existing indexes on raw timestamps cannot directly satisfy the local-date predicate. Truck allocation also parses JSON prompt arrays before excluding most transactions.

A read-only `EXPLAIN ANALYZE` of `investor_pay_costs.go:24` showed:

- A sequential scan of all 11,216 fuel transactions, keeping 160 for the week.
- JSON prompt processing repeated 11,216 times.
- One timezone lookup subplan repeated 11,211 times, plus another 160 calls for the other date boundary.
- No shared-buffer disk reads in that execution: repeated CPU work remained expensive even with data cached.
- The fuel dashboard independently repeats its purchase aggregation for monthly, weekly, and state summaries. One dashboard plan also spilled a small amount of temporary data to disk.

Recommended change: persist validated reporting timezone, merchant-local purchase date, and normalized reported truck-unit evidence during import, with a compatible historical backfill and correction handling. Retain the upstream payload. Represent absent/ambiguous units explicitly and continue resolving only unique aliases. Do not infer a fleet driver or use current truck assignments to rewrite historical attribution. A first step can bound the raw timestamp scan conservatively before applying the exact existing local-date rule; any bound must accommodate all accepted source timezones.

Index candidates to validate against representative plans:

| Candidate | Intended use | Qualification |
| --- | --- | --- |
| Fuel reporting date, with environment/driver as appropriate | Weekly payroll and date filters | Add after the reporting-date contract/backfill; choose partial or composite forms from actual query plans |
| Normalized reported unit / resolved truck evidence | Fuel truck allocation and coverage | Preserve rename/alias ambiguity semantics and re-resolution rules |
| Truck assignment history beginning with `truck_id` and effective boundaries | Historical toll attribution | Existing history index begins with `driver_id`; validate dates and interval predicates before adding |
| Settlement indexes beginning with `week_start` | Bulk frozen-report reads | Existing primary keys begin with driver/truck ID; currently tiny tables, so lower priority |
| Loads pickup date plus ID; coverage identity/date indexes | Lists and transaction coverage as data grows | Rewrite expression-based coverage lookups before choosing exact indexes |
| Search-specific indexes | Global search at larger scale | Ordinary B-tree indexes do not solve arbitrary `%term%` searches across concatenated fields |

Already present: `loads.id` primary key, `loads_gross_board_number_idx` on `lower(btrim(load_id))`, `loads.truck_id`, fuel item transaction indexes, toll posting-date indexes, and expense driver/category/date indexes. Adding another load-ID index would not address the measured problem. Small-table sequential scans can be appropriate; index every candidate only after measuring its benefit and write/storage cost.

## PostgreSQL and server capacity

The VPS has one vCPU, 458 MB usable RAM, and 1 GB swap. Swap use was roughly 238–270 MB during the assessment. The API, PostgreSQL, Nginx, and unrelated `fuelbot` share it. Root storage is 8.7 GB, approximately 72% used. All four services were active; no OOM events were found in the previous seven days. Idle CPU was generally available, but during a single payroll read the CPU reached approximately 100% utilization and one sample showed 708 KB/s swap-out. There is insufficient headroom for simultaneous heavy report reads.

PostgreSQL uses 128 MB shared buffers, 4 MB work_mem, a 4 GB effective_cache_size estimate, and 100 maximum connections. The API pool is capped at ten. The cache-size estimate is larger than this machine's RAM; work_mem is per operation, so increasing it globally on this host would be risky. Increasing the API connection pool would not create CPU or RAM capacity.

JIT compilation is enabled. One truck-fuel plan spent 76.7 ms of its 252 ms execution compiling expressions. An adjacent diagnostic comparison with JIT disabled only for the diagnostic connection produced identical serialized report hashes: Driver Pay fell from 1.78 s to 1.42 s, Investor Pay from 0.73 s to 0.46 s. Later timings varied, including a 0.78 s JIT-off Investor Pay read, so these are directional results rather than a guaranteed percentage gain. Fuel overview remained about one second with JIT off.

Trial `jit=off` on MSERP connections after repeating representative benchmarks, rather than altering the shared PostgreSQL service globally. PostgreSQL documents that compilation overhead can outweigh execution savings on short queries: [PostgreSQL 16 JIT guidance](https://www.postgresql.org/docs/16/jit-decision.html).

Capacity recommendation: plan for at least 2 vCPU and 2 GB RAM, with 4 GB RAM the preferred starting point for this shared host; validate against actual concurrency after query changes. This is an engineering starting point, not a capacity guarantee or a priced purchasing recommendation. Resizing and any restart require a scheduled operation preserving fuelbot and the existing service/network boundaries. Query reductions remain necessary after an upgrade.

## Browser, transport, and the rest of the system

**Static assets are unnecessarily downloaded again.** Live hashed JavaScript responds with `Cache-Control: no-cache` and the epoch expiry used for HTML. ETags and conditional modification responses are disabled. This preserves correct HTML across deployments, but applying it to content-hashed chunks wastes transfers. Give only versioned `/_next/static/` assets a long immutable lifetime; retain current HTML/route-file freshness behavior and all security headers. Nginx supports these header controls: [headers module](https://nginx.org/en/docs/http/ngx_http_headers_module.html).

Public workstation probes observed about 0.72 s for a fresh HTTPS health/asset request, versus about 0.07 s from server-local HTTPS. One static page probe took 2.87 s. Those individual samples include network/TLS effects and are not authenticated page-load benchmarks. A browser waterfall is needed to separate connection setup, route chunks, auth, API wait, parsing, and rendering.

**The weekly page waits for a complete report.** `WeeklyPayPage.tsx:60` makes one report request and displays the result together. It does not issue per-load HTTP requests. `DriverCard` renders expanded tables conditionally, but computes totals and constructs adjustment elements even for collapsed cards; inline callback props also weaken memoization. These are secondary improvements: the current Driver Pay response compresses to about 21 KB, and Investor Pay to 2.5 KB, so payroll payload size is not the leading measured delay.

After fixing backend work, separate summary and detail contracts where useful. Ensure summaries and expanded statements share a revision, and preserve whole-week finalization semantics, including rows outside UI filters. A summary endpoint backed by the same expensive full calculation would improve payload only, not first-response time.

**Requests are ignored rather than canceled.** Pay pages, histories, and global search set cancellation booleans but do not abort active fetches when navigation/search changes. Expose AbortSignal through the API layer, cancel obsolete GETs, and deduplicate identical in-flight reads. Preserve the existing save-before-navigation behavior. Initial authentication gates the first page; subsequent navigation already runs session checks alongside page requests, so there is no reason to weaken authentication.

**Status Board has a larger transfer problem.** The measured response was about 911 KB raw / 163 KB gzip, including repeated plan/stop details across lists. It refreshes every 30 seconds while idle. Share plan details by identity, return compact row/current/next summaries, and load full queues/stops when needed. Preserve current-week suggestion candidates, older current selections, unfinished older plans, manual destination overrides, and midnight/version rules. A cheap revision check can avoid unchanged transfers; an ETag computed only after rebuilding everything saves bandwidth but not calculation time.

**Lookup and list work can be reused.** Loads recomputes all filter options on every page request; these took about 22 of its 42 ms. Fuel/tolls similarly recalculate options. Global search fans out to five full list endpoints, including counts/options that five-result suggestions do not need. Expenses eagerly fetches complete driver/truck/investor lists; driver profiles fetch all investors to find one linked owner. Use lightweight scoped lookups and suggestions; reuse catalogs within the authenticated session with explicit invalidation.

**Polling and integrations share finite capacity.** Today's Nginx log contained 3,008 successful driver-directory requests, consistent with repeated 15-second polling. Existing visibility/idle guards are valuable; add cheap change revisions, overlap prevention, and jitter where missing. Background integration calls are already separated from ordinary page opening. Preserve DataTruck's shared rate gate and discovery priority. Consider bounded background DB concurrency only after pool-wait measurements, and reuse resolved identities within import batches. Manual syncs are currently long synchronous HTTP requests; a durable job/status API would improve progress, cancellation semantics, and navigation later.

## Integration driver creation

The requested no-auto-create policy is already present in the matching production source:

- DataTruck `resolveDriver` (`load_repository.go:506`) returns only a pre-existing match or no match. It does not insert drivers. Unknown source names remain on imported loads. Truck import also resolves existing fleet records.
- Relay `ensureRelayDriver` (`fuel_repository.go:310`) inserts/updates `relay_driver_links` with nullable `driver_id`, not the drivers table. User confirmation links identities and historical purchases.
- DataTruck's new-load import still enriches a matched driver's name, fills a missing dispatcher, can create dispatchers, and can assign existing drivers/trucks. Operational refresh follows its separate source-only path. These are distinct from creating drivers.

Keep the existing no-create behavior and strengthen database integration tests: unknown names, reordered/ambiguous names, new Relay IDs for known people, inactive records, retries, and unmatched historical transactions. If stricter integration ownership is desired, propose a separate change that makes DataTruck fleet enrichment/assignment review-driven; do not silently bundle that policy change into performance work. This audit does not establish whether old versions created any particular historical driver. Do not bulk-delete or merge existing drivers based on names.

## Caching strategy

Start with request-local reuse and in-flight deduplication. They remove repeated computation without stale financial data. Next use small bounded caches for catalogs/filter options and appropriately authorized read models. An external Redis service is unnecessary at the current scale.

For payroll, introduce durable dependency revisions before cross-request caching. A conservative first design uses a payroll-input generation and calculation-version key; refine to week/identity dependency groups when needed. All relevant writes and imports must advance it in the same transaction. A cache fill must read data and revision consistently and must not publish an older result under a newer generation. Include week, report scope, view options, and authorization scope where applicable. Authentication/permission checks continue on every request.

Invalidate after Gross Board edits, source-load/fuel/toll corrections, identity linking, assignments, tariffs, ownership/terms, charges, expenses/payments, escrow/release changes, payroll edits, finalize, and reopen. Earlier corrections can affect later carry; invalidating only the edited week is insufficient. Start broad to preserve correctness. Bound memory and TTL as additional protections; TTL alone is not a financial correctness mechanism.

Finalized snapshots can be cached by settlement identity and version, with reopen/version invalidation. Live reports may reuse validated in-memory data on navigation. If stale-while-revalidate is introduced, show its freshness and require validation before financial edits/finalization; never overwrite unsaved drafts with a late response. Clear per-user state on logout and do not persist fetched payroll or edits in viewMemory/sessionStorage. Do not put authenticated financial JSON in a shared Nginx cache.

## Proposed implementation sequence

| Phase | Concrete work | Completion evidence |
| --- | --- | --- |
| 1. Baseline and low-risk delivery | Add route timings, Server-Timing, pool wait/query counters, response sizes; browser waterfall; hashed-asset cache policy; cancel/deduplicate GETs; benchmark an MSERP-only JIT setting | Before/after captures on cold and warm navigation; fresh HTML across deployment; canceled work stops; unchanged payroll values |
| 2. Payroll calculation restructuring | One transaction-local calculation context; bulk deductions/releases/escrows; bulk truck routing inputs; scoped carry/legacy resolution; share charge data | Same reports/revisions and money totals; bounded query count instead of growth by driver/truck; repeatable-read and concurrency tests |
| 3. Fuel reporting data and indexes | Add canonical reporting fields, compatible backfill, import/refresh maintenance, indexed weekly reads, shared dashboard aggregation | Date/alias/DEF/unlinked parity; EXPLAIN shows date-bounded reads and no per-transaction timezone catalog lookup; backfill restartability |
| 4. Histories and write paths | Target identity/week dependencies; frozen-snapshot fast path; lazy expanded history; targeted post-save calculation; eliminate fleet recalculation inside finalization loop | History speed scales with requested records; no read writes; atomic finalize/reopen, audit and cross-week payment tests pass |
| 5. Revision-based reuse and remaining UX | Bounded report cache if still justified; selective catalog caches; compact Status Board responses; lightweight suggestions/lookups; immutable source-detail projections if profiling warrants them | Multi-user invalidation and permission tests; draft safety; reduced transferred bytes and unchanged refresh behavior |
| Capacity work alongside phases 1–3 | Schedule VPS resize; tune PG/pool to actual memory/concurrency; disk/backup capacity alerts | No sustained swap under representative concurrency; background sync and unrelated service remain healthy |

Ship these as separate reviewable changes. Phase 3 can proceed independently of much of phase 2. Cache implementation depends on demonstrated correctness of phases 2–4 and measured remaining need. The no-driver-creation regression tests can ship independently; further master-data policy changes need their own scope.

Proposed performance budgets, to validate rather than promise: warm list/board APIs p95 below 300 ms; weekly payroll APIs below 500 ms; a ten-week targeted history below one second; visible weekly summaries within 1–1.5 seconds on a representative user connection. Aim for fewer than 50 database calls for an ordinary weekly report, with a documented bounded cost for required legacy dependencies. Measure normal and concurrent usage on a staging fixture sized to production and several times its volume; do not stress-test the live VPS.

Testing must preserve cent rounding, UTC load dates, merchant-local fuel dates/DST, posting-date tolls with crossing-date assignments, owner-only/mixed truck weeks, inactive/history visibility, duplicate/unmatched loads, source corrections, legacy carry, future payment reservations, finalized snapshots, reopen semantics, stale writes and permission boundaries. Run the repository's backend tests/vet, relevant disposable-database suites as `mserp_app`, frontend lint/build/calculation tests, and payroll/investor browser suites for implementation changes. Add query-count/performance regression checks that assert bounded work, not fragile millisecond thresholds.

Persistent production observability is currently insufficient for historical p95 attribution: `pg_stat_statements` is not enabled, slow-query logging is off, and the inspected request logging has no useful route-duration instrumentation. Prefer application tracing first. Enabling `pg_stat_statements` requires shared preload configuration and a PostgreSQL restart, so schedule it with awareness of every database consumer: [PostgreSQL 16 statement statistics](https://www.postgresql.org/docs/16/pgstatstatements.html). Avoid logging query arguments, cookies, or financial payloads.

After each deployment, verify the release SHA, health/authentication, relevant live read timings, source freshness, cache invalidation, and unchanged financial outputs. Do not treat successful CI alone as proof of better production latency. Remaining verification gaps are authenticated browser rendering/waterfalls, peak-concurrency behavior, sustained host metrics, and mutation/lock timings in a disposable environment.
