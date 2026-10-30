# Shared status colors

Settings > Status colors saves company-wide display colors. Reads require a
session; changes require access.manage and CSRF. The versioned singleton stores
only overrides; an empty map restores defaults. Each save appends an immutable
version/actor snapshot. Each status has one global foreground/background pair;
there are no per-page controls. Unknown keys and anything other than six-digit hex colors
are rejected. This setting changes presentation, never operational permissions,
assignments, provider state, payroll routing or financial calculations.

The default catalog and original Status Board pairs live in
frontend/app/lib/statusColors.ts. Keep active and legacy keys in sync with
backend/internal/repository/status_colors.go; test-status-colors.mjs checks parity.
StatusColorsProvider supplies CSS variables to the authenticated
shell and body portals, refreshes on focus/every 30 seconds and updates the current
window immediately after saving. Personal appearance themes remain independent;
explicit foreground/background pairs remain exact in every theme. The editor
previews both surroundings and displays the chosen pair's contrast ratio. Never
silently replace a chosen foreground. Always retain visible status labels.

| Status Board status | Foreground | Background |
| --- | --- | --- |
| Ready (drivers and trucks) | #FFFFFF | #000000 |
| Rest | #000000 | #D9D9D9 |
| Covered | #000000 | #FFFF00 |
| Dispatched | #FFFFFF | #FBBC04 |
| Enroute | #000000 | #3D85C6 |
| Reserved | #000000 | #B7E1CD |
| Home | #000000 | #FF0000 |
| Vacation | #000000 | #E06666 |
| Shop | #FFFFFF | #FF9900 |
| Stop | #000000 | #990000 |
| Other original choices | #000000 | #FFFFFF |

All pages, including Gross Board, profiles, payroll, truck directories and charts,
obey this palette. Other workflow statuses inherit its closest meaning, with a
global pair editable for each status. In use uses Enroute's default; OOS uses
Stop's default. Operational status models and employment remain independent.

The existing `colors` JSON retains legacy single-hue entries for compatibility.
New edits save `<status>.fg` and `<status>.bg` plus the legacy background key.
No schema migration or audit rewriting is needed. The old truck_ready key remains
readable/accepted; new Ready controls use driver_ready globally for both domains.
Unknown dotted suffixes, compound suffixes and invalid hex strings are rejected.
Restoring defaults clears overrides and restores the original Status Board pairs.

The central exact alias registry replaces substring matching (Unpaid must never
match Paid). New status displays use statusClass/statusColorKey/statusPair rather
than hardcoded colors. Markers and charts use the same resolved background and
foreground. Text status labels retain the paired background so dark foregrounds
stay visible on dark personal themes.

Blank/whitespace-only statuses have no status color classes or fill. They must
not inherit the fallback palette for named statuses; an explicit No load remains
a separate, colored status.


Migration 171 retires At PU, At DEL, HOME (YARD), and 34 RESET from the board
catalog. Current records consolidate to DISPATCHED, ENROUTE, HOME, and REST;
driver reset availability consolidates to rest. Existing fleet/board history
triggers audit the transition, preserving ETA, leave plans, notes and loads.
Normalization triggers also cover direct/legacy writes and undo recovery.
At-pickup/delivery progress remains in the ETA column and its existing automation.
New availability commands cannot select reset. In transit is displayed as Enroute
on Loads, Gross Board, payroll and other status surfaces. New Gross Board choices
are ENROUTE and REST; legacy IN TRANSIT/RESET values remain readable, with current
labels/colors, without rewriting historical rows or finalized snapshots.

Validation: TestStatusColorsDatabase uses local disposable
MSERP_DRIVER_BOARD_TEST_DATABASE_URL and checks fresh/upgraded schemas as
mserp_app, aliases, preserved records, immutable history, validation and stale
saves. TestDriverBoardSheetStatuses checks retired API choices. Frontend checks:
node scripts/test-status-colors.mjs, node scripts/test-status-colors-views.mjs,
node scripts/test-status-board-layout.mjs after a /api build, plus existing board,
payroll and theme regressions.
