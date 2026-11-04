#!/usr/bin/env bash
set -euo pipefail

# Keep tests serial inside each runner. Repository tests use PostgreSQL locks
# shared by all schemas in a database, so each shard gets its own server.
shard=${TEST_SHARD:?}
repository_package=$(go list ./internal/repository)
if [[ $shard == other-packages ]]; then
    packages=$(go list ./...)
    mapfile -t other_packages < <(printf '%s\n' "$packages" | awk -v repo="$repository_package" '$0 != repo')
    go test -p 1 -count=1 -timeout 30m "${other_packages[@]}"
    exit
fi

shard_count=$((BACKEND_JOB_COUNT - 1))
if [[ ! $shard =~ ^[1-9][0-9]*$ ]] || (( shard > shard_count )); then
    echo "Invalid repository shard $shard of $shard_count." >&2
    exit 1
fi

# Use the test binary's list so new tests, examples and fuzz seeds are included
# automatically. Sorting makes every runner see the same order.
listed=$(go test -list '^(Test|Example|Fuzz)' ./internal/repository)
pattern=$(printf '%s\n' "$listed" | LC_ALL=C sort -u |
    awk -v shard="$shard" -v count="$shard_count" '
        /^(Test|Example|Fuzz)/ {
            if ((index_in_suite++ % count) + 1 == shard) {
                printf "%s%s", separator, $0
                separator = "|"
            }
        }')
if [[ -z $pattern ]]; then
    echo "Repository shard $shard of $shard_count has no tests." >&2
    exit 1
fi

echo "Running repository shard $shard of $shard_count."
go test -v -p 1 -count=1 -timeout 30m -run "^($pattern)$" ./internal/repository
