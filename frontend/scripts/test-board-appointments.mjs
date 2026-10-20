import { chromium, expect } from '@playwright/test';
import { createServer } from 'node:http';
import { readFile } from 'node:fs/promises';
import { extname } from 'node:path';

const server = createServer(async (req, res) => {
  try {
    const path = new URL(req.url, 'http://localhost').pathname;
    const file = path === '/' ? 'index.html' : extname(path) ? path.slice(1) : `${path.slice(1)}.html`;
    res.setHeader('Content-Type', { '.html': 'text/html', '.js': 'application/javascript', '.css': 'text/css', '.txt': 'text/plain' }[extname(file)] ?? 'application/octet-stream');
    res.end(await readFile(new URL(`../out/${file}`, import.meta.url)));
  } catch { res.writeHead(404); res.end(); }
});
await new Promise(done => server.listen(0, '127.0.0.1', done));
const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 1920, height: 900 } });
const errors = []; page.on('pageerror', e => errors.push(e.message));
let entry = { driverId: 'driver', currentLoad: '4010927482', status: 'ENROUTE', destination: '', trailerNumber: '', trailerNote: '', eta: '', notes: 'Human notes stay', homeTime: '', version: 1 };
const schedule = { kind: 'window', label: 'DEL 24/7 FRI', detail: '2026-10-16 08:00 – 23:59 (facility local time)' };
const stop = { key: 'stop', type: 'delivery', location: 'Chicago, IL', latitude: 41.8781, longitude: -87.6298, appointment: '', schedule };
const load = { planId: 'current', loadId: 65341, number: entry.currentLoad, stops: [stop], warning: '', date: '', slot: 0 };
const loads = { current: load, week: [], next: [], earlier: [], hidden: [], unavailable: [], destinationSource: true, sourceDestination: stop.location, stopKey: stop.key, revision: 'fixture', fromDate: '2026-10-17', automation: { context: 'context', appointment: schedule, manualStopTime: '', stopType: 'delivery', arrived: false, arrivalOverride: 'auto', observedAt: new Date().toISOString(), stale: false, travelMode: 'auto', travelValue: 'ETA 13:00', requiresConfirmation: false, routingEnabled: true, manualETAValid: true } };
let readOnly = false;
loads.automation.travelArrivalAt = '2026-10-20T17:07:00Z';
loads.automation.travelStale = false;
loads.automation.reason = 'Last known miles remaining · GPS data stale · last observed · Assumed semi profile';
await page.context().grantPermissions(['clipboard-read', 'clipboard-write']);
await page.route('**/api/**', async route => {
  const req = route.request(), path = new URL(req.url()).pathname.slice(4);
  if (req.method() === 'PUT' && path === '/driver-board') { entry = { ...req.postDataJSON().entries[0], version: entry.version + 1 }; return route.fulfill({ json: [entry] }); }
  if (req.method() === 'POST' && path === '/driver-board/loads/driver') {
    const { action, manualStopTime } = req.postDataJSON();
    if (action === 'stop_time') {
      loads.automation.manualStopTime = manualStopTime;
      loads.automation.appointment = manualStopTime ? { kind: 'manual', label: `DEL ${manualStopTime}`, detail: 'Manual stop time entered on Status Board' } : schedule;
    }
    entry.version++;
    return route.fulfill({ json: { entry, loads } });
  }
  const fixtures = {
    '/auth/session': { user: { id: 'user', username: 'Updater', permissions: ['driver_board.read', ...(!readOnly ? ['driver_board.write'] : [])] }, csrfToken: 'fixture' },
    '/driver-board': { weekStart: '2026-10-12', drivers: [{ id: 'driver', fullName: 'Appointment Driver', driverType: 'M', truckId: 'truck', truckUnit: '115', dispatcherId: '', dispatcherName: 'Dispatch', phone: '', location: null }], entries: [entry], grossEntries: [], loads: { driver: loads }, eld: { configured: false } },
    '/driver-board/loads/driver': loads, '/sidebar-counts': {},
  };
  await route.fulfill({ json: fixtures[path] ?? [] });
});
try {
  const base = `http://127.0.0.1:${server.address().port}`;
  const pickup = { kind: 'appointment', label: 'PU 14:00 WED', detail: '2026-10-21 14:00:00 (facility local time)' };
  const nextLoad = { ...load, planId: 'next-1', number: 'NEXT-ONE', stops: [{ ...stop, key: 'next-pu', type: 'pickup', schedule: pickup }] };
  loads.next = [nextLoad, { ...nextLoad, planId: 'next-2', number: 'NEXT-TWO' }, { ...nextLoad, planId: 'next-3', number: 'NEXT-THREE' }];
  await page.goto(base + '/driver-board');
  const nextCell = page.getByRole('button', { name: 'Appointment Driver · Next load', exact: true });
  await expect(page.getByRole('columnheader', { name: 'Next load', exact: true })).toHaveCount(1);
  await expect(nextCell).toHaveText('NEXT-ONEPU 14:00 WED');
  await expect(nextCell).toHaveAttribute('title', `NEXT-ONE · ${pickup.detail}`);
  const [numberBox, pickupBox] = await Promise.all([nextCell.locator('span').first().boundingBox(), nextCell.locator('span').last().boundingBox()]);
  expect(pickupBox.x).toBeGreaterThan(numberBox.x + numberBox.width);
  nextLoad.stops[0].schedule = { kind: 'window', label: 'PU 24/7 THU', detail: '2026-10-22 00:00 – 23:59 (facility local time)' };
  await page.reload();
  await expect(nextCell).toHaveText('NEXT-ONEPU 24/7 THU');
  nextLoad.stops = [{ ...stop, type: 'delivery' }];
  await page.reload();
  await expect(nextCell).toHaveText('NEXT-ONE');
  loads.next = [];
  await page.reload();
  await expect(nextCell).toHaveText('—');
  const cell = page.getByRole('button', { name: 'Appointment Driver · ETA', exact: true });
  await expect(cell).toHaveText('DEL 24/7 FRI ETA 13:00');
  await expect(cell).toHaveAttribute('title', 'ETA Oct 20, 12:00 CDT');
  await expect(cell.locator('span').last()).toHaveClass('text-red-500');
  const copied = await cell.evaluate(button => { let value = ''; const e = new Event('copy', { bubbles: true, cancelable: true }); Object.defineProperty(e, 'clipboardData', { value: { setData: (_, text) => { value = text; } } }); button.dispatchEvent(e); return value; });
  expect(copied).toBe('DEL 24/7 FRI ETA 13:00');
  loads.automation.travelValue = 'ETA 13:38'; loads.automation.travelArrivalAt = '2026-10-20T17:38:00Z';
  await page.reload();
  await expect(cell).toHaveText('DEL 24/7 FRI ETA 13:45');
  await expect(cell).toHaveAttribute('title', 'ETA Oct 20, 12:45 CDT');
  loads.automation.travelValue = '138mi'; loads.automation.travelStale = true;
  await page.reload();
  await expect(cell).toHaveText('DEL 24/7 FRI 138mi');
  await expect(cell.locator('span').last()).toHaveClass('text-amber-400');
  await expect(cell).toHaveAttribute('title', '');
  loads.automation.travelStale = false;
  await page.reload();
  await expect(cell.locator('span').last()).toHaveClass('text-red-500');
  loads.automation.travelValue = 'ETA 13:00'; loads.automation.travelArrivalAt = '2026-10-20T17:00:00Z';
  await page.reload();
  await cell.click();
  await expect(page.getByLabel('At stop', { exact: true })).toHaveCount(0);
  await expect(page.getByLabel('Travel estimate', { exact: true })).toHaveCount(0);
  await expect(page.getByLabel('Manual delivery time', { exact: true })).toBeEmpty();
  await page.getByLabel('Manual delivery time', { exact: true }).fill('14:00 SUN');
  await page.getByRole('button', { name: 'Save time', exact: true }).click();
  await expect(cell).toHaveText('DEL 14:00 SUN ETA 13:00');
  await expect(page.getByLabel('Manual delivery time', { exact: true })).toHaveValue('14:00 SUN');
  await page.getByLabel('Manual delivery time', { exact: true }).fill('');
  await page.getByRole('button', { name: 'Save time', exact: true }).click();
  await expect(cell).toHaveText('DEL 24/7 FRI ETA 13:00');
  await page.getByLabel('Miles left', { exact: true }).fill('133');
  await page.getByRole('button', { name: 'Apply ETA', exact: true }).click();
  await expect.poll(() => entry.eta).toBe('133 mi');
  await expect(cell).toHaveText('DEL 24/7 FRI 133mi');
  await cell.click();
  await page.getByRole('button', { name: 'Clear ETA', exact: true }).click();
  await expect.poll(() => entry.eta).toBe('');
  await expect(cell).toHaveText('DEL 24/7 FRI ETA 13:00');
  expect(entry.notes).toBe('Human notes stay');
  stop.type = 'pickup'; loads.automation.stopType = 'pickup'; loads.automation.appointment = null;
  loads.automation.arrived = true; loads.automation.arrivalOverride = 'auto';
  loads.automation.stale = true; loads.automation.travelMode = 'auto'; loads.automation.travelValue = '1mi';
  await page.reload();
  await expect(cell).toHaveText('@PU');
  await expect(cell.locator('span').first()).toHaveClass(/text-amber-400/);
  await expect(cell).toHaveAttribute('title', '');
  await cell.click();
  await expect(page.getByLabel('Manual pickup time', { exact: true })).toBeEmpty();
  readOnly = true; await page.reload();
  await expect(cell).toBeDisabled();
  await page.getByRole('button', { name: 'Appointment Driver · Next load', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Appointment Driver · Loads', exact: true });
  const expand = dialog.getByRole('button', { name: '4010927482 · Current load', exact: true });
  await expand.click();
  await expect(expand).toHaveAttribute('aria-expanded', 'false');
  const copy = dialog.getByRole('button', { name: 'Copy load number 4010927482', exact: true });
  await copy.click();
  await expect(copy).toHaveAttribute('title', 'Copied');
  expect(await page.evaluate(() => navigator.clipboard.readText())).toBe('4010927482');
  await expect(expand).toHaveAttribute('aria-expanded', 'false');
  const [copyBox, expandBox] = await Promise.all([copy.boundingBox(), expand.boundingBox()]);
  expect(copyBox.x).toBeGreaterThanOrEqual(expandBox.x + expandBox.width);
  expect(errors).toEqual([]);
  console.log('Appointment board passed: rounded ETA, facility-local hover, live/stale colors, concise hover, manual overrides, read-only load-number copy, notes and permissions.');
} finally { await browser.close(); await new Promise(done => server.close(done)); }
