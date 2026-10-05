# Status Board implementation plan

Updated October 5, 2026 to reflect the accepted, shipped behavior. The page is
named Status Board (route /driver-board). Phases 0–3 are shipped: colors, history,
personal views, current/next loads, destinations, status-click progress and
personal Ctrl+Z. The Loads panel refinements and previous-load ordering are also
shipped. Phase 5 Five ELD location/freshness is shipped independently of Phase 4.
Phase 4 (shift handoff) is next and still needs a mockup review; later automation
and home-time items remain unimplemented.

This file records the current design and remaining work. Earlier proposals that
were superseded are not instructions to change the accepted app behavior.

## Agreed direction

- Keep the familiar status names and meanings. Updaters maintain the board;
  DataTruck is normally updated only when a load is delivered. It cannot be
  treated as a live pickup/progress feed.
- Keep table fields on one line. Prioritize the columns through ETA; horizontal
  scrolling is acceptable for later columns. Use a side panel for detail.
- Approved: ordered next loads, status-click progress, change history,
  personal work views, and live location/freshness once Five ELD is connected.
- Keep progress on the existing status label and the manual dropdown on its
  chevron. Add no extra progress buttons, tooltips or instructional explanations;
  the user will train dispatchers and workers separately. Driver-type codes need
  no explanatory legend.
- Shift handoff is promising: start with a lightweight design and review it
  before building a more elaborate workflow.
- Later: home-time planning and load advancement triggered by verified external
  delivery signals. User-triggered advancement through status clicks is shipped.
- Excluded: last-driver-contact tracking, attention queue, document uploads.
  Do not introduce mandatory calls, check-in logs, task queues, or duplicate
  document entry. Documents remain in DataTruck.

## Phase 0 — familiar colors (shipped)

Status cell colors follow TODAY column J effective formatting from
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

Apply these fills to the status cell, with dark text for contrast. Manual
dropdown options use neutral colors. Preserve existing colors for statuses
without a sheet equivalent; REST and RESET are not assumed interchangeable.

## Phase 1 — history and personal views (shipped)

### Change history

A small history action for each driver opens a right-side panel with a switch
to Loads. It preserves ordinary input clicks and the driver's profile link.
A toolbar History button opens the board-wide log,
filtered to the current view. Show timestamp, actor, field, and old/new values.
Group field differences from one saved update into one event.

Persist append-only events in the same transaction as board changes, recording
the authenticated user on the server. Include driver-home edits from both the
board and profile, queue edits and progress actions. Record the authenticated
actor, source and stable driver/load identities. Attribution for future
DataTruck/Five ELD integration writes remains part of those integration phases.
History starts at rollout; do not invent authors or events for existing data.
Preserve events if a driver becomes inactive and retain identity/name snapshots
after deletion. Authorize history reads and reversals explicitly.

Undo creates a new version-checked correction event, never deletes history or
silently overwrites someone else's newer change. No-op autosaves add no events.

### Personal work views

Add compact All drivers / My view controls beside the existing filters. Keep
the same board layout, metrics, fields, and edits. My view remembers selected
dispatchers; updaters can cover multiple dispatcher groups. Offer saved views
such as Morning coverage without adding a separate updater-assignment workload.

There is no automatic account-to-dispatcher default; any future default requires
explicit links, never name inference. Save view preferences per user using existing
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

## Phase 2 — current load, ordered next loads, and destinations (shipped)

Keep Current load compact and add a single-line Next loads cell showing
Load B → Load C (+N). Each load without a system match has red text; the cell
background stays unchanged and matched loads retain their normal text color.
Put detail/reorder controls in the Loads side panel.
Fit it by reviewing widths; do not wrap rows or push the essential ETA column
offscreen merely to show the entire queue. Keep freeform Notes intact.

Build the queue from Gross Board plans across week boundaries, keyed by stable
plan and linked load identities. Default to board date/slot ordering and allow
explicit correction. Board placement may be an
accounting date, not an actual pickup sequence. Exclude deleted entries, day
statuses and repeated appearances of the same linked load; retain unmatched
plans visibly until they can be linked. Do not conflate duplicate load numbers.

Users enter a current load, choose a current-week suggestion, or confirm a
selection in Loads. An exact unambiguous number from that driver's current-week
Gross Board links automatically, including loads from earlier days of the week.
A calendar date alone never activates a load. New current loads start DISPATCHED
at pickup. The agreed progress cycle has two stops: pickup and delivery.
Show the relevant location from imported DataTruck details in Origin /
destination. Keep appointment and ETA distinct, and allow a manual destination
override with a return-to-source action.

Reconcile plan edits, removals, reassignment, canceled loads, and reordering
without losing the currently active load or silently changing historical gross,
miles, rates, or payroll placement. Persist queue order separately from financial
board dates. A removed current plan requires review rather than silent deletion.

Acceptance: pickup/delivery, duplicate-number, unmatched-plan, cross-week and driver
reassignment scenarios work; opening a board never advances a load; notes and
financial figures stay intact.

Implemented in migration 048: both Gross Board slot tables have plan UUIDs,
retained through financial edits but renewed when slots change load identity,
become statuses or are removed. Queue order, exclusions, selected load snapshot
and stop/source choice live in separate operational storage. Opening the board
does not activate or advance a load. Current-load edits clear the old selection
and resolve an exact unambiguous current-week match; unmatched text remains
editable without a source location. Manual destination edits switch off the source. Older-client text
disagreements retain a review warning and suppress the automatic destination.

Next loads is a single-line column after ETA. The Loads panel opens from Current
load or Next loads, with a History switch, current-load selection confirmation,
up/down ordering, reset, removal/restoration, source/manual destination selection,
known source stops and first-pickup/final-delivery appointments. ETA is entered
manually and cleared on delivery/promotion as described in phase 3.
Next loads start today in New York and include future plans.

The Loads panel shows a compact expandable sequence: upcoming loads above the
current load, followed by previous plans below it. Previous plans run newest
first, so the most recent sits directly below current and the oldest at the
bottom. The current linked load is marked Current and expanded by default.
Opening another load collapses the previous detail; collapsed cards show just
the load number, with the Current marker retained. Unmatched load numbers also
use red text in the panel. The old Earlier unfinished plans section is gone;
previous plans can be selected directly from their expanded cards. Removed
queue items and delivered/cancelled items remain available in their separate
sections outside the active sequence.

Imported stop locations display City, ST without ZIP codes while
source values, stop keys and manual text remain intact. A click on the source
destination switches pickup/delivery using the first located stop of the opposite
type; a muted PU/DEL label identifies the displayed stop. Source-stop selection
remains in Loads; imported extra stops do not add stages to the two-stop progress
cycle. Location switches are saved/audited without status or ETA changes.
Current selections remain visible across midnight. Manual ordering
does not make past plans upcoming; stale cutoff dates are clamped to today, and
pre-midnight actions must refresh after the queue changes. Weekly totals still
cover Monday–Sunday.
Gross Board owns planned driver assignment; a disagreeing DataTruck driver is
shown for review, not used to silently move or discard the plan. Delivered or
cancelled next loads remain inspectable outside the active queue. A current load
is retained for review even if its source plan disappears or source status finishes.

Actions use the existing driver-row version plus a revision of the displayed
plans, audit atomically, and support history undo. They do not write DataTruck
or alter Gross Board placement/payroll. New-current selection and status-click
progress update operational status as described in phase 3. Merely opening the
panel, reordering loads or switching the displayed location does not record
pickup or delivery; ENROUTE/RESERVED follows next-load presence.
Live details use the existing imported DataTruck snapshot and display its sync
time; this release adds no upstream polling or ELD connection.

Validation covers fresh and migrated PostgreSQL schemas as mserp_app,
the full database-enabled Go suite and vet, frontend lint/calculation checks and
production build, and Chromium flows for selection, multi-stop destinations,
manual/source switching, cross-week order/undo, removed current plans, stale-source
conflicts, read-only access and compact desktop/mobile layout. Recent releases
were validated in CI, with local tests skipped at the user's request.

October 2 layout refinement: remove the repeated Dispatcher column (group labels
and filters remain). ETA uses a compact cell-anchored popup with a date picker
and optional time in New York time. Personal view controls follow All statuses
in the filter row; the current week label sits beside the page actions.
Cells stay single-line, for example `10/3 · 2:30p` or `10/3` when time is unknown.
New values use `YYYY-MM-DD` or `YYYY-MM-DDTHH:mm` in the existing text field;
legacy free text stays intact until replaced or cleared. Existing autosave,
history and permissions apply. The editor pauses idle refresh to avoid replacing
its underlying row during a draft.

Phone cells wait 500ms before copying the +1 number on a single click. A double-click
cancels copying and starts a RingCentral call directly using rcapp://r/call.
There is no separate call button. Keyboard
activation of the number copies immediately. The rcapp link avoids a misconfigured
Windows tel default (Chrome on the operator's PC).

Current-load typing suggests the driver’s Gross Board load numbers for the current
week. Choosing a suggestion or typing an exact unambiguous number previews and
autosaves the current load at its pickup with DISPATCHED status. All days in the week remain eligible, including earlier delivered loads;
the Next queue still starts today. Clearing the number clears its location and
source and status, and changing it removes the previous location. ETA stays
unchanged on manual entry. The location cell still switches PU/DEL; the Loads panel supports
choosing specific stops and manual destinations.

## Phase 3 — status-click progress and personal undo (shipped)

October 4 clarification: DISPATCHED means heading to current pickup. ENROUTE
means the current load is picked up with nothing next; RESERVED means picked
up with a next load available. Each load follows one pickup and one delivery.

Implemented without extra board buttons, tooltips, or instructional text:

- A new current load starts DISPATCHED and shows its pickup.
- Clicking DISPATCHED records pickup and shows delivery; next-load presence
  determines ENROUTE or RESERVED. These two labels also follow queue changes
  during refresh; HOME/SHOP/VACATION and other manual exceptions stay manual.
- Clicking ENROUTE/RESERVED completes the current load and starts the first
  available next load as DISPATCHED at pickup. Without a next load, current
  load, location, ETA and status become blank; never infer NO LOAD.
- Only the chevron opens the manual dropdown. Options have neutral colors;
  the cell retains the familiar status fill. Destination clicks remain display
  switches and never record pickup/delivery.
- Completed identities are stored with operational load state, preventing the
  finished load from returning to Next while DataTruck still says Dispatched.
  Gross Board, source load status and financial values are never rewritten.
- Version and plan-revision checks reject stale/repeated advances. Status
  double-clicks advance once. Clearing stale ETA on promotion avoids carrying
  the prior delivery ETA onto a different load.

Ctrl+Z (Cmd+Z on Mac) reverses this page's own saved actions, using event IDs
returned by its successful writes. The in-memory stack lasts for this page
session, is never populated from global history, and does not include another
browser's actions. The server verifies ownership for personal undo. Consecutive
undos may traverse this user's already-reversed event pairs; another person's
later overlapping edit still blocks undo. Every correction remains audited.
Pending cell edits can be undone before saving; native text undo remains active
while typing. A shortcut during a save waits for that save. Loads-panel actions
use the same stack. No extra undo button or instructional tooltip is added.

## Phase 4 — lightweight shift handoff (next; design review pending)

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

## Phase 5 — Five ELD location and freshness (shipped)

Five ELD was previously TT ELD. Its public documentation describes current
locations, timestamps, VINs, unit assignments and historical tracking:
https://fiveeld.com/api-docs/

Implemented in migration 050 and the server-side Five ELD client. Authentication
uses both the company API key and integration-provider token; credentials and
USDOT stay in the backend environment. The five-minute default poll makes one
fleet position request, matches active assigned trucks only by normalized VIN,
and caches the latest provider coordinates in PostgreSQL. Unmatched, ambiguous
and invalid units are counted for review and never rewrite fleet assignments.

The table places Latest location immediately before Destination. It shows the
latest known provider coordinates regardless of age and is blank only when no
safe matched location is available. A click copies the displayed coordinate pair; the
exact provider update time appears only on hover. Destination continues to mean
the current load stop/manual destination. The Loads panel shows the same latest
coordinate and map. Last known data survives provider failures in the server
cache and remains visible even when its timestamp is old. There is no age cutoff.

Ordinary Driver Board refreshes only reread the shared MSERP cache, so concurrent
viewers do not multiply provider requests. `POST /jobs/sync-eld` provides
an authorized targeted refresh, while the backend interval worker polls at the
configured cadence. No telemetry update changes financial data, assignments,
load status or progress, and the UI makes no ETA or HOS claim. Targeted active
DataTruck load refresh remains separate future work.

Acceptance covers correct VIN mapping, latest coordinate display and copying,
stale/offline units, changed assignments, provider outages/rate limits and access
controls. No telemetry poll modifies financial records or causes a load transition.

## Later — confirmed automation and home-time planning

First observe delivery signals and compare with actual updater actions. DataTruck
delivery events can eventually confirm completion when semantics and freshness
are proven. Older updates must never move progress backward or undo a manual
correction. GPS geofences can indicate arrival/departure, but departure alone is
not proof of unloading. Preserve manual overrides until explicitly cleared or
their defined load-specific scope ends.

Advancement without a user click remains deferred until external delivery
signals are reliable and ordering is unambiguous. The existing status-click
cycle and audit/undo stay available. Handle cancellations, reassignments,
duplicate events and reopened deliveries explicitly, using the agreed
pickup/delivery cycle.

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

Remaining build order: handoff mockup and review → handoff implementation.
Five ELD location display is independent of automated status transitions.
Home-time planning remains deferred.
