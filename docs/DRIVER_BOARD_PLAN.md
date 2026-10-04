# Driver Board implementation plan

Decisions recorded October 2, 2026. Phases 0–1 (colors, history and personal views)
shipped in f0ce1a9. The page is now named Status Board (route /driver-board).
Phase 2 (current selection, ordered next loads and source destinations) is the
current implementation pass. Progress actions, handoff and ELD remain later phases.

## Agreed direction

- Keep the familiar status names and meanings. Updaters maintain the board;
  DataTruck is normally updated only when a load is delivered. It cannot be
  treated as a live pickup/progress feed.
- Keep table fields on one line. Prioritize the columns through ETA; horizontal
  scrolling is acceptable for later columns. Use a side panel for detail.
- Approved: ordered next loads, explicit progress buttons, change history,
  personal work views, and live location/freshness once Five ELD is connected.
- Shift handoff is promising: start with a lightweight design and review it
  before building a more elaborate workflow.
- Later: home-time planning and reliable automatic load advancement.
- Excluded: last-driver-contact tracking, attention queue, document uploads.
  Do not introduce mandatory calls, check-in logs, task queues, or duplicate
  document entry. Documents remain in DataTruck.

## Phase 0 — familiar colors

Read TODAY column J effective formatting and conditional-format rules from
[the source sheet](https://docs.google.com/spreadsheets/d/1mDQM6jUWGgagvI-hHT4WbNhDiOYsOvyiOIDqvhUIhk0/edit?gid=1851742271#gid=1851742271).

| Existing status | Sheet fill |
| --- | --- |
| ENROUTE | #3d85c6 |
| DISPATCHED | #fbbc04 |
| RESERVED | #b7e1cd |
| HOME | #ff0000 |
| VACATION | #e06666 |
| SHOP | #ff9900 |
| LEFT (sheet: Left) | #ffffff |

Apply these fills to the status cell and dropdown options, with dark text for
contrast. Preserve existing colors for unmatched statuses; do not guess that
REST and RESET are interchangeable or add new status values in this change.

## Phase 1 — history and personal views

### Change history

Add a small history action for each driver. It opens a right-side panel with
history now, with a Loads tab deferred to phase 2; it must not hijack ordinary input clicks or replace
the driver's profile link. A toolbar History button opens the board-wide log,
filtered to the current view. Show timestamp, actor, field, and old/new values.
Group field differences from one saved update into one event.

Persist append-only events in the same transaction as board changes, recording
the authenticated user on the server. Include driver-home edits from both the
board and profile, and later queue edits, progress actions, and integrations.
Record the source (person, DataTruck, Five ELD) and stable driver/load identities.
History starts at rollout; do not invent authors or events for existing data.
Preserve events if a driver becomes inactive; determine retention/deletion rules
before adding foreign keys. Authorize history reads and reversals explicitly.

Undo creates a new version-checked correction event, never deletes history or
silently overwrites someone else's newer change. No-op autosaves add no events.

### Personal work views

Add compact All drivers / My view controls beside the existing filters. Keep
the same board layout, metrics, fields, and edits. My view remembers selected
dispatchers; updaters can cover multiple dispatcher groups. Offer saved views
such as Morning coverage without adding a separate updater-assignment workload.

Use explicit account-to-dispatcher links for an automatic dispatcher default;
never infer ownership from names. Save view preferences per user using existing
viewMemory for tab persistence; add server preferences only if cross-device
saved views are needed. View filtering is not an authorization boundary. Preserve
all API permission checks and clarify whether restricted fleet scope is wanted
as a separate requirement.

Acceptance: two users can use different views without affecting each other;
metrics match visible drivers; edits remain shared; concurrent changes and undo
are safe; read-only users cannot mutate through history controls.

Implemented: migration 046 audits board/profile-home writes in their transaction,
with server-attributed actors and no-op suppression. The right-side history panel
supports cursor pagination and explicit undo confirmation. Undo checks both current
versions and later edits of every affected field, preserving unrelated later work.
Read-only users can inspect history but cannot undo. Events retain identity/name
snapshots after deletion. My view and named views remember selected dispatcher
groups per user in the current tab; there is no inferred account assignment or
cross-device synchronization. Existing search/status/dispatcher filters still apply.

Validation: fresh and migrated disposable PostgreSQL schemas as mserp_app, API
permission checks, browser history/profile undo, user view isolation, reload
persistence, filtered totals, mobile layout, existing autosave/concurrency tests,
Go tests/vet, frontend lint, calculation tests and production build.

## Phase 2 — current load, ordered next loads, and destinations

Keep Current load compact and add a single-line Next loads cell showing
Load B → Load C (+N). Put its detail/reorder controls in the Loads side panel.
Fit it by reviewing widths; do not wrap rows or push the essential ETA column
offscreen merely to show the entire queue. Keep freeform Notes intact.

Build the queue from Gross Board plans across week boundaries, keyed by stable
plan and linked load identities. Default to board date/slot ordering, but label
that as planned order and allow explicit correction. Board placement may be an
accounting date, not an actual pickup sequence. Exclude deleted entries, day
statuses and repeated appearances of the same linked load; retain unmatched
plans visibly until they can be linked. Do not conflate duplicate load numbers.

Current load must be explicitly selected/confirmed during initial setup. A
calendar date alone must not activate it. Once selected, resolve pickup,
intermediate stops and final delivery from DataTruck details. Show the relevant
stop in Origin / destination. Keep appointment and ETA distinct, and allow an
explicit manual override with a return-to-source action.

Reconcile plan edits, removals, reassignment, canceled loads, and reordering
without losing the currently active load or silently changing historical gross,
miles, rates, or payroll placement. Persist queue order separately from financial
board dates. A removed current plan requires review rather than silent deletion.

Acceptance: multi-stop, duplicate-number, unmatched-plan, cross-week and driver
reassignment scenarios work; opening a board never advances a load; notes and
financial figures stay intact.

Implemented in migration 048: both Gross Board slot tables have plan UUIDs,
retained through financial edits but renewed when slots change load identity,
become statuses or are removed. Queue order, exclusions, selected load snapshot
and stop/source choice live in separate operational storage. Existing text is
never automatically matched or promoted. Manual current-load edits detach the
selection; manual destination edits switch off the source. Older-client text
disagreements retain a review warning and suppress the automatic destination.

Next loads is a single-line column after ETA. The Loads panel opens from Current
load or Next loads, with a History switch, explicit current/stop confirmation,
up/down ordering, reset, removal/restoration, source/manual destination selection,
all known stops and first-pickup/final-delivery appointments. ETA stays manually set.
Next loads start today in New York and include future plans. Earlier unfinished
plans are available in a separate collapsed section and can be explicitly selected
as current. Imported stop locations display City, ST without ZIP codes while
source values, stop keys and manual text remain intact. Current selections remain visible across midnight. Manual ordering
does not make past plans upcoming; stale cutoff dates are clamped to today, and
pre-midnight actions must refresh after the queue changes. Weekly totals still
cover Monday–Sunday.
Gross Board owns planned driver assignment; a disagreeing DataTruck driver is
shown for review, not used to silently move or discard the plan. Delivered or
cancelled next loads remain inspectable outside the active queue. A current load
is retained for review even if its source plan disappears or source status finishes.

Actions use the existing driver-row version plus a revision of the displayed
plans, audit atomically, and support history undo. They do not write DataTruck,
alter Gross Board placement/payroll, mark stops complete or infer a new status.
Live details use the existing imported DataTruck snapshot and display its sync
time; this release adds no upstream polling or ELD connection.

Validated locally against fresh and migrated PostgreSQL schemas as mserp_app,
the full database-enabled Go suite and vet, frontend lint/calculation checks and
production build, and Chromium flows for selection, multi-stop destinations,
manual/source switching, cross-week order/undo, removed current plans, stale-source
conflicts, read-only access and compact desktop/mobile layout.

October 2 layout refinement: remove the repeated Dispatcher column (group labels
and filters remain). ETA uses a compact cell-anchored popup with a date picker
and optional time in New York time. Personal view controls follow All statuses
in the filter row; the current week label sits beside the page actions.
Cells stay single-line, for example `10/3 · 2:30p` or `10/3` when time is unknown.
New values use `YYYY-MM-DD` or `YYYY-MM-DDTHH:mm` in the existing text field;
legacy free text stays intact until replaced or cleared. Existing autosave,
history and permissions apply. The editor pauses idle refresh to avoid replacing
its underlying row during a draft.

Current-load typing suggests the driver’s Gross Board load numbers for the current
week. Choosing a suggestion fills the text through normal autosave and preserves
status, ETA and destination; source/stop linking still uses the Loads panel.

## Phase 3 — explicit progress actions

Clicking the status cell continues to open the dropdown. Add clearly named
actions for the selected current load in its compact controls/Loads panel:

- Picked up: for a DISPATCHED load, record pickup and change the familiar board
  status to ENROUTE, then show the next delivery stop. Do not introduce an
  IN-TRANSIT label in the board.
- Delivered: record completion of the selected delivery stop. Intermediate
  delivery stops do not finish the whole load.
- Delivered & start [next load]: when the final stop is complete and the next
  load is unambiguous, provide an explicitly named combined action. Promote
  that load and show its pickup. Promotion does not mark the next load picked up.
- With no next load or ambiguous sequencing, finish the current load without
  inventing a replacement or availability state. Keep HOME/SHOP/VACATION and
  other manual exceptions under updater control.

RESERVED can mean a next load is booked while the current one is still moving.
Store physical progress separately from the familiar board status and future
reservation. Review how the visible RESERVED label should behave on Picked up
before enabling that action for RESERVED rows. Likewise agree the status shown
after final delivery with no next load; do not guess HOME or NO LOAD.

Actions save atomically with queue changes and history, use idempotency and
version checks, and expose correction/undo. Repeated clicks or stale responses
must not advance twice. No writes to DataTruck are included in this phase.

## Phase 4 — lightweight shift handoff

Proposed location: a Handoff button in the board toolbar opens a right-side
panel, using the same current dispatcher/personal view. Review a mockup first.

Default to Changes since [time], generated from saved history, with driver,
current/next load and changed notes/status/ETA. This requires no separate updater
log. Allow an optional handoff note for context that existing fields cannot
express. If a note is pinned for the next shift, let the user explicitly clear
it; do not silently treat old notes as resolved. Group repeated edits and show
final values so routine typing does not overwhelm the summary.

This is a handoff view, not an attention queue, mandatory checklist, last-contact
tracker or notification system. Do not send messages or schedule recurring jobs.

## Phase 5 — Five ELD location and freshness

High priority, blocked on integration access rather than lack of interest.
Five ELD was previously TT ELD. Its public documentation describes current
locations, timestamps, VINs, unit assignments and historical tracking:
https://fiveeld.com/api-docs/

Confirm company coverage, API key/provider-token requirements, limits, timestamp
semantics and available fields before implementation. Keep credentials server-side.
Match by VIN, review unmatched/ambiguous units, and never automatically rewrite
fleet assignments from telemetry. Add a compact location/age display and a map
inside the Loads panel. Do not confuse truck location with load destination.

Poll active units at a provider-approved cadence with bounded requests, retries,
backoff and freshness timestamps. Preserve the last known position during
outages and clearly mark it stale. Show when location is unavailable. No claim
of live ETA or remaining driving hours unless the required routing/HOS data
is actually available and verified.

Refreshing Driver Board only rereads MSERP; it does not refresh upstream data.
Add targeted refresh for active DataTruck loads separately from the existing
daily accounting sync. Avoid continually reimporting the entire load history.

Acceptance: correct VIN mapping, stale/offline units, changed assignments,
provider outages/rate limits, and access controls. No telemetry poll modifies
financial records or causes a load transition.

## Later — confirmed automation and home-time planning

First observe delivery signals and compare with actual updater actions. DataTruck
delivery events can eventually confirm completion when semantics and freshness
are proven. Older updates must never move progress backward or undo a manual
correction. GPS geofences can indicate arrival/departure, but departure alone is
not proof of unloading. Preserve manual overrides until explicitly cleared or
their defined load-specific scope ends.

Only automate next-load advancement after reliable final-stop completion,
unambiguous ordering and audit/undo exist. Handle cancellations, reassignments,
multi-stop loads, duplicate events and reopened deliveries explicitly.

Defer home-time planning until structured home-time requests, load timing and
route estimates exist. Current text fields remain editable meanwhile.

## Delivery and verification

Ship each phase independently after review of its UI. Keep backend repository,
HTTP handlers and frontend API/types synchronized. New tables need init.sql and
numbered migrations, mserp_app permissions, and backward-compatible rollout.
Use disposable databases for concurrency/history/queue tests and browser flows
for save, undo, permissions, navigation, personal views and compact layout.
Integration tests should simulate delayed, duplicated and unavailable upstream
data. Never use production records to exercise progress mutations.

Suggested build order: history foundation and My view → next loads/destination
→ progress actions → handoff. Prepare Five ELD access alongside these phases;
location display can ship once access is ready without waiting for automated
status transitions. Home-time planning remains deferred.
