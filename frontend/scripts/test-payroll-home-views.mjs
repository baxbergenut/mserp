// Check the shared collapsed/expanded payroll row against isolated API fixtures.
import { createServer } from 'node:http';
import { readFile } from 'node:fs/promises';
import { extname } from 'node:path';
import { chromium, expect } from '@playwright/test';

const server = createServer(async (req, res) => {
  try {
    const path = new URL(req.url, 'http://localhost').pathname;
    const file = path === '/' ? 'index.html' : extname(path) ? path.slice(1) : `${path.slice(1)}.html`;
    const body = await readFile(new URL(`../out/${file}`, import.meta.url));
    res.writeHead(200, { 'Content-Type': { '.html': 'text/html', '.js': 'application/javascript', '.css': 'text/css', '.txt': 'text/plain' }[extname(file)] ?? 'application/octet-stream' });
    res.end(body);
  } catch { res.writeHead(404); res.end(); }
});
await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
const base = `http://127.0.0.1:${server.address().port}`;
const browser = await chromium.launch();
try {
  for (const investor of [false, true]) {
    const page = await browser.newPage();
    const errors = [];
    page.on('pageerror', error => errors.push(error.message));
    const week = '2026-10-05';
    let homeWeek = true, loads = 0;
    await page.route('**/api/**', async route => {
      const path = new URL(route.request().url()).pathname.slice(4);
      let data = [];
      if (path === '/auth/session') data = { user: { id: 'test', username: 'Test', permissions: ['driver_pay.read', 'investor_pay.read', 'drivers.read', 'investors.read'], expenseCategoryAccess: [], theme: 'default' }, csrfToken: 'fixture' };
      else if (path === '/settings/status-colors') data = {colors:{home:'#cc44aa'},version:1};
      else if (path === '/auth/birthday') data = { eligible: false, serverTime: new Date().toISOString() };
      else if (path === '/driver-pay' || path === '/investor-pay') data = { weekStart: week, revision: 'fixture', drivers: [{ id: 'home', fullName: 'Home Driver', homeWeek, ...(investor ? { investorId: 'owner', operatingDrivers: [{ id: 'operator', name: 'Operator' }] } : {}), payType: 'cpm', payRate: '0.75', truckUnit: 'HOME-1', loads: [], fuelTotal: '0', tollTotal: '0', summary: { loads, missingFees: 0, review: 0, fee: '0', payable: '0' }, edits: { driverId: 'home', weekStart: week, version: 0, comments: {}, adjustments: [], fuelOverride: null, tollOverride: null } }] };
      await route.fulfill({ json: data });
    });
    const url = `${base}/accounting/${investor ? 'investor' : 'driver'}-pay?weekStart=${week}`;
    await page.goto(url);
    const badge = page.getByTitle('Home every day of this week, with no loads', { exact: true });
    await expect(badge).toHaveText('Home');
    await expect.poll(() => badge.evaluate(el => getComputedStyle(el).getPropertyValue('--status-base').trim())).toBe('#cc44aa');
    const cell = badge.locator('..');
    await expect(cell.getByRole('link', { name: investor ? 'Operator' : 'Home Driver', exact: true })).toBeVisible();
    homeWeek = false;
    await page.reload();
    await expect(page.getByRole('button', { name: 'Home Driver', exact: !investor })).toBeVisible();
    await expect(badge).toHaveCount(0);
    homeWeek = true; loads = 1;
    await page.reload();
    await expect(page.getByRole('button', { name: 'Home Driver', exact: !investor })).toBeVisible();
    await expect(badge).toHaveCount(0);
    expect(errors).toEqual([]);
    await page.close();
  }
  console.log('Payroll Home badges passed for Driver Pay and Investor Pay, compact rows, driver placement, partial weeks, shared custom colors and load suppression.');
} finally {
  await browser.close();
  server.closeAllConnections();
  await new Promise(resolve => server.close(resolve));
}
