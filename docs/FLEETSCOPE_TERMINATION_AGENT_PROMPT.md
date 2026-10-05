# FleetScope agent prompt: terminated-driver notifications

Extend the existing FleetScope → MSERP hire webhook integration to send driver
termination notifications as well. Work in the FleetScope repository, read its
AGENTS.md and relevant nested guides, inspect Git status, and preserve unrelated
changes. Inspect the existing sender and termination transaction before editing.
Implement and test the changes; leave them ready for review. Do not deploy or
change production settings/data as part of this implementation.

MSERP's receiver is implemented in `backend/internal/fleetscope/webhook.go`,
`backend/internal/httpapi/fleetscope_handlers.go`, and
`backend/internal/repository/fleetscope_termination.go` in the MSERP repository.
Migration `052_fleetscope_termination.sql` must be deployed before enabling sends.
Keep the existing hire contract and sender working unchanged.

## Trigger and delivery

- Enqueue `driver.terminated` when a driver actually becomes terminated, in the
  same database transaction as that state change. Cover every authorized
  termination path. A generic inactive/unassigned driver is not necessarily
  terminated; inspect the domain and use the real termination transition.
- Only the configured MS Express company may enqueue or deliver. Derive company
  identity from the authorized, company-scoped transaction; never trust arbitrary
  request IDs or allow another tenant's driver to enter this outbox.
- Use the durable webhook outbox/worker already used by hires. Queue once per
  company + driver + event type. Duplicate termination actions must be no-ops.
  A rolled-back termination must leave no event. HTTP happens after commit.
- Do not backfill all previously terminated drivers when enabling the feature.
  Emit for new actual terminations only. Scheduled future terminations enqueue
  when they take effect, not when someone first enters a future date.
- Preserve hire rows and any pending hire deliveries. MSERP handles either
  arrival order. This contract does not implement rehire/reactivation cycles;
  do not generate new event IDs to bypass that limitation.

## Exact version 1 contract

POST to this server-configured URL:

`https://erp.msexpressinc.net/api/integrations/fleetscope/driver-terminated`

```json
{
  "version": 1,
  "eventId": "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
  "type": "driver.terminated",
  "companyId": "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb",
  "occurredAt": "2026-10-05T16:00:00Z",
  "terminationDate": "2026-10-05",
  "driver": {
    "id": "cccccccc-cccc-4ccc-8ccc-cccccccccccc",
    "fullName": "Example Driver"
  }
}
```

Every shown field is required. Event/company/driver IDs are UUIDs. `driver.id`
must be the same stable ID used by the hire event. `occurredAt` is the actual
termination event time in RFC3339. `terminationDate` is the persisted effective
date, YYYY-MM-DD, no later than today's date in America/New_York. Backdated
terminations are accepted; do not invent historical effective dates.

The driver object contains only `id` and `fullName`. Trim the name, require
1–200 UTF-8 bytes, and reject NUL/CR/LF. Use existing decryption helpers where
needed. Do not send termination reasons, SSNs, pay details, contact data,
documents, assignments, or the full driver/profile snapshot. The receiver
rejects unknown fields. Maximum body size is 65,536 bytes.

Generate the event ID and serialize the payload once in the enqueue transaction.
Store immutable exact UTF-8 bytes using the existing encrypted-at-rest outbox
conventions. All retries, including manual retries, reuse the event ID and bytes.

Use the existing shared secret and company setting. Add an optional
`MSERP_TERMINATION_WEBHOOK_URL` alongside the existing hire destination. Empty
disables termination sending while preserving hires. When set, require the
existing company UUID/secret and a valid HTTPS URL (HTTP only for loopback tests),
with no credentials, query, or fragment. Never derive a destination from tenant
input, change the existing hire URL, or expose secrets to the browser.

Headers and signature are identical to hires:

```text
Content-Type: application/json
X-FleetScope-Timestamp: <current UTC Unix seconds, decimal>
X-FleetScope-Signature: v1=<lowercase hex HMAC-SHA256>
```

Sign `timestamp + "." + exact_body_bytes` with the literal secret text's UTF-8
bytes; do not hex/base64-decode the secret. Refresh timestamp and signature on
every attempt. The receiver permits a 300-second delivery clock window.

HTTP 200 acknowledges committed work:

```json
{"status":"accepted","terminationId":"dddddddd-dddd-4ddd-8ddd-dddddddddddd"}
```

`status` is `accepted` or `duplicate`; either is success. Validate the JSON shape
and UUID `terminationId`. Do not expect `intakeId` for terminations or accept an
HTML/login page as an acknowledgment. Hire responses continue using `intakeId`.

Retry network failures, 408, 429, and 5xx with bounded exponential backoff/jitter;
respect bounded Retry-After. A 404 means the receiver is unavailable or not yet
deployed: retain and retry. Surface 400/401/403/409/413 for operator repair.
409 means an event ID was reused with different bytes; never conceal it with a
replacement ID. Preserve failed events, safe diagnostics, and authorized retry.
Use bounded HTTP timeouts, response reads, cancellation, and no redirects.

## Expected MSERP behavior

MSERP resolves the driver through the saved company/driver identity established
by completed hire intake. It marks the linked driver inactive, ends current truck
and dispatcher assignments, and creates one `Offboard [name]` task atomically.
Ownership and historical financial/load/assignment records remain intact.
Assignments end in the current New York accounting week; the source termination
date stays on the receipt/task for review of any earlier settlement corrections.

Charges pause from the current week where possible. If saved financial changes
prevent a pause, operational offboarding still succeeds and the task explicitly
requests accounting review. No confirmed/saved charge is silently erased.

Pending hires are cancelled. If no saved identity link exists (including legacy
drivers never linked through hire intake), MSERP creates an actionable review
task without guessing by name. This is a successful durable receipt, not a
sender failure. Termination arriving before hire prevents a late setup task.
Duplicate events cannot recreate completed/deleted tasks or repeat deactivation.
There are no direct MSERP database writes or callbacks to FleetScope.

## Verification and handoff

Add focused unit and isolated integration tests for transaction rollback,
duplicate/concurrent termination, other-company exclusion, future scheduled
termination, disabled configuration, immutable payloads and retry signatures,
both acknowledgment shapes, lease/crash recovery, 404/5xx retries, and repairable
4xx failures. Verify that existing hire tests still pass and enabling does not
backfill prior terminations. Test the JSON against MSERP's validation/signature
contract using fixtures or an isolated local receiver.

Run the repository's required format, tests, and static checks. Update its
runtime configuration and webhook operations documentation. Report files changed,
test results, and rollout steps: deploy MSERP migration/receiver first, deploy the
sender disabled, then configure the verified company/secret/termination URL.
Never log payloads, driver PII, signing secrets/signatures, or raw response bodies.
