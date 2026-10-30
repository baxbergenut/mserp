// Static-build browser regression with isolated API fixtures.
import { chromium, expect } from '@playwright/test';
import { createServer } from 'node:http';
import { readFile } from 'node:fs/promises';
import { extname, join } from 'node:path';
import { tmpdir } from 'node:os';

const server = createServer(async (req, res) => {
  try {
    const path = new URL(req.url, 'http://localhost').pathname;
    const file = extname(path) ? path.slice(1) : `${path.slice(1)}.html`;
    const body = await readFile(new URL(`../out/${file}`, import.meta.url));
    res.writeHead(200, { 'Content-Type': { '.html': 'text/html', '.js': 'application/javascript', '.css': 'text/css', '.txt': 'text/plain' }[extname(file)] ?? 'application/octet-stream' });
    res.end(body);
  } catch { res.writeHead(404); res.end(); }
});
await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
const base = `http://127.0.0.1:${server.address().port}`;
const week = '2026-10-05';
let settingsRequests = 0;
let edits = { driverId: 'driver', weekStart: week, version: 0, notes: '', comments: {}, adjustments: [], fuelOverride: null, tollOverride: null, layoverExcludedDates: [] };
let writes = 0, finalized = false;
let browser;
try {
  browser = await chromium.launch();
  const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
  const errors = [];
  page.on('pageerror', error => errors.push(error.message));
  await page.route('**/api/**', async route => {
    const request = route.request(), url = new URL(request.url()), path = url.pathname.replace('/lookups/','/').slice(4);
    if (path === '/settings/driver-pay') { settingsRequests++; await route.fulfill({ status: 404 }); return; }
    if (path === '/driver-pay') {
      if (request.method() === 'PUT') {
        const input = request.postDataJSON();
        expect(input.version).toBe(edits.version);
        edits = { ...input, version: edits.version + 1 }; writes++;
        await route.fulfill({ json: edits }); return;
      }
      const selectedWeek = url.searchParams.get('weekStart');
      const driver = { id: 'driver', fullName: 'Shop Driver', truckUnit: '', dispatcherId: '', dispatcherName: 'Unassigned', isOwnerOperator: false, employmentType: 'company', payType: 'cpm', payRate: '0.75', loads: [], fuelTotal: '0.00', tollTotal: '0.00', edits, dayStatuses: [{ date: '2026-10-05', status: 'SHOP' }], ...(finalized && { layovers: ['2026-10-05', '2026-10-06'].map(date => ({ date, amount: '125.25' })), edits: { ...edits, layoverExcludedDates: ['2026-10-05'] }, settlement: { finalized: true } }) };
      await route.fulfill({ json: { weekStart: selectedWeek, revision: 'fixture', drivers: selectedWeek === week ? [driver] : [] } }); return;
    }
    const fixtures = {
      '/auth/session': { user: { id: 'user', username: 'Test user', permissions: ['access.manage', 'driver_pay.read','investor_pay.read','dispatcher_pay.read', 'driver_pay.edit','investor_pay.edit', 'drivers.read','trucks.read','dispatchers.read','updaters.read','investors.read'] }, csrfToken: 'fixture' },
      '/settings/access': { users: [], roles: [], permissions: [], expenseCategories: [] },
      '/sidebar-counts': {}, '/tasks/count': { count: 0 },
    };
    await route.fulfill({ json: fixtures[path] ?? [] });
  });
  await page.goto(`${base}/settings`);
  await expect(page.getByRole('tab', { name: 'Appearance', exact: true })).toBeVisible();
  await expect(page.getByRole('tab', { name: 'Driver Pay', exact: true })).toHaveCount(0);
  await expect(page.getByLabel('Daily layover amount ($)', { exact: true })).toHaveCount(0);
  expect(settingsRequests).toBe(0);
  await page.goto(`${base}/accounting/driver-pay?weekStart=${week}&driverId=driver`);
  await expect(page.locator('td[title="Total payable"]')).toHaveText('$0.00');
  await expect(page.getByText(/Layover ·/)).toHaveCount(0);
  finalized = true;
  await page.reload();
  await expect(page.locator('td[title="Total payable"]')).toHaveText('$125.25');
  await expect(page.getByText('Layover · 10/06')).toBeVisible();
  await expect(page.getByRole('button', { name: /remove layover/ })).toHaveCount(0);
  await page.screenshot({ path: join(tmpdir(), 'mserp-layover-retired.png'), fullPage: true, animations: 'disabled' });
  await page.setViewportSize({ width: 760, height: 900 });
  await expect(page.getByText('Layover · 10/06')).toBeVisible();
  expect(writes).toBe(0);
  await page.goto(`${base}/accounting/driver-pay?weekStart=2026-10-12&driverId=`);
  await expect(page.getByText('No statements match this view.')).toBeVisible();
  expect(errors).toEqual([]);
  console.log('Layover retirement browser checks passed: settings removed, open statement has no bonus, finalized history stays read-only, compact layout, and next week.');
} finally {
  await browser?.close();
  await new Promise(resolve => server.close(resolve));
}
