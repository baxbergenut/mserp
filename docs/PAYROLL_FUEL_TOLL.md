# Fuel and toll statement details

Driver Pay and Investor Pay Fuel/Toll labels open a read-only transaction modal.
The authenticated endpoints are GET `/driver-pay/{id}/cost-transactions` and
`/investor-pay/{id}/cost-transactions`, with `weekStart` (Monday) and `cost`
(`fuel` or `toll`). They require the respective statement read permission and
return only the selected statement's financial evidence, without provider
credentials or raw payloads. Reads never save deductions or call providers.

Fuel includes production Relay diesel, DEF and credits, using merchant-local
purchase dates and confirmed driver/team links. Tolls use posting dates for
the week and crossing dates for historical assignment. The modal shows both
dates. Native truck aliases, dated owner/hired routing and team destinations
match the statement calculator; ambiguous driver assignments are never guessed.
Finalized statement amounts remain frozen. If current evidence no longer
matches a saved total, the modal identifies the discrepancy.

Carried rows show their original week, original amount and remaining amount.
Partial collections and source credits offset the oldest transaction amounts
first, in cents. This is a display allocation of weekly collections, not a
provider payment receipt. Missing or changed evidence appears as an explicitly
unitemized historical balance, never a fabricated vendor transaction.

## First live week

Migration 173 creates an inactive, immutable `payroll_cost_start` receipt and
registers its cache revisions. Deployment alone does not discard any balances.
An explicitly authorized operator activation inserts the first live Monday,
under payroll's financial advisory lock (`736281940`), with a reason:

```sql
BEGIN;
SELECT pg_advisory_xact_lock(736281940);
INSERT INTO payroll_cost_start(week_start,reason)
VALUES('2026-10-19','Authorized first live payroll week; exclude older Fuel/Toll balances only');
COMMIT;
```

For that week and later, earlier driver cost collections/legacy overrides and
investor operating-cost sources are excluded from carry. Historical weeks still
show their historical balances. Later saved partial/zero collections continue
carrying normally. Expenses, recurring deductions, escrow and installments are
unaffected. No ledger or transaction is deleted and no settlement is reopened.
First-live-week frozen reports omit obsolete opening-balance display metadata
on read while retaining their saved deductions and immutable database JSON.

Install the updated deployment helper before activation. Once the receipt is
active, releases without migration 173 cannot be used for rollback: they would
restore unwanted historical carry. Take the normal deployment backup and retain
the activation receipt with the release verification evidence.

Validation: `TestPayrollCostStartDatabase` with local disposable
`MSERP_COST_TEST_DATABASE_URL`, `TestCostCarryTransactionAllocation`, and
`frontend/scripts/test-payroll-expenses.mjs` against a `/api` frontend build.
