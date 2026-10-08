// Browser regression checks against the static build, using isolated API fixtures.
import { chromium, expect } from '@playwright/test';
import { createServer } from 'node:http';
import { readFile } from 'node:fs/promises';
import { extname, join } from 'node:path';
import { tmpdir } from 'node:os';

const week = '2026-10-05';
const driver = { id: 'driver', fullName: 'Test Driver', active: true, driverType: 'Company', phone: '' };
const type = { id: 'fee', name: 'Admin fee', amount: '50.00', amounts: ['50.00'], archived: false, version: 1, eligibility: 'calendar', rules: [] };
const schedule = { id: 'schedule', driverId: driver.id, driverName: driver.fullName, typeId: type.id, kind: 'recurring', name: type.name, startWeek: week, endWeek: null, version: 1, phases: [{ weekStart: week, amount: '50.00', paused: false }], occurrences: [] };
const secondDriver = { ...driver, id: 'second', fullName: 'Second Driver' };
const thirdDriver = { ...driver, id: 'third', fullName: 'Third Driver' };
const fourthDriver = { ...driver, id: 'fourth', fullName: 'Fourth Driver' };
const inactiveDriver = { ...driver, id: 'inactive', fullName: 'Inactive Driver', active: false };
const pausedSchedule = { ...schedule, id: 'paused', driverId: secondDriver.id, driverName: secondDriver.fullName, version: 3, phases: [{ weekStart: week, amount: '25.00', paused: true }] };
const writes = [];
let trucks = [];
const truckCharges = { terms: [], phases: [], eligibleTruckIds: [] };
const truckWrites = [];
let failThirdDriver = true;
let handoffFourthDriver = false;
const archivedType = { ...type, id: 'archived', name: 'Archived fee', archived: true };
const extraTypes = Array.from({ length: 6 }, (_, index) => ({ ...type, id: `fee-${index}`, name: `Other fee ${index}` }));
const data = { currentWeek: week, types: [type, ...extraTypes, archivedType], schedules: [schedule, pausedSchedule] };
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
    if (route.request().method() === 'PUT' && path === '/truck-charges/recurring') {
      const input = route.request().postDataJSON();
      truckWrites.push(input);
      const existing = truckCharges.phases.find(p => p.truckId === input.truckId && p.typeId === input.typeId && p.weekStart === input.weekStart);
      expect(input.version).toBe(existing?.version ?? 0);
      truckCharges.phases = [...truckCharges.phases.filter(p => p !== existing), { ...input, version: input.version + 1 }];
      await route.fulfill({ status: 204 }); return;
    }
    if (route.request().method() === 'PUT' && path === '/driver-charges/recurring') {
      const input = route.request().postDataJSON();
      writes.push(input);
      await new Promise(resolve => setTimeout(resolve, 150));
      if (input.driverId === thirdDriver.id && failThirdDriver) {
        await route.fulfill({ status: 409, json: { error: 'Charge changed; reload and try again.' } }); return;
      }
      const existing = data.schedules.find(s => s.id === input.scheduleId);
      const updated = { ...(existing ?? schedule), id: existing?.id ?? `new-${input.driverId}`, driverId: input.driverId, version: (existing?.version ?? 0) + 1, phases: [{ weekStart: input.weekStart, amount: input.amount, paused: !input.included || (handoffFourthDriver && input.driverId === fourthDriver.id) }] };
      data.schedules = [...data.schedules.filter(s => s.id !== updated.id), updated];
      await route.fulfill({ status: 204 }); return;
    }
    if (route.request().method() === 'DELETE' && path.startsWith('/driver-charges/types/')) {
      const id = path.split('/').at(-1);
      expect(route.request().postDataJSON()).toEqual({ version: 1 });
      data.types = data.types.filter(t => t.id !== id);
      await route.fulfill({ status: 204 }); return;
    }
    const fixtures = {
      '/auth/session': { user: { id: 'user', username: 'Test user', permissions: ['charges.read', 'charges.write', 'fleet.read'] }, csrfToken: 'fixture' },
      '/driver-charges': url.searchParams.has('driverId') ? { ...data, schedules: data.schedules.filter(s => s.driverId === url.searchParams.get('driverId')) } : data,
      '/drivers': [driver, secondDriver, thirdDriver, fourthDriver, inactiveDriver], '/truck-charges': truckCharges, '/trucks': trucks, '/investors': [{ id: 'owner', fullName: 'Truck Owner', active: true }],
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
  await expect.poll(() => page.evaluate(key => JSON.parse(sessionStorage.getItem(key) ?? '{}')['DIV:driver-recurring-charges']?.left, scrollMemoryKey)).toBe(200);
  await page.reload();
  await expect(checked).toBeChecked();
  await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))));
  await expect.poll(() => page.locator('table').evaluate(table => table.parentElement.scrollLeft)).toBe(0);
  await page.locator('table').evaluate(table => { table.parentElement.scrollLeft = 200; });
  await expect.poll(() => page.evaluate(key => JSON.parse(sessionStorage.getItem(key) ?? '{}')['DIV:driver-recurring-charges']?.left, scrollMemoryKey)).toBe(200);
  await page.goto(`${base}/trucks`);
  await expect(page.getByRole('heading', { name: 'Trucks', exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Accounting', exact: true }).click();
  await page.getByRole('link', { name: 'Recurring Charges', exact: true }).click();
  await expect(checked).toBeChecked();
  await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))));
  await expect.poll(() => page.locator('table').evaluate(table => table.parentElement.scrollLeft)).toBe(0);
  // Both matrices overflow. A truck scroll must not hide the driver's checked
  // first fee behind the sticky Driver column after returning and reloading.
  await page.getByRole('tab', { name: 'Truck charges', exact: true }).click();
  await expect(page.getByRole('columnheader', { name: 'Current driver', exact: true })).toBeVisible();
  await page.locator('table').evaluate(table => { table.parentElement.scrollLeft = 200; });
  // Wait for this matrix's scroll event; a saved driver position must not
  // satisfy the wait and let reload discard the pending truck position.
  await expect.poll(() => page.evaluate(key => JSON.parse(sessionStorage.getItem(key) ?? '{}')['DIV:truck-recurring-charges']?.left, scrollMemoryKey)).toBe(200);
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
  // The column checkbox respects filters, preserves paused amounts/versions,
  // skips matching rows and inactive drivers, and reports partial failures.
  await page.getByLabel('Filter driver').selectOption(secondDriver.id);
  const filteredSelectAll = page.getByRole('checkbox', { name: 'Admin fee: select visible drivers', exact: true });
  await filteredSelectAll.click();
  await expect(filteredSelectAll).toBeChecked();
  await expect(page.getByRole('checkbox', { name: 'Second Driver, Admin fee', exact: true })).toBeChecked();
  await expect(page.getByRole('button', { name: 'Reload', exact: true })).toBeEnabled();
  expect(writes).toEqual([{ driverId: secondDriver.id, typeId: type.id, weekStart: week, included: true, amount: '25.00', scheduleId: pausedSchedule.id, version: 3, typeVersion: 1 }]);
  await page.getByLabel('Filter driver').selectOption('');
  const selectAll = page.getByRole('checkbox', { name: 'Admin fee: select all drivers', exact: true });
  // A mixed column clears the included rows, rather than repeatedly trying
  // to enable excluded investor-truck drivers.
  await selectAll.click();
  await expect(page.getByRole('button', { name: 'Reload', exact: true })).toBeEnabled();
  await expect(checked).not.toBeChecked();
  await expect(page.getByRole('checkbox', { name: 'Second Driver, Admin fee', exact: true })).not.toBeChecked();
  expect(writes.slice(1).map(write => [write.driverId, write.included])).toEqual([[driver.id, false], [secondDriver.id, false]]);
  await selectAll.click();
  const failureNotice = page.getByRole('alert').filter({ hasText: 'Admin fee:' });
  await expect(failureNotice).toContainText('Third Driver');
  await expect(failureNotice).toContainText('3 of 4 drivers updated');
  await expect(page.getByRole('button', { name: 'Reload', exact: true })).toBeEnabled();
  await expect(page.getByRole('checkbox', { name: 'Third Driver, Admin fee', exact: true })).not.toBeChecked();
  await expect(page.getByRole('checkbox', { name: 'Fourth Driver, Admin fee', exact: true })).toBeChecked();
  failThirdDriver = false;
  // A partial failure also leaves a mixed column: clear it, then select all.
  await selectAll.click();
  await expect(page.getByRole('button', { name: 'Reload', exact: true })).toBeEnabled();
  expect(writes.slice(7)).toHaveLength(3);
  expect(writes.slice(7).every(write => write.included === false)).toBe(true);
  await selectAll.click();
  await expect(page.getByRole('button', { name: 'Reload', exact: true })).toBeEnabled();
  await expect(page.getByRole('checkbox', { name: 'Third Driver, Admin fee', exact: true })).toBeChecked();
  await expect(selectAll).toBeChecked();
  expect(writes[12]).toEqual({ driverId: thirdDriver.id, typeId: type.id, weekStart: week, included: true, amount: '50.00', scheduleId: '', version: 0, typeVersion: 1 });
  expect(writes.filter(write => write.driverId === secondDriver.id).every(write => write.amount === '25.00')).toBe(true);
  await selectAll.click();
  await expect(page.getByRole('button', { name: 'Reload', exact: true })).toBeEnabled();
  await expect(selectAll).not.toBeChecked();
  await expect(page.getByRole('checkbox', { name: 'Test Driver, Admin fee', exact: true })).not.toBeChecked();
  await expect(page.getByRole('checkbox', { name: 'Second Driver, Admin fee', exact: true })).not.toBeChecked();
  await expect(page.getByRole('checkbox', { name: 'Third Driver, Admin fee', exact: true })).not.toBeChecked();
  await expect(page.getByRole('checkbox', { name: 'Fourth Driver, Admin fee', exact: true })).not.toBeChecked();
  expect(writes.slice(14)).toHaveLength(4);
  expect(writes.slice(14).every(write => write.included === false)).toBe(true);
  handoffFourthDriver = true;
  await selectAll.click();
  await expect(page.getByRole('button', { name: 'Reload', exact: true })).toBeEnabled();
  await expect(checked).toBeChecked();
  await expect(page.getByRole('checkbox', { name: 'Fourth Driver, Admin fee', exact: true })).not.toBeChecked();
  await selectAll.click();
  await expect(page.getByRole('button', { name: 'Reload', exact: true })).toBeEnabled();
  for (const current of [driver, secondDriver, thirdDriver, fourthDriver]) {
    await expect(page.getByRole('checkbox', { name: `${current.fullName}, Admin fee`, exact: true })).not.toBeChecked();
  }
  expect(writes.slice(22).map(write => [write.driverId, write.included])).toEqual([[driver.id, false], [secondDriver.id, false], [thirdDriver.id, false]]);
  await page.getByLabel('Show archived charge types').check();
  await expect(page.getByRole('checkbox', { name: 'Archived fee: select all drivers', exact: true })).toHaveCount(0);
  await page.getByLabel('Show archived charge types').uncheck();
  await page.getByRole('tab', { name: 'Charge types', exact: true }).click();
  await page.getByRole('button', { name: 'Delete Other fee 0', exact: true }).click();
  await page.getByRole('button', { name: 'Cancel', exact: true }).click();
  await expect(page.getByRole('cell', { name: 'Other fee 0', exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Delete Other fee 0', exact: true }).click();
  await page.getByRole('button', { name: 'Delete type', exact: true }).click();
  await expect(page.getByRole('dialog')).toHaveCount(0);
  await expect(page.getByRole('cell', { name: 'Other fee 0', exact: true })).toHaveCount(0);
  trucks = [
    { id: 'truck-a', unitNumber: '201', ownerId: 'owner', ownerName: 'Truck Owner', driverId: driver.id, driverName: driver.fullName, active: true },
    { id: 'truck-b', unitNumber: '202', ownerId: 'owner', ownerName: 'Truck Owner', active: true },
    { id: 'inactive-truck', unitNumber: 'INACTIVE', ownerId: 'owner', ownerName: 'Truck Owner', active: false },
    { id: 'company-truck', unitNumber: 'COMPANY', ownerId: 'company', ownerName: 'Company', active: true, isCompanyOwned: true },
  ];
  type.amounts = ['50.00', '75.00'];
  truckCharges.eligibleTruckIds = trucks.map(t => t.id);
  truckCharges.terms = trucks.map(t => ({ truckId: t.id, ownerId: t.ownerId, weekStart: week, sharePercent: '88', version: 1 }));
  await page.goto(`${base}/accounting/driver-charges?tab=trucks`);
  await expect(page.getByRole('link', { name: 'INACTIVE', exact: true })).toHaveCount(0);
  await expect(page.getByRole('link', { name: 'COMPANY', exact: true })).toHaveCount(0);
  await expect(page.getByRole('link', { name: 'Truck Owner', exact: true }).first()).toHaveAttribute('href', '/investors/detail?id=owner');
  await expect(page.getByRole('link', { name: '201', exact: true })).toHaveAttribute('href', '/trucks/detail?id=truck-a');
  await expect(page.getByRole('link', { name: driver.fullName, exact: true })).toHaveAttribute('href', '/drivers/detail?id=driver');
  const truckCheck = page.getByRole('checkbox', { name: '201, Admin fee', exact: true });
  await truckCheck.click();
  await expect(truckCheck).toBeChecked();
  await expect(page.getByRole('button', { name: 'Reload', exact: true })).toBeEnabled();
  await page.getByLabel('201, Admin fee amount', { exact: true }).selectOption('75.00');
  await expect(page.getByRole('button', { name: 'Reload', exact: true })).toBeEnabled();
  expect(truckWrites.at(-1).amount).toBe('75.00');
  const trucksAll = page.getByRole('checkbox', { name: 'Admin fee: select all trucks', exact: true });
  await trucksAll.click();
  await expect(truckCheck).not.toBeChecked();
  await expect(page.getByRole('button', { name: 'Reload', exact: true })).toBeEnabled();
  await trucksAll.click();
  await expect(page.getByRole('checkbox', { name: '202, Admin fee', exact: true })).toBeChecked();
  await expect(page.getByRole('button', { name: 'Reload', exact: true })).toBeEnabled();
  await page.reload();
  await expect(truckCheck).toBeChecked();
  await expect(page.getByLabel('201, Admin fee amount', { exact: true })).toHaveValue('75.00');
  const backBox = await page.getByRole('link', { name: 'Back to previous page' }).boundingBox();
  const newBox = await page.getByRole('button', { name: 'New charge type', exact: true }).boundingBox();
  expect(Math.abs(backBox.y - newBox.y)).toBeLessThan(5);
  await page.locator('table').evaluate(table => { table.parentElement.scrollLeft = 0; });
  await page.screenshot({ path: join(tmpdir(), 'mserp-truck-charges-review.png'), fullPage: true, animations: 'disabled' });
  await page.getByRole('tab', { name: 'Driver charges', exact: true }).click();
  const weekBox = await page.getByRole('group', { name: 'Effective week', exact: true }).boundingBox();
  const filterBox = await page.getByPlaceholder('Search drivers…').boundingBox();
  expect(Math.abs(weekBox.y + weekBox.height / 2 - filterBox.y - filterBox.height / 2)).toBeLessThan(3);
  await page.screenshot({ path: join(tmpdir(), 'mserp-driver-charges-review.png'), fullPage: true, animations: 'disabled' });
  expect(errors).toEqual([]);
  console.log('Charge views checks passed: select-all add/remove, filtered assignment, existing amounts, partial failures/retry, type deletion/cancellation, initial selections, independent matrix scrolls, filters, week navigation and tab return.');
} finally {
  await browser?.close();
  await new Promise(resolve => server.close(resolve));
}
