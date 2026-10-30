# Gross basis and separate settlements

Customer clarification recorded October 8, 2026.

## Total gross, driver gross and dispatch adjustments

Gross Board total gross and driver gross can differ intentionally. Dispatchers
may enter less driver gross on a higher-paying load, preserving a difference
that can later supplement driver gross on a lower-paying load. This difference
is money held between load-rate adjustments; it is not automatically company
profit, dispatcher commission, driver earned pay or investor earnings.

The existing Gross Board balance tracks total gross minus driver gross over
time. A positive difference increases the available adjustment balance; a load
whose driver gross exceeds total gross uses that balance. Do not create an
additional deduction or payout from the difference in either statement.

All percentage-based driver and investor settlement calculations use **Gross
Board driver gross**. Never substitute total gross. Existing CPM contracts
remain miles multiplied by the agreed CPM rate; this clarification does not
convert a CPM driver to percentage pay. Dispatcher commission and the separate
financial dashboard are outside this change and should not be treated as the
source of driver/investor statements.

Example: total gross of $3,000 and driver gross of $2,800 leaves $200 in the
adjustment balance. If a later $1,500 load is entered at $1,700 driver gross,
that $200 is used. A percentage settlement uses $2,800 and $1,700 respectively.
The $200 must not also be booked as an extra settlement deduction or earning.

## Driver and investor statements

The customer requires **separate statements**, including for driver-investors.
Driver Pay remains the personal driving/owner-operator settlement. Investor Pay
contains truck ownership settlements under explicit effective-dated agreements.
Profiles may show both histories, but must not merge the amounts into a new
combined payable or collect either obligation twice.

For an investor truck-week:

1. Attribute each Gross Board load to its historical truck using the linked
   source unit or an unambiguous dated assignment.
2. Multiply Gross Board driver gross by the truck's agreed owner percentage.
3. Deduct hired-driver earned compensation, before that driver's personal
   deductions. Frozen driver earnings take precedence over later tariff edits.
4. Deduct owner-responsible fuel, tolls, truck charges and expense collections;
   apply explicit credits and adjustments once.
5. Sum exact rounded amounts into the truck statement and its profile summary.

Owner-driven-only weeks retain the existing Driver Pay route. Mixed owner/hired
weeks retain one investor truck statement and deduct only hired labor. Saved
investor truck-weeks keep their destination during corrections. This routing
does not imply that driver and investor statements should be combined.

For a truck owned by a linked driver without investor settlement terms, recurring
fees stay on the recorded owner's Driver Pay. A load attributed to another driver
or recorded work without a full-week owner assignment produces a statement review warning; it
does not remove the fees. Load-based eligibility still chooses the configured
working/idle insurance rate. The existing inactive-driver and frozen-statement
rules remain in effect, and ordinary finalization requires resolving review issues.
An idle owned truck does not need an operating assignment, even when its owner
drives another truck. Its eligible recurring fees remain without an assignment warning.

Truck terms must specify the owner, percentage and effective Monday. No rate is
assumed. There is no separate inferred company-cut deduction. Missing terms or
ambiguous attribution require configuration/review, not a fabricated zero.

Drafts use the live calculation. Finalization records a frozen report; reopening
requires a reason and preserves the existing audit/payment rules. Profile
summaries read those same reports and link to the matching week/truck in Investor
Pay. Reading a profile or statement never creates payments. Finalization is not
a disbursement status; bank/payment tracking would be a separate feature.

## Expense collection window

Driver Pay and Investor Pay include Expenses & Charges dated through the Wednesday
after the selected week ends. For example, September 28–October 4 includes new
expenses dated September 28–October 7; October 8 is outside that window. Older
unpaid balances continue to carry forward. This extension does not change load,
fuel, toll, CAT Scale, escrow or recurring-charge dates.

Reading a statement does not collect money. Saving or finalizing a deduction
reserves that amount across all weeks and settlement destinations. Fully paid
expenses disappear from later unsaved statements; partial payments leave only
the available remainder. Saved historical deductions and finalized snapshots
retain their recorded amounts. Overlapping open statements can suggest the same
unpaid expense, but a stale save must reload after another week collects it.

EFS expenses payable by a driver or truck owner include $3 for each started $500
in their collectible balance (for example, $500 is $503; $500.01 is $506.01).
Fee-aware entries already store that total. Legacy/imported entries without a
fee breakdown retain their entered expense amount; payroll adds the missing fee
to the balance without rewriting the expense. The fee applies once to the full
expense, including when collection spans weeks or moves between statement routes.
Zero amounts, company-paid expenses, driver-only Penalties categories and
assumed-paid history receive no additional fee. Saved deductions and finalized
snapshots retain their recorded amounts; any unpaid fee remains available in
later open weeks. Payment/category metadata cannot change a collected balance.

## Identity, activity and review

Full vacation weeks do not generate or collect recurring truck fees. Determine
Monday–Sunday eligibility from recorded availability transitions in New York
calendar dates; partial vacation weeks keep their ordinary fee. Planned return
dates do not end a vacation. Fees resume after an actual return, without adding
missed vacation fees. Existing balances remain in their ledgers during leave.
For trucks, all dated operators (including team members) must be on vacation;
a working replacement keeps the truck eligible. An idle driver-owned truck
without operators follows its recorded owner's vacation status.

Gross Board, Driver Pay and Investor Pay omit idle full-vacation rows, including
rows recreated by prior autosaves, installments, expenses or carried balances.
Actual weekly loads and finalized statements remain accessible;
their presence does not restore recurring fees for a full vacation week.
Historical vacation weeks remain exempt after return. Unknown legacy vacation
starts apply from the current week, without inventing earlier leave dates.

Driver activity and investor activity are independent. An inactive driver may
remain an active investor; ownership alone does not create personal driving pay.
Existing weekly work and saved personal statements remain historical records.
The Investor Pay driver column shows hired operators; the investor's own identity
is already in the Investor column. Actual owner-driven loads remain in details.

Loads retain their stable indexed source record IDs, and migration 064 adds a
stored, indexed truck ID. Unit labels remain source snapshots. Import resolves
unique current/prior labels and retains established IDs across truck renames and
unchanged source refreshes. Reused ambiguous labels stay unresolved; no report
may guess a new identity. Only a missing unit permits dated-assignment fallback.
Warnings are grouped on affected truck statements; unrelated fleet loads do not
block every investor. Finalization still rejects unresolved affected statements.

Fuel Truck # prompts and imported toll equipment units resolve through unique
current/prior truck aliases. A rename must not remove fuel from a truck statement
or prevent future toll imports from finding that truck. Ambiguous aliases remain
unallocated. Assignment corrections do not rewrite provider fuel history; any
explicit transaction correction needs its own retained original and audit reason.

The regular Investor Pay list and whole-week actions contain active trucks only.
Profile history and explicit statement links preserve access to inactive trucks'
saved and historical statements, including their frozen finalized amounts.
