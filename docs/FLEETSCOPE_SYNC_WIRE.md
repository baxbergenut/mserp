# MSERP → FleetScope v1: implemented wire contract

This document and the checked-in contract bundle supersede provisional route/payload suggestions in the planning documents. MSERP implementation is local only; the FleetScope receiver must pass joint tests before deployment. Existing FleetScope → MSERP hire and termination webhooks are unchanged.

## Shared files and handshake

Copy these exact files to FleetScope; preserve LF and UTF-8 without BOM:

- `backend/internal/fleetscope/protocol.json`: manifest and semantics. Handshake `contractHash` is lowercase SHA256 of these exact bytes.
- `backend/internal/fleetscope/wire.schema.json`: JSON Schema 2020-12 definitions. Manifest `wireSchemaSHA256` pins its exact bytes. Enforce formats and the semantic preconditions below as well as structural validation.
- `backend/internal/fleetscope/testdata/signature.json`: independent .NET-generated HMAC golden vector, with a public synthetic test secret.
- `backend/internal/fleetscope/testdata/operations.json`: request/response fixtures generated from the actual Go wire structs. These are synthetic examples, not production identities.

The Go source of the sender's wire types is `backend/internal/fleetscope/sync.go`. Both parties must update the bundle together; do not implement a second interpretation. UUIDs use canonical lowercase representation. Optional identifiers are omitted from operations, but unbound IDs in snapshots/results are empty strings. Nullable ownership/crew/projection fields are JSON null, not empty objects. Source revisions and assignment sequence must remain safe integers for browser JSON consumers (fail rather than wrap).

Every request uses the fixed configured HTTPS base URL ending `/integrations/mserp/v1` (a public `/api` prefix is allowed). All routes below are relative to that base. No redirect following. Maximum body/response is 4 MiB. Replies are JSON; no credentials, private documents, DOB, SSN or payroll information belong in this API.

Sign the **exact public request path and query**, including any proxy prefix, using the LF-separated components in `protocol.json`. GET signs empty bytes, snapshot POST signs `{}`. Each retry uses a new timestamp/nonce but the same immutable operation body and ID. Receiver verifies constant-time HMAC, configured principal/company/environment, timestamp within ±300 seconds, and an atomically consumed nonce with at least 600 seconds retention. Enforce the contract header against the expected manifest hash. Resolve tenant only from the authenticated machine principal; path/body IDs must never widen its scope.

Use independent live and bootstrap credentials, both different from the existing webhook secret. Bootstrap is an explicitly issued, revocable privilege; ordinary tenant administrators cannot issue it or enable another company. Rotate through receiver support for current/previous key IDs; each ID remains bound to one company, environment and privilege set. Keep secrets in server configuration only.

## Routes and responses

| Route | Request | Response |
| --- | --- | --- |
| GET `/capabilities` | Empty body; live credential | `Capabilities`: version 1, matching contract hash, ownership capability, company/environment, persistent epoch UUID, mode reconcile/live |
| POST `/snapshots` | `{}`; live credential, read-only scope | First `Snapshot` page |
| GET `/snapshots/{id}?cursor=...` | Empty body; live credential | Same immutable snapshot's next page |
| POST `/driver-links` | `driver.link` Operation | `Result` |
| POST `/truck-links` | `truck.link` Operation | `Result` |
| POST `/trucks/upsert` | `truck.upsert` Operation | `Result` |
| POST `/trucks/retire` | `truck.retire` Operation | `Result` |
| POST `/assignments/apply` | `assignments.apply` Operation | `Result` |

Snapshots preserve ID, company, environment, epoch and capturedAt across pages. Empty nextCursor ends pagination. Include all scoped trucks and drivers, including linked retired trucks and terminated drivers; omit sensitive identity data. Sender accepts at most 100 subsequent pages/10,000 total records and snapshots captured within 15 minutes (up to one minute positive clock skew). Reject duplicate IDs/bindings and crew references absent from the driver roster. Snapshot creation must not mutate fleet data. Retain immutable review snapshots for at least 24 hours or explicitly reject expired references.

Truck snapshots include actual current `projection` values for all MSERP-owned fields (null only before binding), ownership snapshot, truck revision and independent assignment revision. Do not return an integration's old stored payload while actual fields differ. Nonmanaged legacy ownership can be a legacy object/null until reviewed; bound trucks must return the exact Ownership contract. Revisions are opaque nonempty strings. Changing an unrelated insurance field must not change the managed-field revision; crew changes use the separate assignment revision. Safety holds remain independent but are checked by the receiver on every crew mutation.

The sender doesn't require an operation-status GET endpoint: replaying the original immutable POST is the recovery lookup. The receiver may add one for its operators, but POST replay must work even after bootstrap credential rotation (with a currently authorized key of the same privilege).

## Atomic operation semantics

Every Operation has version, operationId, companyId, environment, epoch, occurredAt, kind, sourceRevision and assignmentSequence. Nonassignment operations set assignmentSequence=0. Assignments set sourceRevision=0. occurredAt is the projection/review time, not a claim about historical employment, ownership or accounting events.

Authenticate and validate scope before looking up receipts. Store immutable body hash and the committed Result in the same transaction as the mutation, mapping, audit and sequence update. Same operation ID + bytes returns the original committed receipt with status duplicate; changed bytes returns 409 without mutation. A 409/422 must mean **this operation did not commit**; never use those codes for an uncertain commit. Keep committed receipts/tombstones durably through retirement and backup recovery. Check receipts before source-revision comparison so retries after later operations still recover their original result.

- **driver.link:** mserpId and fleetScopeId are required. A reviewed bootstrap link includes snapshotId/expectedRevision, verifies both identities are free or already identically bound, and never creates a driver. Live enrollment instead includes the original FleetScope hireEventId: verify it was emitted for exactly that company/driver and permit only the first matching binding. Preserve one-to-one identity tombstones. No matching by name/email. A reviewed link also allows future terminations to resolve that MSERP driver; old unmatched termination events are not replayed automatically.
- **truck.link:** bootstrap only; mserpId, fleetScopeId, snapshotId, expectedRevision and complete truck required. Check exact normalized VIN and unique identity. Apply the reviewed MSERP-owned fields/ownership atomically with binding. Initial sourceRevision=1.
- **truck.upsert:** complete truck required, truck.mserpTruckId=mserpId. Existing truck uses fleetScopeId and expectedRevision, and sourceRevision=last committed per truck+1. Source creation requires an unbound identity, provisioning `{color,truckClass,gvwrLbs}`, and no duplicate VIN/unit adoption. MSERP currently queues creation through reviewed bootstrap setup, because those required details are not stored in the local truck profile. Keep provisioning outside ownership; routine updates omit it. A reviewed replacement after rejection uses a new operation ID, current snapshot/revision and next uncommitted sourceRevision.
- **truck.retire:** require existing IDs/current managed-field revision and no crew. Preserve history and binding tombstone; do not delete insurance or historical safety data. Return the truck ID, new managed-field revision and current assignmentRevision. Include it in subsequent snapshots.
- **assignments.apply:** assignmentSequence is exactly the company/epoch's last committed sequence+1, initially 1. Each Crew specifies both truck IDs, expected assignment revision, nullable primary and secondary driver refs containing both systems' IDs. Secondary requires primary; same driver cannot occupy two slots. Validate all truck/driver identities, employment eligibility, active/safety restrictions and assignment revisions under database locks. Validate the full final graph before writing. Never evict a driver from a truck outside the list. Apply every listed crew atomically or reject all. Empty crew releases both slots. Trailers remain attached to their truck, including a trailer-only record if necessary. Return the same complete crews with **new** expectedRevision values.

MSERP v1 conservatively sends all enrolled trucks as one assignment component, including empty crews for deleted trucks. Missing linked dependencies block that assignment stream. Per-truck updates continue independently. Reject an oversized operation; do not silently split a swap into unsafe halves. This conservative behavior is deliberate and should be reviewed against actual fleet size before cutover.

Result always contains operationId, status applied/duplicate, companyId, environment, epoch, sourceRevision and assignmentSequence matching the request. Truck results require fleetScopeId, revision and assignmentRevision; driver results require fleetScopeId and revision, assignmentRevision="". Nonassignment crews=null. Assignment results use fleetScopeId/revision/assignmentRevision="" and populated crews. Receipts must reflect committed values, not merely accepted/queued requests; HTTP 202 acceptance is insufficient.

409/422 bodies use sanitized `{ "code": "revision_conflict" }`-style tokens. 401/403 block delivery. 429/5xx/network failures retry the same operation with bounded backoff and fresh signatures; numeric Retry-After up to 3600 seconds is honored. After 20 attempts, MSERP requires operator retry. Unknown/invalid success receipts are treated as uncertain and retried idempotently. Never supersede an uncertain outcome.

## Ownership and authority

Owner kind is company, driver or investor, with stable MSERP investor/owner UUID, display name, nullable MSERP owner-driver UUID and nullable known effective date. Only driver kind has an owner-driver UUID. A driver owner need not have a local FleetScope driver binding. Investor is never converted to lease. No investor ledger, account, bank details, revenue terms or payments are sent.

Owner changes preserve crew and insurance. Assignment changes preserve owner. Driver termination releases operational assignments according to the existing lifecycle flow and does not transfer title. Source operationalActive/status must not clear FleetScope safety holds. Distinct owner/compliance changes may require a FleetScope coverage review; preserve documents and do not invent coverage.

For the configured MS Express company, the receiver must enforce managed-field authority on **every** alternate writer, including ordinary API, imports, recruitment assignment, driver edits and direct repository paths. Switching to receiver live mode locks those fields before sender activation. Other companies retain existing behavior. Disable automatic release/assignment side effects that could conflict, or convert lifecycle/safety releases into explicit audited drift that MSERP pauses and reconciles. A safety emergency must remain possible; it must never silently become a second unrestricted assignment writer.

## Required joint acceptance

Run the actual sender and actual receiver against disposable databases with this exact bundle. Record both Git SHAs and contract hash. Prove company isolation, signing/proxy path, nonce replay, bootstrap revocation, existing hire enrollment, investor and driver-owner-without-local-driver cases, ownership-only edits, main/co-driver swaps, empty crew/deleted truck retirement, missing dependency rejection, stale revision rollback, concurrent ordinary edits, safety holds, lost response replay and restored-database epoch mismatch. Local mock tests do not establish cross-repository compatibility. Production activation is blocked operationally until this suite and human reconciliation are complete.
