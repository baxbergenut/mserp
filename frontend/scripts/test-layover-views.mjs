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
let settings = { layoverDailyAmount: '100.00', version: 1 };
let edits = { driverId: 'driver', weekStart: week, version: 0, notes: '', comments: {}, adjustments: [], fuelOverride: null, tollOverride: null, layoverExcludedDates: [] };
let writes = 0, finalized = false;
let browser;
try {
  browser = await chromium.launch();
  const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
  const errors = [];
  page.on('pageerror', error => errors.push(error.message));
  await page.route('**/api/**', async route => {
    const request = route.request(), url = new URL(request.url()), path = url.pathname.slice(4);
    if (path === '/settings/driver-pay') {
      if (request.method() === 'PUT') {
        const input = request.postDataJSON();
        expect(input.version).toBe(settings.version);
        settings = { ...input, layoverDailyAmount: Number(input.layoverDailyAmount).toFixed(2), version: input.version + 1 };
      }
      await route.fulfill({ json: settings }); return;
    }
    if (path === '/driver-pay') {
      if (request.method() === 'PUT') {
        const input = request.postDataJSON();
        expect(input.version).toBe(edits.version);
        edits = { ...input, version: edits.version + 1 }; writes++;
        await route.fulfill({ json: edits }); return;
      }
      const selectedWeek = url.searchParams.get('weekStart');
      const driver = { id: 'driver', fullName: 'Shop Driver', truckUnit: '', dispatcherId: '', dispatcherName: 'Unassigned', isOwnerOperator: false, employmentType: 'company', payType: 'cpm', payRate: '0.75', loads: [], fuelTotal: '0.00', tollTotal: '0.00', edits, layovers: ['2026-10-05', '2026-10-06'].map(date => ({ date, amount: settings.layoverDailyAmount })), ...(finalized && { settlement: { finalized: true } }) };
      await route.fulfill({ json: { weekStart: selectedWeek, revision: 'fixture', drivers: selectedWeek === week ? [driver] : [] } }); return;
    }
    const fixtures = {
      '/auth/session': { user: { id: 'user', username: 'Test user', permissions: ['access.manage', 'payroll.read', 'payroll.write', 'fleet.read'] }, csrfToken: 'fixture' },
      '/settings/access': { users: [], roles: [], permissions: [], expenseCategories: [] },
      '/sidebar-counts': {}, '/tasks/count': { count: 0 },
    };
    await route.fulfill({ json: fixtures[path] ?? [] });
  });
  await page.goto(`${base}/settings`);
  await page.getByRole('button', { name: 'Driver Pay', exact: true }).click();
  await expect(page.getByLabel('Daily layover amount ($)', { exact: true })).toHaveValue('100.00');
  await page.getByLabel('Daily layover amount ($)', { exact: true }).fill('125.25');
  await page.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(page.getByText('Driver Pay settings saved.')).toBeVisible();
  await page.reload();
  await expect(page.getByLabel('Daily layover amount ($)', { exact: true })).toHaveValue('125.25');
  await page.goto(`${base}/accounting/driver-pay?weekStart=${week}&driverId=driver`);
  await expect(page.locator('td[title="Total payable"]')).toHaveText('$250.50');
  const remove = date => page.getByRole('button', { name: `Shop Driver, remove layover for ${date}`, exact: true });
  await expect(remove('2026-10-05')).toBeVisible();
  await remove('2026-10-05').scrollIntoViewIfNeeded();
  await page.screenshot({ path: join(tmpdir(), 'mserp-layover-before.png'), fullPage: true, animations: 'disabled' });
  await remove('2026-10-05').click();
  await expect.poll(() => writes).toBe(1);
  await expect(page.locator('td[title="Total payable"]')).toHaveText('$125.25');
  await page.reload();
  await expect(remove('2026-10-05')).toHaveCount(0);
  await expect(remove('2026-10-06')).toBeVisible();
  await remove('2026-10-06').click();
  await expect.poll(() => writes).toBe(2);
  await page.locator('body').click({ position: { x: 900, y: 100 } });
  await page.keyboard.press('Control+z');
  await expect.poll(() => writes).toBe(3);
  await expect(remove('2026-10-06')).toBeVisible();
  await page.reload();
  await expect(remove('2026-10-05')).toHaveCount(0);
  await expect(remove('2026-10-06')).toBeVisible();
  await page.setViewportSize({ width: 760, height: 900 });
  await remove('2026-10-06').scrollIntoViewIfNeeded();
  await page.screenshot({ path: join(tmpdir(), 'mserp-layover-compact.png'), fullPage: true, animations: 'disabled' });
  finalized = true;
  await page.reload();
  await expect(remove('2026-10-06')).toBeDisabled();
  await page.goto(`${base}/accounting/driver-pay?weekStart=2026-10-12&driverId=`);
  await expect(page.getByText('No statements match this view.')).toBeVisible();
  expect(errors).toEqual([]);
  console.log('Layover browser checks passed: settings save/reload, positive bonus, removal/autosave/reload/undo, finalized lock, compact layout, and next week.');
} finally {
  await browser?.close();
  await new Promise(resolve => server.close(resolve));
}
