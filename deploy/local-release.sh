#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
repo_root=$PWD

if [[ $# != 2 || ! $2 =~ ^[0-9a-f]{40}$ ]]; then
    echo 'usage: bash deploy/local-release.sh {prepare|publish} <40-character commit SHA>' >&2
    exit 2
fi

sha=$2
[[ $(git rev-parse --verify "$sha^{commit}") == "$sha" ]] || exit 2
store="$(git rev-parse --path-format=absolute --git-common-dir)/mserp-local-releases"
receipt="$store/$sha"
archive="mserp-$sha.tar.gz"
checksums="mserp-$sha.sha256"

case "$1" in
    prepare)
        if [[ $(uname -s) != Linux || $EUID == 0 ]]; then
            echo 'Run prepare as a regular user on Linux (Ubuntu under WSL2 works on Windows).' >&2
            exit 1
        fi
        if [[ -e $receipt ]]; then
            echo "A locally checked release already exists at $receipt." >&2
            exit 1
        fi
        if [[ $(node -p 'process.versions.node.split(".")[0]') != 22 ]]; then
            echo 'Use Node.js 22, matching CI.' >&2
            exit 1
        fi
        for program in go initdb pg_ctl psql createdb python3; do
            command -v "$program" >/dev/null || { echo "Missing $program on PATH." >&2; exit 1; }
        done
        if [[ $(initdb --version) != 'initdb (PostgreSQL) 16.'* ]]; then
            echo 'Use PostgreSQL 16, matching CI.' >&2
            exit 1
        fi

        # The clean checkout has no .env. Disable inherited live test opt-ins
        # and libpq defaults before creating the disposable database.
        while IFS= read -r variable; do
            case "$variable" in
                MSERP_*|PG*|*_DATABASE_URL|PREPASS_INTEGRATION_*) unset "$variable" ;;
            esac
        done < <(compgen -e)
        export GOENV=off GOFLAGS= GOOS= GOARCH= CGO_ENABLED=
        unset NEXT_PUBLIC_API_URL NODE_OPTIONS

        mkdir -p "$store"
        scratch=$(mktemp -d "$store/.prepare-$sha.XXXXXX")
        checkout="$scratch/source"
        cluster="$scratch/pgdata"
        cleanup() {
            local status=$?
            cd "$repo_root"
            if [[ -f $cluster/postmaster.pid ]] && ! pg_ctl -D "$cluster" -m fast -w stop; then
                echo "PostgreSQL did not stop. Temporary files are at $scratch." >&2
                return 1
            fi
            if [[ -e $checkout ]] && ! git worktree remove --force "$checkout"; then
                echo "Could not remove the temporary checkout at $checkout." >&2
                return 1
            fi
            case "$scratch" in
                "$store/.prepare-$sha."*) rm -rf -- "$scratch" ;;
            esac
            return "$status"
        }
        trap cleanup EXIT
        git -c core.autocrlf=false worktree add --detach "$checkout" "$sha"
        export GOTOOLCHAIN
        GOTOOLCHAIN="go$(awk '$1 == "go" {print $2; exit}' "$checkout/backend/go.mod")"
        cd "$checkout"
        bash deploy/release-checks.sh guards
        bash deploy/release-checks.sh frontend
        bash deploy/release-checks.sh build-backend

        # Only the new cluster is reachable through this generated loopback port.
        # No persistent PostgreSQL service or operator credentials are needed.
        port=$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1])')
        initdb -D "$cluster" -U postgres --no-locale -E UTF8 --auth=trust
        pg_ctl -D "$cluster" -l "$scratch/postgres.log" -o "-h 127.0.0.1 -p $port -c unix_socket_directories=" -w start
        db="postgresql://postgres@127.0.0.1:$port/mserp_assignment_history_test?sslmode=disable"
        createdb -h 127.0.0.1 -p "$port" -U postgres mserp_assignment_history_test
        psql -X -v ON_ERROR_STOP=1 "$db" -c 'CREATE ROLE mserp_app; CREATE EXTENSION IF NOT EXISTS pgcrypto WITH SCHEMA public'
        MSERP_RELEASE_TEST_DATABASE_URL="$db" bash deploy/release-checks.sh backend
        git diff --exit-code HEAD --
        bash deploy/release-checks.sh package
        mv -T -- "$checkout/artifact" "$receipt"
        printf 'Local checks passed for %s. Release is ready in %s\n' "$sha" "$receipt"
        ;;
    publish)
        repo=baxbergenut/mserp
        [[ $(gh api "repos/$repo/git/ref/heads/main" --jq '.object.sha') == "$sha" ]] || {
            echo 'Only the current main commit can be deployed.' >&2
            exit 1
        }
        if [[ $(git show -s --format=%B "$sha") != *'[skip ci]'* ]]; then
            echo 'Use [skip ci] in the final merge message to opt in to local deployment.' >&2
            exit 1
        fi
        cd "$receipt"
        checksum=$(sha256sum "$archive")
        checksum=${checksum%% *}
        if [[ $(<"$checksums") != "$checksum *$archive" ]]; then
            echo 'The archive no longer matches the locally checked release.' >&2
            exit 1
        fi

        # Stage atomically under a name reserved for the deploy-only Actions job.
        # All activations, including automatic ones, use the same production queue.
        remote_stage="set -eu
umask 077
cd /var/lib/mserp-deploy/incoming
staging=\$(mktemp .local-$sha.XXXXXX)
trap 'rm -f -- \"\$staging\"' EXIT
cat > \"\$staging\"
printf '%s  %s\\n' '$checksum' \"\$staging\" | sha256sum --check --status
mv -T -- \"\$staging\" '$sha.$checksum.local.tar.gz'"
        MSYS_NO_PATHCONV=1 ssh -o BatchMode=yes -o StrictHostKeyChecking=yes \
            -o ConnectTimeout=15 root@137.184.102.22 "$remote_stage" < "$archive"
        gh workflow run ci-deploy.yml --repo "$repo" --ref main \
            -f "local_release_sha=$sha" -f "local_release_checksum=$checksum"
        ;;
    *)
        echo 'Choose prepare or publish.' >&2
        exit 2
        ;;
esac
