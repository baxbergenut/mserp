# Driver termination and truck assignments

Terminating a driver clears their dispatcher. An assignment to a company or
another owner's truck ends using the selected assignment date, preserving the
truck's active flag and assignment history. An existing assignment to the driver's
own truck remains. Recorded ownership through the driver's investor identity is
authoritative; the legacy owner-operator boolean is ignored.

An active investor's fleet is independent of their linked driver's employment.
For an active investor, only trucks they currently operate themselves enter
driver offboarding. Trucks operated by hired drivers, and unassigned investor
trucks, stay unchanged and do not receive driver-termination review tasks.
The owner's existing driving assignment is the evidence; an active investor
record alone is not an owner-operator classification.

Eligible owner-operated trucks become inactive, except trucks with an active
direct driver or an active member of a current driver team. Those trucks stay
unchanged and receive an offboarding review task. Review task visibility follows
Settings > System tasks > Driver offboarding, including Administrator access.
Completing a task does not itself change a truck; review the crew and explicitly
update the truck through the ordinary fleet editor when appropriate.

All three entry points (full driver editor, Employment status, FleetScope webhook)
use the same transaction helper. Migration 148 records a unique outcome per
termination/truck, including before/after evidence, source and actor when known.
Held outcomes remain held on retries; removing an active driver does not silently
inactivate a previously held truck. New employment terminations get new identities.
Inactivation appends a truck profile note and uses the ordinary status triggers.
Saving unrelated driver/truck details preserves the retained owner-driver link.

Retained links do not activate drivers or trucks. Terminated drivers remain
excluded from the live dispatch roster; new assignments still require an active
driver and truck. Rehiring a driver does not reactivate equipment or restore a
dispatcher. Ownership, loads, finalized statements, team history, recurring truck
fees and existing provider offboarding rules remain separate. No provider mutation
is added by this feature.

Recurring truck charges stop from the Monday after an owner-driver's termination
date. The Recurring Charges page hides the affected truck for those weeks, charge
edits reject it, and both Driver Pay and Investor Pay omit new recurring fees.
The termination week remains eligible. This follows recorded dated ownership and
the owner's operating assignment or offboarding receipt; an inactive investor's
fleet is also subject to the cutoff. Continuing active-investor fleets and trucks
with employed direct/team operators retain their fees. Home, Vacation and other
availability changes do not stop charges. Stored charge phases, settlement terms,
other costs and finalized reports are preserved; viewing earlier weeks still
uses their historical configuration. No cleanup or provider write is required.

`TestTruckChargeTerminationDatabase` covers the cutoff and recurring API/payroll
behavior on fresh and migrated schemas with `MSERP_DRIVER_CHARGES_TEST_DATABASE_URL`.

## Existing-record cleanup

Schema deployment alone does not backfill existing records. With the normal API
service environment, run the deployed binary without starting the service workers:

```sh
mserp-api -termination-cleanup-preview
mserp-api -termination-cleanup-apply <exact-preview-revision>
```

The preview is read-only. Apply verifies the SHA256 under the fleet/assignment
locks before writing. It clears terminated drivers' dispatchers, ends their
non-owned truck assignments, and records each terminated owner's truck outcome.
Current own-truck links remain; unassigned trucks stay unassigned. Corrections
use today's New York assignment date, not an invented historical effective date;
the ordinary finalized-payroll guards still apply. Before/after records and private
review tasks commit atomically. A stale revision or any failed write rolls back
the entire batch. Review held trucks explicitly rather than rerunning to force them.

## Validation

`TestTerminationOffboardingDatabase` uses local disposable
`MSERP_FLEETSCOPE_TEST_DATABASE_URL` and exercises fresh/incremental schemas as
`mserp_app`. It covers all termination entry points, retained links, company truck
release, active direct/team conflicts, review-task privacy, rollback, rehire,
snapshot fencing and idempotent cleanup. `TestFleetScopeDatabase` retains the
existing signed intake/offboarding, identity and charge-pause regression coverage.
After a `/api` frontend build, run `node scripts/test-termination-e2e.mjs` with
the local review access file and database environment used by `review-local.mjs`.
