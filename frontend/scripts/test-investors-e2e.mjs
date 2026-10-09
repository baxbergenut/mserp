import { runUpdatersE2E } from "./updaters-e2e.mjs";
import { runProfilesE2E } from "./profiles-e2e.mjs";
// Requires a disposable local _test database, psql, Go, and a /api frontend build.
// Uses a temporary schema, a real API process, and real browser authentication.
import { chromium, expect } from '@playwright/test';
import { runInvestorPayE2E } from './investor-pay-e2e.mjs';
import { runPhoneE2E } from './phone-e2e.mjs';
import { runAccessE2E } from './access-e2e.mjs';
import { runAssignmentWeekE2E } from './assignment-week-e2e.mjs';
import { runEscrowTasksE2E } from './escrow-tasks-e2e.mjs';
import { runPayrollWorkflowsE2E } from './payroll-workflows-e2e.mjs';
import { runOffboardingE2E } from './offboarding-e2e.mjs';
import { runDriverBoardE2E } from './driver-board-e2e.mjs';
import { execFileSync, spawn } from 'node:child_process';
import { randomBytes } from 'node:crypto';
import { createServer } from 'node:http';
import { Readable } from 'node:stream';
import { mkdtemp, readFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join, extname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const frontend = resolve(fileURLToPath(new URL('..', import.meta.url)));
const backend = resolve(frontend, '../backend');
const dsn = process.env.MSERP_INVESTOR_TEST_DATABASE_URL;
if (!dsn) throw new Error('Set MSERP_INVESTOR_TEST_DATABASE_URL to a disposable local _test database');
const database = new URL(dsn);
if (!['127.0.0.1', 'localhost'].includes(database.hostname) || !database.pathname.includes('_test')) throw new Error('Only local _test databases are permitted');
const schema = `investor_e2e_${randomBytes(8).toString('hex')}`;
const password = randomBytes(24).toString('hex');
const webhookCompany = 'e5200000-0000-0000-0000-000000000001';
const webhookSecret = randomBytes(32).toString('hex');
const temp = await mkdtemp(join(tmpdir(), 'mserp-investor-e2e-'));
const sql = (input) => execFileSync('psql', ['-X', '-q', '-v', 'ON_ERROR_STOP=1', '-d', dsn], { input, encoding: 'utf8', stdio: ['pipe', 'pipe', 'pipe'], windowsHide: true });
let api, browser, server;
try {
  const init = await readFile(join(backend, 'sql/init.sql'), 'utf8');
  sql(`CREATE SCHEMA ${schema}; GRANT USAGE ON SCHEMA ${schema} TO mserp_app; SET search_path TO ${schema},public;\n${init}\n
    GRANT SELECT,INSERT,UPDATE,DELETE ON ALL TABLES IN SCHEMA ${schema} TO mserp_app;
    INSERT INTO app_users(username,password_hash,email,role_id) VALUES('investor-e2e',crypt('${password}',gen_salt('bf')),'investor-e2e@example.com',(SELECT id FROM app_roles WHERE system_role));
    INSERT INTO drivers(full_name,normalized_name,is_owner_operator,pay_type,pay_rate) VALUES('E2e Driver','e2e driver',true,'gross_percentage',80);
  `);
  const binary = join(temp, process.platform === 'win32' ? 'api.exe' : 'api');
  execFileSync('go', ['build', '-o', binary, './cmd/server'], { cwd: backend, windowsHide: true });
  database.searchParams.set('search_path', `${schema},public`);
  database.searchParams.set('role', 'mserp_app');
  const apiPort = Number(process.env.MSERP_E2E_API_PORT || 18549);
  const webPort = Number(process.env.MSERP_E2E_WEB_PORT || 13549);
  api = spawn(binary, [], { cwd: temp, windowsHide: true, stdio: ['ignore', 'pipe', 'pipe'], env: {
    ...process.env, DATABASE_URL: database.toString(), PORT: String(apiPort), BIND_ADDRESS: '127.0.0.1',
    RELAY_API_KEY: 'test-disabled', PREPASS_CLIENT_ID: 'test-disabled', PREPASS_CLIENT_SECRET: 'test-disabled', DATATRUCK_API_KEY: 'test-disabled', DATATRUCK_COMPANY_NAME: 'test', SCHEDULED_SYNCS_ENABLED: 'false',
    AUTH_COOKIE_SECURE: 'false', FRONTEND_ORIGIN: `http://127.0.0.1:${webPort}`,
    FLEETSCOPE_COMPANY_ID: webhookCompany, FLEETSCOPE_WEBHOOK_SECRET: webhookSecret,
  }});
  let apiLog = ''; api.stdout.on('data', (chunk) => { apiLog += chunk; }); api.stderr.on('data', (chunk) => { apiLog += chunk; });
  await expect.poll(async () => { if (api.exitCode !== null) throw new Error(`Test API exited: ${apiLog}`); try { return (await fetch(`http://127.0.0.1:${apiPort}/readyz`)).status; } catch { return 0; } }, { timeout: 20000 }).toBe(200);
  server = createServer(async (req, res) => {
    try {
      const url = new URL(req.url, `http://127.0.0.1:${webPort}`);
      if (url.pathname.startsWith('/api/')) {
        const chunks = []; for await (const chunk of req) chunks.push(chunk);
        const headers = {};
        for (const key of ['cookie', 'content-type', 'x-csrf-token', 'origin']) if (req.headers[key]) headers[key] = req.headers[key];
        const response = await fetch(`http://127.0.0.1:${apiPort}${url.pathname.slice(4)}${url.search}`, { method: req.method, headers, ...(chunks.length ? { body: Buffer.concat(chunks) } : {}) });
        res.writeHead(response.status, Object.fromEntries(response.headers));
        if (response.headers.get('content-type')?.includes('text/event-stream')) {
          const stream = Readable.fromWeb(response.body);
          res.on('close', () => stream.destroy());
          stream.on('error', () => res.end());
          stream.pipe(res); return;
        }
        res.end(Buffer.from(await response.arrayBuffer())); return;
      }
      const path = url.pathname === '/' ? '/index.html' : extname(url.pathname) ? url.pathname : `${url.pathname}.html`;
      const content = await readFile(join(frontend, 'out', path));
      const type = { '.txt': 'text/plain', '.html': 'text/html', '.js': 'application/javascript', '.css': 'text/css', '.svg': 'image/svg+xml', '.woff2': 'font/woff2' }[extname(path)] ?? 'application/octet-stream';
      res.writeHead(200, { 'Content-Type': type, 'Referrer-Policy': 'no-referrer' }); res.end(content);
    } catch { res.writeHead(404); res.end(); }
  });
  // Keep test proxy connections alive beyond the board's five-second autosave
  // interval so a save/read does not race Node's default idle socket shutdown.
  server.keepAliveTimeout = 60000;
  server.headersTimeout = 65000;
  await new Promise((done) => server.listen(webPort, '127.0.0.1', done));
  browser = await chromium.launch();
  const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
  const errors = []; page.on('pageerror', (error) => errors.push(error.message));
  const base = `http://127.0.0.1:${webPort}`;
  expect((await page.request.get(`${base}/api/investors`)).status()).toBe(401);
  await page.goto(`${base}/login?next=/investors`);
  await page.getByLabel('Email or existing username', { exact: true }).fill('investor-e2e@example.com');
  await page.getByLabel('Password').fill(password);
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Investors', exact: true })).toBeVisible();
  if (process.argv.includes('--payroll-workflows-only')) {
    await runPayrollWorkflowsE2E({ page, base, sql, schema, temp });
  } else if (process.argv.includes('--escrow-only')) {
    await runEscrowTasksE2E({ page, base, sql, schema, temp });
  } else if (process.argv.includes('--profiles-only')) {
    await runProfilesE2E({ page, base, sql, schema, temp });
  } else if (process.argv.includes('--assignment-week-only')) {
    await runAssignmentWeekE2E({ page, base, sql, schema });
    console.log('Assignment week E2E passed: future Monday selection and historical assignment boundaries.');
  } else {
  if (!process.argv.includes('--offboarding-only')) {
  await expect(page.getByText('No investors yet.', { exact: true })).toBeVisible();
  await expect(page.getByLabel('Show company')).not.toBeChecked();
  const companyCell = page.getByRole('cell', { name: 'MS Express Inc.', exact: true });
  await expect(companyCell).toHaveCount(0);
  const directory = await (await page.request.get(`${base}/api/investors?page=1&pageSize=25`)).json();
  expect(directory.total).toBe(0);
  expect(directory.items).toEqual([]);
  const owners = await (await page.request.get(`${base}/api/investors`)).json();
  expect(owners.some((owner) => owner.isCompany)).toBe(true);
  await page.getByLabel('Show company').check();
  await expect(companyCell).toBeVisible();
  const withCompany = await (await page.request.get(`${base}/api/investors?page=1&pageSize=25&includeCompany=true`)).json();
  expect(withCompany.total).toBe(1);
  await page.getByLabel('Show company').uncheck();
  await expect(page.getByText('No investors yet.', { exact: true })).toBeVisible();
  expect((await page.request.post(`${base}/api/investors`, { data: { fullName: 'Blocked' } })).status()).toBe(403);
  await page.getByRole('button', { name: 'Add investor', exact: true }).click();
  await page.getByLabel('Full name', { exact: true }).fill('E2e Investor');
  await page.getByLabel('Email', { exact: true }).fill('investor@example.test');
  await page.getByRole('button', { name: 'Create investor', exact: true }).click();
  await expect(page.getByRole('cell', { name: 'E2e Investor', exact: true })).toBeVisible();
  await page.reload();
  await expect(page.getByRole('cell', { name: 'E2e Investor', exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Add investor', exact: true }).click();
  await page.getByLabel('Linked driver').selectOption({ label: 'E2e Driver' });
  await expect(page.getByLabel('Full name', { exact: true })).toBeDisabled();
  await page.getByRole('button', { name: 'Create investor', exact: true }).click();
  await expect(page.getByRole('link', { name: 'E2e Driver', exact: true })).toHaveCount(0);
  await page.getByRole('button', { name: 'Add investor', exact: true }).click();
  await expect(page.getByLabel('Linked driver').locator('option')).toHaveCount(1);
  await page.getByRole('button', { name: 'Cancel', exact: true }).click();
  await page.goto(`${base}/trucks`);
  await page.getByRole('button', { name: 'Add truck', exact: true }).click();
  await page.getByLabel('Unit number', { exact: true }).fill('INV-E2E');
  await expect(page.getByLabel('Owner', { exact: false }).locator('option', { hasText: 'MS Express Inc.' })).toHaveCount(1);
  await page.getByLabel('Owner', { exact: false }).selectOption({ label: 'E2e Investor' });
  await page.getByRole('button', { name: 'Create truck', exact: true }).click();
  await expect(page.getByRole('link', { name: 'INV-E2E', exact: true })).toBeVisible();
  const truckRow = page.getByRole('row').filter({ hasText: 'INV-E2E' });
  await expect(truckRow).toContainText('E2e Investor');
  await truckRow.getByRole('button', { name: 'Edit', exact: true }).click();
  await page.getByLabel(/^Assigned driver/).selectOption({ label: 'E2e Driver' });
  await page.getByRole('button', { name: 'This week', exact: true }).click();
  await page.getByRole('button', { name: 'Save changes', exact: true }).click();
  await expect(truckRow).toContainText('E2e Driver');
  await expect(truckRow).toContainText('E2e Investor');
  await page.goto(`${base}/investors`);
  await expect(page.getByRole('row').filter({ hasText: 'E2e Investor' })).toContainText('INV-E2E');
  await page.getByPlaceholder('Search investors or truck units…').fill('INV-E2E');
  await expect(page.getByRole('row')).toHaveCount(2);
  await page.getByPlaceholder('Search investors or truck units…').fill('no-such-owner');
  await expect(page.getByText('No investors match your search.')).toBeVisible();
  await page.getByPlaceholder('Search investors or truck units…').fill('');
  await page.getByRole('button', { name: 'Edit E2e Investor', exact: true }).click();
  await page.getByLabel('Internal notes').fill('Statement contact verified');
  await page.getByLabel('Active investor').uncheck();
  await page.getByRole('button', { name: 'Save changes', exact: true }).click();
  await expect(page.getByRole('row').filter({ hasText: 'E2e Investor' })).toContainText('Inactive');
  await page.screenshot({ path: join(temp, 'investors-desktop.png'), fullPage: true });
  await runUpdatersE2E({ page, base, sql, schema, temp });
  await runPhoneE2E({ page, base });
  await runAccessE2E({ page, base, sql, schema, temp });
  await runInvestorPayE2E({ page, base, sql, schema, temp });
  await runDriverBoardE2E({ page, base, sql, schema, temp });
  await runAssignmentWeekE2E({ page, base, sql, schema });
  await runPayrollWorkflowsE2E({ page, base, sql, schema, temp });
  await page.goto(`${base}/investors`);
  await page.setViewportSize({ width: 390, height: 844 });
  await page.getByRole('button', { name: 'Collapse sidebar' }).click();
  await expect(page.locator('aside')).toHaveCSS('width', '64px');
  await expect(page.getByRole('heading', { name: 'Investors', exact: true })).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  await page.screenshot({ path: join(temp, 'investors-mobile.png'), fullPage: true });
  expect(errors).toEqual([]);
  console.log(`Investor E2E passed: login, auth/CSRF, create, driver linking, duplicate exclusion, persistence, ownership independent of operation, search, edit, inactive status, responsive layout. Screenshots: ${temp}`);
  }
  await page.setViewportSize({ width: 1440, height: 1000 });
  await runOffboardingE2E({ page, base, sql, schema, apiBase: `http://127.0.0.1:${apiPort}`, webhookCompany, webhookSecret });
  }
  expect(errors).toEqual([]);
} finally {
  await browser?.close();
  if (server) await new Promise((done) => server.close(done));
  if (api && api.exitCode === null) { const exited = new Promise((done) => api.once('exit', done)); api.kill(); await exited; }
  sql(`DROP SCHEMA IF EXISTS ${schema} CASCADE;`);
}
