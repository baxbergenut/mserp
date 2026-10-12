# Driver service setup

Migration 079 adds Relay and WeighMyTruck enrollment requests to manual driver
creation and FleetScope completion. Both forms share draft service cards.
`Add after saving` and `Remind me later` make no vendor calls before the driver
transaction commits. The create/completion request carries an optional `services`
array of `{service: relay|weighmytruck, action: add|defer, followUpOn?: YYYY-MM-DD}`.
The existing `addWeighMyTruck` completion flag remains for older clients.

Driver/contact validation is unchanged. Vendor configuration, missing enrollment
prerequisites and vendor failures cannot roll back a committed driver setup.
The results modal can be closed while work continues. Driver Overview contains
the same saved service cards; Tasks links to `/drivers/detail?id=…&services=1`.

## Requests and provider boundaries

`driver_service_requests` has a unique driver/service key, optimistic versions,
and queued, running, review, deferred, done and canceled states. Creation stores
the request in the same transaction as the driver/intake completion. The server
processes queued requests at startup and every five seconds. Deferred requests
never enroll automatically; review requests never retry automatically. Abandoned
running requests become review after ten minutes. Termination cancels unfinished
setup, and the existing offboarding workers remain authoritative.

The worker reuses the existing vendor mutation receipts and membership versions.
An uncertain Relay operation or WeighMyTruck membership blocks another enrollment
even after a reminder is set. Explicit website review uses the existing guarded
verification APIs; it is not a generic Retry operation. Errors shown to users use
vetted messages, never raw provider bodies. A previous-carrier conflict is offered
as conditional troubleshooting, not claimed as a confirmed cause.

Relay accounts are created without a policy. Existing accounts are never linked
from a name/phone match alone; an authorized Relay review assignee explicitly
confirms the link. The saved card offers the three supported policies and enables
an existing disabled policy without resetting its usage. Assigning another policy
warns about resetting counters. Non ELD PIN/truck inputs are submitted directly
through the existing policy API after driver save. PINs are never queued, stored
in service requests, returned, logged or persisted in browser storage. ELD
policies use the existing provider connection; the UI does not create an ELD link.
Account-added/policy-needed and policy-enabled are distinct statuses.

WeighMyTruck uses the existing initialized roster, explicit enrollment receipts,
email uniqueness checks and manual verification boundary. There is no upstream
roster read API; Added means membership last confirmed in MSERP.

## Tasks and permissions

Every requested service creates one durable system task. Deferred tasks appear on
their chosen New York follow-up date (tomorrow by default). Failures retain that
same task. Confirmed WeighMyTruck membership or a confirmed Relay account with an
enabled policy completes it. Manual fixes are reconciled on the next worker run.
Task history remains on driver deletion. Request transitions are recorded in
`driver_service_events`.

The `relay_setup` and `weighmytruck_setup` category assignments initially copy
the driver-onboarding assignees. Administrators can change them in Settings >
System tasks; existing task assignment privacy applies. Tasks cannot be completed
with the generic task status controls. A reminder updates the same task.

`GET /driver-services/{id}` requires fleet.read and returns only services the user
can read or manage. `POST /driver-services/{id}` accepts a versioned add/defer
intent and additionally requires fuel.drivers.write or weighmytruck.write.
Creation/onboarding validates each requested service's write permission. Existing
Relay linking still requires its review assignment, tasks.write and fuel.read.

## Validation

`MSERP_RELAY_TEST_DATABASE_URL` enables the disposable local fresh/migrated database
tests in `TestRelayDriverDatabase`, including atomic creation and intake completion,
non-blocking vendor prerequisites, reminders, task links/dates, duplicate guards,
uncertain outcomes, manual verification and termination cancellation. Tests run as
mserp_app and use fake providers. Normal tests do not call live vendor APIs.

Build the frontend with `NEXT_PUBLIC_API_URL=/api`, then run
`node scripts/test-driver-services-views.mjs` from frontend for the two modals,
result states, policy setup, uncertain-result review, reminders, linking, task
resumption, read-only permissions and mobile layout against local API fixtures.
