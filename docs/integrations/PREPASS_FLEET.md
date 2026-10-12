# PrePass fleet management

Tolls > PrePass manages the PrePass truck roster and available transponders.
Trucks and Transponders have separate searchable views with account filters.
Refresh PrePass loads both rosters for every active authorized account. Ordinary
page reads use the last complete snapshot; failed refreshes retain it.

## Supported actions

- Link an existing PrePass vehicle to an MSERP truck by explicitly reviewing the
  exact VIN. Unit names are not guessed or normalized; leading zeros are retained.
- Add an active MSERP truck to PrePass and record its reviewed link. Select the
  truck once: its saved VIN, unit and plate details appear in a compact summary.
  Only missing or unsupported unit/plate fields appear as inputs. Weight and
  IRP/IFTA numbers/states are requested because truck profiles do not currently
  store them; these are never copied from an unrelated vehicle. Saved fields
  remain editable under Review or change other details, alongside optional VRN,
  temporary-plate and hazmat settings. A single authorized account is selected
  automatically. Changing trucks resets the form and service confirmation.
- A truck without a saved VIN can supply one for this enrollment. This does not
  modify the local profile, and the VIN must not belong to another local truck.
  A nonempty saved VIN must match; malformed saved VINs require profile correction.
  A VIN already in PrePass cannot be added. Supplied missing fields go to PrePass
  only; later fleet refreshes show the confirmed provider details.
- Edit the provider's unit, plate, weight, registration, VRN and hazmat fields.
  VIN is immutable. Unexposed color/lease fields are omitted from PATCH.
- Remove a vehicle from PrePass. Its local truck, reviewed link, operation
  history and tolls are retained. Physical transponder returns remain separate.
- Assign an available transponder to a truck that has none. It must belong to
  the same account, have no VIN/unit assignment, and appear on no vehicle in
  any account. Availability is checked again immediately before the write.

Provider operations require confirmation that the truck is not enrolled in the
Toll Violation Prevention Program (VPP), which the documented fleet API excludes.
Trailer plates, device replacement/transfer/unassignment, lost/stolen reporting,
device orders, account transfers and automatic truck offboarding are outside
this first version. No unsupported activation/deactivation state is invented.

Fleet links are separate from Tolls > Truck linking. They do not rewrite dated
toll mappings, truck aliases, imported transactions or finalized statements.
Inactive local trucks and VIN/plate mismatches appear as attention warnings.

## API, permissions and configuration

| Route | Purpose | Permission |
| --- | --- | --- |
| `GET /prepass-fleet` | Cached rosters, limited local truck choices, links and latest 50 operations | `tolls.read` |
| `POST /prepass-fleet/refresh` | Refresh complete provider rosters | `tolls.read` |
| `POST /prepass-fleet/link` | Review the local truck link | `tolls.link` + `tolls.read` |
| `POST /prepass-fleet/change` | Add/edit/remove/assign | `tolls.fleet.write` + `tolls.read` |
| `POST /prepass-fleet/operations/{id}/verify` | Record explicit portal review of an uncertain result | `tolls.fleet.write` |

The code-owned Administrator role receives the new permission. Existing custom
roles need it explicitly enabled in Settings; no migration expands their access.
Truck profile navigation additionally requires `fleet.read`.

Migration `082_prepass_fleet.sql` must precede the new API. Fresh installations
include the same tables in `init.sql`. Reuse the existing server-only
`PREPASS_ENVIRONMENT` and selected production/nonproduction client ID/secret.
No credentials reach the browser. Rosters, links and receipts are scoped by
environment; there is no scheduled fleet sync in this version.

## Concurrency and recovery

Refresh and write operations acquire a PostgreSQL advisory lock per environment,
including across multiple API processes. Mutations re-read the complete provider
roster and compare the displayed vehicle/link revision. Local add/link selection
also requires the truck's current timestamp and matching VIN when present.
Only Add may supply an absent VIN; existing-vehicle linking still requires an
exact VIN match. Add holds a shared
truck lock through the provider operation so its local identity cannot change.

Each request has a UUID and a durable, actor-bound payload receipt committed
before the external write. An identical confirmed retry returns success; a
changed payload or unresolved UUID cannot replay. A definitive provider rejection
is recorded as failed. Timeouts, server failures, failed read-back or an unexpected
read-back result leave the request unconfirmed and block further changes for
that environment. Confirmed writes require a fresh expected-state read-back.

For an unconfirmed request, inspect its outcome in the PrePass portal, wait at
least two minutes from request creation, then use Review result and acknowledge
the outcome. This refreshes the roster and records the reviewer without resending
the request. Review the refreshed data before making another action; an uncertain
add may require linking the resulting vehicle manually. A routine refresh alone
never resolves an uncertain write.

The vendor has no documented conditional-write or idempotency primitive. Portal
edits between MSERP's preflight read and write cannot be made atomic. Receipts
prevent MSERP from blindly repeating an operation after an uncertain response.

## Vendor contract and validation

Inspected October 12, 2026:
[PrePass Fleet Management v2](https://developer.prepass.com/api-details#api=prepassapim-fleetmgmt-v2).
The public OpenAPI export is retained in `prepass-fleet-v2.openapi.json`.

Active account discovery reuses `/accounts/v1/accounts`; Fleet v2's deprecated
accounts operation is not used. Vehicle/transponder reads use
`/fleetmanagement/v2/vehicles` and `/transponders` with `accountNumber`.
Vehicle add/edit/remove use POST/PATCH/DELETE `/fleetmanagement/v2/vehicle`.
Existing vehicles are addressed by `vin` and `accountNumber`; transponder
assignment PATCH sends only `transponderNumber`.

Read-only production discovery confirmed access to the account and both roster
endpoints. No live vendor mutations were made during development. Contract tests
use HTTP fixtures, and database/browser tests use isolated local data.

Validation commands:

```powershell
# backend: disposable local database name must contain _test
$env:MSERP_PREPASS_TEST_DATABASE_URL = 'postgres://...@127.0.0.1:5432/mserp_test?sslmode=disable'
go test ./internal/prepass ./internal/httpapi ./internal/repository
go vet ./...

# frontend: browser fixtures never contact PrePass or production MSERP
$env:NEXT_PUBLIC_API_URL = '/api'
npm run build
node scripts/test-prepass-views.mjs
```

Database tests exercise fresh and migration-082 schemas as `mserp_app`, reviewed
links, stale revisions, duplicate submissions, availability races, environment
isolation, failed refresh retention, uncertain recovery and local data retention
after provider removal. Browser checks cover both rosters, all supported actions,
permissions, draft retention, recovery and desktop/mobile layouts.
