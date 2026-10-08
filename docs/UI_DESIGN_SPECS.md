# MSERP UI design specifications

Approved direction: October 8, 2026. This is the common contract for existing
and new MSERP screens. Keep the compact dark zinc/blue appearance.

## Shared foundations

`frontend/app/globals.css` defines the `--ui-*` tokens and the `.mserp-ui`
scope. The authenticated shell and shared modal portals opt into this scope.
`ManagementUI.tsx`, `PageHeader.tsx`, and `MetricCard.tsx` are the shared
primitives. Extend these before introducing another styling pattern.

| Element | Standard |
| --- | --- |
| Font | Geist Sans; tabular digits for numeric columns and money |
| Body, table data, buttons, inputs | 13px, 20px line height |
| Labels, column headings, secondary metadata | 12px, 20px line height |
| Page title | 16px, 24px line height, weight 600 |
| Card and section title | 13px, 20px line height, weight 600 |
| Metric value | 20px, 28px line height, weight 600 |
| Ordinary table row / column header | 32px minimum; 8px horizontal cell padding |
| Buttons, selects, single-line inputs | 32px minimum, 8px corner radius |
| Touch buttons and tabs | 40px minimum on coarse pointers |
| Icons | Lucide, 16px; page identity icon 20px |
| Cards | 8px corners, 1px border, 16px padding |
| Metric cards | Same compact layout; 80px minimum height |
| Spacing | 8px related items, 16px card gaps/padding, 24px section separation |
| Background | `#09090b` |
| Card surface | `#111113` |
| Primary / ordinary text | `#f4f4f5` / `#d4d4d8` |
| Secondary text | `#a1a1aa` |
| Border / table header | `#27272a` / `#18181b` |
| Primary action | Blue `#2563eb`; hover `#3b82f6` |
| Keyboard focus | Visible 2px light-blue outline |

The minimum row height is not a clipping rule. Wrapped content, expanded
statements, multi-row headers, and row-spanning financial cells may grow.
Preserve the 32px accounting entry geometry and sticky offsets. A map or notes
card is content-sized; do not stretch every card to the height of the longest.
Keep operational board status fills and financial warning colors meaningful.
Do not recolor statuses merely to make every cell identical.

## Page structure

- Render exactly one page title through `PageHeader` into the top bar.
- Title content is icon, title, optional count and status. No descriptive
  subtitle or introductory paragraph below a page title.
- Keep filters and actions in the main content toolbar; retain global search,
  quick-create and account actions on the right of the top bar.
- Keep the shared Back link on its own row above content.
- Use the full available width. Overflow belongs to the individual table,
  not the document. Stack profile columns on narrower screens.
- Sign out exists only in the top-bar account menu. The sidebar retains its
  collapse control.
- Preserve actionable errors, empty/loading states, validation and essential
  field instructions. Removing page introductions must not remove data warnings.

## Components and behavior

Use `ManagementHeader` for directory titles, `TableShell` for table boundaries,
`TablePagination` for standard record pagination, and `MetricCard` for metrics.
Use `.ui-card` for new content cards and `.ui-button` / `.ui-button-primary` for
new actions. Currency is right aligned with two decimal places; ordinary counts
use grouping separators. Dates and identifiers must not be disguised as amounts.

Use visible labels for actions when space permits. Icon-only actions need an
accessible name. Focus, hover, disabled, saving, error and empty states are part
of the component, not optional page-specific decorations. Error messages must
retain drafts. Never rely on color alone to distinguish statement status.

Profile summaries link existing truck numbers, drivers, dispatchers and owners
to their details. Missing links remain plain text. A latest-location header has
the title at left and muted `Unit <number>` at right; that unit is not a link.
Do not repeat truck ownership in another driver overview card.

## Profile notes and assignments

- Drivers and trucks use `ProfileNotes`: Add note opens an inline composer with
  Save and Cancel. Entries show the server-recorded author and New York date/time.
- New entries are append-only through the API. A retained submission UUID makes
  retries idempotent. Existing freeform profile notes remain undated legacy
  context; do not invent an author or date for them.
- Driver assignment history switches between Trucks and Dispatchers. Default
  to Trucks, remember the selection, and provide no combined option or Type column.
- Truck assignment history lists drivers. Both views show Started, Ended and
  Source, newest first, with Current for open assignments and Unknown when the
  historical start is not known.

## Investors and statements

- Every owner is discoverable in Investors, including a driver with one truck.
  Company visibility remains an explicit filter. Financial eligibility is a
  separate rule and is not changed by directory visibility.
- Investor details and a linked driver's Ownership tab share `OwnershipPanel`.
  Show owned trucks, operating drivers, current terms where permitted, and
  weekly investor statement summaries.
- Statement summaries show week, truck, Gross Board driver gross, owner share,
  net adjustments, net payable and a Draft/Finalized link. Links select the exact
  truck and week in Investor Pay and open its details, overriding stale filters.
- Investor Pay uses the same compact table and expandable statement layout as
  Driver Pay. Each truck gets its own row, named with investor and unit (for
  example, Morgan Hayes 102). There is no extra investor grouping or summary tier.
- Profile statement summaries use fixed columns; monetary headings and values
  align right, while week, truck and statement links align left.
- Trucks lacking dated terms appear in the same table, with Setup required in
  the Tariff column and dashes for unavailable amounts, never as zero-dollar
  statements. These rows describe the current fleet for the selected week;
  it is not invented historical ownership.
- Driver statements and investor statements remain separate, even when their
  recipient is the same person. Finalized does not mean money was paid.

The financial meaning of gross and rate differences is documented in
[SETTLEMENT_RULES.md](SETTLEMENT_RULES.md).

## Validation

Run frontend lint/build and the board/payroll calculation checks. Inspect
desktop and narrow layouts for directory tables, detail cards, accounting
expansions, modals and board editing. Verify focus visibility, table overflow,
header/icon/control sizes, summary links, permissions and loading/error states.
`node scripts/test-investors-e2e.mjs --profiles-only` runs `profiles-e2e.mjs` and covers profile
notes, assignment switching, details links and statement navigation against an
isolated real API/database. Existing charge/task browser suites cover shared UI.
