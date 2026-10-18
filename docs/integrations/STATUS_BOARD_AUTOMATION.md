# Status Board appointments, arrival, and travel estimates

Migration 116 adds operational `driver_board_automation` evidence/cache. Apply it
through the normal migration process before starting the updated server. Fresh
databases include it in init.sql. The worker runs at startup and every minute,
independently of the daily sync scheduler; a PostgreSQL advisory lock permits one
worker across API processes. GETs and browser viewers never call providers.

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
show a review message in Loads. The legacy requiresConfirmation response stays false
and confirm_driver action remains accepted for older clients.

Import preserves provider stop IDs, coordinates, local schedule dates/times, UTC
schedule instants, ready-time fields and notes. Existing imported snapshots acquire
these fields on the next shared operational refresh; older current selections
remain refresh candidates. Source refresh never advances the current load.

Read-only appointment text shares the ETA cell with editable travel information.
The provider's explicit `appointment` type displays the local start time;
`window` displays 24/7 under the team's shorthand convention. Full local window
boundaries are preserved on hover and in Loads. Live read-only sampling confirmed
`appointment` with a start date/time and null ends, plus `window` with both ends.
All-day ranges returned as `window` use the same formatting. Unknown provider
enums, incomplete/reversed windows and lone legacy timestamps do not produce an
automatic label. Do not guess an all-day or exact classification from midnight.

Fixture: load 4010927482, delivery stop 101555, local October 16, 2026,
08:00–23:59, type window => `DEL 24/7 FRI`.

## Arrival and manual input

Two increasing provider positions inside 150m, spanning at least three minutes,
establish arrival. Two consecutive positions outside 300m remove automatic arrival.
The intermediate band breaks candidate sequences. Samples must be no older than
ten minutes; duplicates/future/out-of-order data cannot advance detection. Long
gaps reset candidate continuity, preserving only last-known arrival. Arrival
requires a uniquely identified assigned truck VIN. Driver/truck assignment identity,
VIN, trailer, load ID, selected stop and facility coordinates scope evidence and corrections.

Arrival displays a red @PU/@DEL prefix; time/day remains the normal theme color.
The ETA popup offers Automatic (GPS), Arrived and Not arrived corrections. Manual
corrections are versioned/audited through load-state actions. Context changes expire
them. Schedule-only refresh does not reset GPS dwell. GPS never changes status,
marks loads completed, or advances stop progress. Last-known arrivals become stale
on hover after ten minutes. The ordinary location column retains its existing
three-hour coloring policy.

ETA uses New York time. Manual times display red `ETA HH:mm`, mileage red `133mi`.
Entering either clears the other. Notes are never rewritten. Existing manual ETA
survives rollout and is bound to its current context; a subsequent load/stop/truck
change invalidates it. The combined cell is copied as one text value. Poll evidence
is stored separately from personal history/undo and broadcast using the existing
SSE snapshots. Active drafts retain the existing concurrency protections.

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
BOARD_ROUTES_PROFILES_FILE=/etc/mserp/board-route-profiles.json
BOARD_ROUTES_REQUESTS_PER_MINUTE=30
FIVE_ELD_SPEED_VERIFIED=true
FIVE_ELD_SPEED_UNIT=mph
```

Enabling requires all verification flags, key, readable profile file, existing
Five ELD configuration and an explicitly verified mph/kph speed unit. Flags are
operator attestations, not an API-access test. No production activation is implied
by deployment. Disable BOARD_ROUTES_ENABLED and restart to stop routing; cached
automatic values are disabled by the next worker pass. Manual entry remains usable.

The profile file is keyed by local truck UUID. Each entry is reviewed for the
specific imported numeric load record ID, current trailer number, and expiration.
This prevents using historical dimensions/actual weight for a different load.
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
restart after reviewed changes. Missing/expired/mismatched attributes suppress the
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
600 miles displays ETA; displayed miles round up. Stopped always displays road miles remaining. Stopped-truck
miles remain visible through stale GPS/rest periods, with the last observation
time and a stale-data warning on hover, until fresh movement is established or
the load/stop/truck context changes. A fresh stationary point after a long rest
does not discard the last stopped state. Moving estimates (ETA or miles) expire after ten minutes;
unknown movement, missing/invalidated routes or partially ignored restrictions
suppress output. Temporary errors retain a prior valid moving estimate for at
most ten minutes, with a hover warning. Results are fenced against the selected load/stop/source revision
and current truck before committing.

When enabled, blank ETA starts in Automatic mode; existing values stay Manual.
The popup can switch modes. Applying a manual value takes precedence; switching
back to Automatic clears the manual value. These are traffic-aware travel
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
