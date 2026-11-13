# Permissions and access

The application uses one role per user. Grants apply to all records in the permitted area; task-assignee restrictions still apply. The built-in Administrator has the entire code-owned catalog and cannot be edited. Last-administrator protection, version checks and access audits remain mandatory.

## Per-user overrides

Settings > Users > Edit user shows User permissions after the account details. Each
capability and expense-category action can inherit its role, explicitly Allow, or
explicitly Deny. The editor previews effective access and can reset all exceptions
to the role. Exceptions survive ordinary role changes; selecting Administrator
clears them because Administrator accounts retain protected full access.

`ManagedUser.permissionOverrides` maps catalog keys to `allow` or `deny`.
`expenseCategoryOverrides` maps category UUIDs to action maps (`canView`,
`canCreate`, `canEdit`, `canDelete`) using the same values. Missing fields preserve
saved exceptions for existing API callers; empty objects clear them. Unknown keys,
values and categories are rejected. Only existing `access.manage` holders may
save exceptions, with that effective permission rechecked under the access lock.

Apply exceptions to current role grants, then remove permissions with missing
prerequisites until stable. Denying View therefore disables dependent actions,
including explicitly allowed ones; it never adds prerequisite grants. Category
actions likewise require effective category View. Administrator-only operations,
task assignment and record-specific rules remain unchanged. SaveUser atomically
persists account details and overrides, versions/audits the save, and revokes every
session. Sessions and board presence evaluate current effective access.

Migration 217 adds the two JSON objects with empty defaults, preserving existing
access. Install the matching deployment helper before release: it quiesces writers
and rejects releases without migration 217 once the new columns exist, since older
applications would ignore explicit denials. Deploy migration, API and frontend
together. Validate `TestUserPermissionOverridesDatabase` on a disposable local
database and `frontend/scripts/test-user-permissions.mjs` against review-local.

## Contract

`backend/internal/repository/access_permissions.go` owns capability keys, groups, labels, supported actions, prerequisites and sensitive flags. Both the session and Settings expose this catalog. Settings renders a searchable matrix; frontend capability helpers validate its prerequisites. An action requires its area's View grant. The API rejects unknown, obsolete, duplicate or inconsistent permissions. Expense-category actions likewise require category View.

Drivers, Trucks, Dispatchers, Updaters and Investors have independent grants. Sensitive driver addresses, emergency contacts and CDL details require `drivers.private.read/edit`. CDL and IRP metadata/downloads use `drivers.documents.read/edit` and `trucks.documents.read/edit`. Driver rates, dispatcher commission rates and investor terms have separate read/edit capabilities. Driver Pay, Investor Pay and Dispatcher Pay have independent View grants; driver/investor reports have separate Edit, Finalize and Reopen grants. Escrow has separate Add, Edit and cancellation (Delete) grants; cancellation cannot change a release’s amount or week. Operational Gross Board amounts remain visible with board access.

`expenses.accounting.verify` authorizes blue accounting verification together with category View. Role names have no authorization meaning. Administrators also use accounting verification and alone can cancel somebody else's verification. Manager verification is retired; its historical approval records are deleted. An accounting verifier can remove their own check while they retain verification and category View access. Existing approval IDs, authors, timestamps and colours remain unchanged.

## Enforcement

`backend/internal/httpapi/access_permissions.go` maps every authenticated HTTP method and route. Literal routes take precedence over parameter patterns. Unknown mappings deny access. Handler-owned policies are explicit for category permissions, file ownership and requests whose action is in the body. The route-coverage test walks the real router.

Combined workflows preflight all requested effects before writes or vendor calls. New driver setup needs Drivers Add, private-detail editing, and compensation/escrow access (or the team financial grant). Completing a FleetScope intake also requires Tasks Review and private-detail viewing. New trucks, driver-owner investor creation, documents and service enrollment add their respective requirements. Existing ordinary status and assignment changes retain automatic accounting effects and financial locks. Team changes require Driver Pay Edit. Ownership changes do not grant access to settlement terms. Creating or reactivating a driver-linked investor additionally requires investor creation/editing access.

Responses are projected before JSON encoding, including profile lists, details, embedded records and mutations. Hidden values are omitted, never replaced with zero. Financial endpoints return the separately authorized report. `/dispatcher-pay` returns only dispatcher commissions and period information. Lookup endpoints return typed operational choices without private details, compensation, notes or documents; driver contact fields are included only with Drivers View.

Profile writes distinguish absence from explicit null/zero. Protected fields omitted by an editor remain unchanged, including concurrent changes to those fields. Sending a protected field without its edit grant fails. File downloads and attachments check the owning driver/truck and document permissions. Unattached files belong to their uploader; only that uploader or an Administrator can use them.

## Browser behaviour

Sessions expire eight hours after sign-in. There is no trusted-device extension;
legacy `trustDevice` login input is ignored. `AUTH_SESSION_TTL` may select a shorter
lifetime; values above eight hours are capped. Migration 170 caps existing sessions
from their original creation time and preserves shorter expirations. Activity never
renews a session.

Viewers retain filters, sorting, date navigation, inspection, spreadsheet selection and copying. Mutation controls are unavailable; event handlers and save functions also guard permissions. Read-only boards and payroll do not acquire dirty state, autosave work, undo actions or navigation blockers.

The shell refreshes the session on navigation, focus, visibility changes, every 30 visible seconds and permission-denied responses. Access is versioned by user and role revisions. A changed revision cancels shared pending reads, rejects obsolete responses and remounts page state. Unwritable drafts/editors are discarded with a notice and their navigation blockers are removed. Rejected writes are not automatically replayed. Ordinary conflicts and network failures retain legitimate drafts. Backend checks remain authoritative on each request.

Settings > Users shows Online when an active, unexpired session has a browser heartbeat within two minutes. Otherwise Last activity shows the latest recorded heartbeat or successful login in New York time. Migration 169 backfills retained heartbeats, falling back to the last known login when older activity is unavailable. Activity is persisted outside sessions and survives logout, expiry and revocation; background API/SSE validation does not advance it. An open visible tab sends heartbeats every 30 seconds, so Online indicates recent browser presence rather than proof of keyboard/mouse use.

## Migration and release

Migration **112_permissions_v2.sql** and the matching fresh-install schema cut over together. Fleet grants map to operational directory actions; they grant no private details, documents or compensation. Financial capabilities derive from prior financial grants. Existing Accountant roles receive explicit accounting-verification access. Names and memberships remain intact. Invalid write-without-view actions are disabled instead of expanding visibility.

Each non-system role receives a `permissions_v2_migration` audit containing before/after permissions and category grants, with `reviewRequired`. Review those rows and explicitly assign private/document grants and any missing setup prerequisites. The migration clears sessions and adds file-uploader attribution. Historical unattached files without an uploader remain Administrator-only.

Deploy backend, migration and frontend as one coordinated release, after isolated validation. All users sign in again. Do not roll back to a pre-112 application: it interprets permissions differently. Use a permission-compatible build, or an explicitly reviewed database-and-application recovery procedure.

## Validation

Run `go test ./...`, `go vet ./...`, frontend `npm run lint` and a production build with `NEXT_PUBLIC_API_URL=/api`. Set `MSERP_ACCESS_TEST_DATABASE_URL` only to a disposable localhost database ending in `_test` to exercise fresh/upgraded schemas and runtime queries as `mserp_app`.

Start `frontend/scripts/review-local.mjs` with `MSERP_REVIEW_TEST_DATABASE_URL`, then set `MSERP_REVIEW_ACCESS_FILE` to its generated local access file and run `node scripts/test-permissions-v2.mjs`. This exercises real restricted accounts, independent pages, raw JSON privacy, safe edits, read-only keyboard actions, separate payroll, Settings dependencies and access loss with open editors/pending saves/remembered tabs. Run the existing Gross Board, Status Board, payroll and profile suites as well. Synthetic test credentials must stay local; no external integration writes are needed.
