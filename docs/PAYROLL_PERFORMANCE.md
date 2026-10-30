# Payroll latency work

## Historical measurements — October 28, 2026

The cutover workflow was subsequently retired by migration 161. These measurements
describe the original fixture; the summary/detail and cache improvements remain.
New benchmark fixtures use ordinary accounting without activating a cutoff.

Migration 160 installs inactive. Active accounting bounds historical ledger and
legacy-override reconstruction at the reviewed start week. Prior source collections
are grouped in SQL; only current/future receipts retain individual buckets. No
historical week is finalized for performance. Activation retains immutable archive
reports, including unfinished drafts, which subsequent archived reads serve directly.

Current pagination uses a compact calculation sharing the authoritative routing,
carry and frozen-report rules. It omits source notes/comments, retained load display
fields, transfer decoration and full-report JSON hashing. It still calculates every
participant's financial dependencies before pagination; it is not a visible-row-only
total. Detailed statements hydrate only the requested connected driver/team/owner set.
Summaries and details use separate caches with committed input revisions, including
cutover and external receipts, and concurrent-request deduplication. Migration 160
also enrolls tables introduced after the original revision registry; their absence
previously forced every payroll cache to use the global database snapshot.

Current reports use a whole-input revision token instead of hashing rendered report
JSON. Finalization still performs an uncached complete calculation under the financial
lock, and rejects a changed input token. Untracked tables conservatively fall back to
the database snapshot for cache invalidation and the original full-report hash for
finalization. Archived reports keep their retained revisions. Compact/full
totals, filters, metadata and revisions and selected/full statement JSON are tested.

The full synthetic fixture on a Windows workstation/PostgreSQL 17 produced these
repository p95 timings. There are 20 samples per ordinary action, 100 for concurrent
actions, and no errors. Concurrent summary reads include ten committed synthetic
load updates. The fixture cutover is October 5 to exercise its four-week history;
the product default remains October 26. These are **not production browser p95s**.

| Action | Driver Pay | Investor Pay |
| --- | ---: | ---: |
| Cold compact summary | 204.48 ms | 209.49 ms |
| Cached summary | 12.07 ms | 11.23 ms |
| Summary after financial change | 247.60 ms | 189.06 ms |
| Selected detailed calculation | 79.39 ms | 71.09 ms |
| Individual save | 75.37 ms | 80.69 ms |
| Five simultaneous saves | 520.80 ms | 444.92 ms |
| Five summary readers with source updates | 208.21 ms | 201.12 ms |

Raw p50/p95/p99/max and fixture scope are in
[the October 28 artifact](performance/payroll-2026-10-28.json). Cold calculations
still perform whole-fleet financial routing; this stage does not implement a separate
SQL-only totals engine. Further batching and narrower invalidation require measured
plans and parity coverage. No speculative financial indexes or weaker locks were used.

A read-only production baseline built from the deployed `3b07bbc` measured full
Driver Pay at 668.79/511.61 ms (57 queries) and Investor Pay at 384.99/150.73 ms
(39 queries). Two samples are a diagnostic baseline, not a p95 claim. No payroll
writes or stress tests ran against production.

The real browser check used all 176 synthetic drivers, five distinct signed-in
accounts and committed load updates, with 175 ms added to each API response.
Conservative automation-to-visible-result p95 was 1,575 ms for full cached page
navigation, 109 ms for expansion (mostly already-fetched details; first cold
expansion reached 1,011 ms), 316 ms for filtering, 990 ms for individual saves and
1,909 ms for concurrent saves. First page load was one 1,594 ms sample. Ordinary
cases have 20 samples and concurrent saves have 100. These measurements shared
the workstation with other local regression work, and full navigation includes
authentication/rendering. They are retained as limits, not a clean production
comparison. **The 300 ms browser p95 target remains unmet**, including filtering,
saves and cold expansion. Further performance work remains necessary.

To reproduce the repository run, set the disposable test DSN and
`MSERP_PAYROLL_PERF=1`, `MSERP_PAYROLL_PERF_CUTOVER=1`,
`MSERP_PAYROLL_PERF_SUMMARY=1`, `MSERP_PAYROLL_PERF_RUNS=20` before running
`go test ./internal/repository -run '^TestPayrollPerformance$' -count=1 -v`.
For full-size browser checks, create that fixture with
`MSERP_PAYROLL_KEEP_FIXTURE=1`, pass the printed schema to
`MSERP_REVIEW_PAYROLL_PERF_SCHEMA` when starting `frontend/scripts/review-local.mjs`,
and use `MSERP_PERF_WEEK_START=2026-10-05` with `measure-cutover-payroll.mjs`.
The launcher validates the synthetic fixture and drops it when stopped normally.

## Release gate

Routine Driver Pay and Investor Pay actions must finish below 300 ms at p95,
from the browser action to the visible result or committed save confirmation.
Measure an already-open app over an established HTTPS connection. The observed
network round trip is about 175 ms; the working API budget is 75 ms and the
scheduling/rendering budget is 50 ms. Bulk finalization/reopening and external
DataTruck refresh are separate measurements.

**The release gate is not yet satisfied.** Passing correctness tests or getting
a fast cache hit does not establish this latency target. Do not treat the API
benchmark below as a browser latency measurement.

On 2026-10-13, the user explicitly accepted the measured improvement and
authorized merging and deploying this stage despite the unmet 300 ms target.
That authorization supersedes the original release gate for this deployment;
the target and remaining optimization work below are still open.

## Implemented

- The full and selected calculations share the same routing and financial
  projection. Individual save validation reads a conservative connected set of
  drivers, teammates, trucks, and owners, including historical relationships.
  Source edges use the requested week and unfrozen legacy weeks without ledger
  records. Older saved balances remain authoritative; there is no arbitrary
  "last few weeks" cutoff on outstanding debt.
- Dated classification, assignments, eligibility, exact load-number matching,
  and toll attribution are calculated in sets. A duplicate load number anywhere
  in source history still prevents an implicit match.
- Expense payment totals and escrow capacity use grouped ledger queries.
  Escrow capacity matches the authoritative database guard across same-week
  releases, future collections and cancellations. Investor expense saves lock
  the requested rows together, check each version, and batch their audited writes.
- Dependency selection traverses the distinct relationship graph in Go to avoid
  PostgreSQL allocating a recursive hash for millions of overestimated nodes.
  This still includes every reachable historical relationship.
- Historical driver carry reads a dedicated cost projection and frozen reports
  directly. Historical investor overrides use scoped inputs without unrelated
  expense/installment enrichment. GET requests create no collection records.
- Financial locks, optimistic versions, individual audits and commit boundaries
  remain in place. Save validation does not use the report cache.
- Transactional cache revisions exclude explicitly reviewed task, telemetry,
  extension and service-operation tables. Financial commits invalidate reports;
  unknown tables retain conservative invalidation. Existing memory bounds remain.
- Paginated GETs accept optional comma-separated UUID `detailIds` (maximum 100).
  Only requested statements on the returned page retain details when `summary=1`.
  Remembered expanded rows therefore arrive in the initial request. Existing
  pagination totals, dispatcher options and whole-week revisions remain intact.
- Browser saves coalesce within an animation frame, submit the latest valid
  draft for each statement, and reconcile the committed receipt locally.
  Pending edits do not produce an "All changes saved" confirmation. Payroll
  search uses a 16 ms debounce instead of 250 ms.

## Diagnostics

HTTP performance logs include inclusive phase durations: source loading,
selection, truck/team routing, carry, financial-lock wait, save calculation,
source validation/writes and writes/commit. Nested durations overlap; do not add
them as disjoint wall-clock phases.

`backend/cmd/perf-audit` remains a bounded read-only diagnostic (1–5 samples).
It records counts, durations, response sizes, report hashes and at most 256 SQL
fingerprints. It does not print query arguments or report contents. Optional
`-explain` repeats the slowest read with a three-second statement timeout and
prints only plan operations, table/index names and numeric counters. Expressions
and literals are removed. Use the diagnostic built from the deployed SHA for
the deployed baseline, separately from the implementation build.

## Reproducing the isolated benchmark

From `backend/`, set `MSERP_DRIVER_PAY_TEST_DATABASE_URL` to an administrator
connection to a disposable local database ending in `_test`. Provision the
`mserp_app` role first. Never point this test at the application database.

```powershell
$env:MSERP_PAYROLL_PERF='1'
$env:MSERP_PAYROLL_API_BUDGET_MS='75'
go test ./internal/repository -run '^TestPayrollPerformance$' -count=1 -v
```

The fixture contains 176 drivers, 176 trucks/owners, 4,928 source loads and board
entries, 6,000 expenses, 6,000 tolls, 11,449 fuel transactions, 132 escrow ledgers
with prior payments/releases, and two legacy override weeks. It compares every selected statement to its full-week result
and the historical cost projection to the full source calculation. All reads
and saves run as `mserp_app` in a temporary schema.

There are 100 samples each for cold reports, cached reports, reports after a
financial update, uncached selected calculations and individual saves. Five
concurrent save workers run 100 samples each. Errors are counted and retained
in the sample population. Reports include p50, p95, p99 and maximum duration.
The optional API budget fails a subtest when its p95 exceeds that budget.

Development-only switches: `MSERP_PAYROLL_PERF_QUICK=1` uses three samples;
`MSERP_PAYROLL_PERF_SKIP_PARITY=1` skips parity work while inspecting plans;
`MSERP_PAYROLL_EXPLAIN=1` prints the slowest synthetic query's plan, optionally
filtered by `MSERP_PAYROLL_EXPLAIN_MATCH`. These are not release runs.
`MSERP_PAYROLL_KEEP_FIXTURE=1` creates and retains only the synthetic schema for
cross-build read comparisons; drop the printed schema explicitly afterward.

## Evidence and remaining work

Finalizing reviewed weeks helps historical carry because their frozen statements
can replace legacy source reconstruction. It does not eliminate the current
week's source loading/routing, and ordinary finalized-week reads still perform
source checks before the frozen overlay. Finalization must remain an accounting
decision; never finalize unreviewed payroll merely to improve this benchmark.

The implementation and baseline `8e751a4` produced identical full report JSON
hashes for Driver Pay and Investor Pay on the synthetic fixture. A bounded
read-only production comparison on 2026-10-13 also produced identical hashes.
After narrowing historical source edges, the sampled production Driver Pay
cold read decreased from about 1,955 ms to 287 ms, with a 9 ms cache hit. Investor
Pay decreased from 294 ms to 164 ms, with a 4 ms cache hit. These
are individual diagnostic samples, not p95 results or visible browser timings.

Correctness validation covers fresh/migrated schemas, source-charge carry,
teams, ownership, stale versions, finalized reports, cache commits/rollbacks,
and browser save/undo/navigation workflows. A full-size stress run on the actual
single-CPU VPS (458 MiB usable RAM, nominal 512 MB), using a separate synthetic
database, still exceeds the API budget. No production payroll writes were used.
During the earlier five-user save stress run, CPU utilization approached 100%
while sampled swap-in/swap-out remained zero. Allocated swap by itself is not
evidence that active swapping caused the payroll latency.

Remaining release work includes reducing cold report calculations further,
replacing historical source-ledger decoding with grouped balances and targeted
later-payment checks, further batching validation/writes, and testing the
complete browser action with 175 ms latency. The selected repository calculation
is available for validation/history; existing paginated HTTP expansion responses
retain their full-week metadata contract. Optimizing that cold HTTP path must
preserve those totals, dispatcher choices and finalization revision semantics.
The existing global financial lock serializes simultaneous saves and must not
be weakened to make a benchmark pass. Larger-history fixtures, five-user reads,
and controlled scheduled-sync interference remain part of the release matrix.

## Latest VPS stress results

Measured repository time in milliseconds, with 100 samples per action and 500
for each five-user save run. All financial writes used the isolated synthetic
database. The cold cases clear the application report cache; PostgreSQL buffers
are not forcibly evicted. Network and browser time are excluded.

| Action | p95 | p99 | Worst | Errors |
| --- | ---: | ---: | ---: | ---: |
| driver/cold | 398.52 | 436.62 | 593.78 | 0 |
| driver/warm | 31.74 | 34.34 | 34.45 | 0 |
| driver/changed | 401.88 | 465.16 | 474.68 | 0 |
| driver/expand | 104.42 | 122.55 | 157.05 | 0 |
| driver/save | 196.68 | 261.06 | 328.35 | 0 |
| driver/save5 | 683.46 | 892.38 | 1336.40 | 0 |
| investor/cold | 294.15 | 372.57 | 405.67 | 0 |
| investor/warm | 31.89 | 33.06 | 33.25 | 0 |
| investor/changed | 262.80 | 315.93 | 343.66 | 0 |
| investor/expand | 90.81 | 93.85 | 94.07 | 0 |
| investor/save | 124.66 | 148.92 | 214.32 | 0 |
| investor/save5 | 678.06 | 827.59 | 913.72 | 0 |

Selected `expand` measurements call the repository calculator directly; they
are not measurements of the existing HTTP expansion endpoint. The preserved
phase/query breakdown is in [the benchmark artifact](performance/payroll-2026-10-13.json).

## October 30 staged optimization

Stage 1 is production release 04a6098 (PR 68). Migration 179 registers the
retired palette tables, restoring committed-input caching and compact reads.
Unknown tables still select conservative fallback. Selected statement responses
preserve the committed revision instead of hashing their smaller response.
Production read-only diagnostic samples after deployment: Driver Pay cold
501 ms, warm 15/13 ms; Investor Pay 935/1094 ms with an intervening source
revision, then 24 ms warm. These are three samples per page, not percentiles or
browser measurements, and confirm why sync invalidation still needs work.

Stage 2 introduces dedicated detail endpoints, per-statement LRU entries,
compact committed mutation receipts, immediate single-statement reopen dialogs
and one background aggregate refresh. Legacy weekly endpoints remain compatible.
No financial rules or provider polling frequencies change.

The repaired browser harness is `frontend/scripts/measure-payroll.mjs`.
It measures cold first expansion (without prefetching its detail), cached and
multiple expansions, saves, single/bulk finalize and reopen, plus five distinct
users saving during source updates. Server-Timing provides query counts and
financial-lock wait; response bodies provide payload sizes. Visible commitment
and background aggregate completion are separate. Only local synthetic fixtures
are accepted. Default weeks are October 26, October 19 and September 28; use
`MSERP_PERF_WEEKS` to move the matrix forward with the fixture.

An initial one-sample-per-action smoke run on the 176-driver local fixture with
175 ms added API delay passed all financial actions. Driver/Investor first
expansion was 500/366 ms, save 923/815 ms, finalize 1536/664 ms and reopen
1439/1139 ms. Whole-week finalization was 4018/2807 ms and used 4044/1837
queries including background refresh. These are diagnostic samples, not p95
claims; the 300 ms target is **not met**. The complete dependency and bulk-action
optimization remains required. See the preserved
[smoke run](performance/payroll-stage2-2026-10-30-smoke.json).
