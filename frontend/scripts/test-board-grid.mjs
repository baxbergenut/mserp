// Fixture-only browser coverage: no live API, database, or vendor writes.
import { chromium, expect } from '@playwright/test';
import { createServer } from 'node:http';
import { readFile } from 'node:fs/promises';
import { extname, join } from 'node:path';
import { tmpdir } from 'node:os';

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
const browser = await chromium.launch();
const base = `http://127.0.0.1:${server.address().port}`;
const week = '2026-10-12';
const drivers = ['Alice Grid', 'Bob Grid'].map((fullName, i) => ({ id: `driver-${i}`, fullName, active: true, driverType: i ? 'M-O' : 'O', truckId: `truck-${i}`, truckUnit: `T${i}`, dispatcherId: 'dispatcher', dispatcherName: 'Dispatch', phone: '', location: i ? null : { latitude: 41.881, longitude: -87.623, reportedAt: new Date().toISOString(), heading: null }, mainUpdaterName: 'Main updater', afterHoursUpdaterName: 'Night updater' }));
const load = (driverId, date, slot = 0) => ({ driverId, date, slot, deleted: false, loadNumber: `LOAD-${slot}`, loadRecordId: null, dayStatus: '', originalRate: '400', driverRate: '350', miles: '90', enteredOriginalRate: '400', enteredMiles: '90', version: 1, duplicate: false });
let statusEntries = drivers.map(d => ({ driverId: d.id, currentLoad: '', trailerNumber: 'OLD', status: '', destination: 'Chicago', eta: '', notes: 'Existing note', homeTime: '', version: 1 }));
let grossEntries = drivers.flatMap(d => [load(d.id, week), load(d.id, '2026-10-13'), load(d.id, '2026-10-14'), load(d.id, '2026-10-14', 1)]);
const emptyLoads = { current: null, week: [], next: [], earlier: [], hidden: [], unavailable: [], destinationSource: false, sourceDestination: '', stopKey: '', fromDate: week, revision: 'fixture', customOrder: false };
let readOnly = false;
let saves = 0;
const errors = [];
try {
  const page = await browser.newPage({ viewport: { width: 1800, height: 1000 } });
  page.on('pageerror', error => errors.push(error.message));
  await page.clock.setFixedTime(new Date('2026-10-13T12:00:00Z'));
  await page.context().grantPermissions(['clipboard-read', 'clipboard-write']);
  await page.route('**/api/**', async route => {
    const request = route.request(), url = new URL(request.url()), path = url.pathname.slice(4);
    if (request.method() === 'PUT') {
      const input = request.postDataJSON();
      const committed = input.entries.map(e => ({ ...e, version: e.version + 1 }));
      if (path === '/driver-board') statusEntries = statusEntries.map(e => committed.find(c => c.driverId === e.driverId) ?? e);
      else grossEntries = [...grossEntries.filter(e => !committed.some(c => c.driverId === e.driverId && c.date === e.date && c.slot === e.slot)), ...committed];
      saves += 1;
      return route.fulfill({ json: committed });
    }
    const fixtures = {
      '/auth/session': { user: { id: 'grid-user', username: 'Grid Test', theme: 'original', permissions: ['driver_board.read', 'board.read', 'board.write', 'loads.read', 'fleet.read', ...(readOnly ? [] : ['driver_board.write'])] }, csrfToken: 'fixture' },
      '/driver-board': { weekStart: week, drivers, entries: statusEntries, grossEntries, loads: Object.fromEntries(drivers.map(d => [d.id, emptyLoads])), eld: { configured: false, unmatched: 0, ambiguous: 0, invalid: 0 } },
      '/gross-board': { weekStart: url.searchParams.get('weekStart') ?? week, drivers, entries: grossEntries, balances: [] },
      '/gross-board/loads': [],
      '/loads': { items: [{ ID: 1, LoadID: 'LIST-1', ShipmentID: 'Shipment', Status: 'DELIVERED', CustomerName: 'List customer', DriverName: 'Alice Grid', TruckUnit: 'T0', PickupTime: week, DeliveryTime: week, TotalMiles: '90', PerMileRevenue: '4', TotalPay: '400' }], total: 1, page: 1, pageSize: 25, totalPages: 1, options: { statuses: [], customers: [], dispatchers: [], drivers: [] } },
      '/tasks/count': { count: 0 },
      '/sidebar-counts': {},
    };
    return route.fulfill({ json: path.startsWith('/driver-board/loads/') ? emptyLoads : fixtures[path] ?? [] });
  });
  await page.goto(`${base}/driver-board`);
  const field = (name, driver = 'Alice Grid') => page.getByLabel(`${driver} · ${name}`, { exact: true });
  const trailer = field('Trailer'), trailerCell = trailer.locator('..');
  await expect(trailer).toHaveValue('OLD');
  await expect(page.getByRole('columnheader', { name: 'Driver type', exact: true })).toHaveCount(0);
  await expect(page.getByRole('columnheader', { name: 'Latest location', exact: true })).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Alice Grid · History', exact: true })).toHaveCount(0);
 await expect(page.locator('tr[data-driver-id="driver-0"] th')).toContainText('(O)');
  await expect(field('Copy current location', 'Bob Grid')).toBeDisabled();
  await field('Copy current location').click();
  expect(await page.evaluate(() => navigator.clipboard.readText())).toBe('41.88100, -87.62300');
  await expect(field('Copy current location').getByRole('status')).toHaveText('Copied');
  expect((await trailer.boundingBox()).width).toBeGreaterThanOrEqual(98);

  await trailer.click(); await expect(trailerCell).toBeFocused();
  await page.keyboard.type('NEW'); await expect(trailer).toHaveValue('NEW');
  await page.keyboard.press('ArrowDown'); await expect(field('Trailer', 'Bob Grid').locator('..')).toBeFocused();
  await page.keyboard.press('ArrowUp'); await expect(trailerCell).toBeFocused();
  await page.keyboard.press('F2'); await expect(trailer).toBeFocused();
  await page.keyboard.press('ArrowLeft'); await page.keyboard.type('X'); await expect(trailer).toHaveValue('NEXW');
  await page.keyboard.press('Escape'); await expect(trailerCell).toBeFocused();
  await page.keyboard.press('Control+c'); expect(await page.evaluate(() => navigator.clipboard.readText())).toBe('NEXW');
  await trailer.dblclick(); await expect(trailer).toBeFocused(); await page.keyboard.press('Enter');
  await page.keyboard.press('ArrowRight'); await expect(page.locator('tr[data-driver-id="driver-0"] td').nth(3)).toBeFocused();
  await page.keyboard.type('999'); await expect(trailer).toHaveValue('NEXW');
  await field('Notes').click(); await page.keyboard.press('Delete'); await expect(field('Notes')).toHaveValue('');
  await page.keyboard.type('New note'); await page.keyboard.press('Enter');
  await field('Current load').click(); await page.keyboard.type('LOAD');
  await expect(page.getByRole('listbox')).toBeVisible();
  await page.keyboard.press('ArrowDown'); await expect(field('Current load')).toBeFocused();
  await page.keyboard.press('Enter'); await expect(field('Current load')).toHaveValue('LOAD-1');
  await page.keyboard.press('Escape'); await page.keyboard.press('ArrowRight');
  await expect(page.locator('tr[data-driver-id="driver-0"] th')).toBeFocused();
  await expect.poll(() => statusEntries[0].trailerNumber, { timeout: 10000 }).toBe('NEXW');
  expect(statusEntries[0].notes).toBe('New note');
  await page.screenshot({ path: join(tmpdir(), 'mserp-status-board-grid.png'), fullPage: true });

  readOnly = true; await page.reload();
  const beforeSaves = saves;
  await expect(trailer).toBeDisabled(); await trailerCell.click(); await page.keyboard.type('NO');
  await expect(trailer).toHaveValue('NEXW'); expect(saves).toBe(beforeSaves);
  await page.setViewportSize({ width: 390, height: 844 });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await page.setViewportSize({ width: 1800, height: 1000 });

  await page.goto(`${base}/gross-board?weekStart=${week}&driverId=driver-0&date=${week}&slot=0`);
  const amount = (date, field = 'original rate') => page.getByLabel(`Alice Grid, ${date}, ${field}`, { exact: true });
  const original = amount(week);
  await expect(original).toHaveValue('$400.00');
  await original.click(); await expect(original.locator('..')).toBeFocused();
  await page.keyboard.type('123'); await expect(original).toHaveValue('123');
  await page.keyboard.press('ArrowRight'); await expect(amount('2026-10-13').locator('..')).toBeFocused();
  await page.keyboard.press('ArrowDown'); await expect(amount('2026-10-13', 'driver rate').locator('..')).toBeFocused();
  await page.keyboard.type('456'); await page.keyboard.press('Enter');
  await page.keyboard.press('ArrowDown'); await expect(amount('2026-10-13', 'miles').locator('..')).toBeFocused();
  await page.keyboard.type('12'); await page.keyboard.press('Enter'); await page.keyboard.press('Control+c');
  expect(await page.evaluate(() => navigator.clipboard.readText())).toBe('12.00');
  await page.keyboard.press('ArrowRight');
  await page.keyboard.press('Enter');
  const dialog = page.getByRole('dialog', { name: 'Daily loads', exact: true });
  await expect(dialog).toBeVisible();
  const modalAmounts = dialog.getByLabel('Alice Grid, 2026-10-14, original rate', { exact: true });
  await modalAmounts.first().click(); await page.keyboard.type('222');
  await expect(modalAmounts.first()).toHaveValue('222');
  await page.keyboard.press('ArrowRight'); await expect(modalAmounts.nth(1).locator('..')).toBeFocused();
  await page.keyboard.type('333'); await page.keyboard.press('Enter');
  await dialog.getByRole('button', { name: 'Apply changes', exact: true }).click();
  await expect(dialog).toHaveCount(0);
  await expect.poll(() => grossEntries.find(e => e.driverId === 'driver-0' && e.date === '2026-10-14' && e.slot === 1)?.originalRate, { timeout: 10000 }).toBe('333');
  expect(grossEntries.find(e => e.driverId === 'driver-0' && e.date === week).originalRate).toBe('123');
  await page.screenshot({ path: join(tmpdir(), 'mserp-gross-board-grid.png'), fullPage: true });
  await page.setViewportSize({ width: 390, height: 844 });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  // Ordinary list tables must retain native selection and arrow behavior.
  await page.goto(base + '/loads');
  const listCell = page.getByRole('cell', { name: 'List customer', exact: true });
  await listCell.click(); await expect(listCell).not.toBeFocused();
  await page.keyboard.press('ArrowRight'); await expect(listCell).not.toBeFocused();
  await expect(page.locator('table [tabindex]')).toHaveCount(0);
  await listCell.dblclick();
  expect(await page.evaluate(() => getSelection()?.toString())).not.toBe('');
  expect(errors).toEqual([]);
  console.log('Board grid checks passed: replacement, caret editing, navigation, copying, suggestions, read-only cells, autosave, daily-load dialog, layout, and location icon.');
} finally { await browser.close(); await new Promise(resolve => server.close(resolve)); }
