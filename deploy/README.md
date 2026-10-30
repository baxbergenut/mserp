# MSERP deployment

The temporary, opt-in expense-sheet exporter has a separate notification-driven service and
Google service-account setup. See [Expense sheet export](../docs/integrations/EXPENSE_SHEET_EXPORT.md)
before enabling it; ordinary deployment does not activate sheet writes.

GitHub Actions builds the Linux API and static frontend. Production receives a
checksum-verified archive through the unprivileged `mserp-deploy` account. The
root-owned `mserp-deploy` helper then:

1. rejects unsafe or malformed archives;
2. creates a PostgreSQL custom-format backup under `/var/backups/mserp`;
3. applies only SQL files absent from `schema_migrations`;
4. switches `/opt/mserp/current` atomically;
5. restarts the API, reloads Nginx, and restores a permission-compatible release
   if health checks fail.

Pushes to `main` deploy production. Pull requests run the complete test/build
job without receiving deployment secrets. `workflow_dispatch` can redeploy the
current `main` commit.

Production is served at `https://clustie.io`. The apex A record points to
`137.184.102.22`; `www` is a CNAME for `clustie.io`, managed with Namecheap DNS.
Nginx redirects HTTP and `https://www.clustie.io` to the canonical HTTPS URL.
The Let's Encrypt certificate under `/etc/letsencrypt/live/clustie.io` covers
both names. The former `https://erp.msexpressinc.net` website redirects while
its `/api/` routes remain proxied for existing signed webhooks and integrations;
retain its DNS record and certificate renewal. The API's `FRONTEND_ORIGIN` is
`https://clustie.io`; browser sessions are host-only, so users sign in again on
the new domain. The Certbot systemd timer renews certificates automatically
through the Nginx ACME webroot at
`/var/www/letsencrypt`. Successful renewals invoke the root-owned
`/usr/local/sbin/mserp-certbot-deploy` hook to validate and reload Nginx. The
original IP endpoint on port 8443 remains available during the domain
transition.

The production frontend is built with `NEXT_PUBLIC_API_URL=/api` so browser
requests, authentication cookies, and CSRF protection remain same-origin behind
Nginx. Do not build production against the direct IP endpoint. Nginx requires
revalidation for HTML and route files because deterministic release archives use an
epoch modification time. Content-hashed `/_next/static/` assets instead receive
a one-year immutable cache lifetime; every changed asset has a new URL.
The Nginx site configuration enables gzip for JavaScript, CSS, route text and
JSON responses (minimum 1 KB), with `Vary: Accept-Encoding`. Site configuration
changes are installed separately from release archives; back up the active
configuration, run `nginx -t`, and reload only after validation.

Database migrations should remain backward-compatible with the previous
application release because an application rollback does not automatically
reverse a successful database migration. Database backups are intentionally
retained for manual recovery.

Migration 180 fills missing cache-revision trigger events on enrolled tables.
Install the updated deployment helper first: it pauses API and expense-sheet
writers during the DDL to avoid lock inversion. It changes no payroll values,
and the preceding release remains compatible for rollback.

Migration 176 adds immutable expense balance corrections without changing existing
balances on deployment. Install the updated helper first: after the first correction
is saved, it refuses rollback to releases that ignore corrected debt. Empty correction
history remains compatible with the previous release; database read failures deny rollback.

Migration 160 installs Accounting cutover **inactive**. Install the updated
deployment helper first. It briefly stops the API and expense-sheet writer while
registering revision triggers, preventing DDL/write deadlocks. Releases without
migration 160 cannot be restored after
it is installed, because they ignore external settlements and the accounting
boundary.

Migration 161 retires the unused Accounting cutover workflow. Install the updated
helper before deployment: it pauses API and expense-sheet writers while removing
cutoff-specific triggers, then restarts them. The migration refuses activated or
populated cutover data, preserves migration-160 compatibility tables and external
settlements, and blocks future activation even on a compatible older release.
It does not repeat the separately authorized production data cleanup. See
`docs/ACCOUNTING_CUTOVER.md`.

Migration 151 removes manager verification and its historical records. Install
the updated deployment helper before releasing it. It stops the API and any
active expense-sheet exporter before the cutover and restarts them on the new
release. After the field is removed, releases without migration 151 cannot be
activated or restored automatically. Recovery requires a compatible release;
application rollback alone cannot restore the deleted records.

Migration 112 is a permission-model cutover. Install the updated deployment
helper before releasing it. The helper stops the API before applying 112 and
refuses an older release after the cutover. If activation fails and the previous
release lacks 112, the API stays stopped until a compatible build is installed.
Database-query failures also deny rollback. Existing migration 113 remains intact.

To roll back manually, repoint `/opt/mserp/current` to a permission-compatible
directory in `/opt/mserp/releases`, then restart `mserp-api` and reload Nginx.

## Temporary fleet reconciliation portal

The truck-color build requires MSERP migration 114. Reviewed truck colors are
saved in both databases on final submission. Unknown MSERP colors remain absent
from snapshot hashes, preserving existing reviews during the additive rollout;
once populated, color participates in snapshot conflict checks. Do not replace
the portal binary while a batch is frozen or partially committed.

The structured-name portal build requires MSERP migration 104. It saves reviewed
first/last names in both systems and uses those parts in snapshot fences. Before
upgrading an existing portal, back up its state and resolve any frozen or partially
committed batch using the original binary; do not change snapshot formats during
recovery. After upgrading, refresh the snapshots and re-review changed driver rows.
The migration leaves all existing full names untouched and does not activate sync.

`backend/cmd/fleet-review` serves the separate, embedded Drivers/Trucks review UI
at `https://conflicts.msexpressinc.net`. Build it locally for Linux with CGO off;
it is not part of the normal MSERP release archive. Use the supplied
`fleet-review.service` and `fleet-review.nginx.conf`, with the same ACME webroot
and renewal hook as MSERP. Validate Nginx before reloading it.

The operator bootstrap `provision-fleet-review.py` accepts secret JSON over SSH
stdin, verifies both company identities and the exact ownership migration hash,
takes private schema/scoped backups, and creates dedicated credentials and
immutable receipt storage. FleetScope's normal API deployment should apply its
ownership migration first. Never log the bootstrap payload. The portal is bound
to loopback port 8096. Its service cannot write outside its private state folder.

- `/etc/fleet-review/config.json`: root-owned, group-readable private settings,
  database URLs, pinned company identity, login hash and `allowApply` gate.
- `/var/lib/fleet-review/state.json`: saved snapshot, drafts and frozen batch;
  private to the service account. Back this up before any operator maintenance.
- `/var/backups/fleet-review`: restricted operator backups.
- Each database's `fleet_review.receipts`: immutable plan hash, identity mappings,
  before/after values and shared reviewer-account attribution. A shared account
  identifies the review account, not an individual staff member.

Generate credentials using `fleet-review -generate-credentials PRIVATE_PATH`.
Transfer the password privately to the authorized reviewer; never commit it.
Changing the configured bcrypt hash and restarting revokes all in-memory sessions.
Cookies are host-only, Secure, HttpOnly and SameSite Strict; mutations require an
exact Origin and session CSRF token. Login attempts are throttled. No database
credentials, payroll amounts, SSNs or documents are sent to the browser.

Reviewers confirm identity, edit values and save drafts before marking rows as
reviewed. **Save reviewed record** confirms the review directly, without a separate
checkbox; **Save draft** leaves the record unreviewed. Candidate names/VINs never
authorize automatic linking. Missing required vehicle facts must be supplied or
explicitly deferred; fabricated identifiers are
rejected. Missing terminated drivers are deferred. Existing drivers' status and
contact values default to MSERP; named exceptions live in private operator policy.
FleetScope's legacy `class_8` is displayed and saved as `8`. Startup upgrades only
unfrozen draft values and increments the review version, preserving review flags
and exact source snapshots. Frozen batches and their receipt hashes stay unchanged.
Ownership uses existing MSERP owners or a confirmed driver owner, with an explicit
effective date when ownership changes. Investor ownership is projected into
FleetScope as a named owner snapshot, never as a lease. Existing insurance facts
remain intact; owner adoption requires separate coverage review. Crew adoption is
an explicit per-truck decision and requires all crew identities and eligibility.

Final submission requires every row to be reviewed or explicitly deferred and a
typed confirmation. The server locks the affected fleet tables briefly, verifies
the frozen snapshots and commits a durable receipt in MSERP, then FleetScope.
These are two database commits, not a distributed atomic transaction. On an
interrupted response/restart, use **Recover the same batch**. Receipt readback
prevents duplicate creation. If either database changed after a partial commit,
stop and inspect the immutable receipt and current records; never delete receipts,
replace the batch or blindly restore a database. **Return uncommitted batch to
review** is allowed only when neither database has a receipt.

Refreshing before submission preserves drafts and unchanged reviews; changed
records need another review. Green requires reviewed matching current values.
Reviewed differences appear amber as **Ready to apply**, while unreviewed records
remain red. This is a saved decision, not a database write. A blank termination
date is valid for an active driver. UI status checks run with
`node backend/scripts/test-fleet-review-status.mjs` from the repository root.
Drivers and Trucks sidebar links show their own reviewed-or-deferred percentage
with a left-to-right fill, matching the list's remaining attention rows. Hover
shows reviewed, deferred and remaining counts. Deferrals count once, even before
confirmation; this display never sets their review flag or bypasses final checks.
Other unsaved reviews do not count. Progress is independent of search/filter state
and updates after each save.
Deferred records and coverage/safety/crew differences remain visible after success.
Normal site changes during review can invalidate the final snapshot, so arrange
a brief fleet-edit freeze for the final check and application.

The portal refuses existing integration enrollment, links or outbound operations,
even if mode is off. It records reviewed mappings in receipts, but does not install
ongoing sync links or activate the worker. Use those receipts in the separately
reviewed bootstrap/baseline workflow from `docs/FLEETSCOPE_SYNC_RUNBOOK.md` and the
FleetScope runbook. After reconciliation, disable final application, retire portal
access and database credentials, preserve its audit records, and only then perform
the separately approved integration activation.

Validation: `go test ./...`, `go vet ./...`, and browser login/draft/navigation
checks. The database suite requires full disposable MSERP and FleetScope schemas
on localhost, with URLs in `MSERP_PORTAL_TEST_DATABASE_URL` and
`FLEETSCOPE_PORTAL_TEST_DATABASE_URL` (database names end in `_portal_test`). It
checks restricted runtime permissions, snapshot conflicts, missing-record
creation, ownership, lifecycle/trailer preservation, partial commits, replay,
immutable receipts and enrollment fencing. Never point these tests at production.

## Status palette rollback (172)

Install the updated deployment helper before migration 172. It pauses API and
expense-sheet writers while restoring Status Board rules and removing the 171
normalization triggers. Original page colors and choices are restored. Retain
migration 171, its unused settings/audit tables and extended day-status checks;
no financial or historical records are rewritten or removed.
