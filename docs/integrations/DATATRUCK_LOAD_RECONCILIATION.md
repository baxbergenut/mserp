# Missing and replaced DataTruck orders

DataTruck may stop returning an order ID and expose another ID with the same
business load number. Imports retain both historical source records. A complete,
validated ID lookup records presence in `load_source_availability`; omission is
not authority to delete financial history or silently substitute an identity.

Loads excludes confirmed missing records from rows, counts and filter options by
default. The **Show missing from DataTruck** checkbox includes labelled historical
records. Gross Board suggestions and new selections exclude missing IDs. Existing
explicit links remain pinned, editable and visibly marked for review. Reappearance
restores visibility. Payroll/frozen source calculations do not discard evidence.

## Reviewed reconciliation

The release includes `backend/reconcile-loads`. Run as the service user from
`/etc/mserp` with `EnvironmentFile=/etc/mserp/mserp.env`. It performs read-only
DataTruck ID queries before acquiring financial locks; it never writes DataTruck.
Use a private directory owned by `mserp` for previews. Do not print credentials.

```sh
reconcile-loads -since 2026-10-14 -preview /private/path/preview.json
reconcile-loads -apply /private/path/preview.json -actor ADMIN_USER_UUID -request UNIQUE_REQUEST_UUID
```

Review each group's `sources`, `before`, `after`, `driverGross` and `hold`. The
preview is read-only and refuses to overwrite a file. Only groups with one current
provider ID, missing predecessors, identical customer, pickup/delivery dates,
ordered route and revenue qualify. Live current values must agree with imported
values. Multi-trip sources, cross-driver/date assignments, manual overrides,
unresolved/repeated identities, notes, load comments, placements, transfers and
finalized statements remain held. The report includes groups with no board repair.

If a current provider record changed outside the operational refresh window,
`-refresh-current` explicitly runs the native source-only refresh for the returned
IDs and records presence for the complete lookup before creating a new preview.
This optional mode writes imported sources and operational presence evidence;
ordinary preview does not. It never creates loads or changes fleet assignments.
Do not reuse an older preview after refreshing sources.

Apply re-fetches provider evidence and recomputes the entire preview under the
exclusive payroll source/financial protocol. Any changed source, board version,
candidate identity set or guard invalidates the digest. Refresh/review a new
preview; never bypass the fence. Per-group repairs keep the current source's
existing board slot when possible, consolidate every original driver-gross cent,
take current source mileage/revenue, and clear redundant slots with version bumps.
They never delete source loads. The complete batch, affected statement publication
and `payroll_command_receipts` entry (`command=load.reconcile`) commit together.
Publication failure rolls back every repair. Reuse the exact same actor, request
UUID and preview for an uncertain-result retry; committed receipts replay rather
than repair twice. Do not reopen finalized statements automatically.

Validation: `TestPrepareLoadReconciliation`, `TestLoadReconciliationDatabase`
(`MSERP_DRIVER_PAY_TEST_DATABASE_URL`, disposable localhost `_test` database),
`TestGrossBoardDatabase`, and `frontend/scripts/test-loads-views.mjs` after a `/api`
frontend build. The database tests run as `mserp_app` on fresh and migrated schemas,
including stale-preview rejection, publication rollback, receipt replay,
historical source preservation and restored provider visibility.
