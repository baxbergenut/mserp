# Status Board history and sidebar navigation

Measured on November 3, 2026. Production inspection was read-only; save benchmarks
used an isolated local PostgreSQL test schema. No production history was changed.

## History

Production had 2,633 history entries occupying about 3.9 MiB including indexes.
Opening the board does not retrieve the history list. The dialog fetches up to
50 entries at a time using an ID cursor; an additional row detects another page.
Saves append/merge one audit event per driver/transaction and look up its undo ID.
Load-plan changes retain before/after snapshots in that event.

Read-only production EXPLAIN ANALYZE measurements (database execution only):

| Query | Observed execution |
| --- | --- |
| Latest global history page, five runs | 0.071–0.424 ms |
| Latest driver history page, five runs | 0.103–0.191 ms |
| Driver filter without a matching field | 0.236–3.336 ms |
| Undo ID lookup | 0.026–0.030 ms |

Local repeated A/B note saves (150 per batch, after warmup) had median 2.41–2.46 ms
with audit triggers and 2.13–2.27 ms without them. The observed history increment
was roughly 0.2–0.3 ms; tail timings were noisy and do not establish a p95 benefit
from disabling history. These are local repository timings, not browser latency.

At roughly 3,000 versus 100,000 synthetic events, board reads stayed around
2.3 ms median and simple note saves around 2.4 ms. A history field filter with no
matches grew from about 0.5 ms to 14.8 ms median: it scans candidates because the
JSON field predicate has no dedicated index. Revisit that query if the archive
grows substantially. These synthetic notes do not model all load-plan payloads
or concurrent production writers. Existing fleet-status write locking is a
separate source of contention. Retain history and undo; current measurements
do not justify pruning them for speed.

## Navigation freeze

Production Nginx 1.24 TLS listeners had no HTTP/2 enabled. Each Status Board tab
holds two SSE connections (tasks and board updates). With three tabs, Chromium
used all six HTTP/1.1 connections for the origin. A probe timed out after 2.5 s
without reaching the test server, and clicking Expenses stayed on Status Board.
Closing two tabs immediately released the queued navigation. This reproduces a
navigation freeze independently of unsaved edits or history reads.

The site configuration now enables HTTP/2 on all TLS listeners. Install this
separately from the application release, validate Nginx, reload, and verify `h2`
negotiation plus navigation with multiple tabs. Until installed, production is
unchanged. The existing unsaved-edit navigation guards remain intact.

Validation: `frontend/scripts/test-sidebar-connections.mjs` reproduces HTTP/1.1
starvation and checks the same three-tab flow over HTTP/2. The HTTP/2 probe
completed in 1.6 ms and Expenses navigation succeeded while all six streams
were open. The proposed site passed `nginx -t` using an isolated config on the
production Nginx binary; the active configuration was not reloaded or modified.
