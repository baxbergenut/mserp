# Fleet statuses and history

Migration 137 introduced shared driver availability, employment, truck restrictions,
and append-only history. Migration 149 replaces the truck-facing condition and
Active/Inactive controls with one effective status. Driver employment and personal
availability remain independent.

## Truck status model (149)

| Status | Meaning | Compatibility active |
| --- | --- | --- |
| Ready | Operational, no employed operator assigned | true |
| In use | Allocated to an employed direct/current team driver | true |
| Shop | Temporary restriction with a specified reason | true |
| OOS | Indefinitely out of operation, explicitly reversible | false |
| Terminated | No longer operating with the company, explicitly reversible | false |

In use does not indicate GPS movement. Home, Vacation, rest, and other personal
unavailability retain In use. Terminated driver links do not count as employed
operators. Company/investor trucks become Ready after their last employed
operator is released, unless another restriction remains.

`fleet_truck_operating_status` is the shared effective projection. Raw
`trucks.status` retains available/assigned/maintenance/out_of_service and adds
terminated; available/assigned never override operator-derived Ready/In use.
`condition_status` retains its database name as the versioned projection cache.
The active flag is derived compatibility data; writing it does not change the
truck lifecycle or release an assignment. Lists, boards and dispatch checks use
the effective projection; Ready and In use both pass the truck dispatch check.
Driver employment/availability dispatch checks continue separately.

Precedence is Terminated, OOS, Shop, then assignment-derived In use/Ready.
Manual and independent work-order restrictions remain authoritative until each
is explicitly released. Completing/paying a work order does not release it.
Shop/OOS retain current crew, but new operating assignments require returning
the truck to service. Existing assignments can be ended or moved elsewhere.

Truck profile changes and returns require a nonblank reason (maximum 1000
characters), current status version and trucks.edit. Manual Terminated refuses
employed direct/team operators; resolve their assignments explicitly first.
Return to service derives Ready/In use and refuses outstanding work-order
restrictions. Rehire never returns a restricted/terminated truck to service.
Full profile writers share the database guards and status-version checks.

Owner-operator termination uses the existing recorded investors.driver_id
ownership relation and idempotent offboarding receipts. Eligible trucks become
Terminated, retaining an existing owner-driver link. Trucks with employed
operators stay unchanged and enter review. Continuing investors' hired-driver
and unassigned fleets remain independent of their driving employment. Receipt
outcome codes retain their historical inactivated/already_inactive wire values.
No provider enrollment/removal or financial settlement is inferred.

## Status Board and work orders

The server-owned fleet_status_rules catalog maps SHOP, STOP and ACCIDENT to
Shop. Board changes require a supplied statusReason and may place restrictions
but cannot weaken OOS/Terminated or release another restriction. HOLD is retired;
existing historical events stay readable. Hold for Load remains an operational
annotation and does not restrict the truck. Left still opens driver termination.

HOME/HOME (YARD), VACATION, REST, 34 RESET, Family issue, Health issue and NEW
DRIVER affect only personal availability. READY activates availability but cannot
release a truck restriction. Operational dispatch labels require an employed,
available driver and an unrestricted truck. Clearing an annotation releases
nothing. Crew share truck status; personal availability remains individual.

Work orders offer Shop/OOS restrictions, require the reason in their description,
and retain existing category Edit permissions for explicit release. Normal
Maintenance expenses remain the standard expense workflow; this does not restore
the retired work-order creation flow. Work-order reasons appear with manual
reasons in truck status details, without rewriting profile or board notes.

## Interfaces, directory and history

- GET /trucks supports the optional status filter: ready, in_use, shop,
  out_of_service, terminated. The paginated directory orders Ready, Shop, In use,
  OOS, Terminated, then unit/id, before LIMIT/OFFSET. Counts use the same filter.
- Truck responses expose operatingStatus, statusReason and statusVersion. Lookup
  responses expose operatingStatus but exclude reasons and private notes.
- PATCH /trucks/{id}/status accepts status, version, reason, and optional RFC3339
  effectiveAt. The legacy /condition route uses identical guards. Ready/in_use
  requests both mean explicitly return to assignment-derived service.
- GET /drivers/{id}/status-history and /trucks/{id}/status-history retain track,
  status, from, to, page and snapshot filters. Truck history retains the internal
  condition track name, displayed as Truck status. Existing Hold history remains
  historical evidence. Driver history excludes truck/dispatch tracks.
- PATCH /drivers/{id}/availability and driver leave-plan/version behavior are
  unchanged. Dates use New York; effectiveAt cannot precede the latest transition
  or lie in the future. Board restrictions cannot predate current assignment.

The directory has Unit (with owner), Equipment, Driver, Status and Status notes.
Status notes show the current manual and work-order restriction reasons. Clicking
anywhere on a truck row opens its profile; Enter/Space support keyboard navigation.
IRP documents and permission-gated edit/delete/status actions are on the profile.
Colored status cards show server-side counts for the current search, independent
of the selected status and pagination. Each card filters the table; the selected
card has a checkmark and highlighted border. All statuses show by default;
search/filter/page memory survives navigation.
Visible pages refresh on focus and every 30 seconds without replacing drafts.

GPS health is a separate read-only advisory, never a lifecycle transition or
proof of movement. Only In use trucks receive `gpsAlert`: `overdue` when the
latest trusted observation is more than 168 hours old, `missing` when no trusted
report is available, otherwise empty. `gpsReportedAt` and `gpsSource` identify
the newest valid cached Five ELD or Samsara observation; refresh/profile-edit
timestamps never count. Future dates, ambiguous VINs, duplicate local matches
and unavailable Samsara vehicles cannot establish freshness. Existing map and
Status Board provider choices stay unchanged. No history table, new polling job,
provider requests, status writes or migration is added.

GET /trucks accepts `gpsAlert=overdue|missing`. Paginated responses include
`gpsAlertCounts` for the current search, independent of status/alert filters and
pagination (as with status counts). The directory has two amber count filters,
GPS update overdue and No GPS data. Selecting one clears the status filter;
selecting a status clears the GPS filter. Counts and rows are server-derived.
The profile shows the last report date/time in Eastern Time and its source,
separately from restriction notes and the map's selected provider.

History retains actor/name/source/reason snapshots, action ID, effective and
recorded times. Migration baselines have unknown earlier starts. GETs/no-ops add
no events. Database triggers synchronize assignments, employment, current teams,
profiles, board and work orders under the fleet status transaction lock. The
cache-only minute worker catches team date changes without advancing loads.
New writers acquire setBoardActor before row locks.

## Conversion, compatibility and rollout

149 applies the approved bulk conversion atomically: all previously inactive
trucks become Terminated; remaining Hold restrictions become Shop; existing
active Shop/OOS restrictions survive; unrestricted active trucks derive Ready
or In use. Existing notes and committed history remain unchanged. Missing
historical reasons receive explicit migration provenance, not invented reasons.
No assignment, ownership, financial ledger or provider membership is removed.

The FleetScope wire format is unchanged. Terminated projects through the legacy
operationalActive=false/operationalStatus=available representation; external
lifecycle authority remains unchanged. DataTruck lifecycle/assignment changes
remain excluded from profile PUT delivery.

149 preserves the existing fleet_status_control activation setting. Existing
activated installations continue immediately. For installations still awaiting
137 activation, review -fleet-status-preview then use
-fleet-status-activate <reviewed-revision>; changed snapshots refuse activation.
Schema deployment never silently enables that earlier reconciliation. Preserve
backups/history; do not rerun activation or discard baselines on rollback.
Production deployment is separate from local implementation/testing.

## Validation

Use disposable localhost databases ending in _test. TestTruckStatusesDatabase
and TestFleetStatusDatabase use MSERP_DRIVER_BOARD_TEST_DATABASE_URL, checking
fresh and incremental schemas as mserp_app. Coverage includes conversion,
reasons, operator/crew eligibility, retained OOS assignments, explicit returns,
independent restrictions, concurrent edits, no-op/read history and pagination.
TestTerminationOffboardingDatabase and TestFleetScopeDatabase use
MSERP_FLEETSCOPE_TEST_DATABASE_URL. Also run affected driver-board, team,
maintenance and weekly-PTI database tests.

After a /api frontend build, start frontend/scripts/review-local.mjs with the
isolated review database. Set MSERP_REVIEW_ACCESS_FILE, then run
node scripts/test-fleet-status-e2e.mjs and node scripts/test-truck-statuses-e2e.mjs.
They verify the real API, screens, permissions, reasons, history, filtering,
ordering, OOS assignment preservation, explicit returns and navigation memory.
