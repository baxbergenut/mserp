# Status Board appointments, arrival, and travel estimates

Migration 116 adds operational `driver_board_automation` evidence/cache. Apply it
through the normal migration process before starting the updated server. Fresh
databases include it in init.sql. The worker runs at startup and every minute,
independently of the daily sync scheduler; a PostgreSQL advisory lock permits one
worker across API processes. GETs and browser viewers never call providers.

Status Board locations, proximity and travel estimates always use Five ELD,
without a Samsara fallback. Fleet profile maps and maintenance odometers keep
their separate source rules. Old Samsara dispatch evidence is hidden immediately
and recomputed by the worker from Five ELD.

## Appointments and destinations

Current load resolves a unique exact imported DataTruck number, including loads
without a Gross Board plan. Ambiguous plan identities remain blocked; no synthetic
plans, assignments, payroll changes, or provider writes are made. The selected
stop drives Destination, appointment and arrival together. DISPATCHED defaults to
pickup; ENROUTE/RESERVED default to delivery. The Loads panel selects intermediate
stops. Entering a uniquely matched load is the dispatcher's operational assignment,
even when DataTruck lists another driver: status defaults to DISPATCHED and pickup
location/appointment display immediately, with no additional confirmation. Existing
selections also remain usable if DataTruck changes its driver. This does not rewrite
the imported driver, fleet assignments, or Gross Board. Missing/ambiguous matches
show a review message in Loads. The legacy requiresConfirmation response stays false.

If Gross Board later adds or recreates a plan for an already selected imported
load, Status Board reconnects its source details by exact load number and the
same imported record ID. Repeated plans for that one source are equivalent;
unresolved or conflicting matches remain blocked. A surviving selected plan
that changes its source record also requires review. Reconnection preserves the
operational selection identity, selected pickup/delivery stop, manual destination,
manual stop time and travel context. It is a read-only projection: no dispatch
progress, assignments, accounting plans, versions or history are rewritten.
Equivalent plans remain eligible across week boundaries for the selected source.

Import preserves provider stop IDs, coordinates, local schedule dates/times, UTC
schedule instants, ready-time fields and notes. Existing imported snapshots acquire
these fields on the next shared operational refresh; older current selections
remain refresh candidates. Source refresh never advances the current load.

Read-only appointment text shares the ETA cell with editable travel information.
The provider's explicit `appointment` type displays the local start time;
`window` displays the facility-local closing time and its weekday when shorter
than 24 hours, including overnight windows. Closing times from 22:00 onward use
`TILL` (for example `DEL TILL 23:00 FRI`). Full calendar days (00:00–23:59)
and windows of at least 24 hours retain `24/7` with the start weekday. Full local window
boundaries are preserved in Loads and the ETA editor. Live read-only sampling confirmed
`appointment` with a start date/time and null ends, plus `window` with both ends.
All-day ranges returned as `window` use the same formatting. Unknown provider
enums, incomplete/reversed windows and lone legacy timestamps do not produce an
automatic label. Do not guess an all-day or exact classification from midnight.

Fixture: load 4010927482, delivery stop 101555, local October 16, 2026,
08:00–23:59, type window => `DEL TILL 23:59 FRI`. A Friday pickup window of
01:00–02:00 displays `PU 02:00 FRI`.

The first queued load's pickup label is separately clickable and highlighted on
hover/focus. Its popup saves/clears the manual pickup time through the existing
audited `stop_time` action with explicit `planId` and `stopKey`. The server checks
the board version, source revision, first queued load and first pickup stop;
stale or changed targets are rejected. The load number and remaining cell open
the queue. A known pickup without a source schedule shows `PU —`; unmatched loads
cannot be edited. Overrides use the imported-load/stop identity, survive source
refresh and promotion to current, and clearing restores the current source time.
This action changes no current-load selection, ETA, progress or provider data.

## Arrival and manual input

The latest provider position within five straight-line miles of the selected stop
shows @PU/@DEL; a newer position beyond five miles clears the automatic marker.
This is a proximity signal, not a claim that the truck reached the dock.
It uses the cached GPS coordinates and selected stop coordinates without requiring
optional road routing. Future/out-of-order positions are ignored. An unchanged
cached position is re-evaluated after a rule change, including when the provider
has not sent a new point. Positions older than ten minutes show only last-known
proximity, with an amber marker. Automatic
proximity requires a uniquely identified assigned truck VIN. Driver/truck assignment identity,
VIN, trailer, load ID, selected stop and facility coordinates scope evidence.

Fresh proximity displays a red @PU/@DEL prefix; stale proximity uses amber.
While @PU/@DEL is displayed, the ETA cell hides the retained travel estimate,
which returns when the automatic marker clears.
Time/day remains the normal theme color.
For a stop without a recognized provider schedule, the prefix appears alone.
The ETA popup has no arrival or travel mode dropdown. Old saved mode choices are
ignored. Schedule-only refresh does not reset GPS proximity. GPS never changes status,
marks loads completed, or advances stop progress. Last-known proximity becomes stale
after ten minutes. The ordinary location column retains its existing
three-hour coloring policy.

ETA uses New York time in the cell, rounded to the nearest 15 minutes (ties round
forward). Manual times use the same display rounding without changing saved input.
The hover shows only the rounded ETA in the selected facility's local timezone,
resolved offline from stop coordinates with tz-lookup; it includes the local date
and timezone abbreviation. No appointment, GPS diagnostic or routing-profile notes
appear in the cell hover. Missing facility coordinates or a valid ETA leave the
hover empty. Stale GPS retains mileage only, without presenting an old ETA.
Manual times/mileage and live automatic travel display red; stale automatic
mileage displays amber, matching stale @PU/@DEL. The API exposes travelStale and
travelArrivalAt separately from arrival-proximity evidence.
Entering either clears the other. Notes are never rewritten. Existing manual ETA
survives rollout and is bound to its current context; a subsequent load/stop/truck
change invalidates it. The combined cell is copied as one text value. Poll evidence
is stored separately from personal history/undo and broadcast using the existing
SSE snapshots. Active drafts retain the existing concurrency protections.
Each load card has a rightmost copy-number icon, available to read-only viewers
without opening the card or changing the load selection.

The optional manual pickup/delivery time input starts blank for each selected load
stop. Saving text replaces the displayed DataTruck appointment label for that stop;
clearing it restores the source schedule, including subsequent DataTruck updates.
The original imported schedule remains unchanged. Entries are versioned in the
load-state JSON and keyed by load and stop; switching stops does not copy a time
to another stop. The input accepts up to 80 characters on one line so dispatchers
can enter the local time and day in their own shorthand (for example, `14:00 MON`).

## Opt-in Google truck routing

Default off. No Google calls or speed-tracking calls are made until enabled.
Google large-vehicle routing requires provisioned access; standard DRIVE/car
routes are never substituted. Verify billing alerts, API key restrictions and
actual truck-route accuracy before activation. Never put keys in browser variables.

Server environment:

```dotenv
BOARD_ROUTES_ENABLED=false
BOARD_ROUTES_API_KEY=...
BOARD_ROUTES_ACCESS_VERIFIED=true
BOARD_ROUTES_OPERATIONS_VERIFIED=true
BOARD_ROUTES_STANDARD_SEMI=true
# Optional measured per-truck overrides:
# BOARD_ROUTES_PROFILES_FILE=/etc/mserp/board-route-profiles.json
BOARD_ROUTES_REQUESTS_PER_MINUTE=30
FIVE_ELD_SPEED_VERIFIED=true
FIVE_ELD_SPEED_UNIT=mph
```

`GOOGLE_ROUTING_API_TOKEN` is also accepted when `BOARD_ROUTES_API_KEY` is empty.
Adding either key alone does not activate the worker; all verification and profile
requirements below still apply. Keep real keys out of commits, including tracked
development environment files.

Enabling requires all verification flags, key, a readable profile file or explicit
standard-semi opt-in, existing
Five ELD configuration and an explicitly verified mph/kph speed unit. Flags are
operator attestations, not an API-access test. No production activation is implied
by deployment. Disable BOARD_ROUTES_ENABLED and restart to stop routing; cached
automatic values are disabled by the next worker pass. Manual entry remains usable.
Matched selections with missing/invalid GPS still report routing as enabled and
explain why an estimate is unavailable. A valid old GPS position can bootstrap
road miles through the same paced Google worker without querying tracking data.
Reuse an existing valid distance from that position; do not reroute an unchanged
resting position on every poll. Fresh telemetry queues on the next minute pass
without waiting for the old-position route's five-minute interval.

On October 18, 2026, the operator approved a shared estimate profile and confirmed
Five ELD tracking speeds are mph. `BOARD_ROUTES_STANDARD_SEMI=true` uses assumed
dimensions of 13 ft 6 in height, 102 in width, 73 ft overall length and a 53 ft
trailer, with assumed 80,000 lb combined tractor/trailer/cargo weight. API metric
values round up. Hazmat restrictions are not modeled. These assumptions remain
in the server cache/diagnostics rather than the ETA/miles hover. This profile is for
board travel estimates, not measured vehicle clearance or driver navigation.
Live DataTruck list/detail samples (including load 8YX9221) did not expose any
weight field; do not describe the fallback as DataTruck weight or use cargo-only
weight as combined vehicle weight. No DataTruck refresh requests are added.

The optional profile file is keyed by local truck UUID. Each entry is reviewed for the
specific imported numeric load record ID, current trailer number, and expiration.
This prevents using historical dimensions/actual weight for a different load.
An explicit entry overrides the shared profile; an invalid/expired explicit entry
suppresses estimates rather than silently falling back. The shared profile follows
the current context and requires no per-load file updates.
Values below are illustrative, not fleet defaults:

```json
{
  "LOCAL-TRUCK-UUID": {
    "loadId": 65341,
    "trailer": "CURRENT-TRAILER-NUMBER",
    "validUntil": "2026-10-20T00:00:00Z",
    "vehicleInfo": {
      "totalHeightMm": "4114",
      "totalLengthMm": "21945",
      "totalWidthMm": "2591",
      "totalWeightKg": "32658",
      "trailerInfo": [{ "lengthMm": "16154" }],
      "hazardousGoodsTypes": []
    }
  }
}
```

Review actual tractor/trailer dimensions, loaded weight and hazmat restrictions;
an empty hazmat array explicitly means none. Profiles are loaded at server startup;
restart after reviewed changes. Missing/expired/mismatched explicit attributes suppress the
automatic estimate. No profile values are inferred from maintenance or truck color.

The minute worker checks changed contexts immediately on its next pass and
recalculates eligible estimates every five minutes. It processes least-recently
attempted trucks first, within a forty-second run budget. Requests are serial,
globally paced per active worker (default 30/minute, configurable 1–120), with
fifteen-second Google timeouts and one-minute error/five-minute 429 cooldowns.
Queue latency can exceed five minutes when fleet size/provider quotas exceed
capacity; tune quotas only after reviewing costs. Five ELD requests reuse the
client's roster identity cache; source mileage values are never rewritten.

Distinct fresh tracking points establish moving at >=5 mph or >=100m movement.
Stopped requires <=2 mph with movement under 100m for at least three minutes;
intermediate speeds retain the previous classification. Speed units are normalized
only after provider verification. Moving displays road miles remaining above 600
miles, otherwise New York ETA from observation time plus routed duration. Exactly
600 miles displays ETA; displayed miles round up. Stopped always displays road
miles remaining. After ten minutes, show last-known road miles instead of ETA,
regardless of the last movement classification. Preserve them through stale
GPS/rest periods, with the provider observation time and stale warning on hover.
If no route existed when the truck went offline, calculate distance from the
shared cached location. This never establishes stopped/ignition state, advances
arrival, or produces an ETA from stale coordinates. New load/stop/truck contexts
clear the old distance and route to the newly selected stop. A fresh stationary
point after a long rest does not discard the last stopped state. Unknown fresh
movement, missing/invalidated routes or partially ignored restrictions suppress
output. Temporary errors retain a prior valid distance with a hover warning;
live moving ETAs expire after ten minutes. Results are fenced against the selected load/stop/source revision
and current truck before committing.

When enabled, a blank ETA displays the automatic estimate. Applying a manual
time or mileage takes precedence; clearing it restores the automatic estimate.
These are traffic-aware travel
estimates, excluding driver breaks, remaining HOS, planned stops and loading delays;
there is no explicit weather-forecast adjustment.

## Validation and operation

Run repository Go tests, including TestBoardSchedules/TestArrivalEvidence/
TestTrackingMotion and boardroutes tests. TestDriverBoardDatabase covers migration
and fresh schema using MSERP_DRIVER_BOARD_TEST_DATABASE_URL, which must be a
disposable local _test database. Browser checks after a /api frontend build:

```text
node scripts/test-board-appointments.mjs
node scripts/test-status-board-layout.mjs
node scripts/test-board-grid.mjs
node scripts/test-investors-e2e.mjs --board-only
```

Monitor board-automation worker errors, last observation/sync times, profile
expirations, Google quota/billing dashboards and a representative sample of
driver-confirmed arrivals. Do not exercise mutations against production for tests.
Provider references: https://fiveeld.com/api-docs/,
https://apidocs.datatruck.io/api-reference/endpoint/get_loads_list,
https://developers.google.com/maps/documentation/routes/lvr.
