# DataTruck load splits

An order can contain multiple `trips`. The singular `trip` is not the complete
allocation. Load import keeps each trip's ID, number, dates, driver/truck labels,
`total_load_pay`, loaded miles (`mile`), empty miles, and pickup/delivery locations.
No external assignment, driver identity, payroll payment or ownership is changed.

## Selection and calculations

Migration 152 adds a nullable `load_trip_id` to each Gross Board slot. The user
selects a trip explicitly in the Split dialog, which shows all provider trips.
Names are display evidence, never automatic identity links. Selecting a trip
accepts its original gross and miles with a source-value comparison in the same
versioned transaction. Existing entered driver gross is preserved, including
zero and blank. Trip `total_load_pay` is allocated load revenue, not a confirmed
driver compensation amount.

The source projection requires unique positive trip IDs, named drivers, complete
numeric amounts/mileage, and allocations that sum exactly to the parent gross
and mileage. Zero loaded miles are valid; missing miles are not zero. Unselected
splits, removed trips and inconsistent totals have no effective original gross
or miles and show review. Stored entered amounts remain available for comparison.
The source `split_expected` flag retains prior split evidence when a later
response omits trips, including before any driver has selected a trip.
Selecting a stale/invalid source fails atomically. Ordinary single-trip orders
continue using parent totals; differing single-trip pay does not establish a split.
Gross Board and load search expose tripReviewReason for invalid source allocations,
including the trip/order totals that differ. The Split dialog disables acceptance
until those allocations reconcile. Save rechecks the reason under the source lock
and distinguishes an already-claimed trip from invalid totals. A rejected autosave
retains its draft and exposes explicit Discard unsaved changes, so the user can
restore the saved board and leave without bypassing financial checks.

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

1. Deploy migration 152 and both applications together.
2. Allow the normal source refresh to import the `trips` data. For older dates
   outside the operational/reconciliation window, explicitly refresh the reviewed
   parent IDs using the existing rate-limited DataTruck client.
3. Preview each existing driver/date/slot, version, current entered values, and
   proposed trip. Select only confirmed mappings and keep driver gross unchanged.
4. Verify board totals, payroll and dashboard reconciliation after acceptance.
   Closed statements must be explicitly reopened before financial corrections.

Confirmed example: load 4010960805 / record 67035 has Daniel's trip 65805 with
$500 allocated gross, zero loaded plus 40 empty miles; Mark's trip 69275 has
$1,313, 475 loaded plus 40 empty miles. The total is $1,813 and 555 miles.
These are source allocations, not authorization to change driver gross.

The nine previously repeated numbers were read from DataTruck on October 27,
2026. Seven had one trip only; 8YX4487 had distinct orders/dates; 4010921488 had
distinct order IDs with inconsistent combined figures. None should be automatically
converted using this confirmed example's pattern.

## Validation

`TestLoadTripsSerialization` checks that sync serialization retains allocations.
`TestLoadTripsDatabase` uses a disposable local `_test` database and isolated
schemas, in fresh and additive-migration modes, exercising selected values,
source acceptance, stale versions, duplicate trip assignment, repetitions,
dashboard/payroll/ownership, missing/changed sources and finalization guards.
Run with `MSERP_GROSS_BOARD_TEST_DATABASE_URL`, plus the existing Gross Board,
Driver Pay, Investor Pay and company-dashboard suites.

Build the frontend with `NEXT_PUBLIC_API_URL=/api`, then run
`node scripts/test-gross-board-splits.mjs`. It uses synthetic API fixtures and
checks selection, unchanged driver gross, totals, autosaves and read-only access.
