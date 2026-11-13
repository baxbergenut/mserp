#!/usr/bin/env bash
set -euo pipefail
helper=${1:-$(dirname "$0")/mserp-deploy}
bash -n "$helper"
permissions_migration=112_permissions_v2.sql
user_permissions_migration=217_user_permission_overrides.sql
manager_verification_migration=151_retire_expense_manager_verification.sql
accounting_cutover_migration=160_accounting_cutover.sql
payroll_cost_start_migration=173_payroll_cost_start.sql
expense_balance_migration=176_expense_balance_corrections.sql
payroll_projection_migration=182_payroll_projections.sql
payroll_telemetry_migration=183_payroll_telemetry_classification.sql
payroll_operational_migration=184_payroll_operational_sources.sql
payroll_shared_migration=185_payroll_shared_dependencies.sql
expense_verification_migration=186_expense_verification_metadata.sql
load_availability_migration=187_load_source_availability.sql
board_operational_migration=188_status_board_operational.sql
pti_operational_migration=191_payroll_pti_classification.sql
unrelated_payroll_migration=192_payroll_unrelated_sources.sql
board_stop_geocoding_migration=216_status_board_stop_geocoding.sql
temp_root=$(cd "${TMPDIR:-/tmp}" && pwd -P)
test_root=$(mktemp -d "$temp_root/mserp-compat.XXXXXX")
trap 'case "$test_root" in "$temp_root"/mserp-compat.*) rm -rf -- "$test_root" ;; esac' EXIT
mkdir -p "$test_root/old/migrations" "$test_root/new/migrations"
touch "$test_root/new/migrations/$permissions_migration"
touch "$test_root/new/migrations/$manager_verification_migration"
touch "$test_root/new/migrations/$accounting_cutover_migration"
touch "$test_root/new/migrations/$payroll_cost_start_migration"
touch "$test_root/new/migrations/$expense_balance_migration"
eval "$(sed -n '/^compatible_release() {/,/^activate_release() {/p' "$helper" | sed '$d')"
payroll_storage_migration=189_payroll_current_storage.sql
mock_payroll_current=f
mock_routing_usage=f
mock_gross_planning=f
mock_gross_week_lock=f
mock_escrow_balance=f
mock_escrow_adjustment=f
mock_expense_drafts=f
mock_accessorials=f
mock_escrow_settlement=f
mock_scale_carry=f
mock_scale_partial=f
mock_investor_retained=f
mock_investor_accounts=f
mock_board_stop_geocoding=f
mock_user_permissions=f
mserp_db() {
    local result=$mock
    if [[ $* == *"column_name='permission_overrides'"* ]]; then result=$mock_user_permissions; fi
    if [[ $* == *"to_regclass('public.board_stop_geocodes')"* ]]; then result=$mock_board_stop_geocoding; fi
    if [[ $* == *"table_name='driver_escrows'"* && $* == *"column_name='truck_id'"* ]]; then result=$mock_investor_accounts; fi
    if [[ $* == *"to_regclass('public.expense_accessorial_payments')"* ]]; then result=$mock_accessorials; fi
    if [[ $* == *manager_verified* ]]; then result=$mock_manager; fi
    if [[ $* == *public.accounting_cutover* ]]; then result=$mock_accounting; fi
    if [[ $* == *"to_regclass('public.payroll_cost_start')"* ]]; then result=$mock_cost_installed; fi
    if [[ $* == *"FROM public.payroll_cost_start"* ]]; then result=$mock_cost_active; fi
    if [[ $* == *"to_regclass('public.expense_balance_corrections')"* ]]; then result=$mock_balance_installed; fi
    if [[ $* == *"FROM public.expense_balance_corrections"* ]]; then result=$mock_balance_active; fi
    if [[ $* == *"to_regclass('public.payroll_projection_control')"* ]]; then result=$mock_projection_installed; fi
    if [[ $* == *"FROM public.payroll_projection_control"* ]]; then result=$mock_projection_active; fi
    if [[ $* == *"FROM public.payroll_projection_sources"* ]]; then result=$mock_telemetry_excluded; fi
    if [[ $* == *"table_name='relay_driver_links'"* ]]; then result=$mock_operational_excluded; fi
    if [[ $* == *"table_name='driver_board'"* ]]; then result=$mock_board_operational; fi
    if [[ $* == *"table_name='weekly_pti_rows'"* ]]; then result=$mock_pti_operational; fi
    if [[ $* == *"table_name='feedback_posts'"* ]]; then result=$mock_unrelated_payroll; fi
    if [[ $* == *"to_regclass('public.payroll_resource_dependency_sets')"* ]]; then result=$mock_shared_dependencies; fi
    if [[ $* == *"public.payroll_expense_verification_change"* ]]; then result=$mock_verification_metadata; fi
    if [[ $* == *"public.load_source_availability"* ]]; then result=$mock_load_availability; fi
    if [[ $* == *"public.payroll_statement_current"* ]]; then result=$mock_payroll_current; fi
    if [[ $* == *"public.board_route_usage"* ]]; then result=$mock_routing_usage; fi
    if [[ $* == *"public.gross_board_planning_entries"* ]]; then result=$mock_gross_planning; fi
    if [[ $* == *"public.gross_board_week_locks"* ]]; then result=$mock_gross_week_lock; fi
    if [[ $* == *"column_name='expense_choices'"* ]]; then result=$mock_expense_drafts; fi
    if [[ $* == *"column_name='escrow_draft'"* ]]; then result=$mock_escrow_settlement; fi
    if [[ $* == *"column_name='correction_reason'"* ]]; then result=$mock_escrow_balance; fi
    if [[ $* == *"JOIN public.weighmytruck_transactions"* ]]; then result=$mock_scale_carry; fi
    if [[ $* == *"scaleOriginalAmount"* ]]; then result=$mock_scale_partial; fi
    if [[ $* == *"constraint_name='escrow_balance_movement_check'"* ]]; then result=$mock_escrow_adjustment; fi
    if [[ $* == *"retainedPay"* ]]; then result=$mock_investor_retained; fi
    [[ $result != error ]] || return 1
    printf '%s\n' "$result"
}
mock_manager=f
mock_accounting=f
mock_cost_installed=f
mock_cost_active=f
mock_balance_installed=f
mock_balance_active=f
mock_projection_installed=f
mock_projection_active=f
mock_telemetry_excluded=f
mock_operational_excluded=f
mock_shared_dependencies=f
mock_verification_metadata=f
mock_load_availability=f
mock_board_operational=f
mock_pti_operational=f
mock_unrelated_payroll=f
mock=f
compatible_release "$test_root/old"
compatible_release "$test_root/new"
mock=t
if compatible_release "$test_root/old"; then echo 'incompatible rollback allowed' >&2; exit 1; fi
compatible_release "$test_root/new"
mock=error
if compatible_release "$test_root/new"; then echo 'database failure allowed rollback' >&2; exit 1; fi
mock=unexpected
if compatible_release "$test_root/new"; then echo 'unknown database result allowed rollback' >&2; exit 1; fi
mock=t
mock_manager=t
mkdir -p "$test_root/permissions-only/migrations"
touch "$test_root/permissions-only/migrations/$permissions_migration"
if compatible_release "$test_root/permissions-only"; then echo 'manager-verification rollback allowed' >&2; exit 1; fi
compatible_release "$test_root/new"
mock_manager=error
if compatible_release "$test_root/new"; then echo 'manager schema query failure allowed rollback' >&2; exit 1; fi
mock_manager=unexpected
if compatible_release "$test_root/new"; then echo 'unknown manager schema allowed rollback' >&2; exit 1; fi
mock_manager=t
mock_accounting=t
compatible_release "$test_root/new"
touch "$test_root/permissions-only/migrations/$manager_verification_migration"
if compatible_release "$test_root/permissions-only"; then echo 'accounting cutover rollback allowed' >&2; exit 1; fi
mock_accounting=error
if compatible_release "$test_root/new"; then echo 'accounting schema query failure allowed rollback' >&2; exit 1; fi
mock_accounting=unexpected
if compatible_release "$test_root/new"; then echo 'unknown accounting schema allowed rollback' >&2; exit 1; fi
mock_accounting=t
mock_cost_installed=t
compatible_release "$test_root/new"
mock_cost_active=t
compatible_release "$test_root/new"
touch "$test_root/permissions-only/migrations/$accounting_cutover_migration"
if compatible_release "$test_root/permissions-only"; then echo 'Fuel/Toll boundary rollback allowed' >&2; exit 1; fi
mock_cost_active=error
if compatible_release "$test_root/new"; then echo 'Fuel/Toll boundary read failure allowed rollback' >&2; exit 1; fi
mock_cost_active=unexpected
if compatible_release "$test_root/new"; then echo 'Unknown Fuel/Toll boundary allowed rollback' >&2; exit 1; fi
mock_cost_active=t
mock_balance_installed=t
mock_balance_active=f
touch "$test_root/permissions-only/migrations/$payroll_cost_start_migration"
compatible_release "$test_root/permissions-only"
mock_balance_active=t
if compatible_release "$test_root/permissions-only"; then echo 'Expense balance correction rollback allowed' >&2; exit 1; fi
compatible_release "$test_root/new"
mock_balance_active=error
if compatible_release "$test_root/new"; then echo 'Expense balance read failure allowed rollback' >&2; exit 1; fi
mock_balance_active=unexpected
if compatible_release "$test_root/new"; then echo 'Unknown expense balance result allowed rollback' >&2; exit 1; fi
mock_balance_installed=error
if compatible_release "$test_root/new"; then echo 'Expense balance schema read failure allowed rollback' >&2; exit 1; fi
mock_balance_installed=t
mock_balance_active=t
mock_projection_installed=t
mock_projection_active=t
if compatible_release "$test_root/new"; then echo 'Active payroll accepted a release without its migration' >&2; exit 1; fi
touch "$test_root/new/migrations/$payroll_projection_migration"
mkdir -p "$test_root/new/backend"
printf '#!/usr/bin/env bash\necho old-protocol\n' > "$test_root/new/backend/mserp-api"
chmod +x "$test_root/new/backend/mserp-api"
if compatible_release "$test_root/new"; then echo 'Incompatible payroll binary accepted' >&2; exit 1; fi
printf '#!/usr/bin/env bash\necho payroll-resources-v1\n' > "$test_root/new/backend/mserp-api"
compatible_release "$test_root/new"
mock_projection_active=error
if compatible_release "$test_root/new"; then echo 'Payroll status failure allowed rollback' >&2; exit 1; fi
mock_projection_installed=unexpected
if compatible_release "$test_root/new"; then echo 'Unknown payroll schema allowed rollback' >&2; exit 1; fi
mock_projection_installed=t
mock_projection_active=t
mock_telemetry_excluded=t
if compatible_release "$test_root/new"; then echo 'Telemetry exclusion allowed an old payroll binary' >&2; exit 1; fi
touch "$test_root/new/migrations/$payroll_telemetry_migration"
compatible_release "$test_root/new"
mock_telemetry_excluded=error
if compatible_release "$test_root/new"; then echo 'Telemetry classification failure allowed rollback' >&2; exit 1; fi
mock_telemetry_excluded=unexpected
if compatible_release "$test_root/new"; then echo 'Unknown telemetry classification allowed rollback' >&2; exit 1; fi
mock_telemetry_excluded=t
mock_operational_excluded=t
if compatible_release "$test_root/new"; then echo 'Operational exclusion allowed an old payroll binary' >&2; exit 1; fi
touch "$test_root/new/migrations/$payroll_operational_migration"
compatible_release "$test_root/new"
mock_operational_excluded=error
if compatible_release "$test_root/new"; then echo 'Operational classification failure allowed rollback' >&2; exit 1; fi
mock_operational_excluded=unexpected
if compatible_release "$test_root/new"; then echo 'Unknown operational classification allowed rollback' >&2; exit 1; fi
mock_operational_excluded=t
mock_shared_dependencies=t
if compatible_release "$test_root/new"; then echo 'Shared dependencies allowed an old payroll binary' >&2; exit 1; fi
touch "$test_root/new/migrations/$payroll_shared_migration"
compatible_release "$test_root/new"
mock_shared_dependencies=error
if compatible_release "$test_root/new"; then echo 'Shared dependency schema failure allowed rollback' >&2; exit 1; fi
mock_shared_dependencies=unexpected
if compatible_release "$test_root/new"; then echo 'Unknown shared dependency schema allowed rollback' >&2; exit 1; fi
mock_shared_dependencies=t
mock_verification_metadata=t
if compatible_release "$test_root/new"; then echo 'Verification metadata allowed an old payroll binary' >&2; exit 1; fi
touch "$test_root/new/migrations/$expense_verification_migration"
compatible_release "$test_root/new"
mock_verification_metadata=error
if compatible_release "$test_root/new"; then echo 'Verification metadata schema failure allowed rollback' >&2; exit 1; fi
mock_verification_metadata=unexpected
if compatible_release "$test_root/new"; then echo 'Unknown verification metadata schema allowed rollback' >&2; exit 1; fi
echo 'Permission, expense verification, accounting, Fuel/Toll, balance and payroll compatibility checks passed'

mock_verification_metadata=t
mock_load_availability=t
if compatible_release "$test_root/new"; then echo 'Load availability allowed an old payroll binary' >&2; exit 1; fi
touch "$test_root/new/migrations/$load_availability_migration"
compatible_release "$test_root/new"
mock_load_availability=error
if compatible_release "$test_root/new"; then echo 'Load availability schema failure allowed rollback' >&2; exit 1; fi
mock_load_availability=unexpected
if compatible_release "$test_root/new"; then echo 'Unknown load availability schema allowed rollback' >&2; exit 1; fi

mock_load_availability=t
mock_board_operational=t
if compatible_release "$test_root/new"; then echo 'Operational board allowed an old payroll binary' >&2; exit 1; fi
touch "$test_root/new/migrations/$board_operational_migration"
compatible_release "$test_root/new"
mock_board_operational=error
if compatible_release "$test_root/new"; then echo 'Board classification query failure allowed rollback' >&2; exit 1; fi
mock_board_operational=unexpected
if compatible_release "$test_root/new"; then echo 'Unknown board classification allowed rollback' >&2; exit 1; fi
echo 'Status Board compatibility checks passed'

mock_board_operational=t
mock_payroll_current=t
if compatible_release "$test_root/new"; then echo 'Current payroll storage allowed an old binary' >&2; exit 1; fi
touch "$test_root/new/migrations/$payroll_storage_migration"
compatible_release "$test_root/new"
mock_payroll_current=error
if compatible_release "$test_root/new"; then echo 'Current payroll schema failure allowed rollback' >&2; exit 1; fi
mock_payroll_current=unexpected
if compatible_release "$test_root/new"; then echo 'Unknown current payroll schema allowed rollback' >&2; exit 1; fi
echo 'Current payroll storage compatibility checks passed'

mock_payroll_current=t
mock_pti_operational=t
if compatible_release "$test_root/new"; then echo "PTI classification allowed an old payroll binary" >&2; exit 1; fi
touch "$test_root/new/migrations/$pti_operational_migration"
compatible_release "$test_root/new"
mock_pti_operational=error
if compatible_release "$test_root/new"; then echo "PTI classification query failure allowed rollback" >&2; exit 1; fi
mock_pti_operational=unexpected
if compatible_release "$test_root/new"; then echo "Unknown PTI classification allowed rollback" >&2; exit 1; fi
echo "PTI payroll compatibility checks passed"

mock_pti_operational=t
mock_unrelated_payroll=t
if compatible_release "$test_root/new"; then echo 'Operational classification allowed an old payroll binary' >&2; exit 1; fi
touch "$test_root/new/migrations/$unrelated_payroll_migration"
compatible_release "$test_root/new"
mock_unrelated_payroll=error
if compatible_release "$test_root/new"; then echo 'Operational classification query failure allowed rollback' >&2; exit 1; fi
mock_unrelated_payroll=unexpected
if compatible_release "$test_root/new"; then echo 'Unknown operational classification allowed rollback' >&2; exit 1; fi
echo 'Operational payroll compatibility checks passed'

mock_unrelated_payroll=t
mock_routing_usage=t
if compatible_release "$test_root/new"; then echo 'Routing usage allowed an incompatible payroll binary' >&2; exit 1; fi
printf '#!/usr/bin/env bash\nif [[ $1 == -board-routing-usage-capability ]]; then echo board-routing-usage-v1; else echo payroll-resources-v1; fi\n' > "$test_root/new/backend/mserp-api"
compatible_release "$test_root/new"
mock_routing_usage=error
if compatible_release "$test_root/new"; then echo 'Routing schema query failure allowed rollback' >&2; exit 1; fi
mock_routing_usage=unexpected
if compatible_release "$test_root/new"; then echo 'Unknown routing schema allowed rollback' >&2; exit 1; fi

mock_routing_usage=t
mock_expense_drafts=t
if compatible_release "$test_root/new"; then echo 'Expense drafts allowed a binary that posts on draft save' >&2; exit 1; fi
printf '#!/usr/bin/env bash\ncase "$1" in -payroll-expense-drafts-capability) echo payroll-expense-drafts-v1;; -board-routing-usage-capability) echo board-routing-usage-v1;; *) echo payroll-resources-v1;; esac\n' > "$test_root/new/backend/mserp-api"
compatible_release "$test_root/new"
mock_scale_carry=t
if compatible_release "$test_root/new"; then echo 'Cross-week CAT Scale collection allowed incompatible rollback' >&2; exit 1; fi
printf '#!/usr/bin/env bash\ncase "$1" in -payroll-scale-carry-capability) echo payroll-scale-carry-v1;; -payroll-expense-drafts-capability) echo payroll-expense-drafts-v1;; -board-routing-usage-capability) echo board-routing-usage-v1;; *) echo payroll-resources-v1;; esac\n' > "$test_root/new/backend/mserp-api"
compatible_release "$test_root/new"
mock_scale_carry=error
if compatible_release "$test_root/new"; then echo 'CAT Scale collection query failure allowed rollback' >&2; exit 1; fi
mock_scale_carry=f
mock_scale_partial=t
if compatible_release "$test_root/new"; then echo 'Partial CAT Scale collection allowed incompatible rollback' >&2; exit 1; fi
printf '#!/usr/bin/env bash\ncase "$1" in -payroll-scale-partial-capability) echo payroll-scale-partial-v1;; -payroll-scale-carry-capability) echo payroll-scale-carry-v1;; -payroll-expense-drafts-capability) echo payroll-expense-drafts-v1;; -board-routing-usage-capability) echo board-routing-usage-v1;; *) echo payroll-resources-v1;; esac\n' > "$test_root/new/backend/mserp-api"
compatible_release "$test_root/new"
mock_scale_partial=error
if compatible_release "$test_root/new"; then echo 'Partial CAT Scale query failure allowed rollback' >&2; exit 1; fi
mock_scale_partial=f
mock_investor_retained=t
if compatible_release "$test_root/new"; then echo 'Retained investor money allowed incompatible rollback' >&2; exit 1; fi
printf '#!/usr/bin/env bash\ncase "$1" in -investor-retained-pay-capability) echo investor-retained-pay-v1;; -payroll-expense-drafts-capability) echo payroll-expense-drafts-v1;; -board-routing-usage-capability) echo board-routing-usage-v1;; *) echo payroll-resources-v1;; esac\n' > "$test_root/new/backend/mserp-api"
compatible_release "$test_root/new"
mock_investor_retained=error
if compatible_release "$test_root/new"; then echo 'Retained pay query failure allowed rollback' >&2; exit 1; fi
mock_investor_retained=f
mock_expense_drafts=error
if compatible_release "$test_root/new"; then echo 'Expense draft schema query failure allowed rollback' >&2; exit 1; fi
mock_expense_drafts=unexpected
if compatible_release "$test_root/new"; then echo 'Unknown expense draft schema allowed rollback' >&2; exit 1; fi

mock_expense_drafts=t
mock_accessorials=t
if compatible_release "$test_root/new"; then echo 'Accessorials allowed an incompatible payroll binary' >&2; exit 1; fi
printf '#!/usr/bin/env bash\ncase "$1" in -accessorial-expenses-capability) echo accessorial-expenses-v1;; -payroll-expense-drafts-capability) echo payroll-expense-drafts-v1;; -board-routing-usage-capability) echo board-routing-usage-v1;; *) echo payroll-resources-v1;; esac\n' > "$test_root/new/backend/mserp-api"
compatible_release "$test_root/new"
mock_accessorials=error
if compatible_release "$test_root/new"; then echo 'Accessorial schema query failure allowed rollback' >&2; exit 1; fi
mock_accessorials=unexpected
if compatible_release "$test_root/new"; then echo 'Unknown accessorial schema allowed rollback' >&2; exit 1; fi
echo 'Accessorial compatibility checks passed'

mock_accessorials=t
mock_escrow_settlement=t
if compatible_release "$test_root/new"; then echo 'Escrow settlement allowed an incompatible payroll binary' >&2; exit 1; fi
printf '#!/usr/bin/env bash\ncase "$1" in -driver-pay-escrow-capability) echo driver-pay-escrow-v1;; -accessorial-expenses-capability) echo accessorial-expenses-v1;; -payroll-expense-drafts-capability) echo payroll-expense-drafts-v1;; -board-routing-usage-capability) echo board-routing-usage-v1;; *) echo payroll-resources-v1;; esac\n' > "$test_root/new/backend/mserp-api"
compatible_release "$test_root/new"
mock_escrow_settlement=error
if compatible_release "$test_root/new"; then echo 'Escrow schema query failure allowed rollback' >&2; exit 1; fi
mock_escrow_settlement=unexpected
if compatible_release "$test_root/new"; then echo 'Unknown escrow schema allowed rollback' >&2; exit 1; fi
echo 'Driver Pay escrow compatibility checks passed'

mock_escrow_settlement=t
mock_gross_planning=t
if compatible_release "$test_root/new"; then echo 'Dispatch planning allowed an incompatible payroll binary' >&2; exit 1; fi
printf '#!/usr/bin/env bash\ncase "$1" in -gross-board-planning-capability) echo gross-board-planning-v1;; -driver-pay-escrow-capability) echo driver-pay-escrow-v1;; -accessorial-expenses-capability) echo accessorial-expenses-v1;; -payroll-scale-partial-capability) echo payroll-scale-partial-v1;; -payroll-scale-carry-capability) echo payroll-scale-carry-v1;; -payroll-expense-drafts-capability) echo payroll-expense-drafts-v1;; -board-routing-usage-capability) echo board-routing-usage-v1;; *) echo payroll-resources-v1;; esac\n' > "$test_root/new/backend/mserp-api"
compatible_release "$test_root/new"
mock_gross_planning=error
if compatible_release "$test_root/new"; then echo 'Planning schema query failure allowed rollback' >&2; exit 1; fi
mock_gross_planning=unexpected
if compatible_release "$test_root/new"; then echo 'Unknown planning schema allowed rollback' >&2; exit 1; fi
echo 'Gross Board planning compatibility checks passed'

mock_gross_planning=t
mock_escrow_balance=t
if compatible_release "$test_root/new"; then echo 'Escrow balance reductions allowed an incompatible payroll binary' >&2; exit 1; fi
sed -i 's/case "$1" in /case "$1" in -escrow-balance-reductions-capability) echo escrow-balance-reductions-v1;; /' "$test_root/new/backend/mserp-api"
compatible_release "$test_root/new"
mock_escrow_balance=error
if compatible_release "$test_root/new"; then echo 'Escrow balance schema query failure allowed rollback' >&2; exit 1; fi
echo 'Escrow balance reduction compatibility checks passed'

mock_escrow_balance=t
mock_escrow_adjustment=t
if compatible_release "$test_root/new"; then echo 'Escrow balance increases allowed an incompatible payroll binary' >&2; exit 1; fi
sed -i 's/case "$1" in /case "$1" in -escrow-balance-adjustments-capability) echo escrow-balance-adjustments-v1;; /' "$test_root/new/backend/mserp-api"
compatible_release "$test_root/new"
mock_escrow_adjustment=error
if compatible_release "$test_root/new"; then echo 'Escrow adjustment schema query failure allowed rollback' >&2; exit 1; fi
echo 'Escrow balance adjustment compatibility checks passed'

mock_escrow_adjustment=t
mock_gross_week_lock=t
if compatible_release "$test_root/new"; then echo 'Week locking allowed a binary without lock enforcement' >&2; exit 1; fi
sed -i 's/case "$1" in /case "$1" in -gross-board-week-lock-capability) echo gross-board-week-lock-v1;; /' "$test_root/new/backend/mserp-api"
compatible_release "$test_root/new"
mock_gross_week_lock=error
if compatible_release "$test_root/new"; then echo 'Week lock schema query failure allowed rollback' >&2; exit 1; fi
echo 'Gross Board week lock compatibility checks passed'

mock_gross_week_lock=t
mock_investor_accounts=t
if compatible_release "$test_root/new"; then echo 'Investor accounts allowed incompatible rollback' >&2; exit 1; fi
sed -i 's/case "$1" in /case "$1" in -investor-truck-accounts-capability) echo investor-truck-accounts-v1;; /' "$test_root/new/backend/mserp-api"
compatible_release "$test_root/new"
mock_investor_accounts=error
if compatible_release "$test_root/new"; then echo 'Investor account query failure allowed rollback' >&2; exit 1; fi
mock_investor_accounts=unexpected
if compatible_release "$test_root/new"; then echo 'Unknown investor account schema allowed rollback' >&2; exit 1; fi
echo 'Investor truck account compatibility checks passed'

mock_investor_accounts=t
mock_board_stop_geocoding=t
if compatible_release "$test_root/new"; then echo 'Stop-city cache allowed an incompatible rollback' >&2; exit 1; fi
touch "$test_root/new/migrations/$board_stop_geocoding_migration"
compatible_release "$test_root/new"
mock_board_stop_geocoding=error
if compatible_release "$test_root/new"; then echo 'Stop-city cache query failure allowed rollback' >&2; exit 1; fi
echo 'Stop-city cache compatibility checks passed'

mock_board_stop_geocoding=t
mock_user_permissions=t
if compatible_release "$test_root/new"; then echo 'User overrides allowed an incompatible rollback' >&2; exit 1; fi
touch "$test_root/new/migrations/$user_permissions_migration"
compatible_release "$test_root/new"
mock_user_permissions=error
if compatible_release "$test_root/new"; then echo 'User override query failure allowed rollback' >&2; exit 1; fi
mock_user_permissions=unexpected
if compatible_release "$test_root/new"; then echo 'Unknown user override schema allowed rollback' >&2; exit 1; fi
echo 'User permission override compatibility checks passed'
