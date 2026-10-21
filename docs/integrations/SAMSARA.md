# Samsara telematics

MSERP reads Samsara vehicle identity and current telemetry server-side. Credentials
are held in the API service environment, never browser code, responses or Git.
The customer authorized reuse of Fuelbot's existing Samsara token/version on the
same server; Fuelbot remains an independent service.

Configuration: `SAMSARA_API_TOKEN`, optional `SAMSARA_API_VERSION`,
`SAMSARA_API_URL` (default `https://api.samsara.com`, HTTPS origin only), and
`SAMSARA_SYNC_INTERVAL` (default `1m`, permitted 1m–1h). Without a token there is
no Samsara worker. With a token, a startup/interval worker fetches a complete
paginated `/fleet/vehicles` roster plus `/fleet/vehicles/stats` for `gps`,
`obdOdometerMeters`, `gpsOdometerMeters`. The token needs Read Vehicles and Read
Vehicle Statistics. Reads retry bounded 429/5xx responses, respect Retry-After,
refuse redirects and never expose provider error bodies or headers.

Migration 131 stores a separate cache and one hour of GPS samples. Whole-roster
transactions disable removed/ambiguous identities. Failures preserve last-known
data with its original observation time. Each GPS/odometer timestamp advances
independently; an older sample cannot rewind a cache. GPS samples are mph;
odometer meters convert to rounded miles. Valid ECU odometer wins over a GPS
odometer; distance-since-gateway-installation is never an odometer substitute.

Matching uses exact trimmed uppercase VIN, unique in MSERP (including inactive
local duplicates). Samsara can retain an unpaired historical vehicle for the same
VIN: when multiple roster records share a VIN, exactly one attached gateway
(`gateway.serial`) resolves the current record. Zero or multiple attached gateways,
duplicate provider IDs/stats, or a gateway serial attached to multiple records stay
ambiguous. Never choose by name, largest mileage or newest timestamp. Reading
telemetry never creates trucks, ownership or assignments.

GPS and odometer selection are independent. GPS keeps the existing Samsara-first
rule. Odometers prefer fresh, plausible Samsara, then fresh, plausible Five ELD.
Saved history and valid last-known telemetry remain available when neither is
fresh, retaining their original dates and normal stale/missing-information status.
Work order autofill requires a valid provider observation within one hour.

Both telemetry providers are checked against active manual readings, completed
work orders and effective program baselines. A candidate cannot precede reviewed
history, fall below its mileage, or imply over 100 mph continuously (whole-date
anchors allow the full observation day plus 100 miles of measurement tolerance).
Five ELD must also agree with an otherwise valid Samsara observation within 100
miles/hour of elapsed observation time plus 25 miles. Each provider's new sample
must not rewind or jump beyond that precise-time bound from its last accepted
valid sample. Rejected updates preserve the last accepted cache value/timestamp;
GPS still updates independently. Read-time checks also screen legacy bad caches
and immediately revalidate after a baseline correction. Superseded source-revision
odometers are not reinstated as validation anchors or saved fallback values.
No offset is invented and no historical service odometer or invoice is rewritten.

The shared location selection feeds Status Board, driver/truck profile maps and
GPS arrival/travel automation. Provider changes invalidate cached arrival/ETA
evidence before it can be shown for the new source. Samsara travel uses cached
mph samples; it does not query Five ELD for a Samsara-selected truck. The shared
mileage selection feeds service programs, due reminders and new Work order
autofill. `/jobs/sync-eld` refreshes both configured providers under its existing
permission, retaining successful provider updates on a partial outage. Browser
provider labels identify Samsara or Five ELD correctly.

Validate with `go test ./internal/samsara ./internal/jobs`, and
`TestSamsaraDatabase` using a disposable local `_test` database via
`MSERP_SAMSARA_TEST_DATABASE_URL`. Fresh/upgrade database tests cover priority,
fallback, outages, ambiguity, timestamp monotonicity and app-role ownership.
`TestDriverBoardDatabase` covers provider changes, GPS proximity and routing.
After a `/api` build, run `test-samsara-views.mjs` with review-local.mjs and its
disposable database. CI runs the Samsara database suite. Production proof must
include active release, sync timestamps/counts and actual selected API fields.

References: [Vehicle stats snapshot](https://developers.samsara.com/reference/getvehiclestats),
[Mileage and distance](https://developers.samsara.com/docs/mileage-and-distance),
[Telematics](https://developers.samsara.com/docs/telematics).
