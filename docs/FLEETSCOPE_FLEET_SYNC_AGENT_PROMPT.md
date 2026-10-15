# FleetScope implementation prompt for MSERP fleet synchronization

Use this prompt for the agent working in `D:\Projects\fleetscope`. MSERP's sender is now implemented locally. Implement the FleetScope receiver against **`D:\Projects\mserp\docs\FLEETSCOPE_SYNC_WIRE.md`** and copy the exact bundle from `D:\Projects\mserp\backend\internal\fleetscope\`: `protocol.json`, `wire.schema.json`, `testdata/signature.json`, `testdata/operations.json`. Preserve UTF-8/LF bytes and their manifest hashes. These implemented wire definitions supersede provisional route/field suggestions in the plan. Read `docs/FLEETSCOPE_SYNC_RUNBOOK.md` for the actual MSERP controls and gates. Do not invent another contract, deploy, push or perform production reconciliation. Local commits are allowed.

Mandatory shared ownership specification: `D:\Projects\mserp\docs\FLEETSCOPE_OWNERSHIP_CONTRACT.md`. Follow it exactly. It replaces the earlier independently managed FleetScope ownership proposal. Read it before implementing payloads, schema changes or ownership UI.

## Objective

For the verified MS Express company only, receive trucks created/updated in MSERP and reflect MSERP-managed driver–truck assignments, including main/co-driver teams, releases and swaps. Keep FleetScope recruiting/compliance and the existing FleetScope → MSERP hire/termination webhooks working. Other FleetScope companies must retain their existing behavior.

Read each repository's `AGENTS.md` and applicable nested instructions. Run `git status --short` separately in `api` and `web`; these are separate Git repositories with existing user changes. Preserve unrelated edits. Inspect the Next.js bundled docs before changing Next.js conventions.

## Inspect these locations first

- API composition/config: `api/cmd/api/main.go`, `api/internal/config/server.go`.
- Existing sender: `api/internal/database/mserpoutbox/`, `api/cmd/mserp-webhook/`.
- Vehicles: `api/internal/database/vehicle/vehicle.repo.go`, `validator.go`, `api/internal/models/vehicle/vehicle.model.go`, `api/sql/vehicle.sql` and relevant migrations.
- Assignments: `api/internal/database/assignment/assignment.repo.go`, `validator.go`, `api/internal/handlers/assignment/assignment.handler.go`.
- Alternate writers: `api/internal/database/applicant/applicant.repo.go`, `api/internal/database/driver/driver.repo.go`, `termination.go`, `api/internal/database/dataimport/` and relevant triggers.
- UI: `web/app/(protected)/assignments/`, `vehicles/`, `drivers/[id]/_tabs/vehicles/`, `recruitment/[id]/_components/`, shared API/types and company capability state.
- Existing schema/test patterns: `api/sql/migration/20260823040000_fleet_assignments.sql`, `20260928180000_mserp_webhook_outbox.sql`, `20261005160000_mserp_termination.sql` and colocated integration tests.

## Establish the shared contract first

Read the implemented wire document, shared schema/fixtures/signature vector, and ownership specification before implementing handlers. Keep the existing inbound-to-MSERP v1 hire/termination contract unchanged. The reverse direction is a separate machine API and credential. If a contract change is necessary, propose and update both sides together before enabling writes; do not independently change the copied manifest hash.

Use one versioned contract bundle shared with MSERP, including ownership semantics and golden fixtures, with identical content hashes in both repositories. Neither side may silently reinterpret fields, enum values, nulls, signing bytes or error codes. Expose the bundle hash and `mserp-fleet-ownership-v1` capability; reject incompatible writes. Acceptance requires the actual MSERP sender against this receiver on disposable databases, recording both tested code revisions. Separate mock suites do not establish compatibility.

Implemented namespace: `/integrations/mserp/v1`, with the actual public proxy prefix included in the signature. Implement the eight routes in the manifest. Exact POST replay is the required result lookup; no status GET is required by the sender. Snapshot trucks include actual full managed-field `projection`, ownership, independent managed-field/crew revisions, and linked retirement tombstones. MSERP v1 sends the whole enrolled fleet as one atomic assignment component, including empty crews. Its activation verifies a fresh complete readback and it pauses unexpected drift. A 409/422 must certify that the operation did not commit, because reviewed recovery may supersede those rejected operations. Other uncertain outcomes must be resolved by original-operation replay.

Mutations need `version`, `operationId`, pinned `companyId`, `environment`, `epoch`, `occurredAt`, source revisions/assignment sequence and typed payloads. Return committed IDs/revisions and `applied`/`duplicate`. Store exact body hash and durable result. Reject operation ID reuse with different bytes. Same-operation retry after a lost response must return the original result without repeating side effects.

Truck identity is the MSERP UUID mapped to a FleetScope vehicle UUID, never a unit-number lookup during live writes. Do not implicitly adopt an unlinked vehicle because its VIN happens to match; require a reviewed link operation. Driver references must resolve through reviewed one-to-one company-scoped links. Store tombstones so deletion/recreation cannot silently reuse an identity.

Support normal future hires after bootstrap credentials are revoked: a live enrollment scope may establish the first driver binding using the original FleetScope hire event ID and driver UUID after MSERP setup. Verify the event belongs to this driver/company and both identities are unbound or already identically bound. Legacy bindings/relinks still require reconciliation scope. Return the committed binding revision; assignment operations depend on its acknowledgment.

## Security requirements

1. Configure the allowed MS Express company UUID server-side after operator verification by name and USDOT. Match the existing webhook company when configured. No browser/company admin can opt another tenant in. Default disabled and fail on partial/mismatched configuration.
2. Use HTTPS, a separate inbound secret and key ID, isolated environment credentials, fixed audience, and rotation support. Sign method, canonical public path/query, timestamp, nonce, environment/audience and exact body digest. Reject bad signatures, timestamps outside five minutes and reused nonces. Specify the wire algorithm precisely and test proxy prefix handling; do not claim standards compliance without implementing the standard.
3. Derive the tenant from the authenticated machine principal and require equality with the payload. Scope every resource lookup, mutation, snapshot, mapping and receipt to that tenant. Add tenant-consistent constraints where practical, including driver/owner references. Test cross-tenant UUID injection even with otherwise valid signatures.
4. Separate bootstrap/link/reconciliation capability from normal truck/assignment delivery. Require reviewed reconciliation run IDs and expected source/target versions; revoke bootstrap credentials after cutover. Do not expose integration secrets in company settings or the frontend.
5. Apply body/batch/page/rate limits, timeouts, strict JSON validation and sanitized errors/logging. No raw secrets/signatures or sensitive driver exports in logs. Preserve encryption/privacy conventions for any stored sensitive snapshot.
6. Browser administration continues using normal session permissions and CSRF. Audit actor, service identity, operation, reason, before/after and mapping revisions. UI restrictions supplement server enforcement.

## Domain rules

MSERP owns linked unit number, VIN, year, make/model, plate/state, recorded truck owner and current driver/truck crew. FleetScope retains driver compliance/lifecycle, truck compliance/safety, insurance, color/class/GVWR, trailers and trailer attachments. Reconcile old ownership labels before baseline adoption; afterward receive MSERP's owner snapshot as specified by the shared ownership contract. Require real missing provisioning details; never fill fake data.

Implement real `investor` ownership support in database constraints, backend validators, API models, list/detail/search/export and UI. The current TypeScript union already mentions investor but storage rejects it and ownership editors convert it to lease; fix this mismatch for managed trucks. Store owner kind, MSERP owner UUID, display name, optional MSERP owner-driver UUID and known effective date. No investor directory, payments, agreements or financial data are needed. Driver-owned trucks may retain the external owner snapshot without a local owner-driver link; narrowly adapt existing constraints for managed trucks, and resolve any local owner reference only from an approved same-company mapping.

Keep ownership and assignment independent in both directions. Updating the owner must not call ownership-based auto-assignment; updating the crew must not infer ownership from driver type. An investor-owned truck with a hired driver remains investor-owned. A driver-owned truck driven by somebody else retains its actual owner. Termination does not transfer ownership. Preserve coverage/documents on owner changes and handle required insurance review explicitly. Do not infer policy coverage from owner kind or relabel an investor as leased/company to bypass insurance validation. Follow the shared contract's incomplete-owner-link, name-change, audit and insurance rules.

Track MSERP operational-active state separately from FleetScope safety status. Do not map MSERP `assigned`/`available` directly into FleetScope's status enum. Retirement/deactivation prevents assignment; reactivation cannot remove a FleetScope hold. Every new MSERP truck must produce either a confirmed vehicle mapping or a visible pending provisioning result, never a silently ignored record.

The present normal assignment functions activate/deactivate drivers and may set vehicle ownership from the driver type. Build/refactor a dedicated transaction that does not infer ownership or employment changes. Preserve vacation/lifecycle values; reject terminated drivers and blocked trucks. Missing required truck information must return a specific validation result so MSERP can show Waiting for truck setup. No assignment may create a placeholder driver/truck.

Apply each affected relationship component atomically: include old and new trucks, displaced drivers, primary/co-driver roles and explicit empty crews for releases. Validate all links/dependencies and target revisions first. Acquire deterministic locks, remove conflicts within the affected component, apply the final state, enforce one open truck relationship and no driver simultaneously occupying multiple primary/co-driver slots, and commit the receipt/revisions together. Unexpected occupation outside the component is a conflict, not permission to evict somebody silently.

Use a serialized company assignment stream with an integration epoch and sequence. Reject gaps/old commands appropriately; duplicate receipt handling comes before applying state again. The recovery design must explicitly account for reviewed supersession of a blocked command. Truck provisioning can use independent per-truck revisions. Never acknowledge a received assignment as applied merely because it was queued.

Preserve trailers on their existing truck when crews change. Do not move trailers with drivers. Retain a trailer-only row where needed. A trailer-only edit must remain possible without touching managed driver/truck fields; ordinary assignment update endpoints must not provide a bypass.

Keep historical assignments/documents. Current reflection uses application time for FleetScope history and separately stores MSERP accounting effective dates as source metadata. Do not rewrite existing history from MSERP's backdated accounting boundaries. Retirement preserves records and history and requires confirmed assignment releases; absence from an export never implies deletion.

## Close every alternate write path for MS Express

Expose a server-derived capability such as `fleetManagedByMSERP` and field-level ownership to the UI. After cutover, direct staff to MSERP for truck creation, managed-field edits and driver/truck assignments. Permit FleetScope-owned compliance/equipment edits.

Enforce this in backend services and relevant database paths for assignment CRUD, `AssignAtHire`, ownership-triggered auto-assignment, vehicle status/deletion, imports and automation. For MS Express, hiring must succeed without a truck selection; remove/disable that selection and reject attempts to submit it. Continue enqueuing the normal hire event so MSERP setup can create the relationship later.

FleetScope termination/safety actions remain available as deliberate exceptions: safely release/prevent the affected assignment, increment receiver revisions and record a hold/reason. Assignment sync must never reactivate a terminated driver or override an out-of-service/safety block. Preserve existing termination delivery to MSERP. Expose the hold/current state through the authenticated read contract so MSERP can reconcile. Clearing a hold must not resurrect an obsolete command.

All guards apply only to the pinned, enabled company. A paused delivery worker does not automatically return assignment authority to FleetScope; store authority mode separately. Other companies keep their existing hiring, vehicle and assignment workflows.

## Reconciliation and activation support

Implement modes for off, reconciliation and live, with an operator-controlled epoch and explicit authority state. Provide consistent snapshots with stable pagination, minimal driver identity fields, vehicle data, crews, lifecycle/safety blockers and revisions. Never export SSNs, birth dates, payroll or documents for this workflow.

Allow reviewed links to existing records without creating fake hire events. Require expected identity/revision checks, one-to-one constraints and audit metadata. Do not auto-merge on names, plates or unit labels. A unique VIN can suggest a candidate; the link still needs reviewed approval. Handle missing/duplicate/foreign-tenant/deleted IDs as explicit errors.

The baseline order is approved driver links → review/adopt MSERP owner snapshots and link/create trucks → verify committed vehicle IDs → atomic assignment components → readback of owner and crew → activation. Enforce assignment dependencies at the receiver even if the sender is wrong. Support stale-preview detection and a short final freeze of managed writes while lifecycle/safety changes invalidate previews. Do not enable live behavior simply by deploying code or adding a secret.

Keep durable receipts/revisions during pause/rollback. Document restore handling: fence the old epoch, compare both systems and establish a new reviewed baseline. Do not blindly replay an old queue after a database restore.

## Acceptance and delivery

Add unit, disposable database and UI coverage for signatures/replay, all tenant boundaries, link collisions, incomplete truck provisioning, missing driver/truck, duplicate/delayed requests, lost responses after commit, concurrent swaps, cross-slot team conflicts, releases, trailer preservation, ownership preservation, lifecycle/safety races, stale baselines, blocked sequence recovery and restore epochs.

Run every ownership case in the shared contract: company, investor, linked/unlinked driver owner, owner different from assigned driver, all owner-kind transitions, renames, ownership date corrections, owner termination, preserved insurance facts, stale ownership delivery and rejection of managed investor-to-lease conversion. Prove assignment-only writes leave owner fields unchanged and owner-only writes leave crew unchanged.

Test every guarded legacy endpoint/import path and verify another company's normal behavior is unchanged. Preserve existing hire and termination compatibility tests. Add a contract/integration test usable by the MSERP repository, including actual HTTP and database receipt effects. Run each repository's declared test/lint/build commands and report any unrun database/browser suites honestly.

Deliver migrations plus fresh-schema changes, API and UI code, contract fixtures, operational configuration/key-rotation instructions, reconciliation/activation/rollback runbook and a concise list of any remaining data decisions. Update relevant repository instructions/docs. Do not print credentials, modify production data, enable production flags or deploy without a separate deployment request.
