# Accounting cutover: October 26, 2026

Migration 160 installs an **inactive** cutover. Deployment does not activate it,
delete records, finalize statements, send money, or write financial balances to
Sheets. Settings > Accounting cutover is restricted to the built-in Administrator.

## Preparation and activation

1. Keep the deployment database backup and save dated copies of the authoritative
   Sheets. Enter both references in the draft. The screen records references; it
   does not itself create a backup or copy a Sheet.
2. Keep the first Clustie week at **2026-10-26**. Enter a reviewed opening **rate
   balance** for every listed driver, with its Sheets reference. A known zero is
   an explicit `0`; an empty value remains unresolved. TSV paste accepts exact
   driver UUID, amount and reference, without name matching.
3. Review Monday–Wednesday expenses against the last Sheets payroll. Preview
   links open the expense; **Keep in Sheets** adds it to the draft's legacy list.
   IDs can also be pasted for already-reviewed overlap expenses.
4. Save and preview. Resolve blockers in the ordinary accounting screens. The
   preview lists saved current/future statements, changed payables, legacy
   collections, existing escrow/installment collections, cross-boundary transfers
   and post-cutover finalized statements. Notes and load edits are retained.
5. During a short accounting pause, generate a fresh preview, review all items,
   check the explicit accounting acknowledgement, and activate. A changed draft
   or source revision rejects activation. Source table locks and the financial
   transaction lock fence concurrent imports; lock waits time out after five
   seconds, so retry during the pause instead of leaving accounting blocked.
6. Validate the first October 26 statements against the reviewed sources before
   paying them. Do not bulk-finalize history to improve application performance.

Activation retains an immutable receipt containing the reviewed draft, preview,
source revision and administrator. The date is immutable afterward. The preview
captures retained historical reports once; archived reads use those copies.
Existing finalized snapshots remain finalized, while historical drafts remain
drafts. Very large or inconsistent historical data can make preparation slower
than ordinary weekly reads; preview errors must be investigated before activation.

## Financial boundary

| Source | Treatment after activation |
| --- | --- |
| Earlier Gross Board and payroll weeks | Read-only, **Managed in Sheets** |
| Earlier expenses and explicitly classified overlap | Excluded from active lists and automatic payroll; Show history retains access |
| All existing escrow and installment plans | Managed in Sheets, including future scheduled collections; do not restart |
| Pay rates, assignments, ownership evidence and recurring configuration | Retained; normal current weekly recurring charges continue |
| New Clustie obligations | Ordinary partial payments and unpaid carry continue beyond the first week |
| Gross Board opening | Reviewed rate balance only; never payroll earnings or debt |

Source timing stays specific: Relay fuel uses merchant-local purchase dates,
PrePass uses posting weeks and dated crossing attribution, CAT Scale uses ticket
dates, and expenses remain eligible through the following Wednesday. The fixed
boundary excludes older obligations before carry reconstruction. It does not
discard unpaid amounts every week.

Late-posted tolls whose crossing predates cutover (or is unknown) are held out of
automatic payroll and produce a weekly review notice. Accounting reconciles them
in Tolls/Sheets and enters a referenced adjustment if collection is still needed.
Newly imported historical fuel/scale transactions stay in their historical weeks.
Historical financial edits and backdated native expenses are rejected server-side;
record corrections as current adjustments instead of reintroducing legacy debt.
Transfers whose source is before the boundary but dispatch or settlement is after
it must be resolved before activation; old slots cannot later be moved across it.

## Weekly accounting

Dispatch finishes Gross Board; accounting reviews new earnings and charges, then
checks Sheets for legacy collections, escrow movements and credits. **Sheets
adjustment** adds a named deduction or credit with a required reference. It uses
normal versioned payroll saves and is retained in finalized reports and the
statement CSV export. Example: $2,000 payable minus a $150 legacy collection means
$1,850 final payout; accounting separately reduces the Sheets balance by $150.
There is no automatic financial synchronization back to Sheets.

**Record outside Clustie** on an active expense records an actual full or partial
external recovery/reimbursement: amount, actual date and reference are required.
It does not issue money. `expenses.settle` and expense-category View access are
required. The action respects existing payment reservations, remaining principal
and finalized statements. Retries reuse a request UUID. Reversals append a full
reversal receipt with a reason; they never erase the original record. Expenses
with receipts retain their identity and principal. Legacy expenses stay in Sheets.

Administrator rate-opening corrections append signed, referenced receipts and
require the current opening value. The original activation opening stays intact.
This also permits an explicitly reviewed opening, including zero, for a new
driver. Corrections affect rate balances only; the cutover date does not move.

## Contracts and validation

- `GET/PUT /accounting/cutover`: versioned draft and active status.
- `POST /accounting/cutover/preview`: retained, immutable preview ID.
- `POST /accounting/cutover/activate`: preview ID plus explicit `reviewed: true`.
- `POST /accounting/cutover/openings`: idempotent signed correction with expected
  opening amount (`null` only when unresolved), driver, reference and request ID.
- `GET/POST /expenses/{id}/external-settlements`: audited external receipt ledger;
  a reversal references its exact original receipt.
- Payroll, Gross Board, escrow, installments and expenses expose management status;
  Gross Board exposes reviewed opening provenance.

Run `TestAccountingCutoverDatabase` with `MSERP_CUTOVER_TEST_DATABASE_URL` pointing
to a disposable local `_test` database. It covers fresh/migrated schemas, stale
previews, retained records, partial carry, reservations, reversals, finalized
protection, immutable dates, opening corrections, late toll notices and exact
summary/detail parity. Existing payroll/team/ownership/expense suites remain part
of the CI database run.

Build the frontend with `/api`, start `scripts/review-local.mjs`, set
`MSERP_REVIEW_ACCESS_FILE` to its private access file, then run
`node scripts/test-accounting-cutover.mjs`. The script activates only that synthetic
schema and checks the real UI, API, CSV export and authorization boundaries.
`scripts/measure-cutover-payroll.mjs` measures browser actions with configurable
added API latency (175 ms by default), including five concurrent sessions and
synthetic source updates. See [PAYROLL_PERFORMANCE.md](PAYROLL_PERFORMANCE.md) for
benchmark scope and unmet cases.

Install the updated `deploy/mserp-deploy` helper before migration 160. Its rollback
guard rejects application releases without this migration, even while inactive:
external receipts can exist before activation and old code would ignore them.
No automatic rollback may remove the accounting boundary or ledger semantics.
