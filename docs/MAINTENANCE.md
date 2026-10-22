# Maintenance

Fleet > Work orders retains the directory and entry form at `/maintenance`, and
a Work orders tab on each truck. Fleet > Service programs lives at
`/fleet/service-programs`, with recurring programs and explicit truck assignments.
The old `/fleet/preventative-maintenance` page redirects to Oil change 20k. Expenses
includes work-order charges in the same ledger alongside other expenses, with
category-scoped verification. Explicit legacy `scope=general` API reads still
exclude the configured work-order category. No expense copies are created.

## Records and accounting

- A visit stores the truck, actual service date, odometer in miles, vendor and
  contact/address snapshots, service location, work-order number, notes, work
  items and documents. Completed visits require a truck and service date.
- Work uses stable reporting types: PM Service, Engine & drivetrain,
  Electrical, Brakes, Tires & wheels, Cooling & fluid systems, Body & chassis,
  Towing & recovery, Cleaning & detailing, and Other maintenance. Detailed names
  now come directly from the reporting type; there is no second work-name field.
  PM Service includes both PM work and oil replacement. Administrators add custom service
  types in Work orders > Settings. Roadside is a location, not a
  component reporting type. Reporting type codes never change during a rename.
- A charged visit has exactly one linked expense. Service and financial writes
  commit together; IDs, approvals, payments and existing responsibility routing
  are retained. Driver and owner settlements continue reading the same expense.
  New work orders can start without an invoice or known cost. An explicit Invoice
  selection adds the existing linked expense; No charge is a separate confirmation. Historical/imported entries without known costs retain their
  existing financial state. Vendor creation and edits live in Work orders > Settings.
- New entry uses one Service type and the shared Expenses Add cost breakdown.
  Cost rows have independent names (including custom parts, labor, tax or discounts)
  and must sum exactly to the invoice base amount. The selected service receives
  the invoice base amount for reporting and alone controls PM completion.
  Migration 110 preserves existing rows and removes the old name-for-name coupling.
  Existing multi-type orders retain their classifications and allocations until
  the user explicitly chooses a single replacement type. Alternatively, all work amounts remain unknown and reports show the
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

## PM Service and odometers

Current odometer cells open a provider comparison showing Samsara and Five ELD
mileage, observation times and rejected-reading reasons. Maintenance viewers can
compare; editors can save Automatic, Samsara, Five ELD or Manual per truck. The
choice applies across programs and work order autofill, with no silent fallback
from an explicitly chosen source. Invalid readings remain visible but cannot be
chosen. Automatic prefers fresh valid Samsara then fresh valid Five ELD.
Manual observations require a timestamp and reason in this dialog, are checked
for chronology and implausible mileage, and remain versioned/audited. Recording
one does not itself change the chosen source. Source changes are also audited.
Migration 133 stores these choices and raw observations. GPS is independent;
Status Board always uses Five ELD. See integrations/SAMSARA.md for validation.

PM work and oil replacement reset the same PM Service schedule automatically when
the visit is confirmed Completed. There is no completion dropdown in the entry form.
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
Preventative maintenance offers Maintenance Edit users a pencil beside Last PM
and clickable service/current odometers. Last PM edits save an audited operational
baseline, including a nullable service odometer, without changing work orders or
invoices. Baseline and latest-work-order versions reject stale edits. Due services
and system reminders share this baseline; a new or changed latest completed PM
work order supersedes it. Current odometers use the existing dated manual-reading
workflow and cannot conflict with saved chronology or the effective PM baseline.
Migration 126 adds maintenance_pm_baselines; PUT /maintenance/preventative/{truckId}
requires Maintenance Edit and records the session actor.

Due services refresh on focus and every 30 visible seconds. A startup/minute worker also creates PM Service system tasks for upcoming, due and overdue services. See the assignment and lifecycle rules below.
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
charges, vendors or historical drivers are invented; confirmed source service
resets the unified PM Service schedule. Undated current
odometer cells and other sheet information remain in source notes.

## Validation

`TestMaintenanceDatabase` uses the existing local disposable
`MSERP_DRIVER_PAY_TEST_DATABASE_URL` for fresh and pre-096 migrations as mserp_app.
It checks cost atomicity, EFS separation, vendor history, odometer chronology,
the unified PM schedule, version guards, reporting and void history.

Build frontend with `/api`, run `review-local.mjs`, then run
`node scripts/test-maintenance-e2e.mjs` with `MSERP_REVIEW_ACCESS_FILE`. Set
`MSERP_REVIEW_OPENAI_ENABLED=1`, server `OPENAI_API_KEY`, and
`MSERP_OPENAI_LIVE_TEST=1` only for explicit synthetic live AI tests. Browser tests
exercise the real API/database plus text/image/PDF extraction and read-only access.

## Preventative overview and US vendor directory (099)

`GET /maintenance/preventative?page=1&pageSize=25` requires
category View and returns paginated active trucks, including trucks with no
recorded service. `search` matches unit numbers; `includeInactive=true` includes
retired trucks. Each row reads its latest Completed PM or legacy oil-tagged visit, with the date,
service odometer and oil grade from that same visit.
Needs-review and voided work never replace completed history. Clicking its date
opens the original work order in a modal on the current overview page. Vendor, service cost and last-service-note fields are omitted from the overview API
and table. No new expense is created by an overview read.

Visible rows request live Five ELD mileage with three concurrent calls and abort
on navigation. The identity roster is cached for five minutes and each reading
for one minute; every returned reading must still satisfy the one-hour freshness
rule. Current mileage uses normal text; unavailable/outdated readings use gray text, with their source/date on hover. Visible rows refresh each minute and on focus.
The entry input shows source/time on hover, without a note beneath it.

PM Service selection defaults new entry to SAE 10W-30 and offers common grades,
Unknown and a custom value. AI extracts a grade only from explicit evidence; the
review form uses its new-entry default when absent. Existing unknown historical
grades remain blank. The migration backfills only explicitly recorded imported
source grades, and imports store their source grade on future visits.

Preventative Settings exposes one fleet PM Service mileage/day default. Only
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

The startup/minute worker evaluates the same PM Service schedule and precedence as
the preventative overview, with New York calendar dates. Fresh Five ELD mileage uses exact
unique VIN identity, bounded concurrency and a 40-second telemetry budget; saved
dated readings remain the fallback. It never writes telemetry as service history.
At most one open PM Service task exists per truck. Repeated runs update the same
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
status, reason, currentSource and currentReportedAt for the unified PM Service table. Red
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
changes/ambiguity, absence of invented dated readings and unified PM due math.
The local review scripts test-work-order-accounting.mjs and test-maintenance-e2e.mjs
cover entry switching, one ledger charge, verification, in-place editing, compact
settings, due colors and optional live Luna text/image/PDF mixed extraction.

## Unified PM Service (108)

PM Service is the single service name, reporting type, schedule and reminder
category. Both completed PM and former oil-tagged work reset it using the latest
completed visit; Needs review and voided records remain excluded. The overview has
no service tabs. Vendor, service cost and last-service-note columns are removed.
Oil grade remains available on PM Service work orders and in the overview.

Migration 108 retains existing PM intervals at each fleet/truck scope and promotes
an oil-only interval at that scope to PM. Conflicting retired settings are recorded
in access_audit, along with both original assignment configurations. People and
role assignees are merged. One open reminder per truck survives with the strongest
observed mileage; duplicate reminders remain as completed history. Historical
invoice amounts, cost breakdown names, service evidence and audit snapshots are
not rewritten. Reads display legacy service types as PM Service; edits and imports
normalize old oil type codes to preventive/pm under the usual financial guards.
The old serviceCode overview selector is accepted for existing clients, but every
read returns the unified PM cycle. Schedule writes accept only pm.


## Service programs (129)

Fleet > Service programs replaces the former PM-only overview. Migration 129
creates Oil change 20k, enrolls the existing trucks (including inactive trucks),
and preserves all existing PM intervals, overrides, manual baselines, historical
work orders, invoices and reminder assignees. Production's reviewed fleet setting
was 20,000 miles with a 1,000-mile warning, without truck overrides. New trucks
are assigned explicitly; creating a truck does not claim service completion.

Administrators with Work orders Edit create/edit programs: name, instructions,
mileage interval, optional days or calendar months, warning miles/days, active
state and reminder people/roles. At least one interval is required. Days and
months are alternative calendar units; mileage and time use whichever comes
first. Month intervals clamp to the last valid day (January 31 + one month is
February 28/29). Programs inherit the existing maintenance_pm system-task
assignees by default; explicit program assignees override that default. Current
active role membership controls task lists and counts. Empty explicit assignees
leave visibility to Administrators. Default assignments stay in System tasks;
program-specific assignments stay on the program.

Work orders View reads programs and truck tracking. Edit assigns/removes trucks
(in batches up to 500), sets optional per-truck interval overrides, corrects
versioned baselines and records service. Removed assignments and paused programs
retain history and resolve open reminders. Reactivating resumes their saved
service cycle. Unknown baselines remain Missing information. No enrollment date
is substituted for a last-service date. The truck profile has a Service programs
tab, with the same data and controls under category permissions.

The tracking table shows Last service, Service odometer, Current odometer,
**Miles since service = current odometer minus service odometer**, and Actions.
The Status column/filter is omitted from both program tables; due calculations
and reminders remain active.
It does not display miles remaining or the next-due odometer. Calendar due dates
remain visible where configured. Missing readings/baselines display a blank
marker; stale readings cannot establish Current. Existing cache-only Five ELD
rules and odometer chronology remain authoritative.

Work orders have explicit completed-program checkboxes. One completed visit can
fulfill several assigned programs; only those programs reset. Needs-review and
voided visits never reset them. Reporting type and accounting amounts remain
independent. Older clients/imports omitting programIds retain type-based PM
behavior. Explicit empty programIds means no program was completed. Historical
records are interpreted without rewriting their evidence. Record service on a
program saves a real completed internal work order without an invoice; users
enter the actual date/odometer and select only completed programs. Normal charged
work orders retain their existing accounting workflow and one expense identity.

Manual last-service corrections preserve invoices and capture the latest source
record/version; a changed/new latest completed work order supersedes the baseline.
Program, enrollment and baseline writes reject stale versions and are audited.
There is no destructive delete operation. Programs use stable UUID identity.
The migrated oil program continues using the original PM baseline/schedule tables
for compatibility. Custom programs use service_program_trucks baselines.

The minute worker maintains one open reminder per truck/program/cycle. The oil
program retains maintenance_pm task identities; custom programs use the separate
maintenance_program kind so an older PM worker cannot rewrite their reminders.
Both kinds inherit the existing default assignees unless a program overrides them.
The internal maintenance_program assignment row is hidden from Settings because
its effective assignees come from the program/default configuration.

API: GET/POST /maintenance/programs, PUT /maintenance/programs/{id},
GET/POST /maintenance/programs/{id}/trucks, PUT
/maintenance/programs/{id}/trucks/{truckId}/baseline, GET
/maintenance/program-trucks?truckId=..., and administrator-only GET
/maintenance/program-assignees. Maintenance options include programs and active
enrollment identities for work-order completion selection.

Validation: TestMaintenanceDatabase covers fresh and migrated schemas as
mserp_app, independent/combined completion, baselines, void fallback, reminders,
role privacy, stale writes and retained history. TestServiceProgramCalendar
covers end-of-month scheduling. With a /api build and isolated review-local.mjs,
run node scripts/test-service-programs-e2e.mjs and
node scripts/test-maintenance-e2e.mjs. Screenshots and credentials stay in the
private local review directory.


## Automatic service-type completion (130)

The Service programs page opens Oil change 20k by default. An explicit
?program= UUID opens another program; ?program= (empty) opens the directory.
Truck-profile tabs continue showing all of that truck's assigned programs.

Work orders > Settings > Service types has a Completes service program selection.
Migration 130 renames the preventive reporting type to Oil change and links it
to the existing Oil change 20k program. Stable codes and historical financial
snapshots remain unchanged. The hidden retired oil type gets a distinct catalog
name so its retained unique name does not block the active type's rename.

Saving a work order snapshots the selected service types' linked programs on the
server. A submitted empty programIds cannot suppress those completions. The
normal form shows the automatic effect instead of program checkboxes. Other
reporting types can link to other programs. Existing explicit completions remain
supported (including combined completed service from a program). Changes to type
configuration do not reinterpret prior saved completions. Older type editors
that omit programId preserve an existing link; an explicit empty string clears it.
Needs-review and voided visits remain excluded. Recording a linked service does
not enroll its truck: excluded trucks remain excluded, and a later deliberate
enrollment can use the recorded service history. Program pauses suppress
reminders without claiming a service did not happen.

The customer requested a one-time reconciliation of Oil change 20k against Fleet
board > Oil change (spreadsheet 1RcRmMoM4Jyj1OTAMqDFxawHDiPG8oRVEPzbVOZHmJDk,
gid 1621284305). Use the live sheet as source, exact unit numbers with leading
zeros, and source/local VIN evidence for identity conflicts. Never substitute
047(O) for 047: their recorded VINs differ. Remove assignments through the
versioned program membership API, and use audited source-fenced last-service
baselines for operational corrections without rewriting invoices. Undated sheet
Current ODO values are not dated odometer readings. The source snapshot hash,
changes, missing identities and read-back results are recorded in access_audit
under oil_sheet_reconciliation; private operational snapshots are not committed.

Program settings also exposes a multi-select dropdown of service types that reset
the program. This edits the same maintenance_types.program_id links used by the
type editor; a type can reset one program, while a program can have multiple
resetting types. Types linked elsewhere show their program and cannot be taken
implicitly. Program settings and affected type links save in one audited
transaction, using program and type versions to reject concurrent edits. API
serviceTypeCodes omitted/null preserves links; [] unlinks all. Reads include
serviceTypeCodes and serviceTypeVersions for assigned types. Writes include the
original assigned type versions plus versions of newly selected types. Existing
work-order completion snapshots and program enrollment remain unchanged.

Service rows use Record service to enter completed work; the last-service date
opens its saved work order. There is no separate last-service pencil. The Actions
header and row controls align to the table's right edge.
Record service prefills the truck's current odometer and lets the user change it.
No available reading leaves the field blank; zero remains a valid reading.

Settings > Roles & permissions > Service programs > Manage Service programs
controls program creation/settings and reminder-assignee choices. It replaces
the Administrator-only restriction. These actions also require Work order
category View and Edit; viewing/enrollment/recording retain their existing
category gates. Maintenance options returns canManagePrograms for button access.
The explicit service_programs.manage route policy protects saves and assignee
lookups; granting it does not grant access administration or other settings.

## Fleet work order lifecycle (132)

The workspace uses tables under In progress, Completed - pending payment, and
Completed - paid. Canceled keeps canceled/voided records out of the paid list.
In progress includes newly created, scheduled and active repairs. Its rows show
truck/hold, service type, description, shop, planning dates, work stage and creator.
Pending payment shows service date, invoice amount (unknown stays blank) and
whether an invoice is entered. Paid shows the current recorded invoice amount, method
and payer; explicitly No charge orders are identified without inventing payment.
Filtering and pagination stay server-side; tab changes cannot momentarily show
another stage's rows. There is no Kanban board or work-order summary-card strip.

Rows open /maintenance/detail?id=... with a three-step progress bar. Creation is
already complete, Work tracks repairs, and Invoice & payment is the final step.
In Work, service type, service date, scheduled date and expected-ready date sit
beside the description on wide screens, stacking on narrow screens. New orders
need only a truck, description and dispatch impact; unknown prices never create
zero-value expenses. Creator/time are immutable server snapshots shown in the
ongoing table and details; there is no responsible-person field.

Internal workStatus codes remain compatible: open/scheduled/in_progress/
needs_review are ongoing, completed means work finished and pending payment,
and closed means paid or explicitly No charge. Actual service completion alone
updates service-program history. Cancellation requires a reason; completed service
cannot be canceled. Planning dates, priority and waiting reason remain optional.

Work orders explicitly select dispatch impact: no restriction, Shop, OOS or Hold.
The hold survives work completion and cancellation until a Work orders Edit user
releases it. Administrator and trucks.edit are not required. Multiple orders and
manual truck restrictions combine; releasing one does not clear another. Changing
drivers preserves manual restrictions. Active/Inactive and crews stay separate.

Status Board receives only truckOperatingStatus and renders Shop/OOS/Hold beside
the truck number, without repair reasons or invoice details. Commit-only updates
refresh the board when a hold changes. New current/queued load assignments and new
current/future Gross Board loads check the truck under its row lock. Existing
loads, corrections and historical accounting remain editable. No external booking
integration or future downtime reservations are implemented. Expected-ready dates
never release a truck automatically.

Invoice & payment reuses the existing invoice amount, reference, payment method,
Paid by and Responsibility fields. Saving an invoice alone does not confirm paid.
Confirm paid & finish explicitly confirms an already made payment and stores an
original confirmation of those invoice fields and the confirming employee/time.
Paid orders expose **Edit work order** for every ordinary field. Versioned saves
keep that original confirmation and append `invoiceCorrections` when amount,
method, payer or reference changes; each save keeps its before/after audit. The
paid directory displays the corrected current invoice. Existing finalized payroll,
collection identity and EFS collectible-total safeguards remain enforced. These
edits do not issue money, create recovery payments, or replay sheet exports.
It never issues money or alters expense_payments/driver-owner recovery. There is
no separate shop-payment screen, endpoint or ledger. Done requires finished work,
release of this order's hold and either that confirmation or explicit No charge.
Confirmed financial fields cannot be silently changed or detached. Other orders'
holds remain effective after this order is Done.

Migration 135 applies the user's explicit confirmation that pre-workflow work was
paid. A one-time historicalPaid marker covers only blank work_status, no hold,
completed/needs_review records created before migration 132's ledger activation
instant. Canceled/voided records and explicit new workflow stages are excluded.
The marker is immutable and cannot be supplied by clients or added to new rows.
Those records appear only in Completed - paid, including unresolved imported oil
history; original service status, truck linkage, dates, odometers and programs
remain unchanged. Never manufacture a payment transaction, zero/no-charge invoice
or payment date. Existing invoice method/payer remain visible as recorded, while
unknown invoice amounts stay unknown. The versioned cutover writes per-record
before/after audit snapshots. Payroll/owner recovery and expense fields are untouched.
Historical correction forms preserve service-review status, and historical records
cannot reopen into active work or place dispatch holds. New orders retain explicit
payment confirmation. Canceled/voided records remain in Canceled.

Validation: TestMaintenanceDatabase covers fresh/incremental schemas as mserp_app,
multiple holds, stale edits, dispatch guards, no-price creation, no-charge closure,
creator immutability and invoice-confirmation guards. The real API/browser
scripts test-maintenance-e2e.mjs and test-fleet-workflow-e2e.mjs use review-local.mjs
with an isolated local _test database, including non-administrator Fleet release,
Shop-only live board display, stage-table separation, details and mobile layouts.
The historical cutover is also covered by test-historical-work-orders-e2e.mjs
against a disposable local review schema, including historical payment display,
unlinked oil history and preservation of new unpaid orders.
