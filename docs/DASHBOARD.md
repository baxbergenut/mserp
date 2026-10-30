# Company dashboard

`/dashboard` is available only to Administrators, as their default signed-in
landing page and first sidebar item. Other users retain their first permitted
page as the landing page and cannot open the dashboard through a direct link.
It reads local records and cached integration data; opening or refreshing it never
starts provider synchronization or writes fleet, accounting, or board records.
No migration or additional environment variable is required.

## Reporting rules

- The UI separates **Weekly** financial results from **Right now** fleet and
  attention cards. Week navigation uses `preset=week`, a full Monday-Sunday
  reporting window (including recorded plans), compared with the preceding week.
  An optional `dateFrom` is normalized to Monday; an omitted date uses New York's
  current week. The older date-range presets remain API-compatible.
- The API resolves business dates in `America/New_York`. This week/month runs
  through today and compares with the same elapsed portion of the previous
  week/month (clamped to the previous month's length). Last week/month uses full
  periods. Custom dates are inclusive, at most 366 days, and compare with the
  immediately preceding equal-length period. Trends use daily buckets up to 90
  days and calendar weeks beyond that; boundary weeks can be partial.
- Original Gross is the headline. Driver Gross is not displayed on the dashboard
  or its reconciliation table; the existing API fields and conflict rules remain
  compatible. Gross Board service dates determine the reporting period. Planned
  rows are included and gross is not represented as collected revenue.
- Exact load identity and effective original/mileage overrides follow Gross
  Board. A linked load with a missing source value stays missing unless an
  explicit entered override exists. Deleted rows, day statuses and blank load
  rows are excluded.
- Resolve identity across board history through the selected end date before
  filtering the period. Count the earliest identical linked-load occurrence
  once, across drivers/slots/dates. Different drivers or differing gross,
  Driver Gross, or miles (including known/unknown differences) make all matching
  rows conflicting. Unresolved/ambiguous identities are provisional. Conflicts,
  repeats and provisional rows are excluded from headlines and shown separately
  in reconciliation, including their amounts. Later corrections can restate prior
  periods; this is an operational report, not a frozen accounting statement.
- RPM is summed Original Gross divided by summed positive miles for counted
  loads having both values. Missing values, excluded rows and coverage counts are
  visible; a missing/zero comparison baseline has no percentage change.
- Dispatcher attribution uses the existing daily historical assignment rule,
  with an explicit Unassigned group. No truck denominator or productivity ranking
  is introduced. Current fleet and attention counts are independent of dates.
- Trucks, drivers and trailers appear as pie charts with linked count/percentage labels
  around their slices and connector lines. Names and totals sit above each card
  (for example, Trucks 117). Truck totals include Ready, In use, Shop and OOS, excluding
  Terminated. Driver totals include Active (including New driver), Home and Vacation;
  other availability and overlapping assignment counts are omitted. Zero counts
  have no slice or callout, and empty charts show a no-data message.
  Trailers use Ready, In use, Shop and OOS with terminated records excluded.
  Truck/trailer In use and driver Active are blue; Shop/Vacation are orange,
  Ready is black, and driver Home is red. The three cards form a responsive row.
  Active driver drill-down uses `availability=active_or_new`, a directory filter
  combining both recorded statuses without changing driver profiles or lifecycle.
- Costs stay separate by source: diesel line items excluding DEF/other purchases,
  production/legacy tolls by posting date, scale tickets by transaction date, and
  visible expense categories by expense date. These all-truck source cards can
  overlap and are separate from the company's waterfall deductions.
  Fuel drill-down shows whole transactions, which may also contain DEF/other
  purchases. Unknown source coverage is not asserted to be complete.

## Weekly waterfall

Gross -> Company fuel -> Company tolls -> Driver / owner pay -> Investor pay ->
Profit. The final value is an earned operating remainder **before other costs**,
not cash received, paid payroll, or net income. All stages use one read-only
repeatable-read transaction. No payroll edits, settlements or syncs are created.

- Gross follows the existing counted-load reconciliation rules.
- Fuel uses production Relay fuel line items, including DEF and refunds. Exactly
  one reported truck unit must resolve through the stored aliases; no driver or
  current-assignment guess supplies a missing truck. Ownership uses purchase date.
- Tolls use production/legacy posting dates for the week and exit dates for
  ownership. Only uniquely resolved company ownership is deducted. Imported
  ownership baselines with an unknown start apply before their recorded end,
  matching payroll classification; an unknown start is not an unknown owner.
  Missing or conflicting ownership blocks profit instead of treating the
  transaction as owner-paid. Fuel and tolls do not wait for payroll finalization.
  Clicking unresolved fuel/tolls opens a paginated list of the exact transactions,
  amounts, truck identities and reasons. Toll posting and ownership (exit) dates
  are shown separately. Known trucks link to their profiles; review never edits
  sources or infers a payer. The list shares the waterfall's attribution query.
  Fuel review also shows its linked driver and exact-purchase-time truck
  assignments as context; those assignments never replace an unmatched source
  truck number automatically.
- Driver/owner pay and investor pay reuse routed weekly payroll and its frozen
  statements: load fees, recorded historical layovers and signed manual weekly adjustments.
  Open statements no longer generate automatic Shop layover bonuses.
  Investor shares deduct their hired-driver earnings so labor is counted once.
  Fuel/toll recoveries, expense recoveries, recurring/installment collections,
  escrow and carry are excluded. Owners/investors therefore cover their own
  operating costs without those recoveries inflating this remainder.
- Counted load ownership and investor terms use Clustie's truck assignment for
  the original service date. Clicking that warning opens the exact counted loads,
  native truck assignments and reasons, using the same query as its count.
  DataTruck assignment labels never determine ownership or create these warnings.
  Missing/conflicting native assignments stay in review. Known trucks link to their profiles.
  Payroll loads must match counted load identities and original gross; missing
  fees, missing payroll coverage, duplicates, stale gross and report issues block
  the final profit. Known partial stages remain visible alongside review links.
- Setup rows without work in the selected week do not block historical profit.
  Other expenses, dispatcher pay, taxes and other income are outside this chart.
  Source completeness is not guaranteed by the existence of recorded transactions.

## API and access

`GET /dashboard/summary?preset=thisWeek` accepts `thisWeek`, `lastWeek`,
`thisMonth`, `lastMonth`, or `custom&dateFrom=YYYY-MM-DD&dateTo=YYYY-MM-DD`.
It also accepts `week&dateFrom=YYYY-MM-DD` and `view=weekly|current`. Hidden view
jobs are not executed; omitting view retains the combined legacy response.
`GET /dashboard/cost-review?kind=fuel|tolls|ownership` accepts the same period parameters
and standard page/pageSize pagination. It requires the waterfall's five read
permissions. All dashboard endpoints require the server-confirmed Administrator flag;
`access.manage` or other permissions alone do not grant access. Session responses
expose `user.administrator` for navigation and login decisions. Within that
boundary, the response also omits unauthorized sections entirely:

| Section | Required access |
| --- | --- |
| Gross, trend, dispatcher breakdown, reconciliation | `board.read` |
| Weekly waterfall | all of `board.read`, `fuel.read`, `tolls.read`, `driver_pay.read`, `investor_pay.read` |
| Trucks / GPS | `trucks.read` |
| Trailers | `trailers.read` |
| Drivers | `drivers.read` |
| Diesel / Relay attention | `fuel.read` |
| Tolls / missing truck links | `tolls.read` |
| Scale / membership attention | `weighmytruck.read` |
| Expenses | `expenses.read` and view access to each included category |
| Service programs | `expenses.read` and Maintenance category view access |
| Tasks | `tasks.read`, existing task viewer visibility and directory access |

`GET /dashboard/loads` additionally requires `board.read`, accepts the same dates plus
`dispatcherId` (UUID or `unassigned`), `inclusion` (included/provisional/conflict/
repeat), `page`, and `pageSize`. Count and page share a database snapshot.
Money is returned as decimal strings. All dashboard responses are `no-store`.

Summary jobs have at most three concurrent queries, 15 seconds per ordinary
section, 45 seconds for payroll reconciliation, and a 60-second request deadline.
A section failure returns an explicit error while
other sections remain usable. The UI refreshes every visible minute and on focus,
aborts obsolete requests, and retains failed sections only for the same resolved
period and still-present permission projection, with their last-result timestamp.

Dashboard drill-down links use `dashboard=1` and explicit filters. The shared
view-memory initializer clears unrelated remembered filters for these links.
Toll listing supports `unlinked=true` and `productionOnly=true`; service programs
support the due/overdue view. Browser Back restores the dashboard date selection.
The sidebar defaults to collapsed below 640px; its manual toggle remains available.

## Validation

Run the repository's normal Go, lint, build and board/payroll regression checks.
After a frontend build with `NEXT_PUBLIC_API_URL=/api`, run
`node scripts/test-dashboard-views.mjs`. It uses isolated API fixtures, covers
permission projection, dates, drill-down, themes, mobile width and error recovery,
and writes screenshots to the system temp directory under
`clustie-dashboard-review`.

`go test ./internal/repository -run TestCompanyDashboard -v` uses optional
`MSERP_DASHBOARD_TEST_DATABASE_URL` for temporary-table calculation fixtures and
`MSERP_DASHBOARD_SCHEMA_TEST_DATABASE_URL` for full-schema reads and fleet
status/drill-down fixtures in a temporary schema, dropped after each run.
It also checks whole-fleet waterfall totals, company-only costs, dated ownership,
refunds, DEF, environment/date exclusions and unresolved-cost handling.
`TestDashboardEarnings` covers pass-through exclusion and stale/missing fees.
`TestDashboardLoadOwnershipReviewDatabase` uses the disposable local
`MSERP_DRIVER_CHARGES_TEST_DATABASE_URL` to verify source identity, missing terms,
owner-driven exemptions, pagination and count/drilldown agreement on fresh and
migrated schemas.
Both must name a disposable localhost database ending `_test`.
Never point these at production. HTTP permission/error tests need no database.
