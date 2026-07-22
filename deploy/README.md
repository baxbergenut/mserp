# MSERP deployment

GitHub Actions builds the Linux API and static frontend. Production receives a
checksum-verified archive through the unprivileged `mserp-deploy` account. The
root-owned `mserp-deploy` helper then:

1. rejects unsafe or malformed archives;
2. creates a PostgreSQL custom-format backup under `/var/backups/mserp`;
3. applies only SQL files absent from `schema_migrations`;
4. switches `/opt/mserp/current` atomically;
5. restarts the API, reloads Nginx, and rolls the application back if health
   checks fail.

Pushes to `main` deploy production. Pull requests run the complete test/build
job without receiving deployment secrets. `workflow_dispatch` can redeploy the
current `main` commit.

Database migrations should remain backward-compatible with the previous
application release because an application rollback does not automatically
reverse a successful database migration. Database backups are intentionally
retained for manual recovery.

To roll back manually, repoint `/opt/mserp/current` to an earlier directory in
`/opt/mserp/releases`, then restart `mserp-api` and reload Nginx.
