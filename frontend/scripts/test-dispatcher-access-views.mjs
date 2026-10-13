import { chromium, expect } from '@playwright/test';
import { createServer } from 'node:http';
import { readFile, mkdtemp } from 'node:fs/promises';
import { extname, join } from 'node:path';
import { tmpdir } from 'node:os';

const id = '11111111-1111-4111-8111-111111111111';
let permission = true, writes = 0;
const accessByKind = Object.fromEntries(['dispatcher', 'updater'].map(kind => [kind, { [`${kind}Id`]: id, enabled: false, accountLabel: '', version: 0, activatedAt: null, tokenExpiresAt: null, activationExpiresAt: null }]));
const server = createServer(async (req, res) => {
  try {
    const path = new URL(req.url, 'http://localhost').pathname;
    const file = extname(path) ? path.slice(1) : `${path.slice(1)}.html`;
    const body = await readFile(new URL(`../out/${file}`, import.meta.url));
    res.writeHead(200, { 'Content-Type': { '.html': 'text/html', '.js': 'application/javascript', '.css': 'text/css', '.txt': 'text/plain' }[extname(file)] || 'application/octet-stream' });res.end(body);
  } catch { res.writeHead(404);res.end(); }
});
await new Promise(r => server.listen(0, '127.0.0.1', r));
const browser = await chromium.launch();
try {
  const page = await browser.newPage({ viewport: { width: 1360, height: 1000 } });
  const errors = [];page.on('pageerror', e => errors.push(e.message));
  await page.route('**/api/**', async route => {
    const path = new URL(route.request().url()).pathname.slice(4);
    const kind = ['dispatcher', 'updater'].find(kind => path === `/settings/${kind}-extension/${id}`);
    if (kind) {
      const access = accessByKind[kind];
      if (route.request().method() === 'PUT') {
        const data = route.request().postDataJSON();
        if (data.version !== access.version) return route.fulfill({ status: 409, json: { error: 'Access changed. Reload.' } });
        writes++;accessByKind[kind] = { ...access, ...data, version: access.version + 1 };
        return route.fulfill({ json: { activationCode: data.enabled ? 'synthetic-activation-code-only' : '' } });
      }
      return route.fulfill({ json: access });
    }
    const fixture = {
      '/auth/session': { user: { id: 'admin', username: 'Test Admin', theme: 'default', permissions: ['fleet.read', ...(permission ? ['access.manage'] : [])] }, csrfToken: 'fixture' },
      [`/dispatchers/${id}`]: { id, fullName: 'Test Dispatcher', active: true, email: 'dispatcher@example.test', extension: null, mainUpdaterId: null, afterHoursUpdaterId: null, payPercentage: null },
      '/dispatchers': { items: [], total: 0, page: 1, pageSize: 25, totalPages: 1 },
      '/drivers': [], '/updaters': [{ id, fullName: 'Roy', shift: 'main', extension: 106, version: 1, dispatcherNames: [] }], '/tasks/count': { count: 0 }, '/sidebar-counts': {},
    }[path];
    return route.fulfill({ status: fixture === undefined ? 404 : 200, json: fixture ?? { error: 'fixture not found' } });
  });
  const url = `http://127.0.0.1:${server.address().port}/dispatchers/detail?id=${id}`;
  await page.goto(url);
  await page.getByLabel('J.B. Hunt account reference (optional)').fill('dispatch@example.test');
  await page.getByRole('button', { name: 'Enable and create code' }).click();
  await expect(page.getByLabel('Extension activation code')).toHaveValue('synthetic-activation-code-only');
  await expect(page.getByRole('button', { name: 'Replace activation code' })).toBeVisible();
  await page.getByRole('button', { name: 'Revoke access' }).click();
  await expect(page.getByText('Access disabled', { exact: true })).toBeVisible();
  await expect(page.getByLabel('Extension activation code')).toHaveCount(0);
  await page.setViewportSize({ width: 480, height: 900 });
  await expect(page.getByRole('button', { name: 'Enable and create code' })).toBeVisible();
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth > innerWidth);
  if (overflow) throw new Error('Narrow dispatcher page overflows');
  const artifacts = await mkdtemp(join(tmpdir(), 'mserp-dispatcher-ui-'));
  await page.screenshot({ path: join(artifacts, 'narrow.png'), fullPage: true });
  permission = false;await page.reload();
  await expect(page.getByRole('heading', { name: 'Assigned drivers' })).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Dispatcher Chrome extension' })).toHaveCount(0);
  permission = true;
  await page.setViewportSize({ width: 1360, height: 1000 });
  await page.goto(new URL('/dispatchers', url).href);
  await page.getByRole('button', { name: 'Chrome extension for Roy' }).click();
  await page.getByRole('button', { name: 'Enable and create code' }).click();
  await expect(page.getByLabel('Extension activation code')).toHaveValue('synthetic-activation-code-only');
  await expect(page.getByRole('dialog', { name: 'Chrome extension · Roy' })).toBeVisible();
  await page.setViewportSize({ width: 480, height: 900 });
  await expect(page.getByRole('button', { name: 'Replace activation code' })).toBeVisible();
  await page.getByRole('button', { name: 'Revoke access' }).click();
  await expect(page.getByLabel('Extension activation code')).toHaveCount(0);
  await expect(page.getByText('Access disabled', { exact: true })).toBeVisible();
  await page.screenshot({ path: join(artifacts, 'updater.png'), fullPage: true });
  await page.getByRole('button', { name: 'Done', exact: true }).click();
  permission = false;await page.reload();
  await expect(page.getByRole('cell', { name: 'Roy', exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Chrome extension for Roy' })).toHaveCount(0);
  if (writes !== 4 || errors.length) throw new Error(`Unexpected UI errors or writes: ${errors.join('; ')} / ${writes}`);
  console.log('Dispatcher and updater access UI: issue, revoke, code removal, narrow layout and permissions passed.');
  console.log(`Preview: ${join(artifacts, 'narrow.png')}`);
} finally { await browser.close(); await new Promise(r => server.close(r)); }
