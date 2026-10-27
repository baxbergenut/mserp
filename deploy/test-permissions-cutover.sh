#!/usr/bin/env bash
set -euo pipefail
helper=${1:-$(dirname "$0")/mserp-deploy}
bash -n "$helper"
permissions_migration=112_permissions_v2.sql
manager_verification_migration=151_retire_expense_manager_verification.sql
test_root=$(mktemp -d)
trap 'rm -rf -- "$test_root"' EXIT
mkdir -p "$test_root/old/migrations" "$test_root/new/migrations"
touch "$test_root/new/migrations/$permissions_migration"
touch "$test_root/new/migrations/$manager_verification_migration"
eval "$(sed -n '/^compatible_release() {/,/^activate_release() {/p' "$helper" | sed '$d')"
runuser() {
    local result=$mock
    if [[ $* == *manager_verified* ]]; then result=$mock_manager; fi
    [[ $result != error ]] || return 1
    printf '%s\n' "$result"
}
mock_manager=f
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
echo 'Permission and expense verification cutover compatibility checks passed'
