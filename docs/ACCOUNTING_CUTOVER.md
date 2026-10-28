# Accounting cutover retired

The administrator cutover workflow was removed on October 28, 2026. Its Settings
screen and draft, preview, activation and opening-correction APIs no longer exist.
The rollout used a separately authorized operational cleanup instead: Gross Board
starts October 19; eligible expenses through October 21 were assumed handled in
Sheets. Backups and audit receipts retain the cleanup evidence. Installing this
release never repeats that cleanup or settles additional expenses.

Migration 161 refuses any populated/activated cutover, permanently rejects changes
to its inactive singleton and removes cutoff-specific write guards. Empty tables
and constant SQL compatibility functions from migration 160 are retained so older
payroll queries and compatible rollback releases remain safe. Applied migration
160 is not edited or reversed: it also supplies external expense settlement
receipts and financial cache revision tracking that are still used.

Ordinary partial-payment carry, finalized-statement protection, and external
settlement and reversal APIs remain available. The special Sheets adjustment and
Export statement controls were also removed from both payroll pages. Existing
adjustment amounts and references remain stored and visible in ordinary rows.
No ongoing date cutoff discards later expenses or debt. Payroll summary/detail
caching and grouped balance queries remain in place; see PAYROLL_PERFORMANCE.md.

Validation: TestAccountingCutoverRetiredDatabase with
MSERP_CUTOVER_TEST_DATABASE_URL pointing to a disposable local _test database;
frontend/scripts/test-accounting-retirement.mjs against review-local.
