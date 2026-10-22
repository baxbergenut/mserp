# DataTruck driver and truck enrollment

## Existing identities and profile updates (141)

`datatruck_enrollments.provider_id` stores DataTruck's numeric driver/truck ID
separately from the local UUID. A tenant/kind/ID unique index prevents duplicate
links. Existing drivers require reviewed contact evidence; trucks use unique
exact VINs. Links retain evidence and append-only enrollment events, without
creating provider accounts or changing assignments. Profiles show the saved ID.

Driver `dateOfBirth` is optional YYYY-MM-DD input under
`drivers.private.read/edit`. Omitted/null input preserves the date; an explicit
empty string clears it. The website's roster XLSX export can include Date of
birth; the documented list response does not return it. Imports retain source
hashes and before/after dates, preserving differing saved dates for review.

Once linked, changes to confirmed first/last names, email, phone, DOB and license
number/state/expiration automatically queue `datatruck_driver_updates` with the
driver save. Initial imports set `mserp.datatruck_import=on` transaction-locally
and establish baselines afterward, so they never bulk overwrite provider data.
No other driver fields or truck edits queue updates. Local saves finish
independently of vendor availability.

The existing five-second DataTruck worker sends PUT `/drivers/update/{id}/`.
The API requires account, phone, type, employee/dispatch status, main-driver flag,
carrier and the misspelled `brith_date`. Missing/null DOB was rejected in a
controlled October 22, 2026 request. The worker preserves the current provider
type/status and unchanged names/contacts, sends only changed optional license
fields, and never sends assignment objects. Missing DOB, invalid required
contacts or unavailable main-driver roles hold for review. The provider offers
no atomic revision precondition; required provider fields are read before PUT.

Each request and receipt is stored with append-only events. Mutations make one
HTTP attempt. A successful reply is followed by name/contact read-back;
DOB/license fields absent from that response retain the acceptance receipt
rather than a claim of independent verification. Unknown outcomes block later
updates for that driver and never automatically replay. A newer local edit can
supersede queued requests and definite rejections. Imports and rolled-back saves
never queue delivery.

New creations retain returned numeric IDs. Success-only creation receipts resolve
through later read-only, carrier-scoped unique name/contact checks. Failed and
uncertain creations are excluded, and POST is never retried. Edits made while
enrolling are queued once identity becomes available.

Truck/dispatcher assignment changes are outside the documented update contract.
A user-authorized GET-shaped `assigned_truck` trial returned 200 but changed
neither roster. Do not infer success from HTTP acceptance or add unverified
assignment fields. Employment lifecycle and dispatch availability also remain
outside this profile sync.

Validation adds driver/truck identity uniqueness, private DOB projection and
preservation, transactional update capture, import suppression, accepted baseline
read-back, and uncertain no-replay coverage to `TestRelayDriverDatabase`
(fresh/migrated schemas). `TestDriverUpdateNeverRetries` covers transport;
`frontend/scripts/test-driver-services-views.mjs` covers DOB and visible IDs.

The driver setup Services step includes DataTruck beside Relay and WeighMyTruck.
DataTruck's Add after saving choice starts selected for each manual or FleetScope
setup. Users can change or undo it. New truck forms, including the optional truck
inside driver setup, default Add to DataTruck after saving on. Editing an existing
truck does not enroll it. Existing profiles are never enrolled by deployment.

Driver service intents and truck enrollment requests commit with their native
creation transaction. Five-second backend workers call DataTruck only afterward.
A provider failure leaves the MSERP record saved and displays its enrollment
status in the saved-results modal and profile. Driver setup has the existing
versioned add/remind actions and a `datatruck_setup` system task, initially assigned
like driver onboarding. Truck profiles can retry definite failures through the
versioned `POST /trucks/{id}/datatruck`; GET on the same path returns status only.
Driver enrollment requires `drivers.create`; truck enrollment/retry requires
`trucks.create`. Status reads require the corresponding directory read permission.

## Configuration and API contract

Calls reuse the server-only `DATATRUCK_API_KEY` and `DATATRUCK_COMPANY_NAME` and
the same paced request gate as load imports. `DATATRUCK_MC_ID` is the tenant's
internal integer carrier ID, never a federal MC number or an inferred default.
An unset/invalid ID holds enrollment for review. Production MS Express Inc ID 1
was verified against the carrier shown in Chrome using driver 2966 and the
documented roster filter: `mc_number is 1` returned that record, whereas
`mc_number is 2` and `mc_number is 999999` returned none (October 21, 2026).

Official references reviewed:

- https://apidocs.datatruck.io/api-reference/openapi.json
- https://apidocs.datatruck.io/api-reference/endpoint/post_create_driver
- https://apidocs.datatruck.io/api-reference/endpoint/post_create_truck
- https://apidocs.datatruck.io/api-reference/endpoint/get_driver_list
- https://apidocs.datatruck.io/api-reference/endpoint/get_truck_list

POST `/api/v1/openapi/drivers/create/` sends reviewed first/last names, phone,
email/license fields when recorded, carrier ID, active employee status and
company/owner-operator driver type. Team membership determines `is_main` but
does not create upstream truck/crew assignments. Unassigned drivers remain in
review until a truck establishes their classification. Provider codes
`company_driver`, `owner_operator`, `active`, `available` were checked against
the live new-driver form; documentation examples use display labels instead.

POST `/api/v1/openapi/trucks/create/` sends unit, VIN, make, model, year, plate,
state, carrier ID, mileage, color and registration expiry when recorded. Required
missing details hold enrollment for review. Recorded company ownership maps to
`company`; explicit driver ownership maps to `owner_operator`; other investor
ownership maps to `other`. No lease, upstream owner ID, driver ID, ELD enrollment
or financial terms are inferred. The ownership codes were checked in Chrome.

## Receipts and duplicate protection

Migration 128 adds `datatruck_enrollments` plus append-only attempt snapshots in
`datatruck_enrollment_events`. Exact outgoing payload, tenant, send time and
successful receipt are committed server-side. Raw upstream error bodies never
reach the browser or logs. Roster preflights check driver phone, reviewed name and
email, or truck VIN and exact unit; any possible match blocks creation for review
without adopting an existing identity. A dedicated advisory lock serializes
enrollment preflights across workers. Pagination cannot forward credentials to
another origin.

Create calls make one HTTP attempt. No idempotency key is documented. Timeouts,
5xx, malformed success responses and interrupted sent requests become uncertain
and cannot be retried through the application. Check DataTruck and the stored
attempt before operator recovery; never reset a sent request simply to resend it.
Definite rejections and failures before sending can be retried explicitly.

Validation: `TestRelayDriverDatabase` exercises fresh/migrated schemas as
`mserp_app`, transactionally queued drivers/trucks, payload mapping, missing
prerequisites, duplicate detection and uncertain no-replay behavior using a fake
provider. `internal/datatruck/fleet_test.go` checks the HTTP boundary.
`node scripts/test-driver-services-views.mjs` checks both setup entrypoints,
default selection, opt-out, truck saved results, read-only views and mobile.
