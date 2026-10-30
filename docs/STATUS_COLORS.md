# Shared status colors

Settings > Status colors saves company-wide display colors. Reads require a
session; changes require access.manage and CSRF. The versioned singleton stores
only overrides; an empty map restores defaults. Each save appends an immutable
version/actor snapshot. Unknown keys and anything other than six-digit hex colors
are rejected. This setting changes presentation, never operational permissions,
assignments, provider state, payroll routing or financial calculations.

The default catalog lives in frontend/app/lib/statusColors.ts. Keep its keys in
sync with backend/internal/repository/status_colors.go; test-status-colors.mjs
checks parity. StatusColorsProvider supplies CSS variables to the authenticated
shell and body portals, refreshes on focus/every 30 seconds and updates the current
window immediately after saving. Personal appearance themes remain independent.
Badge foregrounds adapt to the actual theme surface at a minimum 4.5:1 contrast,
including custom near-black/white hues. Always retain visible status labels.

| Hue | Meaning |
| --- | --- |
| Emerald #10B981 | Truck Ready, active employment/accounts, completed/delivered, paid |
| Blue #3B82F6 | Truck In use, Dispatched, Enroute, work in progress |
| Purple #8B5CF6 | Home, Vacation, Rest |
| Amber #F59E0B | Driver Ready/available, Covered, Reserved, waiting, open, review |
| Orange #F97316 | Shop, Stuck, late delivery, personal/pickup/delivery issues |
| Red #EF4444 | OOS, Stop, Accident, truck issue, failure, Deadhead |
| Teal #14B8A6 | New driver / Needs setup |
| Gray #71717A | Terminated/Left, canceled/rejected, No load, yard, discarded |

Driver Ready is waiting for work; truck Ready describes serviceable equipment.
Keep their keys independent. Employment Active is also independent of driver
availability. Equivalent meanings across pages always share a catalog key.
The central exact alias registry replaces substring matching (Unpaid must never
match Paid). New status displays must use statusClass/statusColorKey rather than
hardcoded hues. Dashboard charts use the same saved base colors as badges.

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
