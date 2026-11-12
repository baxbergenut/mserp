# Missing and replaced DataTruck orders

DataTruck may stop returning an order ID and expose another ID with the same
business load number. Imports retain both historical source records. A complete,
validated ID lookup records presence in `load_source_availability`; omission is
not authority to delete financial history or silently substitute an identity.

Loads excludes confirmed missing records from rows, counts and filter options by
default. The **Show missing from DataTruck** checkbox includes labelled historical
records. Gross Board suggestions and new selections exclude missing IDs. Existing
plans with missing links use the ordinary red unmatched style in both boards.
Reappearance restores source details. Payroll/frozen source calculations retain
historical evidence.

Gross Board rejects a new claim for a load number already present in another
active slot, even under another driver, date or DataTruck record ID. Matching is
case-insensitive and ignores surrounding whitespace. Distinct selected trips
of the same parent are allowed; the same trip cannot be added twice. The error
identifies the existing driver, date and slot. All entries in a rejected save
roll back, including other edits in that batch. The native source/financial lock
also serializes simultaneous claims from different dispatchers.

The guard checks the final batch state to allow atomic moves and cleanup. It
does not scan for duplicates on ordinary amount/note edits and does not rewrite
historical duplicates or finalized statements.

## Historical import identity

The November 3, 2026 sheet-to-system conversion joined each displayed number
to every matching DataTruck ID. Its eligibility check counted tokens and matches
after joining them together, so duplicate source IDs multiplied both counts and
incorrectly passed the check. This produced multiple board slots for one sheet
load and spread the original daily driver gross across those slots. It was a
one-time import error, not a behavior of the scheduled DataTruck refresh or the
reviewed reconciliation command. Historical rows use the guarded replacement
rules below; never discard allocated gross or rewrite finalized statements.

Future imports must resolve each distinct normalized input token separately,
require exactly one verified current provider ID (or an explicit reviewed trip),
then allocate amounts only across the resolved business loads. Count candidate
IDs per token before joining tokens back to the day; never infer uniqueness
from matching aggregate counts after a many-to-many join. Unresolved/ambiguous
days stay in review. Use the native Gross Board save path for new entries so
the duplicate guard, finalization checks and atomic rollback apply; direct SQL
imports bypass the application guard and are not an approved shortcut.

## Automatic missing-link replacement

The 15-minute operational pass reuses its complete ID lookup results to replace
missing links with the unique current record sharing the normalized business
load number. The daily pass provides the same fallback. Only candidate IDs not
already checked need extra provider requests, in batches of 25 through the shared
request gate. The 30-second modified feed is unchanged and cannot prove absence.
Finalized driver weeks and numbers with no possible replacement skip extra calls.

A single open entry retains its entered driver gross, manual total gross/miles
and note. Source-driven total gross/miles follow the replacement; discrepancies
remain visible. Multiple same-driver/day entries with distinct predecessor IDs
can consolidate into the current record's slot, preserving every driver-gross
cent. Unknown amounts, manual total/mileage edits or notes across multiple rows,
multiple current IDs, trips, cross-driver/date usage, transfers, placements and
finalized driver/investor statements hold the group for review. Dates or revenue
may differ on the unique replacement; they are not copied into manual overrides.

When a replacement is unavailable, the plan keeps the entered number and values.
Focusing that field never silently changes its historical identity. Presence
changes notify the board once; unchanged presence checks do not trigger a refresh.
Old source records remain historical evidence, excluded from ordinary selection.

The repair rechecks source evidence, availability, candidate identities, versions
and financial guards under native locks, saves through the duplicate guard, and
publishes affected payroll atomically. Immutable `gross_board_trip_history` rows
record typed before/after board entries with actor `datatruck_replacement`.
Publication failure rolls back changes and audit together. Successful repairs
stop being candidates; replay does not consolidate twice. Unchanged/held groups
do not publish or calculate payroll.

Validation includes `TestMissingBoardReplacementDatabase` (fresh/migrated schema,
manual-value retention, finalized driver/investor guards, source reappearance,
publication rollback, exact gross preservation and replay), the job request-reuse
tests and `frontend/scripts/test-gross-board-reliability.mjs`.

## Reviewed reconciliation

The release includes `backend/reconcile-loads`. Run as the service user from
`/etc/mserp` with `EnvironmentFile=/etc/mserp/mserp.env`. It performs read-only
DataTruck ID queries before acquiring financial locks; it never writes DataTruck.
Use a private directory owned by `mserp` for previews. Do not print credentials.

```sh
reconcile-loads -since 2026-10-14 -preview /private/path/preview.json
reconcile-loads -apply /private/path/preview.json -actor ADMIN_USER_UUID -request UNIQUE_REQUEST_UUID
```

Review each group's `sources`, `before`, `after`, `driverGross` and `hold`. The
preview is read-only and refuses to overwrite a file. Only groups with one current
provider ID, missing predecessors, identical customer, pickup/delivery dates,
ordered route and revenue qualify. Live current values must agree with imported
values. Multi-trip sources, cross-driver/date assignments, manual overrides,
unresolved/repeated identities, notes, load comments, placements, transfers and
finalized statements remain held. The report includes groups with no board repair.

If a current provider record changed outside the operational refresh window,
`-refresh-current` explicitly runs the native source-only refresh for the returned
IDs and records presence for the complete lookup before creating a new preview.
This optional mode writes imported sources and operational presence evidence;
ordinary preview does not. It never creates loads or changes fleet assignments.
Do not reuse an older preview after refreshing sources.

Apply re-fetches provider evidence and recomputes the entire preview under the
exclusive payroll source/financial protocol. Any changed source, board version,
candidate identity set or guard invalidates the digest. Refresh/review a new
preview; never bypass the fence. Per-group repairs keep the current source's
existing board slot when possible, consolidate every original driver-gross cent,
take current source mileage/revenue, and clear redundant slots with version bumps.
They never delete source loads. The complete batch, affected statement publication
and `payroll_command_receipts` entry (`command=load.reconcile`) commit together.
Publication failure rolls back every repair. Reuse the exact same actor, request
UUID and preview for an uncertain-result retry; committed receipts replay rather
than repair twice. Do not reopen finalized statements automatically.

Validation: `TestPrepareLoadReconciliation`, `TestLoadReconciliationDatabase`
(`MSERP_DRIVER_PAY_TEST_DATABASE_URL`, disposable localhost `_test` database),
`TestGrossBoardDatabase`, and `frontend/scripts/test-loads-views.mjs` after a `/api`
frontend build. The database tests run as `mserp_app` on fresh and migrated schemas,
including stale-preview rejection, publication rollback, receipt replay,
historical source preservation and restored provider visibility.

## Unlinked number-only slots

The same 15-minute/daily pass can now link a single open number-only Gross Board
slot with no stored DataTruck ID. Complete provider lookup must prove one live,
unsplit same-number source. Deleted predecessors do not prevent this match, and
no predecessor is required. Multiple slots or current sources, finalized weeks,
transfers, placements, splits and protected comments remain held for review.
The native save/publication path preserves all manually entered gross, miles,
driver gross and notes and records the normal reconciliation audit. Merely
focusing the dropdown never writes a source selection. No data migration runs.
