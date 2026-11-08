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

## Editing pause (188)

Load, status, destination and selected-stop edits defer automatic route requests
until five seconds have passed without another routing edit. Switching Dispatched
to Enroute during this interval restarts the pause for the delivery destination.
Notes and ordinary detail reads do not extend it. The deadline is durable in
`driver_board_automation.route_not_before` and is checked before choosing work
and again before processing each queued target. The existing minute worker cadence
still applies; this is not a five-second polling interval. Version/context fences
continue to discard results from an obsolete selection.

## Appointments and destinations

Current load resolves a unique exact imported DataTruck number, including loads
without a Gross Board plan. Ambiguous plan identities remain blocked; no synthetic
plans, assignments, payroll changes, or provider writes are made. The selected
stop supplies the default destination and the pickup or delivery appointment. DISPATCHED defaults to
pickup; ENROUTE/RESERVED default to delivery. The Loads panel selects intermediate
stops. Entering a uniquely matched load is the dispatcher's operational assignment,
even when DataTruck lists another driver or truck: status defaults to DISPATCHED and pickup
location/appointment display immediately, with no additional confirmation. Existing
selections also remain usable if DataTruck changes its driver or truck. This does not rewrite
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
stale or changed targets are rejected. Routine `syncedAt` updates are excluded
from the source revision in all load groups, so an unchanged import does not block
saving/clearing an open editor. Actual identity, stop/schedule, queue and
board-version changes still invalidate stale edits. The load number and remaining
cell open the queue. A known pickup without a source schedule shows `PU —`; unmatched loads
cannot be edited. Overrides use the imported-load/stop identity, survive source
refresh and promotion to current, and clearing restores the current source time.
This action changes no current-load selection, ETA, progress or provider data.

## Arrival and manual input

The first provider position within five straight-line miles of the selected stop
shows @PU/@DEL and latches that marker for the exact load/stop/truck context. A
newer position beyond five miles does not clear a reached stop; selecting another
stop, load or truck resets the evidence. This is a proximity signal, not a claim
that the truck reached the dock.
It uses the cached GPS coordinates and selected stop coordinates without requiring
optional road routing. Future/out-of-order positions are ignored. An unchanged
cached position is re-evaluated after a rule change, including when the provider
has not sent a new point. Positions older than ten minutes show only last-known
arrival, with a yellow marker. Later outside positions do not refresh the original
arrival observation time. Automatic
proximity requires a uniquely identified assigned truck VIN. Driver/truck assignment identity,
VIN, trailer, load ID, selected stop and facility coordinates scope evidence.

Fresh proximity displays a green @PU/@DEL prefix. Outdated proximity uses yellow.
While @PU/@DEL is displayed, the ETA cell hides the retained travel estimate,
which returns after the operational context advances and the next stop has not yet
been reached. Exact context-fenced cached route origins inside the same radius can
recover arrival markers that older releases cleared after departure.
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
Automatic travel and proximity use green text for fresh data and yellow text for
outdated data, with freshness labels for screen readers. Manual times and
mileage use the normal text color. The API exposes travelStale and
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

## Budgeted TomTom and Google car-route estimates

Default off. No routing or speed-tracking calls are made until enabled.
The November 4 policy uses TomTom first up to 20,000 monthly attempts, then
Google Essentials up to 10,000. After both allowances, the explicitly configured
paid provider continues routing. Production selects TomTom: verified standard
overage pricing is $1/1,000 versus Google's $5/1,000. Prices are operator-verified,
not scraped at runtime; recheck account rates before changing the paid provider.
Sources: https://docs.tomtom.com/pricing and
https://developers.google.com/maps/billing-and-pricing/pricing.

Google uses DRIVE with TRAFFIC_UNAWARE and duration/distance only. TomTom uses
Calculate Route with car, traffic=true, departAt=now, fastest, summaryOnly and no
alternatives. Neither requests truck restrictions or toll prices. Stored travel
assumptions identify the provider and traffic mode. Never put keys in browser variables.

Server environment:

```dotenv
BOARD_ROUTES_ENABLED=false
BOARD_ROUTES_API_KEY=...
TOMTOM_ROUTING_API_KEY=...
BOARD_ROUTES_ACCESS_VERIFIED=true
BOARD_ROUTES_OPERATIONS_VERIFIED=true
BOARD_ROUTES_REQUESTS_PER_MINUTE=30
BOARD_ROUTES_TOMTOM_MONTHLY_FREE=20000
BOARD_ROUTES_GOOGLE_MONTHLY_FREE=10000
BOARD_ROUTES_PAID_PROVIDER=tomtom
BOARD_ROUTES_TOMTOM_BILLING_TIMEZONE=UTC
FIVE_ELD_SPEED_VERIFIED=true
FIVE_ELD_SPEED_UNIT=mph
```

GOOGLE_ROUTING_API_TOKEN is also accepted when BOARD_ROUTES_API_KEY is empty.
Adding either key alone does not activate the worker; verification flags, existing
Five ELD configuration and verified mph/kph speed semantics remain required.
Flags are operator attestations, not an API-access test. Legacy
BOARD_ROUTES_STANDARD_SEMI and BOARD_ROUTES_PROFILES_FILE settings are ignored;
car estimates do not require truck profiles. Keep real keys out of commits,
including tracked development environment files.

Migration 197 adds board_route_usage, keyed by provider and billing month, with
requests, paid_requests and last_attempt_at. It is unrelated to payroll. A separate
transaction advisory lock serializes provider selection and commits each attempt
before HTTP. Restarts, multiple workers, changed destinations and failed/unknown
responses cannot bypass counting. No refunds or immediate cross-provider retries
occur. Database failure stops routing. Redirects are disabled; TomTom GETs use
fresh connections to prevent automatic replay after a reused connection fails.

Google months use America/Los_Angeles. TomTom defaults to UTC; configure its actual
account billing timezone if different. New month rows start at zero, retaining
earlier counters. Counters cover this worker only. Before activation, reconcile
existing same-SKU account usage into the current month rows, or lower the free
thresholds to reserve that usage. Previous Google traffic-aware Pro calls use a
different SKU from the new Essentials requests. Failed attempts are conservatively
counted even when the provider does not bill them.

Free thresholds can be lowered, including zero, but not raised above the published
allowances. A missing key excludes that provider; a configured paid provider must
have a key. BOARD_ROUTES_PAID_PROVIDER defaults to off (hard stop after free usage);
tomtom or google explicitly permits paid continuation. Production uses tomtom per
operator instruction. Provider-side billing/prepaid balance must permit overage.

Read-only operator usage query:

```sql
SELECT provider, month_start, requests, paid_requests, last_attempt_at
FROM board_route_usage ORDER BY month_start DESC, provider;
```

Disable BOARD_ROUTES_ENABLED and restart to stop routing; cached automatic values
are disabled by the next worker pass. Manual entry remains usable. Matched
selections with missing/invalid GPS still report routing as enabled and explain
why an estimate is unavailable. Valid stale coordinates can bootstrap last-known
road miles without querying tracking data or inventing movement.

The minute worker retains independent last-success times and route origins for
each board truck/driver context. Routine routing requires BOTH 90 minutes since
the last successful calculation AND at least one straight-line mile from that
successful route origin. Skipped polls never reset either baseline. Movement is
checked against shared cached Five ELD coordinates before any tracking request,
then checked again against the actual tracking point before calling the route provider.
Unchanged or sub-mile positions reuse the cached distance, even after a long rest.

A changed load, selected stop, destination address or coordinates, assigned
truck/crew/VIN or trailer bypasses the timer and movement threshold on the next
minute pass. Changed contexts take queue priority over routine refreshes.
Typing a custom destination routes to that address while keeping the selected
stop's DataTruck appointment and manual pickup/delivery time. The source-stop
arrival marker is suspended during the override. Clearing the address restores
the selected stop as the routing destination.

Custom addresses use a Google Routes address waypoint under the existing Google
allowance. Once that allowance is exhausted, paid custom routes require
`BOARD_ROUTES_PAID_PROVIDER=google`. Partial or unmatched addresses produce
an address correction message and wait for a changed routing context before
retrying. Custom address ETA is shown in New York time. Fresh movement of at
least one mile can immediately replace a
bootstrapped stale-position distance. Failed attempts retain a five-minute
retry fence. Migration from old truck-route caches establishes a new DRIVE
baseline once; no database migration is needed.

The worker has a forty-second run budget. Requests remain serial and globally
paced per active worker (default 30/minute, configurable 1-120), with fifteen-second
provider timeouts and one-minute error/five-minute 429 cooldowns. Queue capacity and
provider errors can delay a refresh beyond the next minute pass. Five ELD requests
reuse the client's roster identity cache; source mileage values are never rewritten.

Distinct fresh tracking points establish moving at >=5 mph or >=100m movement.
Stopped requires <=2 mph with movement under 100m for at least three minutes;
intermediate speeds retain the previous classification. Speed units are normalized
only after provider verification. Moving displays road miles remaining above 600
miles, otherwise New York ETA from observation time plus routed duration. Exactly
600 miles displays ETA; displayed miles round up. Stopped always displays road
miles remaining. GPS older than ten minutes shows last-known miles instead of ETA,
regardless of the last movement classification. Preserve them through stale
GPS/rest periods, with the provider observation time and stale warning on hover.
If no route existed when the truck went offline, calculate distance from the
shared cached location. This never establishes stopped/ignition state, advances
arrival, or produces an ETA from stale coordinates. New load/stop/truck contexts
clear the old distance and route to the newly selected stop. A fresh stationary
point after a long rest does not discard the last stopped state. Unknown fresh
movement or missing/invalidated routes suppress
output. Temporary errors retain a prior valid distance with a hover warning;
live moving ETAs remain valid between 90-minute refreshes while shared GPS is fresh,
with a five-minute route-age grace period. Older routes show last-known miles.
Results are fenced against the selected load/stop/source revision
and current truck before committing.

When enabled, a blank ETA displays the automatic estimate. Applying a manual
time or mileage takes precedence; clearing it restores the automatic estimate.
These are provider-specific travel
estimates, excluding driver breaks, remaining HOS, planned stops and loading delays;
there is no explicit weather-forecast adjustment.

## Validation and operation

Run repository Go tests, including TestBoardSchedules/TestArrivalEvidence/
TestTrackingMotion, TestBoardTravelRefreshSchedule,
TestBoardTravelFreshnessBetweenRefreshes and boardroutes tests.
TestDriverBoardDatabase asserts paid/tracking request counts for stationary,
sub-mile, timed refresh and changed-destination cases, and covers migration
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

Migration 197 requires the matching deploy helper before installation. It stops
writers for the routing usage classification and rejects rollback to binaries
without `-board-routing-usage-capability` support. The SQL classification and Go
payroll exclusion list must both include `board_route_usage`; fresh/migrated
Status Board database tests verify the production payroll coverage check.
