# FleetScope driver lifecycle handoff

This version accepts one signed notification when MS Express hires a driver in
FleetScope. It stages an immutable hire on MSERP's Drivers page with a **New**
badge. Accounting reviews identity, enters a positive pay rate, and creates the
driver using the existing assignment transaction. Alternatively, staff can
explicitly link an existing driver without modifying that driver's data.
Pending hires are outside the managed fleet and settlement calculations.

The Drivers table and Tasks page check for arrivals every 15 seconds while the browser tab
is visible. Server receipt is immediate; this refresh is only for the open UI.
There is no fleet synchronization, historical backfill, profile-update
propagation, or rehire workflow. Termination notifications use the saved source
identity as described below. Identical normalized driver names remain subject
to MSERP's existing uniqueness constraint.

## Receiver configuration

```dotenv
FLEETSCOPE_COMPANY_ID=<MS Express company UUID from FleetScope>
FLEETSCOPE_WEBHOOK_SECRET=<random secret shared with FleetScope>
```

Both absent disables the receiver (404). Partial configuration, an invalid UUID,
or a secret shorter than 32 bytes fails API startup. Use a cryptographically
random value, for example 32 random bytes encoded as 64 hexadecimal characters;
both applications use the resulting 64-character text as the HMAC key. Never
commit it or put it in a frontend variable, URL, webhook payload, or log.

Production values belong in `/etc/mserp/mserp.env` and FleetScope's server-side
secret/environment configuration. No MSERP company record is required: MSERP
is the destination for MS Express, and pins the allowed FleetScope company UUID.

To identify the UUID, an authorized FleetScope operator can inspect the MS
Express company or run this read-only query against FleetScope's database:

```sql
SELECT id, name, dot_number FROM companies
WHERE name ILIKE '%MS%Express%';
```

Verify name and USDOT before using the UUID; never choose an ambiguous result.
Do not paste database credentials or the signing secret into chat.

## Transport and authentication

Public endpoint:

`POST https://erp.msexpressinc.net/api/integrations/fleetscope/driver-hired`

The Go route is `/integrations/fleetscope/driver-hired`; Nginx strips `/api/`.
Use the canonical HTTPS hostname even though the products share infrastructure.
The MSERP API listens on loopback, and a FleetScope container/other host has its
own loopback. No new public ports, shared database credentials, CORS changes,
browser sessions, or cross-database writes are needed.

The receiver independently verifies HMAC-SHA256, a five-minute timestamp window,
and the configured company UUID. Browser APIs remain session/CSRF protected.
Only this narrowly scoped integration route uses the signing secret instead.

See [the FleetScope implementation prompt](FLEETSCOPE_AGENT_PROMPT.md#exact-wire-contract-version-1)
for the complete versioned JSON schema, limits, signature algorithm, and response
handling. Executable definitions live in `backend/internal/fleetscope/webhook.go`.

Receipts use a unique event ID and SHA-256 of exact request bytes. Reusing an
event ID with different bytes returns 409. Company/driver IDs independently
deduplicate different event IDs. Neither case overwrites the first snapshot.
Only committed database work receives 200. Failed persistence returns 503 so
FleetScope retries. Completed intakes remain deduplicated even if their local
driver is subsequently deleted.

## Terminations

`POST /integrations/fleetscope/driver-terminated` uses the same configuration,
HMAC verification, company pinning, body limit and retry rules. The exact payload
and FleetScope sender instructions are in
[FLEETSCOPE_TERMINATION_AGENT_PROMPT.md](FLEETSCOPE_TERMINATION_AGENT_PROMPT.md).
Deploy migration `052_fleetscope_termination.sql` before enabling the sender.
The hire endpoint continues accepting only `driver.hired`; the termination
endpoint accepts only `driver.terminated`.

A termination atomically marks the explicitly linked driver inactive, clears the
dispatcher, ends the current truck assignment, and creates an **Offboard [name]**
task in the existing shared Tasks list. Assignments end in the current New York
accounting week. Source termination dates are retained for review, without
automatically rewriting earlier financial periods. Ownership, load links,
expenses, payroll, external identity mappings and assignment history remain.
Load imports cannot reactivate inactive drivers or assign inactive trucks/drivers.
Marking a driver/truck inactive through fleet forms also clears current assignments.

Charges are paused from the current New York week using the existing charge
rules. If saved/confirmed charge rows block that pause, the pause rolls back,
operational offboarding still completes, and the task flags the required
accounting correction. Database errors roll back the entire receipt for retry.
Manual deactivation retains its existing charge-pause validation.

Only a completed intake's explicit identity link authorizes automatic driver
changes. Unlinked/legacy/deleted identities create a review task. Names are never
used to automatically deactivate anyone. A pending hire is cancelled; setup
completion rejects it. A termination arriving before hire blocks the late setup
task as well. Neither delivery order can create a managed driver automatically.

Terminations deduplicate by event ID and company/driver identity. Event ID reuse
with different bytes returns 409 across both event types. The first termination
snapshot is retained. Retries do not reapply deactivation or recreate tasks after
completion/deletion. HTTP 200 includes `status` (`accepted` or `duplicate`) and
`terminationId`. Shared tasks refresh every 15 seconds while visible and idle.

## Accounting workflow

1. Find the driver with a **New** badge in the regular Drivers table and click
   **Set up**, or open Tasks and choose **Set up [driver name]**.
2. Review suggested matches (normalized name, email, phone, or CDL/state).
   These are suggestions, never automatic merges.
3. For a new person, choose Complete setup. Review the prefilled details, enter
   a positive pay rate, and choose dispatcher/truck/active status as appropriate.
   If possible matches exist, explicitly confirm this is a different person.
4. For an existing person, use **Already in MSERP?**, select and verify that driver's
   contact details, and confirm. Existing profile, pay, assignments, and active
   status are preserved; any needed edits happen through the normal driver form.

Setup completion and driver creation/assignment commit atomically, including
completion time and the authenticated accounting user's ID. Concurrent completion
returns 409 to the loser. A failed assignment leaves the intake pending and does
not create a partial driver. Completion removes the task and New badge.
There is no automatic expiration of the New badge.

Authenticated UI endpoints:

- `GET /driver-directory?page=1&pageSize=25&search=...&includeInactive=false`
  returns one combined management table (pending rows carry `intakeId`).
- `GET /driver-intake/{id}` returns a pending setup task; completed tasks return 404.
- `GET /driver-intake?page=1&pageSize=25&search=...` — pending hires with candidate matches.
- `POST /driver-intake/{id}/complete` — either
  `{"driver": <normal DriverInput>, "separateConfirmed": false}` or
  `{"linkDriverId": "<existing driver UUID>"}`.

## Rollout

1. Review/commit the MSERP changes and deploy through the normal PR/main workflow.
   Deployment applies `023_add_fleetscope_driver_intake.sql`. Keep receiver
   configuration absent until the verified company UUID and secret are ready.
2. Give the FleetScope agent `docs/FLEETSCOPE_AGENT_PROMPT.md`, implement and test
   its sender, then review/commit/deploy it initially disabled.
3. Verify the MS Express company UUID. Generate one secret in a secure operator
   workflow and set it on both servers. Configure/restart MSERP first.
4. Configure FleetScope's company, HTTPS destination, and matching secret, then
   restart/deploy its API. Capture only hires made after enabling the sender;
   enabling must not enqueue all existing drivers.
5. Verify the complete flow with synthetic data in isolated staging. For
   production, inspect the next legitimate hire's delivery and pending intake;
   do not create fake production employees. Check sender acknowledgment and
   retry state, MSERP pending record, accounting completion, and assigned rate.

Deploying the receiver does not enable sending. Configure the verified company
UUID and shared secret on both applications before expecting live deliveries.

## Local verification

```powershell
# backend, isolated disposable PostgreSQL only
$env:MSERP_FLEETSCOPE_TEST_DATABASE_URL = '<isolated test database URL>'
go test ./...
go vet ./...

# frontend
npm run lint
node scripts/test-gross-board.mjs
npm run build
# Build with NEXT_PUBLIC_API_URL=/api, then use a disposable local
# MSERP_INVESTOR_TEST_DATABASE_URL and psql on PATH for the browser flow:
node scripts/test-investors-e2e.mjs --offboarding-only
```

The PostgreSQL test creates/removes uniquely named test schemas and verifies
baseline/incremental DDL, concurrent duplicate receipts, immutable snapshots,
assignment rollback, explicit matching/linking, and deletion/retry behavior.
Unit tests cover both event envelopes/signatures and the HTTP company/auth boundary.
The database suite also verifies termination deduplication, both delivery orders,
cancelled setup, inactive assignment/import guards, task-failure rollback and
charge-pause exceptions as mserp_app. The browser test exercises both inactive
forms and a signed hire → explicit link → termination → task completion flow.
