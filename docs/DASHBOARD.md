# Company dashboard

`/dashboard` is available only to Administrators, as their default signed-in
landing page and first sidebar item. Other users retain their first permitted
page as the landing page and cannot open the dashboard through a direct link.
It reads local records and cached integration data; opening or refreshing it never
starts provider synchronization or writes fleet, accounting, or board records.
No migration or additional environment variable is required.

## Reporting rules

- The API resolves business dates in `America/New_York`. This week/month runs
  through today and compares with the same elapsed portion of the previous
  week/month (clamped to the previous month's length). Last week/month uses full
  periods. Custom dates are inclusive, at most 366 days, and compare with the
  immediately preceding equal-length period. Trends use daily buckets up to 90
  days and calendar weeks beyond that; boundary weeks can be partial.
- Original Gross is the headline; Driver Gross is the recorded allocation, not
  driver pay. Gross Board service dates determine the reporting period. Planned
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
- Costs stay separate by source: diesel line items excluding DEF/other purchases,
  production/legacy tolls by posting date, scale tickets by transaction date, and
  visible expense categories by expense date. Costs can overlap; there is no
  combined cost/profit total, payer inference, or payment-status inference.
  Fuel drill-down shows whole transactions, which may also contain DEF/other
  purchases. Unknown source coverage is not asserted to be complete.

## API and access

`GET /dashboard/summary?preset=thisWeek` accepts `thisWeek`, `lastWeek`,
`thisMonth`, `lastMonth`, or `custom&dateFrom=YYYY-MM-DD&dateTo=YYYY-MM-DD`.
Both dashboard endpoints require the server-confirmed Administrator flag;
`access.manage` or other permissions alone do not grant access. Session responses
expose `user.administrator` for navigation and login decisions. Within that
boundary, the response also omits unauthorized sections entirely:

| Section | Required access |
| --- | --- |
| Gross, trend, dispatcher breakdown, reconciliation | `board.read` |
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

Summary jobs have at most three concurrent queries, 15 seconds per section and
a 20-second request deadline. A section failure returns an explicit error while
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
`MSERP_DASHBOARD_SCHEMA_TEST_DATABASE_URL` for read checks against the complete
initialized schema. Both must name a disposable localhost database ending `_test`.
Never point these at production. HTTP permission/error tests need no database.
