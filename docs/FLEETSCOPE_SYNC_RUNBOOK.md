# FleetScope fleet sync: local implementation and rollout

Status: MSERP code implemented locally, default off. No production connection, deployment or reconciliation was performed. The FleetScope receiver and cross-repository acceptance remain prerequisites. See `FLEETSCOPE_SYNC_WIRE.md` for the implemented contract and `FLEETSCOPE_FLEET_SYNC_AGENT_PROMPT.md` for the receiver agent handoff.

## Configuration and permissions

Apply migration `103_fleetscope_fleet_sync.sql` in normal numbered migration order; fresh databases use `init.sql`. Applying the schema alone only captures local change revisions; it sends nothing. Existing intake identities are seeded only when one-to-one. New attempts to bind a second FleetScope identity to the same local driver fail transactionally instead of silently allowing competing lifecycle authorities.

Reviewed driver links reserve both identities while delivery is pending, preventing a concurrent intake from claiming the same person. The reservation does not grant new lifecycle authority until the receiver acknowledges it (or the matching trusted intake completes). Only a confirmed rejected operation can release an unconfirmed reservation through reviewed replacement.

All settings are server-only. No real values are included here:

```dotenv
FLEETSCOPE_SYNC_MODE=off
FLEETSCOPE_SYNC_URL=https://<verified-host>/<optional-prefix>/integrations/mserp/v1
FLEETSCOPE_SYNC_COMPANY_ID=<verified-MS-Express-company-UUID>
FLEETSCOPE_SYNC_ENVIRONMENT=staging
FLEETSCOPE_SYNC_KEY_ID=<live-key-id>
FLEETSCOPE_SYNC_SECRET=<independent-random-secret-at-least-32-bytes>
FLEETSCOPE_SYNC_BOOTSTRAP_KEY_ID=<temporary-review-key-id>
FLEETSCOPE_SYNC_BOOTSTRAP_SECRET=<different-random-secret-at-least-32-bytes>
```

Leave **all** optional outbound settings absent for the default off state. If any are supplied, a complete valid base configuration is required even while off. Bootstrap key/secret are an optional pair, required for reviewed mutation delivery. Outbound company must match the existing `FLEETSCOPE_COMPANY_ID`; webhook secret stays separate. Verify the company's actual FleetScope UUID and USDOT with the operator before supplying either side's allowlist. A company name is never an authentication boundary. The MSERP database is a single-company ERP; receiver enforcement of the same pinned tenant is mandatory.

Modes are two independent gates: environment mode off/reconcile/live and persisted state off/reconcile/live/paused. Off prevents all network requests. Reconcile sends only explicit reviews/retries. Configuring live alone does **not** activate the database. A five-second worker, independent of scheduled financial imports, captures latest committed projections and sends at most two operations per tick under a database advisory lock. Each tick is bounded to 45 seconds. Readback checks run every 15 minutes when no unresolved operations remain; detected drift pauses delivery. Pending/blocked operations themselves remain visible and block activation.

Settings → FleetScope shows local records, receiver comparisons, owner/crew differences, explicit exclusions, missing provisioning and the latest 200 operations (unresolved first, total unresolved count separate). It uses local cached data until Compare FleetScope is explicitly requested. Dedicated permissions: `fleetscope.read` plus `fleet.read` for viewing, `fleetscope.manage` for pause/retry, and `fleetscope.reconcile` plus built-in Administrator status for comparison, linking, baseline, replacements and activation. Session auth and normal CSRF protect writes. Grant only needed capabilities; secrets never enter this view.

## Review before deployment

1. Review both repositories' code and exact contract bundle. Run the actual MSERP sender against the actual FleetScope receiver on disposable databases; record both commits and contract hash. Current local tests use a TLS mock receiver, so they do not prove the other implementation is compatible.
2. Back up both databases and preserve existing FleetScope owner/lease/insurance evidence. Identify duplicate VIN/unit/driver identities, terminated/deleted records and current primary/co-driver pairs. Match on reviewed source IDs; do not automatically merge name matches or strip leading zeros.
3. Agree on the initial recorded owner of each truck in MSERP. Company, driver-owner and independent investor remain distinct. A driver owner can be different from its assigned driver or absent from the FleetScope driver roster. Do not import investor finances. Resolve actual ownership disagreements before authorizing the truck link, because linking adopts the MSERP owner/fields immediately.
4. Choose exclusions for historical/unassigned records with explicit reasons. An assigned truck/driver cannot be excluded. An excluded record later assigned in MSERP blocks delivery until reviewed and linked. Remote-only records are visible in the comparison and must be reviewed for duplicates or active crew conflicts; the sender never deletes them automatically.
5. Install receiver support with integration disabled, then deploy MSERP with outbound mode off. Production deployment/reconciliation requires a separate explicit user request. Keep all existing operational integrations available.

## Reconciliation and cutover (not performed)

1. Arrange a short freeze of ordinary managed fleet edits on both systems. Safety and lifecycle actions remain possible and invalidate affected reviews. Put the receiver into restricted reconciliation mode for the verified company and set MSERP environment mode reconcile.
2. Compare FleetScope. Review each driver identity and each existing truck link or missing-truck creation. A driver link also authorizes future termination webhooks for that exact person. Truck links require matching VIN and a full valid source profile. Missing color/class/GVWR require explicit provisioning; do not invent values. If a receiver-specific value is rejected, correct it through a fresh review.
3. Wait for acknowledged receipts. Recompare after any source change. A changed source revision invalidates review; snapshot expiry is 24 hours for individual reviews, 15 minutes for activation. Driver/truck links and source revisions must be acknowledged before assignments can use them.
4. Compare again and select Review assignments. Check every before/after crew, including empty crews for deleted local trucks. V1 submits the entire enrolled truck set as one atomic component; missing identities or safety restrictions block it. Queue the reviewed baseline and wait for acknowledgment. Trailer relationships and ownership are preserved.
5. Switch receiver authority to live (all legacy writer paths must enforce MSERP ownership), configure MSERP environment mode live, and take a **new** comparison after the final writes. Select Activate. It requires current source revision/version, same company/environment/epoch/hash, all local trucks acknowledged or explicitly excluded, no unresolved operations, matching truck/owner readback and the acknowledged complete crew baseline. Release the edit freeze only after confirmation.
6. Revoke bootstrap credentials after baseline completion. Future hiring binds through original hire receipts; normal linked truck/assignment changes use live credentials. Newly created trucks remain tracked as waiting for setup until their required FleetScope provisioning is reviewed; issuing short-lived bootstrap access for that review is deliberate. Monitor pending records and receiver safety/revision conflicts.

## Recovery

- **Transient/network/lost response:** original immutable operation ID/body is retried with fresh timestamp/nonce. Receiver receipt replay resolves uncertain commits without duplication. Backoff grows to roughly 17 minutes, minimum one minute, honoring supported Retry-After. After 20 total attempts it blocks; explicit retry permits another attempt. Retries never allocate a new source revision/assignment sequence.
- **Pause:** stops new delivery claims; an already sent request can finish. The source ERP continues working and capture remains durable. Pausing does not return write authority to FleetScope. Database/env off also preserves queued records.
- **Resume:** Resume review verifies capabilities/scope/epoch and enters reconciliation, not live. Explicitly retry unresolved automatic operations to authorize their original bytes for reconciliation delivery; active, unexpired leases cannot be retried. This resolves any uncertain requests before a replacement. Then compare, reconcile and activate again.
- **Confirmed rejection:** pause first; Review replacement supersedes only blocked operations with confirmed 409/422 noncommit responses. Recompare. For a linked truck, Review refreshes its existing identity from current source fields and receiver revision; identity relinking is intentionally unavailable. For crew rejection, review a fresh assignment baseline. Resume review to deliver approved work, verify readback, then reactivate. The next source/assignment counter is based on the last committed receipt, never advanced by a rejection.
- **Drift or safety action:** automatic readback pauses unexpected truck/owner/crew changes. Review the actual difference; resolve safety/employment issues before preparing current assignments. A source change is not permission to clear a receiver hold. Capture a fresh comparison after any correction.
- **Restore/epoch mismatch:** scope/epoch/hash mismatch pauses delivery. Never edit the epoch or erase receipts to force replay. Keep both workers off, fence old credentials/epochs, restore corresponding receipt/mapping history where possible, and perform a separately reviewed recovery migration/baseline. Automatic epoch reset and arbitrary identity replacement are intentionally not provided.
- **Rollback:** prefer pause/off and code rollback that preserves migration 103 and its receipts/mappings. Do not drop the new tables or re-enable competing FleetScope writers while commands are in flight. Returning authority requires a separate reviewed plan.

No deployment or automatic retries can guarantee simultaneous availability of two systems. The implementation provides explicit authority, transactional capture, durable idempotent delivery, dependency checks and visible blocking rather than silently guessing through conflicts. Intermediate source edits can be coalesced into the latest current-state projection; this integration does not reconstruct historical accounting or every intermediate crew transition.

## Local validation

```powershell
# backend; isolated localhost PostgreSQL database with _test in its name
$env:MSERP_FLEETSCOPE_TEST_DATABASE_URL='postgres://<test-user>@127.0.0.1:<test-port>/<database>_test?sslmode=disable'
go test ./...
go vet ./...
# frontend
npm run lint
node scripts/test-gross-board.mjs
node scripts/test-driver-board.mjs
node scripts/test-driver-pay.mjs
node scripts/test-phone.mjs
$env:NEXT_PUBLIC_API_URL='/api'
npm run build
node scripts/test-fleetscope-sync-views.mjs
```

Database tests create/drop only their uniquely named schemas. They cover fresh/incremental schemas, runtime role access, rollback capture, investor/crew separation, missing dependencies, stale/duplicate reviews, immutable payloads, response loss after remote commit, activation and drift pausing. Sender tests cover scope/contract, signed bytes, fresh retry nonces, snapshot validation and schema/fixture parity. Receiver-side security, legacy writer guards and real interoperability remain required acceptance tests in the FleetScope repository.
