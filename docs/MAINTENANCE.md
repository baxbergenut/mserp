# Maintenance

Maintenance has its own directory and entry form at `/maintenance`, and a tab
on each truck. General Expenses excludes the Maintenance category when reading
with `scope=general`; payroll and ordinary legacy expense API reads still use
the complete financial ledger.

## Records and accounting

- A visit stores the truck, actual service date, odometer in miles, vendor and
  contact/address snapshots, service location, work-order number, notes, work
  items and documents. Completed visits require a truck and service date.
- Work uses stable reporting types: Preventive maintenance, Engine & drivetrain,
  Electrical, Brakes, Tires & wheels, Cooling & fluid systems, Body & chassis,
  Towing & recovery, Cleaning & detailing, and Other maintenance. Detailed names
  may use the existing defaults or custom text. Roadside is a location, not a
  component reporting type. Reporting type codes never change during a rename.
- A charged visit has exactly one linked expense. Service and financial writes
  commit together; IDs, approvals, payments and existing responsibility routing
  are retained. Driver and owner settlements continue reading the same expense.
  A visit may also record internal/warranty work with no financial record.
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

PM and Oil change have separate explicit completion tags. Only completed visits
with the corresponding tag reset that schedule. PM never implies an oil change.
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
The truck's legacy mileage/last-service/next-service fields remain legacy evidence;
they are not fabricated PM/oil completion records. Five ELD still supplies positions
only; automatic odometer import is not enabled.

Due services are in-app reminders, refreshed on focus and every 30 visible seconds.
Reports aggregate exact service amounts by reporting type, truck, vendor and period,
with separate payment fees and unallocated costs. Repeated repairs group the same
truck/component across distinct visits, excluding routine preventive/cleaning work
and unclassified fees. Directory filters scope report totals to matching visits.

## AI and documents

The existing server-only Luna Responses client has a strict maintenance schema for
vendor details, actual service date, odometer, service location, invoice presence,
work-order number and typed work/cost items. Type codes are restricted to the active
catalog. Explicit completed internal/no-charge notes are supported. Sources are
untrusted data; instructions in documents cannot change the extraction task.

Text, TXT, images and PDFs up to 20 MB can be analyzed. Dates, vehicle links,
completion tags and costs remain editable; every suggestion starts Needs review.
Multi-visit documents are reviewed and saved one visit at a time, with a clear
Save & review next action. File uploads follow a successful visit save; failures
retain the saved visit and allow upload retry without creating another charge.

Documents live in `maintenance_documents`, independent of fleet cab-card files.
Downloads require Maintenance category View; fleet file permissions do not grant
access. Uploads require category Edit, or Create for a visit owned by that user ID.

## Permissions and migration

All routes require session/CSRF and enforce the existing Maintenance category's
View/Create/Edit/Delete actions. Create-only users can prepare a new visit without
reading other invoices. Only built-in Administrators can manage reporting types or
fleet-wide schedule defaults; per-truck schedules and odometers require Edit.
Verification/cancellation use the existing expense role and receipt guards.

Migration 096 is additive and updates `init.sql`. It backfills existing Maintenance
expenses as Needs review, retaining their original expense IDs and costs. Service
date, vendor and odometer remain unknown; no PM/oil completion is invented. Existing
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
