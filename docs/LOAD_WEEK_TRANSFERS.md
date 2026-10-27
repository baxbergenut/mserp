# Manual weekend load transfers

Accountants can right-click a Saturday/Sunday load, or use its row menu, in
Driver Pay and Investor Pay. Both pages call the same placement transaction:

- **Move for everyone** moves dispatcher gross and both settlement calculations
  to next Monday.
- **Keep settlement this week** moves dispatcher gross to next Monday while
  retaining the original driver and investor settlement week.
- **Restore original placement for everyone** reverses the recognition decision.

The original day on Gross Board shows **Late delivered / moved to next week**
without contributing gross, mileage or balance. Next Monday contains the load
once, under its original dispatcher, even after a Monday assignment change.
The moved cell shows its original date. Transferred board entries are read-only;
restore the transfer in payroll before changing their underlying load values.

This is a manual accounting decision. Monday 3 p.m. is not evaluated against
provider schedules, inferred delivery times or document upload times. Deployment
does not transfer existing loads or send any provider mutation.

## Storage and boundaries

Migration 159 adds `gross_board_transfers` and append-only
`gross_board_transfer_history`. Source driver/date/slot and load/trip identity
stay unchanged. Separate dispatch/settlement projections move recognition only.
Unique reserved Monday slots prevent overwrites; original dispatcher ID/name and
resolved truck attribution are retained. Unknown load/trip identities require
review before transfer. Settlement moves also require resolved truck identity;
changed crew or investor terms between the weeks require review.

Payroll and investor load routing use the settlement projection, including
historical lists and cost-carry routing. Hired-driver earned compensation moves
with the investor load. Fuel, toll, scale, expense, escrow, recurring-charge and
manual adjustment date rules stay unchanged. This feature does not implement
dispatcher commission or change its future rate basis; future calculations must
use dispatch placement plus the retained dispatcher, not Monday's assignment.

Both affected weeks' driver/team and investor statements must be open when the
settlement date changes. Dispatcher-only decisions can annotate frozen
statements without rewriting stored reports or monetary amounts. Live placement
annotations remain available on frozen statements so the transfer can still be
managed. Concurrent board/placement versions reject stale requests. The shared
accounting lock serializes transfers against edits and finalization. Database
guards reserve transferred source/destination slots against older clients.

`POST /driver-pay/transfer-load` and `/investor-pay/transfer-load` use their
respective `.edit` permissions. Input includes `driverId`, original `date` and
`slot`, `boardVersion`, `transferVersion`, `mode` (`everyone`, `dispatch_only`,
`restore`), and a required reason. The transaction records actor, before/after
placement and reason. A retry with an obsolete version cannot apply twice.

## Validation

`TestLoadWeekTransfersDatabase` uses local disposable
`MSERP_GROSS_BOARD_TEST_DATABASE_URL`, temporary schemas, fresh/migrated fixtures
and the application database role. Run payroll, investor and Gross Board database
regressions as well. After building the frontend with `/api`, run
`node scripts/test-load-week-transfers.mjs` for both entry points, both modes,
restore, stale responses, finalized/view-only controls and dispatcher totals.
