#!/usr/bin/env bash
# Exercise the real transition orchestration with disposable files and fake
# services/database. No connection to PostgreSQL, systemd or production.
set -euo pipefail
helper=${1:-$(dirname "$0")/mserp-payroll-protocol}
bash -n "$helper"
temp_root=$(cd "${TMPDIR:-/tmp}" && pwd -P)
test_root=$(mktemp -d "$temp_root/mserp-protocol.XXXXXX")
trap 'rc=$?; if [[ $rc != 0 && -f $test_root/output ]]; then cat "$test_root/output" >&2; fi; case "$test_root" in "$temp_root"/mserp-protocol.*) rm -rf -- "$test_root" ;; esac' EXIT
mkdir -p "$test_root/etc" "$test_root/bin" "$test_root/lock"
export test_root
# Replace fixed deployment paths only in this private test copy. The production
# helper offers no environment override for its root check or privileged paths.
sed -e 's/\[\[ $EUID == 0 \]\]/true/' \
    -e "s@/run/lock/@$test_root/lock/@g" \
    -e "s@/opt/mserp/current/backend/mserp-api@$test_root/bin/api@g" \
    -e "s@/opt/fleet-review/fleet-review@$test_root/bin/portal@g" \
    -e "s@/etc/mserp/@$test_root/etc/@g" "$helper" > "$test_root/helper"
cat > "$test_root/bin/api" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
read -r mode epoch < "$test_root/status"
case $1 in
    -payroll-protocol-capability) echo payroll-resources-v1 ;;
    -payroll-protocol-status)
        [[ ! -f $test_root/uncertain ]] || exit 1
        printf '{"mode":"%s","epoch":%s}\n' "$mode" "$epoch" ;;
    -payroll-protocol-switch)
        [[ $4 == "$epoch" ]] || exit 1
        [[ ! -s $test_root/active ]] || { echo 'switch attempted with active writers' >&2; exit 1; }
        printf '%s %s\n' "$2" "$((epoch+1))" > "$test_root/status"
        printf 'switch %s\n' "$2" >> "$test_root/events"
        # Lost CLI response after commit must recover using observed epoch.
        if [[ ${scenario:-} == lost && $2 == resources ]]; then exit 1; fi
        if [[ ${scenario:-} == uncertain && $2 == resources ]]; then touch "$test_root/uncertain"; exit 1; fi
        ;;
    *) exit 2 ;;
esac
SH
cat > "$test_root/bin/portal" <<'SH'
#!/usr/bin/env bash
[[ ${scenario:-} != incompatible_portal ]] || exit 1
echo payroll-resources-v1
SH
chmod +x "$test_root/bin/api" "$test_root/bin/portal"
flock() { return 0; }
runuser() { shift 3; "$@"; }
systemctl() {
    local verb=$1 service=${@: -1}
    case $verb in
        is-active) grep -Fxq "$service" "$test_root/active" ;;
        stop)
            if [[ ${scenario:-} == stop_failure && $service == mserp-api.service ]]; then return 1; fi
            grep -Fxv "$service" "$test_root/active" > "$test_root/next" || true
            mv "$test_root/next" "$test_root/active"
            echo "stop $service" >> "$test_root/events" ;;
        start)
            if [[ ${scenario:-} == start_failure && ! -f $test_root/start_failed ]]; then touch "$test_root/start_failed"; return 1; fi
            echo "$service" >> "$test_root/active"
            echo "start $service" >> "$test_root/events" ;;
        *) return 2 ;;
    esac
}
curl() { [[ ${scenario:-} != readiness_failure ]]; }
sleep() { return 0; }
# Python is used for JSON/config serialization, separate from the orchestration
# under test. This stand-in lets the recovery tests run in Windows Git Bash too.
# Linux CI sets MSERP_PROTOCOL_REAL_PYTHON=1 to run real JSON/config serialization.
python3() {
    if [[ ${MSERP_PROTOCOL_REAL_PYTHON:-0} == 1 ]]; then
        [[ $1 == -c || ${scenario:-} != prepare_failure ]] || return 1
        command python3 "$@"
        return
    fi
    if [[ $1 == -c ]]; then
        sed -n 's/.*"mode":"\([a-z]*\)","epoch":\([0-9]*\).*/\1 \2/p'
        printf '\n'
    else
        cat >/dev/null
        [[ ${scenario:-} != prepare_failure ]] || return 1
        printf 'PAYROLL_RESOURCE_LOCKS=%s\n' "$([[ $3 == resources ]] && echo true || echo false)" >> "$2"
    fi
}
export -f flock runuser systemctl curl sleep python3
for scenario in success incompatible_portal stale prepare_failure stop_failure lost start_failure readiness_failure uncertain rollback; do
    export scenario
    printf 'global 1\n' > "$test_root/status"
    printf 'ORIGINAL=preserved\n' > "$test_root/etc/mserp.env"
    printf 'mserp-api.service\nmserp-expense-sheet.service\nfleet-review.service\n' > "$test_root/active"
    : > "$test_root/events"
    rm -f -- "$test_root/uncertain" "$test_root/start_failed"
    target=resources; epoch=1
    if [[ $scenario == stale ]]; then epoch=2; fi
    if [[ $scenario == rollback ]]; then printf 'resources 5\n' > "$test_root/status"; target=global; epoch=5; fi
    rc=0
    bash "$test_root/helper" "$target" "$epoch" > "$test_root/output" 2>&1 || rc=$?
    read -r mode actual_epoch < "$test_root/status"
    case $scenario in
        success|rollback)
            [[ $rc == 0 && $mode == "$target" && $actual_epoch == "$((epoch+1))" ]]
            [[ $(wc -l < "$test_root/active") == 3 ]]
            grep -Fxq ORIGINAL=preserved "$test_root/etc/mserp.env" ;;
        lost|start_failure|readiness_failure)
            [[ $rc != 0 && $mode == global && $actual_epoch == 3 ]]
            [[ $(cat "$test_root/etc/mserp.env") == ORIGINAL=preserved ]]
            [[ $(wc -l < "$test_root/active") == 3 ]] ;;
        uncertain)
            [[ $rc != 0 && $mode == resources && ! -s $test_root/active ]]
            compgen -G "$test_root/etc/.payroll-protocol-backup.*" >/dev/null
            # Retained backups are deliberately preserved by the real helper.
            rm -f -- "$test_root"/etc/.payroll-protocol-backup.* ;;
        stop_failure)
            [[ $rc != 0 && $mode == global && $actual_epoch == 1 ]]
            [[ ! $(grep '^switch ' "$test_root/events" || true) ]]
            compgen -G "$test_root/etc/.payroll-protocol-backup.*" >/dev/null
            rm -f -- "$test_root"/etc/.payroll-protocol-backup.* ;;
        stale|prepare_failure|incompatible_portal)
            [[ $rc != 0 && $mode == global && $actual_epoch == 1 ]]
            [[ ! -s $test_root/events ]]
            [[ $(cat "$test_root/etc/mserp.env") == ORIGINAL=preserved ]] ;;
    esac
    if compgen -G "$test_root/etc/.payroll-protocol-*" >/dev/null; then
        echo "unexpected temporary configuration after $scenario" >&2; exit 1
    fi
    echo "PASS $scenario"
done
