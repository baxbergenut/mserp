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

Production is served at `https://erp.msexpressinc.net`. Nginx redirects port 80
to HTTPS and uses the Let's Encrypt certificate under
`/etc/letsencrypt/live/erp.msexpressinc.net`. The Certbot systemd timer renews
the certificate automatically through the Nginx ACME webroot at
`/var/www/letsencrypt`. Successful renewals invoke the root-owned
`/usr/local/sbin/mserp-certbot-deploy` hook to validate and reload Nginx. The
original IP endpoint on port 8443 remains available during the domain
transition.

The production frontend is built with `NEXT_PUBLIC_API_URL=/api` so browser
requests, authentication cookies, and CSRF protection remain same-origin behind
Nginx. Do not build production against the direct IP endpoint.

Database migrations should remain backward-compatible with the previous
application release because an application rollback does not automatically
reverse a successful database migration. Database backups are intentionally
retained for manual recovery.

To roll back manually, repoint `/opt/mserp/current` to an earlier directory in
`/opt/mserp/releases`, then restart `mserp-api` and reload Nginx.
