#!/usr/bin/env bash
set -euo pipefail
helper=${1:-$(dirname "$0")/mserp-deploy}
bash -n "$helper"
permissions_migration=112_permissions_v2.sql
test_root=$(mktemp -d)
trap 'rm -rf -- "$test_root"' EXIT
mkdir -p "$test_root/old/migrations" "$test_root/new/migrations"
touch "$test_root/new/migrations/$permissions_migration"
eval "$(sed -n '/^compatible_release() {/,/^activate_release() {/p' "$helper" | sed '$d')"
runuser() { [[ $mock != error ]] || return 1; printf '%s\n' "$mock"; }
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
echo 'Permission cutover compatibility checks passed'
