# Gross Board planning entries

Gross Board has three explicit entry choices: Load, Status or note, and Planned
destination. Loads can be selected from DataTruck or entered manually before a
provider match exists. Notes and destinations are dispatch planning only and can
coexist with any load on the same driver/day.

Typing in a load cell creates an unconfirmed local draft. Select a DataTruck
result, a fixed status shortcut, or Add as manual load before it changes a load
slot. Daily loads cannot Apply while typed text is still unconfirmed. Escape
discards that typed load draft. Planning entries use their own explicit Save;
closing the editor keeps a recoverable draft and Cancel discards that draft.

## Storage and compatibility

Migration 206 adds `gross_board_planning_entries`, keyed by driver, service date
and kind (`note` or `destination`). A day can have one of each kind. Text is
limited to 1,000 Unicode characters. Clearing text keeps a versioned tombstone,
so a stale editor cannot recreate deleted content without review.

`GET /gross-board` and its live snapshots expose `planningEntries` separately
from `entries`. `PUT /gross-board/planning-entries` accepts `{weekStart, entries}`
and returns the saved planning entries. Each entry has `driverId`, `date`,
`kind`, `text` and `version`. Version zero creates an entry. Different stale
writes return 409; an exact successful retry returns the committed version.
The usual Gross Board read/write permissions apply.

Load saves, including old full-row clients, never write planning storage.
Planning edits never clear a load, change its gross or miles, select a current
load, change driver availability, or set an active destination. Existing
per-load notes remain separate from these day-level planning entries.

## Financial and privacy boundary

Free-form planning text is not a `day_status` or a `load_number`. It is excluded
from Status Board load queues and overdue rules, load resolution, payroll
source inputs, statements, exports, and provider payloads. It is loaded only
for dispatch Gross Board reads, not the shared payroll board projection.
Canonical day-status shortcuts keep their existing display and payroll rules.

A manual load remains eligible for the Status Board even if DataTruck has no
matching record yet. Do not solve free-text classification by requiring an
upstream match or by guessing whether text looks like a load number.

## Historical records

The migration does not classify or move existing records. Existing unmatched
loads, ambiguous free text, canonical statuses, financial values and current
selections remain unchanged. Review ambiguous legacy records individually
before deliberately clearing an old load slot and saving its planning text.
Do not infer load identity from a number appearing inside a note.

## Rollout

Install the matching deployment helper before applying migration 206. The helper
quiesces API, expense-export and fleet-review writers for the schema change and
rejects rollback binaries without `-gross-board-planning-capability` returning
`gross-board-planning-v1`. Older binaries do not recognize the new unrelated
payroll-source classification. Keep the SQL classification and Go exclusion list
in sync.
This change itself does not apply migrations to any live environment.

## Verification

Use only a disposable localhost database whose name contains `_test` for
PostgreSQL checks. Build the frontend with `NEXT_PUBLIC_API_URL=/api` before
fixture or real-API browser tests. Exercise note/destination coexistence,
manual unmatched loads, old-client load saves, concurrent planning changes,
delete/recreate, failed saves, live snapshots, reload recovery, Cancel and
read-only permissions. Run the repository's Go tests/vet and frontend
lint/build plus relevant existing Gross Board and Status Board regressions.

Validated on the deployed-code base `3b05d6fe`:

- `go test ./...` and `go vet ./...`.
- Fresh and incremental planning, Gross Board, Driver Board, Driver/Investor Pay,
  team, updater, maintenance and truck-recurring database suites, plus payroll
  source coverage and unrelated-source isolation on a disposable local database.
- `deploy/test-permissions-cutover.sh` and the planning capability command.
- Frontend TypeScript, full lint and `NEXT_PUBLIC_API_URL=/api npm run build -- --webpack`.
- `test-gross-board.mjs`, `test-driver-board.mjs`, `test-driver-pay.mjs`,
  `test-phone.mjs`, and `test-gross-board-planning-helpers.mjs`.
- Browser scripts: `test-gross-board-planning.mjs`, notes, reliability,
  daily-loads, numbers, layout, splits and `test-board-grid.mjs`.
- `node scripts/test-investors-e2e.mjs --gross-live-only`, including authenticated
  read-only denial, real planning persistence, old-client load saves, stale
  planning conflicts and cross-user SSE delivery without an extra board GET.
- Desktop and narrow editor visual review using synthetic data.

The numeric fixture's obsolete automatic Total gross-to-Driver gross expectations
were reproduced on the unchanged deployed worktree, then updated to the already
shipped independent financial-input contract. No application calculation was
changed for that test repair.

Production rollout uses the approved main-branch CI deployment. Verify migration
206 and the installed release after activation; never test writes on live records.
