# DataTruck load splits

An order can contain multiple `trips`. The singular `trip` is not the complete
allocation. Load import keeps each trip's ID, number, dates, driver/truck labels,
`total_load_pay`, loaded miles (`mile`), empty miles, and pickup/delivery locations.
No external assignment, driver identity, payroll payment or ownership is changed.

## Selection and calculations

Migration 152 adds explicit trip identity. Migration 175 adds reviewed original
revenue allocations in loads.split_allocation and immutable load_split_history.
The Split editor shows DataTruck trip amounts as references, editable original
gross for every trip, and separate driver gross/explicit trip selections for
all linked Clustie board entries. Provider names never establish local identity.

Original allocations must sum exactly to the parent order total. Driver gross
is independent, including zero or blank; it need not sum to order revenue.
Original revenue is not inferred from total_load_pay. New blank driver gross
defaults from that field on the explicitly selected trip (or the sole identified
trip of a nonsplit order). Existing driver gross, including zero, is preserved.
Missing or ambiguous driver gross remains blank. Prior entered original amounts
prefill the review when linked to a specific trip. Otherwise allocations remain
blank until entered. Existing unreviewed splits retain their legacy projection
only while provider trip amounts and miles reconcile; this is compatibility,
not a new claim about provider field semantics.

GET /gross-board/loads/{id}/split requires board.read and returns a repeatable-read
snapshot. PUT /gross-board (board.write) accepts a splitReview on one entry.
The existing autosave/Daily loads boundary atomically saves the reviewed split
and all linked entries, including repeats in other weeks, under fleet/financial
and source locks. It fences the source fingerprint, allocation revision and
every board version. All affected driver/investor statements must be open.
Transferred entries must be restored before editing. Ordinary individual gross
edits cannot override a reviewed revenue allocation. Every native row change
retains its existing board audit, and allocation history records actor/reason.
The existing loads revision trigger and registered audit table invalidate payroll
caches. Migration/deployment alone changes no financial values.

Sync never overwrites split_allocation. A fingerprint of parent totals and
normalized trip identity, amount, mileage, dates and labels detects source changes;
routine synced_at updates do not. Changed/missing source evidence retains the
saved allocation and entered amounts but holds effective original gross/miles
for explicit re-review. The editor restores those saved allocations. A reviewed
split can differ from provider trip pay, but trip IDs must be unique, names and
numeric source fields present, and trip miles must reconcile with order miles.
Zero loaded miles remain valid. Ordinary single-trip orders use parent totals.

A rejected autosave retains its draft with explicit Discard unsaved changes.
Source changes and stale companion edits reject the entire transaction, never
partially change one driver's side. Finalized snapshots remain frozen.

Gross Board balances and totals, Driver Pay, Investor Pay, source acceptance and
dashboard reconciliation use the same trip projection. Loaded/empty mileage and
locations come from the selected trip. Truck attribution uses Clustie's dated
fleet assignment for the board driver and original service date, including for
selected trips. DataTruck's driver/truck/dispatcher labels are source provenance
only; stale or unknown source labels never override Clustie or create assignment
warnings. Missing/conflicting Clustie assignments require review.

Identity is `(load record ID, trip ID)`. A trip cannot be assigned to a second
board driver. Repeats for the same driver count on the earliest date, including
across weeks. Separate trips for one driver remain separate earnings. Dashboard
gross sums included trip allocations and load count counts distinct parent IDs.
Different provider order records with the same displayed number are never merged.

All selection changes are audited in `gross_board_trip_history`. Financial edits
and removal of selected-trip entries respect finalized driver and investor
statements. Finalized payroll snapshots remain frozen if the provider later changes.
No migration auto-selects trips or rewrites historical financial values.

## Rollout and historical review

1. Deploy migrations 152/175 and both applications together.
2. Allow the normal source refresh to import the `trips` data. For older dates
   outside the operational/reconciliation window, explicitly refresh the reviewed
   parent IDs using the existing rate-limited DataTruck client.
3. Preview each existing driver/date/slot, version, current entered values, and
   proposed trip. Select only confirmed mappings and keep driver gross unchanged.
4. Verify board totals, payroll and dashboard reconciliation after acceptance.
   Closed statements must be explicitly reopened before financial corrections.

Confirmed user-reviewed example: load 4010960805 / record 67035 has Daniel's
trip 65805 with $500 original gross and $700 driver gross, zero loaded plus
40 empty miles; Mark's trip 69275 has $1,313 for both gross amounts, 475 loaded
plus 40 empty miles. Original revenue totals $1,813; driver gross totals $2,013.
On October 30, 2026, the live order/list APIs returned $700/$1,313 as trip
amounts and $1,813 as order total, with no separate original allocation field.
An earlier response had $500 for Daniel. Subsequent November 4 verification of
Frank's loads 4010835104 and 80C4995 confirmed trip driver gross $2,300/$2,000,
separately from parent total gross $2,800/$2,299. This establishes the driver-gross
default; it does not establish a revenue-allocation rule.

## Driver gross defaults and refresh

Migration 195 marks only newly created blank plans as pending an initial driver
gross. Install the matching deployment helper first to pause writers for the
transactional schema change. Normal import/refresh fills those blanks once, under the existing financial
and source guards. Historical blanks, explicit amounts, zero, transferred plans
and finalized statements are preserved. Clearing an existing amount is manual.
Pending candidates use partial indexes and only the refreshed load IDs. A repeated
identical sync does not change board versions or payroll financial revisions.

The typed trips already persist in loads.raw_payload; no extra vendor request,
polling loop or duplicate driver-gross storage is needed. Board lookup, normal
payroll and prepared source facts share the trip amount. Projection schema 9
refreshes statement contracts, with only the driver_base source-card fingerprint
changed; unrelated reusable facts keep their existing cache keys.

DataTruck total gross, driver gross and miles comparisons refresh through the
existing operational/reconciliation sync. Source-backed total gross/miles retain
their existing live behavior; explicit manual overrides stay visible as differences.
Driver gross defaults once and then stays saved until explicitly changed. Board
and Driver/Investor Pay show independent driver-gross discrepancies and acceptance.
Acceptance validates current source and board versions, preserves other fields,
honors finalized guards, and payroll acceptance retains its undo receipt. Frozen
cards read current comparison evidence in one bounded batch without changing paid
amounts, frozen JSON or running a calculator.

The nine previously repeated numbers were read from DataTruck on October 27,
2026. Seven had one trip only; 8YX4487 had distinct orders/dates; 4010921488 had
distinct order IDs with inconsistent combined figures. None should be automatically
converted using this confirmed example's pattern.

## Validation

`TestLoadTripsSerialization` checks that sync serialization retains allocations.
`TestDataTruckDriverGrossDatabase` covers fresh/migrated defaults, historical and
manual preservation, late arrivals, zero/missing amounts, changed/identical syncs,
normal/prepared parity, independent acceptance/undo and finalized guards.
`TestLoadTripsDatabase` uses a disposable local `_test` database and isolated
schemas, in fresh and additive-migration modes, exercising selected values,
source acceptance, stale versions, duplicate trip assignment, repetitions,
dashboard/payroll/ownership, missing/changed sources and finalization guards.
Reviewed-split checks additionally cover fresh/additive migration, independent
amounts, whole-load reconciliation, atomic companion versions, finalized guards,
source-change review/reacceptance, audit rollback and dashboard/payroll parity.
Run with `MSERP_GROSS_BOARD_TEST_DATABASE_URL`, plus the existing Gross Board,
Driver Pay, Investor Pay and company-dashboard suites.

Build the frontend with `NEXT_PUBLIC_API_URL=/api`, then run
`node scripts/test-gross-board-splits.mjs`. It uses synthetic API fixtures and
checks selection, unchanged driver gross, totals, autosaves and read-only access.
