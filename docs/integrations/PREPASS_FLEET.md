# PrePass fleet management

Tolls > Trucks, Transponders and Needs attention manage the PrePass truck roster and available transponders.
Trucks and Transponders have separate searchable top-level tabs. The single account is
selected internally; no account columns, filters or selection controls are shown.
A single Truck column links to the local profile when reviewed. Unlinked vehicles
are flagged. Needs attention contains unlinked toll groups and saved mappings.
Refresh PrePass loads both rosters for every active authorized account. Ordinary
page reads use the last complete snapshot; failed refreshes retain it.

## Supported actions

- Link an existing PrePass vehicle to an MSERP truck by explicitly reviewing the
  exact VIN and selecting a toll linking start date. Preview then save creates
  the roster link and dated toll rules atomically. Unit names are not guessed;
  leading zeros and punctuation are retained.
- Add an active MSERP truck to PrePass and record its reviewed link. Select the
  truck once: its saved VIN, unit and plate details appear in a compact summary.
  Only missing or unsupported unit/plate fields appear as inputs. Weight and
  IRP/IFTA numbers/states are requested because truck profiles do not currently
  store them; these are never copied from an unrelated vehicle. Saved fields
  remain editable under Review or change other details, alongside optional VRN,
  temporary-plate and hazmat settings. A single authorized account is selected
  automatically. Changing trucks resets the form.
- A truck without a saved VIN can supply one for this enrollment. This does not
  modify the local profile, and the VIN must not belong to another local truck.
  A nonempty saved VIN must match; malformed saved VINs require profile correction.
  A VIN already in PrePass cannot be added. Supplied missing fields go to PrePass
  only; later fleet refreshes show the confirmed provider details.
- Edit the provider's unit, plate, weight, registration, VRN and hazmat fields.
  VIN is immutable. Unexposed color/lease fields are omitted from PATCH.
- Resolve a linked truck's plate discrepancy from Trucks > Resolve plate. Review
  both plate/state pairs and choose MSERP or PrePass to save that pair to both
  systems. Choosing PrePass copies its freshly read plate/state into MSERP only,
  requires `trucks.edit`, and never sends a provider write. Choosing MSERP
  additionally requires `tolls.fleet.edit`, and sends a permanent plate to
  PrePass. Unsupported MSERP plate
  details must be corrected first. Both paths require `tolls.read`, a current
  matching VIN and a reviewed roster link.
  The API uses an `edit` change with `plateSource=mserp|prepass`, `truckId` and
  `truckUpdatedAt`. Values come from the server's version-checked records; the
  provider PATCH for the MSERP source contains only `licensePlate`. The local row
  is locked through the update (and provider read-back when writing PrePass), then
  its plate, cached roster and confirmed receipt commit together. Receipts retain
  the prior local plate and selected source. A failed
  or unconfirmed provider result never confirms a local change. Portal review
  of an uncertain result only acknowledges it; review the refreshed discrepancy
  and resolve again if needed. It does not silently overwrite the local profile.
- Remove a vehicle from PrePass. Its local truck, reviewed link, operation
  history and tolls are retained. Physical transponder returns remain separate.
- Assign an available transponder to a truck that has none. It must belong to
  the same account, have no VIN/unit assignment, and appear on no vehicle in
  any account. Availability is checked again immediately before the write.

The operator confirmed this fleet has no trucks enrolled in the Toll Violation
Prevention Program (VPP), which the fleet API excludes. No per-operation checkbox
is required. The legacy `fleetServiceConfirmed` request field is accepted only
for compatibility with older clients and immutable receipt hashes.
Trailer plates, device replacement/transfer/unassignment, lost/stolen reporting,
device orders, account transfers and automatic truck offboarding are outside
this first version. No unsupported activation/deactivation state is invented.

Migration 083 connects fleet links to dated toll mappings. Linking requires an
inclusive travel start date and previews all eligible unlinked tolls from that
date, regardless of posting date. Zero existing tolls is valid for future imports.
The reviewed current fleet transponderNumber maps to transaction ppDeviceId/tag;
exact vehicleNumber is a fallback when no reviewed tag rule matches. Rules are
account/environment scoped. Existing linked transactions are always preserved.
The operation refuses stale previews, conflicting reviewed mappings, duplicate
provider unit/tag identities and changes affecting finalized statements. Relinking
closes only this fleet vehicle's previous rules at the chosen start date, without
unlinking any of their existing transactions. Events record retired rules and the
exact backfilled transaction IDs; the durable receipt records the selected date.

Previously linked or newly added vehicles without dated rules show Link tolls.
Provider unit/tag changes also flag missing current rules for dated review. No
start date is invented by a roster refresh, Add, Edit or Assign. Fleet linking
never changes truck aliases. Inactive local trucks and VIN/plate mismatches remain
attention warnings. Historical identities absent from the current provider roster
can still be handled under Tolls > Needs attention.

Transaction fields include ppDeviceId (PrePass main device ID), deviceNumber
(transponder/sticker number), vehicleNumber and plateNumber/plateState, but no
VIN. deviceNumber is distinct from ppDeviceId and must not be equated with the
fleet transponderNumber. Read-only production samples confirmed fleet transponder
numbers match transaction ppDeviceId; deviceNumber values did not match.

## API, permissions and configuration

| Route | Purpose | Permission |
| --- | --- | --- |
| `GET /prepass-fleet` | Cached rosters, limited local truck choices, links and latest 50 operations | `tolls.read` |
| `POST /prepass-fleet/refresh` | Refresh complete provider rosters | `tolls.read` |
| `POST /prepass-fleet/link/preview` | Preview dated unlinked tolls | `tolls.link` + `tolls.read` |
| `POST /prepass-fleet/link` | Save reviewed roster and dated toll links | `tolls.link` + `tolls.read` |
| `POST /prepass-fleet/change` | Add/edit/remove/assign | `tolls.fleet.write` + `tolls.read` |
| `POST /prepass-fleet/operations/{id}/verify` | Record explicit portal review of an uncertain result | `tolls.fleet.write` |

The code-owned Administrator role receives the new permission. Existing custom
roles need it explicitly enabled in Settings; no migration expands their access.
Truck profile navigation additionally requires `fleet.read`.

Migrations `082_prepass_fleet.sql` and `083_prepass_fleet_toll_links.sql` must
precede the new API. Fresh installations
include the same tables in `init.sql`. Reuse the existing server-only
`PREPASS_ENVIRONMENT` and selected production/nonproduction client ID/secret.
No credentials reach the browser. Rosters, links and receipts are scoped by
environment. With scheduled syncs enabled, the complete fleet roster refreshes daily
at SCHEDULED_FUEL_SYNC_TIME in SCHEDULED_SYNCS_TIMEZONE alongside fuel and
WeighMyTruck. Failed refreshes preserve the last complete roster.
Available transponders can be assigned from either Trucks or Transponders; both
use the same versioned provider operation. Select the truck/device and press
Assign transponder once. The form shows Assigning while submitting; accepted
assignments show an automatic processing indicator until the provider finishes.

## Concurrency and recovery

Refresh and write operations acquire a PostgreSQL advisory lock per environment,
including across multiple API processes. Mutations re-read the complete provider
roster and compare the displayed vehicle/link revision. Local add/link selection
also requires the truck's current timestamp and matching VIN when present.
Only Add may supply an absent VIN; linking requires an exact VIN match or the
same already-reviewed fleet link when the local VIN is still missing. Add holds a shared
truck lock through the provider operation so its local identity cannot change.

Each request has a UUID and a durable, actor-bound payload receipt committed
before the external write. An identical confirmed retry returns success; a
changed payload or unresolved UUID cannot replay. A definitive provider rejection
is recorded as failed. Timeouts, server failures, failed read-back or an unexpected
read-back result leave the request unconfirmed and block further changes for
that environment. Confirmed writes require a fresh expected-state read-back.

Fleet v2 assignment success means the vehicle update was added to a processing
queue (observed live October 19, 2026), rather than applied immediately. Migration
120 adds `queued` receipts and unique truck/device reservations. An accepted
assignment releases the global fence, while further changes to its truck or
device remain blocked. The API checks the roster once immediately, then a
startup/15-second server worker reads rosters only while assignments are waiting.
It never resends an assignment. Both vehicle and transponder rosters must show
the exact account/VIN/device, without another vehicle/device conflict, before
the receipt and cached roster confirm atomically. Transient read failures retain
the queue. Old uncertain assignments can also confirm automatically when both
rosters agree. The browser polls cached status every five visible seconds while
waiting; no Review result or acknowledgment is required for assignments. Provider
processing time is outside MSERP's control. Outstanding assignments are always
returned alongside recent history, even when older than the latest 50 changes.
Do not roll back to code without queue reservations while queued receipts exist.

For other unconfirmed operations, inspect the outcome in the PrePass portal, wait at
least two minutes from request creation, then use Review result and acknowledge
the outcome. This refreshes the roster and records the reviewer without resending
the request. Review the refreshed data before making another action; an uncertain
add may require linking the resulting vehicle manually. A routine refresh alone
never resolves those uncertain writes. Refresh can confirm an assignment when
both rosters agree.

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

Database tests exercise fresh and incrementally migrated schemas as `mserp_app`, reviewed
links, stale revisions, duplicate submissions, availability races, environment
isolation, failed refresh retention, uncertain recovery and local data retention
after provider removal. Dated-link tests cover tag precedence, exact-unit fallback,
account/date isolation, unchanged existing links, stale previews, finalized
statements, relinking, future imports and audit. Browser checks cover both rosters, all supported actions,
permissions, draft retention, recovery and desktop/mobile layouts.
