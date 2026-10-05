// Browser regression checks against the static build, using isolated API fixtures.
import { chromium, expect } from '@playwright/test';
import { createServer } from 'node:http';
import { readFile } from 'node:fs/promises';
import { extname } from 'node:path';

const week = '2026-10-05';
const driver = { id: 'driver', fullName: 'Test Driver', active: true, driverType: 'Company', phone: '' };
const type = { id: 'fee', name: 'Admin fee', amount: '50.00', amounts: ['50.00'], archived: false, version: 1, eligibility: 'calendar', rules: [] };
const schedule = { id: 'schedule', driverId: driver.id, driverName: driver.fullName, typeId: type.id, kind: 'recurring', name: type.name, startWeek: week, endWeek: null, version: 1, phases: [{ weekStart: week, amount: '50.00', paused: false }], occurrences: [] };
const archivedType = { ...type, id: 'archived', name: 'Archived fee', archived: true };
const extraTypes = Array.from({ length: 6 }, (_, index) => ({ ...type, id: `fee-${index}`, name: `Other fee ${index}` }));
const data = { currentWeek: week, types: [type, ...extraTypes, archivedType], schedules: [schedule] };
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
let browser;
try {
  browser = await chromium.launch();
  const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
  const errors = [];
  page.on('pageerror', error => errors.push(error.message));
  await page.route('**/api/**', async route => {
    const url = new URL(route.request().url());
    const path = url.pathname.slice(4);
    const fixtures = {
      '/auth/session': { user: { id: 'user', username: 'Test user', permissions: ['charges.read', 'charges.write', 'fleet.read'] }, csrfToken: 'fixture' },
      '/driver-charges': data, '/drivers': [driver], '/truck-charges': { terms: [], phases: [], eligibleTruckIds: [] }, '/trucks': [], '/investors': [],
    };
    await route.fulfill({ json: path === '/trucks' && url.searchParams.has('page') ? { items: [], total: 0, page: 1, pageSize: 25, totalPages: 1 } : fixtures[path] ?? [] });
  });
  await page.goto(`${base}/accounting/driver-charges`);
  const checked = page.getByRole('checkbox', { name: 'Test Driver, Admin fee', exact: true });
  await expect(checked).toBeChecked();
  await page.reload();
  await expect(checked).toBeChecked();
  await page.getByRole('tab', { name: 'Truck charges', exact: true }).click();
  await page.getByRole('tab', { name: 'Driver charges', exact: true }).click();
  await expect(checked).toBeChecked();
  const memoryKey = name => `mserp-navigation-v1:user:/accounting/driver-charges:view:${name}`;
  const scrollMemoryKey = 'mserp-navigation-v1:user:/accounting/driver-charges:scroll';
  await page.locator('table').evaluate(table => { table.parentElement.scrollLeft = 200; });
  await expect.poll(() => page.evaluate(key => Object.values(JSON.parse(sessionStorage.getItem(key) ?? '{}')).some(position => position.left === 200), scrollMemoryKey)).toBe(true);
  await page.reload();
  await expect(checked).toBeChecked();
  await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))));
  await expect.poll(() => page.locator('table').evaluate(table => table.parentElement.scrollLeft)).toBe(0);
  await page.locator('table').evaluate(table => { table.parentElement.scrollLeft = 200; });
  await expect.poll(() => page.evaluate(key => Object.values(JSON.parse(sessionStorage.getItem(key) ?? '{}')).some(position => position.left === 200), scrollMemoryKey)).toBe(true);
  await page.goto(`${base}/trucks`);
  await expect(page.getByRole('heading', { name: 'Trucks', exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Accounting', exact: true }).click();
  await page.getByRole('link', { name: 'Charges', exact: true }).click();
  await expect(checked).toBeChecked();
  await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))));
  await expect.poll(() => page.locator('table').evaluate(table => table.parentElement.scrollLeft)).toBe(0);
  // Both matrices overflow. A truck scroll must not hide the driver's checked
  // first fee behind the sticky Driver column after returning and reloading.
  await page.getByRole('tab', { name: 'Truck charges', exact: true }).click();
  await expect(page.getByRole('columnheader', { name: 'Current driver', exact: true })).toBeVisible();
  await page.locator('table').evaluate(table => { table.parentElement.scrollLeft = 200; });
  await expect.poll(() => page.evaluate(key => Object.values(JSON.parse(sessionStorage.getItem(key) ?? '{}')).some(position => position.left === 200), scrollMemoryKey)).toBe(true);
  await page.reload();
  await expect(page.getByRole('columnheader', { name: 'Current driver', exact: true })).toBeVisible();
  await expect.poll(() => page.locator('table').evaluate(table => table.parentElement.scrollLeft)).toBe(200);
  await page.getByRole('tab', { name: 'Driver charges', exact: true }).click();
  await expect(checked).toBeChecked();
  await page.reload();
  await expect(checked).toBeChecked();
  await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))));
  await expect.poll(() => page.locator('table').evaluate(table => table.parentElement.scrollLeft)).toBe(0);
  const driverColumn = await page.getByRole('columnheader', { name: 'Driver', exact: true }).boundingBox();
  const firstFee = await checked.boundingBox();
  expect(firstFee.x).toBeGreaterThanOrEqual(driverColumn.x + driverColumn.width);
  for (const savedFilter of ['missing-type', archivedType.id]) {
    await page.evaluate(({ key, value }) => sessionStorage.setItem(key, JSON.stringify(value)), { key: memoryKey('page:typeFilter'), value: savedFilter });
    await page.reload();
    await expect(checked).toBeChecked();
    await expect(page.getByLabel('Filter charge type')).toHaveValue('');
  }
  await page.getByLabel('Show archived charge types').check();
  await page.getByLabel('Filter charge type').selectOption(archivedType.id);
  await page.reload();
  await expect(page.getByLabel('Filter charge type')).toHaveValue(archivedType.id);
  await expect(page.getByRole('columnheader', { name: 'Archived fee (archived)', exact: true })).toBeVisible();
  await page.getByLabel('Show archived charge types').uncheck();
  await expect(checked).toBeChecked();
  await page.getByRole('button', { name: 'Previous week', exact: true }).click();
  await expect(checked).not.toBeChecked();
  await expect(page.locator('[data-week-start]')).toHaveAttribute('data-week-start', '2026-09-28');
  await page.reload();
  await expect(page.locator('[data-week-start]')).toHaveAttribute('data-week-start', '2026-09-28');
  await page.getByRole('button', { name: 'Next week', exact: true }).click();
  await expect(checked).toBeChecked();
  await page.getByRole('button', { name: 'Next week', exact: true }).click();
  await page.getByRole('button', { name: 'This week', exact: true }).click();
  await expect(page.locator('[data-week-start]')).toHaveAttribute('data-week-start', week);
  await page.getByRole('tab', { name: 'Truck charges', exact: true }).click();
  await expect(page.locator('[data-week-start]')).toHaveAttribute('data-week-start', week);
  await page.getByRole('button', { name: 'Previous week', exact: true }).click();
  await expect(page.locator('[data-week-start]')).toHaveAttribute('data-week-start', '2026-09-28');
  await page.getByRole('button', { name: 'This week', exact: true }).click();
  await page.getByRole('tab', { name: 'Driver charges', exact: true }).click();
  await expect(checked).toBeChecked();
  // Invalid values left by the old free-date input fall back to the API week.
  await page.evaluate(key => sessionStorage.setItem(key, JSON.stringify('')), memoryKey('RecurringMatrix:week'));
  await page.reload();
  await expect(checked).toBeChecked();
  expect(errors).toEqual([]);
  console.log('Charge views checks passed: initial selections, independent matrix scrolls, stale/archived filters, week navigation, remembered weeks and tab return.');
} finally {
  await browser?.close();
  await new Promise(resolve => server.close(resolve));
}
