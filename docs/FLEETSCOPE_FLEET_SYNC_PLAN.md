# MSERP and FleetScope truck and assignment integration plan

Design based on the local source on October 15, 2026. MSERP's sender, review UI, durable receipts and migration 103 are now implemented locally, default off. The exact contract is [FLEETSCOPE_SYNC_WIRE.md](FLEETSCOPE_SYNC_WIRE.md) with its pinned schema/fixtures; it supersedes provisional API suggestions below. [FLEETSCOPE_SYNC_RUNBOOK.md](FLEETSCOPE_SYNC_RUNBOOK.md) describes implemented rollout/recovery. V1 uses a conservative whole-enrolled-fleet assignment component, reviewed truck provisioning and original POST replay for receipt recovery. The FleetScope receiver and real cross-repository acceptance remain outstanding; no production reconciliation or activation has occurred. The companion [FleetScope agent prompt](FLEETSCOPE_FLEET_SYNC_AGENT_PROMPT.md) defines the work for the second repository.

The [shared ownership contract](FLEETSCOPE_OWNERSHIP_CONTRACT.md) specifies the common company/driver/investor representation and compatibility release gate. It is required input for both agents. FleetScope receives a read-only owner snapshot; investor accounting stays in MSERP.

MSERP should own the operational truck directory and driver–truck assignments for MS Express. FleetScope should receive those records through a narrow server API, while continuing to send its existing hire and termination notifications to MSERP. Start with a reviewed reconciliation of existing identities and assignments; enable continuous synchronization only after that baseline is verified.

## Current implementation and implications

| Source | Current behavior | Consequence for the integration |
| --- | --- | --- |
| MSERP `backend/internal/fleetscope/webhook.go`, `repository/fleetscope_repository.go`, `repository/fleetscope_termination.go` | Signed hire/termination events, pinned company UUID, immutable receipts, explicit intake completion/linking | Reuse the identity evidence and delivery principles. There is no existing outbound truck sync. |
| MSERP `repository/fleet_repository.go` | Truck CRUD and transactional assignment/release; an assignment can displace an existing driver and release another truck | Capture the complete change, including displaced relationships, in the same transaction. |
| MSERP `repository/driver_teams.go` | Team membership is dated separately from the main driver's truck assignment | Build the outbound crew from both sources; two separate solo assignments would be wrong. |
| FleetScope `api/internal/database/assignment/assignment.repo.go` | `fleet_assignments` supports primary/co-driver and two trailers; assignment changes can activate/deactivate drivers and infer owner-operator ownership | Implement a dedicated integration transaction. Ordinary assignment methods cannot be reused unchanged. |
| FleetScope `api/internal/database/vehicle/vehicle.repo.go` | Ownership edits can assign the owner as driver; non-active vehicle status releases assignments | Guard indirect assignment writes as well as the Assignments screen. |
| FleetScope `api/internal/database/applicant/applicant.repo.go` | Hiring can call `AssignAtHire` | For the enabled company, hire without assigning a truck; accounting assigns in MSERP after setup. |
| FleetScope `api/internal/database/vehicle/validator.go`, `sql/vehicle.sql` | Vehicle creation requires fields MSERP does not collect; VIN/year and ownership also have storage constraints | Preflight missing fields. Do not manufacture VINs, years, ownership, color, class or GVWR. |
| FleetScope `api/internal/database/mserpoutbox/` | Durable, company-scoped hire/termination sender | Preserve its existing v1 contract and independent credentials. |

FleetScope is two Git repositories at `D:\Projects\fleetscope\api` and `D:\Projects\fleetscope\web`. Both have existing local changes; implementation must preserve them. No production inventory was queried for this plan, so actual duplicate/missing-record counts remain an output of reconciliation.

## Ownership of information

| Information | Authority and proposed behavior |
| --- | --- |
| Recruiting, driver identity and compliance | FleetScope retains its records and existing hire/termination handoff. MSERP setup and explicit identity review remain required. |
| Truck operational identity | MSERP owns UUID, unit number, VIN, year, make/model and plate/state once a truck is linked and enrolled. FleetScope stores the MSERP UUID as an external identity. |
| Driver–truck assignment and main/co-driver roles | MSERP owns current operational relationships. FleetScope displays the resulting crew. |
| Payroll, ownership ledgers and financial effective dates | Stay in MSERP. An assignment never changes truck ownership. |
| Truck ownership identity | MSERP owns the recorded owner. After reviewed baseline adoption, FleetScope reflects company/driver/investor ownership plus the owner's MSERP ID and name using the shared ownership contract. Assignment changes never change this owner. |
| FleetScope color, class, GVWR, insurance, compliance documents, trailers and trailer attachments | Stay in FleetScope. Initial provisioning may collect missing required values through a reviewed setup step. Routine truck updates omit these fields. |
| Employment and safety status | Preserve FleetScope lifecycle/compliance controls. A received assignment cannot undo termination, vacation status or a safety hold. FleetScope's `inactive` is not equivalent to MSERP's terminated state. |

For the enabled company, FleetScope truck creation and edits to MSERP-owned fields should direct staff to MSERP. Existing unenrolled records remain visible for reconciliation, with their managed fields locked after the company cutover. Other companies retain their current behavior.

FleetScope needs real `investor` support in storage/validation/display, but no investor directory or financial features. Its current frontend normalization of investor to lease must be removed for managed trucks. Ownership changes preserve crew, policy records and documents; any coverage review is a separate FleetScope action. Driver-owned trucks can retain their external owner identity even if the owner has no FleetScope driver link. See the shared contract for schema constraints, payloads and tests.

Include every newly created MSERP truck in delivery tracking, even if its remote creation is waiting for required information. For the initial baseline, inventory active and inactive trucks; require all current assignments to be resolved, and explicitly record any historical/inactive exclusions. A truck's local `assigned`/`available` label must not be copied into FleetScope's different status enum. Keep an integration operational-active flag separate from FleetScope safety status: retirement/deactivation blocks use, while reactivation never clears a FleetScope hold. MSERP maintenance/out-of-service changes should block new assignment delivery until the operational conflict is resolved; they do not certify FleetScope compliance.

Keep trailers editable in FleetScope through a dedicated equipment operation that cannot change driver/truck identity. Preserve trailers on their existing truck when its crew changes; trailers must not follow a driver to another truck. When the last driver is removed, retain a trailer-only row if needed by FleetScope's schema.

Start with current-state synchronization and an audit trail from cutover onward. Do not copy or rewrite historical assignments or financial records as part of the baseline. Store MSERP's accounting effective week separately from the time FleetScope observes/applies a change. Backdated accounting corrections must not backdate FleetScope's existing safety history. A change that affects only historical periods produces no current assignment change. Dated team changes require a scheduled boundary check even when no one edits a form that day.

## Identity links and reconciliation data

Use UUID links, never names or unit numbers, for live writes:

- Driver link: environment + FleetScope company UUID + FleetScope driver UUID ↔ MSERP driver UUID.
- Truck link: environment + FleetScope company UUID + FleetScope vehicle UUID ↔ MSERP truck UUID.
- Keep approved links one-to-one within that scope, with reviewer, timestamp, evidence, source versions and a link revision. Replacing a link requires a separate audited correction.
- Keep retired identities/tombstones after deletion; never reuse an old ID or automatically attach a replacement truck with the same unit number.

Introduce a general driver-link table in MSERP. Seed it from valid, completed intake links, checking for conflicting links first. Do not invent historical hire events to link older drivers. Intake completion should maintain the same link transactionally. Termination lookup must use the approved general link, with backward-compatible behavior for existing intake links.

For future hires, MSERP intake completion must also enqueue a narrowly scoped driver-binding operation with the original FleetScope hire event/driver identity. FleetScope validates that it issued that event for the pinned company and that neither identity is already bound elsewhere. The live credential may establish this first binding, but cannot relink existing identities or bind arbitrary legacy drivers. Assignments wait for the binding acknowledgment. Legacy matching and link corrections require the separate reconciliation privilege; revoking the bootstrap credential must not break ordinary new-driver onboarding.

The legacy-link review must explicitly say that confirming a link also authorizes future FleetScope termination notifications to affect that MSERP driver. If a termination was already received before a legacy link existed, show a separate offboarding review; linking alone must not replay an old termination or silently deactivate someone. Preserve existing receipt deduplication. Rehires need an explicit lifecycle review because the current v1 intake/termination identity deduplication does not implement repeated employment cycles.

Suggested MSERP tables: driver/truck links, provisioning supplements, reconciliation runs/items, outgoing operations, operation dependencies, per-truck source revisions and a serialized assignment stream. Suggested FleetScope tables: company integration mode/epoch, external links, receipt journal, source revisions, reconciliation snapshots and independent safety holds. Final names should follow each repository's conventions.

## Initial synchronization without conflicts

1. **Deploy disabled.** Install migrations, API routes, receipts, operator views and tests. Keep the new sender and receiver in `off` mode. Existing hire/termination delivery continues.
2. **Verify the company.** An authorized operator verifies the actual MS Express company UUID against its name and USDOT. Pin that UUID server-side in both applications. If the existing webhook company is configured, the new integration must match it. No company-name substring matching or browser-selected company can authorize writes.
3. **Take read-only snapshots.** Export only the pinned company's drivers, vehicles, current crews, relevant statuses and existing external links. Capture an MSERP snapshot of trucks, current assignments, dated teams and completed intake links. Use snapshot IDs, source revisions, capture times and full pagination. A snapshot must be materialized or consistently read; paginating a changing live table is not a consistent baseline.
4. **Build a reconciliation report.** Classify every in-scope row as already linked, proposed link, create, missing required information, conflicting identity, assignment mismatch, or intentionally excluded. Show before/after values and all affected drivers/trucks. Do not include payroll, SSNs, birth dates or documents in exports.
5. **Approve identities.** Existing explicit intake links are strongest evidence. Driver names/email/phone or CDL evidence may suggest candidates but never authorize automatic merges. A unique exact normalized VIN can propose a truck match; require review if the records disagree. Unit number alone is a suggestion, preserving leading zeros. Same unit/different VIN, duplicate VIN, reused plates and ambiguous driver matches block automatic linking.
6. **Resolve missing records.** For an MSERP-only truck, create the FleetScope vehicle only after required vehicle data and a valid MSERP owner snapshot are supplied. Review existing ownership discrepancies before adopting that snapshot. A missing owner-driver link does not require a fake driver; actual assigned drivers still require confirmed links. For an MSERP-only assigned driver, find/link or properly onboard that person in FleetScope; do not create a hollow compliance driver from an assignment event. FleetScope-only vehicles/drivers are explicitly linked, reviewed for normal MSERP onboarding, or excluded. Never delete or deactivate a record simply because it is absent from the other snapshot.
7. **Review assignment differences.** MSERP is the proposed winning source, but show displaced drivers, co-drivers, occupied trucks and FleetScope safety/termination blockers before applying. If FleetScope is actually correct, correct MSERP through its normal versioned workflow first. Do not modify assignments by SQL to bypass financial/history guards.
8. **Establish a cutover barrier.** During final reconciliation, briefly freeze MS Express truck identity and driver/truck assignment edits in both systems, including indirect writers and imports. FleetScope lifecycle/safety actions remain available; they invalidate the relevant preview and force a fresh check. Record an integration epoch and MSERP stream watermark; verify all source/target revisions again.
9. **Apply in dependency order.** Persist/confirm driver links → link or create trucks → confirm actual FleetScope vehicle IDs and readiness → apply reviewed assignment components. Each component includes every affected old/new truck and displaced driver. Replacements/swaps commit atomically, with target preconditions. A failed component changes nothing. Components sharing a driver/truck cannot be applied independently.
10. **Verify and activate.** Read back IDs, vehicle data and complete crews. Require every in-scope assignment to have confirmed driver/truck links and acknowledged vehicle provisioning. Record intentional exclusions and their blocked dependent assignments. Activate the same epoch on both sides, release the MSERP freeze, keep FleetScope managed fields locked, and deliver only changes after the baseline watermark. Queued older operations must be explicitly covered by the baseline; do not replay them over it.

For example, if MSERP assigns driver D to new truck T while FleetScope has no T, MSERP saves the local transaction and queues both operations. T is provisioned first. Its committed response supplies the FleetScope vehicle UUID. Only then can the assignment execute. If T lacks a valid VIN or required setup information, the assignment stays `Waiting for truck setup`; FleetScope's previous relationship remains intact and MSERP shows that it is not yet synchronized. The receiver independently rejects a missing truck even if the sender makes a mistake.

## Continuous delivery design

Use a **transactional outbox**: the same PostgreSQL transaction that saves the MSERP truck/assignment writes its outgoing operation. A background worker sends committed operations over HTTPS. A temporary FleetScope outage does not undo a successful MSERP edit; the UI reports synchronization pending.

Capture all supported mutation paths, not just the truck form: manual creation, guided driver setup with a new truck, FleetScope intake completion, Five ELD truck tasks, profile assignments, displacement/release, driver status changes, truck deactivation/deletion, team edits and team date boundaries, and incoming FleetScope termination. Audit triggers and other direct SQL writers. New-load refresh must continue preserving MSERP assignments; the current `syncTruckAssignment` helper has no production caller in the inspected checkout and must not be reintroduced as an authority.

Also capture ownership transfers and effective-date corrections, investor-name changes and driver-name changes that alter owner display names. Update the affected trucks' owner snapshots under new source revisions, even if those changes do not touch `trucks.updated_at`. Ownership synchronization never emits an implied assignment.

Store a durable dirty marker/revision where projection must wait until the final transaction state. A worker may materialize immutable operations from committed desired state, but must retain enough affected IDs to release old relationships and survive deletion. Do not enqueue the temporary half-state between release and reassignment.

Use per-truck monotonic revisions for truck fields. Serialize assignment operations with a company-scoped stream sequence and lock, because assignments connect multiple trucks and drivers. A command describes the complete desired crew for the affected component, including empty crews for releases, expected target assignment revisions and required truck/link revisions. Close affected rows and create their replacements within one FleetScope transaction. Lock all involved records in a deterministic order and enforce one open truck/crew plus no driver in multiple primary/co-driver slots under concurrency.

Do not skip a failed assignment sequence and apply later ones blindly. Unrelated truck provisioning can continue. Operator recovery may explicitly supersede blocked commands with a newly reviewed current-state command and an auditable sequence transition. This is a deliberate initial simplicity tradeoff for a single company; monitor head-of-line delays.

FleetScope must commit mutation, external link/revision and operation receipt atomically. A lost HTTP response is safe: replay the same operation ID with identical bytes and return its original committed result. Same ID/different bytes is a conflict. A delayed old revision cannot overwrite a newer state. Retain receipt identities/hashes and revision tombstones through retry, archival, backup and restore; deleting the deduplication record must not make an old request executable again.

Worker behavior:

- Use leases, fencing tokens, bounded concurrency and request timeouts; recover abandoned leases after crashes.
- Retry network failures, 429 and transient 5xx with exponential backoff/jitter and `Retry-After` where supplied. Retry the same bytes and operation ID with a fresh authentication timestamp/nonce.
- On timeout, query the operation receipt or replay idempotently. Never create a replacement operation merely because the response was lost.
- Stop and surface identity, validation, stale-preview and safety conflicts for review. Pause the company stream on authentication/company/epoch mismatch.
- After a bounded retry horizon, show an actionable failure and retain the operation for safe recovery; never discard it.
- Periodically compare linked current state/revisions and alert on drift, missing counterparts and stuck dependencies. A fresh snapshot/diff is required after a restore. Drift repair uses the same reviewed command path and cannot overwrite a safety hold.

Recommended operational targets: wake the worker after commit with a five-second polling fallback; show a delay warning after five minutes; run incremental checks periodically and a full read-only comparison daily. These are proposed targets, not guarantees measured from the deployed systems.

## Proposed FleetScope API contract

Put the new routes in a dedicated machine-authenticated namespace, for example `/integrations/mserp/v1`. Verify the actual deployed prefix/proxy rewriting before configuring URLs. Do not borrow browser login cookies or expose the normal multi-tenant CRUD API to this integration.

| Route | Purpose |
| --- | --- |
| `GET /capabilities` | Pinned company, environment, protocol version, shared contract bundle hash, ownership capability, epoch, mode and supported features; requires authentication. |
| `POST /snapshots` and `GET /snapshots/{id}?cursor=...` | Create/read a bounded, consistent, company-scoped reconciliation snapshot. |
| `POST /driver-links` | Establish a reviewed legacy mapping with reconciliation privilege, or a first binding backed by an existing FleetScope hire event with live enrollment privilege. |
| `POST /truck-links` | Bind a reviewed MSERP truck to an existing vehicle with identity/revision preconditions. |
| `POST /trucks/upsert` | Create or update a linked truck's allowed fields; never implicitly claim a matching unlinked vehicle. |
| `POST /assignments/apply` | Atomically apply a complete affected crew component with dependencies, sequence and expected revisions. |
| `POST /trucks/retire` | Explicitly retire a linked vehicle after releases are confirmed; preserve FleetScope history/documents. |
| `GET /operations/{operationId}` | Read a committed result after an uncertain network outcome. |

All mutations carry a common envelope: `version`, `operationId`, `companyId`, `environment`, `epoch`, `occurredAt`, `sourceRevision` or `assignmentSequence`, plus operation-specific data. Separate `occurredAt`, `sourceEffectiveWeek` and receiver `appliedAt`; timestamps never determine event ordering.

Assignment data contains affected MSERP truck UUIDs, required acknowledged truck revisions, expected FleetScope assignment revisions, and primary/co-driver references with both systems' approved driver UUIDs. An explicit empty crew releases a truck. Fields omitted from the command are unchanged; omission never means delete all other assignments. Do not accept trailer IDs, ownership changes, pay data or driver profile updates on this endpoint.

Truck updates have explicit field presence: omitted means preserve, a clear is explicit and validated. Existing FleetScope values are not wiped by a blank MSERP field during initial linking; the reviewed baseline must resolve such differences before the field becomes managed. VIN changes on a linked truck require a separate identity correction review. New trucks missing required fields remain pending provisioning. FleetScope-only setup fields may be accepted on creation. The shared contract's complete owner snapshot is required for initial adoption/creation and reflected on subsequent ownership changes; ownership is never accepted on the assignment endpoint.

Return 200/201 only after commit, with operation ID, result (`applied` or `duplicate`), remote IDs, accepted source revision and resulting receiver revisions. Define machine-readable errors: `dependency_missing`, `driver_unlinked`, `identity_conflict`, `revision_conflict`, `sequence_gap`, `safety_hold`, `terminated_driver`, `required_fields_missing`, `company_rejected`, `epoch_mismatch`. Use 409 for state conflicts, 422 for invalid data, 401/403 for authentication/authorization, 429 for throttling and 503 for transient inability to commit. Reject unsupported fields, trailing JSON and oversized bodies. Publish JSON schemas and cross-repository golden request/response/signature fixtures before either implementation starts.

Both agents must use the same versioned contract bundle and content hashes. Reject incompatible capabilities before sending writes. Release only after the actual MSERP sender and FleetScope receiver pass the shared fixtures and a two-database integration test using the exact candidate revisions. This prevents independent interpretations; it does not promise uninterrupted synchronization. Outages and review blockers remain visible until acknowledged state matches current desired state.

## Security boundary

Use HTTPS with certificate validation and endpoint-level authorization, consistent with [OWASP REST Security guidance](https://cheatsheetseries.owasp.org/cheatsheets/REST_Security_Cheat_Sheet.html). The controls below are requirements for this proposed integration:

- A separate MSERP → FleetScope key, separate staging/production credentials and key IDs for rotation. Keep the existing FleetScope → MSERP webhook secret unchanged. Keys are generated securely and stored only in server secret configuration, never committed or returned to browsers.
- Sign method, exact canonical public path/query, timestamp, nonce, key ID, environment/audience and SHA-256 of exact body bytes using HMAC-SHA256. Specify byte encoding, separators and proxy path handling in a single contract. Verify in constant time with a five-minute clock window. Persist nonce use for at least the acceptance window; operation receipts provide longer-term replay protection. Do not present this custom profile as RFC 9421 compliant unless implemented and tested as that standard. The binding/replay concerns are described in [RFC 9421](https://www.rfc-editor.org/rfc/rfc9421.html).
- Bind the machine principal to the pinned company server-side. Require payload company equality; query every supplied driver, owner-driver, vehicle, link, snapshot and receipt inside that company. Foreign UUIDs must neither mutate nor reveal another tenant's data. Add composite tenant foreign keys where practical; application checks remain mandatory.
- Default disabled; fail startup on partial or inconsistent configuration. Company, destination, allowed fields and credentials are operator-controlled. Tenant administrators cannot enable this integration for other companies. Production does not accept staging epochs/keys.
- Separate normal synchronization/live-hire enrollment privileges from legacy reconciliation/link correction privileges. A bootstrap credential is narrowly scoped and revocable after cutover; live credentials cannot create arbitrary identity mappings or rewrite the baseline. Scope recovery operations and require a signed, versioned reconciliation run reference.
- Fixed HTTPS destination/host allowlist, no redirects, no request-supplied callback URLs. Bound bodies, response sizes, pagination, batch size, timeouts and request rate. Authenticate before generating exports or detailed diagnostics.
- Browser management stays session/CSRF protected. Use dedicated MSERP `fleetscope.read`, `fleetscope.manage` and `fleetscope.reconcile` permissions, with reconciliation restricted to administrators initially. Existing fleet/payroll permissions still govern the underlying business edit.
- Audit original human/system actor, operation ID, reason, before/after, mappings and receipt results in both systems. Log only sanitized identifiers/error codes; exclude keys, signatures and raw driver payloads. Limit and expire reconciliation exports; protect any retained sensitive snapshots using each app's storage controls.

Enforce MSERP ownership in FleetScope at the service/database boundary as well as the UI. Cover direct assignment CRUD, hiring with a vehicle, ownership-based auto-assignment, truck status/deletion, imports and any automated writer. The enabled tenant cannot bypass the lock by calling an old endpoint. Other companies must remain unaffected.

FleetScope termination and safety holds are explicit exceptions to the single-writer rule: they may prevent/release an assignment immediately, bump the receiver revision and surface the block to MSERP. Existing termination notifications continue to offboard MSERP. Received assignment commands cannot restore those blocked relationships. A hold's removal does not replay an old command automatically; re-evaluate current MSERP desired state. Manual MSERP termination releases the reflected assignment but does not silently extend the existing one-way lifecycle contract to terminate the FleetScope employee.

## Implementation work in MSERP

1. Add migrations and matching `backend/sql/init.sql` definitions for links, reconciliation, revisions, outbox/dependencies and receipts. Preserve completed intake evidence and financial history. Add new permissions and audited/versioned management endpoints.
2. Add an outbound client alongside `backend/internal/fleetscope/`, repository persistence, and a lifecycle-managed worker wired through config and `backend/cmd/server/main.go`. The worker should be independent of unrelated scheduled fuel/load sync settings.
3. Add transactional capture to the fleet/setup/status/team/termination and owner/name-update paths. Confirm no supported mutation can commit a changed relationship or owner snapshot without a durable sync marker. Coalesce to the final state of the local transaction; handle time-driven team boundaries.
4. Add a reconciliation workspace under Settings/Integrations with snapshot comparison, explicit identity links, missing-field completion, assignment previews, exclusion reasons and an activation gate. Reuse management components and keep types/API contracts synchronized.
5. Add compact synchronization status to truck/driver details: Synced, Pending, Waiting for driver link, Waiting for truck setup, Conflict, Failed. Include the last acknowledged time, affected dependency and permitted recovery action. Do not show success until FleetScope has acknowledged it.
6. Update `AGENTS.md` and existing FleetScope documentation when the implementation changes actual behavior/configuration. Keep new configuration names and migration numbers aligned with the repository state at implementation time.

## Rollout stages and exit conditions

| Stage | Deliverable | Exit condition |
| --- | --- | --- |
| Contract | One versioned bundle with shared schemas, ownership rules, signatures, error/retry rules, fixtures and hashes | Both implementations pass identical fixtures, reject bundle mismatch, and pass the actual sender-to-receiver integration test. |
| FleetScope receiver | Company isolation, receipts, snapshot/link endpoints and guarded writes | Other-tenant and legacy-endpoint bypass tests pass; sender still disabled. |
| MSERP sender | Transactional capture, dependencies, worker, status and review tools | Crash/retry and missing-dependency tests pass against the real receiver. |
| Staging rehearsal | Synthetic solo/team fleets and conflicting baseline | Full reconciliation, outage recovery, cutover and rollback drill pass. |
| Production reconciliation | Read-only report followed by reviewed links/provisioning | All in-scope dependencies resolved or explicitly excluded; no live assignment writes yet. |
| Controlled activation | Frozen final comparison, baseline commit, readback and new epoch | Exact crews verified; zero unexplained mismatches; other companies unchanged. |
| Normal operation | Continuous delivery and drift monitoring | Successful legitimate changes with bounded lag and no growing failure queue. |

Rollback means pause new delivery and retain FleetScope managed-field locks, links, receipts and queued operations. Resume only after comparing current state. Do not restore old assignment snapshots over newer lifecycle/safety changes. Re-enabling manual FleetScope assignment editing requires an explicit authority handoff and a fresh reconciliation before MSERP resumes. After a database restore, fence the old epoch and rebuild a reviewed baseline; never reset sequence counters and replay the old queue blindly.

## Required verification

- **Identity:** duplicate VINs/names, same unit with different VIN, missing driver, deleted mapping, cross-company owner/driver/vehicle UUID, existing intake links and legacy-link termination authority.
- **Dependencies:** create truck + assign in one MSERP transaction; assignment arriving before provision; missing VIN/year; unlinked co-driver; failed truck creation; pre-existing occupied truck; no half-applied swap.
- **Durability:** rollback produces no event; crash after local commit; receiver commit followed by lost response; repeated identical delivery; reused ID with changed bytes; old revisions; sequence gaps; concurrent workers; expired leases; database restore/epoch mismatch.
- **Domain:** solo/team transitions, co-driver removal/termination, both-driver termination, truck retirement, main-driver moves, dated team activation, historical-only correction, owner-operator assigned to somebody else's truck, preservation of trailers and safety/insurance history.
- **Security:** invalid/missing signatures, modified method/path/query/body, expired/future timestamp, nonce replay, wrong key/environment/company, unauthorized reconciliation, oversized payload, attempted old-route/import bypass and SQL tenant constraints.
- **Cutover:** data changing during review, late hire/termination, stale snapshot, partial provisioning, intentional exclusions, baseline watermark and post-baseline queue delivery.
- **Regression:** existing hires/terminations, setup/finance locks and all other FleetScope tenants. Use isolated databases and synthetic identities; do not create test production employees.

Run MSERP `go test ./...`, `go vet ./...`, appropriate disposable database suites, frontend lint/build and focused browser flows per `AGENTS.md`. Run FleetScope API unit/database suites and the web repository's declared validation scripts. Add a two-system integration test that exercises actual HTTP authentication and committed database effects; mocks alone cannot verify the contract or transaction ordering.

The operational data review must settle the actual company UUID, legacy matches, excluded records, missing FleetScope provisioning fields, status conflicts and the cutover time. Those are implementation/activation inputs; they cannot be safely inferred from source code.
