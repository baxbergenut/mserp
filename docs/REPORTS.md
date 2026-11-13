# Reports

Open `/reports` for Company Truck P&L, Owner-Operator Truck P&L, Dispatcher
Performance, and Driver Performance. Each report provides the selected and
comparison periods, historical truck/driver/dispatcher filters, source records,
and a CSV with both periods. `reports.read` grants the financial views and their
exports. `reports.performance` grants the performance views and their exports.
A source link to another Clustie page also requires that page's permission.
Report requests use read-only snapshots and never initiate provider syncs or
prepare, edit, or finalize payroll. No migration is required.

- Dates are inclusive in New York. The default is the last completed
  Monday–Sunday week with the preceding week as its comparison. Both ranges
  can be changed independently, up to 366 days per range.
- Confirmed gross and miles come from Loads by actual delivery date. Completed
  statuses use the Status Board convention (delivered, completed, invoiced).
  An exact upstream ID takes precedence over a uniquely matching normalized
  load number. Gross Board supplies dated assignments and provisional manual
  evidence. For splits, provider trip gross and miles are used when they sum to the Loads record. Repeat links do not multiply source amounts.
  A completed load without actual delivery is visible for review when another
  source date places it in the period. Conflicting assignment evidence keeps
  attributable totals in Unassigned.
- Dispatcher credit follows the original service day. An approved Gross Board
  transfer changes the dispatcher reporting date and retains the stored
  dispatcher. An active truck-week is one truck with qualifying work for the
  dispatcher in a New York calendar week. Loaded RPM uses gross and positive
  loaded miles from the same records. Deadhead uses matched loaded/empty miles.
- Driver active days are distinct recorded pickup/delivery days in the selected
  period for its confirmed load cohort. On-time rates use matching actual and
  scheduled timestamps, with coverage across eligible completed load records.
  Reports do not estimate MPG from fuel purchases.
- Company P&L uses attributable earned driver pay, production Relay fuel,
  production/legacy tolls, CAT Scale tickets, maintenance expenses, and direct
  equipment expenses. Fuel uses purchase date; tolls use posting date and
  resolve ownership on the exit date. Native maintenance expense records are
  counted once, including expenses reimbursed through payroll. Equipment uses
  expense types Equipment, Equipment Lease/Rental, Truck Lease/Rental/Payment
  and Trailer Lease/Rental/Payment. Shared overhead is outside contribution.
  Unresolved source identity or cost responsibility is visible in Unassigned.
  Where pay or known truck costs need review, dependent metrics stay blank.
- Driver earnings use finalized Driver Pay matched by exact load/trip identity,
  including cross-week statement placement. Owner-operator carrier fees are
  Loads gross less the matched operator load earnings. Carrier contribution
  subtracts carrier-borne operating costs. Weekly settlement rows use finalized
  Driver Pay for weeks starting in the selected period. They show the whole
  statement, with reimbursements, deductions, and escrow movements. Weekly
  amounts with several dated truck assignments are shown in Unassigned.
