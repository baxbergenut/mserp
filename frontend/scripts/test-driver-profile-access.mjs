// Real static frontend with permission-scoped API fixtures; no provider calls.
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
  for (const permissions of [['drivers.read'], ['drivers.read', 'drivers.create', 'drivers.edit', 'trucks.read']]) {
    const page = await browser.newPage();
    const errors = [], unexpected = [];
    let serviceReads = 0;
    let driverStatus = { active: true, availabilityStatus: 'active' };
    page.on('pageerror', error => errors.push(error.message));
    await page.route('**/api/**', async route => {
      const path = new URL(route.request().url()).pathname.slice(4);
      let data;
      if (path === '/auth/session') data = { user: { id: 'dispatcher', username: 'Dispatcher', permissions, expenseCategoryAccess: [], theme: 'default' }, csrfToken: 'fixture' };
      else if (path === '/auth/birthday') data = { eligible: false, serverTime: new Date().toISOString() };
      else if (path === '/drivers/driver') data = { id: 'driver', fullName: 'Profile Driver', status: 'active', employmentType: 'company', updatedAt: '2026-10-28T12:00:00Z', ...driverStatus };
      else if (path === '/driver-services/driver') {
        serviceReads++;
        // The API deliberately hides Relay policies and other service cards.
        data = { driverId: 'driver', name: 'Profile Driver', driverStatus: 'active', tomorrow: '2026-10-29', checkedAt: new Date().toISOString(), policies: null, canLinkRelay: false, items: [{ service: 'datatruck', state: 'added', requestState: 'done', dataTruckId: 123, version: 1, configured: true, candidates: [], operations: [] }] };
      } else if (path.endsWith('/notes')) data = [];
      else if (path.endsWith('/location')) data = { truckId: null, location: null };
      else if (path === '/sidebar-counts') data = { fuel: 0, weighMyTruck: 0, tolls: 0 };
      else if (path === '/feedback/messages/count') data = { count: 0 };
      else if (path === '/maintenance/access') data = {};
      else { unexpected.push(path); await route.fulfill({ status: 403, json: { error: 'Permission denied' } }); return; }
      await route.fulfill({ json: data });
    });
    await page.goto(`${base}/drivers/detail?id=driver`);
    await expect(page.getByRole('heading', { name: 'Profile Driver', exact: true })).toBeVisible();
    const title = page.locator('[data-page-header]');
    for (const [availabilityStatus, label] of [['vacation', 'Vacation'], ['home', 'Home'], ['rest', 'Rest'], ['reset', '34 Reset'], ['family_issue', 'Family issue'], ['health_issue', 'Health issue'], ['new_driver', 'New driver'], ['active', 'Active'], [undefined, 'Active']]) {
      driverStatus = { active: true, availabilityStatus };
      await page.evaluate(() => window.dispatchEvent(new Event('focus')));
      await expect(title.getByText(label, { exact: true })).toBeVisible();
      await expect(page.locator('dl > div').filter({ has: page.locator('dt', { hasText: /^Availability$/ }) }).locator('dd')).toHaveText(label);
    }
    driverStatus = { active: false, availabilityStatus: 'vacation' };
    await page.evaluate(() => window.dispatchEvent(new Event('focus')));
    await expect(title.getByText('Terminated', { exact: true })).toBeVisible();
    await expect(title.getByText('Vacation', { exact: true })).toHaveCount(0);
    await expect(page.getByLabel('DataTruck setup')).toContainText('DataTruck ID: 123');
    await expect(page.getByLabel('Relay setup')).toHaveCount(0);
    await expect(page.getByText('License & documents', { exact: true })).toHaveCount(0);
    await expect(page.getByRole('tab', { name: 'Pay history', exact: true })).toHaveCount(0);
    const priorReads = serviceReads;
    await page.evaluate(() => window.dispatchEvent(new Event('focus')));
    await expect.poll(() => serviceReads).toBeGreaterThan(priorReads);
    await expect(page.getByLabel('DataTruck setup')).toBeVisible();
    await expect(page.getByRole('heading', { name: 'Profile Driver', exact: true })).toBeVisible();
    expect(errors).toEqual([]);
    expect(unexpected).toEqual([]);
    await page.close();
  }
  console.log('Driver profile access passed: availability headers and refresh, termination precedence, View Drivers alone and dispatcher editing grants, null Relay policies, service refresh, document/payroll isolation.');
} finally {
  await browser.close();
  server.closeAllConnections();
  await new Promise(resolve => server.close(resolve));
}
