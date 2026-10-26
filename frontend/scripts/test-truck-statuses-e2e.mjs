// Real local API + database checks. Never run against production.
import { readFile, mkdir } from 'node:fs/promises';
import { dirname, join } from 'node:path';
import { execFileSync } from 'node:child_process';
import { randomUUID } from 'node:crypto';
import { chromium, expect } from '@playwright/test';

const access = JSON.parse(await readFile(process.env.MSERP_REVIEW_ACCESS_FILE, 'utf8'));
const database = new URL(process.env.MSERP_REVIEW_TEST_DATABASE_URL);
if (!['localhost', '127.0.0.1'].includes(database.hostname) || !database.pathname.endsWith('_test') || !['localhost', '127.0.0.1'].includes(new URL(access.base).hostname) || !/^review_[a-f0-9]+$/.test(access.schema)) throw Error('Disposable local review required');
const cliDatabase = new URL(database); cliDatabase.searchParams.set('search_path', access.schema + ',public'); cliDatabase.searchParams.set('role', 'mserp_app');
const cli = args => execFileSync(join(dirname(process.env.MSERP_REVIEW_ACCESS_FILE), process.platform === 'win32' ? 'api.exe' : 'api'), args, {cwd: dirname(process.env.MSERP_REVIEW_ACCESS_FILE), env: {...process.env, DATABASE_URL: cliDatabase.toString(), DATATRUCK_API_KEY: 'test-disabled', DATATRUCK_COMPANY_NAME: 'test', RELAY_API_KEY: 'test-disabled', RELAY_PRODUCTION_API_KEY: 'test-disabled', PREPASS_CLIENT_ID: 'test-disabled', PREPASS_CLIENT_SECRET: 'test-disabled', AUTH_COOKIE_SECURE: 'false', FRONTEND_ORIGIN: access.base, SCHEDULED_SYNCS_ENABLED: 'false'}, encoding: 'utf8', windowsHide: true});
const preview = JSON.parse(cli(['-fleet-status-preview']));
if (!preview.enabled) cli(['-fleet-status-activate', preview.revision]);
const prefix = 'STATUS-' + randomUUID().slice(0, 8).toUpperCase();
const driverId = randomUUID(), inUseId = randomUUID();
execFileSync(process.env.PSQL_PATH || 'psql', ['-X', '-q', '-v', 'ON_ERROR_STOP=1', '-d', database.toString()], {
  input: `SET search_path TO ${access.schema},public;
  INSERT INTO trucks(unit_number,status,status_reason) VALUES
  ('${prefix}-Z-READY','available',''),('${prefix}-Y-SHOP','maintenance','Brake repair'),
  ('${prefix}-B-OOS','out_of_service','Awaiting engine replacement'),('${prefix}-A-TERMINATED','terminated','Owner left company');
  INSERT INTO drivers(id,full_name,normalized_name,pay_type,pay_rate) VALUES('${driverId}','Truck Status Browser ${prefix}','truck status browser ${prefix}','cpm',0.7);
  INSERT INTO trucks(id,unit_number) VALUES('${inUseId}','${prefix}-X-USE');
  INSERT INTO truck_driver_assignments(truck_id,driver_id) VALUES('${inUseId}','${driverId}');
  INSERT INTO trucks(unit_number,status,status_reason) SELECT '${prefix}-A-'||lpad(n::text,2,'0'),'terminated','Synthetic departed truck' FROM generate_series(1,25) n;`,
  encoding: 'utf8', windowsHide: true, stdio: ['pipe', 'pipe', 'pipe'],
});
const artifacts = join(dirname(process.env.MSERP_REVIEW_ACCESS_FILE), 'truck-statuses');
await mkdir(artifacts, { recursive: true });
const browser = await chromium.launch();
try {
  const context = await browser.newContext({ viewport: { width: 1440, height: 1000 } });
  const page = await context.newPage(), errors = [];
  page.on('pageerror', e => errors.push(e.message));
  await page.goto(access.base + '/login');
  await page.getByLabel('Email or existing username').fill(access.email);
  await page.getByLabel('Password', { exact: true }).fill(access.password);
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await page.waitForURL(u => u.pathname !== '/login');
  const session = await (await page.request.get(access.base + '/api/auth/session')).json();
  const labels = {ready: 'Ready', shop: 'Shop', in_use: 'In use', out_of_service: 'OOS', terminated: 'Terminated'};
  const card = code => page.getByRole('group', {name: 'Filter truck status'}).getByRole('button', {name: new RegExp('^' + (labels[code] || 'All') + ',')});
  const headers = { 'X-CSRF-Token': session.csrfToken, Origin: access.base };
  const api = async (method, path, data) => {
    const r = await page.request[method](access.base + '/api' + path, { headers, ...(data ? { data } : {}) });
    if (!r.ok()) throw Error(path + ': ' + r.status() + ' ' + await r.text());
    return r.status() === 204 ? null : r.json();
  };
  const first = await api('get', `/trucks?page=1&pageSize=25&search=${prefix}`);
  expect(first.total).toBe(30);
  expect(first.statusCounts).toEqual({ready:1,shop:1,in_use:1,out_of_service:1,terminated:26});
  expect(first.items.slice(0, 4).map(t => t.operatingStatus)).toEqual(['ready', 'shop', 'in_use', 'out_of_service']);
  const second = await api('get', `/trucks?page=2&pageSize=25&search=${prefix}`);
  expect(second.items).toHaveLength(5);
  expect(second.items.every(t => t.operatingStatus === 'terminated')).toBe(true);
  await page.goto(access.base + '/trucks');
  await page.getByPlaceholder('Search trucks…').fill(prefix);
  await expect(page.getByRole('table').locator('tbody tr')).toHaveCount(25);
  expect(await page.getByRole('columnheader').allTextContents()).toEqual(['Unit', 'Equipment', 'Driver', 'Status', 'Status notes']);
  await expect(page.getByRole('table').locator('tbody tr td:nth-child(4)')).toHaveText(first.items.map(t => ({ ready: 'Ready', shop: 'Shop', in_use: 'In use', out_of_service: 'OOS', terminated: 'Terminated' })[t.operatingStatus]));
  await expect(page.getByRole('table').locator('tbody tr td:last-child')).toHaveText(first.items.map(t => t.statusReason?.trim() || '—'));
  await expect(card('')).toHaveAttribute('aria-pressed', 'true');
  await expect(card('')).toContainText('30');
  await page.screenshot({ path: join(artifacts, 'trucks-all.png'), fullPage: true });
  await page.getByRole('cell', { name: 'Brake repair', exact: true }).click();
  await expect(page).toHaveURL(new RegExp('/trucks/detail\\?id=' + first.items[1].id));
  await page.goBack();
  const readyRow = page.getByRole('row', { name: `Open truck ${prefix}-Z-READY details`, exact: true });
  await readyRow.focus();
  await readyRow.press('Enter');
  await expect(page).toHaveURL(new RegExp('/trucks/detail\\?id=' + first.items[0].id));
  await page.goBack();
  for (const [status, count] of [['ready', 1], ['shop', 1], ['in_use', 1], ['out_of_service', 1], ['terminated', 25]]) {
    await card(status).click();
    await expect(card(status)).toHaveAttribute('aria-pressed', 'true');
    await expect(card('')).toHaveAttribute('aria-pressed', 'false');
    await expect(card(status)).toContainText(status === 'terminated' ? '26' : '1');
    await expect(page.getByRole('table').locator('tbody tr')).toHaveCount(count);
    const filtered = await api('get', `/trucks?page=1&pageSize=25&search=${prefix}&status=${status}`);
    expect(filtered.statusCounts).toEqual(first.statusCounts);
    expect(filtered.total).toBe(status === 'terminated' ? 26 : 1);
  }
  await card('in_use').click();
  await page.getByRole('row', { name: `Open truck ${prefix}-X-USE details`, exact: true }).locator('td:nth-child(3)').click();
  await expect(page.getByRole('button', { name: 'Change status', exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Delete truck', exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Edit truck', exact: true }).click();
  await expect(page.getByText('Active truck', { exact: true })).toHaveCount(0);
  await page.getByRole('button', { name: 'Cancel', exact: true }).click();
  await page.getByRole('button', { name: 'Change status', exact: true }).click();
  await page.getByLabel('Truck status', { exact: true }).selectOption('out_of_service');
  await page.getByRole('button', { name: 'Save status', exact: true }).click();
  await expect(page.getByRole('dialog')).toBeVisible();
  await page.getByLabel('Reason', { exact: true }).fill('Browser test engine repair');
  await page.getByRole('button', { name: 'Save status', exact: true }).click();
  await expect(page.getByRole('dialog')).toHaveCount(0);
  let truck = await api('get', '/trucks/' + inUseId);
  expect(truck.operatingStatus).toBe('out_of_service'); expect(truck.active).toBe(false); expect(truck.driverId).toBe(driverId);
  await expect(page.getByText('Browser test engine repair', { exact: true })).toBeVisible();
  await page.screenshot({ path: join(artifacts, 'truck-oos.png'), fullPage: true });
  await page.getByRole('button', { name: 'Return to service', exact: true }).click();
  await page.getByLabel('Reason', { exact: true }).fill('Engine repaired and checked');
  await page.getByRole('button', { name: 'Save status', exact: true }).click();
  await expect(page.getByRole('dialog')).toHaveCount(0);
  truck = await api('get', '/trucks/' + inUseId); expect(truck.operatingStatus).toBe('in_use');
  expect((await page.request.patch(access.base + '/api/trucks/' + inUseId + '/status', { headers, data: { status: 'terminated', version: truck.statusVersion, reason: 'Cannot terminate employed crew' } })).ok()).toBe(false);
  expect((await page.request.patch(access.base + '/api/trucks/' + inUseId + '/condition', { headers, data: { status: 'shop', version: truck.statusVersion, reason: '' } })).ok()).toBe(false);
  await page.goBack();
  await expect(card('in_use')).toHaveAttribute('aria-pressed', 'true');
  await expect(page.getByPlaceholder('Search trucks…')).toHaveValue(prefix);
  expect(errors).toEqual([]);
  console.log('Truck status E2E passed: server pagination/order/filtering, required reasons, OOS assignment preservation, returns, legacy endpoint validation, navigation memory and profile actions. Screenshots: ' + artifacts);
} finally { await browser.close(); }
