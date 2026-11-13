#!/usr/bin/env bash
set -euo pipefail
helper=${1:-$(dirname "$0")/mserp-deploy}
bash -n "$helper"
eval "$(sed -n '/^# BEGIN BACKUP POLICY$/,/^# END BACKUP POLICY$/p' "$helper")"
task_root=$(mktemp -d)
trap 'rm -rf -- "$task_root"' EXIT
mkdir "$task_root/migrations"
touch "$task_root/migrations/001_initial.sql" "$task_root/migrations/187_load_source_availability.sql"
release_id=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
backup_helper=$task_root/backup
export backup_calls=$task_root/calls
cat > "$backup_helper" <<'SCRIPT'
#!/usr/bin/env bash
echo "$*" >> "$backup_calls"
[[ ${fail_backup:-0} == 0 ]] || exit 1
echo verified-backup
SCRIPT
chmod +x "$backup_helper"
ledger=t
applied_versions=$'001_initial.sql\n187_load_source_availability.sql'
mserp_db() {
    [[ $ledger != error ]] || return 1
    if [[ $* == *to_regclass* ]]; then printf '%s\n' "$ledger"; else printf '%s\n' "$applied_versions"; fi
}
timer_enabled=true timer_active=true service_failed=false service_loaded=loaded
systemctl() {
    case $1 in
        is-enabled) [[ $timer_enabled == true ]] ;;
        is-active) [[ $timer_active == true ]] ;;
        is-failed) [[ $service_failed == true ]] ;;
        show) echo "$service_loaded" ;;
        *) return 1 ;;
    esac
}
collect_pending_migrations "$task_root/migrations"
[[ ${#pending_migrations[@]} == 0 ]]
prepare_release_backup
[[ $(cat "$backup_calls") == --check ]]
: > "$backup_calls"
applied_versions=001_initial.sql
collect_pending_migrations "$task_root/migrations"
[[ ${pending_migrations[*]} == 187_load_source_availability.sql ]]
prepare_release_backup
[[ $(cat "$backup_calls") == "--create --label $release_id" ]]
ledger=f
collect_pending_migrations "$task_root/migrations"
[[ ${#pending_migrations[@]} == 2 ]]
for ledger in error unexpected; do
    if collect_pending_migrations "$task_root/migrations"; then echo 'Ledger failure allowed a deployment' >&2; exit 1; fi
done
ledger=t applied_versions=$'001_initial.sql\n187_load_source_availability.sql'
touch "$task_root/migrations/188_invalid-name.sql"
if collect_pending_migrations "$task_root/migrations"; then echo 'Invalid migration name accepted' >&2; exit 1; fi
rm "$task_root/migrations/188_invalid-name.sql"
collect_pending_migrations "$task_root/migrations"
timer_enabled=false
if prepare_release_backup; then echo 'Disabled timer allowed a deployment' >&2; exit 1; fi
timer_enabled=true timer_active=false
if prepare_release_backup; then echo 'Inactive timer allowed a deployment' >&2; exit 1; fi
timer_active=true service_failed=true
if prepare_release_backup; then echo 'Failed backup service allowed a deployment' >&2; exit 1; fi
service_failed=false service_loaded=not-found
if prepare_release_backup; then echo 'Missing backup service allowed a deployment' >&2; exit 1; fi
service_loaded=loaded
export fail_backup=1
if prepare_release_backup; then echo 'Stale/missing/corrupt backup allowed a deployment' >&2; exit 1; fi
pending_migrations=(187_load_source_availability.sql)
if prepare_release_backup; then echo 'Failed migration backup allowed a deployment' >&2; exit 1; fi
echo 'Deployment backup policy checks passed'
