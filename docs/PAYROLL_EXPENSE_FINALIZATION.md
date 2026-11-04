# Expense choices and statement finalization

Ordinary unpaid expenses appear on open statements independently of invoice date.
The statement stores an automatic, fixed-amount or skipped deduction choice.
Saving a choice does not reserve or collect the expense. Only finalization reduces
the central unpaid balance. Reopening reverses that statement's exact collection
and preserves its choice for the next review. External settlements and balance
corrections retain their separate audited workflows.

Reimbursements, escrow, recurring charges, Fuel/Toll and other source collections
remain separate ledger types. Their validation and posting rules are not replaced
by an ordinary expense choice. Frozen statement JSON and historical receipts remain
authoritative. A fixed choice exceeding the current available amount stays visible
for correction; finalization rejects it rather than silently reducing it.

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
