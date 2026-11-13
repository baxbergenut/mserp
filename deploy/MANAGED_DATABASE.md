# Managed PostgreSQL operations

The active application database is selected by `DATABASE_URL` in the root-owned
`/etc/mserp/mserp.env`. The database remains `mserp`, with runtime role `mserp_app`.
Keep secrets outside releases, source control, logs and command arguments.

## Administration and backups

Install `mserp-db` as `/usr/local/sbin/mserp-db`, root:root 0755, before the updated
`mserp-backup` and `mserp-deploy` helpers. Local loopback port 5432 uses the existing
`runuser -u postgres` path. A remote application URL requires the root:root 0600
libpq service file `/etc/mserp/database-admin.conf`:

```ini
[mserp]
host=PRIVATE_MANAGED_HOST
port=25060
dbname=mserp
user=doadmin
password=SECRET_PROVISIONED_OUTSIDE_SOURCE_CONTROL
sslmode=verify-full
sslrootcert=/etc/mserp/database-ca.crt
connect_timeout=10
```

The CA certificate is public material and may be root-owned 0644. Runtime URLs
also use `sslmode=verify-full` and this CA path. The helper validates host, port
and database against the runtime URL, clears inherited libpq environment options,
and passes the service-file path without credentials in process arguments.
Missing/mismatched managed configuration fails closed. Credentials may be staged
before cutover: a local runtime URL still selects the local database.
Service keys must immediately precede `=`. If generating this file with Python's
ConfigParser, use `write(file, space_around_delimiters=False)`. Preflight must run
actual `psql` and `pg_dump` commands with the staged service file, in addition to
checking the application's destination.

Use `/usr/local/sbin/mserp-db psql -X --dbname=mserp ...` for operator SQL. The
deploy helper uses the same route for migration discovery, compatibility checks
and migration execution. Managed migrations execute as `doadmin`; objects needed
by the application must retain their explicit `mserp_app` ownership/grants.

Backup receipts include the database destination identity. Legacy receipts qualify
only for the local database. A new destination requires a fresh verified backup;
changing credentials alone does not invalidate destination identity. Pruning
retains snapshots belonging to earlier destinations. `--adopt` is an operator
assertion that the archive belongs to the currently selected destination: never
adopt an old local archive to satisfy a managed-database backup check.

## Connection budget

The initial approved cluster has 1 vCPU, 1 GiB RAM and 10 GiB storage in NYC1,
PostgreSQL 16 Standard, primary only, with storage autoscaling off. The observed
price at creation was $15.15/month. Its connection limit is 22.

The API pool is capped at 10, with separate board-ready and payroll LISTEN
connections. The exporter uses up to two direct connections. The reconciliation
portal source pool defaults to four on this server: cap it explicitly at four in
its source URL. This gives a potential 18 application connections, leaving four
for administration/backup/restore. Avoid concurrent operator workers during
maintenance. Monitor actual concurrency and memory before increasing pools.
Direct connections preserve LISTEN and session advisory locks; transaction
pooling is not compatible with those clients.

Set `max_locks_per_transaction=256` through DigitalOcean Advanced configurations
before activation. The 1 GiB node has 25 server connections including reserved
ones; its default 64 locks therefore allocates only a quarter of the former
100-connection server's shared lock capacity. Payroll resource claims exhausted
that default during startup. The 256 setting restores equivalent capacity
without a plan upgrade and requires a database restart. Verify the live setting
with SQL and confirm payroll preparation completes without shared-memory errors.

The separate `fleetreview` OS user cannot traverse `/etc/mserp` (0750). Give its
source URL a CA file under `/etc/fleet-review/database-ca.crt` (root-owned 0644),
and verify readability as `fleetreview`. Its receiver URL remains unchanged.

## Verified production cutover

On 2026-11-13 the active Clustie database moved to `clustie-pg-prod` using the
private endpoint. All 204 public table counts and content digests, sequence
values, canonical schema definitions and effective application/portal table
permissions matched before activation. The active database retains UTF8 and
`C.UTF-8` collation/ctype. Source snapshots and comparison receipts are retained
in the root-only `/root/clustie-managed-migration` directory. The managed backup
passed catalog, checksum and receipt verification before writers started.

The old local `mserp` has `default_transaction_read_only=on` and no remaining
application connections. It is a recovery snapshot, not a current replica.
Returning to it requires a verified reverse migration of subsequent managed
writes. Other local PostgreSQL databases and services remain in place.

## Cutover and recovery

1. Verify the target cluster, region, VPC, trusted source and CA. Restrict network
   access to the production Droplet. Rehearse restoring a consistent custom dump
   into a separate target database. Check extensions, object ownership, runtime
   and portal grants, schema ledger, constraints and sequences. Do not run
   background integrations against the rehearsal copy.
   Create the target from `template0` with UTF8 encoding and the source database's
   `C.UTF-8` LC_COLLATE/LC_CTYPE. The provider's default `en_US.UTF-8` sorts text
   differently. Compare data manifests by table name independent of result order.
   Preserve database ownership by `mserp_app` and the existing inherited
   `mserp_app` membership of `fleet_review_portal`; table grants alone do not
   reproduce the source database's effective schema privileges. Former
   `postgres`-owned objects become managed-administrator-owned, while the
   application-owned tables, sequences, views and functions keep their owners.
2. Keep the original runtime/portal configuration and verified source backup in a
   root-only recovery directory. Inventory all writers. The portal has a separate
   `sourceUrl` in `/etc/fleet-review/config.json`; its FleetScope `receiverUrl`
   belongs to another system and must remain unchanged.
3. Serialize against `/run/lock/mserp-deploy.lock` and the backup lock. Pause the
   backup timer, API, expense exporter and reconciliation portal. Ensure no other
   operator job or database session can write Clustie. Do not stop the PostgreSQL
   service: it also hosts unrelated databases.
4. Take the final consistent dump while writers are stopped. Retain its checksum,
   catalog validation and source manifest. Restore to a fresh managed `mserp`
   database, preserving roles/grants and application ownership. Compare every
   public table's row count and deterministic content digest, sequences, migration
   ledger, constraints, indexes and triggers. Test read permissions as both runtime
   roles. Analyze the restored database before activation.
5. Atomically replace the application URL, portal source URL, CA and matching
   administrator service file. Verify helper destination identity. Create and
   verify a managed backup before allowing ordinary deployments again. Restore
   the timer and services, then check active release, readiness, HTTPS/login,
   board reads, live updates and connection counts.
6. Keep the old database and recovery files. Before any new managed writes,
   rollback can restore the original configuration and resume the frozen source.
   After managed writes begin, returning to the old snapshot loses data: stop
   writers and perform a verified reverse migration/reconciliation instead.

Run `python3 deploy/test-db.py`, `python3 deploy/test-backup.py`,
`bash deploy/test-backup-policy.sh` and `bash deploy/test-permissions-cutover.sh`
for changes to this routing and backup contract. These checks do not replace a
real restore and runtime-permission rehearsal.
