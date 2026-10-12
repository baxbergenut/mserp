# Driver teams

Configure a team in the main driver's profile under Overview > Driver team.
Select one existing active team driver and an effective Monday. Select Solo driver
to end a team, or choose a replacement partner. A save replaces later planned team
changes for that main driver. Team history and an actor-stamped before/after audit
remain in the database. Changes use optimistic versions and the payroll lock.

One driver can belong to only one two-person team in a week. A main driver cannot
also be a team member. Each person keeps their profile, documents, status, Relay
identity, and individual offboarding/access workflow. Team membership does not
grant a second truck assignment. Dispatch loads and operational board entries
belong to the main driver, whose existing tariff determines the shared earnings.
Team drivers are hidden from the effective week's Gross Board and the current
Status Board. Earlier solo work remains available.

Driver Pay produces one statement in the main driver's name and shows the partner.
Both names find that statement in payroll search. Production Relay diesel and DEF
items, including credits, follow confirmed identities and merchant-local purchase
dates. The team period resolves their payroll recipient without changing the
source transaction's driver. Cash advances, fees, and other products retain the
existing exclusion. Investor truck routing still takes precedence per transaction;
costs cannot appear on both driver and investor statements. Financial reporting
uses the same effective recipient for diesel responsibility.

Recurring charges are configured only on the main driver. Both the recurring
matrix and database prevent enabling them for a team member in a team period.
Existing recurring phases must be paused and unpaid recurring balances settled
before pairing. Existing manual expenses and installments retain their individual
principal ledgers but are collected through the main statement while paired.
Only the main driver's escrow is collected; a team member's earlier escrow account
is retained for solo weeks. Escrow release credits and CAT Scale driver charges
remain visible on the shared statement when applicable.

Before pairing, move the prospective member's affected loads/truck assignment to
the main driver. Existing personal payroll and escrow collections cannot be hidden
by a backdated pairing. Outstanding personal Fuel/Toll debt must be settled first.
Finalized driver/investor statements and later saved Fuel/Toll collections prevent
earlier membership corrections. Same-week open collections rebase atomically;
explicit deductions must still fit the new sources. Choose a later Monday for an
ordinary partner change. A finalized main statement freezes both people's displayed
deductions and protects edits to their underlying ledgers until reopened.

Ending a team does not shift prior costs or unpaid main-driver Fuel/Toll carry to
the former partner. Termination does not automatically rewrite membership: the
profile and affected payroll week flag terminated team members for review, and an
accountant chooses the appropriate end Monday. A later termination does not flag
earlier work. Midweek switches are deliberately represented at a Monday boundary;
review any exceptional split explicitly before finalizing.

Migration 078 is additive and creates no teams automatically. Once teams have been
configured, prefer a forward fix: older binaries do not understand their payroll
routing and can display separate statements despite retaining the new schema.

Validation: `TestDriverTeamsDatabase` uses fresh/migrated disposable local
`MSERP_DRIVER_PAY_TEST_DATABASE_URL` schemas as `mserp_app`. Browser validation is
`node scripts/test-investors-e2e.mjs --teams-only`, with the usual local test database
and `/api` frontend build. It covers profile setup, boards, one statement, search,
Relay fuel, single escrow, autosave, stale writes and narrow layouts.
