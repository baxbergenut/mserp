# Shared MSERP and FleetScope ownership contract

Contract revision `mserp-fleet-ownership-v1`, October 15, 2026. This is the common ownership specification for both implementations. The implemented wire bundle and route details are in `FLEETSCOPE_SYNC_WIRE.md`; use its exact schema/fixtures. This replaces the earlier proposal to leave FleetScope ownership classifications independently managed after linking. It does not enable production changes.

MSERP owns truck ownership and driver assignments as separate relationships. FleetScope stores a read-only copy of the owner identity needed to describe the truck. FleetScope does not need an investor directory, agreements, balances, payment calculations or investor accounts.

## Exact mapping

Derive ownership from MSERP `trucks.owner_id` and its referenced `investors` row, not from the assigned driver or a legacy `isOwnerOperator` flag.

| MSERP owner record | Wire `kind` | FleetScope ownership value | Display |
| --- | --- | --- | --- |
| `is_company=true` | `company` | `company` | Company owned · MSERP owner display name |
| Non-company with `driver_id` | `driver` | `driver` | Driver owned · owner driver's name |
| Non-company without `driver_id` | `investor` | `investor` | Investor owned · owner name |

The company owner ID is MSERP's owner-record UUID, not the FleetScope tenant UUID. Names are labels; UUIDs identify records. An inactive owner still owns a truck until an explicit ownership transfer. A driver can own a truck without driving it, and can drive a different truck.

FleetScope must add real support for `investor` to its database, backend validation, API models and display code. Its existing TypeScript union alone is insufficient: the inspected `sql/vehicle.sql` permits only `company`, `driver`, `lease`, and `OwnershipForm.tsx`/`OwnershipSection.tsx` currently normalize `investor` to `lease`. Remove that conversion for managed trucks. Do not translate an investor into a lease, company owner or fake driver.

FleetScope may keep `lease` for its existing unmanaged workflows. The MSERP ownership payload never emits `lease`: the MSERP owner relationship does not establish a legal leasing arrangement. Preserve separately stored lease documents and facts during reconciliation; ownership synchronization neither creates nor cancels those records.

## Shared payload

Every managed truck creation/link/upsert includes a complete non-null `ownership` object. It is a complete replacement of the owner snapshot, not a partial patch. Assignment and retirement operations omit the truck object and preserve ownership. Invalid or unknown kinds reject the operation atomically; there is no fallback classification.

| Field | Type | Rule |
| --- | --- | --- |
| `kind` | `company` / `driver` / `investor` | Exactly one of these strings. |
| `mserpOwnerId` | UUID string | Required for every kind; the stable MSERP owner record ID. |
| `displayName` | String | Trimmed, nonempty, at most 200 Unicode code points; reject control characters. Render as text. |
| `mserpOwnerDriverId` | UUID string or null | Required field; non-null only for `driver`, otherwise null. |
| `effectiveDate` | `YYYY-MM-DD` or null | MSERP current ownership start in New York calendar dates, when known. Unknown remains null. |

Example fragment for an independent owner, using illustrative identities:

```json
{
  "ownership": {
    "kind": "investor",
    "mserpOwnerId": "11111111-1111-4111-8111-111111111111",
    "displayName": "Example Truck Holdings",
    "mserpOwnerDriverId": null,
    "effectiveDate": "2026-10-12"
  }
}
```

The enclosing authenticated truck operation supplies company, environment, epoch, operation ID, truck UUID and a monotonic integer `sourceRevision`. The ownership object shares that truck revision. Do not reuse MSERP's existing `ownershipVersion` hash as an ordered counter. Receiver receipt/revision updates and owner snapshot updates commit in the same transaction.

The sender resolves the actual persisted owner record inside a consistent source transaction. It cannot accept arbitrary owner claims from a browser. The receiver treats MSERP owner IDs as external identities within this integration/company/environment, never as FleetScope driver IDs or another tenant's owner ID. The payload does not include financial terms, contacts, bank details, tax information or portal credentials.

For a driver owner, FleetScope may resolve a local owner-driver link through the already approved driver mapping. It must never match on the owner's name or assume the currently assigned driver is the owner. The local FleetScope owner-driver UUID is a receiver-derived value, not a sender-selected substitute for the external identity.

## Storage and UI rules

Store a per-vehicle ownership snapshot with these fields, integration scope, last acknowledged source revision and applied time. This is sufficient; no standalone FleetScope investor entity is required. Keep before/after owner snapshots in the integration audit history.

For managed trucks, derive the legacy `vehicles.ownership` and optional owner `vehicles.driver_id` from that snapshot in the same transaction. Expose one effective ownership model to list/detail/search/export/UI code. Do not leave the legacy fields editable or allow two conflicting current owner records. A separate link from `fleet_assignments` remains the sole source of assigned driver/co-driver.

Existing schema checks require a local driver ID for `ownership='driver'`. Extend them narrowly so an integration-managed truck with a valid external driver-owner snapshot can exist before its owner has a FleetScope driver mapping. Other companies keep the existing validation. For this case, show `Driver owned · [name]` with no local profile link; optionally show that the owner link is pending. This does not prevent an assignment to a different, properly linked driver. The local owner pointer can be filled later when a reviewed mapping is created, without changing ownership or assignments. Local driver deletion must preserve the external owner snapshot and flag the missing profile link.

`investor` has no local owner-driver pointer and no fabricated `leased_from`. `company` also has neither. A migration must preserve any previous lease/owner values as reconciliation evidence before replacing the managed projection. Composite company checks apply to any resolved local owner-driver reference. Prevent non-enabled tenants or ordinary endpoints from selecting the new integration-only investor path as a bypass.

In FleetScope display ownership and assigned crew separately. For example:

```text
Truck 120
Ownership: Investor owned
Owner: Example Truck Holdings
Assigned driver: Jordan Smith
Ownership and assignment managed in MSERP
```

For managed trucks, remove ownership editing and server-reject ownership changes through normal vehicle, import or assignment endpoints. Scope these rules to the pinned MS Express company and enrolled integration mode. Other companies' current ownership workflows remain unchanged.

## Changes over time

| Change in MSERP | Required FleetScope result |
| --- | --- |
| Driver A is replaced by Driver B | Crew changes; owner snapshot stays identical. |
| Investor X transfers a truck to Investor Y | Owner ID/name changes; crew stays identical. |
| Driver A owns the truck but Driver B drives it | A remains owner; B is assigned driver. |
| Driver owner A becomes terminated or unassigned | Existing lifecycle rules affect A's assignments, never ownership merely because of termination. |
| Owner name changes | Update names on affected trucks with new truck revisions; do not create new identities. |
| A dated ownership boundary is corrected | Update source effective-date metadata; preserve FleetScope observation/audit history and all MSERP financial locks. |
| A stale ownership event arrives after a transfer | Return the defined stale-revision result; never restore the former owner. |
| A truck changes from company to driver/investor ownership | Update the owner snapshot, preserve crew, flag insurance/compliance review as needed. |

MSERP must capture owner transfers, owner-name changes and driver-name changes used by owner records, in addition to ordinary truck edits. Generate the affected truck's new source revision transactionally, or store durable dirty markers for projection with equivalent ordering guarantees. This includes previously synchronized inactive trucks. A rename must not be lost merely because `trucks.updated_at` did not change.

The integration can deliver ownership and assignment independently because neither operation is allowed to infer the other. A stale ownership conflict must not silently discard a crew change; dependencies should concern truck existence/readiness and approved driver identity, not require an unrelated owner name to match. Where an ownership transfer triggers a safety/insurance review, the explicit hold and its revision govern assignment readiness.

## Insurance and compliance remain explicit

Ownership does not prove that a truck is covered by any insurance policy. The inspected FleetScope creation validator restricts its company-policy inputs to company ownership; audit all creation/edit/coverage/compliance paths before adding investor support. Existing coverage/document data remains intact when ownership changes.

Do not mark investor trucks insured, uninsured, compliant or out of service merely from the new owner kind. Do not automatically enroll or remove them from a company policy. Preserve policy facts and create an ownership-change review when the existing policy rules require it. Company-policy eligibility for investor/driver-owned equipment must use an explicit reviewed FleetScope insurance decision supported by actual policy records, not an invented ownership conversion. If current validation cannot represent the reviewed result, add a separate coverage decision path for the managed company before activation; do not mislabel the owner to pass validation.

The integration does not decide legal ownership or insurance coverage. It reflects MSERP's recorded owner and retains FleetScope's actual coverage decisions. Tests must prove that existing coverage and documents survive all three ownership-kind transitions.

## Initial reconciliation

Include owner IDs, names/kinds, dates and current FleetScope classifications in the baseline comparison. A human reviews the mapping and any ownership mismatch before adopting MSERP's snapshot. Once adopted, MSERP is the only writer of that truck's owner fields. Do not leave a conflicting FleetScope lease/driver label as a second current owner, and do not change MSERP ownership merely to satisfy FleetScope's old enum.

This review establishes recorded identity; it does not replace ownership documents or alter historical insurance facts. Unknown/invalid MSERP ownership blocks adoption instead of defaulting to company. Resolve it through the ordinary MSERP ownership workflow, preserving financial guards.

## Cross-system compatibility gate

One contract bundle must be used by both agents: this ownership specification plus the full protocol's JSON schemas, semantic rules, golden payload/response/signature fixtures and fixture version manifest. Freeze and version that bundle before either implementation is accepted. Both repositories consume identical copies with the same content hashes; CI must detect divergence. Neither agent may silently rename fields, change null handling, introduce an enum or reinterpret an error independently.

Expose the protocol version, bundle hash and `mserp-fleet-ownership-v1` capability on authenticated `/capabilities`. MSERP compares them to its compiled supported bundle before enabling writes. Recheck after reconnect/deployment, and reject unsupported ownership fields rather than dropping them. Exact bundle matching is a deliberately strict first-release rule; a future compatible-version negotiation needs a separate explicit contract change.

The release test must run the actual MSERP sender against the actual FleetScope receiver on disposable databases, with the exact candidate revisions recorded. Required assertions include company, independent investor, linked and unlinked driver owners, owner names changing, all owner-kind transfers, assignment changes preserving ownership, ownership changes preserving crew, reordered/repeated deliveries, insurer review preservation, tenant isolation, legacy writer rejection and bootstrap reconciliation. Include negative fixtures for `lease`, unknown enum, null/partial ownership object, foreign-tenant local references and ID/name ambiguity.

Zero undetected conflicting writes is the design requirement. Temporary disagreement during an outage or a blocked review is still possible and must be shown as pending/conflict. Do not claim a truck is synchronized until the receiver's acknowledged owner/crew state matches the current intended state. No plan can guarantee that deployed systems will never fail; compatibility is established by the shared contract, enforcement, integration tests and production readback.
