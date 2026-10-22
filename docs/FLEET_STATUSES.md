# Fleet statuses and history

Migration 137 adds a shared, database-enforced status mapping and append-only
history. Employment, personal availability, truck condition, and dispatch detail
are separate. Only termination makes a driver inactive or starts offboarding.
Weekly PTI is not implemented in this phase.

## Mapping and permissions

`fleet_status_rules` is the server-owned catalog; the board selector reads it
through `GET /fleet-status/catalog`. READY activates availability but requires a
ready truck. COVERED/DISPATCHED/ENROUTE/RESERVED/DEADHEAD and pickup/delivery
labels require active availability and a ready truck. Yard and Hold for Load
require active availability without changing the truck condition.

HOME/HOME (YARD), VACATION, REST, 34 RESET, Family issue, Health issue, and NEW
DRIVER set personal unavailability only. SHOP, STOP, ACCIDENT, and HOLD place
manual truck restrictions; STOP/ACCIDENT mean OOS. Board writers cannot weaken
an existing stronger manual restriction or release any work-order hold. Left
opens the existing permission-protected termination dialog; raw board saves
cannot terminate a driver. Clearing a board annotation releases nothing.

Board writers can place restrictions. Manual release requires `trucks.edit`;
work-order releases retain their category Edit rules. Driver availability changes
from profiles require `drivers.edit`. Releasing the last truck restriction shows
READY, preserving any personal unavailability and never restoring an old load
status. Crew members share the truck's condition; personal reasons remain
individual, and only the main team driver has a dispatch row.

## History and interfaces

- `GET /drivers/{id}/status-history` and `/trucks/{id}/status-history` require
  the respective profile read permission. Optional `track`, `status`, `from`,
  `to`, `page`, and `snapshot` filters return 50 records and `hasMore`.
- `PATCH /drivers/{id}/availability` and `/trucks/{id}/condition` accept
  `status`, `version`, optional RFC3339 `effectiveAt`, and optional `reason`.
- Profile responses include status versions, availability, employment, and
  effective truck condition. Full profile forms pass `statusVersion` to prevent
  stale status changes. Legacy Home/Vacation and `active` consumers are retained.
- Effective times cannot precede the last transition or lie in the future.
  Truck restrictions from the board also cannot predate the current assignment.
  Older-period insertion/correction is outside v1.

Events retain name/actor/source snapshots and a transaction action ID. No-op
saves create no events. Baselines explicitly have unknown earlier starts. Driver
history clips truck condition periods to dated solo/team assignments. Table and
timeline views display New York times; recorded time is separate from effective
time. Existing board edit/undo history remains intact; undo cannot bypass holds.

Database triggers cover board, profile, assignment, and work-order writers.
Use `setBoardActor` before taking operational row locks in new repository writers;
the short transaction advisory lock serializes linked status changes. Work-order
source references and operator/provider contexts stay server-side. Gross Board
and queue actions persist ENROUTE/RESERVED changes; a startup/minute cache-only
worker handles queue/date changes without advancing loads. GETs never write.

## Reviewed activation and deployment

Schema deployment alone leaves synchronization disabled. Run the deployed API
with its normal service environment and `-fleet-status-preview`; this is read-only
and exits before workers/provider clients run. It emits current status/assignment
snapshots and a SHA256 revision. Review discrepancies before running
`-fleet-status-activate <reviewed-revision>`. Activation locks the fleet, verifies
the exact snapshot, adopts the reviewed current board mapping, creates baselines,
and enables synchronization atomically. A changed revision requires a new review.
Departure and retired labels must be resolved explicitly before activation.

Activation never terminates a driver from an old board label, changes employment
for a personal reason, or creates payroll/provider actions. Preserve backups and
history on application rollback; do not rerun activation or reset the baseline.

## Validation

`TestFleetStatusDatabase` uses `MSERP_DRIVER_BOARD_TEST_DATABASE_URL` pointing to
a disposable localhost database ending in `_test`, checking fresh/migrated
schemas as `mserp_app`. It covers the mapping, activation fencing, actor capture,
holds, crew behavior, concurrency, backdating, no-op history, and queue persistence.

After a `/api` frontend build, start `scripts/review-local.mjs` with the documented
local review database. Set `MSERP_REVIEW_ACCESS_FILE` to its private access file
and run `node scripts/test-fleet-status-e2e.mjs`. The test activates only that
synthetic schema and verifies profiles, live board updates, history views,
restricted roles, explicit release, and termination UI without vendor calls.
