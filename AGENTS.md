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

The browser talks directly to the Go API. There is no authentication layer,
container setup, migration runner, or generated API client in this repository.

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
- `backend/internal/httpapi/toll_handlers.go` and `toll_import.go`: toll listing
  and strict PrePass-style CSV upload/parsing.
- `backend/internal/httpapi/file_handlers.go`: IRP/cab-card and CDL uploads,
  extraction orchestration, and stored-file downloads.
- `backend/internal/repository/`: SQL and domain/API structs. `fleet_repository.go`
  also owns assignment transactions; `naming.go` owns canonical person and truck
  naming; `load_repository.go` maps DataTruck records to local loads.
- `backend/internal/datatruck/`: paginated DataTruck client with rate-limit retry.
- `backend/internal/relay/`: Relay Payments fuel transaction client.
- `backend/internal/jobs/sync_loads.go`: synchronous load sync over a rolling
  seven-day window.
- `backend/internal/jobs/sync_fuel.go`: synchronous missing-day Relay fuel sync.
- `backend/internal/groq/`: vision extraction client and normalization for truck
  cab cards and driver CDLs.
- `backend/internal/db/pool.go`: pgx pool configuration.
- `backend/sql/init.sql`: complete schema for a new database.
- `backend/sql/002_add_tolls.sql` through `006_allow_multiple_relay_driver_ids.sql`:
  manual incremental migrations for older databases.

### Frontend

- `frontend/app/layout.tsx`: global shell and sidebar.
- `frontend/app/page.tsx`: redirects `/` to `/dashboard`.
- `frontend/app/dashboard/`: load-derived metrics and charting.
- `frontend/app/loads/`: load table, filters, sorting, and manual sync.
- `frontend/app/tolls/`: toll table and CSV import UX.
- `frontend/app/drivers/`, `trucks/`, and `dispatchers/`: client-side CRUD pages;
  their colocated `*Form.tsx` files own form conversion/defaults.
- `frontend/app/components/management/ManagementUI.tsx`: shared management page,
  modal, form, table, empty/error, and confirmation primitives. Reuse these
  before creating parallel UI patterns.
- `frontend/app/components/`: shared dashboard, filtering, and navigation pieces.
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
GROQ_API_KEY=...
GROQ_MODEL=qwen/qwen3.6-27b
RELAY_ENVIRONMENT=production
RELAY_STAGING_API_KEY=...
RELAY_PRODUCTION_API_KEY=...
RELAY_FUEL_SYNC_START_DATE=2026-01-01
```

`GROQ_API_KEY` is only required when document extraction is used. CORS currently
allows `http://localhost:3000` only.

For a new database, apply `backend/sql/init.sql`. For an existing database, apply
the numbered SQL files in order as needed. There is no automatic migration tool,
so schema changes must update `init.sql` and add a new incremental SQL file.

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

## Current API surface

- Health: `GET /healthz`, `GET /readyz`
- Loads: `GET /loads`, `POST /jobs/sync-loads`
- Drivers: `GET/POST /drivers`, `PUT/DELETE /drivers/{id}`
- Trucks: `GET/POST /trucks`, `PUT/DELETE /trucks/{id}`
- Dispatchers: `GET/POST /dispatchers`, `PUT/DELETE /dispatchers/{id}`
- Tolls: `GET /tolls`, `POST /toll-reports` (multipart CSV)
- Fuel: `GET /fuel-transactions`, `POST /jobs/sync-fuel`
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

- DataTruck and Relay fuel syncs are initiated by the frontend and remain
  synchronous. DataTruck fetches the last seven days and upserts by the upstream
  integer load record ID. The server write timeout is fifteen minutes to permit
  pagination, rate-limit retry, and an initial Relay historical backfill.
- Person names are title-cased for display and normalized for matching. Truck
  unit numbers are trimmed/collapsed and uppercased. Use the helpers in
  `backend/internal/repository/naming.go` rather than duplicating this logic.
- Driver/truck assignment history lives in `truck_driver_assignments`. Partial
  unique indexes enforce at most one current truck per driver and one current
  driver per truck. Assignment changes must remain transactional.
- Files are stored as `BYTEA` in PostgreSQL with metadata and SHA-256. IRP/CDL
  uploads accept PDF, PNG, JPEG, or WEBP originals up to 10 MB. PDFs require up
  to three browser-rendered page images for extraction. Never replace the stored
  original with rendered pages.
- GROQ-extracted fields are suggestions: the frontend fills the form and the user
  reviews before saving the truck or driver.
- Toll CSV headers and date/time formats are deliberately strict. Imports match
  `EquipID` to normalized truck units, report unmatched units, and use stable row
  fingerprints to avoid duplicate charges while allowing later re-imports after
  missing trucks are added.
- Database money/rates use PostgreSQL numeric values. Toll parsing uses integer
  cents before persistence. Avoid binary floating-point for new financial logic.
- Fuel report dates use each transaction's Relay merchant timezone rather than
  the browser timezone or a single UTC offset so ERP totals reconcile with Relay.
- Relay fuel sync records completed UTC dates and never marks the current UTC
  date complete. Driver identity is persisted in `relay_driver_links`; fuel,
  DEF, other products, fees, reporting dimensions, and raw payloads are stored.

## Implementation conventions

- Backend flow is handler -> repository, with integrations/jobs injected in
  `main.go`. Keep transport validation in `httpapi` and SQL/transactions in
  `repository`.
- Use `context.Context` through network and database calls. Return JSON errors via
  the existing API helpers and log operational failures with `slog`.
- Add focused Go tests beside the package as `*_test.go`. Existing tests cover
  DataTruck retry, load mapping, naming, document extraction, file validation,
  and toll CSV parsing.
- Frontend pages that fetch or mutate data are client components. Keep request
  code centralized in `app/lib/api.ts` and shared contracts in `app/lib/types.ts`.
- Follow the existing dark zinc/blue visual language and reuse management
  primitives. Preserve loading, empty, error, confirmation, and responsive states.
- Use the `@/*` TypeScript alias when it improves readability; strict TypeScript
  and no emit are enabled.
- No frontend test framework is configured. For behavior-heavy frontend changes,
  add one only if the task calls for it; otherwise validate with lint, build, and
  targeted browser checks.

## Validation

Run from the indicated subdirectory:

```powershell
# backend/
gofmt -w <changed-go-files>
go test ./...
go vet ./...

# frontend/
npm run lint
npm run build
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
rg -n 'fetchLoads|createDriver|uploadTollReport' frontend/app

# Find schema ownership for a field
rg -n '<field_name>' backend/sql backend/internal frontend/app/lib
```

Prefer these targeted searches over recursively reading the repository.
