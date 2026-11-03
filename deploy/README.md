# MSERP deployment

The temporary, opt-in expense-sheet exporter has a separate notification-driven service and
Google service-account setup. See [Expense sheet export](../docs/integrations/EXPENSE_SHEET_EXPORT.md)
before enabling it; ordinary deployment does not activate sheet writes.

GitHub Actions builds the Linux API and static frontend. Production receives a
checksum-verified archive through the unprivileged `mserp-deploy` account. The
root-owned `mserp-deploy` helper then:

1. rejects unsafe or malformed archives;
2. reads the migration ledger and requires either a fresh verified backup for
   pending migrations or a recent verified scheduled backup for code-only changes;
3. applies only SQL files absent from `schema_migrations`;
4. switches `/opt/mserp/current` atomically;
5. restarts the API, reloads Nginx, and restores a permission-compatible release
   if health checks fail.

Pushes to `main` deploy production. Pull requests run the complete test/build
job without receiving deployment secrets. `workflow_dispatch` can redeploy the
current `main` commit.

The backup helper requires a PostgreSQL client with zstd support (verified on the
production PostgreSQL 16 client). The deploy job allows 45 minutes for a complete
backup, migration and activation. Never remove the last valid backup merely to
make space: verify any relocated copy by SHA-256 and `pg_restore --list` first.
Deployment SSH sends keepalives during the otherwise quiet database backup.

## Database backup policy

`deploy/mserp-backup` creates PostgreSQL custom-format zstd level 1 snapshots in
`/var/backups/mserp`. Install it as root-owned `/usr/local/sbin/mserp-backup`
(0755), and install `mserp-backup.service` and `mserp-backup.timer` under
`/etc/systemd/system` (0644). The directory and backup/receipt files are root-only.
Enable the timer with `systemctl enable --now mserp-backup.timer`.

The timer runs daily at **03:00 America/New_York**, with up to five minutes of
jitter and catch-up after downtime. A manual/bootstrap run can reuse a snapshot
under 12 hours old only if it stays within the 24-hour freshness window until the
next scheduled start. The daily timer does not skip an overnight migration backup
and create a multi-day gap. Snapshots are written to `.dump.partial`;
only successful `pg_dump`, catalog validation, SHA-256 and fsync publish the
atomic `latest-verified.json` receipt. Freshness uses the snapshot's start time,
not its verification time. Receipts also fence the file's size, timestamp and
identity, so normal deployment checks do not reread a multi-gigabyte checksum.

Retention keeps **one completed verified snapshot**, retaining the previous one
until the replacement is verified and published. Unknown legacy dumps and changed
files are retained for explicit review. A shared lock prevents simultaneous backup
writers. Preflight checks budget 110% of the previous size plus 512 MiB; a runtime
256 MiB free-space floor stops the process group and removes its partial output
before filling the disk. Existing verified backups survive every failure.

Code-only deployments skip full `pg_dump` only when the timer is enabled/active,
the service is loaded and has not failed, and a verified snapshot is **at most
24 hours old**. Missing/stale/changed receipts, database lookup errors and unknown
ledger state stop deployment before activation. Every pending numbered migration,
including data migrations, still forces a fresh verified snapshot. Concurrent
backups cause a safe failure; retry after the running backup finishes. Out-of-band
data repairs must explicitly take a fresh backup with `--create --label <label>`.

Useful operator commands:

```sh
systemctl list-timers mserp-backup.timer
systemctl status mserp-backup.service
journalctl -u mserp-backup.service
/usr/local/sbin/mserp-backup --check
systemctl start mserp-backup.service
/usr/local/sbin/mserp-backup --create --label operator
```

Bootstrap only from a **known completed successful** `pg_dump`, for example a
completed deployment backup: `/usr/local/sbin/mserp-backup --adopt <absolute-path>`.
This validates the catalog/hash before publishing receipts. A catalog listing
alone cannot establish that an interrupted dump is complete. Never adopt a dump
from a failed deployment or while another process is writing it.

For a restore drill, create an isolated database owned by `mserp_app`, then restore
using `pg_restore --exit-on-error --no-owner --no-acl --role=mserp_app --dbname=<test-db> <backup>`.
Use a privileged connection that can assume `mserp_app`; validate application-role
reads, source rows, statement versions and migration records before declaring the
drill successful. Restoring production also requires the matching compatible
release and its numbered migrations; an application rollback does not undo data.

This is **server-local daily recovery**, not point-in-time or off-server recovery.
WAL archiving remains disabled. A server/storage loss can affect both the live
database and local backups; off-server storage is a separate future setup.

Validate policy changes with `bash deploy/test-backup-policy.sh`,
`python3 deploy/test-backup.py` and `bash deploy/test-permissions-cutover.sh`.
Install the script, units, verified baseline and enabled timer **before** installing
the matching deploy helper. Install helpers under the deployment lock so an active
release cannot observe a partially changed deployment script.

Migration 185 requires the matching helper before release. It pauses all payroll
writers while installing shared dependency maps and the optional retired-week
boundary, and rejects rollback to binaries lacking migration 185. Installation
alone deletes no saved statements; see the quiesced retirement command in
[Persistent payroll](../docs/PAYROLL_PERSISTENCE.md).

Migration 186 likewise requires its matching helper before release. It quiesces
API/export/portal writers while installing the exact expense-verification metadata
exception, and refuses older binaries whose payroll coverage checks cannot recognize
the new trigger. All financial edits continue to publish synchronously.

Migration 187 adds operational DataTruck source-presence evidence without changing
historical loads or financial matching. Install the matching deploy helper first;
it quiesces API/export/portal writers for the schema coverage change and refuses
older binaries afterward. Missing provider IDs become unavailable on Status Board,
and only a unique remaining live source can resolve an unpinned number. Pinned
selections retain their source ID for review. Failed lookups never mark sources missing.

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

Keep HTTP/2 enabled on every TLS listener (`listen ... ssl http2` on Nginx
1.24). Each browser tab can hold task and board/payroll event streams open;
HTTP/1.1's per-origin connection limit can stall sidebar navigation with three
board tabs. HTTP/2 multiplexes those streams and page requests. Verify `h2`
negotiation after installing the site configuration; updating the API release
alone does not install this change.

Database migrations should remain backward-compatible with the previous
application release because an application rollback does not automatically
reverse a successful database migration. Database backups are intentionally
retained for manual recovery.

Migration 180 fills missing cache-revision trigger events on enrolled tables.
Migration 181 ignores load updates that change only `synced_at`, comparing all
other columns (including source JSON and later-added columns) for invalidation.
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

## Persistent payroll protocol (182)

The implementation and outstanding release gates are tracked in
`docs/PAYROLL_PERSISTENCE.md`. Migration 182 is additive and defaults to capture
off/global locking. Installing it alone does not activate prepared payroll.
Install the matching `mserp-deploy` helper before this migration. Once capture is
active, rollback binaries must include migration 182 and answer
`-payroll-protocol-capability` with `payroll-resources-v1`.

After correctness checks, measured performance review and shadow cohort reconciliation,
install `deploy/mserp-payroll-protocol` as `/usr/local/bin/mserp-payroll-protocol`
owned by root with mode 0755. Migration 182 pauses the fleet-review service as well
as the API/exporter. Before protocol activation, install the packaged
`backend/fleet-review` binary at its existing `/opt/fleet-review/fleet-review`
path while that service is stopped; preserve the preceding binary for recovery.
The protocol helper checks its capability before stopping or switching any writer.
Migration 182 grants the existing `fleet_review_portal` role only the new capture
and protocol-read privileges its canonical source writes require; it cannot change
protocol settings. A differently named portal role needs equivalent explicit grants.

First enable shadow mode with prepared capabilities off, restart the API, and wait
for its full-reference backfill to reconcile every queued period. Inspect job errors,
source backlog, pending generations and the saved/finalized week cohort. Then use
the quiesced transitions below to enable prepared capabilities and resource locking.
Read the current mode and epoch using the same environment and working directory
as the service. Optional integration dotenv files in `/etc/mserp` supplement the
systemd environment; launching the binary from a login shell is not equivalent:

```sh
sudo systemd-run --quiet --wait --pipe --collect --uid=mserp \
  --property=WorkingDirectory=/etc/mserp \
  --property=EnvironmentFile=/etc/mserp/mserp.env \
  /opt/mserp/current/backend/mserp-api -payroll-protocol-status
```

The transition helper uses this exact operator environment for every CLI call.
Pass the reviewed epoch explicitly:

```sh
sudo mserp-payroll-protocol dual REVIEWED_EPOCH
sudo mserp-payroll-protocol resources REVIEWED_EPOCH
# Rollback uses the same quiesced transition with the latest reviewed epoch:
sudo mserp-payroll-protocol global REVIEWED_EPOCH
```

Each command holds the deployment lock, stages the payroll environment flags,
stops all active API/export/review writers and confirms they stopped. The CLI
takes the source gate and all classified source-table locks before advancing the
protocol epoch, recording a transition receipt and invalidating prepared jobs.
Only previously active services restart. Prepare and verify the affected cohort
before declaring the transition complete; readiness alone does not establish
payroll freshness. Pause any separately operated import/administrative writers
for this transition too.

A failed restart or lost switch response is recovered only from the observed
mode/epoch. The helper stops writers again, switches back when the outcome is
known, and restores the original environment. If writer shutdown or the database
outcome cannot be verified, it retains the private configuration backup and
requires operator recovery. Do not delete the backup or retry a financial command
to repair derived preparation. Inspect status and transition receipts, restore a
compatible configuration, then use the same helper with the current epoch.

`bash deploy/test-payroll-protocol.sh` exercises success, stale epochs, preparation
failure, stop/start failures, failed readiness, lost responses, uncertain outcomes
and rollback with isolated mocks. It does not contact production or replace live
release verification. The release checks also run its real Python serializer on Linux with synthetic
configuration, including preservation of unrelated settings.
Migration 190 permits deletion of paid or verified expenses outside finalized payroll.
External settlement and balance-correction receipts retain their original UUIDs and
financial fields plus an exact deleted invoice snapshot. Install the matching
helper first; it quiesces all writers while replacing receipt references.
