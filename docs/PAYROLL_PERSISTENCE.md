# Persistent payroll implementation

## Release status

This release implements persistent Driver Pay and Investor Pay, building on
checkout `44c7f75`. Runtime capabilities default off. Installation and activation
are separate: record the deployed commit, CI result, reconciled cohort and final
protocol using the verification procedure below.

The user removed the 300 ms routine-action latency requirement and requested that
optimization conclude with practical measured results. The original latency targets
remain useful comparisons, not claims that this implementation meets them. Financial
correctness, complete atomic publication, stale-input rejection and recovery remain
mandatory. No automatic financial retry against a newer revision is allowed.

The user also removed the whole-week finalization button from both pages.
Individual finalization and whole-week reopening remain. The compatible bulk API
retains combined validation, ordering, individual audits and all-or-nothing rollback.
Benchmarks prepare finalized fixtures through that API outside timing, then measure
the actual Reopen week workflow.


Migration 183 classifies `maintenance_telemetry` as unrelated to payroll and removes
only its payroll capture/protocol triggers. Ordinary cache revisions and telemetry
records remain. Neither calculator reads this table; truck ownership/terms and
expenses remain financial sources. This prevents periodic odometer refreshes from
invalidating every prepared week. The migration requeues retained periods without
changing immutable history or canonical financial records. Install the updated
deploy helper first: it pauses all writers and refuses rollback to a release lacking
migration 183 once the classification changes, even if migration-ledger recording
fails. Fresh/incremental tests check both report parity and financial invalidation.

## Implemented stages

| Stage | Implementation | Release verification |
| --- | --- | --- |
| 0: contract and capture | Classified source registry, old/new routing capture, complete trigger-event checks, independent authoritative calculator, explicit capability gates | Fresh/incremental coverage and writer audit |
| 1: durable preparation | Migration 182, immutable versions, exact dependencies, leased/coalesced jobs, bounded retries, crash recovery, frozen snapshot backfill | Shadow parity and retained cutover cohort |
| 2: persistent reads | Stored summary/detail/evidence, repeatable snapshot, retained version reads, preparation/failure/retry states, drafts and view restoration | Zero calculator calls; 2 summary / 3 expanded payroll-data queries |
| 3: incremental preparation | Normalized trip/routing facts, exact balance buckets, captured old/new components, separate dated scopes, conservative fallback when completeness is unknown | Full-input parity including dated teams, owners, future reservations, expense boundaries and historical components |
| 4: commands | Validated stored inputs, exact authoritative collection limits, atomic affected-version publication, durable actor-scoped receipts, exact replay, preserved implicit deductions | Financial parity, rollback, lost responses, in-flight typing and related edits |
| 5: finalization and reopening | Prepared source/resource fencing, original posting/confirmation rules, post-write frozen snapshots, explicit review/override/reason behavior, atomic bulk reopening | Single/bulk parity and second-statement failure rollback |
| 6: concurrency | Ordered cross-week resource locks, source gate, SQL claims, exact resource epochs, independent-resource concurrency, shared-resource serialization, quiesced reversible transition | Independent/shared races, source races, complete writer audit and helper recovery |
| 7: release | Compatible deploy helper, ordered runtime gates, transition CLI/helper, reconciliation and recovery procedures | Main CI/deployment, prepared cohort and production diagnostics; signed-in visual verification by the operator |

Prepared pages receive commit-only invalidation hints and retain one-second polling
for recovery. Notifications carry no financial payload and never recursively enqueue
source work. Authorized reads always validate the current snapshot. Refreshes are
coalesced and paused while drafts, commands or settlement reviews are active.
Open comment editors register their local draft immediately, before a live read can
replace readiness. A focused regression reproduces a source-update notification
while typing and verifies that Save still reaches the server on both pages.
Current HTTP reads lease preparation priority for ten seconds; that lease expires
after navigation and never advances a calculation generation.

Single-resource commands can accept intervening source changes only with complete
captured-revision and old/new routing proof outside their locked component. Missing
capture, unknown impact, duplicate-number matches and intersecting resources reject
the command. Financial epochs and the submitted statement version still must match.
Unrelated source work stays pending for the worker.

Publication shares input data only after canonical writes. Dated relationship,
expense/reimbursement/escrow, driver eligibility and investor input batches preserve
separate period scopes and defensive copies. Resource epochs are reread after writes.
Repeated load normalization now uses a durable complete-cohort marker tied to the
exact loads revision and calculator. Partial preparations cannot certify a cohort;
marker and facts commit/roll back together, and consumers still check content hashes.
Fresh/incremental tests verify corrections, deletion/reinsertion, timestamp-only sync,
rollback and a query plan that performs no load scan on a complete unchanged cohort.

The final writer audit includes ELD task-based truck creation, odometer source
selection, PTI and feedback transactions. Writers enter the source protocol before
row/subsystem locks. ELD truck creation and task completion also share native payroll
publication, including rollback when publication fails.

## Measurements and limits

The final bounded browser pass completed **504 measured actions with zero failures**
across Driver Pay and Investor Pay for 2026-09-28, 2026-10-19 and 2026-10-26.
It retained the standard 175 ms artificial API delay. Each page/week group contains
five observations per routine action/source update, two whole-week reopenings,
and 25 saves from five concurrent users. Every measured payroll GET executed zero
calculations. Full per-group p50/p95/p99, requests, queries, payloads, lock waits
and aggregate refresh timings are retained in
`performance/payroll-persistent-release-browser-performance.json`.

| Action | Observations | Observed range | Median |
| --- | ---: | ---: | ---: |
| Prepared page navigation | 30 | 823–945 ms | 863 ms |
| First expansion | 30 | 212–276 ms | 232 ms |
| Individual save | 30 | 260–1,814 ms | 279 ms |
| Individual finalization | 30 | 278–351 ms | 311 ms |
| Individual reopening | 30 | 309–435 ms | 344 ms |
| Whole-week reopening | 12 | 1,956–2,736 ms | 2,301 ms |
| External commit to visible statement | 30 | 339–1,596 ms | 660 ms |
| Five users saving during source updates | 150 | 262–781 ms | 370 ms |

These are bounded observations, not the original 200/50-sample tail estimates.
The final run reused the older Driver Pay fixture after correcting a comment-editor
race; other page/week groups had not run. Concurrent saves follow bulk setup and
therefore exercise existing allocations. Earlier first-save concurrency diagnostics
reached 10.93 seconds with 8.48 seconds of resource-lock waiting on shared historical
components. Truly independent-resource overlap and shared-balance protection are
separately verified by database races. That remaining cold/shared-resource limit
is not hidden by the faster repeated-save measurements.

The initial browser attempt found one comment save with no outgoing request while
a background update changed preparation state. The comment remained a draft, but
autosave could not proceed. Open comment editors now suspend live replacement;
the focused regression failed before the fix and passes on both pages afterward.
The final browser report above contains the successful rerun. Earlier `before`
artifacts are diagnostics, not passing release evidence.

The final local backend run passed all packages with all CI disposable-database
settings enabled (`go test -p 1 ./... -count=1 -timeout 30m`); the repository
package completed in 1,071 seconds. `go vet ./...`, frontend lint and the production
`/api` build passed. Fourteen frontend regression scripts and the isolated Linux
deployment compatibility/transition tests also passed. The separately installed
fleet-review role's capture privileges were verified on fresh and upgraded schemas.

Final artifacts use the `performance/payroll-persistent-release-*` prefix.
Earlier diagnostic artifacts record intermediate changes and may contain superseded
failures; they are not release evidence. No timed financial benchmark runs against
production. The unrelated local `backend/.env` modification is preserved.

## Production rollout safeguards

Initial shadow reconciliation exposed a retained expense dated year 0205. The
source date, amount, allocations and carried balance remain unchanged. Automatic
backfill now seeds actual source weeks, every saved/ledger period and recent weeks;
it does not expand distant dates into every intervening calendar week. Other weeks
prepare explicitly on demand. Startup removes only unmaterialized automatic jobs
without active leases when they no longer belong to that seed set. Explicit requests,
completed work and immutable versions remain. Fresh/incremental tests verify bounded
work, historical/future coverage, leases and unchanged complete financial reports.

Operator CLI calls must have the API service's environment precedence and working
directory. The protocol helper uses transient systemd units with `/etc/mserp` and
its `EnvironmentFile`; a bare login-shell invocation does not reproduce integration
files or database configuration. The Linux recovery harness verifies this launch
contract alongside failure recovery and configuration preservation.

## Capability boundaries

`PAYROLL_PROJECTION_MODE=off` is the default. `shadow` enables capture and runs
one preparation worker in the existing API process. `PAYROLL_PREPARED_READS=true`
requires shadow mode and selects the persistent HTTP adapter plus atomic native
publication. `PAYROLL_PREPARED_COMMANDS=true` requires prepared reads and uses the
validated stored card for saves. `PAYROLL_PREPARED_FINALIZATION=true` additionally
requires prepared commands. `PAYROLL_RESOURCE_LOCKS=true` requires all three and
a database already switched to `resources`; a runtime/database mismatch fails
before any startup worker or HTTP listener. Defaults remain off. Runtime wiring
is implemented for local verification; no production capability is enabled.

The worker also receives the read/command gates. Shadow mode compares every
publication with the independent full-input reference. After both gates are
enabled, proven captured components publish incrementally without a second
full-fleet calculation. Backfill and uncertain/undispatched impact still reconcile
fully. Fresh/incremental worker tests exercise both paths, verify the absence of
reference calculation on the prepared incremental path, and compare its complete
published result against a separately invoked authoritative reference.

Fuel-derived driver/truck selection edges are also persisted by exact captured
fuel/alias revisions. Weekly edge rows and an all-history completeness marker
commit together, including an empty cohort. Unknown/stale cohorts fall back to
the original relationship query. Insertions, purchase-date corrections, deletion,
previous-week routing and empty scopes are checked on fresh/incremental schemas.
This avoids rescanning historical fuel transactions at every resource discovery;
the source facts never replace current ledger balances or allocation validation.

The test-only resource-lock audit discovers cross-week driver/truck/owner scopes
and shared expense, escrow and schedule keys before acquiring ordered session
locks. It then opens a fresh financial snapshot and rejects a changed resource
set. Lock order uses sorted advisory IDs, including hash collisions; cancellation
closes a partially locked connection. Forced-overlap tests now demonstrate two
independent financial saves reaching canonical writes concurrently. Shared-invoice
commands across weeks block, then reject the second request's stale review after
the first publishes its allocation and every affected statement.
Saves and both kinds of single/bulk settlement now share the same transaction
entry point. Exact committed receipts are checked before component discovery.

Migration 182 supports `global`, `dual` and `resources` database protocol modes. Dual
statement guards take the domain gate for source writers; financial claims must
prove the current protocol epoch and actual held advisory locks. Claimed commands
cannot edit source profiles or principal values. Source mutations and resource
epoch increments roll back together. Missing/disabled protocol guards fail capture
coverage. Batched session lock calls preserve deterministic actual-ID order;
SQL claim checks inspect the held lock set once rather than once per key.

Resource commands take the source gate in shared mode, then the complete ordered
component locks before opening their financial snapshot. Source and administrative
writers take it exclusively. Existing financial guard bodies keep their collection
checks; their gate entry validates actual resource claims instead of upgrading to
an exclusive lock. Canonical resource writes advance exact epochs rather than table
counters. Publication updates immutable versions and selected statement pointers
without touching shared week/job rows. Week review tokens combine the source header
and current statement versions; legacy input tokens include the committed financial
resource snapshot. Current prepared reads still use two or three data queries.

The worker claims a complete captured component before its snapshot and derived
writes. Backfill and uncertain impact claim all principals. If a new source widens
the required group during discovery, that derived attempt is discarded. The full
reference remains enabled for controlled reconciliation and uncertain impact.

Immutable statement versions in dual mode store their resource dependencies.
Edit/finalization metadata has statement/week keys; shared balances retain
cross-week keys. Legacy overrides without canonical cost receipts also depend on
their original statement's revision. Reads, single commands and whole-week
finalization validate these dependencies even when table revisions and the queue
are unchanged. Period-rebase tests include actual retained statements in the
other weeks. Enable durable capture before activating either resource protocol.

Native mutation entry points and load/fuel/toll/scale imports, FleetScope webhooks
and the fleet reconciliation batch enter the source protocol before their own
row, table or subsystem locks. The reconciliation batch retains its existing
two-database receipts and recovery boundaries. Relay roster/link persistence,
WeighMyTruck membership persistence, toll review, inventory/imports, access changes,
dispatcher pay and maintenance/service-program writes also enter before local locks.
Provider receipts/rosters and FleetScope delivery/link state are unrelated inputs;
their resulting canonical driver/truck/fuel/toll changes remain independently captured.
Network identity fences retain their existing handoff boundaries. The final audit also covers administrative/direct SQL entry points and the ELD, odometer, PTI and feedback paths.

The repository protocol switch requires the reviewed epoch, drains the exclusive
gate and locks classified source tables, records a transition receipt, advances
source fences and queues retained projections. Tests cover both directions, stale
epochs, active writers, runtime-mode mismatch, rebuilding and unchanged financial
history. `deploy/mserp-payroll-protocol TARGET EPOCH` stops the API, expense exporter
and fleet-review service before switching, updates only payroll capability settings,
and restarts the previously active services. Its failure path stops writers again
and attempts a quiesced protocol/configuration rollback; uncertain state leaves
writers stopped with the private configuration backup. Helper recovery scenarios are validated in the isolated Linux mock harness. The deploy helper rejects an incompatible rollback
binary when capture or a resource protocol is active. Activation requires completed correctness/browser checks and a review of measured
performance limits.

Capture activation is persisted in `payroll_projection_control`. Stopping the
worker does not erase or disable durable capture. Startup fences old revision
snapshots when enabling capture and queues all retained projections for
reconciliation, including possible changes made while capture was disabled.
The API never retries a financial command through the preparation worker.

## Source and writer inventory

The actual table-by-table classification is persisted in
`payroll_projection_sources` by migration 182. It covers every existing table in
the revision registry; every table not explicitly excluded is conservatively
financial. `app_users` is display input (recorded actor names). Authentication
sessions, login activity, reviewed operational exclusions, and derived projection
tables are excluded. This is intentionally broader than the known financial joins.

| Input / owner | Canonical writer families | Impact and guards |
| --- | --- | --- |
| Native load placement and source evidence | `gross_board_repository.go`, `load_repository.go`, `load_sync_repository.go`, `payroll_source_accept.go`, `payroll_load_placements.go`, `load_week_transfers.go`; DataTruck import/refresh jobs | Old/new driver and truck routes, source/service/placement dates, trip IDs, transfer destinations, later carry. Existing optimistic versions and finalized guards remain. |
| Fleet, teams, ownership and terms | `fleet_repository.go`, `assignment_week.go`, `driver_teams.go`, `driver_team_setup.go`, `truck_charges.go`; supported fleet integration/import paths | Both sides of dated relationships, team principals and saved destinations. Native assignments remain authoritative. |
| Expenses and collections | `expense_repository.go`, `expense_maintenance.go`, `expense_external.go`, `expense_balance.go`, `driver_charges_pay.go`, `maintenance_repository.go`; supported import/export workflows | Driver, investor and expense principals, reimbursement vs charge, corrections, external receipts, current/future reservations. Existing financial locks and immutable receipt protections remain. |
| Escrow, recurring charges and installments | `driver_charges_repository.go`, `driver_charges_pay.go`, `truck_charges.go` and related canonical SQL guards | Account/schedule/type, old/new effective dates, releases, collections and later remaining balances. No generic merged balance replaces these distinctions. |
| Fuel, toll and scale sources | Fuel and toll repositories/jobs, `toll_transaction_links.go`, `toll_truck_links.go`, `prepass_fleet.go`, `weighmytruck_settlements.go` | Dated driver/truck attribution, linked-source changes, first-live-week rules, carry, original evidence. Capture never calls a provider. |
| Payroll edits / settlements / undo | `driver_pay_repository.go`, `investor_pay_save.go`, `payroll_settlements.go`, `payroll_source_accept.go`, payroll bulk helpers | Canonical edit/settlement revisions, frozen snapshots, confirmations, reservations, individual audits. Comment saves retain existing implicit deductions. |
| Direct SQL and existing database triggers | The same classified tables, regardless of entry point | AFTER STATEMENT capture for INSERT/UPDATE/DELETE/TRUNCATE in the source transaction. Rollback also rolls back the event. |
| Derived work | Projection generations, statement versions/state, jobs, receipts | No recursive source invalidation. Canonical ledgers are never written by preparation. |

Source capture retains distinct old/new routing keys and dates, not entire row
images. It excludes private profile fields, provider payloads and credentials.
Unresolved routing is covered by a domain dependency; no destination is guessed.
`loads.synced_at` alone is ignored using the same full-row-except-timestamp
comparison as migration 181. Every other load content change remains relevant.

Coverage verifies both revision and capture event triggers, enabled state,
statement-level AFTER semantics and transition relations. New unknown tables,
missing/disabled trigger events and unapproved exclusions refuse publication.
A future source migration must explicitly enroll its classification and triggers;
registration alone is insufficient.

## Storage and consistency

Each week job prepares both pages in one repeatable-read transaction. The shadow
implementation compares optimized publication against independent full-input
results in the same snapshot. For a proven captured principal group, it publishes
only that group and reconciles the resulting current pointers against the full
reference. The reference calculation remains enabled during this development
stage; these shadow runs do not establish the final performance target. Existing settlement overlays
copy frozen financial values from stored reports; current source corrections do
not reconstruct their historical money. Tests compare frozen reports before and
after preparation and changed sources.

The generation stores exact table revisions, payroll calendar week, calculator
version and schema version. This conservative domain dependency includes existing
records and selection scopes that can introduce new statements. Publication locks
the dependency revision rows in a deterministic order and checks the current job
lease/generation. A concurrent source commit aborts publication. A failed or old
worker cannot publish partial statements or acknowledge newer requested work.

Immutable statement versions hold complete display JSON and compact summaries;
monetary summaries additionally retain exact integer cents. Stable original load,
trip, slot and allocation identities remain in the detail. Generation headers,
all statement pointers, summaries and job completion commit together. There is no
shared weekly total counter. Removed eligibility drops the current pointer and
retains prior versions. Command receipts include removed identities and retain
their last visible versions. Current reads join indexed statement pointers in
one repeatable snapshot; each save avoids copying a whole week's membership.
History has no pruning job yet, so finalized and
receipt-referenced versions cannot be lost through retention cleanup.

`ReadPreparedPayrollPage` reads only these stored records. It uses the existing
pagination/search/dispatcher/finalization-progress rules and restores requested
details in the same snapshot. It explicitly reports `preparing`, `ready`,
`updating`, or `failed`; existing complete data survives staleness/failure. It
compares dependencies, calendar and implementation versions independently of the
queue. `PAYROLL_PREPARED_READS` selects this adapter for the public payroll routes.

`payroll_routing_facts` stores immutable dated load-to-truck candidates, native
truck cost allocations, source driver cards and original transaction evidence,
keyed by the calculator and their exact source revisions. Driver cards are stored
per identity and retain currently ineligible drivers; current eligibility and
saved edits are read separately from canonical financial records. Native evidence
preserves private attribution and ordering alongside its public fields. These
inputs contain neither editable deductions nor settlement overlays. A
selected calculation can reuse a complete stored fact but cannot publish a partial
selection as a whole-week fact. Changed toll amounts and assignment dates produce
new facts; ledger-only changes reuse them across transactions and process restarts.
Itemized evidence reuses source facts, then applies current routing and exact
oldest-first collection allocation. Background publication retains the facts
needed by later commands; independent authoritative reads bypass them.

Vacation and terminated-owner fee eligibility also retain source-only facts.
Their dependencies cover status corrections, dated crews, ownership, investor
activity and termination receipts. Vacation facts additionally include the current
payroll calendar week because untracked legacy availability changes meaning at
rollover. Allocations and saved edits do not invalidate these inputs. Fresh and
incremental database tests compare reuse after source changes against uncached
queries, including partial leave, continuing teams and calendar invalidation.

Native Relay identity review publishes its linked fuel changes atomically with
payroll. PrePass provider completion retains the truck identity lock through
network read-back, releases it, then reacquires the payroll and truck locks in
the ordinary order and rechecks the original reviewed version. Its local result,
receipt and payroll commit together. A handoff conflict or publication failure
leaves the provider request pending without resending it. Tests cover these
failures and prove that provider calls do not hold the global payroll lock.

Registered expense, escrow and schedule principal edits resolve both captured
destinations. Deleted principals can resolve deleted allocations only when their
old routing is present in the same transaction. Missing parents and unknown
sources retain full publication. Broker expense edits retain both old and new
load numbers and resolve native board, placed and frozen destinations independently
of the expense's optional driver/truck attribution.
Selected source publication refreshes current setup candidates and excludes
unselected retained statements. Three-week, both-page database checks cover
invoice creation, amount changes, reassignment, future reservations, deletion,
new eligibility and broker reminder changes, with authoritative parity and unchanged
unrelated statement versions.

Load corrections retain numeric source IDs and old/new display numbers. Impact
includes every candidate for reused numbers, manual placements and frozen reports;
financial identity remains the source ID and trip. Changed sources prepare their
new immutable facts before native publication. Placement and transfer audit images
capture only their old/new routes and dates. Native acceptance/undo, weekend
transfer/restoration, historical placement, gross edits, removal and later re-add
have three-week parity tests on both pages with unchanged unrelated versions.
Frozen source corrections preserve both kinds of finalized financial JSON.
Native FleetScope intake completion also publishes atomically; injected publication
failure rolls back both the new driver and the intake completion.

Dated assignment, team, ownership, settlement-term and truck-charge edits now
retain both endpoints of their old and new relationship edges. Truck source
changes include the truck identity and both recorded owners. Three-week parity
checks cover replacement assignments, team creation/ending/deletion, recorded
owner changes and truck fee creation/edit/deletion while retaining unrelated
statement versions. FleetScope delivery state is explicitly unrelated; canonical
fleet changes still have their own capture. Unknown secondary writer effects
continue to request full preparation.

Fuel impact retains old/new driver IDs, reported truck-unit identities and line
parent IDs. It resolves every alias candidate without choosing an attribution;
missing aliases or deleted parents without captured old routing retain full
fallback. Linked toll changes retain both truck destinations. Corrections to
amounts, driver/truck routing, source/posting dates and deleted fuel parents with
cascaded lines have three-week, both-page parity checks with unchanged unrelated
statement versions.

During a command's post-write publication phase, charge and truck configuration
can be reused across weeks only for an identical complete selected component.
Those queries contain all dated phases and occurrences; period-specific balances
and reports keep separate memos. A new command or canonical write starts a fresh
phase, and the independent reference never uses this reuse.

Prepared settlement publication can reuse a complete post-write report only
inside the same fenced transaction, with identical input snapshot, week, kind
and selected resources. Canonical financial read caches remain invalidated after
writes. Newly finalized rows receive committed settlement metadata without
replacing current display decorations on other finalized rows in the component.
An explicit regression preserves live broker reminders on those existing rows.

The shadow dispatcher invalidates every materialized week, retaining a coalesced
union of captured old/new principals for registered ledger, expense, escrow and
schedule changes. Unknown batches, initial preparation, explicit retries and code
or calendar changes require full preparation. A narrow job falls back to a full
calculation if any committed source events remain undispatched. Revision and job
fences still reject later commits and superseded claims. It
claims exact committed outbox rows with `FOR UPDATE SKIP LOCKED`; an ID high-water
mark is never used. Thus a delayed lower-ID transaction cannot be skipped. Jobs
coalesce a batch of source changes and use two-minute reclaimable leases with a
90-second calculation timeout. Ordinary failures use bounded exponential backoff;
changed inputs are discarded for later preparation. The worker listens for
notifications and polls at one second as a fallback, and warns about work older
than 30 seconds. Current/recent/interactive work precedes historical work.

Backfill seeds current week, previous eight weeks, next two, every saved/finalized
week including future saved weeks, and history from the earliest relevant source.
It rechecks the calendar and calculator version each minute. It never creates
payments, deductions, collections or finalizations. Native commands publish all
materialized dependent weeks in their financial transaction. Proven ledger and
registered principal changes recalculate each week's related group; other changes use the
conservative full calculation. The asynchronous scheduler does not replace this atomic boundary; unknown impact deliberately retains the full fallback.

Source accept/undo, placement and transfer responses include the durable request
identity and affected statement kind/ID/week, with the existing undo ID retained.
Replay returns the original committed identities, scoped to the authenticated
actor and the route's current authorization. Legacy callers retain their response.

Metadata-only finalize/reopen commands can rebase other current periods without
recalculating their statements, but only after inspecting their actual source
events and excluding unrecorded legacy cost overrides. Posting, confirmation,
allocation and uncertain changes still publish all dependent periods. A period
with a stale calendar, calculator, schema or source snapshot uses full calculation.

The prepared-versus-authoritative financial fixture now includes two drivers and
two trucks sharing an investor, selected trips from one split load, corrected
expense balances and external settlements. Single and whole-week finalize/reopen compare
canonical money, reservations, confirmations, correction/settlement receipts and versions. A forced failure on
the second statement verifies rollback of allocations, audits, projection pointers
and command receipts before retrying the same request.
Prepared team and owner-operator scenario tests also reuse the established dated
assignment, team-expense, recurring-truck-charge, finalized-guard and frozen-cost
assertions, including changed source fuel before reopening.
The existing expense-window fixture also runs prepared receipt-based finalization
and reopening for driver, investor and owner-driver routing, retaining its
partial/zero collections, next-week carry, late-expense freeze and EFS assertions.
Prepared first-live-week tests cover driver-owner and investor routing, excluded
historical fuel, production versus staging, DEF and credits, toll posting versus
crossing dates, partial collections, retained transaction evidence and frozen JSON
after a correction and reopening.

The source-entry audit also covers FleetScope identity review, supersession and
the local acknowledgement after provider delivery. These paths acquire the
source gate before integration state/identity locks. Delivery HTTP completes
before acquiring the gate. A forced shared-gate test verifies that a blocked
supersession leaves the integration state row available, then removes only its
unconfirmed identity and commits the operation atomically after the gate opens.

`measure-payroll.mjs` defaults to 200 routine observations and 50 whole-week
observations per group. Prepared runs also measure direct captured source commits
to visible updated mileage, using a database timestamp immediately after COMMIT
and the browser's second animation frame. Recorded reports disclose sample counts
and failures; sparse diagnostics are not high-sample tail estimates.

## Validation and operational recovery

Use only a local disposable database ending in `_test`:

```powershell
# backend/, with MSERP_DRIVER_PAY_TEST_DATABASE_URL already configured
go test ./internal/repository -run '^TestPayrollProjectionDatabase$' -count=1 -v
# Exercise the existing expense/charge/team/ownership fixtures with capture on.
# These also require MSERP_DRIVER_CHARGES_TEST_DATABASE_URL.
$env:MSERP_PAYROLL_CAPTURE_TEST='1'
go test ./internal/repository -count=1 -timeout 25m
Remove-Item Env:MSERP_PAYROLL_CAPTURE_TEST
$env:MSERP_PAYROLL_PERF='1'
$env:MSERP_PAYROLL_PROJECTION_PARITY='1'
go test ./internal/repository -run '^TestPayrollPerformance$' -count=1 -v -timeout 20m
```

The focused suite exercises fresh and incremental migration 182 as `mserp_app`,
disabled default capture, routing/private-field boundaries, rollback,
timestamp-only sync, both pages, cache-free repository restart, stale data before
dispatch, late commits, active/expired leases, missing capture, source commits
during publication, failed-publication rollback, frozen snapshots and financial
side-effect checks. The larger fixture compares both pages and multiple pages of
summaries/details over three weeks, retaining the historical dependency component.
Diagnostic results are in `performance/payroll-persistent-shadow-local.txt`.
These are not 200-observation browser percentiles or proof of the full release gate.

Release validation uses the complete backend suite with every disposable-database
setting configured in CI, followed by `go vet ./...`. Frontend validation includes
lint, a production `/api` build, calculation/autosave checks, prepared/lazy/grid
flows, individual finalization and whole-week reopening, expense/balance controls,
charge views, and load-split/daily-load regressions. Isolated Linux helper tests
cover compatibility, transition failures, recovery and configuration preservation.
External provider lifecycle tests remain explicitly opt-in; no provider mutation
or financial write benchmark is performed against production.

The final browser run is bounded rather than the original 200/50-observation sweep.
Its report records actual samples, failures and percentile estimates. Financial
parity, concurrency, rollback and recovery checks are not reduced by this timing
choice. Local results and subsequent CI/production evidence are separate gates.

The older recurring-charge migration fixture now installs capture after rebuilding
its revision registry and completing event triggers, matching numbered production
migration order. The strict coverage check exposed the previous fixture ordering;
it was not relaxed to make the capture-enabled suite pass.

Before installing migration 182, install the corresponding deploy helper. It
pauses API/export/portal writers while adding source triggers, then uses the existing
restart/rollback protocol. Migration alone leaves capture disabled. Do not enable
capability gates out of order or change locking protocols with online flags.

For stalled shadow work, inspect job `last_error`, `attempts`, `available_at`,
`lease_until` and pending age; expired leases recover without deleting work.
Repair missing source coverage through a reviewed migration, then restart the
shadow worker to reconcile retained projections. Retry derived work through
`RequestPayrollPreparation(..., true)`; never replay a financial command. Keep
stored source events and version history when disabling the worker or rolling
back the application. The additive schema does not require a financial rollback.
