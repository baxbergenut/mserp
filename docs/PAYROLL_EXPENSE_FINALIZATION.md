# Expense choices and statement finalization

Ordinary unpaid expenses appear on open statements independently of invoice date.
The statement stores an automatic, fixed-amount or skipped deduction choice.
The UI exposes only the amount: a new deduction defaults to the unpaid balance,
typing replaces that amount, and zero skips it. Existing saved amounts remain.
Automatic/fixed/skip modes are internal compatibility details, not a dropdown.
Saving a choice does not reserve or collect the expense. Only finalization reduces
the central unpaid balance. Reopening reverses that statement's exact collection
and preserves its choice for the next review. External settlements and balance
corrections retain their separate audited workflows.

Reimbursements, escrow, recurring charges, Fuel/Toll and other source collections
remain separate ledger types. Their validation and posting rules are not replaced
by an ordinary expense choice. Frozen statement JSON and historical receipts remain
authoritative. A fixed choice exceeding the current available amount stays visible
for correction; finalization rejects it rather than silently reducing it.

## Accessorials and owner reimbursements

Accessorials, including DOT inspection bonuses, are entered through the ordinary
expense form or directly from Driver Pay. The expense date does not assign a
payroll week. Driver credits follow the ordinary unpaid balance and draft choice
rules; finalization writes the separate accessorial payout ledger and reopening
reverses the exact statement receipt. Truck Owner coverage creates an independent
owner collection against the same source expense. Both rows can appear on one
owner-operator statement and have distinct choices.

New Truck Owner reimbursements have independent owner collections; their driver
payout still uses the existing reimbursement window and reservations. Existing
records retain the previous mirror behavior and appear in the read-only Owner
reimbursement review filter. The migration creates no historical owner debt.
The export counts accessorials and reimbursements in separate breakdown columns.
Generic external settlement and balance correction are disabled for linked
expenses to avoid applying them to the wrong side.

Install the updated deploy helper before migration 203. It pauses writers and
requires the binary capability `accessorial-expenses-v1` once the ledger exists.
Run `TestAccessorialPayrollDatabase` using the disposable payroll test database,
then `frontend/scripts/test-payroll-expenses.mjs` against a `/api` build.

## Review and transaction boundaries

Prepared Driver Pay reads overlay current driver-paid ordinary expenses in one
batched query. The statement revision protects prepared wages and source evidence;
`expenseRevision` protects the exact displayed live expense set, versions, amounts
and choices. Finalization validates both under the source gate/resource locks,
locks affected invoices, posts set-based allocations, freezes the reviewed result
and stores the idempotent request receipt in the same transaction. An old review
cannot silently collect a new or changed invoice. Exact retries recover their
original result. Older clients may use a still-exact prepared expense set or the
complete current input revision; they cannot bypass changed-balance validation.

Commit-driven browser refresh compares `expenseRevision` and page aggregates as
well as the statement revision. Live balances can change with no new prepared
generation. Lazy detail requests restart when either review token changes; stale
responses cannot replace the new row. The prepared-payroll browser regression
covers changed visible deductions and off-page totals at an unchanged generation.

A native driver-paid expense mutation in resource protocol can avoid payroll publication only after
every captured event and old/new recipient is proved eligible. Each materialized
week must already have a compatible statement whose membership exists independently
of the expense (loads, saved edits or a frozen statement). Posting uses the same
membership restriction. The precise events are marked live within the transaction;
later source proofs use that recorded classification, not today's payer. Missing
cards, uncertain routing and expense-only membership changes retain full publication.
Investor/owner-routed balances retain regular publication while using the same
finalization-only ordinary expense collection lifecycle.

Local save/freeze publication records the validated current source baseline on
the changed statement's generation. It leaves the weekly header and other
statements' certificates intact. Shadow reconciliation applies the same live
expense overlay before comparing the independently calculated financial report.

Reopening classifies the exact reversed driver expense events using the same
membership proof. Metadata-only resource-mode reopening preserves the other payroll
page. The reopened statement still rebuilds from current sources: frozen values
cannot establish what today's open statement should contain. Recurring confirmation
or non-live ledger changes retain normal impact routing.

Statement-local notes, adjustments and ordinary expense choices do not write
unchanged source/cost ledgers. The comparison uses the locked server card. Explicit
cost overrides, recurring changes and reimbursement/escrow applications retain
their normal validation. Local publication recomputes the statement summary without
recalculating its loads or copying the edit into other periods.

Resource transaction setup retains its before-lock discovery and after-lock
revalidation. Receipt absence is reused only for the exact actor/request while its
session lock remains held. Source proofs are memoized only inside one read-only
validation call; that memo does not escape into posting. Source fences and local
query-plan settings are retained only in the resource transaction that owns them.
Empty escrow and principal-resolution queries are skipped.

## Migration and recovery

Install the release's `deploy/mserp-deploy` before migration 198. The helper stops
writers before the cutover backup and migration and requires the binary capability
`payroll-expense-drafts-v1` whenever `driver_pay_weeks.expense_choices` exists.
Schema detection protects against a migration ledger write failing after schema
commit. Never resume a pre-198 binary against the changed schema.

The migration matches expense payment rows to retained current statement contents.
Confirmed open allocations become fixed choices (zero becomes skip), and their
old draft payment rows are removed transactionally. Finalized and unreferenced
historical payments are untouched. Ambiguous ownership or conflicting finalized
state aborts the migration. A failed migration must be investigated rather than
resolved by guessing a historical owner. Restoring the cutover backup requires
writers to remain stopped and the existing deployment recovery procedure.

## Validation and measurements

Use a disposable localhost database ending in `_test`, with
`MSERP_DRIVER_PAY_TEST_DATABASE_URL` set. Focused checks include:

```powershell
go test ./internal/repository -run 'TestPayrollExpenseDraft|TestPayrollLiveDriverExpense|TestPayrollStatementLocalSave|TestPreparedPayrollExpenseWindow|TestPayrollResourceSharedBalance|TestPayrollResourceSourceScope' -count=1
```

The populated cutover fixture checks frozen and unreferenced payment preservation,
fixed/zero choices, investor choices and atomic rejection of ambiguous ownership.
Lifecycle fixtures cover partial/zero deductions, EFS fees, dated teams, owner
routing, stale review rejection, finalization races, reopen/refinalize, rollback
and legacy/prepared financial parity. Browser checks include
`frontend/scripts/test-payroll-expenses.mjs` after a production `/api` build.

The initial small-fixture diagnostic fell from 57 to 47 database calls for a
driver-paid expense finalization, with zero payroll calculations. A native expense
edit used 9 calls and zero calculations. These counts include the transaction's
locking, freshness validation, posting, frozen publication and receipt. They are
not production latency percentiles or a completed performance acceptance result.
The optional `MSERP_PAYROLL_SQL_DIAGNOSTIC=1` records SQL only in disposable tests;
production instrumentation records counts and fingerprints, not argument values.

The 47-call diagnostic breaks down into 18 transaction/resource/receipt setup calls,
13 current-statement/source checks, five expense-posting and balance/proof calls,
eight freeze/publication calls, and three receipt/commit calls. Only three calls
directly lock/post/touch the ordinary invoices. Counts include BEGIN/ROLLBACK/COMMIT
and do not count SQL executed inside PostgreSQL triggers. Before/after-lock resource
discovery is deliberate; removing it admits changes between discovery and lock acquisition.

Migration 198 also replaces quadratic resource claim/lock comparisons with set
differences. All epoch, declared-claim, domain-gate and actual advisory-lock checks
remain. A rollback-only SQL measurement with 3,803 locked resources reduced the
expense-payment trigger from 860 ms to 23 ms (complete insert: 905 ms to 60 ms).
`TestPayrollResourceClaimSetsDatabase` rejects missing claims/locks/gates, obsolete
epochs, empty sets and NULL keys on fresh and incrementally migrated schemas.

Three days of production HTTP logs ending November 4, 2026, before this deployment
showed the following mixed-release timings. Errors remain in these distributions;
SSE connection lifetimes, presence requests and ancillary routes are excluded.
These are server request durations, not browser interaction latency.

| Route/scenario | Method | Requests | Errors | Median ms | p95 ms |
| --- | --- | ---: | ---: | ---: | ---: |
| Status Board | GET | 11,304 | 18 | 321 | 2,967 |
| Status Board | PUT | 971 | 49 | 92 | 11,589 |
| Gross Board | GET | 2,313 | 11 | 1,524 | 6,460 |
| Gross Board | PUT | 365 | 36 | 362 | 13,756 |
| Driver Pay summary | GET | 1,787 | 54 | 524 | 4,576 |
| Driver Pay summary with details | GET | 14,234 | 109 | 942 | 4,847 |
| Driver Pay save | PUT | 108 | 20 | 2,205 | 13,071 |
| Driver Pay finalize | POST | 100 | 6 | 975 | 6,664 |
| Driver Pay reopen | POST | 19 | 1 | 2,125 | 5,534 |
| Investor Pay summary | GET | 300 | 5 | 220 | 1,161 |
| Investor Pay summary with details | GET | 107 | 1 | 472 | 1,168 |

There were no Investor Pay PUT/finalize/reopen requests in this captured window.
Board optimizations already on main are separate from this expense lifecycle change.

### Large local HTTP run, November 4

The isolated fixture has 176 drivers/trucks/investors, 7,392 loads, 6,000 expenses,
6,000 tolls and 11,449 fuel transactions, with dense historical assignment links.
It retains the production first-week boundary of October 19. The run exercised
36 driver statements and 27 eligible investor statements, twice each, through the
authenticated HTTP API. All 702 recorded requests succeeded. Times below are local
HTTP wall times; they do not establish production p95 or include WAN latency.

| Action | Samples | Median ms | p95 ms | Median DB calls |
| --- | ---: | ---: | ---: | ---: |
| Driver expense PUT | 72 | 20 | 69 | 25 |
| Driver summary GET, first cycle | 36 | 81 | 108 | 17 |
| Driver detail GET, first cycle | 36 | 30 | 40 | 19 |
| Driver notes PUT, first cycle | 36 | 106 | 170 | 48 |
| Driver first finalize POST | 36 | 991 | 1,193 | 186 |
| Driver repeat finalize POST | 36 | 202 | 319 | 46 |
| Driver reopen POST, first cycle | 36 | 530 | 640 | 99 |
| Investor summary GET, first cycle | 27 | 42 | 46 | 25 |
| Investor detail GET, first cycle | 27 | 28 | 32 | 26 |
| Investor notes PUT, first cycle | 27 | 91 | 112 | 37 |
| Investor finalize POST, first cycle | 27 | 130 | 157 | 43 |
| Investor reopen POST, first cycle | 27 | 467 | 556 | 110 |

Expense saves, notes saves and repeat finalization performed zero payroll
calculations. First driver finalization posted other ledgers and performed up to
four calculations across the two retained weeks. Reopening performed one current
statement calculation. Repeat reopen p95 was 727 ms for Driver Pay and 595 ms for
Investor Pay. The 300 ms target is **not achieved for every operation**. The next
performance work is the non-live cost/escrow posting and prepared projection
publication path; do not remove financial fences to claim that target. No production
expense or statement was modified for these measurements.
