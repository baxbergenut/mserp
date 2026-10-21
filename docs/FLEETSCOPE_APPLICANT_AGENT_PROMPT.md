# FleetScope applicant sync: implement and deploy the sender

Implement FleetScope's side of applicant expense identities for MSERP. Commit,
push to main, deploy, backfill existing MS Express applicants, and verify delivery.
MSERP's receiver, expense selector and hire-history linkage are already implemented
in migration `134_fleetscope_applicants.sql`. Preserve unrelated local work.

## Purpose and identity

MSERP stores applicants outside its operational `drivers` table. HR selects either
a real driver or an applicant when adding expenses. A later hire makes the same
expenses appear under the local driver's Company & other expenses history, without
changing who pays or creating payroll deductions. Existing manual-name expenses
remain historical; do not match or merge them automatically.

FleetScope's **applicants.id and drivers.id are different UUIDs**. The inspected
`api/internal/database/applicant/applicant.repo.go` Hire transaction creates a new
driver and saves `applicants.hired_driver_id`. Send that exact relationship. Never
reuse the applicant UUID as a driver UUID or infer a link by name/email/phone.
Keep the existing `driver.hired` and `driver.terminated` payloads unchanged.

## Exact new wire contract

POST `https://erp.msexpressinc.net/api/integrations/fleetscope/applicant`

```json
{
  "version": 1,
  "eventId": "<unique UUID for this immutable source snapshot>",
  "type": "applicant.updated",
  "companyId": "<existing configured MS Express company UUID>",
  "occurredAt": "2026-10-21T14:30:00.123456Z",
  "applicant": {
    "id": "<FleetScope applicants.id>",
    "fullName": "First Last",
    "status": "applied"
  }
}
```

For a hired applicant send `status: "hired"` and
`hiredDriverId: "<applicants.hired_driver_id>"` inside `applicant`. That ID must
equal the existing hire event's `driver.id`. `hiredDriverId` is required for hired
and must be absent/empty for other statuses. Allowed statuses: `applied`,
`rejected`, `withdrawn`, `hired`. FleetScope currently has applied/rejected/hired;
do not invent a withdrawal transition. Rejected applicants remain available for
recording legitimate recruiting costs. No deletion endpoint is exposed.

Send **only** these fields. No `driver`, SSN, birth date, address, contacts,
documents or licenses in applicant events. Names must be trimmed, nonempty,
at most 200 UTF-8 bytes, with no CR/LF/NUL. UUIDs must be valid UUID strings.
The body limit is 64 KiB. `occurredAt` is the persisted source `updated_at` in UTC
(PostgreSQL microsecond precision), not retry/send time. Use a monotonic update
timestamp under the applicant row lock when changing sync-visible fields; avoid
transaction-start NOW() ordering that can regress behind a committed newer update.

Authentication reuses the existing server-only MSERP webhook secret and company
pin. For each attempt send fresh Unix-seconds `X-FleetScope-Timestamp` and
`X-FleetScope-Signature: v1=<lowercase hex HMAC-SHA256(secret, timestamp + "." + exactBodyBytes)>`.
The receiver accepts a +/-5-minute signing timestamp. Request payload bytes and
event ID stay identical across retries. `occurredAt` may be historical, but cannot
be more than five minutes in the future.

HTTP 200 means committed and returns
`{"status":"accepted|duplicate","applicantId":"<MSERP UUID>"}` (additional empty
lifecycle result fields may be present; ignore them). `duplicate` also acknowledges
an older source snapshot ignored to preserve newer state. 503/network failures:
retry the same event bytes. 400/401/403/404/409: hold visibly for review/configuration,
never mark delivered or silently drop. 409 covers event-ID byte reuse, conflicting
equal source timestamps, or attempting to clear/change an established hired-driver
link. Event IDs share a global namespace with hire/termination receipts.

## Sender implementation

Inspect `api/internal/database/mserpoutbox/`, the hire transaction above, and
`api/internal/database/driver/application.repo.go` (initial applications and
corrections). Reuse existing durable outbox encryption, signing, retry, lease,
redacted diagnostics and company scoping. The current hire outbox has
`UNIQUE(company_id,driver_id,event_type)` and a driver foreign key: **that schema
cannot represent prehire applicants or repeated applicant updates unchanged**.
Add a separate applicant outbox or a carefully migrated identity/revision model;
preserve existing hire/termination deduplication and immutable receipts.

Capture applicant creation, name corrections, rejection and hire in the same
database transaction as each source change. Enqueue the hired applicant mapping
alongside the existing hire event. Delivery order is independent: MSERP resolves
history after both the applicant mapping and explicit local hire setup/link exist.
Webhook delivery must not auto-complete MSERP driver setup, create pay/assignments,
activate the separate outbound fleet integration, or overwrite local driver data.

Add server-only applicant destination/enablement configuration consistent with the
existing sender. Restrict to the verified existing MS Express company. Do not put
secrets in browser configuration, logs, command output or this document.

Provide an idempotent, paginated backfill of **all existing applicants for that
company**, including rejected applicants and hired applicants with their recorded
`hired_driver_id`. Read current source snapshots and enqueue via the same code.
Exclude other companies and do not create synthetic production people. Guard the
backfill against concurrent updates with row locks/revision fences. Backfill retries
must reuse an already queued immutable snapshot or produce an equivalent new event;
never mutate the bytes behind an existing event ID. Validate/count and then execute
the backfill as part of this authorized rollout. Review malformed/missing source
identities rather than guessing.

## Verification and completion

Test the exact wire shape against MSERP's receiver tests and this contract. Cover
company isolation, no extra sensitive fields, rollback, repeated updates, stale
delivery, duplicate retries, changed-byte conflicts, worker restart, backfill/live
update races, distinct applicant/driver UUIDs and both hire delivery orders. Prove
that sending applicants alone creates no operational drivers/payroll entries.

In an isolated environment, create an applicant, deliver it, save a company-covered
expense with no truck, hire the applicant (new driver UUID), deliver mapping/hire,
complete/link the MSERP intake and verify the original expense in driver history
with unchanged amount, receipt and responsibility. MSERP's executable local test is
`frontend/scripts/test-applicant-expenses-e2e.mjs`; database coverage is included in
`TestFleetScopeDatabase` fresh/incremental paths as `mserp_app`.

Deploy the sender, confirm active FleetScope deployment/CI, enable the applicant
destination for the existing verified company, execute the real-data backfill,
and check terminal acknowledged counts against MSERP applicant counts/options.
Verify pending/failed events and do not report a queue insert as delivered. Confirm
HR's expense dropdown contains real applicants while the driver roster does not.
Report commits/releases, backfill counts, delivered/pending/failed counts and any
source records held for review. Do not expose applicant PII in the final report.
