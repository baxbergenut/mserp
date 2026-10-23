# Weekly PTI

Fleet > Weekly PTI is a compact checklist for a Monday–Sunday New York week.
The default On the road view shows trucks that need a video. Missing video,
To check, Checked and All trucks are one-click filters; search includes units,
drivers and notes. Receipt and review use checkboxes, with authenticated names
and timestamps on hover. Notes use a fixed-height field and save on blur or Ctrl/Cmd+Enter. There is no
history button or explanatory page copy. Issues
belong in Notes. Videos continue arriving through existing channels.

## Automatic roster

After shared fleet statuses are activated, the startup/minute worker creates
and reconciles the current week from current fleet state. Active trucks with a
Ready condition and at least one active, available assigned driver require PTI.
Home, vacation and other personal unavailability, Shop/OOS/Hold, inactive trucks
and trucks without an available driver are excluded automatically. Team trucks
have one row; one available employed crew member is sufficient. Dated assignments
are evaluated at the refresh time, including team membership and midweek additions.

There is no Monday confirmation or manual eligibility override. The existing
current-week roster, including old Needs confirmation entries, is reconciled on
the first worker run. Returning trucks reappear. Changes normally appear within
the worker's one-minute interval plus the page's 30-second refresh.

Reconciliation updates only roster context and automatic eligibility, incrementing
the row version only when those change. Receipt, reviewer, notes and original
snapshot fields are preserved. Excluded trucks and their saved work remain under
All trucks. Prior weeks keep their last stored roster and eligibility; they are
never reinterpreted using today's statuses. The original snapshot/late-capture
metadata remains stored for audit but is no longer part of the workflow.

## Receipt and review

A video must be received before it can be checked. Unchecking receipt also clears
its review. Review attribution changes only when the checkbox changes; editing
notes preserves who checked it. Legacy Issues found reviews display as checked,
with their original notes retained. Legacy video URLs remain archived and cannot
be edited. No videos are uploaded and no driver messages are sent.

Every edit uses an optimistic row version and request UUID. Concurrent changes
conflict, retries are idempotent, no-ops add no audit, and append-only events retain
before/after values and actors. Automatic roster updates do not alter edit history.
PTI has no fleet, dispatch, accounting or provider mutations. GETs never write.

## API and access

- GET `/weekly-pti?weekStart=YYYY-MM-DD` reads a selected Monday week.
- POST `/weekly-pti/weeks` creates/reconciles only the current week, idempotently.
- PATCH `/weekly-pti/{week}/{truck}` accepts `received`, `review`, `notes`, `version`
  and UUID `requestId`. Legacy eligibility/link fields are tolerated only when
  unchanged; they cannot override automatic eligibility or edit archived links.
- GET `/weekly-pti/{week}/{truck}/history` returns the latest 200 edits.

`pti.read` requires Drivers and Trucks read access; `pti.edit` requires `pti.read`.
Read-only users see disabled checkboxes. Audit history remains stored server-side. Migration 140's
permissions and schema remain unchanged; no data migration is needed.

## Validation

`TestWeeklyPTIDatabase` uses disposable local `MSERP_DRIVER_BOARD_TEST_DATABASE_URL`
for fresh/migrated schemas as `mserp_app`. It covers automatic teams, unavailable
trucks/drivers, midweek additions, return to road, record preservation, frozen
prior weeks, versions, retries, attribution and no-op refreshes. `TestPTIEligibility`
covers eligibility and New York DST weeks. After a `/api` frontend build, start
`scripts/review-local.mjs`, then run `node scripts/test-weekly-pti-e2e.mjs` with
its private access file. Synthetic mutations stay in disposable test schemas.
