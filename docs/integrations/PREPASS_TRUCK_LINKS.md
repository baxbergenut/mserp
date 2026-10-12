# PrePass truck linking

Tolls > PrePass > Needs attention groups unlinked tolls by PrePass PPDeviceID (tag), falling
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
