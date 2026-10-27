# PrePass truck linking

Tolls > Needs attention lists each unlinked transaction and each unreviewed actual
spending flag (status `review`) separately. Linked `data_issue` / Check data hints
are excluded. Unlinked transactions remain for truck linking, without Check data
hints in this view. Flags use
the reviewed local truck when linked, falling back to the reported unit otherwise.
The shared flag popover retains the reason and load evidence. The posting cutoff
defaults to the first day of the previous New York month; pagination preserves
selection across pages. Transactions shows PPDeviceID (the fleet transponder
number) below the truck, never deviceNumber (the separate plate/device value).

Flags assess the exit/crossing date, including one day before load pickup and
after delivery. Posting dates only control the list's Posted since filter.
Mark reviewed records an actor/time decision and hides the flag by default.
Show reviewed exposes those decisions with Reopen. Unlinked transactions remain
until linked even if their spending flag has been reviewed. Review is only a
workflow acknowledgement; source charges, links, statements and the derived flag
on Transactions remain unchanged.

Migration 156 stores append-only `toll_flag_review_events`. POST
`/tolls/{id}/review` requires `tolls.link`, a request UUID, the current evidence
revision, previous review version, and explicit `reviewed` boolean. Saves reject
stale source/evidence or concurrent review decisions; retries are idempotent.
Changed amounts, dates, truck attribution or surrounding load evidence require
review again, while routine resync timestamps do not. GET `/tolls/truck-links`
accepts `includeReviewed=true` to include reviewed flags with attribution.

Select up to 100 transactions, choose one truck, preview, then Link transactions.
Only the explicit selected IDs change; no future mapping is created. The same
`/tolls/truck-links/preview` and `/tolls/truck-links` endpoints accept
`transactionIds` with `truckId`, plus the save UUID and reviewed revision.
Migration 154 records the actor, destination, immutable request revision and full
before snapshots in `toll_transaction_links`, with per-toll provenance. Source
details remain unchanged. Saves are atomic and idempotent, reject stale previews,
and check finalized driver/investor statements for both old and new attribution
under the financial and mapping locks. Explicit links survive provider resync and
legacy mapping undo. Linking alone does not dismiss a load-coverage flag.

Existing saved mappings remain available for undo. Fleet roster mapping still
uses the dated rule workflow below. Legacy mapping API clients group unlinked
tolls by PrePass PPDeviceID (tag), falling
back to the exact normalized vehicle unit only when a tag is missing. Leading
zeros, punctuation and spaces remain meaningful. A missing tag and missing unit
cannot be mapped; investigate that identity in PrePass. No mapping is inferred
from a similar unit, plate, VIN, or transponder number. The UI shows source units
and transponder/plate values and truck choices with local VIN/plate for review.

The list defaults to postings from the start of the previous New York month,
matching the sidebar badge, and allows an earlier posting cutoff. Mapping dates
instead use the toll's exit (travel) date, with an inclusive start/end and optional
open end. This supports tags moved between trucks. The preview covers all unlinked
tolls in those travel dates, regardless of the list's posting cutoff. It displays
count, amount and source unit names before a mapping can be saved.

Fleet roster linking now shares these rules through migration 083; see
[PrePass fleet management](PREPASS_FLEET.md) for the combined dated link flow.
Legacy group mappings remain available for historical identities absent from the
current roster. Their tag-first matching semantics are unchanged.

Migration 077 adds `prepass_truck_mappings`, its link/undo audit events, and a
mapping ID on tolls. GET `/tolls/truck-links?from=YYYY-MM-DD` requires `tolls.read`.
POST `/tolls/truck-links/preview`, POST `/tolls/truck-links`, and
POST `/tolls/truck-links/{id}/undo` require `tolls.link` (granted explicitly to
non-administrator roles). Writes validate the reviewed transaction snapshot and
serialize mapping changes/imports. Overlapping active mappings for one identity
and environment are rejected. Repeated identical save UUIDs are idempotent.

Mappings apply only to unlinked historical rows and new imports. On new rows a
confirmed mapping wins over a unit-name guess; sync preserves existing truck
links. Environment and travel-date bounds are always enforced. The database
trigger also records mapping provenance, including future imports. Undo disables
the rule, clears only its own attributed links, and records all affected IDs.
It does not delete tolls or mapping history. A stale undo version is rejected.
Manual link/undo checks both driver and investor finalized statements under the
shared financial lock; affected statements must be reopened first. Normal import
source reconciliation retains its existing financial-report behavior.

Validation: `MSERP_RELAY_TEST_DATABASE_URL` and `TestRelayDriverDatabase` exercise
fresh/migrated schemas as mserp_app, including grouped identities, stale previews,
future imports, environment/date isolation, preservation of existing links,
finalized-statement protection, and undo/audit. The static frontend fixture
`node scripts/test-relay-drivers-views.mjs` also exercises the truck-linking tab,
preview invalidation, saving, and undo without contacting PrePass.
