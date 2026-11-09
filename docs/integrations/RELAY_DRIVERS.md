# Relay driver management

Fuel > Drivers is the third tab, after Overview and Transactions. The roster
includes Relay drivers without transactions or policy assignments, including
drivers automatically created when they sign into their ELD app. Existing ELD
connections stay in Relay; the documented integration API has no connection
management endpoint. Link these existing drivers before considering Add driver.

## Supported actions

- Refresh the complete paginated Relay roster and each driver's policy assignment.
- Add an active MSERP driver, edit Relay name/email and driver data, remove a
  Relay driver, and explicitly link existing Relay identities.
- Assign MS Express Company - ELD, MS Express O/O - ELD, or Non ELD Policy.
  Flinn policies are excluded from assignment choices. Existing Flinn assignments
  remain visible rather than hiding accounts that might require removal.
- Enable/disable the current policy without resetting usage, or unassign it.
  Assigning a different policy resets usage, which the dialog explains.
- Non ELD Policy requires the exact data fields `Driver Pin` and `Truck #`.
  Empty edits preserve current values fetched on the server. PINs are never
  stored in the roster, receipts, browser view memory, or logs, or returned by
  the API. Only Truck # is exposed from Relay custom data.

Policy creation/editing, ELD setup, app invites, payment codes, and onboarding
status are outside this tab. Policy definitions remain managed in the portal.

## API and permissions

- `GET /relay-drivers`: cached roster enriched with live MSERP statuses/links.
- `POST /relay-drivers/refresh`: fetches Relay; failed/partial refreshes keep the
  last complete snapshot. Four bounded workers fetch policy assignments.
- `POST /relay-drivers/change`: add/edit/remove/assign/unassign/enable/disable.
  Requires a request UUID and, for existing accounts, the displayed revision.
- `POST /relay-drivers/{relayId}/link`: explicitly confirms an existing identity.
- `POST /relay-drivers/operations/{id}/verify`: acknowledges a portal-reviewed
  uncertain result after the in-flight timeout, then refreshes the roster.

Reads/refresh require `fuel.read`; vendor writes/recovery require the new
`fuel.drivers.write` permission. Existing account linking additionally requires
`tasks.write` and the existing Relay review assignment visibility. The built-in
Administrator has the new permission through the code-owned catalog. Other roles
must receive it explicitly in Settings. No migration broadens existing roles.

Roster discovery uses the same environment-scoped `relay_driver_links` as fuel
imports and creates no fleet drivers. Phone/name matches are suggestions only.
Confirmed links remain authoritative, including multiple Relay IDs per driver.
Add driver explicitly selects a local driver and records a confirmed link for
the newly created Relay account. Linking updates unassigned historical fuel
transactions with the existing audited review semantics. Removal retains links,
reviews, transactions and the MSERP driver. Terminated/inactive drivers still in
Relay are flagged even when their policies are disabled. Unlinked accounts show
a warning because their employment status cannot yet be established.

Migration 075 is required before deploying the API. It adds
`relay_driver_rosters` and `relay_driver_operations` to both incremental/fresh
schemas. A database advisory lock serializes refreshes and mutations per Relay
environment across API instances. External writes first commit a receipt; uncertain requests
are never automatically retried. Fresh driver/assignment reads reject stale
revisions; usage counters are deliberately excluded. Relay has no documented
conditional-write primitive, so a concurrent portal edit after this check
cannot be made atomic with an MSERP action. An uncertain result blocks further
changes until an operator checks the portal and explicitly acknowledges it.

The existing server-only `RELAY_ENVIRONMENT` and selected
`RELAY_PRODUCTION_API_KEY` / `RELAY_STAGING_API_KEY` are reused. Documentation
credentials are separate and are not application configuration.

## Vendor contract and validation

Source inspected October 11, 2026:
[Relay TMS Fuel API](https://docs.relaypayments.com/), `tmsfuel.yaml`.
Driver/policy paths are under `/api/integrations`, while fuel transactions remain
under `/api`. Drivers use `/drivers/` with offset/limit pagination (maximum 50).
Policies use `/fuel/policies/`. Assignment POST uses `driver_id` and `policy_id`
in its JSON body. Assignment GET, PUT and DELETE all use the **driver ID** in
the path, without a query parameter. No private portal endpoints are used.

Live Relay staging verification on October 11, 2026 resolved the inconsistent
OpenAPI parameter labels: PUT with an assignment ID returned HTTP 400; PUT and
DELETE with the driver ID succeeded. The actual Go client passed create, edit,
assign, disable, enable, unassign and remove, with read-back checks after each
operation. Disable/enable retained the assignment ID and unchanged limit values.
The temporary driver used a reserved fictional phone number and the staging
Company policy; the full roster confirmed its removal. Deleted-driver lookup
returns 403, so cleanup verification checks the complete roster instead.
No production driver was changed. Production-specific ELD eligibility and the
three production policy configurations were not exercised by this staging test.

The opt-in `TestLiveStagingDriverPolicyLifecycle` uses a hardcoded staging host
and requires `MSERP_RELAY_LIVE_STAGING_API_KEY`. It creates and removes one
synthetic driver and uses the existing staging Company policy. Ordinary test
runs skip it. Credentials remain server-side and must never appear in output.

`MSERP_RELAY_TEST_DATABASE_URL` must name a disposable localhost `_test` database.
`go test ./internal/repository -run TestRelayDriverDatabase -v` checks fresh and
migrated schemas as `mserp_app`, including one-connection pools, task privacy,
duplicate prevention, termination flags, PIN redaction, usage retention, stale
revisions, partial refreshes, history retention, and uncertain-operation recovery.
`go test ./internal/relay ./internal/httpapi` checks pagination, request formats,
permissions, validation, error redaction and mutation redirect/retry behavior.
`node scripts/test-relay-drivers-views.mjs` tests the static `/api` build against
isolated fixtures, including desktop/narrow layout, linking, add, read-only
controls and ELD/non-ELD assignment forms. Append `--serve` for manual inspection
at localhost port 55477. It never calls Relay.

## Automatic termination offboarding and bulk actions

Migration 076 adds the source and termination date to durable operation receipts.
When Relay is configured, the server starts an offboarding pass immediately and
repeats it every minute, independently of scheduled transaction imports. It uses
current `drivers.status`, `termination_date`, and an explicit environment-scoped
Relay link. A terminated driver's assigned policy is disabled; the Relay account
is removed on termination date + 10 calendar days in America/New_York. Drivers
already overdue are removed on the first successful pass; no new grace period is
introduced. Accounts with no policy are still removed when due. Unlinked Relay
accounts remain visible for manual review and are never automatically removed.

The worker refreshes stale rosters (five minutes), reads current upstream policy
state, and shares the manual-operation environment lock. System receipts retain
the local driver and termination date, with `source=termination` and no human
actor. A committed pending receipt precedes each remote action. Employment/link
rows are rechecked and locked through the action so concurrent rehires cannot
race removal. Rehires cancel later actions but do not automatically restore fuel
access. Uncertain requests block all further actions until portal verification;
definitive failures back off for 15 minutes. Successful removal preserves local
profiles, identity links and transaction history.

The Drivers tab supports page selection and selections across pages. Bulk
assign/enable/disable/unassign/remove uses `POST /relay-drivers/bulk`, at most 100
distinct drivers and a separate request UUID/revision per driver. Actions are
independent, with per-driver results and one final roster refresh. Successful
rows leave the selection; failed rows can be reviewed individually. Non ELD bulk
assignment uses each driver's existing PIN/truck values, never a shared PIN.
The same `fuel.drivers.write` permission applies.

The fresh/migrated database suite also covers day-10 removal, existing overdue
accounts, no-policy removal, rehire protection, uncertain automation, bulk partial
failure and idempotent retries. Browser fixtures cover bulk selection/results.

## Status-linked policy automation (migration 208)

`RELAY_STATUS_POLICY_ENABLED=true` opts the selected Relay environment in. It is
OFF by default. Apply 208 before the binary, review a fresh provider comparison,
then activate explicitly. The existing termination worker runs first and remains
independent of this flag: disable on termination and delete at day 10.

Current dated team policies follow the main driver's current fleet status using
`payroll_driver_at` on today's New York date. Future crews and historical/custom
Gross Board text are excluded. Main-driver personal availability precedes the
board label; each linked person's own current termination/inactive state remains
authoritative. Fleet status reconciliation must also be enabled.

On: READY, REST, COVERED, DISPATCHED, ENROUTE, RESERVED, DEADHEAD, In the YARD,
Hold for Load, Issue with PU/Del, At PU, At DEL, 34 RESET.
Off: VACATION, HOME, SHOP, STOP, Left, Family issue, Health issue, HOME (YARD),
NEW DRIVER, ACCIDENT, blank and unknown/retired labels. Comparison trims space
and ignores case. Explicit Left still uses native termination workflow.

Status always governs existing assignments for active, verified linked drivers.
This includes pre-existing or manually disabled assignments: an ON status enables
them on the next successful pass. There is no hidden manual-block or ownership
exception. This behavior was explicitly approved. A manual/portal disable while
the effective status is ON is temporary, not a persistent security block. To keep
fuel OFF, change the effective fleet status or disable this automation. Relay
cannot identify same-state portal actions; it need not do so under this rule.
No assignments, limits or usage are changed, and missing assignments/accounts are
never created. Currently terminated/inactive drivers are never enabled. Under
this opt-in feature, a rehired active driver with an existing assignment follows
current status again; a removed Relay account is never recreated automatically.
The termination worker itself still does not restore rehires.

GET `/relay-drivers/status-policy-preview` requires fuel.read. It never calls
Relay or writes; it works with automation off. A repeatable-read snapshot returns
items and metadata: rosterRefreshedAt, automationEnabled, fleetStatusEnabled,
evidence=cached_relay_roster. Sensitive availability labels/internal context are
redacted. Actions distinguish changes, no assignment, no-change, unlinked,
employment, uncertainty and backoff holds. Cached impact is not confirmation of
current remote state. Activation must use a fresh provider comparison.

A minute sweep follows changes by any actor. Durable cursor progress prevents
slow accounts starving later accounts. The existing environment lock serializes
manual/offboarding/status work. Initial assignment reads and no-ops do not hold
the fleet lock. Each actual change commits a pending receipt, then reacquires
native fleet and identity locks, checks current source/context and upstream
assignment, sends only enable/disable and reads back confirmation. That final
fence is bounded to 10 seconds total, with a 5-second lock wait limit, and can
delay fleet edits for that one action. It never spans the roster loop. Known-
unsent aborts are reconsidered safely. Definitive provider failures back off 15
minutes. Unknown sent outcomes remain pending until explicit portal review.

A settled pass reads one assignment per active linked account (~91 GETs/minute
for 91 accounts), plus existing five-minute refresh and actual-action checks.
Throttling can delay convergence. Relay has no conditional-write API: portal
changes after final read cannot be atomic with an ERP mutation.

Validation: TestRelayStatusEnabled, TestRelayDriverDatabase (fresh and migrated
schemas, fake provider, actual source/link races and local writer fencing),
TestRelayStatusPreviewRedaction, TestRelayDriverPermissions and
TestRelayStatusPolicyDefaultOff. Existing termination tests remain in the same
harness. The live staging lifecycle is opt-in and is not part of these checks.
