# Dispatcher commissions

Accounting > Dispatcher Pay uses Gross Board's effective dispatch-week entries,
not a separate DataTruck query or the legacy dispatchers.pay_percentage field.
Migration 163 installs the supplied workbook's commission rules starting in the
deployment's current New York Monday week. Earlier weeks explicitly need a rule
revision with an earlier effective starting week. Deployment creates no payroll
finalizations and does not change Gross Board values.

## Formula and attribution

The qualification unit is the Gross Board main driver's complete Monday–Sunday
week, matching the workbook's Driver/Carrier grouping. Effective Total Gross and
Total Miles include reviewed overrides and selected trip allocations. Deleted,
duplicate and transfer-out markers and day-status cells do not earn commission.
Transfers follow their Gross Board dispatch placement and original dispatcher
credit. Daily assignment history attributes other entries; unassigned loads stay
visible and unpaid. Team co-drivers are already consolidated by Gross Board.

For each driver-week, actual RPM is summed gross / summed miles. Adjusted gross
is actual gross + approved home days × home-day credit. Select its gross tier,
capped at actual gross tier + maximum promotion levels (No tier counts as level
zero). Select the highest RPM threshold met in that tier, or zero commission
below its first threshold. Apply that rate to actual credited gross for each
dispatcher. Do not average load RPMs, increase RPM by home credit, pay imputed
gross, or use Driver Gross. Midweek dispatcher changes divide credited gross
without duplicating the driver's qualifying gross or commission.

Default gross tiers: 7,500 / 9,000 / 11,000. RPM/rate tables:

| Gross minimum | RPM minimums | Percentages |
| --- | --- | --- |
| 7,500 | 2.20 / 2.31 / 2.70 | 1 / 1.25 / 1.5 |
| 9,000 | 2.40 / 2.61 / 2.90 | 1 / 1.5 / 1.75 |
| 11,000 | 2.50 / 2.71 / 2.90 | 1.25 / 1.75 / 2 |

Home-day credit defaults to $1,100 with maximum promotion one tier. Higher tiers
have stricter RPM thresholds: home days CAN reduce or eliminate commission, as
the customer explicitly confirmed. There is no earnings protection or fallback
to the previous tier. Commission is rounded half up to cents per dispatcher /
driver row and summed in cents. Missing gross/miles and unresolved split trips
show Review and prevent statement finalization. Explicit zero gross is valid;
zero or negative weekly miles requires review. Signed Gross Board corrections
remain in gross/mileage totals and reduce credited commission where applicable;
positive and negative commission amounts round symmetrically.

## Rules, inputs and frozen history

Rules are immutable revisions; greatest effective_week <= report week wins,
then greatest id for that starting week. Saving an existing starting week adds
a revision. Future starting weeks supersede older rules from that week forward.
Thresholds, bracket percentages, home-day credit and promotion limit are editable.
Thresholds must increase, with positive gross tiers, 1–10 tiers, 1–20 brackets per
tier, at most two decimals and rates between 0 and 100 percent.

Weekly home days (whole 0–7) and comments are versioned per driver-week. They are
entered explicitly, not inferred from planned leave or the driver's current
status. Editing is blocked while any affected dispatcher statement is finalized.
Inputs, finalizations and reason-required reopenings have append-only history.

Finalization checks the reviewed calculation's revision and statement version
under the shared accounting lock, then saves the complete rule / row / money
snapshot. Reads preserve frozen amounts and flag differing live inputs. Gross
Board corrections remain possible under its existing driver/investor guards;
they never rewrite finalized dispatcher statements. Reopening explicitly returns
the dispatcher statement to current calculations. Rules and audit tables reject
UPDATE/DELETE. New tables register payroll cache revision triggers.

## Access and API

View: dispatcher_pay.read. Weekly input editing: dispatcher_pay.edit. Rule
changes: dispatcher_pay.rules. Finalize / reopen: dispatcher_pay.finalize and
dispatcher_pay.reopen. Each write permission requires View. Administrators have
these automatically; custom roles must be explicitly granted them in Access.
No broader driver private or compensation permissions are needed to view this
dedicated report. Legacy dispatcher percentages remain stored for compatibility
and are removed from operational dispatcher forms and directories.
The dispatcher profile/request/financial API contracts no longer expose or accept
payPercentage; native dispatcher queries never read or write it. Its database
column and legacy permission keys remain inert for schema/rollback compatibility;
the retired permission group is hidden from Access.

The weekly page uses collapsible dispatcher sections and Driver Pay's previous /
next / This week controls. Gross, miles and displayed RPM belong to that
dispatcher's own loads. Full-week gross, adjusted gross and RPM still qualify the
driver's tiers; numbered medal tooltips show that basis and the saved rule's
thresholds. Gross tier is the selected tier; the actual tier and duplicate gross
columns are omitted. Row clicks link to Gross Board by driver UUID and Monday
weekStart, clearing remembered filters and showing that driver under every dated
dispatcher. Per-dispatcher mileage is additional display detail and is excluded
from financial revision hashes, preserving comparisons to older frozen snapshots.
Older split snapshots without recorded own mileage display a dash for that field.

- GET /dispatcher-pay?weekStart=YYYY-MM-DD (defaults current New York week)
- GET /dispatcher-pay/rules
- POST /dispatcher-pay/rules (expectedRevision, effectiveWeek, rules)
- PUT /dispatcher-pay/inputs (weekStart, driverId, homeDays, note, version)
- POST /dispatcher-pay/finalize or /reopen (weekStart, dispatcherId, revision,
  version; reopening also requires reason)

## Validation

`go test ./internal/repository -run TestDispatcher` checks all nine RPM brackets,
exact thresholds, promotion cap, home-day reductions, weighted RPM and credits.
Set MSERP_DISPATCHER_PAY_TEST_DATABASE_URL to a disposable localhost database
ending in _test for fresh / upgraded app-role database checks, effective dates,
stale writes, frozen snapshots, reopenings, audit immutability and cache enrollment.
`node scripts/test-dispatcher-pay-e2e.mjs` uses review-local's isolated real API,
database and browser. CI runs the dispatcher database suite before deployment.
