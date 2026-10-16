# Temporary expense-sheet export

`backend/cmd/export-expense-sheet` is server-side MSERP code that writes directly
to the Google Sheets API. A persistent VPS systemd service wakes on a PostgreSQL notification after each
committed entry. It does
not depend on a desktop, browser, chat automation, or a connected ChatGPT account.
Deploying the code or migration does not enable capture or send any sheet writes.

## Scope and delivery

After explicit activation, migration 105 captures new native HR expenses into
`HR expenses` (sheet ID 1713619712), Safety/Penalties expenses into `Safety Expenses`
(752928518), and new Other/work-order invoices into
`Maintenance_and_other_expenses` (377431143), in spreadsheet
`1VKSL8fyBRjwOWAUhHlHUrJtn2HOa53tpuTm95TNqll4`.
Administrative, other categories, system-generated charges, and imported
sheet records are excluded. The configured category UUIDs survive category renames.
There is no historical backfill. The trigger is insert-only: later edits,
verification, payment changes and deletions are not mirrored. This is a copy of
the entry as originally saved, not a replacement for database backups.

A deferred transactional trigger captures the final invoice and linked work-order
metadata at commit. A rolled-back save creates no export. Durable immutable outbox
payloads survive source deletion and outages; Google availability never gates an
expense save. Pausing the service preserves pending work. Disabling capture stops
queuing future records, so pause only the service for a temporary outage.

The exporter takes a PostgreSQL advisory lock, verifies all three pinned sheet IDs and
the original column headers, and processes batches of up to 10 pending entries. Watch mode drains successive
batches immediately. All Sheets requests share pacing at 50 requests per minute
to stay below per-user quotas during bursts, including receipt checks. Normal
entries begin delivery as soon as their save commits; delivery takes a few seconds
and can queue behind a bulk import. The worker listens before its startup drain,
recovers pending entries on restart, and checks once a minute for retries or missed
notifications. A broken listener exits so systemd reconnects after 10 seconds.
It inserts rows after the last populated expense, before preformatted blank rows.
It preserves other rows, side lists, literal leading-zero units, and original total
amounts without calculating an extra EFS fee. Company stays blank because MSERP no
longer records that field. The old category label `Maintenance` is used for work
orders. New text is written as literal strings, never interpreted as formulas.
New rows retain formatting and verification checkboxes; obsolete dropdown lists
do not constrain new MSERP names or payers. Existing dropdowns are left alone.

Dedicated columns start at **T** on Maintenance and **AB** on HR/Safety: MSERP Entry ID,
creation time, work-order ID/date/odometer/vendor/location/service detail JSON,
oil grade, work-order number/status, charged-driver ID, owner ID, **MSERP Paid**
and the original **MSERP Category** (which distinguishes Penalties in the Safety tab).
While capture is enabled, new eligible native entries dated strictly before
**September 28, 2026** are marked settled at creation (configuration `paid_before`).
September 28 and later dates retain normal payment status; this cutoff never
slides with today's date. The exported boolean is separate from manager and
accounting verification, which are never set by the payment cutoff. Initialization
refuses occupied reserved columns or changed source headers. Unknown service
values remain blank. `prepare-expense-import.ps1` skips valid MSERP Entry IDs so
these rows cannot feed back through that importer.

Each row and a deterministic, spreadsheet-unique Google developer-metadata ID are
created in one atomic batch. A duplicate metadata ID rejects the entire batch,
including its row insertion. After a timeout or restart the worker checks that
receipt before trying again. A hash collision or foreign receipt stops that entry
for review rather than adding another row. Delivered outbox receipts are retained.
Do not delete the Google developer metadata or clear local delivery receipts.

## Google setup (one time)

1. In a Google Cloud project, enable **Google Sheets API**.
2. Create a dedicated service account (no project IAM role or domain-wide
   delegation is required). Create its JSON key and keep it private.
3. Share the existing Expense List spreadsheet with that service account's
   `client_email` as **Editor**. No public sharing is needed.
4. Transfer the JSON key privately to `/etc/mserp/expense-sheet-service-account.json`
   on the VPS. Set owner `root:mserp` and permissions `0640`. Never commit the key,
   paste it in chat, or put its JSON contents in an environment variable.
5. Create `/etc/mserp/expense-sheet.env`, also `root:mserp`, mode `0640`:

   ```dotenv
   EXPENSE_SHEET_CREDENTIALS_FILE=/etc/mserp/expense-sheet-service-account.json
   ```

Google's [service-account setup and authentication guide](https://developers.google.com/identity/protocols/oauth2/service-account)
and [atomic Sheets batches](https://developers.google.com/workspace/sheets/api/guides/batch)
describe the APIs used here. Authentication uses Google's maintained Go OAuth library.

## Deploy and activate

The normal release pipeline builds/packages `backend/export-expense-sheet` and
applies migrations 105/107. The deployment helper restarts an already active
exporter after release health checks. No frontend changes or API restart for credentials are
required. For a manual build, run `go build -o export-expense-sheet
./cmd/export-expense-sheet` from `backend/`; use GOOS=linux/GOARCH=amd64 for the VPS.
Install the built binary in the release using the normal release/deploy mechanism.
Do not replace files inside the active immutable release manually.

After credentials and sharing are ready, initialize the export headers once using
the same service user and environment as the service:

```sh
sudo systemd-run --wait --pipe --collect --uid=mserp --gid=mserp \
  --property=WorkingDirectory=/etc/mserp \
  --property=EnvironmentFile=/etc/mserp/mserp.env \
  --property=EnvironmentFile=/etc/mserp/expense-sheet.env \
  /opt/mserp/current/backend/export-expense-sheet -initialize -apply
```

This adds only the dedicated export headers, not a test financial entry. Read
them back in the spreadsheet and confirm the original fields remain intact.
Only after initialization succeeds, enable capture and the service:

```sh
sudo -u postgres psql -X -v ON_ERROR_STOP=1 -d mserp -c \
  'UPDATE expense_sheet_export_config SET enabled=true WHERE singleton;'
sudo install -m 0644 deploy/mserp-expense-sheet.service /etc/systemd/system/
sudo systemctl disable --now mserp-expense-sheet.timer 2>/dev/null || true
sudo systemctl stop mserp-expense-sheet.service
sudo systemctl daemon-reload
sudo systemctl enable --now mserp-expense-sheet.service
```

The `deploy/` paths above refer to this checkout; copy the service template to the
VPS first if necessary. The service is independent of the API's scheduled vendor
sync settings. For a read-only dry run, use the same `systemd-run` command without
`-initialize -apply`; it reports pending count without Google or database writes.

Watch the next real eligible expense reach the sheet and its delivery receipt:

```sh
sudo journalctl -u mserp-expense-sheet.service -n 40 --no-pager
sudo -u postgres psql -d mserp -c \
  'SELECT expense_id,attempts,last_attempt_at,delivered_at FROM expense_sheet_exports ORDER BY created_at DESC LIMIT 20;'
```

Delivery failures retain entries for automatic retry. Listener failures exit
nonzero and systemd restarts the service. Logs contain IDs/counts
and sanitized errors, not expense text, Google tokens or credential files. Inspect
the service journal while the transition is active; no separate alert channel is
configured. Renamed tabs resolve through their numeric IDs; changed/missing original
headers stop delivery until an operator restores or reviews them.

## Finish the transition

After checking the queue is empty, stop/disable `mserp-expense-sheet.service`, then
set `expense_sheet_export_config.enabled=false`. Retain the outbox and delivery
metadata with accounting history. Remove the service-account sharing and key
when no longer needed. Disable the service before rolling back to an old release
that lacks the exporter binary.

## Validation

`go test ./internal/expensesheet ./cmd/export-expense-sheet` exercises literal
values, exact numeric transport, leading zeros, metadata collisions and uncertain
commits (including a stale receipt read). `MSERP_EXPENSE_SHEET_TEST_DATABASE_URL`
must be a disposable `_test` database on localhost. Database tests run fresh and
pre-105 schemas, verify application-role ownership, disabled-by-default capture,
rollback, source-import exclusion, final work-order capture, Safety routing,
paid-cutoff boundaries, source deletion, committed-only notifications, startup
recovery, retries without a new save, cancellation and
snapshot/receipt immutability. CI supplies that local database automatically.
