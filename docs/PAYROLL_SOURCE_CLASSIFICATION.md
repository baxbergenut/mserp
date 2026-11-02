# Payroll source classification audit (192)

Driver/Investor Pay must not rebuild or wait behind unrelated operational saves.
The migration and `payrollUnrelatedTables` use the same reviewed exclusions;
unreviewed tracked tables remain conservative financial dependencies. Ordinary
cache revisions, business guards and audit receipts remain in place.

The audit traced `readDriverPayWeek`, `readInvestorPay`, prepared cost evidence,
their transitive repository SQL and SQL helper functions. It also inspected native
writers and database triggers: absence from the calculator alone is insufficient
when a writer changes canonical financial records indirectly.

| Newly excluded state | Why it is not a current payroll input |
| --- | --- |
| Feedback posts, comments, votes, attachments, messages, status history | Product feedback only. |
| Inventory collections, trailers, stock movements, import plans and events | Equipment tracking only; truck identity and dated assignments remain captured. |
| Maintenance records, documents, odometer readings, schedules, baselines, vendors, types, programs and audits | Costs are canonical `expenses`; work-order status effects write canonical fleet history. Those writes remain captured and the mixed work-order save/delete keeps its financial transaction. |
| Expense/file attachments and Sheets export configuration/delivery bookkeeping | They neither change expense amounts nor collection attribution. Actual expense writes remain captured. |
| Roles, category permissions, access audits and phone normalization audit receipts | Authorization is checked on each request; these records are not statement values. Own access-administration serialization and last-administrator checks remain. |
| FleetScope intake, termination and webhook receipts | Delivery/intake evidence only. Mixed handlers retain source locks for actual fleet changes. Applicant and confirmed driver identity tables remain captured because `expense_applicant_driver` uses them. |
| Relay/PrePass/scale sync-day markers, toll review/link/mapping receipts and toll report import records | Actual fuel, toll and ticket rows and canonical mappings remain captured. Toll review retains row serialization and now uses a repeatable snapshot. |
| Scale memberships, enrollment requests/events/import receipts, audit and retired manual billing preferences | Current calculator uses dated ownership/assignment and actual ticket rows. Reconciliation still captures any newly linked tickets. Legacy billing preferences are explicitly ignored by `wmtChargeFrom`. |
| Dispatcher commission rules, inputs, statements and audit history | Separate Dispatcher Pay records, not Driver/Investor Pay sources. Its actual financial commands retain existing locks. Merely reading commission rules now uses a read-only transaction. |

The exact 56-table inventory is in migration 192. PTI and Status Board operational
tables were already excluded by 191 and 188 respectively. Newly added tables still
require explicit review; missing source coverage continues to prevent activation.

Account appearance/password/session updates use a separate transaction marker.
Every changed user row must preserve id, username, email, role and active status;
mixed or identity edits fail. The dispatcher synchronizer skips these exact metadata
transitions before even issuing an empty dispatcher update. Renames and dispatcher
profile synchronization remain guarded and captured. Startup verifies the metadata
guard, source capture and custom cache triggers are enabled.

Repeated toll/scale updates compare complete old/new rows, excluding only sync
timestamps. Source payloads and all other columns remain dependencies. Identical
Relay purchases preserve derived item/fee rows instead of deleting/recreating
them; the exact JSON payload and confirmed local driver must both match. Actual
provider import batches retain source serialization because they can change money
or attribution. This does not remove financial concurrency checks.

Deployment requires the matching helper first. Migration 192 quiesces API, expense
export and review-portal writers, reclassifies tables, removes their six capture/
protocol triggers, installs exact metadata guards, and requests one rebuild against
the reduced fingerprint. The helper refuses an older binary after this transition.
No financial rows, current statements or audit receipts are deleted.

Validation uses a disposable localhost `_test` database:

```powershell
$env:MSERP_DRIVER_PAY_TEST_DATABASE_URL='postgres://postgres:postgres@localhost:5432/mserp_payroll_storage_test?sslmode=disable'
go test ./internal/repository -run 'TestPayroll(UnrelatedSources|RepeatedImports|PTIClassification|AdditionalWriterGates)Database' -count=1
```

Fresh and incremental fixtures verify native operational saves finish while a
payroll command holds its shared source gate, unchanged report values/input
fingerprints/current pointers/jobs, no preparation events, and ready page reads.
They cover mixed account metadata rejection and positive rename/financial controls.
Repeated native imports must queue nothing; changed fuel evidence, scale amounts
and toll amounts must still invalidate. Existing financial publication, work-order,
provider identity and access tests cover canonical side effects and rollback.
