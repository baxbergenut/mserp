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
const stop = { key: 'stop', type: 'delivery', location: 'South River, NJ', appointment: '', schedule };
const load = { planId: '', loadId: 65341, number: entry.currentLoad, stops: [stop], warning: '', date: '', slot: 0 };
const loads = { current: load, week: [], next: [], earlier: [], hidden: [], unavailable: [], destinationSource: true, sourceDestination: stop.location, stopKey: stop.key, revision: 'fixture', fromDate: '2026-10-17', automation: { context: 'context', appointment: schedule, manualStopTime: '', stopType: 'delivery', arrived: false, arrivalOverride: 'auto', observedAt: new Date().toISOString(), stale: false, travelMode: 'auto', travelValue: 'ETA 13:00', requiresConfirmation: false, routingEnabled: true, manualETAValid: true } };
let readOnly = false;
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
  await page.goto(base + '/driver-board');
  const cell = page.getByRole('button', { name: 'Appointment Driver · ETA', exact: true });
  await expect(cell).toHaveText('DEL 24/7 FRI ETA 13:00');
  await expect(cell).toHaveAttribute('title', /08:00/);
  const copied = await cell.evaluate(button => { let value = ''; const e = new Event('copy', { bubbles: true, cancelable: true }); Object.defineProperty(e, 'clipboardData', { value: { setData: (_, text) => { value = text; } } }); button.dispatchEvent(e); return value; });
  expect(copied).toBe('DEL 24/7 FRI ETA 13:00');
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
  await expect(cell).toHaveAttribute('title', /Last known within 1 mile of selected stop.*GPS stale/);
  await cell.click();
  await expect(page.getByLabel('Manual pickup time', { exact: true })).toBeEmpty();
  readOnly = true; await page.reload();
  await expect(cell).toBeDisabled();
  expect(errors).toEqual([]);
  console.log('Appointment board passed: automatic ETA, manual ETA override, manual stop time override/clear, stale @PU, scheduleless stop, combined copy, notes and read-only permissions.');
} finally { await browser.close(); await new Promise(done => server.close(done)); }
