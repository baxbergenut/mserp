# Weekly PTI

Fleet > Weekly PTI records one video per truck for a Monday–Sunday New York week.
Videos continue arriving through the existing process; the optional link opens
their existing location. There are no video uploads or outbound driver messages.

## Monday eligibility

A startup/minute worker creates the current week's checklist once, after shared
fleet statuses are activated. Monday 12:00 AM America/New_York is the eligibility
boundary; the normal worker captures the roster within the first minute.

The stored roster includes trucks that existed at that boundary and their dated
solo/team assignments. Employment, availability and condition come from the
latest effective status-history records at or before the boundary, never the
current board dispatch annotation. Active crew on a Ready/Hold truck require one
video; Shop/OOS, no driver, inactive trucks, or all crew unavailable are exempt.
One available team member is enough to require the shared truck's video.

Crew names, status evidence, original eligibility and reason remain frozen.
Subsequent status changes, reassignment and backdating do not rewrite a checklist.
Trucks created midweek join the following week's roster.

There is no historical active/inactive truck ledger. To avoid inferring a missed
Monday roster from later data, creation more than five minutes after the boundary
marks every row Needs confirmation. Missing Monday status baselines also remain
Needs confirmation. Staff resolve these rows with a reason based on Monday's
actual situation. This applies to the initial midweek rollout; status-history
activation did not invent earlier starts. Old Sheets data is not imported.

## Receipt and review

Receipt and review are independent. Review is Not reviewed, Checked, or Issues
found. A received video is required before recording a review/link; issues require
notes. Replacing a previously reviewed video link resets its review. Authenticated
actor names and recorded timestamps accompany receipt, review and edits.

Required/Received/Not received totals exclude exempt or unconfirmed eligibility.
Review totals include all received videos, including voluntarily submitted exempt
videos. Not received does not imply an automatically determined overdue deadline.

Each change uses a row version and request UUID. Concurrent edits conflict,
retries are idempotent, no-ops add no audit, and an append-only event stores the
before/after values and actor. Snapshot metadata and week creation remain separate
from corrections. No fleet, dispatch, financial or provider writes occur from PTI
editing, and no GET endpoint creates or changes a checklist.

## API and access

- GET `/weekly-pti?weekStart=YYYY-MM-DD` reads a selected Monday week.
- POST `/weekly-pti/weeks` with `weekStart` creates only the current week, idempotently.
- PATCH `/weekly-pti/{week}/{truck}` accepts receipt/review/notes/link/eligibility,
  an eligibility reason, `version`, and a UUID `requestId`.
- GET `/weekly-pti/{week}/{truck}/history` returns the latest 200 edits.

`pti.read` requires Drivers and Trucks read access. `pti.edit` requires `pti.read`.
Migration 139 grants read to existing fleet-profile readers and edit to those who
already have Trucks edit. Administrators may adjust the new Weekly PTI capability
in Settings. History remains readable to PTI readers without editing permission.

## Validation

`TestWeeklyPTIDatabase` uses the existing local disposable
`MSERP_DRIVER_BOARD_TEST_DATABASE_URL`, with fresh/migrated schemas as `mserp_app`.
`TestPTIEligibility` covers exemptions, unknowns, teams and New York DST weeks.
After a `/api` frontend build, use `scripts/review-local.mjs` and
`node scripts/test-weekly-pti-e2e.mjs` with its private access file for real
API/browser checks. Production checks only read fleet data; synthetic mutations
are confined to disposable test schemas.
