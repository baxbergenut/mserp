// Run against review-local.mjs with its disposable database and access file.
import { execFileSync } from 'node:child_process';
import { readFile } from 'node:fs/promises';
import { chromium, expect } from '@playwright/test';

const access = JSON.parse(await readFile(process.env.MSERP_REVIEW_ACCESS_FILE, 'utf8'));
const dsn = process.env.MSERP_REVIEW_TEST_DATABASE_URL;
const database = new URL(dsn);
if (!['127.0.0.1', 'localhost'].includes(new URL(access.base).hostname) || !/^review_[a-f0-9]+$/.test(access.schema) || !['127.0.0.1', 'localhost'].includes(database.hostname) || !database.pathname.endsWith('_test')) throw Error('Isolated local review required');
const sql = query => execFileSync(process.env.PSQL_PATH || 'psql', ['-X', '-Atq', '-v', 'ON_ERROR_STOP=1', '-d', dsn], { input: `SET search_path TO ${access.schema},public;\n${query}`, encoding: 'utf8', windowsHide: true });
const suffix = Date.now();
const units = [`BADGE-READY-${suffix}`, `BADGE-SHOP-${suffix}`, `BADGE-OOS-${suffix}`];
sql(`INSERT INTO trucks(unit_number,status,status_reason) VALUES ('${units[0]}','available',''),('${units[1]}','maintenance','Fixture shop'),('${units[2]}','out_of_service','Fixture OOS');
INSERT INTO service_program_trucks(program_id,truck_id) SELECT '00000000-0000-4000-8000-000000000020',id FROM trucks WHERE unit_number IN ('${units.join("','")}');`);
const browser = await chromium.launch();
try {
  const page = await browser.newPage();
  const login = await page.request.post(access.base + '/api/auth/login', { headers: { Origin: access.base }, data: { email: access.email, password: access.password } });
  expect(login.ok()).toBe(true);
  await page.goto(access.base + '/fleet/service-programs');
  await page.getByPlaceholder('Search trucks or programs…').fill(`BADGE-`);
  for (const [index, label] of [[0, null], [1, 'Shop'], [2, 'OOS']]) {
    const row = page.locator('tbody tr').filter({ has: page.getByRole('link', { name: units[index], exact: true }) });
    await expect(row).toBeVisible();
    if (label) await expect(row.locator('td').first().getByText(label, { exact: true })).toBeVisible();
    else await expect(row.locator('td').first().getByText(/^(Shop|OOS)$/)).toHaveCount(0);
  }
  console.log('Service program Shop and OOS badges verified.');
} finally { await browser.close(); }
