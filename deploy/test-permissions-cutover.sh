#!/usr/bin/env bash
set -euo pipefail
helper=${1:-$(dirname "$0")/mserp-deploy}
bash -n "$helper"
permissions_migration=112_permissions_v2.sql
manager_verification_migration=151_retire_expense_manager_verification.sql
accounting_cutover_migration=160_accounting_cutover.sql
payroll_cost_start_migration=173_payroll_cost_start.sql
test_root=$(mktemp -d)
trap 'rm -rf -- "$test_root"' EXIT
mkdir -p "$test_root/old/migrations" "$test_root/new/migrations"
touch "$test_root/new/migrations/$permissions_migration"
touch "$test_root/new/migrations/$manager_verification_migration"
touch "$test_root/new/migrations/$accounting_cutover_migration"
touch "$test_root/new/migrations/$payroll_cost_start_migration"
eval "$(sed -n '/^compatible_release() {/,/^activate_release() {/p' "$helper" | sed '$d')"
runuser() {
    local result=$mock
    if [[ $* == *manager_verified* ]]; then result=$mock_manager; fi
    if [[ $* == *public.accounting_cutover* ]]; then result=$mock_accounting; fi
    if [[ $* == *"to_regclass('public.payroll_cost_start')"* ]]; then result=$mock_cost_installed; fi
    if [[ $* == *"FROM public.payroll_cost_start"* ]]; then result=$mock_cost_active; fi
    [[ $result != error ]] || return 1
    printf '%s\n' "$result"
}
mock_manager=f
mock_accounting=f
mock_cost_installed=f
mock_cost_active=f
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
echo 'Permission, expense verification, accounting and Fuel/Toll compatibility checks passed'
