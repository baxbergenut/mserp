#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."

case "${1:-}" in
    guards)
        bash deploy/test-permissions-cutover.sh
        MSERP_PROTOCOL_REAL_PYTHON=1 bash deploy/test-payroll-protocol.sh
        python3 deploy/test-payroll-protocol-config.py
        bash deploy/test-backup-policy.sh
        python3 deploy/test-backup.py
        ;;
    backend)
        : "${MSERP_RELEASE_TEST_DATABASE_URL:?set the disposable release test database URL}"
        for suite in FEEDBACK DISPATCHER_PAY DASHBOARD DASHBOARD_SCHEMA RELAY PREPASS \
            INVENTORY WMT DRIVER_BOARD ACCESS SAMSARA DRIVER_CHARGES DRIVER_PAY COST \
            INVESTOR PHONE FLEETSCOPE ASSIGNMENT_HISTORY CUSTOM_TASK GROSS_BOARD \
            RELAY_REVIEW TOLL CUTOVER EXPENSE_SHEET; do
            export "MSERP_${suite}_TEST_DATABASE_URL=$MSERP_RELEASE_TEST_DATABASE_URL"
        done
        cd backend
        go test -p 1 ./... -timeout 30m
        go vet ./...
        ;;
    build-backend)
        cd backend
        export CGO_ENABLED=0 GOOS=linux GOARCH=amd64
        go build -trimpath -ldflags="-s -w" -o mserp-api ./cmd/server
        go build -trimpath -ldflags="-s -w" -o export-expense-sheet ./cmd/export-expense-sheet
        go build -trimpath -ldflags="-s -w" -o fleet-review ./cmd/fleet-review
        go build -trimpath -ldflags="-s -w" -o reconcile-loads ./cmd/reconcile-loads
        ;;
    frontend)
        cd frontend
        npm ci
        npm run lint
        node scripts/test-gross-board.mjs
        node scripts/test-driver-board.mjs
        node scripts/test-driver-pay.mjs
        node scripts/test-read-requests.mjs
        node scripts/test-phone.mjs
        NEXT_PUBLIC_API_URL=/api npm run build
        ;;
    package)
        sha=$(git rev-parse HEAD)
        mkdir -p release/backend release/frontend release/migrations artifact
        for binary in mserp-api export-expense-sheet fleet-review reconcile-loads; do
            install -m 0750 "backend/$binary" "release/backend/$binary"
        done
        cp -a frontend/out/. release/frontend/
        cp backend/sql/[0-9][0-9][0-9]_*.sql release/migrations/
        tar --sort=name --mtime='UTC 1970-01-01' --owner=0 --group=0 --numeric-owner \
            -czf "artifact/mserp-$sha.tar.gz" -C release .
        (cd artifact && sha256sum "mserp-$sha.tar.gz" > "mserp-$sha.sha256")
        ;;
    *)
        echo 'usage: release-checks.sh {guards|backend|build-backend|frontend|package}' >&2
        exit 2
        ;;
esac
