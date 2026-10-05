# MSERP Agent Guide

Read this file before doing anything else in a new task. Then read the nearest
nested `AGENTS.md` for the area being changed. Do not scan the entire repository
for routine work; use the map and targeted searches below first. Update this file
when architecture, commands, environment variables, or important invariants
change.

## What this repository is

MSERP is an internal ERP for MS Express Inc. It has two independently run apps:

- `backend/`: Go 1.23 HTTP API using chi and pgx/PostgreSQL.
- `frontend/`: Next.js 16 App Router UI using React 19, TypeScript, Tailwind CSS
  4, and Lucide icons.

In local development the browser talks directly to the Go API. Production uses
the same-origin Nginx `/api/` reverse proxy. Authentication uses database-backed
users and opaque HttpOnly-cookie sessions. There is no container setup or
generated API client. The API does not run migrations on startup; the production
deployment helper applies numbered migrations recorded in `schema_migrations`.

## Start every task here

1. Run `git status --short`; the worktree may contain user changes. Preserve
   unrelated changes and never discard them to make a task easier.
2. Identify the affected area from the map below and read only those files plus
   their tests.
3. For frontend work, read `frontend/AGENTS.md` before editing. This repository
   uses Next.js 16, whose bundled docs may differ from remembered APIs; consult
   the relevant guide under `frontend/node_modules/next/dist/docs/` when changing
   Next.js behavior or conventions.
4. Keep API types synchronized across `backend/internal/repository/`,
   `backend/internal/httpapi/`, `frontend/app/lib/types.ts`, and
   `frontend/app/lib/api.ts` when a contract changes.
5. Run the narrowest relevant tests during development, then the validation
   commands listed below before handing off.

## Repository map

### Backend

- `backend/cmd/server/main.go`: composition root, `.env` loading, CORS, server
  timeouts, dependency wiring, and graceful shutdown.
- `backend/internal/config/config.go`: all backend environment variables and
  defaults.
- `backend/internal/httpapi/router.go`: health/readiness, load sync, and route
  group registration.
- `backend/internal/httpapi/fleet_handlers.go`: driver, truck, and dispatcher
  JSON CRUD handlers and input validation.
- `backend/internal/fleetscope/`, `backend/internal/httpapi/fleetscope_handlers.go`,
  and `backend/internal/repository/fleetscope_repository.go`: signed new-hire
  intake and setup tasks. See `docs/FLEETSCOPE_WEBHOOK.md` for configuration.
- `backend/internal/httpapi/toll_handlers.go`: toll listing and manual PrePass
  API sync.
- `backend/internal/httpapi/expense_handlers.go`: expense listing, filtering,
  validation, and CRUD handlers.
- `backend/internal/httpapi/gross_board_handlers.go` and
  `backend/internal/repository/gross_board_repository.go`: weekly dispatch plans,
  exact load matching, suggestions, and versioned atomic autosaves.
- `backend/internal/httpapi/file_handlers.go`: IRP/cab-card and CDL uploads,
  extraction orchestration, and stored-file downloads.
- `backend/internal/repository/`: SQL and domain/API structs. `fleet_repository.go`
  also owns assignment transactions; `naming.go` owns canonical person and truck
  naming; `load_repository.go` maps DataTruck records to local loads.
- `backend/internal/datatruck/`: paginated DataTruck client with rate-limit retry.
- `backend/internal/relay/`: Relay Payments fuel transaction client.
- `backend/internal/prepass/`: authenticated PrePass account discovery and
  paginated toll transaction client.
- `backend/internal/jobs/sync_loads.go`: synchronous load sync for new upstream
  record IDs plus a rolling 21-day service-date reconciliation.
- `backend/internal/jobs/sync_fuel.go`: synchronous missing-day Relay fuel sync.
- `backend/internal/jobs/sync_tolls.go`: synchronous missing-day PrePass toll
  sync in API-compatible date windows.
- `backend/internal/groq/`: vision extraction client and normalization for truck
  cab cards and driver CDLs.
- `backend/internal/gemini/`: structured expense extraction from user-provided
  text, PDFs, text files, and receipt images.
- `backend/internal/db/pool.go`: pgx pool configuration.
- `backend/sql/init.sql`: complete schema for a new database.
- `backend/scripts/prepare-expense-import.ps1`: validates Google Sheets expense
  CSV exports and creates an idempotent, source-row-traceable SQL import.
- `backend/sql/002_add_tolls.sql` through
  `051_five_eld_heading.sql`:
  manual incremental migrations for older databases.

### Frontend

- `frontend/app/layout.tsx` and `components/AppShell.tsx`: session-aware global
  shell and sidebar; `frontend/app/login/` owns the login page.
- `frontend/app/page.tsx`: redirects `/` to `/gross-board`.
- `frontend/app/dashboard/`: redirects legacy Dashboard links to Gross Board.
  The old load-derived dashboard is removed pending a Gross Board-based replacement.
- `frontend/app/accounting/driver-pay/`: collapsed weekly driver accordions sourced
  from Gross Board, system load details, profile tariffs, comments and adjustments.
  Driver summaries form a connected compact table; comments use hover previews and
  modal editing. Weekly adjustment name/amount columns follow the driver fee, independent of
  load rows, and scroll after seven 32px entries. Names and signed amounts edit
  inline in blank table rows; clearing both removes an entry. Partially filled
  adjustments block autosave/navigation until completed or cleared.
  `backend/internal/repository/driver_pay_repository.go` and
  `backend/internal/httpapi/driver_pay_handlers.go` own its API and persistence.
- `frontend/app/accounting/driver-charges/`: reusable charge types, effective-dated
  an active-driver recurring fee matrix and installment plans, schedules and audit
  history. Driver profiles link to this centralized management view.
- `frontend/app/accounting/dispatcher-pay/`: weekly dispatcher commission reports.
- `frontend/app/loads/`: load table, filters, sorting, and manual sync.
- `frontend/app/gross-board/`: Monday–Sunday dispatch planning grid, dispatcher
  grouping/filtering, load suggestions, autosave, and exact-decimal totals.
- `frontend/app/driver-board/`: live active-driver dispatch grid, dispatcher/status
  filters, current New York week Gross Board totals and versioned autosaves.
  Keep fields on one line with compact rows. Prioritize columns through ETA;
  later fields may extend into horizontal scrolling. Show type codes without a legend.
  `driver_board_repository.go` and `driver_board_handlers.go` own its backend.
  Status fills follow the TODAY sheet where a matching rule exists. Future
  feature scope, exclusions and rollout order live in `docs/DRIVER_BOARD_PLAN.md`.
- `frontend/app/tolls/`: toll overview, transaction table, and manual PrePass sync UX.
- `frontend/app/expenses/`: paginated expense management, filters, linked fleet
  assignments, CRUD forms, and review-first AI transaction entry.
- `frontend/app/investors/`: truck owners, independent investors and driver-linked
  investors. The company is hidden by default with a Show company filter.
  The directory hides driver-linked owners with fewer than two owned trucks;
  unpaginated owner lookups retain them for forms and history.
  Truck forms select explicit owners; ownership is separate from operation.
- `frontend/app/drivers/`, `trucks/`, and `dispatchers/`: client-side CRUD pages;
  their colocated `*Form.tsx` files own form conversion/defaults. Driver and
  truck detail pages include expenses linked to that record.
- `frontend/app/components/management/ManagementUI.tsx`: shared management page,
  modal, form, table, empty/error, and confirmation primitives. Reuse these
  before creating parallel UI patterns.
- `frontend/app/components/`: shared dashboard, filtering, and navigation pieces.
- `frontend/app/components/TopBar.tsx`: global paginated record search, quick-create
  links and account menu. PageHeader portals render each page title, icon, count,
  and description on the left of the top bar, preserving page context/handlers.
  Page action buttons/status controls remain in the main content; global controls
  stay on the right.
  Explicit search query parameters override remembered
  list search/page values; new query tokens open existing forms once.
- `frontend/app/lib/api.ts`: API base selection and every frontend request.
- `frontend/app/lib/types.ts`: frontend view/input contracts.
- `frontend/app/lib/pdf.ts`: browser-side PDF-to-JPEG rendering used before GROQ
  extraction; the original PDF is still uploaded and stored.
- `frontend/app/globals.css`: Tailwind import, theme tokens, and global animation/
  scrollbar styles.

## Runtime and setup

### Backend environment

Run the API from `backend/` because `main.go` loads `.env.local` and `.env` from
the current working directory. `backend/.env.local` is ignored by Git.

Required:

```dotenv
DATABASE_URL=postgres://...
DATATRUCK_API_KEY=...
DATATRUCK_COMPANY_NAME=...
```

Optional:

```dotenv
PORT=8080
BIND_ADDRESS=127.0.0.1
GROQ_API_KEY=...
GROQ_MODEL=qwen/qwen3.6-27b
GEMINI_API_KEY=...
GEMINI_EXPENSE_MODEL=gemini-3.5-flash-lite
FIVE_ELD_API_KEY=...
FIVE_ELD_PROVIDER_TOKEN=...
FIVE_ELD_USDOT=...
FIVE_ELD_API_URL=https://read.fiveeld.com
FIVE_ELD_SYNC_INTERVAL=5m
RELAY_ENVIRONMENT=production
RELAY_STAGING_API_KEY=...
RELAY_PRODUCTION_API_KEY=...
RELAY_FUEL_SYNC_START_DATE=2026-01-01
PREPASS_ENVIRONMENT=production
PREPASS_PRODUCTION_CLIENT_ID=...
PREPASS_PRODUCTION_CLIENT_SECRET=...
PREPASS_NONPRODUCTION_CLIENT_ID=...
PREPASS_NONPRODUCTION_CLIENT_SECRET=...
PREPASS_TOLL_SYNC_START_DATE=2026-01-01
FRONTEND_ORIGIN=http://localhost:3000
AUTH_COOKIE_SECURE=false
AUTH_SESSION_TTL=12h
FLEETSCOPE_COMPANY_ID=...
FLEETSCOPE_WEBHOOK_SECRET=...
SCHEDULED_SYNCS_ENABLED=true
SCHEDULED_SYNCS_TIMEZONE=America/New_York
SCHEDULED_LOADS_SYNC_TIME=06:00
SCHEDULED_FUEL_SYNC_TIME=06:30
SCHEDULED_TOLLS_SYNC_TIME=07:00
```

`GROQ_API_KEY` is required for document extraction. CORS allows only
`FRONTEND_ORIGIN`. `AUTH_COOKIE_SECURE` defaults to true for an HTTPS
frontend origin and false for HTTP; production must use HTTPS and secure cookies.

For a new database, apply `backend/sql/init.sql`. For an existing database, apply
the numbered SQL files in order as needed. There is no automatic migration tool,
so schema changes must update `init.sql` and add a new incremental SQL file.

Accounts are created by administrators in Settings; there is no self-sign-up
or email delivery dependency. New users require a name, unique email, role and
password (12-72 bytes). Administrators can reset passwords in Edit user; users
can change their own password from the account menu. Migration 044 preserves
existing users' unrestricted access as the Administrator role, revokes old
sessions, and temporarily allows legacy username login until a real email is
attached in Settings. Email addresses are not verified.

For a completely new database, provision the initial account below and assign
its real email and the built-in Administrator role directly in the database.

To provision the initial user, generate a bcrypt hash without exposing the password in
shell history, then insert it directly:

```powershell
cd backend
go run ./cmd/hash-password
# Use the printed hash as <bcrypt-hash>:
# INSERT INTO app_users (username, email, password_hash, role_id)
# SELECT 'admin', 'actual-admin@example.com', '<bcrypt-hash>', id
# FROM app_roles WHERE system_role;
```

### Run locally

```powershell
# terminal 1
cd backend
go run ./cmd/server

# terminal 2
cd frontend
npm install
npm run dev
```

Frontend defaults to `http://localhost:8080`. Override with
`NEXT_PUBLIC_API_URL`; `NEXT_PUBLIC_LOADS_API_URL` remains a legacy fallback.
The UI runs at `http://localhost:3000`.

`npm run build` produces a static frontend in `frontend/out/`; set
`NEXT_PUBLIC_API_URL` at build time because the API URL is embedded in the
browser bundle.

## Current API surface

- Health: `GET /healthz`, `GET /readyz`
- Auth: `POST /auth/login` (email/password, optional trustDevice),
  `GET /auth/session`, `POST /auth/logout`, `POST /auth/password`.
- Access administration: `GET /settings/access`, `POST /settings/users`,
  `PUT /settings/users/{id}`, `POST /settings/users/{id}/revoke`,
  `POST /settings/roles`, `PUT /settings/roles/{id}`. All require `access.manage`.
- Loads: `GET /loads`, `POST /jobs/sync-loads`
- Gross board: `GET/PUT /gross-board` (`weekStart=YYYY-MM-DD` for reads),
  `GET /gross-board/loads?search=...`, and
  `GET /gross-board/balance?driverId=...&weekStart=...` for load-by-load history.
- Driver board: `GET /driver-board?weekStart=YYYY-MM-DD`, `PUT /driver-board`
  plus `GET /driver-board/history?driverIds=...&before=...` (50-event cursor pages)
  and `POST /driver-board/history/{id}/undo` (driverId, version, homeVersion).
  `GET/POST /driver-board/loads/{driverId}` reads/changes operational load selection,
  order and destination source; GET accepts an optional `from` date, POST requires
  board/home versions and the displayed plan revision.
  with changed entries only. Dedicated `driver_board.read`/`driver_board.write`
  permissions include board contact/home reads and home updates respectively.
  `POST /jobs/sync-eld` performs an authorized targeted Five ELD refresh and
  requires `driver_board.write`.
- Driver pay: `GET/PUT /driver-pay`, `POST /driver-pay/refresh-loads`,
  `POST /driver-pay/finalize` and `/reopen` (optional driverId, report revision;
  reopening requires a reason). `GET /drivers/{id}/pay-history` is paginated;
  `GET /drivers/{id}/settlement-history?weekStart=...` returns the audit snapshots.
  GET accepts Monday `weekStart`; PUT saves one driver's versioned weekly notes,
  per-entry comments and adjustments. Refresh accepts `weekStart` and updates
  source details of already-linked loads only, including older report weeks.
- Driver charges: `GET /driver-charges` (optional driverId),
  `POST /driver-charges/types`, `POST /driver-charges/schedules`,
  `POST /driver-charges/schedules/preview`, `POST /driver-charges/bulk`,
  `GET /driver-charges/schedules/{id}/preview` and `/history`,
  `POST /driver-charges/confirm` and `/reopen`.
- Drivers: `GET/POST /drivers`, `GET/PUT/DELETE /drivers/{id}`
- Driver assignment history: `GET /drivers/{id}/assignments` returns truck and
  dispatcher periods, current links, source notes, and whether the start is known.
- New hires: `POST /integrations/fleetscope/driver-hired`, `GET /driver-intake`,
  `GET /driver-intake/{id}`, `GET /driver-directory`, `POST /driver-intake/{id}/complete`.
- Investors: `GET/POST /investors`, `PUT /investors/{id}`. List supports search and
  pagination; paginated reads exclude the company unless includeCompany=true.
  Unpaginated owner lookups include the company. Inactive investors retain
  ownership/history. Company identity is protected.
- Trucks: `GET/POST /trucks`, `GET/PUT/DELETE /trucks/{id}`
  plus `GET /trucks/{id}/location` and `GET /drivers/{id}/location` (fleet.read).
  Location reads return the shared cached coordinates/heading and resolve the
  driver's current truck assignment, without requesting upstream telemetry.
- Dispatchers: `GET/POST /dispatchers`, `PUT/DELETE /dispatchers/{id}`.
- Updaters: `GET/POST /updaters`, `PUT/DELETE /updaters/{id}` (fleet permissions).
- Tolls: `GET /tolls`, `GET /toll-dashboard`, `POST /jobs/sync-tolls`
- Expenses: `GET/POST /expenses`, `PUT/DELETE /expenses/{id}`
- Expense settings: `GET/POST /expense-settings`, `PUT /expense-settings/{id}`
  (category, name, payment_method, payer; updates require the current version).
- AI expense entry: `POST /expenses/extract` (multipart text/file analysis) and
  `POST /expenses/bulk` (atomic reviewed batch creation)
- Fuel: `GET /fuel-transactions`, `GET /fuel-dashboard`, `POST /jobs/sync-fuel`
- Relay review: `GET /tasks/relay-identities` (pagination/search),
  `POST /tasks/relay-identities/{id}/review` (`driverId`, `action: link|reject`).
  `frontend/app/tasks/` owns the authenticated review queue.
- Custom tasks: `GET/POST /tasks/custom`, `PUT/PATCH/DELETE /tasks/custom/{id}`.
  GET accepts pagination, search, and status (`open`, `completed`, `all`);
  PUT edits title/notes and PATCH sets `completed` without replacing content.
- Financial reporting: `GET /financial-dashboard` (latest qualifying week, or
  `weekStart=YYYY-MM-DD`)
- Documents: `POST /irp-files`, `POST /cdl-files`, `GET /files/{id}`

Backend JSON is intentionally not uniform: loads retain exported Go field names
such as `LoadID`, while fleet/toll/file responses use lower camel case JSON tags.
Do not “normalize” one side without updating its consumers.

List endpoints accept `page` and `pageSize` (maximum 100) to return a paginated
object with `items`, `total`, `page`, `pageSize`, and `totalPages`. Frontend table
pages pass search/filter values to these endpoints. Requests without pagination
parameters retain the legacy raw-array response for dashboard calculations and
assignment lookup lists.

## Domain invariants and data flows

- Dispatchers and updaters share /dispatchers. Migration 049 adds optional integer
  phone extensions (0–999999), updater profiles with main/after_hours shifts, and
  one assignment per dispatcher/shift. An updater may serve multiple dispatchers.
  Composite foreign keys enforce shift matching; assigned updaters cannot change
  shift until unassigned. Updater edits are versioned. Dispatcher profile saves
  atomically include updater assignments; omitted new fields preserve them for
  older clients. Status Board headings show both updaters and known extensions,
  without requiring fleet.read. Main updaters align right before Original gross;
  after-hours updaters start after Status. Shift labels are omitted and gross
  column borders continue through group headings. No updater changes affect commissions or rosters.
  backend/scripts/seed-dispatcher-updaters.sql imports the October 4 TODAY headings
  explicitly; it is an operator data import, not a schema migration.


- Driver Board uses migrations 045–046. It lists active managed drivers with live
  truck, phone and dispatcher assignments. Operational text, trailer and status
  are persistent per driver, not weekly snapshots; Gross Board alone supplies
  the current New York Monday–Sunday gross and miles (same entered/source and
  status/deletion rules). Filters scope cards and dispatcher totals to shown
  drivers. Type badges use actual truck ownership plus driver pay method: O for
  percentage owner operators, M for CPM company/own-truck drivers, %-O/M-O for
  percentage/CPM hired drivers on other investors' trucks, % for company
  percentage drivers. No ownership or pay settings change from this view.
  `drivers.driver_home` is a separate optional home-base field (not the mailing
  address), shared by profiles and the board. Database-triggered home versions
  prevent stale profile/board overwrites; older clients omitting home preserve it.
  Five-second autosave writes changed rows atomically with optimistic versions;
  clearing retains versions, navigation flushes, conflicts retain local drafts,
  and visible idle boards refresh every 30 seconds/on focus. Drafts are never
  persisted in view memory. Tests use disposable local
  MSERP_DRIVER_BOARD_TEST_DATABASE_URL as mserp_app for fresh/migrated schemas;
  the investor E2E runner includes driver-board-e2e.mjs.
  Migration 046 records board and profile-home differences transactionally,
  grouped by driver/transaction, with authenticated actor and name snapshots.
  No-op saves add no history; existing data has no invented past events. Events
  survive driver/account deletion and cannot be rewritten after their transaction.
  History reads use driver_board.read; undo uses driver_board.write and creates
  an audited correction. It locks the driver, checks board/home versions and
  rejects any later change to affected fields, including changes back to the same
  value. Unrelated later fields remain intact. The history side panel opens per
  driver or for the shown view. All drivers, My view and named dispatcher-group
  views use per-user, per-tab viewMemory; these filters are not access restrictions.
  Gross cards and group totals follow all visible filters. Migration 048 adds
  stable Gross Board plan UUIDs and separate driver_board_load_state JSON storage
  for current selection, stop/source choice, order and removed-from-queue plans.
  Reusing a slot renews its UUID; financial edits preserve it. Reads are pure and
  never activate/advance loads. Current selection is explicit and retained with
  a warning after source removal/reassignment. Unique linked loads deduplicate;
  identical business numbers with distinct records remain separate. Unmatched
  plans stay visible. Next loads start today in New York and include future plans. Older unfinished
  plans are shown separately and may still be explicitly selected as current.
  Older current selections stay visible; custom ordering cannot make past plans
  upcoming. The server clamps stale cutoff dates and rejects stale midnight actions.
  Gross Board controls planned assignment;
  DataTruck driver mismatches warn instead of dropping or moving plans. Finished
  next loads remain inspectable; finished current loads are not auto-cleared.
  Stops come from imported raw_payload. Only first pickup/final delivery inherit
  load-level appointments; ETA remains manually set in a compact cell-anchored
  popup with a date picker and optional time, shown on one line. New ETAs persist as YYYY-MM-DD or
  YYYY-MM-DDTHH:mm in New York wall time in the existing string field; legacy text
  is displayed unchanged until explicitly replaced or cleared. Idle board refresh
  pauses while the ETA editor is open. Dispatcher is shown by group/filter rather
  than a repeated table column. Personal view controls follow the status filter
  on the same row. Source destinations have an explicit
  manual override. Imported locations display City, ST without ZIP codes; formatting
  does not alter source values, stop keys or manual destination text. Clicking a
  source destination switches to the first located stop of the opposite type,
  with a muted PU/DEL indicator. This uses versioned load-source actions/history
  without changing status or ETA; specific multi-stop choices remain in Loads. Text edits detach current identity or destination source safely.
  Queue mutations lock the driver, check board/home versions plus source revision,
  share transactional history and support undo. No financial dates/rates or upstream
  records change. History loadPlan values contain operational snapshots, formatted
  for people in the UI. Next loads follows ETA to preserve priority column widths.
  A phone click copies its +1 number after a 500ms double-click window with
  brief feedback; double-click starts a RingCentral call via
  rcapp://r/call, without copying the number.
  Blank numbers have no action.
  Current-load typing suggests this driver’s current-week Gross Board load numbers,
  excluding statuses/deleted entries and deduplicating numbers. Selection fills
  the current load and pickup immediately, sets DISPATCHED, and
  links it through atomic autosave. Exact typed numbers resolve the same way,
  using every day of the current New York week, including delivered/hidden plans.
  Distinct duplicate load matches remain unlinked. Changing the text clears the
  old displayed destination; clearing Current load clears manual and sourced
  destinations. Loads.week supplies matching candidates independently of Next;
  write-only resolveCurrentLoad intent supports reselecting an unchanged number.
  Status-cell labels advance pickup/delivery; only the chevron opens the neutral
  dropdown. Pickup shows delivery and sets RESERVED when Next is nonempty,
  otherwise ENROUTE; these labels also follow queue changes during reads.
  Delivery records completed identities in operational load state, promotes the
  next load to DISPATCHED/pickup, and clears the stale ETA. Without Next, current
  load/location/status/ETA become blank, never NO LOAD. Other statuses remain
  manual. Version/revision checks and double-click handling prevent double advance.
  Writes return their own undoId. Ctrl+Z/Cmd+Z keeps a page-session stack of
  these IDs and pending edits, with server-enforced actor ownership for personal
  undo. It can traverse this user's reversed event pairs but never overwrite
  another actor's later overlapping edits. Native draft text undo stays native.
  No extra board buttons, tooltips or workflow explanations are added.
  Five ELD uses migration 050 and server-only API key/provider-token/USDOT
  configuration. A bounded interval poll matches current positions to assigned
  trucks by normalized VIN and caches the coordinates in PostgreSQL. Opening the
  board never calls Five ELD. The Latest location column is immediately before
  Destination and shows the latest known coordinates regardless of age,
  copies them on click and exposes the exact provider timestamp only on hover;
  missing values stay blank. Loads contains no truck telemetry. Fleet-readable
  truck numbers link to Truck details. Truck details mirrors the Driver profile:
  summary, editor, Overview and Expenses tabs, vehicle/documents/maintenance/notes.
  Truck and Driver Overview pages have a compact side map using Leaflet/OSM;
  driver locations resolve the current assigned truck on each cache read.
  Visible profiles refresh locations every 30 seconds and on focus, never calling
  Five ELD directly. Migration 051 caches optional heading with each coordinate;
  the map arrow rotates to the reported heading, or uses a neutral dot if absent.
  OSM tile images explicitly use strict-origin referrers, overriding the global
  no-referrer policy only for tiles. This identifies the site as OSM requires
  without disclosing profile paths/IDs. Browser tests mirror production headers.
  Locations recorded three hours ago or more appear muted gray in all views.
  Last known points remain visible during outages. Polling defaults to five minutes;
  the retired FIVE_ELD_STALE_AFTER setting is ignored. The legacy database
  stale_after_seconds column is retained only for rollback compatibility.
  Unmatched/ambiguous units require review and never change assignments, loads,
  progress or financial records. Handoff remains a future phase in
  docs/DRIVER_BOARD_PLAN.md.

- Access control uses migration 044. One role per user, with a code-owned
  permission catalog in repository/access_permissions.go. The built-in
  Administrator role is immutable and has the complete catalog. The API checks
  current role permissions on every request and denies unmapped resources;
  hiding navigation is supplementary. Settings creates/edits roles, provisions
  and disables users, resets passwords, and revokes all sessions. There is no
  email verification, SMTP requirement, invitation token or public registration.
  New/edited accounts require a valid unique email. Existing accounts retain
  their full access as Administrators and may use their old username until an
  email is attached; subsequently login requires that email. Passwords are bcrypt
  hashes. Trust this device for 30 days extends only that session to a fixed
  30-day lifetime; normal sessions use AUTH_SESSION_TTL. Password/account changes
  and explicit revocation invalidate every session, including remembered ones.
  Session issuance locks/rechecks the user version to prevent stale login races.
  Current role permissions are loaded for every request. Account and role writes
  are versioned and audited without password material; serialized updates retain
  at least one active Administrator. Fleet read includes full profiles/documents;
  grant it to roles using fleet selectors. Payroll history/finalization require
  separate permissions. Login throttling trusts Nginx X-Real-IP only on loopback.
  Tests use disposable local MSERP_ACCESS_TEST_DATABASE_URL (_test database),
  fresh/migrated schemas as mserp_app; access-e2e.mjs runs with investor E2E.
  Pre-044 binaries do not enforce permissions: rollback to them is unsafe once
  access restrictions are in use.

- Contact phones are optional ten ASCII digits without a country code; blank
  values store as NULL. Drivers, dispatchers, investors, FleetScope intake and
  Relay review use this contract. Forms, APIs and database triggers normalize
  common punctuation and an optional leading US 1, and reject invalid manual
  input. UI contact displays use +1 (XXX) XXX-XXXX; formatted phone searches
  resolve against canonical digits. Shared rules live in backend/internal/phone
  and frontend/app/lib/phone.ts, with PhoneInput for all contact forms. Migration
  042 archives changed originals in phone_normalization_audit, keeps the first
  number in valid slash-separated legacy lists, and clears irrecoverable values.
  FleetScope retains its first source JSON snapshot and exposes a separate
  canonical phone column; Relay retains raw upstream payloads but never uses
  malformed contacts as matching evidence. Database checks use only disposable
  MSERP_PHONE_TEST_DATABASE_URL (_test database), as mserp_app.

- Investor Pay at /accounting/investor-pay shares driver-pay/WeeklyPayPage.tsx
  and DriverCard.tsx with Driver Pay. Reports group trucks by investor, retain
  full load details, and deduct hired driver earnings in the charges column.
  Earnings use percentage/CPM tariffs before personal deductions; finalized
  driver load fees take precedence over later tariff changes. Truck shares use
  explicit dated percentages of Gross Board driver gross. No default percentage,
  separate dispatch-fee deduction, or blanket start cutoff is inferred.
  Migration 041 adds truck terms, recurring phases, weekly investor snapshots,
  audit records, and expense payment destinations. Configure each truck's owner,
  share, and effective Monday in Charges > Truck charges. Historical owner terms
  are explicit accounting agreements, not inferred ownership history. Known
  conflicting ownership periods require review. Driver charges/installments and
  shared charge types remain on the same Charges page. Truck fees follow trucks
  and type eligibility. Explicitly moving a driver fee atomically pauses its
  driver assignment; personal fees never move automatically. Saved overrides
  and finalized weeks block conflicting moves. Later truck phases remain intact.
  Migration 043 automatically hands recurring deductions to investor trucks on
  assignment and recurring-charge writes, and backfills current assignments.
  The effective Monday follows the stored assignment and any later known owner
  start. Driver fees stay paused after departure until explicitly resumed.
  Existing truck fee selections win; gaps inherit driver phases without stacking.
  Unconfirmed driver occurrences from the handoff week are removed with full
  audit details; earlier weeks, reimbursements and personal installments remain.
  Finalized/confirmed collections block handoff until reopened. Rates are never
  inferred, and transferred fees still need explicit truck settlement terms.
  Truck charge management and writes exclude single-truck driver owners and
  trucks currently operated by their owner; their fees use Driver charges.
  GET /truck-charges supplies backend-filtered eligibleTruckIds. Historical
  accounting reads retain saved terms independently of management eligibility.
  Owner-only weeks stay in Driver Pay using the owner's tariff without a second
  driver-wage deduction. Mixed owner/hired-driver weeks use one investor truck
  statement and charge hired labor only. Once an investor truck-week is saved,
  its destination stays Investor Pay during corrections to preserve payments.
  Unassigned trucks retain calendar fees. No-load owner routing requires a
  unique full-week historical assignment. Load source units or unique dated
  assignments for unmatched plans determine truck attribution. Missing matches,
  duplicate system loads, ownership conflicts and unlinked fuel need review.
  Fuel uses confirmed Relay identities, unique source Truck # prompts, diesel
  items and merchant-local dates; tolls use stored truck IDs/crossing dates and
  include credits. Routed costs leave driver sources transaction by transaction;
  legacy manual cost overrides require review/reset. Owner-covered truck expenses
  route to the owner settlement, sharing expense principal/version protection.
  Existing payments retain their destination. Reads never collect expenses;
  autosave/finalize saves displayed deductions. Reopening preserves payments.
  Finalization freezes reports, checks revisions and shares the payroll lock;
  reopening requires a reason. Negative net has no automatic debt carry beyond
  existing expense balances. Financial dashboard calculations remain separate.
  API: GET /truck-charges; PUT /truck-charges/terms and /recurring;
  GET/PUT /investor-pay; POST /investor-pay/finalize and /reopen. Shared payroll
  edits use driverId as the truck ID on Investor Pay endpoints. Tests use only
  disposable MSERP_INVESTOR_TEST_DATABASE_URL (_test DB), fresh/migrated schemas
  as mserp_app, and test-investors-e2e.mjs (including investor-pay-e2e.mjs).
  Earlier clients cannot display truck settlements after rollback; new expense
  payment destinations remain protected.

- Navigation preserves view state throughout the app. PageNavigation supplies a
  shared Back link and restores main/nested scroll positions after data loads;
  viewMemory.useViewState retains filters, sorting, weeks, pagination, tabs and
  expanded rows in session storage, scoped by authenticated user, page and detail
  record. RememberedDetails handles native expandable sections. Ordinary visits
  append to the navigation trail; explicit Back and browser Back unwind it.
  State survives reloads within that browser tab; records are fetched afresh.
  Never persist fetched records, credentials, form drafts or pending financial
  edits through this hook. New page view controls should use useViewState.
  Explicit load deep links override the remembered Gross Board week/filter/scroll
  on the first render, avoiding an initial request for the remembered week.
  Internal navigation after autosave uses the client router, preserving the app
  shell. After initial authentication, route session rechecks run alongside page
  requests; every API request still enforces authentication and handles expiry.
  Entering login unmounts the authenticated shell and clears its session state.
  IntentLink prefetches route code on hover/focus without caching financial records.
  Weekly payroll/board tables and metrics show reduced-motion-aware skeletons
  during reads. Gross Board indexes entries once by driver/day per revision.
  Production Nginx compresses JavaScript, CSS, route payloads and JSON responses.

- Driver charges use migration 033: charge types, schedules, effective Monday
  phases, weekly occurrences and append-only audit events. They are independent
  of freeform driver_pay_weeks.adjustments JSON, Expenses, investors, dispatcher
  commission and estimated profit. Weekly generated rows affect Driver Pay net
  payable, including charge-only weeks. Recurring labels are read-only in Driver
  Pay (also enforced by the API); amounts, skips and resets remain editable.
  Charge creation lives on Driver charges; Driver Pay retains legacy installment
  reopening and source links only. Weekly amount edits autosave normally; payroll
  finalization confirms collection. Type defaults never update assignments.
  Bulk changes are current/future only and replace subsequent planned phases;
  saved overrides/confirmations must be explicitly corrected first.
  Recurring assignments use an active-driver matrix with charge types as columns.
  Driver and truck matrices use the payroll-style previous/next week range and
  This week controls. Valid effective weeks remain remembered. Driver charge
  type filters use the visible catalog; remembered missing or hidden archived
  types fall back to all visible types so initial entry cannot hide every fee.
  Driver charges lists and driver pickers show active drivers only; stored
  schedules and historical payroll for inactive drivers remain retained.
  Each type defines 1–50 exact amount options and calendar/loads/no_loads
  eligibility (migration 034); drivers select an amount with a dropdown. Type
  eligibility changes apply from the current New York Monday through dated
  rules; prior weeks retain their rules and weekly overrides stay explicit.
  Existing amount selections survive option edits. PUT /driver-charges/recurring
  version-checks a cell, pauses unchecked assignments, and preserves later phases.
  Matrix saves lock only the affected driver row and refresh that driver’s
  schedules; concurrent responses merge by driver identity. Page-wide actions
  await pending row saves. Save/error notices use a viewport-fixed portal so
  feedback never moves the table. The matrix has compact 32px rows and accepts any valid Monday, including past
  weeks. Backdated assignments fill gaps up to the next assignment; dated type
  rules apply, with the earliest type rule used before the type existed. Migration
  035 permits no-load eligibility in these recurring historical periods. Changes
  affect only the selected phase interval and reject conflicting weekly overrides.
  Installments retain their own calendar/load eligibility.
  Nonempty unmatched plans qualify; day statuses do not. Read-only projections
  account for all elapsed eligible weeks regardless of browsing order. Writes
  snapshot prior projected occurrences; explicit weekly edits, zero skips and
  confirmed rows survive source changes. Installment overrides reserve principal
  before future allocation; underpayments extend schedules. Confirming selected
  deductions is explicit and idempotent, records the session user, and locks
  those rows. Reopening requires a reason and reverses only collection status.
  Viewing/autosave never confirms generated installments; finalizing payroll does.
  Driver rows serialize charge writes;
  schedule, type and occurrence versions reject stale edits. Money is integer cents
  in calculations and numeric/decimal strings at persistence/API boundaries.
  Driver deactivation requires chargePauseWeek when charges exist and pauses
  them atomically; reactivation does not resume them. Charge history prevents
  permanent driver deletion. Existing releases cannot display these new charges
  after rollback but cannot erase them through the legacy adjustment contract.
  Tests use disposable MSERP_DRIVER_CHARGES_TEST_DATABASE_URL (_test database),
  fresh and migrated schemas as mserp_app, and test-driver-charges-e2e.mjs.

- Investors own trucks independently of operating-driver assignments. A driver may
  have one investor profile, sharing live name/contact details; independent owners
  have their own details. MS Express Inc. is the fixed company owner. Migration 032
  initializes current owner-operator-assigned trucks to driver-linked investors and
  all remaining trucks to the company. This is a one-time backfill; reassignment or
  changing driver pay classification never transfers ownership. New trucks default
  to the company unless an explicit active owner is selected. Omitted ownerId on
  updates preserves ownership for older clients. The legacy is_company_owned flag
  follows owner_id. Owners can be deactivated, not deleted; linked identity cannot
  be replaced. Driver deletion retains the investor and last known contact details.
  truck_ownership_history records changes and retains truck-unit snapshots after
  deletion; migration starts are marked unknown, not invented effective dates.
  Future charges/statements must use ownership periods and handle unknown starts
  explicitly. Truck settlements and charges are described above.
  investor_repository.go and investor_handlers.go own the API. Tests use only
  disposable MSERP_INVESTOR_TEST_DATABASE_URL (_test database), checking fresh and
  migrated schemas as mserp_app. CI runs these and real-API Chromium E2E flows.

- Custom tasks are shared by all authenticated users, separate from generated
  driver setup and Relay review tasks. Titles are required (200 characters max),
  notes are optional (5,000 max), and completion is reversible. Creation records
  the session user; editing content preserves completion and vice versa.
  `custom_task_repository.go` and `custom_task_handlers.go` own persistence and
  validation. Database tests use only `MSERP_CUSTOM_TASK_TEST_DATABASE_URL`, a
  disposable administrator connection with an `mserp_app` role, and verify both
  fresh schema and migration 026 as that application role.

- FleetScope is a one-way new-hire handoff for the configured MS Express company
  UUID. Both `FLEETSCOPE_COMPANY_ID` and `FLEETSCOPE_WEBHOOK_SECRET` must be set
  together; both absent disables receipt. Verify HMAC-SHA256 of timestamp + dot
  + exact body bytes and a five-minute delivery timestamp window. Pin the company
  independently of the signature. Receipts commit before acknowledgment and
  deduplicate by event ID plus company/driver ID; never overwrite the first
  snapshot, import the historical fleet, or propagate later profile changes.
  New hires appear as regular Drivers table rows with a small New badge and
  a corresponding Set up [name] task. Both views refresh every 15 seconds while
  visible. The driver-directory endpoint combines pending and configured drivers
  with shared search/pagination; ordinary /drivers lookups exclude pending hires.
  They stay outside managed drivers/payroll until accounting completes
  setup with a positive pay rate or explicitly links an existing record. Creation,
  assignments, and intake completion are atomic. Linking preserves existing
  profile, rates, assignments, and active status. Keep completed intake identity
  after driver deletion so retries cannot recreate it. Tests use the isolated
  `MSERP_FLEETSCOPE_TEST_DATABASE_URL`; never load production credentials for them.

- Gross board plans persist up to 100 load slots per driver and calendar day, separately
  from imported loads and settlement accounting. Active drivers and inactive
  drivers with saved entries through the selected week appear. Gross Board and
  unfinalized Driver Pay resolve dispatcher/truck headings from assignment history
  for the selected New York week, never today's links. Weeks start Monday.
  Day statuses are stored separately from load numbers (`day_status`, migration
  028). Status days have no linked load, rates, or miles and never contribute to
  totals, balance history, or incomplete-rate counts. The picker supports all 13
  dispatcher statuses (including LOAD CANCELLED, migration 039), exact names/aliases
  when no amounts are present, and
  explicit suggestions for partial text. Replacing populated days requires a
  clear-values confirmation. Statuses never auto-link to imported loads, even
  when a load number happens to equal the status label. Existing free text is
  not reclassified by migration. Clearing a status retains the entry version.
  Free text remains a plan; a unique exact load number (case-insensitive,
  trimmed) or explicit suggestion selection confirms it. Duplicate business
  load numbers require explicit selection. Entered original gross and miles
  take precedence in the grid, totals and balance; absent entered values fall
  back to the linked load's current `total_pay` and `total_miles`. Source values
  remain available as `systemOriginalRate` and `systemMiles` for comparison.
  Original-pay entry also fills driver gross while it is blank or still equals
  the previous original pay; a distinct driver gross is preserved. Rate balance carries original minus driver rate
  forward from the first saved board entry through the selected week's end,
  including plans. Missing load numbers or rates are excluded and counted as
  incomplete, never treated as zero. Repeated system loads for the same driver
  contribute only on their earliest board date and are identified in history.
  Future weeks never affect an earlier balance. Balances follow driver IDs across
  dispatcher changes; source corrections and historical edits recalculate carry.
  The grid and opening balances are read in one repeatable-read snapshot.
  Migration 027 preserves entered original rates and miles separately from
  current source values. Differences show red and retain both values for
  review; accepting system values resets the comparison only if the source still
  matches the values reviewed. Driver-rate differences are intentional.
  Exact unique load numbers resolve on every board read, including late imports;
  unmatched or mistyped numbers remain unchanged. There is no fuzzy auto-linking.
  Unmatched manual load numbers show red on Gross Board, including grouped-day
  summaries, hover details and daily editors; blank cells and statuses keep their
  own styling. Confirmed system loads remain green.
  The visible idle board refreshes every 30 seconds and on focus, discarding
  responses if editing or saving occurred during the request. Cards
  and row totals include planned and confirmed entries in the selected view,
  accumulated as integer hundredths. Slot zero remains in `gross_board_entries`
  for rollback compatibility; additional slots use `gross_board_extra_entries`.
  Each slot has its own optimistic version; removed extra slots keep tombstones.
  Autosave submits only changed slots in an
  atomic version-checked batch; stale saves return 409. Cleared days retain
  their version to prevent lost updates. The grid keeps one row per driver: multi-load days show slash-separated
  references and summed amounts, with a hover breakdown and staged modal editor.
  Autosave waits for a five-second pause
  in editing; week navigation flushes pending changes immediately. Memoized day
  cells and a stable edit callback keep typing from re-rendering the whole grid.

- Driver Pay follows Gross Board placement, including unmatched plans, all load
  statuses, and inactive drivers with entries. Driver sections start collapsed.
  Percentage fees use Gross Board driver gross times the current driver profile
  percentage; CPM fees use system total miles times the profile tariff. Exact
  decimals round each fee to cents, and totals sum visible rounded fees. Missing
  source details stay blank; unmatched/incomplete rows and provisional totals
  are marked for review. Percentage-pay drivers have permanent Fuel and Toll rows
  with locked names and signed editable amounts; CPM drivers never show or apply
  these rows. Percentage owner-operators default to negative weekly costs, while
  company drivers default to zero. Fuel uses production Relay diesel line items
  (excluding DEF/products), confirmed driver links and merchant-local dates. Tolls
  include production/legacy credits by crossing date and unique historical truck
  assignment using New York calendar dates, never today's assignment or guessed
  nearby loads. Unmatched/ambiguous tolls remain outside payroll. Source totals
  remain visible; null overrides follow current source totals, explicit zero or
  signed overrides persist until reset. Individual drivers or an entire week can
  be finalized and reopened (migration 038). Finalization requires the current
  report revision, rejects future weeks and unresolved loads, saves suggested
  expense deductions, pins generated charges and confirms installment deductions.
  payroll_settlements freezes the full report with actor/time; source or profile
  changes do not change finalized history. Database guards reject payroll/payment
  edits until reopened. A reason is required to reopen; audit events retain every
  snapshot. Reopening reverses only installment confirmations made by finalization;
  saved expense payments remain paid until corrected, matching payment-on-save.
  Whole-week actions are atomic and ignore UI filters. Charge writes share an
  advisory lock; finalization obtains its exclusive session counterpart before
  opening a repeatable-read snapshot. Driver profiles expose editable details,
  weekly pay history, personal balances, other expenses and assignment history.
  Payroll load numbers deep-link to Gross Board by driver ID, service date and
  slot, scrolling to and highlighting that entry directly in the Gross Board.
  Daily loads opens only when the user chooses to edit a multi-load day. The load number
  guards against replaced/deleted slots, which display an explicit missing-entry
  notice. Pending payroll edits save before normal link navigation. The daily
  editor applies valid edits and awaits Gross Board autosave before Back to
  Driver Pay returns to the accountant's prior view.
  Notes, load comments, and named additions/reimbursements/deductions belong to
  a driver/week in `driver_pay_weeks`, with version checks and five-second autosave.
  Comments are keyed by date, slot, and normalized load number so replacing a
  load does not reuse its old comment. DataTruck stop ordering selects the first
  pickup and final delivery; trip `mile`/`empty_mile` supply loaded/deadhead miles.
  These source fields survive sync serialization. Historical payloads may need
  Refresh load details; that action never imports new loads or changes assignments.
  Database tests use only disposable `MSERP_DRIVER_PAY_TEST_DATABASE_URL` and
  verify fresh schema and migrations 029-030 as `mserp_app`.

- Toll overview aggregates production PrePass and historical imported records by
  stored posting date (inclusive range, year-to-date default, maximum five years).
  Credits reduce net spend, weeks begin Monday, and truck breakdowns use source
  equipment units, including unmatched units. SQL numeric aggregation preserves
  cent precision. All overview cards and charts share the same date range.
  The state spending map uses PrePass `tollAgencyState` first; only records
  without source state fall back to verified single-state agency locations.
  Unrecognized states and unresolved agencies stay in an explicit unmapped bucket.
  Never infer toll location from truck registration, fleet assignments, or the
  billing network. Agency mapping sources live beside the dashboard query.
  Toll sync preserves agency state/name and entry/exit plaza names separately
  from plaza codes. `-backfill-toll-locations` refreshes existing current-year
  toll location metadata in 31-day API windows without changing amounts,
  assignments, or completed sync days; it exits without HTTP or scheduled jobs.

- Paginated fuel and toll transactions derive optional load-coverage flags on
  each read; the existing tables show an icon in their final column. A load
  covers one calendar day before pickup through one day after delivery, using
  actual dates with appointment fallback. Fuel uses merchant-local purchase
  dates and reported Truck # prompts; tolls use stored crossing dates and source
  equipment units. Never substitute current fleet assignments. Credits and test
  data are excluded. Ambiguous units, incomplete history/dates, conflicting
  driver loads, and load data older than 48 hours show data-quality hints instead
  of review flags. Flags indicate a need for review, not confirmed fraud.

- DataTruck, Relay fuel, and PrePass toll syncs are initiated by the frontend
  and remain synchronous when manually triggered. The API process also
  schedules loads at 6:00 AM, fuel at 6:30 AM, and tolls at 7:00 AM
  America/New_York by default. DataTruck load sync
  fetches every upstream ID newer than the local maximum, plus a rolling 21-day
  reconciliation over pickup/delivery actual and appointment dates, then
  upserts by the upstream integer load record ID. Numeric ID filters must use
  DataTruck's inclusive `greater_than`/`less_than` operators (`after` is a date
  operator and is ignored for IDs). New-ID requests start at maximum ID + 1;
  reconciliation is capped at the pre-sync maximum to avoid fetching new loads
  repeatedly. The upstream page-size limit is 25. Loads without a business
  `load_id` retain their upstream identity and display as `DataTruck #<id>` until
  a subsequent sync supplies the number; they must not block the whole import.
  DataTruck does not expose an
  order last-modified timestamp. The server write timeout is fifteen minutes to
  permit pagination, rate-limit retry, and an initial Relay historical backfill.
- DataTruck imports never create managed `drivers` or `trucks`. They retain the
  upstream driver name and truck unit on the load, link only to an existing
  unambiguous fleet record, and create a current assignment only when both
  links resolve. Add fleet master records through the management UI first.
- Person names are title-cased for display and normalized for matching. Truck
  unit numbers are trimmed/collapsed and uppercased. Use the helpers in
  `backend/internal/repository/naming.go` rather than duplicating this logic.
- Assignment changes in driver, truck, and dispatcher forms require a start Monday
  in the current or a past New York week. The optional assignmentWeek API field
  defaults to the current week for older clients. Migration 047 makes dispatcher
  history honor this boundary; truck writes use the same boundary. Same-week
  corrections retain zero-length periods without overlaps. Changes cannot cross
  a later recorded assignment week; unchanged links never reopen periods.
  Tests cover fresh/migrated schemas and assignment-week-e2e.mjs. Migration 046
  is reserved for the separate Driver Board history work.

- Status Board is the visible name of /driver-board; route/permission identifiers
  stay compatible. Sidebar order begins Status Board, Gross Board, Loads.

- Driver/truck assignment history lives in `truck_driver_assignments`. Partial
  unique indexes enforce at most one current truck per driver and one current
  driver per truck. Assignment changes must remain transactional.
  Re-saving the same truck does not close/reopen its assignment. Driver detail
  pages show both truck and dispatcher history. Migration 031 adds dispatcher
  periods in `driver_dispatcher_assignments`; a database trigger captures driver
  creation, reassignment, and unlinking from every write path, including dispatcher
  deletion and direct imports. Existing dispatcher links have unknown start dates;
  their migration timestamp is only the observation time. Historical sheet weeks
  are source notes, never invented effective dates. Imports may set the local
  `mserp.assignment_source` PostgreSQL setting for dispatcher provenance and the
  truck assignment's `source` column. Dispatcher names survive dispatcher deletion.
  Tests use only `MSERP_ASSIGNMENT_HISTORY_TEST_DATABASE_URL`, whose database name
  must contain `_test`, and verify fresh schema and migration as `mserp_app`.
  CI runs these checks against a disposable PostgreSQL service.
- Expense responsibility is distinct from the operating driver. Migration 037
  stores owner_id and charge_driver_id: Driver bills the linked driver; Truck
  Owner bills the explicitly selected investor. Configured truck terms route
  expenses to Investor Pay or owner-only Driver Pay; unconfigured driver-linked
  owners retain their existing Driver Pay behavior. Independent investors never bill the operating
  driver. Current driver/truck selections fill their counterpart and default to
  Company for company drivers or the actual truck owner for owner-operators.
  Historical dates require explicit owner selection rather than guessing from
  current ownership. Ownership changes never transfer an existing expense debt.
  Historical Truck Owner expenses are settled without inferring a past owner.
  Personal charges use charge_driver_id; other linked expenses remain separate.
- Driver-covered expenses (coveredBy = Driver) require a linked driver and a
  nonnegative total. Penalties is an expense category. Migration 036 marks all
  pre-existing driver-covered expenses as settled with zero remaining balance.
  New expenses suggest their full unpaid balance in Driver Pay from the expense
  date's Monday, including weeks without loads. Normal payroll autosaves include
  all displayed unsaved expense deductions; editing a deduction saves its new
  amount. Reading payroll alone does not collect payments. Zero explicitly defers a week.
  Subsequent weeks show the unpaid remainder; saved historical rows retain their
  amounts, and fully paid expenses do not generate new rows. Saved deductions in
  other weeks reserve the principal, including future weeks, preventing duplicate
  collection. expense_payments stores exact numeric weekly amounts, actor and
  update time; expense balance versions reject stale cross-week edits. Payment
  writes and payroll autosave commit atomically. Expenses show total, paid and
  remaining; company expenses have no driver balance. Expense identity, date,
  responsibility and total cannot change once payroll rows exist; deletion is
  restricted. Legacy settled totals/responsibility cannot be repurposed.
  Use a new expense for an additional charge. Tests run as mserp_app against the
  disposable MSERP_DRIVER_PAY_TEST_DATABASE_URL and the driver charges E2E flow.

- Expense settings at /expenses/settings (linked from Expenses) manage categories,
  category-specific default names, payment methods and common payer suggestions.
  Migration 040 adds expense_settings with optimistic versions and archive/restore;
  there is no delete API. Active categories replace the former fixed enum in manual
  entry, filters and AI review. Default names belong to a category; custom expense
  names and one-off payment/payer text never automatically become defaults. Changing
  category clears a matching old default name but preserves custom text. AI receives
  the active catalog and leaves unrecognized categories blank for review.
  Seeded names are curated; existing payment methods and payer names seed their lists.
  Existing expense text remains a historical snapshot across catalog renames/archives.
  A database guard requires an active category for new/changed classifications while
  allowing unchanged historical categories on edits; archived parents hide all their
  default names. Category/name uniqueness is case-insensitive within its scope.
  Responsibility (Company/Driver/Truck Owner) stays separate from editable settings.
  Database tests use MSERP_DRIVER_PAY_TEST_DATABASE_URL for fresh and migrated schemas
  as mserp_app; test-driver-charges-e2e.mjs includes expense-settings-e2e.mjs.

- Expense type is labeled Name in manual and AI review forms, required after Category.
  The API retains expenseType for compatibility and requires a nonblank name on writes.
  Driver Pay shows this name and a negative deduction amount; unnamed legacy records
  fall back to category. A small muted Left balance beside the name previews the
  opening weekly balance minus the edited deduction, before later-week payments.
  Expense payment amounts remain nonnegative in the API and are subtracted once
  from net pay. There are no separate save-expense or confirm-installment buttons.

- Expenses optionally link to existing drivers and trucks with `ON DELETE SET
  NULL` while retaining imported unit/name snapshots for historical display.
  The Google Sheets import is idempotent by spreadsheet, sheet, and source row;
  exact normalized unit/name matches populate the foreign keys and unmatched
  values remain visible for later manual linking.
- AI expense entry accepts transaction text or one PDF, TXT, PNG, JPEG, or WEBP
  attachment up to 20 MB. Gemini suggestions never write directly: one detected
  transaction fills the add-expense form, while multiple transactions are
  returned for editable table review and atomic batch creation. High-confidence
  fleet matches populate foreign keys while raw extracted names and units remain
  available when unmatched. Gemini capacity responses use a compatible Flash
  fallback.
- Files are stored as `BYTEA` in PostgreSQL with metadata and SHA-256. IRP/CDL
  uploads accept PDF, PNG, JPEG, or WEBP originals up to 10 MB. PDFs require up
  to three browser-rendered page images for extraction. Never replace the stored
  original with rendered pages.
- GROQ-extracted fields are suggestions: the frontend fills the form and the user
  reviews before saving the truck or driver.
- PrePass toll sync discovers active accounts from the authenticated Account
  API, fetches posting-date ranges of at most 31 days with pagination, and
  never fetches earlier than January 1 of the current UTC year. It upserts by
  the stable PrePass toll ID. Completed UTC posting dates are
  recorded, while the current UTC date is rechecked. Vehicle numbers are matched
  to normalized truck units; unmatched tolls remain stored and are reconciled
  after the truck is added. PrePass timestamps may omit a timezone; toll dates
  and times use the calendar and clock fields encoded by PrePass so they remain
  compatible with historical CSV values. Historical CSV report records remain
  read-only.
- Database money/rates use PostgreSQL numeric values. Toll parsing uses integer
  cents before persistence. Avoid binary floating-point for new financial logic.
- Fuel report dates use each transaction's Relay merchant timezone rather than
  the browser timezone or a single UTC offset so ERP totals reconcile with Relay.
  Legacy `US/*` timezone aliases are normalized to canonical IANA names, and
  reporting falls back to `America/New_York` for an unrecognized source value.
- DataTruck load timestamps frequently encode schedule dates at `00:01 UTC`.
  Load reporting must use the encoded UTC calendar date; converting those values
  to America/New_York shifts them to the prior day. This rule is load-specific:
  fuel remains merchant-local and toll dates remain their stored date values.
- Fuel dashboard spend, gallons, prices, and discounts use diesel fuel line items
  only. Weekly gross and RPM use invoiced and delivered loads grouped
  Monday-first solely by DataTruck's encoded UTC pickup calendar date,
  regardless of delivery date.
- Percentage-based owner-operator pay is their gross share. Their fuel and tolls
  reduce their net settlement and must not also reduce company profit; company
  contribution is the retained gross percentage. CPM owner-operators do not pay
  fuel or tolls, so those costs remain company expenses, as they do for company
  drivers.
- Dispatcher pay is calculated weekly as the dispatcher's configured commission
  percentage multiplied by managed gross for qualified loads in that Monday–
  Sunday pickup-date week. Unassigned loads and dispatchers without a configured
  percentage remain visible but do not contribute to calculated dispatcher pay.
- Relay fuel sync records completed UTC dates and never marks the current UTC
  date complete. Driver identity is persisted in `relay_driver_links`; fuel,
  DEF, other products, fees, reporting dimensions, and raw payloads are stored.
  New Relay identities and their purchases import with nullable `driver_id`;
  they never create fleet drivers or block completion of a sync day. The Tasks
  page suggests existing drivers using normalized phone/email and name variants.
  Each new Relay ID requires explicit user confirmation, including changed IDs
  for known drivers. Existing mappings remain authoritative. Multiple Relay IDs
  may link to one driver, scoped by environment. Review and sync lock the same
  identity row; confirmation atomically links all its unassigned historical
  transactions and records the reviewer. Rejected suggestions remain dismissed.
  Fuel lists use LEFT JOINs so unassigned spend remains visible. Financial totals
  expose unassigned diesel separately (provisionally included in company costs);
  no driver settlement receives those purchases until confirmed. Never use the
  placeholder integration ID `0000000000000000` as identity evidence.
  Migration 024 is additive/relaxes nullability; rollbacks to pre-review binaries
  cannot safely display unassigned rows. Resolve pending accounts before such a
  rollback or deploy a compatible forward fix; never manufacture placeholder drivers.
- Except for health, readiness, login, and the HMAC-authenticated FleetScope
  webhook, every API route requires a valid
  database session. State-changing requests also require the session's CSRF
  token. Session cookies are opaque, HttpOnly, SameSite=Strict, and host-only;
  only SHA-256 token digests are stored in PostgreSQL.

## Implementation conventions

- Backend flow is handler -> repository, with integrations/jobs injected in
  `main.go`. Keep transport validation in `httpapi` and SQL/transactions in
  `repository`.
- Use `context.Context` through network and database calls. Return JSON errors via
  the existing API helpers and log operational failures with `slog`.
- Add focused Go tests beside the package as `*_test.go`. Existing tests cover
  DataTruck retry, load mapping, naming, document extraction, file validation,
  and PrePass client/sync mapping.
- Frontend pages that fetch or mutate data are client components. Keep request
  code centralized in `app/lib/api.ts` and shared contracts in `app/lib/types.ts`.
- Follow the existing dark zinc/blue visual language and reuse management
  primitives. Preserve loading, empty, error, confirmation, and responsive states.
- Authenticated pages use the full width available beside the sidebar. Keep the
  global shell fluid and apply horizontal scrolling only at individual tables
  when the viewport is narrower than their readable minimum width.
- Use the `@/*` TypeScript alias when it improves readability; strict TypeScript
  and no emit are enabled.
- Playwright verifies investor flows against an isolated real API/database through
  scripts/test-investors-e2e.mjs. Other frontend changes use lint, build, existing
  calculation scripts, and targeted browser checks.

## Validation

Run from the indicated subdirectory:

```powershell
# backend/
gofmt -w <changed-go-files>
go test ./...
go vet ./...

# frontend/
npm run lint
node scripts/test-gross-board.mjs
node scripts/test-driver-board.mjs
node scripts/test-driver-pay.mjs
node scripts/test-phone.mjs
npm run build
# E2E: build with NEXT_PUBLIC_API_URL=/api; set disposable MSERP_INVESTOR_TEST_DATABASE_URL
npx playwright install chromium
node scripts/test-investors-e2e.mjs
# Same isolated database safety requirement, using MSERP_DRIVER_CHARGES_TEST_DATABASE_URL
node scripts/test-driver-charges-e2e.mjs
# Browser view regressions use isolated API fixtures, with no database needed.
node scripts/test-charge-views.mjs
```

Do not run `gofmt` across untouched files in a dirty worktree. A frontend build
is the practical type/production compile check; lint alone is not enough for
contract changes. Database-dependent behavior may also require a local PostgreSQL
smoke test because the Go unit suite does not exercise every repository query.

## Efficient lookup recipes

```powershell
# Find API routes
rg -n 'r\.(Get|Post|Put|Patch|Delete)\(' backend/internal/httpapi

# Find a backend repository method or model
rg -n '^func \(.*Repository\)|^type ' backend/internal/repository

# Find frontend API usage
rg -n 'fetchLoads|createDriver|syncTolls' frontend/app

# Find schema ownership for a field
rg -n '<field_name>' backend/sql backend/internal frontend/app/lib
```

Prefer these targeted searches over recursively reading the repository.

## VPS deployment

### Production topology

- Canonical URL: `https://erp.msexpressinc.net`.
- VPS: `137.184.102.22`. DNS is managed by Wix; the `erp` A record points to
  this address.
- Nginx listens on ports 80 and 443, redirects HTTP to HTTPS, serves the static
  frontend from `/opt/mserp/current/frontend`, and proxies `/api/` to
  `127.0.0.1:18080`. The original self-signed IP endpoint on port 8443 remains
  available only as a legacy fallback.
- The domain certificate is managed by Certbot/Let's Encrypt under
  `/etc/letsencrypt/live/erp.msexpressinc.net`. Renewal uses the webroot
  `/var/www/letsencrypt`, the enabled `certbot.timer`, and the root-owned deploy
  hook `/usr/local/sbin/mserp-certbot-deploy` to validate and reload Nginx.
- The Go API is `mserp-api.service`, bound only to `127.0.0.1:18080`. Its
  working directory is `/etc/mserp` so runtime environment files stay outside
  releases.
- PostgreSQL 16 is the existing `postgresql@16-main` service. MSERP uses database
  `mserp` and application role `mserp_app`; never record its password here.
- `fuelbot.service` on port 5000 is an unrelated production service. Do not
  restart, reconfigure, move, or reuse its port. Do not alter other VPS services
  unless the user explicitly expands the task.
- UFW allows SSH, ports 80/443, the legacy 8443 endpoint, and PostgreSQL only
  from its existing private-network rule. Preserve this boundary.

### Releases, configuration, and database safety

- Releases live at `/opt/mserp/releases/<40-character-git-sha>` and
  `/opt/mserp/current` is the atomically updated symlink. Production does not
  contain or deploy from a Git checkout; do not SSH in and run `git pull`.
- Runtime environment and secret files live under `/etc/mserp`, primarily
  `/etc/mserp/mserp.env`. Never copy them into a release, artifact, log, commit,
  or tool output. Production `FRONTEND_ORIGIN` is
  `https://erp.msexpressinc.net`.
- The live Nginx site is `/etc/nginx/sites-available/mserp`, enabled through
  `/etc/nginx/sites-enabled/mserp`. Keep `deploy/nginx-mserp.conf` as its
  version-controlled source. Always back up the live file, run `nginx -t`, and
  reload rather than restart Nginx when applying a manual configuration update.
- The production frontend must be built with `NEXT_PUBLIC_API_URL=/api`. A
  direct IP API URL breaks host-only SameSite authentication cookies and causes
  CORS/login loops. Nginx requires frontend asset revalidation because release
  archives normalize timestamps to the Unix epoch; preserve the `expires epoch`
  directives. Static responses also disable ETag and If-Modified-Since
  validation: epoch timestamps plus unchanged file sizes would otherwise return
  304 for outdated HTML that references removed JavaScript chunks.
- Migration `009_add_schema_migrations.sql` created the migration ledger. The
  deploy helper applies only unrecorded numbered migrations. New schema changes
  must update `init.sql`, add the next numbered migration, and remain compatible
  with the previous app release because app rollback does not undo migrations.
- Migrations execute as `postgres`, while the API runs as `mserp_app`. Every
  newly created runtime table must transfer ownership to `mserp_app` when that
  role exists, following migration 025. Verify queries as the application role;
  an administrator-only database test cannot detect missing runtime permissions.
- Each deployment creates a custom-format PostgreSQL backup in
  `/var/backups/mserp` before migrations or release activation. Do not delete
  backups casually.

### CI/CD and access model

- `.github/workflows/ci-deploy.yml` runs all tests/builds on pull requests.
  Every push or merge to `main` automatically builds and deploys production;
  `workflow_dispatch` redeploys the selected `main` commit. App updates should
  normally go through a branch and PR, then be verified through the resulting
  `main` Actions run.
- GitHub Actions builds the Linux API and static frontend, packages numbered SQL
  migrations, uploads the checksum-verified artifact, and connects as the
  restricted `mserp-deploy` user. Repository secrets are
  `MSERP_DEPLOY_SSH_KEY` and `MSERP_DEPLOY_KNOWN_HOSTS`; never print or replace
  them during routine work.
- CI installs Playwright Chromium separately with a five-minute timeout, using
  the hosted Ubuntu 24.04 runner's existing system libraries. Do not add
  `--with-deps` to routine browser setup: redundant apt upgrades have stalled
  on the runner's package mirror and exhausted the entire build timeout.
- The deploy user owns only `/var/lib/mserp-deploy/incoming`, uses a restricted
  SSH key, and may sudo only `/usr/local/sbin/mserp-deploy`. Do not broaden its
  filesystem ownership or sudo permissions.
- `deploy/mserp-deploy` validates the SHA/checksum and archive paths, takes the
  database backup, applies migrations, switches `/opt/mserp/current`, restarts
  only `mserp-api`, reloads Nginx, runs health checks, and rolls the application
  symlink back if activation fails.
- Operator SSH access from this workstation is already configured with a key.
  Resolve exact targets before changing remote files and preserve unrelated
  services and configuration.

### Production verification

For an operator-triggered load recovery that may exceed the HTTP timeout, the
deployed API binary supports `-sync-loads-once`. It uses the same job and runtime
configuration, has a 45-minute timeout, and exits without starting HTTP or other
jobs. Run it as `mserp` from `/etc/mserp` with the service environment file, using
a uniquely named transient systemd service so an SSH disconnect cannot interrupt
the import. Avoid overlapping another load sync. For example:

```bash
systemd-run --unit=mserp-load-recovery-YYYYMMDD-HHMMSS \
  --uid=mserp --gid=mserp --working-directory=/etc/mserp \
  --property=EnvironmentFile=/etc/mserp/mserp.env \
  /opt/mserp/current/backend/mserp-api -sync-loads-once
```

Check that unit's exit status, its `sync loads complete` journal entry, and the
database load count/latest `synced_at`; process startup alone is not success.

After a deployment, do not stop at a green Actions badge. Confirm the deployed
SHA, service boundaries, HTTPS, API health, and authentication behavior:

```powershell
# GitHub Actions result for the main deployment
gh run list --repo baxbergenut/mserp --workflow "CI and deploy" --branch main --limit 1

# Active release and all services that must remain healthy
ssh root@137.184.102.22 'readlink -f /opt/mserp/current; systemctl is-active mserp-api nginx fuelbot postgresql@16-main'

# Public routing and trusted TLS (do not use -k for the domain check)
curl.exe -sS https://erp.msexpressinc.net/api/healthz
curl.exe -sSI https://erp.msexpressinc.net/login
```

For authentication-related deployments, use a temporary test session to verify
login -> `/auth/session` -> authenticated page -> logout. Confirm browser
requests stay on `https://erp.msexpressinc.net/api/...`; any request to the IP
endpoint indicates a stale or incorrectly built frontend. Never expose session
cookies, CSRF tokens, password hashes, or plaintext credentials in handoff text.
