# Ownership and expense payer corrections

Truck ownership and expense responsibility are separate decisions. A hired
driver can operate an investor's truck without owing its recurring fees or repairs.

## Ownership effective dates

Saving the owner or correcting their start date recalculates saved recurring-fee
allocations from the Monday containing the selected ownership date. This uses
the existing weekly rule: ownership at the end of the week supplies that week's
recurring-fee destination. Truck fee phases remain configured for the truck.

Allocations for a different dated owner are cancelled, with their original base,
collection, destination, actor and effective date retained in
`truck_settlement_events` (`ownership_fee_correction`). Earlier unpaid debt stays
with the former owner. Later payments can collect that earlier debt only; partial
payments and explicit zero deferrals remain bounded by the corrected balance.
Fuel/tolls, personal expenses, and legitimate fees before the selected week are
not changed. Re-saving the same owner/date also repairs stale allocations left
by an older release. Affected finalized statements must be reopened first.

## Changing an expense's responsible person

Use the existing expense editor's **Who will cover** field. Category Edit access
continues to govern edits. For an expense already deducted in open payroll,
saving a payer change performs one transaction:

1. Lock financial writes and validate the invoice version and both destinations.
2. Remove the old weekly allocations through the ordinary database guards.
3. Update the expense responsibility through the ordinary expense validation.
4. Restore each allocation on the new destination with its original week and
   amount, including partial deductions and explicit zero deferrals.
5. Record the old responsibility, allocations, new destination and actor in
   `access_audit` (`expense.payer_corrected`).

Driver responsibility uses Driver Pay, including existing dated team routing.
Truck Owner responsibility transfers saved deductions to the linked truck's
Investor Pay statement. Company responsibility releases the personal deduction.
An existing agreement/statement naming another owner requires review.

An expense-only investor statement exposes transferred historical deductions
when no agreement existed that week. It shows a review issue and never invents
earnings, a share percentage, or recurring fees. Configure an explicit dated
agreement before finalizing a complete truck settlement. A saved investor
deduction also keeps an owner-operated week's destination on Investor Pay.

Payer corrections retain the invoice date, truck, category, method and total.
Save other financial corrections separately. Reimbursements, historical settled
records and external settlement receipts retain their existing protections.
Finalized source/target snapshots must be reopened before a transfer. A failure
rolls back the complete operation. Payroll and invoice versions reject stale
browser saves; normal revision triggers invalidate cached reports.

## Validation

Run `TestOwnershipFeeCorrectionsDatabase` and `TestExpensePayerCorrectionsDatabase`
with `MSERP_DRIVER_CHARGES_TEST_DATABASE_URL` targeting a disposable localhost
`_test` database. They exercise fresh and incrementally migrated schemas as
`mserp_app`. Run the broader truck/expense/pay-source suites and `go vet ./...`.

After a frontend `/api` production build, run
`node scripts/test-driver-charges-e2e.mjs --payer-corrections-only` with the same
isolated database. This exercises the actual truck and expense editors and both
payroll pages, including a historical expense predating the investor agreement.
