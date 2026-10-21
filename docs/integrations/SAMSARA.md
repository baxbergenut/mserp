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

Matching uses exact trimmed uppercase VIN, unique in both the provider roster and
MSERP (including inactive local duplicates). Names/unit numbers never establish
identity, and reading telemetry never creates trucks, ownership or assignments.

When a valid matched Samsara field exists it takes priority, including over a
newer Five ELD observation or a manual operational current reading. Missing GPS
or odometer independently falls back to the existing source. Last-known Samsara
data retains its provider timestamp and normal stale display; an old provider
reading is not made fresh by polling. The Work order autofill still requires a
reading within one hour; stale preferred data is not silently relabelled as live.
Historical service odometers and invoices are not rewritten.

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
