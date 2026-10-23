# Clustie Agent Guide

DataTruck migration 141 stores unique tenant-scoped driver/truck IDs and private
driver DOBs. Verified links establish a baseline; later name/contact/DOB/license
edits queue durable profile PUTs by default. Imports suppress delivery. Missing
DOB and unknown outcomes hold for review without replay. Truck/dispatcher
assignments and lifecycle/dispatch changes are excluded. Success-only creation
receipts resolve IDs by read-only carrier-scoped checks. See
`docs/integrations/DATATRUCK_ENROLLMENT.md` for the contract and validation.

Read this file before doing anything else in a new task. Then read the nearest
nested `AGENTS.md` for the area being changed. Do not scan the entire repository
for routine work; use the map and targeted searches below first. Update this file
when architecture, commands, environment variables, or important invariants
change.

## What this repository is

Clustie (formerly MSERP) is an internal ERP for MS Express Inc. It has two independently run apps:

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
- Outbound FleetScope fleet sync uses migration 103, `fleetscope/sync.go`,
  repository `fleetscope_sync*.go`, `httpapi/fleetscope_sync_handlers.go`, and
  Settings > FleetScope. Default off; no receiver writes on deployment alone.
  MSERP owns truck identity/recorded company-driver-investor ownership and crews;
  FleetScope owns lifecycle/compliance/insurance/trailers. Do not conflate owners
  with assigned drivers or map investors to lease. Durable transactional source
  revision capture, immutable outbox bytes, HMAC HTTPS delivery, one-to-one
  reviewed UUID links, receipt replay, separate assignment revisions/sequences,
  pinned company/environment/epoch and periodic drift pausing protect writes.
  V1 applies the whole enrolled fleet crew graph atomically. Initial provisioning
  and baseline require review; no name matching or implicit VIN adoption.
  `FLEETSCOPE_SYNC_MODE` (off/reconcile/live), URL, COMPANY_ID, ENVIRONMENT, KEY_ID,
  SECRET, BOOTSTRAP_KEY_ID and BOOTSTRAP_SECRET use the `FLEETSCOPE_SYNC_` prefix.
  Outbound company matches the existing webhook company, credentials differ.
  The independent five-second worker remains off until configuration and reviewed
  activation; settings reads use cache only. `fleetscope.read/manage/reconcile`
  plus drivers.read and trucks.read protect routes; reconciliation also requires Administrator.
  See `docs/FLEETSCOPE_SYNC_WIRE.md` and `docs/FLEETSCOPE_SYNC_RUNBOOK.md` before
  changing the contract, enabling delivery or recovering restored databases.
  Exact LF contract bytes/schema hashes and fixtures must match the receiver.
  `MSERP_FLEETSCOPE_TEST_DATABASE_URL` runs fresh/incremental database tests;
  build frontend with `/api`, then `node scripts/test-fleetscope-sync-views.mjs`.
- `backend/internal/httpapi/toll_handlers.go`: toll listing and manual PrePass
  API sync.
- The temporary MS Express reconciliation portal is a separate Go binary at
  `backend/cmd/fleet-review`, with embedded UI and workflow in
  `backend/internal/fleetreview`. It uses private JSON configuration and state,
  distinct database credentials and HTTPS at `conflicts.msexpressinc.net`.
  Drafts never write fleet records. A single explicitly reviewed batch uses
  snapshot fences and immutable receipts in both databases; partial commits
  recover only through the same frozen batch. It refuses enrolled integrations.
  Approved mappings remain in receipts for later integration enrollment; portal
  submission does not activate ongoing sync. Driver creation excludes missing
  terminated drivers; ownership and crew/insurance review remain separate.
  See `deploy/README.md` for installation, recovery and retirement. Local database
  tests use `MSERP_PORTAL_TEST_DATABASE_URL` and
  `FLEETSCOPE_PORTAL_TEST_DATABASE_URL`, each ending in `_portal_test` on localhost.
- `backend/internal/httpapi/expense_handlers.go`: expense listing, filtering,
  validation, and CRUD handlers.
  Expense Driver is a searchable selection dropdown for hired drivers and synced
  FleetScope applicants. It uses the standard native select; new unlinked names cannot be saved.
  Drivers and applicants share one case-insensitive alphabetical order by name.
  Opening an expense form or returning to its tab refreshes driver/applicant choices
  without replacing the expense draft.
  Legacy manual-name expenses retain their historical text. Migration 134 stores
  applicants separately from the fleet and uses explicit FleetScope applicant-to-hired
  driver UUIDs plus reviewed local identity links to show prehire expense history.
  No payroll responsibility is inferred. See docs/FLEETSCOPE_APPLICANT_AGENT_PROMPT.md.
  AI extraction receives active category/type settings, matches
  expense types by meaning before allowing a custom type, and never substitutes
  a truck's current driver for an explicit unmatched/ambiguous name. Work-order
  drafts and saves preserve the same unlinked source name.
  Identifiable expense-related service reports such as MVR/PSP can produce partial
  review drafts without a price; unknown amounts/payment details remain blank.
  Bare licenses and unrelated documents do not establish a purchased service.
  Payment methods use active Expense settings in dropdowns and
  native expense/bulk/work-order save validation. Existing entries can retain
  their saved historical method; arbitrary new methods are rejected.
- `backend/internal/httpapi/gross_board_handlers.go` and
  `backend/internal/repository/gross_board_repository.go`: weekly dispatch plans,
  exact load matching, suggestions, and versioned atomic autosaves.
- `backend/internal/httpapi/file_handlers.go`: IRP/cab-card uploads,
  extraction orchestration, and stored-file downloads.
- `backend/internal/repository/`: SQL and domain/API structs. `fleet_repository.go`
  also owns assignment transactions; `naming.go` owns canonical person and truck
  naming; `load_repository.go` maps DataTruck records to local loads.
  Migration 104 adds nullable driver `first_name`/`last_name` (API `firstName`/
  `lastName`, each 1–50 characters), matching FleetScope's name structure.
  Confirmed parts produce `full_name` in First Last order and its matching key;
  legacy full-name-only writers cannot overwrite confirmed parts. Existing names
  are never split automatically. Forms require both for new drivers; unrelated
  legacy profile edits can leave both unknown. Portal review saves both parts.
  FleetScope's existing signed full-name-only hire contract remains unchanged;
  its setup form requires a reviewed split, never guessed token positions.
  Migration 114 adds optional truck `color` (maximum 30 characters). Truck forms
  and details expose it; omitted/null API input preserves an existing color,
  while an explicit empty string clears it. Portal batch submission saves the
  reviewed color to both systems. Unknown source colors are omitted from portal
  snapshots to preserve existing review fences; known color changes are fenced.
  The ongoing FleetScope wire contract and color authority are unchanged.
- `backend/internal/datatruck/`: paginated DataTruck client with rate-limit retry.
  A shared priority request gate paces all attempts at 15/minute; discovery
  takes priority over queued refreshes and 429 cooldowns apply to every caller.
  Driver Services and new truck forms default DataTruck enrollment on. Migration
  128, repository `datatruck_enrollment.go` and client `fleet.go` queue requests
  atomically with native saves, then enroll server-side. `DATATRUCK_MC_ID` is an
  explicitly verified internal carrier ID. Create POSTs never auto-retry; durable
  receipts and possible-match checks hold uncertain/duplicate outcomes for review.
  Truck enrollment status/retry is at `/trucks/{id}/datatruck` and on its profile;
  driver status uses the existing service cards/tasks. No assignment, lease, owner
  ID or ELD mapping is inferred. See `docs/integrations/DATATRUCK_ENROLLMENT.md`.
- `backend/internal/relay/`: Relay Payments fuel transactions and driver/policy client.
  Fuel's third tab, Drivers, uses `repository/relay_drivers.go` and
  `httpapi/relay_driver_handlers.go`; migration 075 stores roster snapshots and
  durable operation receipts. See `docs/integrations/RELAY_DRIVERS.md`.
  Driver/policy calls use `/api/integrations`; transactions use `/api`.
  Assignment GET/PUT/DELETE paths use driver IDs (verified against staging).
  `MSERP_RELAY_LIVE_STAGING_API_KEY` explicitly opts into the staging lifecycle
  test, which creates and removes a synthetic driver; ordinary tests skip it.
- Relay termination offboarding uses migration 076 and
  `repository/relay_offboarding.go`: immediate startup plus minute interval,
  confirmed links only, disable assigned policies on termination and remove on
  termination date + 10 New York calendar days (including existing overdue).
  Rehires cancel later actions; uncertain receipts require portal review.
  `POST /relay-drivers/bulk` shares revision/receipt guards with single actions,
  processes at most 100 independently and refreshes once. See
  `docs/integrations/RELAY_DRIVERS.md`.
- PrePass truck mappings use migration 077, `repository/toll_truck_links.go`,
  `httpapi/toll_truck_link_handlers.go`, and Tolls > Needs attention. Group by tag
  with exact-unit fallback; never guess equivalent names or strip leading zeros.
  Date-bounded reviewed mappings resolve unlinked tolls and future imports;
  preserve existing links. Preview/save/undo use `tolls.link`; listing uses
  `tolls.read`. Finalized driver/investor statements block manual corrections.
  See `docs/integrations/PREPASS_TRUCK_LINKS.md` for dates, audit and validation.
- `backend/internal/prepass/`: authenticated PrePass account discovery and
  paginated toll transaction client.
- Tolls > Trucks, Transponders and Needs attention use top-level tabs. Fleet
  rosters refresh daily at the configured fuel sync time when scheduled syncs
  are enabled. Available devices can be assigned from either roster tab.
  PrePass uses Fleet Management v2 for truck/transponder rosters, reviewed
  VIN links, add/edit/remove trucks and assignment of available same-account
  transponders. Migration 082 adds environment-scoped snapshots, links and durable
  receipts; `repository/prepass_fleet.go` and `httpapi/prepass_fleet_handlers.go`
  own persistence and routes. Reads/refresh use `tolls.read`, local links use
  `tolls.link`, vendor changes/recovery use `tolls.fleet.write`. Existing credentials
  are reused server-side. Cached reads never call the vendor. Writes refresh first,
  check revisions and availability, then commit a receipt before the network call.
  Unconfirmed writes block further changes until explicit portal review, except
  assignments, which reconcile automatically. Migration 120 tracks accepted
  assignments as queued with truck/device reservations instead of a global pause.
  A startup/15-second read-only worker confirms matching vehicle/transponder
  rosters, including older uncertain assignments, without resending mutations.
  Assignment UI submits once with loading, then displays automatic provider
  processing status. This fleet has no VPP enrollment; no VPP checkbox is required.
  VPP,
  trailer plates, device transfers/replacements and ordering are outside this tab.
  Migration 083 and repository/prepass_fleet_tolls.go make fleet linking atomic
  with dated toll mappings: require start date and preview; tag (transaction
  ppDeviceId = fleet transponderNumber) first, exact unit fallback, account/env
  scoped. Never equate transaction deviceNumber with that tag. Only NULL truck
  transactions are backfilled; existing links stay unchanged. Finalized reports,
  stale previews and conflicting mappings block writes. Relinking closes old
  fleet rules from the chosen date without unlinking their historical tolls.
  Previously linked/add results and changed provider identities without current
  rules show Link tolls. No start dates are inferred. Truck numbers link to profiles;
  one Truck column replaces separate local/provider columns. Account UI is hidden.
  Historical unlinked toll review and saved mappings live under Needs attention.
  Add selects an existing truck and summarizes saved VIN/unit/plate values;
  only missing or unsupported fields are prompted. Weight/IRP/IFTA are not stored
  in truck profiles and remain required inputs. Other details expand for edits.
  A missing local VIN may be supplied for enrollment only, without changing the
  profile or taking another local truck's VIN. Existing VINs must match. Selection
  keeps its original timestamp through refreshes; switching trucks resets drafts.
  See `docs/integrations/PREPASS_FLEET.md`; tests use disposable
  `MSERP_PREPASS_TEST_DATABASE_URL` and `frontend/scripts/test-prepass-views.mjs`.
  Trucks > Resolve plate lets users choose the linked MSERP or PrePass plate/state
  for both records (`edit` with `plateSource`, truck ID and current timestamp).
  PrePass source copies locally with `trucks.edit` and no vendor write.
  MSERP source also requires `tolls.fleet.edit`;
  exact linked VIN, receipt guards, plate-only provider PATCH and verified read-back
  protect the local update.
  Uncertain-result review acknowledges the request without changing local plates.
- `backend/internal/weighmytruck/`, repository/httpapi `weighmytruck_*` files and
  `frontend/app/weighmytruck/`: server-only OAuth driver enrollment/removal,
  versioned local membership tracking and terminated-driver warnings. Migration
  073; saved vendor documentation and setup are in `docs/integrations/WEIGHMYTRUCK.md`.
  There is no upstream roster endpoint. Initialize via `-import-weighmytruck-roster`
  from the private website CSV; unmatched identities require explicit linking.
  Pending/uncertain changes cannot replay until verified; removal uses saved email.
  The page lists only added drivers, attention rows first, with warnings after
  names. Rows use compact status labels with warning details on icon hover.
  Add driver selects an active unadded driver; success notices clear after
  five seconds. No Membership column/filter or routine Verify action is shown.
  Uncertain additions remain visible as notices. Failed/stale changes expose
  Review membership with weighmytruck.recover: explicitly confirm the saved email's
  fleet-roster membership with a note through the versioned, audited verification
  API. Removed clears the row; enrolled re-enables Remove. Generic HTTP 400 is not
  proof of removal, and pending changes stay locked for two minutes.
  `weighmytruck.read/write` protect routes. Server config is WEIGHMYTRUCK_CLIENT_ID,
  WEIGHMYTRUCK_CLIENT_SECRET, WEIGHMYTRUCK_TOKEN_URL, WEIGHMYTRUCK_SCOPE,
  WEIGHMYTRUCK_API_URL and WEIGHMYTRUCK_COMPANY_NAME. No browser vendor calls.
  `MSERP_WMT_TEST_DATABASE_URL` runs fresh/migrated tests as mserp_app;
  `MSERP_WMT_BROWSER_TEST=1` adds real API/database browser checks after a /api build.
  Migration 074 adds scale transactions, audited links and durable onboarding
  enrollment. Scale payer is automatic from ticket-date truck ownership: company
  pays company trucks, owner-operators pay on Driver Pay (CPM and percentage),
  hired investor-truck drivers charge Investor Pay with matching dated terms.
  Teams route to the dated main driver. Manual billing rows are history only;
  the old billing endpoint rejects writes and the UI shows a read-only payer.
  Missing/conflicting attribution stays in review. Charges frozen elsewhere
  block a second destination until the old statement is explicitly reopened.
  Drivers/Transactions tabs use server-only CSV settlement sync, scheduled at
  the fuel sync time; production `WEIGHMYTRUCK_SYNC_START_DATE=2026-09-17`
  includes the full available month (the code fallback remains October 5).
  Vendor CSV dates may have unpadded months/days. The vendor permits only the last 30 days;
  rechecking that window catches corrections without duplicate tickets.
  Exact driver codes or unique dated truck assignments resolve drivers, and
  stored unit aliases resolve trucks. Ambiguity is flagged for manual linking.
  Driver and Investor Pay include read-only CAT Scale autoCharges in the ticket
  week. Finalized snapshots stay frozen; differing source tickets show warnings.
  Link writes require payroll.write and use the financial lock.
  No Expenses & Charges rows are created. FleetScope setup can queue enrollment;
  a startup/minute worker removes terminated enrolled drivers and processes adds.
  Uncertain provider mutations retain the existing review/no-replay boundary.
  `-sync-weighmytruck-once` runs only settlement import for operator recovery.
- `backend/internal/jobs/sync_loads.go`: synchronous load sync for new upstream
  record IDs, five-minute operational refresh, and morning 21-day reconciliation.
  `backend/internal/repository/load_sync_repository.go` owns operational candidate
  selection and source-only refreshes without replaying fleet assignments.
- `backend/internal/jobs/sync_fuel.go`: synchronous missing-day Relay fuel sync.
- `backend/internal/jobs/sync_tolls.go`: synchronous missing-day PrePass toll
  sync in API-compatible date windows.
- `backend/internal/groq/`: vision extraction client and normalization for truck
  cab cards. CDL upload/extraction is no longer exposed by the API.
- `backend/internal/openai/`: structured expense extraction from user-provided
  text, PDFs, text files, and receipt images.
- `backend/internal/db/pool.go`: pgx pool configuration.
- `backend/sql/init.sql`: complete schema for a new database.
- `backend/scripts/prepare-expense-import.ps1`: validates Google Sheets expense
  CSV exports and creates an idempotent, source-row-traceable SQL import.
  Imports after migration 085 create unattributed historical verification records;
  reimports preserve the application's current approvals and cancellations.
- Temporary outbound expense-sheet backup uses migrations 105/107,
  `backend/cmd/export-expense-sheet`, and `internal/expensesheet`. It captures only
  new native HR/Safety/Penalties/Other/work-order invoices after explicit database activation;
  deployment alone sends nothing. A separate notification-driven systemd service uses
  `EXPENSE_SHEET_CREDENTIALS_FILE` (server-only Google service-account JSON key).
  Immutable transactional snapshots and atomic Google row/metadata receipts
  prevent retry duplicates. It fills the blank row after the latest expense, excluding
  Company, Accounting Verification and side-list columns from all writes; literal
  prefilled company labels do not block otherwise blank rows. No row insertion
  or dimension changes. Occupied targets and exhausted blank rows hold for review.
  It never mirrors later edits or backfills history.
  Migration 138 adds separately opt-in prospective scale copying via scale_enabled
  and scale_from_date. Newly inserted WeighMyTruck tickets use Other/Scale in the
  existing Maintenance sheet, final linked identity and dated payer, with stable
  site/ticket receipt IDs. No native expense or duplicate payroll charge is created;
  existing tickets and later corrections never auto-export. Unknown payer stays in review.
  While activated, eligible native entries dated before September 28, 2026 are
  settled on creation; later dates retain normal payment status. The dedicated
  MSERP Paid export column stays separate from verification checkboxes.
  Imported/system-generated records and other categories are excluded. Sheet
  MSERP Entry ID columns prevent feedback through the CSV import helper. See
  `docs/integrations/EXPENSE_SHEET_EXPORT.md` for sharing, activation and retirement.
  `MSERP_EXPENSE_SHEET_TEST_DATABASE_URL` runs local fresh/migrated database tests.
- `backend/sql/002_add_tolls.sql` through
  `075_relay_drivers.sql`:
  manual incremental migrations for older databases.

### Frontend

- Product branding is Clustie. `components/BrandLogo.tsx` uses the supplied logo
  pack artwork in `frontend/public/brand/`, with transparent backgrounds and the
  symbol in the collapsed sidebar. A CSS alpha mask makes only the wordmark light
  on dark themes; light themes retain navy, and the symbol/i dot keep their colors. Root metadata,
  `favicon.ico`, `icon.png`, `apple-icon.png` and `manifest.webmanifest` own browser
  and home-screen branding. Keep existing MSERP API/wire identifiers, environment
  variables, cookie/storage/event keys, sheet headers and historical records
  compatible; MS Express remains the operating company.
  Sidebar branding uses a 96px wordmark whose visible symbol matches the padded
  32px collapsed icon. Reserve the frame's aspect ratio, remount image variants,
  and clip the header during width transitions to avoid stale-image enlargement.
  `node scripts/test-sidebar-branding.mjs` checks delayed image loads and toggling.

- Fleet > Inventory (`/fleet/inventory?tab=tablets`) and Fleet > Trailers
  (`/fleet/trailers`) use migration 119, repository `inventory.go` /
  `inventory_import.go` and `httpapi/inventory_handlers.go`.
  Fleet navigation starts with Trucks, Trailers, then Inventory. Trailers uses
  the Trucks directory/table patterns and `/fleet/trailers/detail?id=...` profiles;
  its shared versioned editor preserves trailer-only read/manage permissions.
  Inventory tracks devices, separate carrier lines, camera/gateway links, locations, annual sticker
  batches, truck observations and an append-only stock ledger. Versioned edits,
  idempotent movement IDs, foreign keys and a short inventory transaction lock
  protect assignments and nonnegative stock. No stock is inferred from sheet
  checkboxes; physical opening counts (including zero) precede issuance. Truck
  details expose permission-gated Equipment summaries. The PrePass tab reuses
  existing cached rosters and vendor safeguards, without a local tag-notes UI;
  historical imported notes remain stored for audit. Migration 121 unifies
  `dashcams` with controlled Samsara/FiveELD brands, adds `phones`, and keeps
  `samsara-gateways` separate. Only Samsara dashcams link gateways. Legacy camera
  bookmarks redirect; unclassified Other devices stay stored outside normal UI.
  Truck Equipment shows linked serialized devices only; old hardware checklists
  are archived. Device installation requires a direct/inherited truck, enforced
  by API and deferred DB trigger. Gateway moves reset camera verification in the
  same transaction. Statuses use controlled codes, capitalized colored badges.
  Supplies is a versioned presence checkbox matrix grouped by recorded company
  ownership, including inactive trucks. Stock/units/history UI is deferred;
  audits and ledger APIs remain stored, and checking presence never moves stock.
  Inventory phone displays reuse the shared phone formatter;
  carrier status appears on AT&T lines rather than next to tablet numbers.
  Stored trailers have no dispatch/Status Board or
  FleetScope assignment synchronization. Imports require Administrator plus
  `inventory.import`; source snapshots and row outcomes remain private and must
  be reviewed before an atomic commit. No accounting, compliance or vendor writes
  occur on deployment/import. See `docs/FLEET_INVENTORY.md` for snapshot preparation,
  review, cutover and maintenance-baseline reconciliation. Database tests use
  disposable `MSERP_INVENTORY_TEST_DATABASE_URL`; browser checks use a `/api` build
  and `node scripts/test-inventory-views.mjs`.

- Status Board appointment/arrival automation uses migration 116, repository
  `board_automation.go`/`board_travel.go`, and `internal/boardroutes`. Preserve
  DataTruck per-stop schedule fields through import serialization. Appointment
  text is read-only inside ETA; @PU/@DEL replaces travel mileage when the last
  GPS position was within five straight-line miles of the selected stop,
  including stops without a provider schedule.
  Fresh automatic markers are red; stale last-known markers are amber.
  ETA is automatic while its entry is blank; a typed
  time or mileage overrides it until cleared. The ETA popup has a blank optional
  manual PU/DEL time for the selected load stop, overriding only the displayed
  DataTruck schedule until cleared. Both former arrival/travel mode dropdowns
  are removed and legacy mode choices have no effect. Notes are never rewritten. Unique imported loads can
  resolve without Gross Board plans; entering a load is the operational assignment,
  even when DataTruck lists a different driver, without an extra confirmation.
  A startup/minute cache-only GPS evaluator never advances progress. Google truck
  routing is default off and requires explicit access/operations/speed verification
  plus reviewed per-load/trailer truck profiles or explicit
  `BOARD_ROUTES_STANDARD_SEMI=true` (operator-approved shared dimensions, assumed
  80,000 lb gross; assumptions retained server-side, hazmat not modeled). Explicit
  invalid profiles never fall back. DataTruck weight is absent in live samples.
  Never substitute car routes.
  `GOOGLE_ROUTING_API_TOKEN` aliases `BOARD_ROUTES_API_KEY` when the latter is empty.
  Stale GPS displays amber last-known miles without diagnostic hover notes, including
  a one-time route from cached coordinates when no estimate existed. Reuse the
  unchanged position's route; never infer stopped/ignition state from staleness.
  Fresh moving trucks show miles above 600 miles remaining, otherwise ETA;
  moving ETAs become last-known miles after ten minutes.
  See `docs/integrations/STATUS_BOARD_AUTOMATION.md` for environment variables,
  proximity/freshness rules and rollout. ETA times round to the nearest 15 minutes;
  cell hover shows only facility-local ETA (offline coordinate timezone lookup).
  Live travel is red; stale miles are amber. Routing assumptions remain server
  diagnostics. Load cards expose a rightmost read-only-compatible copy-number icon.
  Browser checks: `test-board-appointments.mjs`
  and `test-status-board-layout.mjs`; database tests use the existing disposable
  `MSERP_DRIVER_BOARD_TEST_DATABASE_URL`.

- `frontend/app/layout.tsx` and `components/AppShell.tsx`: session-aware global
  shell and sidebar; `frontend/app/login/` owns the login page.
  `GET /sidebar-counts` returns permission-scoped red badge counts for terminated
  Relay accounts in the cached roster, terminated WeighMyTruck memberships with
  confirmed/unresolved access, and unlinked tolls posted since the first day of
  the previous New York calendar month. Counts use local data only and refresh
  after relevant mutations, navigation, focus and every 30 visible seconds.
  Zero counts are hidden; collapsed badges retain their exact accessible counts.
  `components/PageNavigation.tsx` restores view and scroll state for browser/mouse
  Back and Forward navigation. Pages have no in-app Back links.
- `frontend/app/page.tsx`: redirects `/` to `/gross-board`.
- `frontend/app/dashboard/`: redirects legacy Dashboard links to Gross Board.
  The old load-derived dashboard is removed pending a Gross Board-based replacement.
- `frontend/app/accounting/driver-pay/`: collapsed weekly driver accordions sourced
  from Gross Board, system load details, profile tariffs, comments and adjustments.
  Driver summaries form a connected compact table; comments use hover previews and
  modal editing. Weekly adjustment name/amount columns follow the driver fee, independent of
  load rows, and scroll only when entries exceed the visible load rows. Names and signed amounts edit
  inline in blank table rows; clearing both removes an entry. Partially filled
  adjustments block autosave/navigation until completed or cleared.
  `backend/internal/repository/driver_pay_repository.go` and
  `backend/internal/httpapi/driver_pay_handlers.go` own its API and persistence.
- `frontend/app/accounting/driver-charges/`: truck-based recurring fees for every
  non-company truck, driver installment plans, and shared charge types. The Truck
  charges tab defaults open; per-charge counts and Included/Excluded truck filters
  show membership for the selected effective week. There is no driver recurring tab.
  Truck-charge reads include per-truck/type finalized-period locks. Select all
  changes only unlocked visible cells and selects the remainder of a mixed
  selection; locked fees stay read-only and retain their inclusion and amount.
  Writes recheck the same period guard under the financial lock.
  Eligibility for reads and writes uses ownership at the selected week's end;
  later company transfers do not block historical non-company truck fees.
- `frontend/app/accounting/escrow/`: standalone escrow directory, payment status
  filters, collection history and the new-driver default; shared table in Driver
  details. `escrow_repository.go` and `escrow_handlers.go` own its API.
- `frontend/app/accounting/dispatcher-pay/`: weekly dispatcher commission reports.
- `frontend/app/loads/`: load table, filters, sorting, and manual sync.
- `frontend/app/gross-board/`: Monday–Sunday dispatch planning grid, dispatcher
  grouping/filtering, load suggestions, autosave, and exact-decimal totals.
  Migration 115 adds commit-only notifications and week-scoped, session-owned
  presence. `/gross-board/events?weekStart=...` pushes grossBoard/peers data
  directly; normal updates perform no follow-up browser board GET. Snapshot
  caches are bounded to 16 weeks and keyed by week plus committed revision;
  projection and session checks remain per subscriber. Presence read/write/delete
  require board.read and never grant board.write. Cursors identify driver, date,
  load slot and field (including Daily loads and notes), expire after 30 seconds,
  heartbeat every 10 seconds and clear on leave. Both boards share
  `components/boards/useLiveBoard.ts` for stream/presence lifecycle.
  `liveMerge.ts` retains dirty/saving slots and active day-dialog baselines while
  other slots refresh immediately. Daily loads applies its original draft versions,
  so concurrent edits conflict instead of silently overwriting saved changes.
  Run `node scripts/test-investors-e2e.mjs --gross-live-only` with a disposable
  MSERP_INVESTOR_TEST_DATABASE_URL for two-user data/presence, week isolation,
  no extra GET, concurrent typing, modal conflicts, reconnect and latency checks.
  Dispatcher checkboxes share `components/management/BoardFilter.tsx` with Status
  Board and remember multiple selections (legacy single choices still restore).
  Weekly driver RPM has its own column. After Sunday, columns are Total gross,
  Total miles, RPM, Driver gross, and Rate balance. Total gross is the shared display
  name across boards, Daily loads, balance history and payroll; legacy API/storage
  field names remain compatible. `BoardScrollArea.tsx` scrolls main until the table is
  8px below navigation, then main stops and rows scroll inside the bounded table
  under its native sticky header. Upward scrolling unwinds table rows before
  revealing the controls. Horizontal scrolling and frozen Driver/Truck remain.
  `node scripts/test-gross-board-layout.mjs` covers filters, RPM, header scrolling,
  frozen columns, resizing and navigation restoration with fixture APIs.
  Numeric fields preserve typed text and the caret through autosave/refresh,
  reject more than two decimal places, and send only complete decimals to the API.
  While editing Total gross, a following Driver gross mirrors its exact text;
  load fields show comma separators and gross dollar signs outside editing;
  totals, balances, previews and RPM share two decimal places, without
  changing saved amounts or exact-decimal totals. Editing uses plain numbers.
  New load matches default blank Driver gross to Total gross on both browser
  selection and server exact-number resolution; explicit amounts, including zero,
  remain intact. `test-gross-board-numbers.mjs` covers delayed formatted saves.
  Status Board, Gross Board (including Daily loads), and payroll share opt-in
  spreadsheet selection/navigation in `app/lib/spreadsheetGrid.ts`: click selects,
  arrows move, typing replaces, F2/double-click edits with a caret, Ctrl/Cmd+C copies.
  Gross Board marks stacked fields with data-grid-cell. Suggestions retain their
  own keyboard handling. Ordinary list/directory tables do not opt into this behavior.
- `frontend/app/driver-board/`: live active-driver dispatch grid, dispatcher/status
  filters, current New York week Gross Board totals and versioned autosaves.
  Driver status choices/order/colors follow Driver Board > TODAY!J4 (gid 1851742271),
  separately from load/Gross Board day statuses. Migration 122 adds the sheet
  choices; legacy saved statuses and undo history remain valid without inferred
  replacements, but are not offered for new selection. ACCIDENT is trimmed.
  Migration 118 stores a shared driver order per dispatcher (including Unassigned).
  Drag the grip beside a driver to move the actual row vertically within its
  dispatcher; neighboring rows animate into place, with reduced-motion support.
  Pointer capture and edge scrolling support mouse/touch. The preview stays in
  place through a save; failed saves and Escape/cancel restore the prior order.
  Focus the grip and press
  Up/Down for keyboard moves. `POST
  /driver-board/order` requires driver_board.write and a current roster/order
  revision. Moves stay within one dispatcher, retain filtered-out drivers, and
  broadcast the saved order to other viewers through Status Board SSE. New drivers
  append; departing drivers disappear without changing the saved order. Test with
  TestDriverBoardDatabase and `test-investors-e2e.mjs --board-only`.
  Migration 125 stores one shared dispatcher-group order, including Unassigned.
  Drag the grip in the Status Board Dispatcher filter to preview animated, bounded
  moves; the board groups update after drop and save. Up/Down moves also work from
  the grip. `POST /driver-board/dispatcher-order` requires driver_board.write and
  a current order/roster revision; new groups append, absent groups disappear, and
  saved changes broadcast through Status Board SSE. The filter remains shared with
  Gross Board, but ordering is enabled only on Status Board.
  Status Board's Next load column shows only the first queued load, with its first
  pickup stop's source schedule label beside the number using the ETA PU format.
  Clicking the cell still opens the full queue; missing schedules stay blank.
  Loads > Add next load saves an exact number in the audited load-state JSON,
  independently of Gross Board. Manual queue IDs persist across imports; cached
  unique number matches populate the same details and stop/progress controls.
  Unknown/ambiguous numbers stay visible, and manual entries do not age out at
  midnight. Queue ordering/removal, history, undo and live updates remain shared.
  Migration 113 adds commit-only PostgreSQL notifications for board sources and
  authenticated `/driver-board/events` SSE. Board snapshots and named cursors
  are pushed directly as board/peers frames, avoiding a follow-up browser GET.
  Per-channel snapshots are shared across subscribers for the same revision;
  each subscriber still rechecks authorization. Empty invalidations are the
  fallback if snapshot loading fails; GET/polling also recover missed updates.
  `useBoardLive.ts` publishes a tab's selected cell; session-owned cursors expire
  after 30 seconds, heartbeat every 10 seconds, and clear on hide/navigation/logout.
  Presence GET/PUT/DELETE require driver_board.read, including read-only viewers;
  actor/session identity comes only from the cookie. No session hashes reach clients.
  Incoming updates preserve dirty, saving and actively edited rows, while refreshing
  other rows immediately; existing version conflicts still prevent lost writes.
  `CellPresence.tsx` clips named outlines to visible cells and sticky columns.
  The shared notification hub in httpapi/task_events.go listens once per channel
  per API process, reconciles reconnects, rechecks sessions and drains on shutdown.
  TestDriverBoardDatabase covers fresh/migrated cursor ownership, expiry and revocation;
  TestTaskEventsDatabase covers both channels and reconnect/rollback behavior.
  `node scripts/test-investors-e2e.mjs --board-only` includes two-user live updates,
  cell movement, concurrent typing, reconnect, expiry and navigation cleanup.
  It verifies zero follow-up board reads and measures edit-to-screen latency with
  both local networking and simulated 178 ms round-trip latency, including SSE frames.
  Saves run immediately and queue typing during in-flight requests. ETA accepts
  24-hour time or numeric miles left: entering either clears the other; the
  last input wins. Legacy dated ETAs display time only, and old combined values
  display their last stored component (miles), retaining full audit history.
  StatusBoardScrollArea caps page scrolling at the table header, then routes
  vertical wheel/touch scrolling into the table; upward scrolling reverses that
  order. Its viewport sizing follows main and keeps horizontal scrolling local.
  `node scripts/test-status-board-layout.mjs` checks ETA persistence and wheel,
  touch, reverse, horizontal and resize behavior with isolated browser fixtures.
  Dispatcher and status dropdowns retain checked selections (intersection),
  including previous My view selections. Notes precedes ETA. Trailer cells use
  right-click Add/Edit/Delete note and hover previews with corner markers;
  replacing the trailer clears its note. Migration 111 stores/audits trailerNote.
  Right-click editable dispatch cells for driver/column history; the history
  column filter runs before pagination. Ctrl+Z/Cmd+Z reverses the current page's
  own actions, grouping consecutive saved keystrokes in one cell edit. Server
  actor/version/field-history guards protect other users' changes. Reload clears
  the personal undo stack. Run `test-investors-e2e.mjs --board-only` for real
  API/database editing, notes, filters, history and undo regression coverage.
  Keep fields on one line with compact rows. Prioritize columns through ETA;
  later fields may extend into horizontal scrolling. Show type codes in round parentheses after
  driver names, with a copy-location map pin. No separate type/location columns or
  per-name history icon; history remains in the toolbar and Loads panel.
  `driver_board_repository.go` and `driver_board_handlers.go` own its backend.
  Status fills follow the TODAY sheet where a matching rule exists. Future
  feature scope, exclusions and rollout order live in `docs/DRIVER_BOARD_PLAN.md`.
- `frontend/app/tolls/`: toll overview, transaction table, and manual PrePass sync UX.
- `frontend/app/maintenance/`: typed service visits, vendor snapshots, invoice/document
  history, background odometers and independent PM/oil schedules.
  `repository/maintenance_*`, `httpapi/maintenance_handlers.go` and migration 096
  own its backend. See `docs/MAINTENANCE.md` for accounting, permissions, migration
  review and reminder rules. Truck details reuse the workspace in a Maintenance tab.
  Migration 097 simplifies entry: service types supply work/invoice names and PM Service
  resets, service date supplies invoice date, and the visit stores its driver even
  without a charge. Default US shops are directory records. AI source files attach
  automatically. Today's blank odometer reads Five ELD telemetry by unique exact VIN
  (one-hour freshness); receipt/manual readings and historical dates are preserved.
  Vendor management lives under Fleet > Work orders > Settings, outside the entry modal.
  Fleet groups Work orders (`/maintenance`, existing workspace) and Preventative
  maintenance (`/fleet/preventative-maintenance`). Migration 099 stores oil grades
  and seeds 300 sourced US vendor locations (docs/data/maintenance-vendors-us.json).
  The preventative API lists each truck with its latest completed PM Service visit
  and latest dated reading. Migration 126 adds audited, versioned manual last-PM
  dates/service odometers through PUT /maintenance/preventative/{truckId} and
  compact table editors requiring Maintenance Edit. Work orders/invoices remain
  unchanged; a new/changed latest completed PM supersedes the operational baseline.
  Due services and reminders share the same baseline; current odometers use the
  existing dated reading API and chronology guards.
  PM and legacy oil completions reset the same schedule;
  one table has no service tabs, vendor, service cost or last-service-note columns. Migration 102 adds durable Five ELD mileage snapshots: the minute
  worker polls active exact-unique VINs on a five-minute cadence (three concurrent,
  forty-second budget), independently of browsing and schedules. GETs use cache
  only; source/time appears on odometer hover. Due odometers, signed miles remaining
  and status use the same interval/override logic as service tasks. Stale mileage
  cannot establish Current; historical service readings are never rewritten.
  New PM Service entry defaults to 10W-30, supports other/custom grades, and AI extracts
  only explicit source grades. Existing unknown historical grades stay blank.
  Preventative Settings edits one fleet PM Service interval; truck overrides
  and existing reminder behavior remain in place. Work orders has History/Settings
  only; Settings separates service types, searchable paginated vendors and intervals.
  The manual odometer-history, Due services and Reports panels are removed.
  Expenses includes existing work-order charges once, with ordinary category-scoped
  verification. Selecting its Work orders category opens the shared work-order modal
  in place. Mixed AI imports review entries sequentially using the appropriate form
  and attach source files to work orders after saving. The configuration category
  UUID stays stable (default display name becomes Work orders); existing financial
  snapshots, approval/payment identities and legacy scope=general reads are kept.
  New operational work orders can start without an invoice or known cost;
  imported history keeps unknown costs.
  Work-order entry uses one unframed Service type and the shared expense Add cost
  breakdown. Migration 110 separates invoice cost names from typed services while
  retaining exact totals, historical allocations and PM classification. Existing
  multi-type orders keep their saved types until an explicit selection replaces them.
  `cmd/import-oil-changes` defaults to a dry run of the Fleet board A:G snapshot;
  reviewed `-apply` writes idempotent visits with source notes, never expenses.
- `frontend/app/expenses/`: Expenses directory with name/category rows, amount-hover
  payment balances, row-click editing and modal deletion; review-first AI entry.
  POST `/expenses/{id}/verify` takes the displayed updatedAt. Built-in Administrators
  add the green managerVerified check; `expenses.accounting.verify` adds the blue
  accountingVerified check. Category View is required. Ordinary single/batch
  creation cannot set checks; edits preserve them. Verification is version-checked
  and adds only the caller role’s check, without overwriting the other role.
  Migration 085 records verifier IDs, name snapshots and timestamps, plus
  cancellation actor/time. Historical checks have unknown authors. POST
  `/expenses/{id}/unverify` takes the exact verificationId; own accounting checks with the current verification grant, and both roles
  for Administrators can be removed. Cancelling another person’s check is Administrator-only. Category
  View remains required. Old IDs cannot cancel a newer approval. Click the tick
  for details/removal; Ctrl+Z/Cmd+Z undoes this page session’s own approvals
  without intercepting form typing. Hover lists each verifier.
  The session exposes expenseVerificationRole, derived server-side.
  GET `/expenses` accepts optional accountantVerified/adminVerified (`true` or
  `false`, omitted for all). Both combine with category/search/date filters before
  summary totals and pagination. Verify/cancel refresh the filtered result.
- `frontend/app/investors/`: truck owners, independent investors and driver-linked
  investors. The company is hidden by default with a Show company filter.
  The directory includes driver-linked owners regardless of truck count.
  `/investors/detail` and linked drivers' Ownership tabs share owned-truck and
  investor statement summaries. Driver and investor statements stay separate.
  Truck forms select explicit owners; ownership is separate from operation.
- `frontend/app/drivers/`, `trucks/`, and `dispatchers/`: client-side CRUD pages;
  their colocated `*Form.tsx` files own form conversion/defaults. Driver and
  truck detail pages include expenses linked to that record.
- `frontend/app/components/management/ManagementUI.tsx`: shared management page,
  modal, form, table, empty/error, and confirmation primitives. Reuse these
  before creating parallel UI patterns.
- `frontend/app/components/management/ColumnFilter.tsx`: native popovers in table
  column headers for record filters, with active indicators and keyboard dismissal.
  Keep headers present on loading/empty results; keep search and page-wide periods
  above tables. `test-column-filter-views.mjs` checks 16 views against the isolated
  local preview using `MSERP_REVIEW_ACCESS_FILE` from `review-local.mjs`.
  Status Board/Gross Board retain toolbar filters. Column titles have no filter icons.
- `frontend/app/components/`: shared dashboard, filtering, and navigation pieces.
- `frontend/app/components/TopBar.tsx`: global paginated record search, quick-create
  links and account menu. PageHeader portals render each page title, icon and count
  on the left of the top bar, preserving page context/handlers. Page headers
  have no descriptions. Sign out exists only in the top-bar account menu.
  Page actions/status controls sit beside filters/search below summary cards; global controls
  stay on the right.
  Explicit search query parameters override remembered
  list search/page values; new query tokens open existing forms once.
- `frontend/app/lib/api.ts`: API base selection and every frontend request.
- `frontend/app/lib/types.ts`: frontend view/input contracts.
- `frontend/app/lib/pdf.ts`: browser-side PDF-to-JPEG rendering used before GROQ
  extraction; the original PDF is still uploaded and stored.
- `frontend/app/globals.css`: Tailwind import, theme tokens, and global animation/
  scrollbar styles.
- `frontend/app/themes.css`, `app/lib/themes.ts`, and `components/ThemeProvider.tsx`:
  personal color themes. Settings > Appearance is available to all signed-in users;
  administrative tabs still require `access.manage`. `PUT /auth/theme` stores only
  the session user's validated choice in `app_users.theme` (migration 065), without
  changing security versions or revoking sessions. The session response supplies
  the theme before authenticated content paints; logout restores the original
  appearance. Original MSERP remains the default. Neutral utilities and portal
  surfaces inherit theme tokens; operational status fills retain their meaning.

## Runtime and setup

Refresher Orb is a separate repository and service. MSERP no longer hosts the
Chrome extension or issues its credentials. Migration 098 retires the old access
tables and trigger; migrations 091/092 remain only for upgrade history.

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
OPENAI_API_KEY=...
OPENAI_EXPENSE_MODEL=gpt-6-luna
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
DATATRUCK_NEW_LOADS_INTERVAL=1m
DATATRUCK_OPERATIONAL_SYNC_INTERVAL=5m
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
  `GET /auth/session`, `POST /auth/logout`, `POST /auth/password`, `PUT /auth/theme`.
- Access administration: `GET /settings/access`, `POST /settings/users`,
  `PUT /settings/users/{id}`, `POST /settings/users/{id}/revoke`,
  `POST /settings/roles`, `PUT /settings/roles/{id}`. All require `access.manage`.
- Loads: `GET /loads`, `POST /jobs/sync-loads`
- Gross board: `GET/PUT /gross-board` (`weekStart=YYYY-MM-DD` for reads),
  `GET /gross-board/loads?search=...`, and
  `GET /gross-board/balance?driverId=...&weekStart=...` for load-by-load history.
- Driver board: `GET /driver-board?weekStart=YYYY-MM-DD`, `PUT /driver-board`
  plus `GET /driver-board/history?driverIds=...&before=...` (50-event cursor pages)
  and `POST /driver-board/history/{id}/undo` (driverId, version).
  `GET/POST /driver-board/loads/{driverId}` reads/changes operational load selection,
  order and destination source; GET accepts an optional `from` date, POST requires
  board versions and the displayed plan revision.
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
- Escrow: `GET /escrows` (pagination, search, driverId, status: paid/partial/unpaid),
  `GET/PUT /escrows/settings` for the versioned new-driver default. Dedicated
  `escrow.read/create/edit/delete` permissions; collection writes use payroll permissions.
  `POST /escrows/{id}/releases` and `PUT /escrows/{id}/releases/{releaseID}`
  create/edit/cancel a versioned release using escrow.create/edit/delete respectively.
- Installment plans and charge catalog: `GET /driver-charges` (optional driverId),
  `POST /driver-charges/types`, `DELETE /driver-charges/types/{id}` (version),
  `POST /driver-charges/schedules`,
  `POST /driver-charges/schedules/preview`, `POST /driver-charges/bulk`,
  `GET /driver-charges/schedules/{id}/preview` and `/history`,
  `POST /driver-charges/confirm` and `/reopen`.
- Drivers: `GET/POST /drivers`, `GET/PUT/DELETE /drivers/{id}`
  plus `GET /drivers/setup-defaults` for the current new-driver escrow default.
- Driver assignment history: `GET /drivers/{id}/assignments` returns truck and
  dispatcher periods, current links, source notes, and whether the start is known.
- Truck assignment history: `GET /trucks/{id}/assignments` returns driver periods.
- Profile notes: `GET/POST /drivers/{id}/notes` and `/trucks/{id}/notes` use fleet
  read/write. POST accepts a client UUID `id` and `body` (1–5000 characters),
  records the authenticated author, and is idempotent for an identical retry.
  New entries are separate dated records; existing freeform notes remain undated.
- Detail lookups: `GET /investors/{id}` and `GET /dispatchers/{id}` use fleet.read.
- Investor profile history: `GET /investor-pay/history?investorId=...&page=...`
  uses payroll.read, returns up to ten complete weeks per page, and shares the
  read-only Investor Pay calculation/frozen reports. Reads never collect payments.
- New hires: `POST /integrations/fleetscope/driver-hired`, `GET /driver-intake`,
  `GET /driver-intake/{id}`, `GET /driver-directory`, `POST /driver-intake/{id}/complete`.
- Terminations: `POST /integrations/fleetscope/driver-terminated` (same HMAC/company
  boundary), version 1 `driver.terminated`, minimal driver ID/name plus terminationDate.
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
- PrePass management: `GET /prepass-fleet`, `POST /prepass-fleet/refresh`,
  `POST /prepass-fleet/change`, `POST /prepass-fleet/link`, and
  `POST /prepass-fleet/operations/{id}/verify` (explicit reviewed result).
- Expenses & Charges: `GET/POST /expenses`, `PUT/DELETE /expenses/{id}`. Roles
  receive independent View/Add/Edit/Delete access per configurable category;
  every list, summary, related-record view and write is category-scoped.
- Expense settings: `GET/POST /expense-settings`, `PUT /expense-settings/{id}`
  (category, name, payment_method, payer; updates require the current version)
  All expense settings routes require `expense_settings.manage`.
- AI expense entry: `POST /expenses/extract` (multipart text/file analysis) and
  `POST /expenses/bulk` (atomic reviewed batch creation)
- Expense and work-order modals also offer independent receipt/invoice uploads
  through `components/ReceiptUpload.tsx`, without AI extraction. Migration 127
  stores general expense files separately from work-order documents. Expense
  `GET/POST /expenses/{id}/documents` and `GET /expenses/documents/{id}` enforce
  category view/edit rights; create-only users may attach to their own entries.
  Uploads accept nonempty PDF/TXT/PNG/JPEG/WEBP up to 20 MB and deduplicate retries
  by entry, content hash and filename. Failed uploads retain the saved entry ID
  for retry without another financial write. AI review retains its source file.
  Saved receipts appear in expense details. Local fresh/migrated storage tests use
  `MSERP_EXPENSE_DOCUMENT_TEST_DATABASE_URL`; `test-receipt-uploads-e2e.mjs` uses
  the isolated review access file for real UI/API uploads and permission checks.
- Fuel: `GET /fuel-transactions`, `GET /fuel-dashboard`, `POST /jobs/sync-fuel`
- Relay review: `GET /tasks/relay-identities` (pagination/search),
  `POST /tasks/relay-identities/{id}/review` (`driverId`, `action: link|reject`).
  `frontend/app/tasks/` owns the authenticated review queue.
- Custom tasks: `GET/POST /tasks/custom`, `PUT/PATCH/DELETE /tasks/custom/{id}`.
  GET accepts pagination, search, and status (`open`, `in_process`, `completed`, `all`);
  PUT edits title/notes and PATCH sets `completed` without replacing content.
  POST/PUT accept `assignedTo` (active user UUID, empty string to clear; omitted
  on PUT preserves assignment). `GET /tasks/users` returns active user ID/name
  options. `GET /settings/system-tasks` and `PUT /settings/system-tasks/{kind}`
  manage versioned system category assignments, requiring `access.manage`.
- Unified tasks: `GET /tasks` (search/pagination/status), `GET /tasks/count`, and
  `GET /tasks/events` (authenticated SSE invalidations). List and Kanban share
  assignment privacy and include system completion history. Only user-created
  tasks allow generic edits/deletion/status changes. Offboarding completes via
  `POST /tasks/offboarding/{id}/confirm` with equipment/access/settlement checks.
- Financial reporting: `GET /financial-dashboard` (latest qualifying week, or
  `weekStart=YYYY-MM-DD`)
- Documents: `POST /irp-files`, `GET /files/{id}`

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
  without requiring fleet.read. Main updaters align right before Total gross;
  after-hours updaters start after Status. Shift labels are omitted and gross
  column borders continue through group headings. No updater changes affect commissions or rosters.
  backend/scripts/seed-dispatcher-updaters.sql imports the October 4 TODAY headings
  explicitly; it is an operator data import, not a schema migration.


- Driver Board uses migrations 045–046. It lists active managed drivers with live
  truck, phone and dispatcher assignments. Operational text, trailer and status
  are persistent per driver, not weekly snapshots; Gross Board alone supplies
  the current New York Monday–Sunday gross and miles (same entered/source and
  status/deletion rules). Filters scope cards and dispatcher totals to shown
  drivers. Plain type codes use actual truck ownership plus driver pay method: O for
  percentage owner operators, M for CPM company/own-truck drivers, %-O/M-O for
  percentage/CPM hired drivers on other investors' trucks, % for company
  percentage drivers. No ownership or pay settings change from this view.
  Driver addresses use the profile's street/city/state/postal fields. The former
  home field is absent from the UI and API; migration 080 consolidates its data.
  Five-second Status Board autosave writes changed rows atomically with optimistic versions;
  clearing retains versions, navigation flushes, conflicts retain local drafts,
  and visible idle boards refresh every 30 seconds/on focus. Drafts are never
  persisted in view memory. Tests use disposable local
  MSERP_DRIVER_BOARD_TEST_DATABASE_URL as mserp_app for fresh/migrated schemas;
  the investor E2E runner includes driver-board-e2e.mjs.
  Migration 046 records board differences (and legacy profile-home edits) transactionally,
  grouped by driver/transaction, with authenticated actor and name snapshots.
  No-op saves add no history; existing data has no invented past events. Events
  survive driver/account deletion and cannot be rewritten after their transaction.
  Historical events containing home edits remain readable but cannot be undone.
  History reads use driver_board.read; undo uses driver_board.write and creates
  an audited correction. It locks the driver, checks board versions and
  rejects any later change to affected fields, including changes back to the same
  value. Unrelated later fields remain intact. The history side panel opens per
  driver or for the shown view. All drivers, My view and named dispatcher-group
  views use per-user, per-tab viewMemory; these filters are not access restrictions.
  Double-clicking My view opens customization; there is no separate Customize button.
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
  Queue mutations lock the driver, check board versions plus source revision,
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

- Access control uses migrations 044/112. See docs/ACCESS_CONTROL.md for the
  permission matrix, sensitive data rules, migration and compatible rollback.
  One role per user, with a code-owned
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
  Settings > Users shows Online for an active, unexpired session whose browser
  session heartbeat was seen within two minutes; otherwise it shows last login in
  New York time, or No login recorded when historical data is unavailable.
  Migration 117 stores durable successful logins in user_login_activity and adds
  auth_sessions.last_seen_at. Only GET /auth/session updates presence, using the
  shell's visible-tab 30-second checks; SSE validation never extends presence.
  Users refreshes every 30 visible seconds and on focus. Activity does not change
  account versions or invalidate financial caches.
  Session issuance locks/rechecks the user version to prevent stale login races.
  Current role permissions are loaded for every request. Account and role writes
  are versioned and audited without password material; serialized updates retain
  at least one active Administrator. Drivers, Trucks, Dispatchers, Updaters and
  Investors are independent. Private details, documents, compensation and each
  payroll area require explicit grants. Lookup endpoints return limited choices.
  Viewers cannot acquire dirty state. Access revision changes clear page drafts
  and pending reads; backend field projections and file-owner checks are mandatory.
  Every authenticated method/route needs an explicit policy and coverage test. Login throttling trusts Nginx X-Real-IP only on loopback.
  Tests use disposable local MSERP_ACCESS_TEST_DATABASE_URL (_test database),
  fresh/migrated schemas as mserp_app; access-e2e.mjs runs with investor E2E.
  Pre-112 binaries are not permission-compatible rollback targets. Install the
  updated deploy helper before cutover: it stops the API before 112 and refuses
  incompatible deployment/rollback, leaving the API stopped on failed cutover. Validate with
  scripts/test-permissions-v2.mjs against review-local.mjs before deployment.

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

- Expenses & Charges category access uses migration 056. Expenses retain their
  saved category label but link to a stable category ID, so renames do not change
  history or role access. Non-administrator roles store independent View/Add/Edit/
  Delete rights per category; Administrator implicitly has all rights, including
  for newly created categories. New categories are otherwise unassigned. Creator
  identity is server-recorded for new entries; pre-056 entries have no invented
  creator. Add plus View also permits editing expenses/work orders and attaching documents
  when the recorded creator currently has the same nonempty role ID. Unknown/imported
  creators do not qualify; category Edit still permits all records. Server per-record
  canEdit drives forms; deletes, verification, vendors and service settings keep
  their separate permissions. Expense settings management is independent of category access.
  Run `frontend/scripts/test-same-role-records-e2e.mjs` against `review-local.mjs`
  with `MSERP_REVIEW_ACCESS_FILE` for real multi-user API/form checks.

- Migration 064 binds imported loads to indexed `loads.truck_id` foreign keys.
  `loads.id` is already the indexed upstream record identity; the business load
  number is not unique. `truck_unit_aliases` records current and prior unit names.
  Import/refresh resolves only unique labels, preserves an existing ID when the
  source label is unchanged, and re-resolves changed labels. Truck renames do not
  alter existing load links; label reuse never transfers linked loads. Unresolved
  labels stay NULL; blank source units may use a unique dated assignment when
  calculating a statement. Report reads do not match every truck name per load.
  Driver and investor active flags are independent; global search labels both
  roles explicitly. Investor Pay hides inactive trucks by default and excludes
  them from whole-week actions; explicit `truckId` links and profile history
  retain their statements. Revisions cover the full report for both views.
  Attribution warnings belong to affected truck cards, not unrelated investors.

- Investor Pay at /accounting/investor-pay shares driver-pay/WeeklyPayPage.tsx
  and DriverCard.tsx with Driver Pay, using the same compact expandable rows.
  Each row is one truck statement with separate investor/unit columns, followed
  by operating drivers, dispatcher and tariff. Names are frozen in new reports;
  older snapshots use their recorded driver-earnings names. Reports retain
  full load details, and deduct hired driver earnings in the charges column.
  Earnings use percentage/CPM tariffs before personal deductions; finalized
  driver load fees take precedence over later tariff changes. Truck shares use
  explicit dated percentages of Gross Board driver gross. No default percentage,
  separate dispatch-fee deduction, or blanket start cutoff is inferred.
  Migration 041 adds truck terms, recurring phases, weekly investor snapshots,
  audit records, and expense payment destinations. Configure each truck's owner,
  share, and effective Monday in Charges > Truck charges. Historical owner terms
  are explicit accounting agreements, not inferred ownership history. Known
  conflicting ownership periods require review. Migration 081 removes driver
  recurring schedules and the specifically requested type named "charge", retaining
  installment ledgers, deletion audit snapshots and finalized payroll reports.
  All non-company trucks, including owner-operated and inactive trucks, appear in
  truck charge management. No automatic driver-to-truck handoff remains. Fees can
  be selected without investor terms; owner-operated trucks without terms route
  fees to Driver Pay using existing driver tariffs. Other trucks still need explicit
  terms for Investor Pay. Never infer investor percentages. GET /truck-charges
  supplies eligibleTruckIds; write validation enforces the same company exclusion.
  Owner-only weeks stay in Driver Pay using the owner's tariff without a second
  driver-wage deduction. Mixed owner/hired-driver weeks use one investor truck
  statement and charge hired labor only. Once an investor truck-week is saved,
  its destination stays Investor Pay during corrections to preserve payments.
  Unassigned trucks retain calendar fees. Without investor terms, dated ownership
  routes recurring fees to the recorded owner's Driver Pay independently of load
  attribution. Other-driver loads and missing full-week owner assignments show
  statement review warnings instead of hiding fees; ordinary finalization requires
  resolving them. Explicit load/no-load eligibility (including insurance rates)
  remains automatic. Inactive-driver and finalized-snapshot safeguards still apply.
  Load source units or unique dated assignments for unmatched plans determine
  truck attribution. Missing matches,
  duplicate system loads, ownership conflicts and unlinked fuel need review.
  Fuel uses confirmed Relay identities, unique source Truck # prompts, diesel
  and DEF items. Fuel truck matching and PrePass import/reconciliation resolve
  unique `truck_unit_aliases`, including prior names; ambiguous aliases remain
  unallocated. Source labels and provider payloads remain intact across renames.
  Fuel uses merchant-local dates; tolls use stored truck IDs and posting
  dates, with crossing-date assignments, and include credits. Routed costs leave
  driver sources transaction by transaction;
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

- Navigation preserves view state throughout the app. Gross Board always opens
  at the current New York week on ordinary visits/reloads; explicit payroll load
  links still select their historical week. Other weekly pages retain their week.
  PageNavigation restores main/nested scroll positions after data loads;
  viewMemory.useViewState retains filters, sorting, weeks, pagination, tabs and
  expanded rows in session storage, scoped by authenticated user, page and detail
  record. RememberedDetails handles native expandable sections. Ordinary visits
  append to the navigation trail; browser Back/Forward restore visited views.
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

- Recurring fees are truck assignments (migration 081); legacy driver recurring
  writes are rejected by the API/repository and database constraint. Migration
  033's driver schedules now hold installment plans only. Viewing/autosave never
  confirms installments; payroll finalization does. Existing installment principal,
  amounts, explicit overrides, pause/reopen and audit rules remain in effect.
  Truck phases are effective Mondays, use exact amount options and calendar/loads/
  no_loads eligibility, and retain later phases. Optimistic versions and the shared
  financial lock protect saves; finalized driver or investor statements block
  changes. Charge type deletion normally requires no truck assignments; Archive
  retains used types. The one-time removal of "charge" is explicit migration data
  cleanup, not a reusable destructive deletion endpoint.
  Truck matrix selections update immediately and save through
  `PUT /truck-charges/recurring/bulk` (`updates`, at most 1000 unique cells).
  One financial lock/commit and per-cell savepoints retain valid changes when
  another truck is stale, ineligible or finalized. Ordered results identify each
  truck/type/week and optional error; unexpected database errors roll back the
  batch. The UI restores rejected cells and lists their errors, then refreshes
  truck charges once. Single-cell saves retain the existing endpoint.
  Migration 093 restores editable weekly truck-fee collections in both payroll
  pages through pay_source_collections. Source amounts are signed and bounded by
  current source plus saved carry; zero defers collection. The ledger follows the
  truck and original owner across Driver/Investor Pay routing, including excluded
  fees, without transferring debt on an ownership change. GET never collects.
  Investor Fuel/Toll now have the same partial/zero carry semantics as Driver Pay;
  pre-ledger investor overrides resolve their historical sources and are pinned
  when later collections depend on them. Genuine source credits keep their sign
  and carry unpaid credit too. Cross-week revisions reject stale saves; later
  collections must be zeroed before an earlier correction. Finalized snapshots
  remain frozen and reopening retains collections. Driver autosave uses the
  exclusive shared financial lock to serialize against truck/investor writes.
  Source input limits apply in the browser and API; expenses/escrow/installments
  retain their existing principal bounds. Computed driver earnings, CAT tickets
  and escrow releases remain fixed on both pages. Payroll undo retains current
  source revisions, including when a keyboard event precedes the receipt render.
  node scripts/test-driver-charges-e2e.mjs --pay-source-only tests both real payroll
  UIs, carry, autosave, undo and bounds. TestPaySourceCollectionsDatabase covers
  fresh/migrated schemas, legacy overrides, routing and finalized protection.
  TestDriverChargesDatabase and TestTruckRecurringDatabase use the disposable
  MSERP_DRIVER_CHARGES_TEST_DATABASE_URL as mserp_app for fresh/migrated schemas.
  test-driver-charges-e2e.mjs --truck-recurring-only checks direct driver ownership,
  classification, truck charges and payroll with the real API; test-charge-views.mjs
  checks the compact matrix, inclusion filters, navigation and narrow layouts.

- Investors own trucks independently of operating-driver assignments. A driver may
  have one investor profile, sharing live name/contact details; independent owners
  have their own details. MS Express Inc. is the fixed company owner. Migration 032
  initializes current owner-operator-assigned trucks to driver-linked investors and
  all remaining trucks to the company. This is a one-time backfill; reassignment or
  automatic driver classification never transfers ownership. New trucks default
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
  migrated schemas as mserp_app. CI always runs database checks; real-API Chromium
  E2E flows are available through its optional full_browser_tests input.

- Task assignments use migration 054. Settings > System tasks assigns each
  generated category (driver onboarding, driver offboarding, Relay review) to one
  active user. Existing and future tasks follow the current assignment; only the
  built-in Administrator role and that user can read/change them, subject to the
  usual role permissions. Unassigned system tasks are Administrator-only.
  API sessions carry the actual system-role flag, rather than inferring admin
  status from access.manage. Lists/counts, intake details/completion, pending
  driver-directory rows, Relay reviews and offboarding custom-task mutations all
  enforce visibility. Offboarding is marked by its integration link, never title
  matching. Assignment changes are versioned/audited and take effect immediately.
  `task_assignments.go` owns the shared authenticated-viewer context and settings.
  Its unscoped repository calls are trusted background/internal operations only.
  Older app releases do not enforce task privacy; rollback to them is unsafe
  once private assignments are in use.

- Unassigned custom tasks are shared by users with Tasks permissions. Any user
  with tasks.write may assign a custom task to themselves or another active user;
  assigned tasks are visible only to Administrators, the assignee and the assigning
  user. Reassigning changes that assigning user only when the assignee changes;
  editing content/completion preserves assignment. System offboarding assignment
  stays in Settings. Titles are required (200 characters max),
  notes are optional (5,000 max), and completion is reversible. Creation records
  the session user; editing content preserves completion and vice versa.
  `custom_task_repository.go` and `custom_task_handlers.go` own persistence and
  validation. Database tests use only `MSERP_CUSTOM_TASK_TEST_DATABASE_URL`, a
  disposable administrator connection with an `mserp_app` role, and verify both
  fresh schema and migrations 026/054 as that application role. Real session/API
  privacy checks use MSERP_ACCESS_TEST_DATABASE_URL in task_assignments_integration_test.go.

- FleetScope is a one-way hire/termination handoff for the configured MS Express company
  UUID. Both `FLEETSCOPE_COMPANY_ID` and `FLEETSCOPE_WEBHOOK_SECRET` must be set
  together; both absent disables receipt. Verify HMAC-SHA256 of timestamp + dot
  + exact body bytes and a five-minute delivery timestamp window. Pin the company
  independently of the signature. Receipts commit before acknowledgment and
  deduplicate by event ID plus company/driver ID; never overwrite the first
  snapshot, import the historical fleet, or propagate later profile changes.
  New hires appear as regular Drivers table rows with a small New badge and
  a corresponding Set up [name] task. The driver directory refreshes every 15 seconds
  while visible; Tasks uses server push. The driver-directory endpoint combines pending and configured drivers
  with shared search/pagination; ordinary /drivers lookups exclude pending hires.
  They stay outside managed drivers/payroll until accounting completes
  setup with a positive pay rate or explicitly links an existing record. Creation,
  assignments, and intake completion are atomic. Linking preserves existing
  profile, rates, assignments, and active status. Keep completed intake identity
  after driver deletion so retries cannot recreate it. Tests use the isolated
  `MSERP_FLEETSCOPE_TEST_DATABASE_URL`; never load production credentials for them.
  Migration 052 adds durable termination identities/receipts and cancels pending
  intakes. Only explicit completed-intake links authorize automatic deactivation;
  unlinked/deleted identities create review tasks without name matching. Receipt,
  deactivation, current truck/dispatcher release, and an Offboard task in custom_tasks
  commit atomically. Effective assignments/charge pauses use the current New York
  week; source termination dates remain for accounting review. Charge validation
  conflicts roll back the pause only and flag the task; database errors roll back
  the whole event. History, ownership, financial records and external identity links
  remain. First termination wins; retries never repeat work or recreate deleted
  tasks, and late hires cannot reopen setup. Rehire remains unsupported. See
  docs/FLEETSCOPE_TERMINATION_AGENT_PROMPT.md for the sender implementation prompt.
  Tasks uses one shared browser EventSource and one PostgreSQL LISTEN connection
  per API process with subscribers. Migration 066 emits transaction-committed
  invalidations via NOTIFY; reconnects and focus refresh the authorized feed/count.
  No browser availability polling remains. SSE uses X-Accel-Buffering: no and
  validates sessions/permissions on events and heartbeats. System task records
  retain onboarding/Relay completion times and actor-name snapshots, including
  source deletion; unknown legacy actors remain unknown. Offboarding stays in
  custom_tasks for compatibility, but requires its checklist workflow to complete.
  Migration 067 adds persisted in-process state for user tasks. List and three-column
  Open/In process/Completed Kanban mix all task sources; only user tasks
  are draggable, with equivalent complete/reopen buttons for keyboard/touch use.
  Cards and list rows keep the assignee visible; assignment/completion metadata is
  available by hovering the task title and in its detail dialog. Status PATCH accepts
  open/in_process/completed; legacy completed booleans still work.
  Tasks is third in the sidebar, with a permission-scoped incomplete count badge.
  Manual inactive drivers
  clear truck/dispatcher links; inactive trucks release their driver. Imports
  preserve inactive status and cannot reconnect inactive fleet records.
  The investor E2E runner includes offboarding-e2e.mjs; pass --offboarding-only
  for its focused signed-webhook/form/task flow using the same isolated setup.

- Gross Board load notes use migration 084 and the existing versioned slot autosave.
  Right-click or Shift+F10 on a load cell opens Add/Edit/Delete note; a dark
  corner marks notes and hover opens the compact editor. Grouped days retain one
  note per load. Notes use board.read/write, are limited to 5,000 characters,
  and clear when a load is replaced, removed or changed to a status. Omitted
  notes on older clients preserve the existing note for the same load.
  `node scripts/test-gross-board-notes.mjs` verifies the interaction with fixture
  APIs after a frontend build using NEXT_PUBLIC_API_URL=/api.

- Gross board plans persist up to 100 load slots per driver and calendar day, separately
  from imported loads and settlement accounting. Active drivers whose known setup/
  reactivation week starts by the selected week and inactive
  drivers with nondeleted saved entries in the selected Monday–Sunday week appear.
  Older entries still contribute to balance history without keeping inactive
  drivers on later empty weeks. Gross Board and
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
  load numbers require explicit selection. Entered total gross and miles
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
  Migration 027 preserves entered total gross and miles separately from
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
  Autosave starts immediately for valid edits, serializes requests and sends
  queued typing after the current request finishes; week navigation flushes pending changes. Memoized day
  cells and a stable edit callback keep typing from re-rendering the whole grid.

- Driver Pay follows Gross Board placement, including unmatched plans, all load
  statuses, and inactive drivers with entries. Driver sections start collapsed.
  Percentage fees use Gross Board driver gross times the current driver profile
  percentage; CPM fees use Gross Board entered/fallback miles times the profile tariff. Exact
  decimals round each fee to cents, and totals sum visible rounded fees. Missing
  source details stay blank; unmatched/incomplete rows and provisional totals
  are marked for review. Percentage-pay drivers have permanent Fuel and Toll rows
  with locked names and signed editable amounts; CPM drivers never show or apply
  these rows. Percentage owner-operators default to negative weekly costs, while
  company drivers default to zero. Fuel uses production Relay diesel and DEF line
  items, confirmed driver links and merchant-local dates. Tolls include
  production/legacy credits by posting date and unique historical truck
  assignment on the crossing date using New York calendar dates, never today's
  assignment or guessed nearby loads. Unmatched/ambiguous tolls remain outside payroll. Source totals
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
  Both Driver Pay and Investor Pay accept explicit `override: true` on finalize,
  for one statement or the whole week. The shared confirmation dialog previews
  issues and offers Override and finalize, bypassing future-week and all report,
  statement and load review checks (including missing fees, counted as zero).
  Permissions, current revisions, financial locks and ledger validation still
  apply. Frozen JSON and audit history retain the override and unresolved issues;
  finalized rows show Finalized* with an accessible Finalized with override label.
  No migration is needed.
  `TestPayrollOverrideDatabase` uses `MSERP_DRIVER_CHARGES_TEST_DATABASE_URL`;
  `node scripts/test-investors-e2e.mjs --payroll-override-only` exercises both
  real API/browser flows with `MSERP_INVESTOR_TEST_DATABASE_URL` and a /api build.
  Payroll load numbers deep-link to Gross Board by driver ID, service date and
  slot, scrolling to and highlighting that entry directly in the Gross Board.
  Daily loads opens only when the user chooses to edit a multi-load day. The load number
  guards against replaced/deleted slots, which display an explicit missing-entry
  notice. Pending payroll edits save before normal link navigation. The daily
  editor's Apply and return to Driver Pay action applies valid edits and awaits
  Gross Board autosave before returning to the accountant's prior view.
  Notes, load comments, and named additions/reimbursements/deductions belong to
  a driver/week in `driver_pay_weeks`, with version checks and immediate autosave
  for valid edits. Navigation flushes immediately; in-flight edits stay queued.
  Comments are keyed by date, slot, and normalized load number so replacing a
  load does not reuse its old comment. DataTruck stop ordering selects the first
  pickup and final delivery; trip `mile`/`empty_mile` supply loaded/deadhead miles.
  These source fields survive sync serialization. Historical payloads may need
  Refresh load details; that action never imports new loads or changes assignments.
  Database tests use only disposable `MSERP_DRIVER_PAY_TEST_DATABASE_URL` and
  verify fresh schema and migrations 029-030 as `mserp_app`.

- Shop layover bonuses are separate from deduction carry: migration 086 adds a
  $100/day default, configurable under Settings > Driver Pay through
  GET/PUT /settings/driver-pay (access.manage, optimistic version). Count distinct
  nondeleted Gross Board SHOP dates in the selected Monday–Sunday week, including
  Shop-only weeks; never use the persistent Status Board status. Positive layovers
  stay on Driver Pay, including team statements (once per day), never Investor Pay.
  driver_pay_weeks.layover_excluded_dates persists per-day removal; omitted fields
  preserve prior exclusions. Removals autosave with payroll versions and support
  page-session undo. No bonus or exclusion carries into another week. Rate changes
  affect open statements; finalized JSON snapshots retain their amounts/removals.
  TestDriverPayLayoverDatabase covers fresh/migrated schemas as mserp_app, and
  node scripts/test-layover-views.mjs checks the settings/removal UI.

- Migration 053 makes reduced Driver Pay deductions carry forward. Fuel/Toll
  source totals remain separate from opening unpaid balances; collection snapshots
  in driver_pay_cost_collections are written atomically on payroll save/finalize,
  never on reads. Existing cost overrides resolve against routed historical source
  reports or frozen settlements and are pinned when a later collection uses them.
  Unpaid balances remain visible through empty weeks and later tariff changes.
  CPM drivers do not acquire new fuel/toll obligations, but retain existing debt.
  Cost revisions reject stale cross-week saves. Before correcting an earlier
  amount, later saved deductions must be zeroed (and finalized weeks reopened),
  then reapplied; prior collections cannot silently change a later saved payment.
  Recurring deduction occurrences retain a base amount separately from carry.
  Earlier recurring deductions can be reduced or zeroed with carry enabled even
  when later open/reopened weeks have saved payments. Those payments retain their
  amounts, and unpaid balances carry onward. Later finalized weeks must be reopened;
  later waivers and corrections that could invalidate payments remain protected.
  Reduced/zero amounts carry by default, even after pause/end or in ineligible
  weeks; those weeks add no new base charge. Right-click/Shift+F10 on a recurring
  row offers an explicit audited this-week-only reduction (waiveRemainder), with
  a small This week marker. It does not change future schedule rates. Reset removes
  that exception. Fuel/Toll have no waiver action. Expense balances continue their
  existing principal-based behavior. Freeform adjustments have no separate unpaid
  principal, and negative net pay itself never creates a second debt. Investor Pay
  keeps its existing truck-cost rules. Tests cover fresh/migrated schemas and the
  charge E2E right-click/keyboard workflow.

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
  Additional interval jobs run immediately at startup: new-load discovery every
  minute, operational refresh every five minutes. Both interval environment
  settings accept 1m–24h and obey SCHEDULED_SYNCS_ENABLED. Discovery has no date
  cutoff and saves before reconciliation, so refresh failures do not hide new
  loads. Discovery serializes separately from full/operational/payroll refreshes;
  the shared HTTP gate prioritizes discovery between pages, with four-second
  request spacing and a shared Retry-After cooldown (including waits over 60s).
  Without Retry-After, a 429 pauses the client for at least a minute. The gate is
  process-local: run only one scheduled API instance and avoid concurrent operator
  recovery processes using the same token.
  Operational refresh selects existing IDs once from cached pickup/delivery
  actual/appointment dates: seven calendar days back through three days ahead,
  plus recent creations with incomplete dates and active drivers' selected current
  loads regardless of age. Today's date is New York; comparisons retain DataTruck's
  encoded UTC schedule dates. Current IDs go first; batches use array-valued
  is_in filters, at most 25 IDs, all statuses. Changed upstream dates that move
  a previously out-of-window, unselected load into the window are caught by the
  morning reconciliation. Repeated operational runs never overlap or accumulate
  behind full/payroll refreshes. Existing-load refreshes update source fields and
  identity links without changing fleet profiles/assignments. Morning scans can
  recover missing records below their starting watermark; only discovery advances
  it. Gross Board still owns planning and Status Board still requires its plans.
  A selected plan with no original stops gains a pickup/delivery source on reads
  after an exact unique import, according to its existing dispatch status. Reads
  do not write history or advance loads. Explicit manual destinations (including
  a saved manual blank), changed identities and ambiguous matches stay protected.
- New DataTruck imports never create managed `drivers` or `trucks`. They retain the
  upstream driver name and truck unit on the load, link only to an existing
  unambiguous fleet record, and create a current assignment only when both
  links resolve. Add fleet master records through the management UI first.
- Person names are title-cased for display and normalized for matching. Truck
  unit numbers are trimmed/collapsed and uppercased. Use the helpers in
  `backend/internal/repository/naming.go` rather than duplicating this logic.
- Assignment changes in driver, truck, and dispatcher forms accept any effective date,
  including future New York dates. The legacy assignmentWeek API field
  defaults to the current week for older clients. Migration 047 makes dispatcher
  history honor this boundary; truck writes use the same boundary. Same-week
  corrections retain zero-length periods without overlaps. Migration 062 permits
  corrections across later assignment weeks, retaining superseded periods at zero
  length. Migration 139 removes the roster Monday check and protects finalized
  payroll starting with the week containing the effective date. Finalized driver
  payroll from that week onward blocks changes,
  including displaced drivers; affected finalized investor truck payroll is also
  protected. Fleet writes share the payroll finalization lock. Unchanged links never
  reopen periods. Gross Board and unsaved Driver Pay projections exclude drivers
  before drivers.roster_start_week. Ordinary setup and reactivation save the date
  independently of old assignment history; migration 062 backfills only known
  initial starts. Unknown legacy starts and explicitly saved historical work
  remain visible. Current fleet links still
  represent the latest selection; weekly reports use the effective history.
  Gross Board exposes daily assignments and groups each driver under every
  dispatcher active during that week; other groups' days show Changed dispatcher.
  Daily loads and dispatcher totals count once. Rate balances remain per driver,
  counted once under the latest visible dispatcher. Driver Pay retains one
  complete statement, shows all weekly dispatchers and matches any of them in
  filters. Team membership and recurring-charge schedules remain weekly.
  Tests cover fresh/migrated schemas and assignment-week-e2e.mjs (run with
  test-investors-e2e.mjs --assignment-week-only). Migration 046
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
  Owner bills the selected truck's owner. Configured truck terms route
  expenses to Investor Pay or owner-only Driver Pay; unconfigured driver-linked
  owners retain their existing Driver Pay behavior. Independent investors never bill the operating
  driver. Current driver/truck selections fill their counterpart and default to
  Company for company trucks or Truck Owner for privately owned trucks, regardless
  of the operating driver's pay type. When the linked driver owns that truck,
  default to Driver and omit the duplicate Truck Owner choice. There is no
  unrelated investor picker. Dates do not clear the selected owner; editing an
  existing entry preserves its saved responsibility until explicitly changed.
  Ownership changes never transfer an existing expense debt.
  Historical Truck Owner expenses are settled without inferring a past owner.
  Personal charges use charge_driver_id; other linked expenses remain separate.
- Driver-covered expenses (coveredBy = Driver) require a linked driver and a
  nonnegative total. Penalties is an expense category. Migration 036 marks all
  pre-existing driver-covered expenses as settled with zero remaining balance.
  Driver Pay and Investor Pay include expenses through the following Wednesday
  (expense_date < week_start + 10), including weeks without loads. Monday–Wednesday
  expenses can therefore enter the preceding week's statement. Older unpaid
  balances still carry forward; fuel, tolls, escrow and recurring charges retain
  their existing weekly windows. Driver history includes the earlier eligible week.
  Normal payroll autosaves include
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
  `node scripts/test-investors-e2e.mjs --expense-window-only` verifies both payroll
  UIs and autosave against the real API/database using the investor test database.

- Escrow is a standalone Accounting feature at /accounting/escrow and a Driver
  detail tab, with Required, Balance, Still owed, funding status filters and history.
  Its new-driver default lives on Escrow, independently of Expenses & Charges.
  The directory defaults to active drivers; includeInactive=true includes inactive
  and deleted driver accounts. Profile tabs explicitly include inactive accounts.
  GET /escrows returns summary totals over all filtered records, independent of
  pagination. Compact cards show Escrow balance, Still owed and Fully funded drivers, with
  only a value and title per card. Paid/total driver counts always include only
  active drivers, even with Show inactive enabled; monetary totals follow filters.
  The default sits at the toolbar right edge: right-click (or keyboard/click) Edit,
  then save inline with Enter or cancel with Escape. Escrow and profile expense
  histories use the shared modal PaymentHistoryPanel without expanding rows.
  Inactive drivers do not enter Driver Pay through automatic charges, expenses,
  escrow or carried fuel/tolls alone; actual weekly loads, saved payroll and frozen
  settlements remain available. Fully paid escrow alone does not create empty weeks.
  Owner-truck fee routing enforces the same driver activity boundary even when
  an active truck retains an old open assignment. No-load inactive owners with
  explicit truck terms stay in Investor Pay; ownership does not reactivate driving.
  Team membership alone cannot bypass the driver's inactive status.
  Migration 059 transfers system and named driver escrow expenses and their
  payments to driver_escrows/driver_escrow_payments, preserving IDs, exact amounts,
  actors, source snapshots, and frozen payroll/audit JSON. Settled legacy records
  retain paid credit. Pre-September-28 collections display as Previously paid.
  Remaining deductions begin the week of September 28, 2026, or a later known
  hire week. Manual/FleetScope setup atomically creates an escrow account with
  zero paid and the selected/default target; later profile edits preserve it.
  Expenses no longer contain escrow records and reject new driver escrow entry.
  Safety is no longer protected for escrow. GET /drivers/setup-defaults retains
  the fleet-readable onboarding default without requiring escrow management.
  Payroll retains its expenseDeductions wire array for compatible rendering;
  source=escrow identifies standalone ledger entries. Reads never collect money;
  autosave/finalize use the existing explicit amount, zero-deferral and carry
  rules. Cross-week versions and reserved payments prevent double collection;
  database guards protect finalized weeks. Reopening retains paid collections.
  Migration 060 adds driver_escrow_releases and actor-stamped release events.
  Release is a compact anchored amount/week form in both directory and profile;
  History offers edit/cancel for current/future unfinalized releases. Past weeks
  cannot be selected or altered; the API and database use the New York week.
  Client-generated release UUIDs plus escrow/release versions prevent duplicate
  submissions and stale writes. Driver/advisory locks serialize against payroll
  and finalization. Released amounts reserve collected funds, including scheduled
  future credits; Balance = collected minus noncancelled releases and Still owed
  = required minus balance. Lifetime collected/released totals stay in History.
  Migration 061 reopens released amounts for collection from the following week;
  payroll's normal editable deductions allow partial repayments until fully funded.
  Database and read-time capacity checks reserve later saved repayments and reject
  overcollection, same-week replenishment and release edits/cancellations that
  would leave already saved repayments overfunding escrow. Funding checks
  prevent spending later collections or lowering a collection below reserved funds.
  Each release is one fixed positive autoCharges entry with source escrow-release:ID
  in its selected Driver Pay week, retained in frozen settlements/history. Payroll
  cannot edit it; the credit never repeats, while unpaid replenishment carries as
  an escrow deduction into later weeks. Explicit release
  recipients appear even when inactive, but only in their selected release week
  unless other actual payroll history/work exists. Credits stay on the personal
  driver statement, never Investor Pay. Once releases exist, use a forward fix
  rather than an older binary that cannot display those credits.
  Deleting a driver retains their escrow/name snapshot. Separate escrow.read and
  escrow.edit permissions control the default, while payroll writes
  retain payroll permissions. Older binaries cannot display migrated escrow and
  cannot create expense-backed escrow; use a forward fix after this migration.
  Tests use MSERP_DRIVER_PAY_TEST_DATABASE_URL as mserp_app for fresh/migrated
  schemas; test-driver-charges-e2e.mjs includes escrow-e2e.mjs.

- Maintenance and general Expenses have separate entry/directory flows. General
  page/search reads send `scope=general`; payroll and legacy API reads retain the
  complete ledger. A stable Maintenance category ID in maintenance_configuration
  preserves existing category View/Create/Edit/Delete access and financial routing.
  Maintenance access/options are scoped server-side; reporting-type administration
  and fleet schedule defaults require built-in Administrator status.
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

- Driver Pay and Investor Pay share expense MM/DD dates and the in-page
  ExpenseEditModal. GET /expenses/{id} requires category View; updates retain
  category Edit/Create rules and optionally check updatedAt for stale edits.
  Opening from payroll waits for pending payroll edits to save; saving reloads
  the current report. Finalized financial guards remain authoritative.
- Migration 089 makes EFS a permanent active payment method (no rename/archive/delete).
  Fee-aware expense requests send baseAmount; the server computes $3 for each
  started $500 using exact arithmetic (zero has no fee, negatives are rejected).
  amount remains the fee-inclusive ledger total; baseAmount/paymentFee preserve
  the breakdown for editing without compounding. Manual and AI batch forms preview
  fees. Existing historical totals are unchanged by migration; saving an EFS entry
  through the fee-aware form applies the displayed fee. Legacy requests retain
  their total-amount contract. node scripts/test-payroll-expenses.mjs covers both
  payroll pages, fee boundaries, refresh and view-only access.
  Payroll also adds the missing EFS fee to unsettled legacy/imported Driver/Truck
  Owner entries with no baseAmount or recorded paymentFee, leaving their source
  amounts unchanged. Shared settlement totals drive Driver/Investor Pay, save
  limits, partial-payment carry and Expenses remaining balances. Existing explicit
  fees are never added twice; company/driver-only categories and assumed-paid
  history stay excluded. Saved deductions and finalized snapshots retain their
  amounts. Migration 109 prevents later payment/category metadata edits from
  changing a collected expense's settlement total. TestExpensePaymentsDatabase
  covers all three routes (driver, investor, owner-driver) on fresh/upgraded schemas.

- Migration 095 seeds the requested Maintenance default names and stores optional
  `expenses.cost_breakdown` JSONB rows (name/decimal amount snapshots). Manual,
  payroll edit and AI batch forms use proper category-specific Name dropdowns with
  Custom name entry. Breakdown sums must exactly equal the entered base amount
  before the EFS fee; browser/API/database enforce decimal precision and a
  100-row limit. Omitted breakdowns preserve existing rows on legacy updates;
  an explicit empty array clears them. Reports can group saved names using
  jsonb_array_elements without parsing descriptions. Fresh/migrated database
  tests exercise application-role reads/writes, atomic batches and EFS fees.
  `test-expense-entry-e2e.mjs` uses the isolated review access file for real
  API/browser create, reload and edit checks. `MSERP_OPENAI_LIVE_TEST=1` opts
  into synthetic model checks; previews also require
  `MSERP_REVIEW_OPENAI_ENABLED=1` and server OPENAI_API_KEY for live extraction.
- Expense type is labeled Expense type in manual and AI review forms, required after Category.
  Description sits immediately below it and is the primary Expenses directory text;
  blank historical descriptions fall back to the saved type/category. Work-order notes
  appear as Description beside service types in entry and History, and expense reads
  expose them separately as read-only workOrderDescription without replacing saved
  expense descriptions. Only explicit description edits in the work-order form update
  both note and invoice text. Catalog values, service codes and historical rows stay intact.
  Migration 106 pins driver-only entry behavior to the Penalties category UUID
  (`expense_settings.driver_only`), retaining it across renames. Penalty forms
  require a linked driver and omit responsibility/payment-method/paid-by inputs.
  Single, batch and update APIs enforce Driver responsibility, clear owner/payer
  fields and apply no EFS fee. Existing ledger rows and financial locks are retained.
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
  attachment up to 20 MB. OpenAI Luna suggestions never write directly: one detected
  transaction fills the add-expense form, while multiple transactions are
  returned for editable table review and atomic batch creation. High-confidence
  fleet matches populate foreign keys while raw extracted names and units remain
  available when unmatched. Extraction uses the Responses API with strict JSON
  schema, server-only OPENAI_API_KEY and OPENAI_EXPENSE_MODEL (gpt-6-luna).
  Receipt line costs use catalog names when their service meaning matches;
  unresolved amounts stay available for manual review.
  Expenses no longer read, require, or write a company field, including AI entry
  and CSV imports. Migration 087 defaults the legacy column during the first
  deployment; migration 088 drops the column and its historical values after that
  compatible API is live. Rollbacks must retain a company-independent API (087 or
  later). Cached clients may still send company; it is ignored.
  Column filters support unitNumber, driverName, paymentType, paidBy, and coveredBy.
  options.filters lists historical values from visible categories, independently
  of active filters; entry suggestions remain separate. Save/verification notices
  reserve space beside the action buttons, and verification refreshes retain rows.
  Add entry has no field hints or AI
  frame. The AI textarea accepts dropped files and pasted images; Browse sits
  beside Analyze. Manual and AI batch forms share responsibility controls.
  Paid by is a dropdown of active MSERP usernames from expenses options.paidBy,
  defaulting to the signed-in user for manual and extracted new entries. Existing
  payer text is preserved on edits; expense-setting payer suggestions no longer
  populate this dropdown.
  `node scripts/test-expense-views.mjs` checks ownership, payer defaults, AI file
  drops, single/batch submissions and narrow layouts against fixture APIs.
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
  Driver Pay fuel deductions include both diesel and DEF fuel line items. Driver
  Pay toll weeks use PrePass posting dates to reconcile with the Tolls report;
  each toll is still attributed through the truck assignment on its crossing date.
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

- Follow `docs/UI_DESIGN_SPECS.md` for all UI work: shared headers without
  introductions, 32px ordinary rows/controls, 13px body text, 12px labels,
  16px icons (20px page icons), 16px card padding and 8px radii. The shell and
  shared modal portals use `.mserp-ui`; reuse ManagementUI and MetricCard.
- `docs/SETTLEMENT_RULES.md` records the customer-confirmed gross basis. Original
  minus driver gross is a dispatch rate-adjustment balance, not automatically
  anyone's earnings. Percentage driver/investor pay uses Gross Board driver
  gross; CPM still uses miles. Never merge personal and investor statements or
  deduct the dispatch difference again. Missing truck terms are configuration
  gaps (`setupRequired` in Investor Pay), not zero-valued statements.

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
node scripts/test-payroll-grid.mjs
node scripts/test-board-grid.mjs
node scripts/test-investors-e2e.mjs
node scripts/test-investors-e2e.mjs --profiles-only
# Same isolated database safety requirement, using MSERP_DRIVER_CHARGES_TEST_DATABASE_URL
node scripts/test-driver-charges-e2e.mjs
# Focused automatic ownership, truck fees, and payroll browser checks
node scripts/test-driver-charges-e2e.mjs --truck-recurring-only
# Browser view regressions use isolated API fixtures, with no database needed.
node scripts/test-charge-views.mjs
node scripts/test-task-board-views.mjs
node scripts/test-theme-views.mjs
node scripts/test-prepass-views.mjs
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

### Local synthetic review

Build frontend with NEXT_PUBLIC_API_URL=/api, then run from frontend/:
`node scripts/review-local.mjs`. Set MSERP_REVIEW_TEST_DATABASE_URL to a local
PostgreSQL database whose name ends in _test; PSQL_PATH optionally locates psql.
The runner creates a temporary schema with synthetic escrow/expense examples,
starts a real API on 18570 and static/proxy frontend on 13570, and disables
scheduled syncs and telemetry. Login details and process IDs are written only
into a temporary review-access.json file. Keep the process alive for review;
Ctrl+C stops its servers and drops that schema. Production data is never loaded.
`MSERP_REVIEW_API_PORT` and `MSERP_REVIEW_PORT` override these ports when another
local review is already running.

### CI/CD and access model

- `.github/workflows/ci-deploy.yml` runs backend/database tests, vet, lint,
  calculation/autosave checks and production builds on pull requests and pushes.
  The four full browser regression suites and Chromium installation are opt-in
  through workflow_dispatch's full_browser_tests input (default false).
  Go and npm dependency/build caches reduce repeat setup time.
  Every push or merge to `main` automatically builds and deploys production;
  `workflow_dispatch` redeploys the selected `main` commit. App updates should
  normally go through a branch and PR, then be verified through the resulting
  `main` Actions run.
- GitHub Actions builds the Linux API and static frontend, packages numbered SQL
  migrations, uploads the checksum-verified artifact, and connects as the
  restricted `mserp-deploy` user. Repository secrets are
  `MSERP_DEPLOY_SSH_KEY` and `MSERP_DEPLOY_KNOWN_HOSTS`; never print or replace
  them during routine work.
- When full_browser_tests is enabled, CI installs Chromium with a five-minute timeout, using
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


## October 2026 reporting performance

- Migration 068 stores fuel `purchased_on` (merchant-local date), validated reporting
  timezone and normalized truck-unit evidence. A trigger maintains them for imports,
  corrections and older binaries; source timestamps/prompts remain unchanged. Weekly
  reads use the date index instead of per-row timezone-catalog and JSON processing.
- DataTruck discovery, refresh and reconciliation are source-only. They may resolve
  existing fleet IDs onto a load but never create/enrich drivers or dispatchers, or
  create/change truck/dispatcher assignments. Only managed fleet workflows own those.
  Relay still creates review identities, never drivers.
- Payroll reads bulk-load deductions and truck routing. Shared inputs are memoized
  only inside the same read transaction. Histories share one repeatable-read snapshot;
  finalized driver history reads frozen statements directly. Whole-week finalization
  calculates the post-write fleet once before atomically freezing the selected rows.
- Payroll's bounded process cache (32 reports, 8 MiB, five minutes) uses migration
  090's transactional per-table revisions, report kind/week and current charge week.
  Statement triggers cover application tables except auth_sessions and
  schema_migrations; zero-row writes do not invalidate. Committed source writes
  invalidate atomically; rollbacks/session activity retain cached reports. New tables
  must add tracking triggers/revision rows; until covered, reads conservatively fall
  back to MVCC snapshot keys. TTL is only a memory limit. Writers never use this cache or reuse read memos across
  mutation phases. Permission checks remain per request; cached JSON is cloned.
- MSERP pool connections use jit=off. HTTP Server-Timing and structured performance
  logs include route templates, duration, query count/time, pool acquisition time,
  status and bytes, never SQL arguments or report contents. Streaming remains supported.
  The router shutdown hook closes task-event streams before the server drains ordinary
  requests; keep it registered with http.Server.RegisterOnShutdown.
- Status Board GET accepts compact=1, using a shared plan dictionary and integer
  references. Different snapshots with the same plan ID remain separate. The frontend
  can expand it to the normal domain contract. The frontend uses the legacy transport
  by default: production measurement showed the dictionary increased gzip bytes.
- Browser GETs share in-flight work only, with independent abortable consumers and
  cloned results. Mutations clear reuse, logout aborts reads. Payroll/history/search
  cleanup cancels obsolete requests; unsaved drafts retain existing protections.
- Only hashed /_next/static/ assets receive immutable caching. HTML freshness and
  security headers stay enforced. Install Nginx changes separately per deploy/README.md.
- Read-only diagnostics: backend/cmd/perf-audit, MSERP_DIAGNOSTIC_DATABASE_URL,
  optional MSERP_DIAGNOSTIC_ROLE. Modes driver/investor/driver-history/investor-history/
  fuel/financial/board/gross; --week YYYY-MM-DD, --runs 1..5, --jit on|off. Output is
  sanitized timings/counts/sizes/hashes. It never writes financial data.
- Run node scripts/test-read-requests.mjs for cancellation/deduplication and compact
  board transport regression coverage, in addition to the normal validation suite.


## Driver status and escrow termination reviews (migration 069)

- Drivers expose `status` (`active`, `vacation`, `home`, `terminated`) and an optional
  `terminationDate`. Only terminated maps to active=false. A compatibility trigger
  synchronizes legacy active writes; nonterminated named statuses retain fleet links.
  Termination keeps the existing charge pause/assignment release workflow. FleetScope
  records its source termination date. Legacy inactive records become terminated;
  known source dates survive, otherwise migration day minus 30 days is the explicitly
  approved transition date. Each termination has a durable review identity.
- Escrow `group=active|terminated` tabs have independent collection/release statuses.
  Active includes Vacation and Home. API status codes are paid/partial/unpaid and
  released/partially_released/not_released. Release status compares actual released
  funds and held balances. An explicitly completed released review for the current
  termination also marks a zero balance Released, allowing approved historical
  migration clearances without fabricated transactions. Unreviewed zero balances
  are not automatically released; a later positive balance or new termination
  cannot inherit that zero-balance clearance.
- An internal worker runs at startup and every minute, independently of external
  sync settings, to materialize escrow reviews on termination date + 30 New York
  calendar days. Downtime catches up idempotently; system task notifications update
  open browsers. No task is shown before its due date.
- Settings > System tasks supports multiple escrow assignees via assigneeIds.
  assigneeId remains the compatibility primary; all assignees share a single task.
  Active assignees and Administrators can see it, with normal Tasks/Escrow permissions.
  GET /tasks/escrow/{id} returns review balances; POST /tasks/escrow/{id}/complete
  derives released/partially_released/kept from actual releases and held funds;
  caller decisions are compatibility inputs only. A reason is required only for
  partial releases or retained funds (migration 071), along with every escrow
  version. Driver/escrow locks validate actual releases and retain actor/balance
  snapshots. Generic task edits cannot complete system reviews.
- PUT /escrows/{id}/opening edits Previously paid during the transition with
  escrow.edit, current version and correction reason. Existing funding/collection
  guards remain authoritative; each change has an audit event. Saved payroll
  payments and finalized snapshots are not replaced by opening corrections.
- TestEscrowTerminationWorkflowDatabase uses disposable local
  MSERP_DRIVER_PAY_TEST_DATABASE_URL, fresh and migrated schemas, as mserp_app.
  `node scripts/test-investors-e2e.mjs --escrow-only` exercises real worker/SSE,
  multi-assignee privacy, opening corrections and full/partial/kept decisions.
  MSERP_E2E_API_PORT and MSERP_E2E_WEB_PORT optionally isolate this browser runner.

Migration 070 also publishes task invalidations when a termination review date is
corrected, so an open Tasks page removes reviews whose due date moves forward.

## Working start dates and paginated payroll

- Payroll latency diagnostics and the unresolved 300 ms p95 release gate are
  documented in `docs/PAYROLL_PERFORMANCE.md`. `TestPayrollPerformance` is opt-in
  with `MSERP_PAYROLL_PERF=1` and a disposable local `_test` database. Never use
  production payroll writes for benchmarks. `perf-audit -explain` is read-only,
  bounded to three seconds for its plan inspection, and removes plan expressions.
- Payroll calculations share full-week and conservative selected dependency
  scopes. Dated relationships remain historical; load/fuel source edges need
  only the requested week plus unfrozen legacy weeks without collection records.
  Outstanding balances are never discarded by a calendar cutoff. Save validation
  bypasses the report cache and retains the financial locks. Query planning uses
  transaction-local custom plans for the different full/selected cardinalities.
- Paginated payroll GET accepts `detailIds` (up to 100 comma-separated UUIDs).
  With `summary=1`, requested rows on the returned page keep full details from
  the same snapshot. Remembered expansions use this option to avoid a second GET.
  Existing whole-week revision and pagination metadata contracts remain intact.
  Explicit unrelated operational cache dependencies are maintained in
  `payroll_cache.go`; unknown tables still invalidate conservatively.

- New-driver setup asks when the driver started working. Creation derives the
  assignment/roster date from hireDate; truck and dispatcher assignments share
  that date, and escrow starts on the actual selected date. Existing profile
  assignment edits keep the explicit Assignment effective date control.
- Profile hire-date corrections atomically move the matching initial roster,
  truck/dispatcher boundaries and uncollected setup escrow (actual date). Later
  transfers, reactivation periods, independent escrow dates and truck ownership
  stay explicit. Conflicting assignments, crossed work/team/payroll periods,
  finalized statements and collected setup escrow require review before saving.
  `driver_hire_start.go` records the correction in access_audit; database coverage
  uses `TestDriverHireStartDatabase` with the assignment-history test database.
- PATCH /drivers/{id}/status accepts status, updatedAt, assignmentWeek and, for
  termination, terminationDate/chargePauseWeek. It updates status only under the
  managed driver locks, rejects stale profiles, and retains the existing charge
  pause, assignment release and escrow-review workflow. Directory context menus
  and profile Change status share DriverStatusDialog.
- GET /driver-pay and /investor-pay accept summary=1 with pagination for compact
  collapsed-row totals from the same authoritative report. Shared LazyDriverCard
  fetches a single statement when expanded and reuses it until report reload.
  Aborted/obsolete responses cannot overwrite a refreshed report or pending edits.
  Full legacy reads, exports, settlement review and save validation retain details.
  Run test-payroll-lazy.mjs and test-payroll-expenses.mjs for both pages.
- GET /driver-pay and /investor-pay accept page/pageSize, search, dispatcherId
  (including __unassigned), and statementId. Paging runs after complete financial
  routing/carry/frozen overlays and uses the snapshot cache; only the selected
  rows are transferred/rendered. pagination includes totals across all matching
  rows, complete dispatcher choices and week-wide finalized counts. Legacy reads
  remain complete reports. Whole-week actions fetch/review the full report and
  ignore filters/pages. Unsaved edits retain navigation protections.
- Both payroll statements show entered/fallback total gross and miles, with
  DataTruck comparison values and red mismatches. POST /driver-pay/accept-system
  and /investor-pay/accept-system require payroll.write and a driver/date/slot,
  board version, source record ID and reviewed source value. The optional `field`
  selects `originalRate` or `totalMiles`; omitted fields retain legacy both-value
  acceptance. Menus show only the selected value's before/after numbers. They update
  only the selected Gross Board input, reject stale board/source values and
  finalized related settlements, and preserve driver gross and plan identity.
  Migration 072 records actor-owned source acceptance receipts. With
  `undoReceipt=1`, acceptance returns `undoId`; POST to the same payroll resource's
  `/undo-system` with `id` restores exact prior inputs only once, while the board
  version is unchanged and related settlements remain open. Consecutive receipts
  by the same actor can be undone in reverse order without bypassing intervening
  board changes. Legacy acceptance
  responses remain 204. Ctrl+Z/Cmd+Z uses an in-memory page-session stack for
  payroll edits and source acceptance; saves retain current concurrency versions,
  and undo waits for in-flight saves. Reload/filter/week changes clear the stack.
  Finalization and reopening keep their explicit audited workflows. Arrow keys
  navigate editable and read-only payroll cells, and Ctrl+C copies the selected
  cell. One click selects the cell without highlighting text; typing replaces its
  value, while F2 or double-click edits existing text with a caret. Sourced amount
  cells keep an uncommitted typing draft until Enter or blur, then display the
  source sign and two decimals. Escape/Ctrl+Z cancels that draft. Recurring,
  installment, Fuel/Toll and expense deductions retain their source direction;
  payroll/API writes reject amounts above the authoritative available amount
  (including carry). Source credits retain their credit direction. Manual
  adjustments remain separate; existing manual deductions require an explicit
  plus sign to become reimbursements.
  Charges and load rows share 32px geometry and explicit borders through totals.
- node scripts/test-investors-e2e.mjs --payroll-workflows-only covers corrections,
  pagination/search, working start dates, quick status changes, global/Gross Board
  search, all seven themes and narrow layouts against an isolated API/database.

## Driver teams (migration 078)

- Configure team membership from the secondary driver's Add/Edit or FleetScope
  setup form: Solo driver / Team driver toggle and a truck selection. Team mode
  hides compensation and dispatcher fields, inherits the main driver's dispatcher,
  and creates zero compensation/escrow for a new team member. Driver team cards
  appear only on main profiles with a teammate. Changes use an effective Monday
  and optimistic versions. GET `/drivers/team-trucks?weekStart=...` supplies dated,
  unambiguous main-driver truck choices. GET/PUT `/drivers/{id}/team`
  use drivers.read/driver_pay.edit. Dispatch, tariff, escrow and recurring charges
  belong to the main driver; weekly Driver Pay has one statement showing both names.
  Team members cannot have recurring phases or independent loads/truck assignments
  during the team period. Prior solo history and individual identities remain.
- `payroll_driver_at` routes confirmed production Relay diesel/DEF by merchant-local
  purchase date, preserving source driver IDs and investor truck cost routing.
  Existing personal expenses/installments use the main statement with separate
  principal ledgers. Finalized statements and later saved cost weeks guard changes;
  termination requires an explicit team end-week review. See `docs/DRIVER_TEAMS.md`.
- `TestDriverTeamsDatabase` uses the existing disposable driver-pay test database.
  `node scripts/test-investors-e2e.mjs --teams-only` validates the real API/browser.

## Driver service setup (migration 079)

- Manual Add driver and FleetScope onboarding share `drivers/DriverServices.tsx`: draft Add after saving / Remind me later choices, then durable result cards. Driver Overview and system tasks resume the same setup.
- `repository/driver_services.go`, `httpapi/driver_service_handlers.go` and migration 079 persist service intent atomically with driver creation/completion. A startup/five-second worker processes queued requests only; failures never undo the driver. Existing vendor receipts prevent uncertain replay. Relay account added and policy enabled are separate statuses; PINs are never queued/stored.
- GET/POST `/driver-services/{id}` require drivers.read; writes additionally require the selected service’s create permission. New `relay_setup` / `weighmytruck_setup` task kinds inherit onboarding assignees and support versioned follow-up dates, automatic completion and durable history. See `docs/integrations/DRIVER_SERVICE_SETUP.md`.
- `TestRelayDriverDatabase` covers fresh/migrated service workflow; `node scripts/test-driver-services-views.mjs` covers both modals and task resumption with fixture APIs.

## Guided driver setup (migration 080)

- Initialize each `?setup=` intake link once per navigation. Routine session
  refreshes replace permission objects every 30 seconds and on focus/visibility;
  they must never reinitialize the open form or clear its draft/service choices.
- Manual Add and FleetScope setup use `drivers/DriverSetup.tsx`: information,
  assignments (pay and escrow), optional new-truck details, services, and finalize
  (start date/status). New truck is the solo default; existing/unassigned and team
  selections skip that step. Team trucks still require a dated main-driver link.
- `DriverInput.newTruck` and `newTruckOwnedByDriver` are creation-only. Both entry
  points create the driver, optional owner/truck, assignment, escrow and service
  requests in one transaction. Explicit driver ownership creates a linked investor;
  otherwise the selected existing owner applies. Truck assignments use the hire
  date's Monday. Existing driver intake links do not change profiles/assignments.
- Service card titles link to their management pages. Reminders are offered only
  during setup and its saved-results modal, not on Driver Overview.
- CDL upload UI, browser request and POST route are removed. Manual CDL fields and
  existing document downloads remain. No stored documents are deleted.
- Migration 080 fills empty city/state from recognized legacy home entries without
  changing populated addresses. Ambiguous/conflicting values become system profile
  notes. Original home columns remain solely as migration evidence/rollback data;
  application reads/writes and home-version API fields are removed. Old history is
  retained and cannot restore the retired field.
- `TestDriverSetupDatabase` uses the disposable Relay test database for fresh and
  migrated address/atomic truck setup. `test-driver-services-views.mjs` covers the
  wizard with fixtures; `test-investors-e2e.mjs --driver-setup-only`, `--teams-only`
  and `--board-only` cover the real API/database and affected board behavior.

## Automatic driver classification and direct ownership (migration 081)

- Driver employmentType is read-only: company, owner_operator, investor_driver,
  or unassigned. The UI labels investor_driver "Investor truck driver". It derives
  from the assigned truck and its owner; team members use the main driver's truck.
  Ownership/assignment changes take effect on the next read. No form selects type,
  and legacy isOwnerOperator input is ignored. Driver Board's compact pay/type codes
  remain separate. Payroll reads derive dated relationships; frozen reports remain
  unchanged. The legacy database boolean is no longer authoritative.
- TruckInput.ownerDriverId selects an existing driver directly as owner, mutually
  exclusive with ownerId. Creation/update creates or reuses the driver's investor
  identity atomically with the truck, including rollback on failure. The unique
  investor driver_id prevents duplicate owner identities. Driver and investor
  statements remain separate. A failed truck save must not leave a new investor.
- Recurring Charges contains Truck charges, Installment plans, Charge types.
  GET /truck-charges accepts weekStart (Monday, current week by default) and
  returns owners from fleet ownership history at the selected week's end.
  Owner links, search, sorting and eligibility use that history, independently
  of settlement terms. An agreement naming another owner shows Review terms;
  ownership edits do not infer percentages or rewrite accounting agreements.
  Returning to the browser tab refreshes idle charge views.
  Inclusion is attached to trucks, never to the current driver. Counts open an
  Included trucks filter for the chosen charge; searching and selected week still
  apply. Show all non-company trucks, including inactive/unassigned/owner-operated.
- Truck forms expose ownershipEffectiveDate (a New York calendar date, no future
  transfers). It can correct the current owner's start without changing owner.
  Truck reads return ownerSince (null for an unknown migration baseline) and
  ownershipVersion; forms send expectedOwnershipVersion to reject stale edits.
  Only the latest boundary can move, never across an earlier known transfer.
  Adjacent history periods remain contiguous; unknown baseline timestamps are
  observation dates, not acquisition dates. Fleet/financial locks and affected
  finalized driver/investor statements guard corrections. Investor agreement
  percentages and dates remain separate; correcting ownership never invents terms.

## Five ELD truck reviews (migration 094)

- Status Board no longer shows the Five ELD review counter or week/totals caption. Missing trucks from the latest successful Five ELD cache become durable `five_eld_truck` system tasks, matched against all local trucks by normalized VIN, including inactive/unassigned trucks. Ambiguous VINs never authorize creation.
- Settings > System tasks supports multiple people for every category: name chips with hover/focus removal and an Add person dropdown, saved with the existing versioned assignment API. The Five ELD category initially belongs to the unique active Nate Finesse account; later assignment edits apply to existing and future tasks.
- GET `/tasks/five-eld/{id}` and POST `/tasks/five-eld/{id}/complete` require task visibility plus trucks.read/create and tasks.review for completion. Review prefills provider VIN/unit in an editable truck form. Creation and task completion commit atomically with source-version/duplicate guards and a durable source identity/actor-name receipt. No driver assignments are inferred.
- `TestFiveELDTruckTasksDatabase` uses local disposable `MSERP_DRIVER_BOARD_TEST_DATABASE_URL`; `node scripts/test-task-board-views.mjs` covers review, multi-assignee chips, and caption removal.

### Maintenance brands, historical dates and service tasks (100–101)

- Migration 100 consolidates untouched branch vendors into 12 brands; superseded rows are hidden from options, while work-order address/name snapshots remain historical. Enter branch addresses per work order. Sources: docs/data/maintenance-brands.json.
- Customer-confirmed untouched expense-sheet imports use saved expense_date as service date and Completed status, audited without changing ledger amounts. Only legacy imports can be Completed without a resolved truck. Exact PM Service/Oil change names reset the unified PM Service schedule; missing mileage stays unknown.
- Preventative dates open the shared work-order modal in place; notes truncate with hover/focus details, and stale mileage is gray. Embedded truck History/Due services omit Truck; Settings remains on standalone Work orders.
- Migration 101 originally added maintenance_pm/maintenance_oil system categories and assigneeRoleIds. Settings supports people and roles; all active current role members receive the category. Keep role membership checks synchronized in task_assignments.go and task_board_repository.go. API writes validate role IDs and versions, preserve omitted role arrays, and audit changes.
- repository/maintenance_tasks.go and jobs/maintenance_tasks.go reconcile service tasks at startup/every minute using existing schedules plus fresh exact-VIN Five ELD mileage. One open PM Service task per truck; source completion closes its cycle. Missing telemetry preserves outstanding reminders. Worker never creates service/expense readings. See docs/MAINTENANCE.md for lifecycle and history rules.

### Unified PM Service (108)

- PM and former oil completions share one PM Service schedule and reminder category.
  Migration 108 keeps existing PM intervals per scope, promotes oil-only intervals,
  merges people/role assignees, and retains duplicate reminders as completed history.
  Prior schedules and assignments are saved in access_audit. Retired oil schedule
  writes are rejected; legacy work/import type codes are normalized to preventive/pm.
- Historical services, invoice amounts, itemized names and audit snapshots remain
  stored unchanged. Maintenance reads present the legacy oil type as PM Service;
  due calculations and reminders use the latest completed pm or oil tag. Unconfirmed
  and voided records never reset the schedule. The retired type is hidden from options.
- Preventative maintenance has one table and one interval setting; vendor, service
  cost and last-service-note fields are removed from its API response and table.
  Work-order invoices retain their normal accounting fields and oil-grade evidence.


### Service programs (129)

- Fleet > Service programs (`/fleet/service-programs`) replaces the PM overview;
  the old Preventative maintenance path redirects to Oil change 20k. Migration
  129 preserves existing PM coverage, intervals, overrides, baseline/history and
  reminder assignees. New truck enrollments are explicit. Programs support miles
  and days/months (first limit reached), warnings, instructions, pause state,
  per-truck overrides and people/role reminder assignment. Program definitions
  require Administrator plus Work orders Edit; truck operations require Edit.
- repository/service_programs.go and httpapi/service_program_handlers.go own
  versioned program/enrollment/baseline APIs. Work orders explicitly select
  completed programIds; omitted legacy inputs retain PM tracking. Completion
  never follows a free-text program name. Costs stay on the existing invoice.
  Record service creates completed internal work without an invoice. Unknown
  last service stays unknown; manual baselines never rewrite invoices.
- Truck profiles share ServiceProgramsWorkspace. The table displays Miles since
  service (current odometer minus service odometer), without miles remaining or
  next-due odometer. Status/reminders still use configured intervals. Calendar
  months clamp to month end. Work-order Settings retains types/vendors; interval
  configuration lives in programs.
- maintenance_pm remains the migrated oil task kind; custom programs use
  maintenance_program. Reminder uniqueness is per truck/program. Task list/count
  visibility applies effective per-program people/roles or inherited PM defaults.
  The internal assignment placeholder is excluded from Settings. Preserve the
  original PM work-order/baseline source fences and stale-telemetry behavior.
- Run TestMaintenanceDatabase (fresh/migrated as mserp_app),
  TestServiceProgramCalendar and frontend/scripts/test-service-programs-e2e.mjs
  against review-local.mjs after a /api build. See docs/MAINTENANCE.md.


### Automatic service-type program completion (130)

- maintenance_types.program_id links a reporting type to a service program.
  Oil change (stable code preventive) resets Oil change 20k automatically when
  a completed work order is recorded. Other types may link to other programs
  in Work orders Settings. The server snapshots linked program IDs even when a
  caller sends an empty selection. Type edits do not reinterpret saved history;
  omitted mapping input preserves it, explicit empty string clears it. Normal
  work orders display the automatic effect rather than program checkboxes.
- Recording work never enrolls an excluded truck. Program recording still
  supports explicit combined completions. Source-only metadata, accounting
  identity, reviewed/voided exclusions and baseline source fences remain intact.
- The standalone Service programs page defaults to Oil change 20k; explicit
  ?program= opens the full directory. Embedded truck profiles show all programs.
  Live oil-sheet reconciliation uses exact identity and audited versioned APIs,
  preserves invoice history, and never treats undated Current ODO as a reading.
- Program settings edits the same type links with a multi-select dropdown.
  serviceTypeCodes and serviceTypeVersions expose linked types and fence changes
  against both program and affected type revisions. Omitted codes preserve links;
  [] unlinks all. Program/type changes commit atomically with per-type audit;
  types linked elsewhere cannot be implicitly reassigned. No new schema is needed.
- service_programs.manage is configurable under Settings > Roles & permissions
  and replaces Administrator-only program creation/settings/assignee checks.
  Work order category View/Edit is still required. Maintenance options exposes
  canManagePrograms; route policies and handler checks enforce the capability.

### Samsara-first telematics (131)

- `internal/samsara`, `jobs/sync_samsara.go`, and repository `samsara_repository.go`
  read complete paginated vehicle/stat snapshots into separate caches. Server-only
  `SAMSARA_API_TOKEN`, optional `SAMSARA_API_VERSION`, `SAMSARA_API_URL` and
  `SAMSARA_SYNC_INTERVAL` configure a startup/minute worker. No provider writes.
- Exact VINs match only one local truck. Duplicate Samsara VINs resolve only to
  one attached gateway; multiple gateways or duplicate IDs/stats stay ambiguous.
  Fleet maps remain Samsara-first. Status Board GPS, arrivals and ETA use Five ELD
  exclusively through statusBoardELDLocationJoinSQL, with no Samsara fallback;
  invalidate old Samsara route/evidence before display. Odometers prefer fresh plausible Samsara, then validated fresh
  Five ELD; `maintenance_odometer_checks.go` rejects backwards/implausible mileage
  against reviewed service/manual history, effective baselines and accepted samples.
  Read-time guards exclude legacy bad caches; rejected imports preserve last good
  readings. GPS is independent. Preserve timestamps and one-hour autofill guard.
  Status Board always uses Five ELD for GPS/arrival/ETA. Profile maps remain
  Samsara-first. Migration 133 adds raw odometer observations and versioned,
  audited per-truck Automatic/Samsara/Five ELD/Manual maintenance choices at
  `/maintenance/odometer-sources/{truckId}`. Category View permits comparison;
  Edit permits source changes/manual observations. Explicit Five ELD resolves
  provider-only disagreement without bypassing service history or continuity.
  Explicit unavailable sources do not fall back. Source choice applies across
  programs and work order autofill, never GPS. Manual observations retain time
  and reason; recording one alone never pins Manual. See integrations/SAMSARA.md.
- Keep read-only review servers' Samsara token empty. Run TestSamsaraDatabase,
  TestDriverBoardDatabase, TestMaintenanceDatabase and test-samsara-views.mjs.
  See docs/integrations/SAMSARA.md for matching, configuration and live proof.

### Fleet work order lifecycle (132)

- Migration 132 adds workStatus, explicit dispatch holds, planning fields and an
  immutable final invoice payment confirmation. Tables use In progress,
  Completed - pending payment, Completed - paid; Canceled keeps canceled/voided
  records separate. Internal completed is pending payment; closed is paid/no charge.
  /maintenance/detail?id=... shows three progress steps with service type/dates
  beside description in Work. Stage-specific columns and server pagination replace
  Kanban cards; tab changes clear rows from the previous stage. Creation needs no price;
  creator/time are immutable server snapshots, with no responsible-person field.
- Any Work orders Edit user may release an order without Administrator/trucks.edit.
  Effective operatingStatus combines all order holds with manual restrictions;
  assignment changes preserve restrictions. Active/Inactive and crews stay separate.
- Status Board shows only Shop/OOS/Hold beside Truck, without repair details.
  New board/Gross Board loads check holds under the truck lock; existing/historical
  records remain editable. No external booking integration, automatic date-based
  release or future downtime reservation is implemented.
- Final payment uses existing invoice fields and explicit Confirm paid & finish;
  Save invoice alone does not confirm paid. There is no separate payment endpoint
  or ledger. Done requires finished work, no own hold, and confirmation or explicit
  No charge. Paid work orders expose Edit work order for all ordinary fields. Migration 136
  preserves the original payment confirmation and appends invoiceCorrections for
  explicit versioned invoice edits; existing finalized/collection guards remain. It never issues money or
  changes expense_payments/payroll recovery; do not infer payment from collections.
  See docs/MAINTENANCE.md and test-fleet-workflow-e2e.mjs.
- Migration 135 records the user's explicit historical paid confirmation. Only
  blank workflow stages without holds, created before migration 132's activation,
  receive immutable historicalPaid and per-record audits. Effective reads put them
  in Completed - paid, preserving service/identity review, invoices and payroll
  recovery. No payment transaction, date, price or No charge is inferred. New
  orders still require explicit payment confirmation; historical corrections cannot
  reopen work or place holds. See testHistoricalPaidCutover in maintenance tests.

### Shared fleet statuses and history (137)

- Weekly PTI uses migrations 140/144, repository/httpapi `weekly_pti*`, and
  `/fleet/weekly-pti`. The matrix lists every truck and every recorded week,
  with company/investor trucks above owner-operated trucks. Grouping uses the
  recorded owner, never driver availability. Restricted marks are / (not required,
  yellow), 1 (sent and checked, green), and 0 (not sent, red). Blank means unrecorded.
  Legacy eligibility snapshots remain stored but never filter the matrix or set marks.
  The startup/minute worker maintains the current roster without changing manual marks.
  The grid has 64px week cells and fills the available page height. Notes and
  Checked by are hidden; stored notes/attribution survive edits. Source provenance,
  optimistic versions, idempotency, immutable audit and pti.read/edit are preserved.
  GET /weekly-pti/matrix is read-only; missing historical cells are created only on
  explicit edits to existing weeks. No fleet, financial or provider writes occur.
  `backend/scripts/prepare-weekly-pti-import.mjs` prepares the reviewed four-week
  Sheets import and comparison outside Git, preserving native edits and source blanks.
  See docs/WEEKLY_PTI.md. Tests: TestWeeklyPTIDatabase and test-weekly-pti-e2e.mjs.
- Employment, driver availability, truck condition, and dispatch detail remain
  separate. `fleet_status_rules` owns the mapping; database triggers synchronize
  profile, board, crew, and work-order writes. Left requires explicit termination;
  personal reasons never restrict trucks. Board editors may place restrictions,
  while existing Fleet/work-order permissions govern releases. Independent holds
  remain effective until explicitly released; loads and finances stay intact.
- Driver/Truck profiles have Status history tables/timelines showing Status,
  Date changed, Duration, and Changed by, with recorded time and source in details.
  Driver histories show only driver availability/employment, defaulting to
  availability; truck condition history appears only on truck profiles.
  Profile history excludes operational dispatch events and filter choices;
  Status Board edit history retains the operational audit.
  No-op writes and GETs add no history. Newest transitions may be backdated after
  the prior transition; migration baselines have unknown earlier starts.
- Status activation is separate from schema deployment: review
  `-fleet-status-preview`, then use `-fleet-status-activate <reviewed-revision>`.
  Snapshot changes refuse activation. See docs/FLEET_STATUSES.md. New writers
  call setBoardActor before operational row locks. Queue changes persist
  ENROUTE/RESERVED; a cache-only startup/minute worker catches date/source changes.
- Database checks use the disposable MSERP_DRIVER_BOARD_TEST_DATABASE_URL.
  Browser coverage: `node scripts/test-fleet-status-e2e.mjs` against review-local.


### Driver leave plans and dispatcher accounts (142–143)

- Status Board Home time uses versioned driver_board.leave_plan JSON (homeStart,
  homeEnd, vacationReturn). Calendar dates use New York; Monday–Sunday dates show
  HOME FRI/BACK TUE, otherwise day/month. Home switches to Back only once its
  start has arrived and status is Home. Vacation returns accept a date, month,
  or unknown. Legacy home_time text stays until explicitly replaced/cleared.
- Board and driver-profile availability dialogs share date fields. Home requires
  a return date when selected; vacation remains optional. Profile saves fence
  both statusVersion and leavePlanVersion. Dates, status, history/undo and SSE
  retain the existing atomic version/audit boundaries; no date changes status.
- A startup/minute cache-only driver-leave-tasks worker creates one overdue task
  per still-Home/Vacation driver beginning the next New York day after return.
  Month-only vacation returns expire at month end; unknown dates create no task.
  Return, extension, unknown dates or termination resolve reminders automatically.
  Only the assigned dispatcher's active account and Administrators see them;
  reassignments move visibility. Unassigned cases remain visible to Administrators.
- Dispatcher creation requires dispatchers.create plus access.manage, valid email
  and a 12–72 byte password. One transaction creates the dispatcher and its login
  in the permanent built-in Dispatcher role; failure rolls back both. Never place
  credentials/hashes in audit payloads. Existing account/profile pairs require
  explicit reviewed UUID pairing; migration never matches names automatically.
- Dispatcher name/email/active synchronize with their one-to-one account; login
  detail edits require access.manage and revoke sessions. Deleting a dispatcher
  disables the retained account. The Dispatcher role cannot be deleted/renamed
  or replaced on a paired account; its permissions remain administrator editable.
  Create new dispatchers through Dispatchers, not the generic Add user flow.
- Verify TestFleetStatusDatabase (fresh/migrated as mserp_app), test-driver-board.mjs,
  test-status-board-layout.mjs and test-driver-leave-e2e.mjs against review-local.
