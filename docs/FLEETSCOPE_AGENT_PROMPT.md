# FleetScope implementation prompt

For the extension to an existing sender, use
[the terminated-driver implementation prompt](FLEETSCOPE_TERMINATION_AGENT_PROMPT.md).
The remaining instructions describe the original hire sender contract.

Implement the FleetScope sender for the MSERP new-hire webhook. MSERP's receiver
and accounting review UI are already implemented in
`C:\BBG\SoftEng\mserp`. Work in `C:\BBG\SoftEng\fleetscope\api`.
Read `C:\BBG\SoftEng\fleetscope\AGENTS.md` completely first, check the separate
`api` and `web` Git statuses, and preserve unrelated changes. Leave changes
uncommitted for the user to review, commit, and push. Do not deploy or modify
production settings/data as part of implementation.

## Scope and observed architecture

This is a one-way handoff of **new recruitment hires for MS Express only**.
There is no historical fleet import, polling of the full fleet, profile/status
synchronization or writeback to FleetScope. Termination handling is a separate
extension described in the linked prompt. Ordinary
driver creation and CSV import must not emit events. Other FleetScope tenants
must neither enqueue nor deliver to MSERP.

The inspected checkout has:

- `internal/handlers/recruitment/recruitment.handler.go`: `RecruitmentRepo.Hire`
  checks recruitment permissions, gets `companyID` from the authenticated
  active company context, and calls `applicantdb.Hire`. Truck selection has
  additional driver/vehicle permissions. Preserve those checks.
- `internal/database/applicant/applicant.repo.go`: `Hire` starts a transaction
  on `ctxhelper.GetDB(ctx)`, locks the company-scoped applicant, returns the
  previous driver ID on duplicate hiring, inserts the driver, optionally assigns
  a truck, repoints child documents, marks the applicant hired, updates the
  recruitment process, and commits. This is the enqueue boundary. Queue the
  snapshot **inside this transaction**, after the driver/documents exist and
  before commit. Do not enqueue on the duplicate-hire early return.
- Hires without a truck are inactive. Trigger on successful recruitment hiring,
  not `drivers.status='active'`, or unassigned hires will be missed.
- Driver type derives from the application: `company` or `owner_operator`.
  Fleet-owner applicants map to `owner_operator`. Use the persisted driver's
  values. Read the stored hire date; don't independently calculate it.
- `internal/database/workflowoutbox/outbox.go` / `maintenance.go` implement
  durable encrypted email delivery. Follow the transaction, encryption, worker,
  retry, and shutdown conventions, but use a separate webhook outbox table and
  package; don't overload email templates or send HTTP in the hiring request.
- `internal/helpers/privacy` owns encryption. Driver contact/address fields
  and CDL numbers can be encrypted. Use the existing field-specific decryption
  helpers on company-scoped reads, then encrypt the complete immutable webhook
  snapshot at rest with suitable domain-bound authenticated data. Never send
  `enc:v1:...` ciphertext as a driver's contact/license value.
- `internal/config/server.go` owns runtime configuration; `cmd/api/main.go`
  starts workers using `appCtx` and the shared pool.
- `sql/migrations.go` embeds timestamped migrations and has an ordered baseline
  manifest. Add a new migration and matching fresh-install baseline. Never edit
  applied migrations or the squash boundary. Recruitment improvements are
  deferred; do not reactivate the paused recruitment triggers/workflow.

Recheck these files before editing because other work may have changed them.

## Operator-owned configuration

Add these optional environment settings:

```dotenv
MSERP_WEBHOOK_COMPANY_ID=<verified MS Express FleetScope company UUID>
MSERP_WEBHOOK_URL=https://erp.msexpressinc.net/api/integrations/fleetscope/driver-hired
MSERP_WEBHOOK_SECRET=<shared random signing secret>
```

All three empty means disabled. Partial/invalid configuration must fail startup
with variable names only, never values. Require a valid UUID, a secret of at
least 32 bytes, and HTTPS with no user info, query, or fragment; allow HTTP only
for loopback development targets. Keep the destination server-configured, not
an arbitrary tenant/browser-supplied URL. Do not hardcode or guess the real
company UUID. Validate the configured company exists before enabling dispatch.

Use the authenticated hiring transaction's company ID, compare it with the
configured company UUID, and only then queue. Bind each outbox row to company
and driver with scoped foreign keys. Recheck the configured company immediately
before delivery; changing configuration must never send another tenant's queued
payload to the destination. Keep configuration fixed for the process lifetime.

MSERP independently has `FLEETSCOPE_COMPANY_ID` and
`FLEETSCOPE_WEBHOOK_SECRET`. They must match the sender's UUID and exact secret
text. No user login, browser cookies, CSRF token, MSERP database credentials, or
`X-Company-ID` browser context participates in webhook authentication.

The two products share network infrastructure, but keep their databases/roles
independent. Use the HTTPS domain above. MSERP's API is bound to loopback on
port 18080 behind Nginx; another host/container cannot assume that
`127.0.0.1:18080` points to MSERP. Do not expose that port or widen firewall rules.

## Exact wire contract (version 1)

Canonical receiver sources:

- `C:\BBG\SoftEng\mserp\backend\internal\fleetscope\webhook.go`
- `C:\BBG\SoftEng\mserp\backend\internal\httpapi\fleetscope_handlers.go`
- `C:\BBG\SoftEng\mserp\docs\FLEETSCOPE_WEBHOOK.md`

Send `POST` with `Content-Type: application/json`, at most 65,536 bytes:

```json
{
  "version": 1,
  "eventId": "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
  "type": "driver.hired",
  "companyId": "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb",
  "occurredAt": "2026-09-28T16:00:00Z",
  "driver": {
    "id": "cccccccc-cccc-4ccc-8ccc-cccccccccccc",
    "fullName": "Example Driver",
    "driverType": "company",
    "hireDate": "2026-09-28",
    "phone": "+15555550100",
    "email": "example@example.test",
    "address": "100 Example Street",
    "city": "Example City",
    "state": "NY",
    "postalCode": "10001",
    "licenseNumber": "EXAMPLE123",
    "licenseState": "NY",
    "licenseExpires": "2028-09-28"
  }
}
```

Required envelope: exactly version 1, type `driver.hired`, UUID event/company
IDs, and a nonzero RFC3339 timestamp. Required driver fields: UUID `id`, nonempty
`fullName`, `driverType` (`company` or `owner_operator`), and valid `hireDate`
(`YYYY-MM-DD`). The other listed driver fields are optional strings, omitted or
empty when absent. The receiver rejects unknown fields. Never include SSN,
birthday, background checks, consent/application JSON, documents, attachment
URLs, pay rates, dispatcher/truck assignments, or unrequested information.

Trim fields. Maximum UTF-8 byte lengths: fullName 200, email 254, phone 50,
address 500, city 100, state/licenseState 2, postalCode 20, licenseNumber 100.
No NUL or CR/LF in these fields. Email must be a bare valid address. Optional
licenseExpires must be `YYYY-MM-DD`. Map any multiline address to a single line.
Select CDL data deterministically from this company's driver after repointing
documents (latest relevant record with a stable tiebreaker). If source data is
invalid, never truncate silently or block other companies; retain a visible
retryable/repairable delivery failure, with no sensitive values in errors.

Generate `eventId` once when queuing. Derive `occurredAt` from database time in
the hire transaction. Serialize the snapshot once and preserve its **exact UTF-8
bytes** across every attempt, including operator retries. Do not re-read current
driver data to build retries; this integration never propagates subsequent edits.

Headers:

```text
X-FleetScope-Timestamp: <current UTC Unix seconds, decimal>
X-FleetScope-Signature: v1=<lowercase hex HMAC-SHA256>
```

The signed message is `timestamp + "." + exact_body_bytes`. The key is the
literal secret's UTF-8 text bytes (do not base64/hex decode the secret). Refresh
timestamp/signature on every attempt; preserve event ID/body. MSERP accepts
timestamps within 300 seconds of its clock. Use constant-time signature
verification in any sender tests/reference receiver.

Go algorithm:

```go
stamp := strconv.FormatInt(time.Now().Unix(), 10)
mac := hmac.New(sha256.New, []byte(secret))
mac.Write([]byte(stamp + "."))
mac.Write(body)
signature := "v1=" + hex.EncodeToString(mac.Sum(nil))
```

HTTP 200 body:

```json
{"status":"accepted","intakeId":"dddddddd-dddd-4ddd-8ddd-dddddddddddd"}
```

`status` is `accepted` for a new intake or `duplicate` for an already-recorded
event/driver. Both mean delivered. Verify that acknowledgment shape; don't mark
an arbitrary HTML/login 200 as delivered. The receipt is durable before 200.

- 400: invalid contract; mark failed for repair/operator retry.
- 401: invalid signature/stale clock; expose safe failure metadata for repair.
- 403: wrong company; stop delivery and require configuration correction.
- 404: receiver disabled/not deployed/wrong path; keep pending for retry and
  expose the status; do not discard the hire.
- 409: same event ID with different body bytes; operator investigation, never
  automatically generate a replacement event ID to conceal the conflict.
- 413: oversized; mark failed for repair.
- Network failures, 408, 429, and 5xx: retry with bounded exponential backoff
  and jitter. Respect bounded Retry-After when provided. Retain failed entries
  with a documented authorized retry mechanism; never silently drop them.

New event IDs for an already-seen company/driver are acknowledged as duplicate
and do not refresh or reopen the original intake. This first version does not
implement rehire events. Document that explicitly.

## Durability and operations

Use a unique constraint on company + driver + event type to prevent duplicate
queue rows. Enqueue and hiring must commit/rollback together for the configured
company. After commit, wake a worker immediately; use a short periodic recovery
scan (for example five seconds) so crashes/missed wakeups recover. Never make
hiring wait on MSERP HTTP availability.

Persist attempts, next attempt, last safe status/error code, delivery time, and
lease state. Claim work safely across API replicas with a bounded lease or the
existing proven outbox pattern. Send HTTP outside long database transactions;
apply a short request timeout, don't follow redirects, cap response reads, and
honor cancellation. Recover abandoned leases. Never log payloads, secrets,
signatures, driver names/contact/license data, or raw response bodies.

Provide a company-scoped inspect/retry API or an explicit operator CLI/runbook
that does not expose plaintext snapshots. If using tenant routes, enforce
recruitment permissions and the active configured company. A general webhook
settings UI or changes in `web/` are not required for this first version.
Preserve data-protection/deletion policies for snapshots and outbox rows.

Add focused unit and isolated PostgreSQL integration tests for:

1. MS Express hire enqueues once; another company never enqueues/delivers.
2. Failed hiring rolls back the event; duplicate hiring is a no-op.
3. Inactive/unassigned hires deliver; manual driver/CSV additions do not.
4. Snapshot mapping/decryption and minimal allowed fields.
5. Fresh signature on retry with immutable event ID/body; cross-check against
   MSERP's `fleetscope.Sign` and a fake receiver.
6. Retry, restart/lease recovery, concurrent workers, response validation, and
   redirect refusal. Timeout after MSERP commits is safe to retry.
7. Disabled/partial configuration and company switching fail closed.
8. Migrations for both fresh and upgraded databases.

Run gofmt only on changed files, `go test ./...`, `go vet ./...`, and applicable
isolated integration tests. Update the FleetScope guide and write an operations
document explaining configuration, monitoring, and retries. Report the exact
files changed and remaining deployment/setup steps. Do not contact real hires
or change the production company/secret while testing.
