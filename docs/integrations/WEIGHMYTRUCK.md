# WeighMyTruck driver management

The full vendor OpenAPI 3.0.1 document is saved as
[`weighmytruck-openapi.json`](weighmytruck-openapi.json), downloaded on October 9,
2026 from the authenticated Swagger page:
https://app.weighmytruck.com/fleetsettlement/swagger/index.html.
The document includes Settlement and Sites for future use; MSERP currently calls
only AddDriverToFleet and RemoveDriver.

## Authentication and wire format

OAuth uses the Microsoft token URL, form-encoded client ID/client secret/scope,
and `grant_type=client_credentials`. The Go client caches the short-lived bearer
token in memory. Browsers call only authenticated MSERP `/weighmytruck` routes;
they never receive vendor credentials or call the vendor directly.

Server-only environment variables:

```dotenv
WEIGHMYTRUCK_CLIENT_ID=...
WEIGHMYTRUCK_CLIENT_SECRET=...
WEIGHMYTRUCK_TOKEN_URL=https://login.microsoftonline.com/0c42b1f7-92b4-4df0-915d-4bebceb95491/oauth2/v2.0/token
WEIGHMYTRUCK_SCOPE=api://6b61f2df-5ec0-4dea-bfb8-b9589ec9942d/.default
WEIGHMYTRUCK_API_URL=https://app.weighmytruck.com/fleetsettlement
WEIGHMYTRUCK_COMPANY_NAME=MS Express Inc.
```

Local secrets belong in ignored `backend/.env.local`; production uses
`/etc/mserp/mserp.env`, outside release archives. Empty credentials disable the
integration; partial credentials are rejected. Never use `NEXT_PUBLIC_` variables.

`POST /DriverManager/AddDriverToFleet` accepts firstName, lastName, driverEmail,
phone and companyName. MSERP derives them from the selected driver and server
company configuration. The first word of the stored full name becomes firstName;
remaining words become lastName. Missing or invalid details block Add and are
corrected in the ordinary driver profile. Terminated drivers cannot be added.

`POST /DriverManager/RemoveDriver` accepts a **JSON string** containing the email,
for example `"driver@example.com"`. The Swagger object example is inconsistent
with its string schema. Empty-email validation requests from the production
server confirmed the string schema on October 9, 2026; an object fails model
binding. Removal uses the saved enrollment email, even after profile changes.

## Membership tracking

The published integration API has no driver roster/list endpoint. The website
does call `GET https://weighmytruck.com/Fleet/GetDriverList`; it uses a website
login session, not the supplied integration OAuth token. On October 9, 2026,
server-side GET requests with and without a valid integration bearer token both
returned HTTP 302 to `/Home/Login?ReturnUrl=%2FFleet%2FGetDriverList`. No website
cookie is copied into the integration. If the vendor adds supported bearer access
to a list API, it can replace manual roster reconciliation.

Migration 073 adds a local
membership table, actor-stamped event log and initial-import receipt. Refresh
reads MSERP's database; it does not claim to synchronize the vendor roster.
Manage subsequent membership changes through MSERP. The page shows only added
drivers, with attention rows sorted first and warning icons after driver names.
Add driver opens a picker of active drivers not already added; removal stays on
each row. Success notices disappear after five seconds. There is no Membership
column, membership filter or routine Verify action. Pending/uncertain additions
remain visible as error notices without appearing as confirmed added drivers.
Administrative recovery for external changes or uncertain operations retains
the versioned verification API; normal users do not need a website-login workflow.

The October 9 website export contains 25 accounts. Its CSV is stored privately
under ignored `backend/.weighmytruck/` locally and `/etc/mserp/` in production;
personal contact data is not committed. Initialize each database once:

```powershell
# Run from backend after migration 073, with the intended DATABASE_URL.
go run ./cmd/server -import-weighmytruck-roster .weighmytruck/roster-20261009.csv
```

The production release binary accepts the same flag. Imports exit before HTTP,
scheduled jobs or provider calls. Exact unique email/phone evidence links drivers;
ambiguous/unmatched accounts remain visible for explicit linking or removal.
The same file is idempotent; different files cannot overwrite an initialized
tracker. An explicit header-only CSV can initialize a genuinely empty fleet.

Enrolled terminated drivers are highlighted. Deleting a driver preserves the
membership and email so it remains removable. One-click operations reserve a
versioned pending row and audit event before calling the provider. Concurrent or
stale clicks cannot issue another mutation. Successful results update membership;
authentication/rate-limit failures retain its previous value. Ambiguous failures
require explicit verification. Pending operations left by a process crash become
verifiable after two minutes, longer than the bounded network/finalization time.
Provider mutation requests are never automatically replayed. Provider response
bodies and credentials are excluded from logs and frontend errors.

Permissions are `weighmytruck.read` and `weighmytruck.write`; Administrators receive
them automatically through the code-owned catalog. Other roles require explicit
assignment. The page includes only driver identity/contact/status fields; normal
profile links additionally require fleet.read. All writes require session CSRF.

MSERP API: `GET /weighmytruck`, `POST /weighmytruck/change`, and
`POST /weighmytruck/{id}/verify` or `/link`. Linking/verification are local tracking
corrections and never call the provider. Settlement, sites, driver password and
account-profile updates are intentionally not implemented.

## Validation

Set `MSERP_WMT_TEST_DATABASE_URL` to a disposable local database containing `_test`
and run `go test ./internal/httpapi -run TestWeighMyTruckDatabase -v` from backend.
Both fresh and migrated schemas run as mserp_app. CI runs this database coverage.
Client tests validate OAuth encoding/token reuse, mutation payloads, redaction,
failure classification and absence of mutation retries.

Build frontend with `NEXT_PUBLIC_API_URL=/api`, install Playwright Chromium, then
set `MSERP_WMT_BROWSER_TEST=1` for the same Go test. It launches the browser against
the real Go routes/database and a synthetic upstream OAuth/provider server.
The browser test covers the added-only roster, active-driver picker, Add/Remove,
double-clicks, timed notices, persistence, search, attention ordering, narrow
layout and absence of external browser requests. It never
changes real fleet membership.
