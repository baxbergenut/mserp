# Maintenance

Fleet > Work orders retains the directory and entry form at `/maintenance`, and
a Work orders tab on each truck. Fleet > Preventative maintenance lives at
`/fleet/preventative-maintenance` with PM Service and Oil change tabs. Expenses
includes work-order charges in the same ledger alongside other expenses, with
category-scoped verification. Explicit legacy `scope=general` API reads still
exclude the configured work-order category. No expense copies are created.

## Records and accounting

- A visit stores the truck, actual service date, odometer in miles, vendor and
  contact/address snapshots, service location, work-order number, notes, work
  items and documents. Completed visits require a truck and service date.
- Work uses stable reporting types: PM service, Oil change, Engine & drivetrain,
  Electrical, Brakes, Tires & wheels, Cooling & fluid systems, Body & chassis,
  Towing & recovery, Cleaning & detailing, and Other maintenance. Detailed names
  now come directly from the reporting type; there is no second work-name field.
  PM service and Oil change are separate types. Administrators add custom service
  types in Work orders > Settings. Roadside is a location, not a
  component reporting type. Reporting type codes never change during a rename.
- A charged visit has exactly one linked expense. Service and financial writes
  commit together; IDs, approvals, payments and existing responsibility routing
  are retained. Driver and owner settlements continue reading the same expense.
  New UI entries always include the invoice amount and charge; there is no optional
  invoice checkbox. Historical/imported entries without known costs retain their
  existing financial state. Vendor creation and edits live in Work orders > Settings.
- Itemized work must price every row and equal the invoice base amount exactly.
  The linked expense breakdown must match each priced work row by name and numeric
  amount. Alternatively, all work amounts remain unknown and reports show the
  invoice cost as unallocated. Tax, fees and discounts use explicit cost rows.
  EFS payment fees remain separate. Unchanged historical EFS invoices with no
  stored base amount retain their original total when adding service metadata.
- Financial guards still reject changes to paid/finalized obligations. Payroll's
  expense detail modal links to the Maintenance visit for typed work corrections.
  Voiding requires category Delete and a reason; paid or verified invoices cannot
  be voided. An unpaid expense is removed atomically while its audit snapshot,
  service details and attachments remain. Voided work does not affect reminders
  or spending reports. Ordinary expense DELETE cannot bypass the visit workflow.
- Vendor directory edits never rewrite earlier visit snapshots. Existing vendor
  IDs and source details stay on the visit. New source-provided details are retained
  when an invoice differs from the directory. Saved historical owners remain the
  financial payer across ownership changes.

## PM, oil and odometers

PM service and Oil change reset their corresponding schedules automatically when
the visit is confirmed Completed. There is no completion dropdown in the entry form.
PM never implies an oil change.
Needs-review entries, estimates and voided visits do not reset a schedule.

Schedules support miles, days or both, with fleet defaults and explicit per-truck
overrides. Either configured limit can make a service due. Warning thresholds
are editable. No service interval is assumed or seeded. A disabled truck override
suppresses the inherited default. Unknown completion information remains visible.

Odometer readings are dated and sourced as manual or service. Truck locks serialize
new readings and corrections. Readings cannot decrease across dates, and conflicting
same-day readings are rejected. Historical service entries never overwrite a newer
reading. For mileage-based schedules, readings older than seven calendar days do
not establish a Current status; missing/stale readings are shown as Missing
information unless a known date or mileage limit already proves Due/Overdue.
The truck's legacy mileage/last-service/next-service fields remain legacy evidence.
For a new visit dated today, selecting the truck reads Five ELD's active-unit and
historical-tracking endpoints using its exact unique VIN. The latest nonfuture
odometer within one hour fills an empty field. Receipt values and manual edits
are preserved. Older service dates never receive today's reading. Missing, stale,
ambiguous or unavailable telemetry leaves manual entry available. The GET is
authorized by Maintenance View/Create/Edit and reads only the background cache.
The manual odometer-history panel has been removed from Settings.

Due services refresh on focus and every 30 visible seconds. A startup/minute worker also creates PM and oil-change system tasks for upcoming, due and overdue services. See the assignment and lifecycle rules below.
Reports aggregate exact service amounts by reporting type, truck, vendor and period,
with separate payment fees and unallocated costs. Repeated repairs group the same
truck/component across distinct visits, excluding routine preventive/cleaning work
and unclassified fees. Directory filters scope report totals to matching visits.

## AI and documents

The existing server-only Luna Responses client has a strict maintenance schema for
vendor details, actual service date, odometer, service location, invoice presence,
work-order number, explicit engine oil grade and typed work/cost items. Type codes are restricted to the active
catalog. Explicit completed internal/no-charge notes are supported. Sources are
untrusted data; instructions in documents cannot change the extraction task.

Text, TXT, images and PDFs up to 20 MB can be analyzed. Dates, vehicle links,
completion tags and costs remain editable; every suggestion starts Needs review.
Multi-visit documents are reviewed and saved one visit at a time, with a clear
Save & review next action. File uploads follow a successful visit save; failures
retain the saved visit and allow upload retry without creating another charge.

Documents live in `maintenance_documents`, independent of fleet cab-card files.
The AI source file is attached automatically after save; there is no separate
Attach receipt control. Downloads require Maintenance category View; fleet file permissions do not grant
access. Uploads require category Edit, or Create for a visit owned by that user ID.

## Permissions and migration

All routes require session/CSRF and enforce the existing Maintenance category's
View/Create/Edit/Delete actions. Create-only users can prepare a new visit without
reading other invoices. Only built-in Administrators can manage reporting types or
fleet-wide schedule defaults; per-truck schedules and odometers require Edit.
Verification/cancellation use the existing expense role and receipt guards.

Migration 096 is additive and updates `init.sql`. It backfills existing Maintenance
expenses as Needs review, retaining their original expense IDs and costs. Service
date, vendor and odometer initially remain unknown. Migration 100 applies the customer-confirmed historical completion/date correction described below. Existing
clients creating a Maintenance expense receive a review placeholder automatically.
The database guards completed work costs against mismatching ledger edits. Application
rollback does not undo this schema or those guards; use a compatible forward fix to
edit typed visits if an older frontend is temporarily restored.

## API

- GET/POST `/maintenance`, GET/PUT `/maintenance/{id}`, POST `/maintenance/{id}/void`.
- GET `/maintenance/access`, `/options`, `/reports`, `/due`, `/readings`.
- GET `/maintenance/{id}/history`; immutable visit audit snapshots retain actor names.
- POST/PUT `/maintenance/vendors[/{id}]`; PUT `/maintenance/types/{code}`.
- POST/PUT `/maintenance/schedules[/{id}]`, `/maintenance/readings[/{id}]`.
- POST `/maintenance/{id}/documents`; GET `/maintenance/documents/{id}`.
- POST `/maintenance/extract` with multipart `text` and/or `file`.

Visit, vendor, schedule and reading edits require the displayed version. Invoice
edits also retain the displayed expense updatedAt. A client UUID identifies each
new visit, preventing retry-created duplicates.

Migration 097 separates the Oil change type, seeds six US shop chains, and stores
driver identity on the visit independently of an invoice or its payer. It preserves
existing financial records and recorded completion evidence. Invoice names derive
from selected service types; the invoice date follows the service date. The entry
form has one Amount field, with optional exact service cost allocation.
Single-service invoices automatically allocate that amount to the selected type;
only multiple-service invoices offer separate allocation fields.

`backend/cmd/import-oil-changes` imports a bounded A:G JSON snapshot from the Fleet
board Oil change tab. It defaults to dry run; `-apply` writes source-traceable visits.
Exact truck matching retains leading zeros. Deterministic event IDs make retries
idempotent; existing confirmed oil changes on the same date are skipped. Odometer
conflicts, unlinked source trucks and possible matching unreviewed invoices are
retained as Needs review. Unlinked entries keep the original unit and reference VIN
for later linking; they never create fleet profiles. No
charges, vendors, historical drivers or PM completions are invented. Undated current
odometer cells and other sheet information remain in source notes.

## Validation

`TestMaintenanceDatabase` uses the existing local disposable
`MSERP_DRIVER_PAY_TEST_DATABASE_URL` for fresh and pre-096 migrations as mserp_app.
It checks cost atomicity, EFS separation, vendor history, odometer chronology,
independent schedules, version guards, reporting and void history.

Build frontend with `/api`, run `review-local.mjs`, then run
`node scripts/test-maintenance-e2e.mjs` with `MSERP_REVIEW_ACCESS_FILE`. Set
`MSERP_REVIEW_OPENAI_ENABLED=1`, server `OPENAI_API_KEY`, and
`MSERP_OPENAI_LIVE_TEST=1` only for explicit synthetic live AI tests. Browser tests
exercise the real API/database plus text/image/PDF extraction and read-only access.

## Preventative overview and US vendor directory (099)

`GET /maintenance/preventative?serviceCode=pm|oil&page=1&pageSize=25` requires
category View and returns paginated active trucks, including trucks with no
recorded service. `search` matches unit numbers; `includeInactive=true` includes
retired trucks. Each row reads its latest Completed tagged visit, with the date,
vendor snapshot, service odometer, oil grade and notes from that same visit.
Needs-review and voided work never replace completed history. Clicking its date
opens the original work order in a modal on the current overview page. Notes stay on one line with a scrollable hover/focus popup. Service cost sums only allocated matching work
items, excluding payment fees; historical unknown costs and mixed unallocated
invoices remain unknown. No new expense is created by an overview read.

Visible rows request live Five ELD mileage with three concurrent calls and abort
on navigation. The identity roster is cached for five minutes and each reading
for one minute; every returned reading must still satisfy the one-hour freshness
rule. Current mileage uses normal text; unavailable/outdated readings use gray text, with their source/date on hover. Visible rows refresh each minute and on focus.
The entry input shows source/time on hover, without a note beneath it.

Oil-change selection defaults new entry to SAE 10W-30 and offers common grades,
Unknown and a custom value. AI extracts a grade only from explicit evidence; the
review form uses its new-entry default when absent. Existing unknown historical
grades remain blank. The migration backfills only explicitly recorded imported
source grades, and imports store their source grade on future visits.

Preventative Settings exposes separate fleet PM/oil mileage/day defaults. Only
administrators edit fleet defaults; existing truck overrides take precedence,
and the first configured limit reached makes a service due. No numerical service
interval is invented. Work orders now has History and Settings only.

Migration 100 replaces the untouched branch seeds from 099 with one entry per
brand: Love's, Speedco, TA, Petro, Goodyear, Blue Beacon, Boss, Southern Tire Mart,
Southern Tire Mart at Pilot, Rush, MHC and Velocity. Sources are retained in
`docs/data/maintenance-brands.json`. Superseded branch records remain in the
database but disappear from vendor options. Existing visits reference the brand
while keeping their original name/address/phone snapshots. Custom/edited vendors
are preserved. Enter a location address on the work order, not as another brand.
Truck-detail History and Due services omit the truck column; Settings is available
only in the standalone Work orders workspace.

## Historical correction and service tasks (100–101)

The customer confirmed that old expense-sheet work orders were completed and the
sheet payment date is their service date. Migration 100 copies the already-saved
`expenses.expense_date` for untouched Needs-review imports from
`Maintenance_and_other_expenses`, retains ledger IDs/costs, and audits the change.
Exact PM Service and Oil change names receive their respective completion tags.
Unlinked historical units remain unlinked but Completed; the scoped legacy flag
allows this without permitting newly entered completed work to omit a truck.
Missing dates and edited records are preserved for review. No mileage is invented.

Migration 101 adds PM and oil-change task categories and `assigneeRoleIds` to
versioned system-task assignments. Settings > System tasks supports users and
roles together. Active membership is evaluated on each request across list,
counts and source workflow authorization, so role changes immediately affect
visibility. Duplicate user/role membership appears once. Assignment changes are
audited; omitted role arrays preserve previous roles for older clients, while an
empty array clears them. Existing task permission requirements still apply.
Unassigned categories are visible to Administrators, matching other categories.

The startup/minute worker evaluates the same PM/oil schedules and precedence as
the preventative overview, with New York calendar dates. Fresh Five ELD mileage uses exact
unique VIN identity, bounded concurrency and a 40-second telemetry budget; saved
dated readings remain the fallback. It never writes telemetry as service history.
At most one open task exists per truck/service. Repeated runs update the same
reminder; completed service resolves that cycle and a later due cycle creates a
new task. Disabling a schedule or retiring a truck resolves with an explicit
outcome. Missing mileage does not silently clear an existing reminder. Missing
baselines cannot produce a guessed due date. Task links open filtered Work orders;
completion comes from recorded service, not generic task editing.

Validation includes fresh and migrated schemas, historical dates without truck
links, role membership/privacy, idempotent reminders, missing telemetry, completion
and subsequent cycles, plus the real-API maintenance browser suite.

## Shared accounting workspace and background mileage (102)

The preventative endpoint returns nextMiles, remainingMiles (signed), nextDate,
status, reason, currentSource and currentReportedAt for both service tabs. Red
rows are overdue/due; amber rows are within the configured warning threshold.
Truck-specific settings override fleet defaults, including disabled overrides.
The next due odometer can be known even without current mileage; remaining miles
requires a known current reading. Missing baselines never invent a deadline.

The startup/minute maintenance worker selects active trucks by exact unique VIN,
including trucks without schedules or service history. It persists provider
mileage/report time separately in maintenance_telemetry, with five-minute attempt
spacing, three workers, oldest attempts first and a forty-second cycle budget.
Failures preserve prior observations; changed/ambiguous VINs cannot reuse cache.
GET /maintenance/odometer and the overview never contact the provider. Only fresh
cache fills today's blank work-order odometer; source receipts/manual service
values and older dates are preserved. Telemetry never becomes a dated service
reading or resets any schedule. Background page refreshes retain table rows.

Settings opens on service reporting types; vendors have search/pagination and
intervals have their own section. The manual reading panel, Due services and
Reports UI tabs are removed; the underlying audit, report and correction APIs
remain compatible. Non-administrators see reporting types without edit controls.

Migration 102 renames the canonical default Maintenance category to Work orders
without changing its UUID, permissions or historical expense category snapshots.
The expense options return workOrdersCategoryId for stable form routing. Expenses
and global expense search include work-order charges; verification uses the same
record, revision and author checks as before. The list has only financial columns.
Existing work-order rows open the shared work-order modal in place; deleting a
service still uses the audited void workflow and existing financial guards.

Selecting Work orders while adding an expense carries the financial draft into
the work-order modal. Change expense category can return to ordinary entry. AI
uses nullable strict maintenance metadata for mixed documents, restricted to the
caller’s creatable categories and active service codes. Each reviewed item saves
separately in source order, retaining its remaining queue after a failed save.
Source files attach automatically to work orders after persistence; a failed
upload retains the saved record for retry. Non-service entries use the ordinary
expense form. No reviewed service bypasses work-order validation or creates a
second expense. Category changes, truck/date confirmation, cost allocation and
oil grades remain editable before saving.

Validation: TestMaintenanceDatabase covers cache persistence/outages, exact VIN
changes/ambiguity, absence of invented dated readings and independent due math.
The local review scripts test-work-order-accounting.mjs and test-maintenance-e2e.mjs
cover entry switching, one ledger charge, verification, in-place editing, compact
settings, due colors and optional live Luna text/image/PDF mixed extraction.
