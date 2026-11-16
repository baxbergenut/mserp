# Reports

Open `/reports` for Company Truck P&L, Owner-Operator Truck P&L, Dispatcher
Performance, and Driver Performance. Each report provides the selected and
comparison periods, historical truck/driver/dispatcher filters, source records,
and a CSV with both periods. `reports.read` grants the financial views and their
exports. `reports.performance` grants the performance views and their exports.
A source link to another Clustie page also requires that page's permission.
Report requests use read-only snapshots and never initiate provider syncs or
prepare, edit, or finalize payroll. Apply migration 222 before enabling this release; it adds read indexes and an immutable statement-load-ID extractor, with no financial data or payroll protocol changes.

- Dates are inclusive in New York. The default is the last completed
  Monday–Sunday week with the preceding week as its comparison. Both ranges
  can be changed independently, up to 366 days per range.
- Confirmed gross and miles come from Loads by actual delivery date. Completed
  statuses use the Status Board convention (delivered, completed, invoiced).
  An exact upstream ID takes precedence over a uniquely matching normalized
  load number. Gross Board supplies dated assignments and provisional manual
  evidence. For splits, provider trip gross and miles are used when they sum to the Loads record. Repeat links do not multiply source amounts.
  Clustie owns assignments: original service-day native selections and dated fleet history supply driver/truck/dispatcher identity. Provider driver and truck labels never fill an unassigned native record.
  A completed load without actual delivery is visible for review when another
  source date places it in the period. Conflicting assignment evidence keeps
  attributable totals in Unassigned.
- Dispatcher credit follows the original service day. An approved Gross Board
  transfer retains its recorded original dispatcher and truck. Revenue stays on
  the actual delivery date, including when a transfer crosses a week boundary. An active truck-week is one truck with qualifying work for the
  dispatcher in a New York calendar week. Loaded RPM uses gross and positive
  loaded miles from the same records. Deadhead uses matched loaded/empty miles.
- Driver active days are distinct days from recorded pickup through delivery,
  clipped to the selected period for its confirmed load cohort. Missing pickup
  or reversed timestamps leave active days unavailable. On-time rates use matching actual and
  scheduled timestamps, with coverage across eligible completed load records.
  Reports do not estimate MPG from fuel purchases.
- Company P&L uses attributable earned driver pay, production Relay fuel,
  production/legacy tolls, CAT Scale tickets, maintenance expenses, and direct
  equipment expenses. Fuel uses purchase date; tolls use posting date and
  resolve ownership on the exit date. Native maintenance expense records are
  counted once, including expenses reimbursed through payroll. Equipment uses
  expense types Equipment, Equipment Lease/Rental, Truck Lease/Rental
  and Trailer Lease/Rental. Truck/trailer payments, loan principal and shared
  overhead are outside contribution; mixed loan payments are not estimated.
  Unresolved source identity or cost responsibility is visible in Unassigned.
  Where pay or known truck costs need review, dependent metrics stay blank.
- Driver earnings use finalized Driver Pay matched by exact load/trip identity,
  including cross-week statement placement. Owner-operator carrier fees are
  the frozen settlement driver-gross basis less matched operator load earnings.
  The difference between confirmed Loads gross and that basis is shown separately
  as Dispatch adjustment balance and does not become carrier income. Carrier contribution
  subtracts carrier-borne operating costs. Weekly settlement rows use finalized
  Driver Pay for weeks starting in the selected period. They show the whole
  statement, with reimbursements, deductions, and escrow movements. Weekly
  amounts with several dated truck assignments are shown in Unassigned.

## Read performance

- The initial response contains summary rows and filter choices for one period.
  Comparison is opt-in; row source records are fetched on expansion. CSV includes
  both periods and their evidence, with the same server-side view permission.
- Loads are narrowed by the indexed delivery period before resolving board links.
  Ownership is resolved in a batch of distinct truck/service days. The frozen
  load-ID GIN index finds cross-week statements without a load-by-statement JSON join.
- Read-only repeatable snapshots validate source revision counters, financial
  resource epochs, the payroll boundary and missing-load evidence before cache reuse.
  The cache is bounded to 32 periods / 16 MiB / five minutes, stores independent
  serialized values and coalesces identical in-flight reads. Changed sources never
  serve a stale cached period. There is no browser cache of financial data.
  The digest shares payroll's audited operational exclusions, so board presence,
  provider delivery bookkeeping, telemetry and derived preparation do not evict
  unchanged reports. Maintenance classification stays explicitly included.
  Drilldowns and exports carry the displayed source revision and reject changes
  with a refresh instruction. Returning to the page refreshes its snapshot.
- Tables render at most 50 groups at once; source rows are lazy. Measurements must
  include loaded data and two animation frames, not just the page heading.

## Closed-week reconciliation

The disposable source-record fixture covers October 12–18, 2026. Preview dates
can shift the same records to another completed Monday–Sunday week. These are
synthetic records, not production accounting sign-off.

| Source calculation | Company truck | Owner-operator truck |
| --- | ---: | ---: |
| Confirmed Loads revenue | $1,100 | $1,800 |
| Confirmed miles | 400 | 600 |
| Frozen load earnings | $300 | $1,360 |
| Fuel / tolls / scales | $200 / $40 / $14 | — |
| Company maintenance / equipment | $80 / $60 | $0 / $50 |
| Carrier fee (settlement gross $1,700 less $1,360) | — | $340 |
| Dispatch adjustment balance (separate) | — | $100 |
| Contribution | $406 | $290 |

Company RPM is $2.75 and cost/mile is $1.735 before display rounding. The $200
manual board entry stays provisional, and a duplicate exact load link adds no
revenue or miles. A $900 truck payment is excluded; a separate $30 maintenance
record without a truck remains Unassigned. Operator earnings $1,360 + $25
reimbursements − $50 deductions − $20 escrow withheld + $5 escrow returned =
$1,320 net payable. A $10 escrow application is disclosed separately.

Checks: TestReportsClosedWeekDatabase (fresh/incremental), TestReportsCutoff,
TestReportsFleetScaleDatabase, TestReportsPermissionsAndCSV and
frontend/scripts/test-reports-e2e.mjs. The database tests also verify source-edit
cache invalidation, provider-missing transitions, missing finalized pay, native
assignment authority, unavailable timing, permissions and CSV formula escaping.

For the local production-build preview, use review-local.mjs with
MSERP_REVIEW_REPORTS_WEEK=2026-11-09 and optionally MSERP_REVIEW_REPORTS_SCALE=1.
The scale fixture adds 120 company trucks, 960 current loads and 12,000 historical
loads/board links. Set MSERP_REVIEW_ACCESS_FILE to the runner's access JSON before
running test-reports-e2e.mjs; REPORTS_COLD_DOCUMENT=1 measures a new document load
instead of first sidebar navigation. Results and screenshots go to the local
temporary clustie-reports-review directory. This does not measure production
network/database contention.

Local Chromium production-build measurements on November 16, 2026, with the
scale fixture: first uncached sidebar navigation 260 ms; fresh document including
session verification 344 ms; first uncached other views 78–105 ms; repeat view
switches p95 60 ms (12 samples). Timings end after data renders and two animation
frames. The user accepted the 344 ms fresh-document result. These measurements
do not establish a production latency guarantee; reconcile a real closed company
and owner-operator week and measure the deployed environment before rollout.

Production read-only verification on November 16 reconciled November 2–8 for
company truck 049 (Loads $5,963.32 / 2,079.01 miles, earned pay $1,455.31,
carrier costs $3,714.18, contribution $793.83) and owner-operator truck 001
(Loads $9,881.00 / 3,176.49 miles, carrier fees/contribution $1,156.92, separate
dispatch balance $240.00 and settlement net $6,616.77). Independent raw Loads,
cost and frozen-pay reads matched, with 12 company and 8 operator checks.

Initial live profiling found collaboration and integration bookkeeping invalidated
the broad digest continuously. The operational exclusion fix and bounded SQL join
planning reduced same-period repeated repository reads to 13–53 ms in a read-only
production check. Uncached default-period reads were 342–827 ms; the earlier
operator week took 1,178 ms. These are server repository timings, not browser
measurements, and do not prove the local 344 ms opening time in production.

## Next reports

Revenue and settlement exceptions; fleet utilization and downtime; operating
cost trends; broker and lane profitability.
