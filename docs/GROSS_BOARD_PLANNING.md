# Gross Board planning entries

The day-cell input accepts a system load selection, an explicit unmatched load
identifier, or an inline Add as status/destination suggestion. Manual identifiers must
contain a digit and use letters, digits, dots, slashes, underscores, # or hyphens;
free-form sentences and destinations are offered only as custom statuses. Existing
stored load identifiers remain readable and are not reclassified.

New custom statuses use the existing note storage kind. Existing destination
entries retain their independent storage and appear with the same custom-status
style. There is no Add entry chooser or planning modal. Click a status to edit
inline; Enter or blur saves, Shift+Enter inserts a newline, and Escape cancels an
unsaved edit. Failed saves retain recoverable drafts. Conflicting changes require
explicit inline replacement or choosing the saved text.

A day defaults to load entry. Custom status is an explicit alternative and fills
the day without a load input. A day must contain loads or a status, never both.
To switch types, clear the current status or all loads first; no financial load
is silently replaced by a planning write. Multiple loads remain supported.
Fixed day statuses also cannot be added beside another load.

Legacy mixed days are not migrated or erased. They show a Review saved status
disclosure for explicit cleanup, with normal financial edits and clearing
preserved. The API serializes both save paths per driver/day and rejects newly
introduced overlap, including stale-client races.

Deleting the load input clears its load identity, notes and amounts immediately
and uses the ordinary guarded load autosave; there is no empty unconfirmed draft
or extra Clear load number action. Load identifiers save as loads on Enter or blur without an extra confirmation
choice. A unique exact search result can supply the source; unmatched identifiers
remain manual loads. Free-form text commits only through the explicit custom-
status suggestion. Escape discards unselected typing.

Weekly cards and all table totals use Total gross, Driver gross, Total miles,
Total RPM, Driver RPM, Rate balance. Both RPM values use the existing exact
amount/mileage calculation and show a dash when miles are zero.

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
load, change driver availability, or set an active destination. Nonempty planning
text cannot be added to a day with load/financial or fixed-status content. Existing
per-load notes remain separate from these day-level planning entries.

## Financial and privacy boundary

Free-form planning text is not a `day_status` or a `load_number`. It stays outside
Status Board load queues and overdue rules, load resolution, financial source
inputs, exports and provider payloads. The shared financial board projection
does not load it. Canonical shortcuts retain their existing payroll rules.

Driver and Investor Pay show custom text as display metadata in `dayStatuses`:
`{date, status: "CUSTOM", text, kind}`, where kind is `note` or `destination`.
Literal text such as HOME does not become a fixed status. A dated team member
routes to the main statement; Investor Pay uses that driver's unique truck
assignment at the end of the service day. Ambiguous truck assignments are not
guessed. Cleared/versioned tombstones never produce labels. Planning alone does
not create payroll roster membership.

Open summary and detail reads overlay live planning in their database snapshot,
including legacy in-memory caches and pinned prepared detail. Prior CUSTOM rows
are replaced, not accumulated, so edits and clears cannot leave stale labels.
Financial caches and native day-status facts remain independent; planning does
not recalculate amounts, advance reviewed financial revisions or enqueue payroll
preparation. Changed planning saves publish a commit-only payroll live refresh;
clients compare day metadata even when financial statement revisions are equal.

Finalization, including the prepared fast path, overlays current text before
freezing the report. Planning API writes take the existing source gate before
day-mode and row locks, serializing with both global and resource finalizers.
Existing frozen labels and legacy finalized snapshots without day metadata are
never backfilled from mutable planning. No schema migration is needed for this
display projection; migration 206's financial-source classification remains
unrelated.

A manual load remains eligible for the Status Board even if DataTruck has no
matching record yet. Manual identifiers do not require an upstream match. Free-form text is committed
only by the custom-status suggestion, never by typing or blur.

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

The inline flow is covered by `frontend/scripts/test-gross-board-inline-entry.mjs`
(the existing planning script delegates to it). It uses intercepted synthetic API
fixtures, not live data, and covers mutually exclusive day types, direct clearing,
old note/destination records, failed saves, reload recovery and stale conflicts.
Native financial/finalization protection remains intact. The payroll display
regressions cover cached and prepared summary/detail reads, custom note and
destination routing, tombstones, finalization and legacy frozen reports, unchanged
financial values, commit-only refresh, and source-gate blocking. Run
`TestPayrollPlanningDisplayDatabase`, `TestPayrollPlanningRoutingDatabase` and
`TestPayrollPlanningNotificationsDatabase`, `TestPayrollPlanningSaveReceiptDatabase`
and `TestPayrollPlanningFinalizationRaceDatabase` with the disposable
`MSERP_DRIVER_PAY_TEST_DATABASE_URL`. New day-mode tests
also cover competing load/status writes, split companion scope and transfers
on a disposable localhost PostgreSQL instance.

Use only a disposable localhost database whose name contains `_test` for
PostgreSQL checks. Build the frontend with `NEXT_PUBLIC_API_URL=/api` before
fixture or real-API browser tests. Exercise note/destination coexistence,
manual unmatched loads, old-client load saves, concurrent planning changes,
delete/recreate, failed saves, live snapshots, reload recovery, Escape and
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
