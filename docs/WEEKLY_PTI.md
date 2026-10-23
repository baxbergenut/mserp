# Weekly PTI

Fleet > Weekly PTI is a truck-by-week matrix. Every recorded week appears as a
column, oldest to newest, with frozen week headings and truck numbers. Company
and investor trucks are grouped above owner-operated trucks. All trucks appear,
including inactive, unassigned, home, vacation and shop trucks. Ownership uses
the recorded investor/company/driver-owner relationship, never the assigned driver.

Each cell accepts only `/`, `1`, or `0`, enforced by the UI, API and database:
- `/`: not operational, no driver, or PTI not required; yellow (#ffff00).
- `1`: sent and checked; green (#00ff00).
- `0`: not sent; red (#ff0000).

Blank cells are unrecorded, never inferred as 0 or /. Manual marks are independent
of fleet eligibility. Legacy completed native checks display as 1; other unchecked
legacy records remain blank until explicitly recorded. Existing snapshots and
audit history remain intact. The current-week worker maintains roster context
but does not set or overwrite manual marks. Past weeks are never recalculated.

The page shows only Unit # and week columns. Week cells are 64px wide; the
scroll area fills the available page height and resizes with the window. Truck
numbers and week headers stay frozen. Notes and reviewer columns are removed;
previous notes and attribution remain stored and are preserved by mark edits.
Cell hover shows recording attribution and time. There is no notes-week selector,
history button, eligibility UI, video link, or explanatory page copy.

GET /weekly-pti/matrix returns all recorded weeks, current trucks/ownership and
saved cells in a consistent read-only transaction. pti.read/edit protect reads
and writes. Row versions and immutable request receipts prevent stale overwrites
and duplicate saves. Missing cells for an existing week are created only on an
explicit version-zero edit. Concurrent creation conflicts. Conflicting edits refresh the row without overwriting another user.
Legacy clients cannot overwrite a recorded symbol without supplying it.

## Reviewed Sheets import

The user authorized the last four columns of Fleet board / Weekly Report:
27-Sep, 05-Oct, 12-Oct and 19-Oct, mapped to Monday weeks 2026-09-28, 2026-10-05,
2026-10-12 and 2026-10-19. The first source header is an inconsistent Sunday;
its neighboring columns identify the corresponding Monday week.

backend/scripts/prepare-weekly-pti-import.mjs prepares SQL and a comparison from
private source and current truck snapshots. Keep these files outside Git.
Exact units and unique exact VINs resolve identities; user-reviewed aliases and
duplicate-cell resolutions are explicit input. Source blanks remain blank.
Unknown marks and conflicting duplicates are held instead of guessed.
SQL locks PTI rows, rechecks truck identity, preserves any native edit, completed
check or note, records source digest/rows and appends import audit events.
Reapplying the same source preserves already imported cells. No fleet records,
ownership, eligibility, financial records or external services are changed.

Migration 144 adds the database mark constraint. The original migration 140
and append-only audit tables remain compatible.

## Validation

- go test ./...
- go vet ./...
- MSERP_DRIVER_BOARD_TEST_DATABASE_URL: local disposable _test database;
  go test ./internal/repository -run 'TestWeeklyPTIDatabase|TestPTIEligibility' -count=1
- Frontend npm run lint and NEXT_PUBLIC_API_URL=/api npm run build.
- Start scripts/review-local.mjs, then run scripts/test-weekly-pti-e2e.mjs with
  its access file. Covers all trucks/weeks, ownership grouping, marks/colors,
  preserved notes, compact full-height layout, historical cells, retries/conflicts,
  mobile scrolling and authorization boundaries.
