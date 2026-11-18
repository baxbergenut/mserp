# Reports

Open `/reports` for Company Truck P&L, Owner-Operator Truck P&L, Investor Truck P&L, Dispatcher
Performance, and Driver Performance. Each report provides the selected
week, historical truck/driver/dispatcher filters, source records,
and a CSV for the selected week. `reports.read` grants the financial views and their
exports. `reports.performance` grants the performance views and their exports.
A source link to another Clustie page also requires that page's permission.
Report requests use read-only snapshots and never initiate provider syncs or
prepare, edit, or finalize payroll. Apply migration 222 before enabling this release; it adds read indexes and an immutable statement-load-ID extractor, with no financial data or payroll protocol changes.

- The shared WeekSelector exposes completed New York Monday–Sunday weeks from
  October 19, 2026 through the last completed week. That latest week is the default.
  Summary, sources and CSV use the same server-validated week; custom date and
  comparison controls are removed.
- Only active Gross Board entries and explicitly selected trips contribute load
  evidence. Gross and miles use Gross Board's displayed values: manual overrides,
  linked source defaults and reviewed split allocations follow the shared board
  projection. Manual unmatched entries contribute their entered amounts too.
  Linked completed loads retain actual-delivery-date recognition; manual entries
  use their board date. Repeat links count once. Unknown amounts stay blank.
  Clustie owns original service-day assignments; provider labels cannot fill them.
  The UI shows data without Review badges, a review counter or provisional columns.
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
  Only known company-paid costs attributed to a truck are included. Driver, owner
  and broker charges and unassigned costs are excluded from costs and drilldowns.
  Company reimbursements remain company costs. Equipment and carrier-cost columns
  are hidden; attributable company costs still participate in Profit.
  A genuinely missing tariff or required pay basis leaves pay-dependent metrics blank.
- Driver earnings use the same exact decimal calculation as Driver Pay: current
  configured CPM times Gross Board miles, or current percentage times Gross Board
  driver gross. Dated team membership selects the main driver's tariff; manual
  Gross Board entries also qualify. This bounded source calculation does not run
  payroll preparation or write statements. Finalized Driver Pay fees take precedence
  when matched by exact load/trip identity (including cross-week placement), or
  native source driver/date/slot for manual entries. Reopening restores calculated
  pay; source and tariff edits invalidate the report cache. Owner-operator carrier fees are
  the driver-gross basis less matched operator load earnings (frozen values when finalized).
  Dispatch adjustments are excluded from carrier income. Owner-operator Profit is carrier fees plus RC less attributable company-paid costs.
  RC (recurring charges) estimates the selected week's scheduled negative truck
  charges using the same effective phases, per-truck amounts, eligibility rules,
  vacation/termination exclusions and dated owner routing as Driver Pay. It does
  not accrue intervening weeks or estimate balance collections. Finalized statements
  replace the schedule with actual deductions, including partial/zero source choices.
  Reopening restores the schedule estimate. Truck sources retain dated truck attribution. Legacy
  recurring occurrences require a single dated owner-operated truck. Zero/waived
  charges, positive credits, installments and escrow add no RC. Whole statements are available in Accounting.
- Investor Truck P&L shows carrier profit. Carrier fees use driver gross less the gross investor share under the applicable weekly agreement. Matched frozen Investor Pay load earnings and actual recurring deductions take precedence. Missing agreements leave dependent metrics blank. Profit adds RC and subtracts attributable company costs. Summary, sources and CSV require both Reports and Investors View.
- Trucks with zero report loads contribute zero miles. Missing mileage on a qualifying load still makes that total unavailable.

## Read performance

- The initial response contains summary rows and filter choices for one period.
  Row source records are fetched on expansion. CSV includes the selected week
  and its evidence, with the same server-side view permission.
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
  Row expansion sends the displayed revision. If it changed, the sources response
  also supplies refreshed summary rows from the same snapshot. The UI updates totals
  and opens the selected row in one request. CSV checks the displayed revision.
  Window focus does not refetch or collapse rows. Explicit Refresh and changes
  to week/view/filter load a snapshot; the calendar hook only changes it on week rollover.
- Tables render at most 50 groups at once; source rows are lazy. Measurements must
  include loaded data and two animation frames, not just the page heading.
- Every table header sorts locally before pagination, with blanks last and a
  300ms row movement matching Dashboard (disabled for reduced motion). Profit is
  last, RPM and CPM are adjacent, and Miles follows Load gross. Truck rows show
  all drivers assigned on the selected week's native New York service days,
  including dated team members and midweek changes, not today's crew. Units and
  driver names link to permission-checked profiles; the rest of the row expands
  sources. Weekly crew labels do not change revenue or cost attribution. Driver
  names use the profile's first name (legacy full-name records fall back to the
  first word). Numeric cells and headers are centered. Driver earnings are labelled
  Driver fee; Source records and Reported groups summary cards are removed.
- Per-metric `estimated` flags identify calculated driver fees, carrier fees, RC,
  and their dependent Profit/Margin/CPM. Only those values render in the Status Board stale-ETA yellow, including
  the aggregate Profit and source evidence. CSV notes list estimated metrics;
  finalized amounts retain their ordinary styling. These flags do not alter totals.

## Closed-week reconciliation

The disposable source-record fixture covers October 12–18, 2026. Preview dates
can shift the same records to another completed Monday–Sunday week. These are
synthetic records, not production accounting sign-off.

| Source calculation | Company truck | Owner-operator truck |
| --- | ---: | ---: |
| Gross Board revenue | $1,199 | $1,999 |
| Gross Board miles | 400 | 500 |
| Driver earnings | $300 frozen + $75 manual entry tariff | $1,360 frozen |
| Fuel / tolls / scales | $200 / $40 / $14 | — |
| Company maintenance / equipment | $80 / $60 | $0 / $50 |
| Carrier fee (settlement gross $1,700 less $1,360) | — | $340 |
| Profit | $430 | $290 |

Gross Board's $999 / 300-mile override and $200 / 100-mile manual entry give
company revenue $1,199 / 400 miles. Removing the unmatched manual entry yields
company profit $305 with frozen pay $300 and costs $394. The duplicate link adds
no revenue or miles. Driver-charged repairs and the unassigned $30 maintenance
record are excluded. A $900 truck payment remains excluded. The owner-operator
base fixture has RC $0.

The recurring-charge fixture deducts $125 insurance (base $200 / due $450) and
$25 legacy recurring fees. A waived $75 fee, $50 credit, $40 installment and
escrow movements do not add RC. RC is $150 and Profit is $440.

Checks: TestReportsClosedWeekDatabase, TestReportsTariffProfitDatabase,
TestReportsEstimatedRecurringDatabase, TestReportsRecurringChargesDatabase and TestReportsGrossBoardCohortDatabase
(fresh/incremental), TestReportsCutoff,
TestReportsFleetScaleDatabase, TestReportsPermissionsAndCSV and
frontend/scripts/test-reports-e2e.mjs. Run test-reports-estimates-e2e.mjs afterward
against the same disposable review schema to verify scheduled/finalized/waived/
reopened RC, CSV estimate status and exact stale-ETA color parity in both themes.
The database tests also verify source-edit
cache invalidation, provider-missing transitions, tariff/finalized/reopened pay, native
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
