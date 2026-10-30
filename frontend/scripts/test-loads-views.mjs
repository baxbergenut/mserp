// Run against the /api static build. Repository tests cover source projection.
import { chromium, expect } from '@playwright/test';
import { createServer } from 'node:http';
import { readFile } from 'node:fs/promises';
import { extname } from 'node:path';

const server = createServer(async (req, res) => {
  try {
    const path = new URL(req.url, 'http://localhost').pathname;
    const file = path === '/' ? 'index.html' : extname(path) ? path.slice(1) : `${path.slice(1)}.html`;
    const body = await readFile(new URL(`../out/${file}`, import.meta.url));
    res.writeHead(200, { 'Content-Type': { '.html': 'text/html', '.js': 'application/javascript', '.css': 'text/css', '.txt': 'text/plain' }[extname(file)] ?? 'application/octet-stream' }); res.end(body);
  } catch { res.writeHead(404); res.end(); }
});
await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
const base = `http://127.0.0.1:${server.address().port}`;
let browser;
try {
  browser = await chromium.launch();
  const page = await browser.newPage({ viewport: { width: 1440, height: 900 } });
  const errors = [];
  page.on('pageerror', error => errors.push(error.message));
  const stop = (type, address, city = '', state = '', zipCode = '') => ({ type, address, city, state, zipCode });
  const makeLoad = (ID, Stops) => ({ ID, LoadID: 'SHARED-123', CustomerName: `Customer ${ID}`, DriverName: 'Alex Driver', TruckUnit: '100', Status: 'delivered', PickupTime: '2026-10-26T12:00:00Z', DeliveryTime: '2026-10-28T12:00:00Z', TotalMiles: '500', PerMileRevenue: '2', TotalPay: '1000', Stops });
  const loads = [makeLoad(1, [
    stop('pickup', '100 First St, Dock 2', 'Columbus', 'OH', '43215'),
    stop('delivery', '200 Middle St', 'Toledo', 'OH', '43601'),
    stop('pickup', '300 Third St', 'Detroit', 'MI', '48201'),
    stop('delivery', '400 Last St', 'Chicago', 'IL', '06001'),
    stop('delivery', '', 'Unknown street', 'IL'),
  ]), makeLoad(2, []), makeLoad(3, [stop('pickup', 'A very long complete address '.repeat(18))])];
  let mode = 'normal';
  const queries = [];
  await page.route('**/api/**', async route => {
    const url = new URL(route.request().url());
    if (url.pathname === '/api/auth/session') return route.fulfill({ json: { user: { id: 'viewer', username: 'Loads viewer', permissions: ['loads.read'], theme: 'default' }, csrfToken: 'test' } });
    if (url.pathname === '/api/loads') {
      queries.push(url.search);
      return route.fulfill({ json: { items: mode === 'empty' ? [] : url.searchParams.get('page') === '2' ? [loads[1]] : loads, page: Number(url.searchParams.get('page') || 1), pageSize: 25, total: mode === 'empty' ? 0 : 26, totalPages: mode === 'empty' ? 1 : 2, options: { statuses: ['delivered'], customers: [], drivers: [], dispatchers: [] } } });
    }
    return route.fulfill({ json: [] });
  });
  await page.goto(base + '/loads');
  const toggles = () => page.getByRole('button', { name: 'Load SHARED-123 stops', exact: true });
  await expect(toggles()).toHaveCount(3);
  await expect(toggles().first()).toHaveAttribute('aria-expanded', 'false');
  await expect(page.getByText('100 First St, Dock 2', { exact: true })).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Sync DataTruck' })).toHaveCount(0);
  await toggles().first().focus();
  await page.keyboard.press('Enter');
  await expect(toggles().first()).toHaveAttribute('aria-expanded', 'true');
  const details = page.locator('#load-1-stops');
  await expect(details.locator('tbody tr')).toHaveCount(5);
  expect(await details.locator('tbody tr td:nth-child(2)').allTextContents()).toEqual(['Pickup', 'Delivery', 'Pickup', 'Delivery', 'Delivery']);
  for (const text of ['100 First St, Dock 2', '200 Middle St', '300 Third St', '400 Last St', 'Chicago, IL 06001', 'Address unavailable']) await expect(details).toContainText(text);
  await toggles().nth(1).click();
  await expect(page.locator('#load-2-stops')).toContainText('Pickup and delivery stops are unavailable.');
  await expect(details).toBeVisible(); // Same display numbers never share expansion state.
  await toggles().first().focus(); await page.keyboard.press('Space');
  await expect(details).toHaveCount(0);
  await page.getByText('Customer 1', { exact: true }).click();
  await expect(details).toBeVisible();
  await page.getByRole('button', { name: 'Miles', exact: true }).click();
  await expect.poll(() => queries.some(query => query.includes('sort=TotalMiles'))).toBe(true);
  await page.getByLabel('Customer filter').focus();
  await page.getByRole('button', { name: /next page/i }).click();
  await expect(toggles()).toHaveCount(1);
  await expect(page.locator('#load-1-stops')).toHaveCount(0);
  await page.getByRole('button', { name: /previous page/i }).click();
  await expect(toggles()).toHaveCount(3);
  await expect(toggles().first()).toHaveAttribute('aria-expanded', 'false');
  for (const width of [1440, 390]) {
    await page.setViewportSize({ width, height: 900 });
    await toggles().first().click(); await toggles().nth(2).click();
    await expect(details).toBeVisible();
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    const box = await toggles().nth(1).locator('xpath=ancestor::tr').boundingBox();
    await page.screenshot({ path: `${process.env.TEMP}/clustie-loads-${width}.png`, fullPage: true });
    expect(box.height).toBeLessThan(55);
    await toggles().first().click(); await toggles().nth(2).click();
  }
  mode = 'empty'; await page.reload();
  await expect(page.getByText('No loads match these filters.')).toBeVisible();
  expect(errors).toEqual([]);
  console.log('Loads passed: every stop/address, source order, missing data, independent record identity, keyboard/row toggles, sorting/pagination, desktop/mobile layout and read-only access.');
} finally {
  await browser?.close();
  await new Promise(resolve => server.close(resolve));
}
