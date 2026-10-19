# Fleet inventory and stored trailers

Fleet > Inventory replaces operational equipment worksheets. Its tabs are
directly addressable, for example `/fleet/inventory?tab=tablets`, `samsara-gateways`, `samsara-cameras`,
`devices`, `supplies`, `lines`, `equipment`, `transponders`, `history`, `locations`
and the Administrator-only `imports` workflow. Fleet > Trailers is a separate
directory at `/fleet/trailers` for stored/unused company trailers. Operational
Schneider/JB Hunt trailer text on Status Board remains independent.

Each directory keeps search, filters, refresh and add controls in one desktop
toolbar, wrapping on smaller screens. Tablet columns show make/model, IMEI and
the linked wireless number without a redundant device type or gateway column.
Samsara gateways and dashcams have separate directories, with linked dashcam and
linked gateway columns respectively; old `tab=samsara` bookmarks open gateways.
Phone displays use the shared `+1 (AAA) BBB-CCCC` formatter in tablet and carrier
lists, details and truck summaries. The tablet number column shows only the
number; carrier status remains in AT&T lines. Gateway forms omit blank IMEI,
make and model fields; existing source values remain editable and are preserved.
The import review remains available at `/fleet/inventory?tab=imports` for
authorized administrators, without a Migration review button in daily inventory.

## Authority and access

MSERP owns local inventory and custody after reviewed cutover. Samsara/AT&T
provider synchronization is not enabled. A tablet's IMEI identifies hardware;
its tablet number is the wireless number on its separate service line. Moving a
line changes its device relationship and retains the old relationship in history.
Truck Samsara serials, cameras and tablet details derive from linked assets,
not editable truck copies. A camera inherits its truck through its gateway.

PrePass remains the source of its fleet/transponder roster. The shared existing
UI retains provider permissions, refresh/receipt guards and unresolved-operation
recovery. The Transponders tab shows only this shared provider workspace. Legacy
local custody/tag notes stay stored for source provenance and audit, without a
separate notes list or editor in this tab. They never change a provider assignment
or historical toll link.

The new capabilities are `inventory.read`, `inventory.manage`, `inventory.import`,
`trailers.read`, and `trailers.manage`. Administrator receives the code-owned
catalog automatically. Grant other roles only their needed capabilities and
prerequisites in Settings. Import additionally requires built-in Administrator
status, inventory management, trailer management, truck read and toll read.
Inventory permission alone does not expose equipment inside a truck profile;
the truck route still requires truck read. PrePass reads/notes also require toll
read. Trailer-only users need no inventory permission.

FleetScope lifecycle/compliance/insurance/trailer contracts are unchanged.
Local stored-trailer records are not synchronized or assigned through FleetScope.
Equipment presence is not a compliance or expiry determination. Document/permit
checklists and DOT inspections are outside this feature.

## Storage and API

Migration `119_fleet_inventory.sql` is also included verbatim in `init.sql`.
Dedicated tables store assets, lines, locations, supplies, equipment observations,
stored trailers, local transponder notes, import batches, record history and stock
movements. Category-specific descriptive fields are sparse JSONB; relational
identities, unique IMEIs/serials/phone numbers, linked UUIDs and versions are
constrained separately. Generated relationship columns have foreign keys.

All writes take one short transaction-scoped advisory lock to protect invariants
across devices, lines, assignments, ledger balances and import commits. Edits
require the current record version. Every save has a UUID `requestId`; identical
replay returns the original result, while changed bytes conflict. Actor and
verification timestamps are server-owned. There is no destructive delete API.

| API | Purpose |
| --- | --- |
| GET `/inventory` | Assets, lines, locations, supplies, observations, authorized tag notes, stock balances and latest movements |
| PUT `/inventory/{collection}/{id}` | `{requestId,record:{id,version,data}}`; create at version zero or update current version |
| GET `/inventory/history?collection=...&id=...` | Latest 500 authorized record changes |
| POST `/inventory/movements` | Idempotent stock transaction using UUID `id` |
| GET `/trailers` | Stored trailer directory |
| PUT `/trailers/{id}` | Versioned trailer create/update/archive |
| GET `/trailers/{id}/history` | Trailer audit history |
| GET `/inventory/imports` | Latest 20 saved review batches |
| POST `/inventory/imports/preview` | Preserve a dated source snapshot and prepare review outcomes |
| PUT `/inventory/imports/{id}` | Save `{version,rows}` without changing inventory |
| POST `/inventory/imports/{id}/commit` | Atomically apply the reviewed batch using `{version}` |

Collections are `assets`, `lines`, `locations`, `supplies`, `equipment` and
`transponder-notes`. Trailers cannot be written through the inventory route.
Requests exceeding 10 MB are rejected by import endpoints. Normal record bodies
use the application's standard limit. The TypeScript contract is re-exported
from `app/lib/types.ts`; API helpers live in `app/lib/api.ts`.

## Stock and equipment

The initial location is Main storage. Add other named storage locations before
moving stock there. Holders are `location:<UUID>`, `truck:<UUID>` or
`custodian:<name>`. System destinations are external, consumed, lost and damaged.
Quantities are whole numbers: chains in sets, extinguishers in units and IFTA
stickers in pairs with an explicit year. Supplies can be reusable or consumable.

Record a physical opening count before issuing from a storage location. Zero is
valid and remains distinguishable from unknown. Receipts can be recorded before
a count, but issuance remains blocked. If receipts already exist, physically
count and record an adjustment for the difference; a verified zero-difference
adjustment establishes the count without inventing stock. Transfers, issues,
returns, use, damage and loss preserve the ledger. Only reusable supplies accept
return movements. Consumables use an explicit use movement when installed/used.
An observation's attached status never consumes stock automatically; legacy
observations have no inferred quantities. This avoids double accounting imports.

Every movement needs a reason. Correct mistakes with an exact reversal or a
counted adjustment. Reversals remain linked to originals and cannot be repeated.
An issue/return/transfer/reversal cannot make a real holder's balance negative.
Supply units, reuse rules and year cannot change after stock movements. Resolve
held stock before archiving supplies; locations with stock history remain active.

Truck observations distinguish unknown, source-reported presence, verified
installation and missing. Verification stores the actual verifying user/time.
IFTA tracks sent and attached independently, with optional known dates. Imported
2026 statuses preserve unknown dates and do not fabricate sticker serials.

## Source snapshot and reviewed migration

1. Export the complete Fleet board workbook as XLSX. Keep a dated private copy
   outside Git; it preserves formulas, formatting, notes, links and source values.
   Retain the original Google workbook unchanged during preparation.
2. Convert the XLSX using Python with `openpyxl` installed:

   ```powershell
   python backend/scripts/prepare-fleet-inventory.py fleet-board.xlsx fleet-snapshot.json --spreadsheet-id 1RcRmMoM4Jyj1OTAMqDFxawHDiPG8oRVEPzbVOZHmJDk
   ```

   The converter retains all tabs, row positions, text identifiers, boolean values,
   explicit zero-padded unit formats and date values. It refuses to overwrite an
   existing snapshot. Numeric precision already lost in Sheets cannot be recovered;
   legacy ICCIDs always require carrier verification.
3. Upload the snapshot under Migration review. This stores a preview only.
   Repeating the identical snapshot returns the same batch. Raw snapshots remain
   in the database with a SHA-256 hash; record provenance names source tabs/rows.
4. Review proposed merges, exact truck matches and all flags. Unique VIN takes
   precedence; otherwise only an exact unit matches. Never strip leading zeros,
   `#` prefixes, or guess aliases. Review `777T (022)` and the known Samsara
   `ESK`/`E5K` disagreement explicitly. Cross-tab device facts can merge by exact
   identifiers; conflicting source values require resolution reasons. Newer dated
   carrier sheets supply initial attributes, but never establish physical custody.
5. Review camera/gateway links and line/device relationships. Excluding a parent
   requires relinking or excluding its dependent rows. Correct duplicated
   assignments before commit. Every operational row requires a reviewed outcome;
   flagged or excluded rows require a reason. Existing record versions are frozen
   by the preview and stale changes conflict rather than overwrite newer work.
6. Commit only the reviewed batch. Record saves and batch status commit in one
   transaction. Retrying an already committed batch returns its receipt without
   duplicating inventory. If an existing record changed during review, preserve
   the source batch, compare the current record, and prepare a newly captured
   snapshot/review rather than bypassing the version fence.
7. Physically verify devices and count opening stock. Imported checkboxes are
   reported/unknown observations; FALSE is not treated as verified absence.
   Warehouses remain unverified until a physical count is recorded.
8. Reconcile each category against its included/merged/excluded/archived source
   outcomes, exercise normal user workflows, and freeze sheet editing only after
   staff accepts the results. Keep the dated workbook as a read-only archive.

Samsara order/license side blocks remain procurement references in the source;
licenses never become counted hardware. Old PrePass tags import only as reviewed
local historical notes after selecting exact account/environment, not as roster
entries. AT&T phone rows remain service lines with optional `other` hardware and
named custodians.

Oil change rows remain archived reconciliation items. Compare date, odometer and
oil grade with existing Preventative Maintenance and apply reviewed missing
baselines through that feature, preserving newer telemetry/manual readings and
creating no financial transactions. Fleet Dashboard, Monthly report, Expense
stats, Weekly Report, Week reference table and financial history receive no new
report pages or historical expense imports.

## Validation and release

Use a disposable localhost database ending `_test`:

```powershell
$env:MSERP_INVENTORY_TEST_DATABASE_URL='postgres://...@127.0.0.1:5432/mserp_inventory_test?sslmode=disable'
# backend/
go test ./...
go vet ./...
# frontend/
npm run lint
$env:NEXT_PUBLIC_API_URL='/api'
npm run build
node scripts/test-inventory-views.mjs
node scripts/test-prepass-views.mjs
```

Tests cover fresh/incremental schemas as mserp_app, idempotency, conflicts,
exclusive assignments, line/device replacement, immutable history, concurrent
stock issues, reusable returns, consumable use, physical zero counts, annual
sticker observations, import preview/review/atomic replay and authorization.
Browser fixtures cover normal management, draft retention on conflict, linking,
read-only roles, shared PrePass behavior and narrow screens. CI enables database
tests on normal pushes; full-browser workflow dispatch includes the new suite.

Deploying migration 119 creates only empty directories, supply definitions and
Main storage. It imports nothing, counts nothing, sends nothing, and changes no
fleet ownership, Status Board, payroll, provider roster or FleetScope records.
Verify release SHA, readiness, authenticated directory reads and restricted-role
responses before starting migration review. On rollback retain the additive
tables and immutable history; roll forward to a corrected release. Never drop
the inventory tables after staff has started recording work.
