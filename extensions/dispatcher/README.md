# MS Express Dispatch Watch

Private Chrome Manifest V3 extension, version 0.2.2.

## Install for a pilot

1. Deploy the MSERP backend/frontend changes and migration 091 first. The packaged
   extension uses `https://erp.msexpressinc.net/api/dispatcher-extension`.
2. Extract the ZIP into a permanent folder. In Chrome open `chrome://extensions`,
   turn on **Developer mode**, choose **Load unpacked**, and select the folder
   containing `manifest.json`. Keep the extracted folder on the computer.
3. In MSERP, open the dispatcher's details. An administrator with `access.manage`
   can **Enable and create code** in **Dispatcher Chrome extension**. For an updater
   (migration 092), use **Chrome extension** beside their name in the Updaters table.
4. Paste the code into the extension's toolbar popup and click **Activate**.
   Codes expire in 10 minutes and can be redeemed once. This authorizes one
   browser for 30 days. A replacement code revokes the previous browser.
5. Reload an already-open J.B. Hunt tab so the response listener is installed.
   Sign in normally, select Find Loads → grid, set the site's search criteria,
   and run the search. Configure the extension's refresh interval and sound.
6. Press **▶ Play** beside the pagination controls below the load table. Press
   **Ⅱ Pause** to stop. Sound is unlocked by the page's Play button.

The extension starts paused after reload. Native site interactions pause it so
refreshes do not race a filter edit, offer, booking, or manual page change.
Settings are stored locally in that Chrome profile. Use Test sound to check the
device volume; Chrome/OS mute settings still apply.

## What it does

- Default interval 5 seconds, minimum 4 seconds, configurable up to one hour.
  The interval is a minimum between automatic refresh/page requests, not a
  guaranteed deadline. Slow requests are awaited; none overlap.
- Uses the site's native Refresh/First/Next controls. Reads structured
  `data.searchJobsElastic.data.jobs` responses from the confirmed
  `/carrier/secure/loadboard/graphql/native` endpoint. No token copying, private
  GraphQL request construction, or undocumented booking calls.
- Uses only the filters selected on J.B. Hunt. No extension filters are applied;
  saved filters from version 0.1 are ignored.
- The first complete scan of every result page after Play is a silent baseline.
  Later, an available load with an unseen `jobIdentifier` is new. IDs stay in
  memory for the entire run, so price changes, page moves and disappearing then
  returning loads do not alert again. Pause/reload then Play starts a new baseline.
  At 100,000 seen IDs monitoring stops rather than evicting history.
- New rows are highlighted, labeled NEW and moved to the top of their native
  page. Original row nodes and click handlers are retained and restored before
  the site's next action. The New loads popover beside pagination lists new
  discoveries across pages, newest first, with their last checked page number.
  Highlights and promotion are reapplied after row rerenders and remain while
  paused. Discoveries absent from a completed scan stay in the list with a
  "No longer in results" label and their last-seen time; this does not confirm
  booking. Returning discoveries regain their highlight without another alert.
  The discovery list and baseline reset on the next Play or page reload.
- Chime, Bell, Radar or Silent, volume control, and three highlight colors.
  One sound per response containing newly discovered IDs; baseline scans and
  unchanged results never play a notification.
- One running tab per Chrome profile. Keep Chrome/the tab open and the computer
  awake. Background throttling, memory saving, network latency and sleep can
  delay checks. This does not promise a four-second background SLA.
- Stops on authorization failure, site errors/429, dialogs, malformed responses,
  response/row mismatch or a 30-second timeout. Restart explicitly after resolving
  the issue; no retry storm or CAPTCHA bypass.

Native origin/destination radii, equipment, job type and search dates remain in
J.B. Hunt. The extension monitors only that returned search.
More than one result page takes multiple intervals
to scan; for 2 pages at 5 seconds, a full scan takes roughly 10 seconds plus latency.
The combined list reflects the latest completed checks, not a booking guarantee.

## Access and privacy

All load processing and alerts run locally. Load responses, credentials and
cookies are never sent to MSERP. The main-world observer projects only the load
fields needed for new-load detection and display; it does not capture headers, request bodies,
coordinates, commodities or unrelated GraphQL operations. Data is in memory only.
No remote scripts, analytics, all-sites permissions or Telegram traffic.

The service worker stores an extension-scoped token in trusted-context-only
Chrome local storage. Content/page scripts never receive it. The server stores
only SHA-256 digests, checks enabled status, expiry and dispatcher activity before
every cycle, and grants no ordinary ERP API access. Revocation and replacement
take effect on the next check; a failed check stops monitoring. Deactivating a
dispatcher permanently revokes the device, including after later reactivation.
Administrative changes are version-checked, CSRF-protected, audited, and require
`access.manage`. Activation is rate limited. There is no offline authorization.

The optional J.B. Hunt account reference is administrative metadata, **not verified
account binding**. No official J.B. Hunt SSO integration has been established.
An activation code is a credential: share it only with its intended dispatcher.
Someone with a copied token could impersonate that installation until revoked.
As with any client-side extension, a person with its source can modify/remove
local checks. This controls access to the supported extension and ERP service;
it is not copy protection against a rewritten client. Stronger distribution can
use managed company browsers and device-bound authentication in a later phase.

## Distribution and updates

Publishing is unnecessary for a small pilot: distribute the ZIP privately and
use Load unpacked. A ZIP is not directly installable, and an arbitrary local CRX
is not the normal Windows/macOS installation route. Unpacked updates are manual:
replace the files, click Reload on the extension card, and reload J.B. Hunt.
For ordinary team rollout and automatic updates, use a private Chrome Web Store
listing or enterprise-managed deployment. An unlisted listing alone is not access
control; keep ERP activation enabled.

Official guidance: https://developer.chrome.com/docs/extensions/how-to/distribute

## Development and validation

From `frontend/`: `node scripts/test-dispatch-extension.mjs` launches an isolated
Chromium profile with the actual extension; every HTTP request is locally mocked.
It checks structured parsing, silent all-page baselines, ID deduplication, sound,
activation, legacy-setting migration, footer layout, page scanning, promotion,
minimum spacing, revocation and rate-limit stops. It never uses live accounts.
`node scripts/test-dispatcher-access-views.mjs` checks the ERP access card.

From `backend/`: `go test ./...` and `go vet ./...`. Set the optional
`MSERP_EXTENSION_TEST_DATABASE_URL` to a disposable local database ending in
`_test` to test fresh and migrated schemas as `mserp_app`, atomic code redemption,
expiry, version conflicts, revocation, reactivation and audit records.

Run `extensions/package-dispatcher.ps1` from PowerShell to create the ZIP under
`extensions/dist/`. Backend production deployment is separate from packaging.

## Later phases

Telegram notifications, official account binding, device-bound credentials,
multiple saved search profiles, gross-maximizing / RPM-maximizing ranking
(including origin/destination deadhead), and booking workflow investigation.
Autobook is intentionally not implemented: the response's eligibility flag alone
does not establish the booking contract, price revalidation or duplicate guards.
