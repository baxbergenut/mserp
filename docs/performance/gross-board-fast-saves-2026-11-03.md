# Gross Board save latency follow-up — November 3, 2026

After release 7d585dc, production board reads commonly completed in 156–256 ms,
but sampled writes still took 657–4,301 ms. Financial writes executed 204–464
database calls and six calculator events; even a no-op retry could wait behind
the shared financial work. The typing debounce and query improvements alone did
not remove synchronous payroll publication from the acknowledgement path.

The user chose durable board saves with background payroll preparation, and
explicitly required unrelated payrolls to remain editable and finalizable.

## Commit and preparation boundaries

The interactive SaveChanges path now commits only the canonical board transaction,
including its existing source revisions, capture records and commit notifications.
The existing durable worker coalesces those records and publishes derived payroll.
No request goroutine owns recovery. Internal SaveEntries callers retain their
explicit synchronous publication contract. The 300 ms typing pause is unchanged.

Prepared payroll reads prove complete intervening source capture once per snapshot,
then check each statement's dependency component and current resource revisions.
Only proven current statements receive preparationState=ready. Unknown impact,
missing revisions, incompatible schema/calendar and genuinely related changes
remain unavailable for financial commands. Server commands independently validate
under their existing locks. The UI no longer uses the weekly header's Updating
state as a blanket disable for every row. Aggregate totals remain last-complete.

The change retains financial locks, exact source/trip review and frozen-statement
semantics. Lock waits are logged separately as gross_source_lock_wait and
gross_financial_lock_wait. There is no new migration or version-history store.

## Evidence

- Small local database fixture: 17 database calls, zero payroll-calculation events
  and approximately 11–27 ms per durable board write. These are local API-layer
  measurements, not a promise about production/network latency.
- Fresh and incremental schemas in both global and resource modes: committed
  board reads and live notifications precede payroll preparation; stale finalization
  fails; worker failure/restart preserves the edit; recovered driver/investor
  reports match the independent full reference; frozen reports remain unchanged.
- Independent resource components remain readable/editable/finalizable while
  Gross Board work is pending, including both Driver and Investor Pay.
- Real API/PostgreSQL/browser test with the prepared-payroll worker enabled:
  edit-to-other-screen samples were 463/462/476/495/508 ms locally and
  643/662/662/679/662 ms with 178 ms simulated network RTT, including the
  300 ms typing pause. Same-account tabs, distinct users, reconnects and queued
  typing passed. Browser tests also confirmed independent payroll editing and
  finalization while another statement updates.

## Google design references

Google's historical collaboration description explains immediate local edits,
small change messages and reconciliation through operational transformation:
https://drive.googleblog.com/2010/09/whats-different-about-new-google-docs_22.html

Its Sheets case study documents browser-side calculations in a separate worker:
https://web.dev/case-studies/google-sheets-wasmgc

These support the design principles, not a claim to reproduce all current Sheets
internals. Clustie's authoritative financial calculation remains server-side.
