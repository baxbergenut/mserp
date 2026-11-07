# Driver Pay escrow settlement

Accountants can add a driver to any selectable completed week, including a week
after termination with no loads or wages. A dated team member is represented by
the main driver's statement. The add and remove actions require Driver Pay edit.
An untouched manual addition can be removed until it is first edited or finalized.

Manage escrow requires Driver Pay edit and escrow read/create/edit. A statement
draft covers one escrow account. It supports returns through payroll and payment
of the driver's own ordinary expenses. The expense's salary deduction defaults to
the unpaid amount left after escrow. An explicit salary deduction remains a fixed
choice and must be within that remainder. Fuel/Toll, recurring charges, and salary
carry use their existing payroll treatments.

A terminated driver defaults to stopping collection when the chosen week is
after termination. Stops apply from the selected week and resumes from the next
week. A stop keeps remaining funds available and persists through rehire. Positive
saved collections that conflict with a stop must be reviewed and cleared first.
An active driver maintaining escrow can replenish it from the following week.

Drafts make no payments or reservations. The draft's account version, the reviewed
payroll and expense revisions, and the dated escrow funding checks fence
finalization. Legacy scheduled releases reserve funds and share the same funding
guard. Statement escrow writes use the exclusive source gate because they alter
cross-week release and funding state. Ordinary finalizations retain their
resource-lock path. The posting and freeze commit together. Reopening reverses
the exact statement posting and preserves the instructions. Later finalized
statements must be reopened first. Exact request retries use payroll receipts.

The escrow history shows what went to the driver, what paid charges, and the
collection policy change. Expense payment history shows the portion funded from
escrow. Driver Pay exports include the return and expense portions when relevant
to the exported payables.

Install the updated deploy helper before migration 204. It quiesces writers and
rejects rollback to a binary that cannot read statement escrow movements. The
migration extends existing captured tables and leaves existing balances and
release receipts in place.

Run the database test with a disposable local MSERP_DRIVER_PAY_TEST_DATABASE_URL
and the browser scenario after a /api build with MSERP_INVESTOR_TEST_DATABASE_URL.

```text
go test ./internal/repository -run TestDriverPayEscrowSettlementDatabase
node scripts/test-investors-e2e.mjs --escrow-settlement-only
```
