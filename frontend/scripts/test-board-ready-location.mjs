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
let entry = { driverId: 'driver', currentLoad: '4010927482', status: 'READY', destination: '', trailerNumber: '', trailerNote: '', eta: '', notes: 'Human notes stay', homeTime: '', version: 1 };
const schedule = { kind: 'window', label: 'DEL 24/7 FRI', detail: '2026-10-16 08:00 – 23:59 (facility local time)' };
const stop = { key: 'stop', type: 'delivery', location: 'Chicago, IL', latitude: 41.8781, longitude: -87.6298, appointment: '', schedule };
const load = { planId: 'current', loadId: 65341, number: entry.currentLoad, stops: [stop], warning: '', date: '', slot: 0 };
const loads = { current: load, week: [], next: [], earlier: [], hidden: [], unavailable: [], destinationSource: true, sourceDestination: stop.location, stopKey: stop.key, revision: 'fixture', fromDate: '2026-10-17', automation: { context: 'context', appointment: schedule, manualStopTime: '', stopType: 'delivery', arrived: false, arrivalOverride: 'auto', observedAt: new Date().toISOString(), stale: false, travelMode: 'auto', travelValue: 'ETA 13:00', requiresConfirmation: false, routingEnabled: true, manualETAValid: true } };
let readOnly = false;
let retryCount = 0;
loads.automation.travelArrivalAt = '2026-10-20T17:07:00Z';
loads.automation.travelStale = false;
loads.automation.reason = 'Last known miles remaining · GPS data stale · last observed · Assumed semi profile';
await page.context().grantPermissions(['clipboard-read', 'clipboard-write']);
await page.route('**/api/**', async route => {
  const req = route.request(), path = new URL(req.url()).pathname.slice(4);
  if (req.method() === 'POST' && path === '/driver-board/eta-retry/driver') {
    retryCount++;
    return route.fulfill({ status: 500, json: { error: 'READY must never request ETA' } });
  }
  if (req.method() === 'PUT' && path === '/driver-board') {
    entry = { ...req.postDataJSON().entries[0], version: entry.version + 1 };
    return route.fulfill({ json: [entry] });
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
  const destination = page.getByRole('textbox', { name: /Appointment Driver .* Destination/, exact: true });
  const eta = page.locator('[data-driver-id="driver"] [data-board-column="eta"]');
  loads.automation.readyLocationAddress = '123 Main Street, Chicago, IL 60601';
  entry.eta = '133 mi'; // Must hide retained manual input as well as automatic data.
  await page.goto(base + '/driver-board');
  await expect(destination).toHaveValue(loads.automation.readyLocationAddress);
  await expect(eta).toHaveText('');
  await expect(eta.getByRole('button')).toHaveCount(0);
  await destination.fill('Dispatcher yard');
  await expect.poll(() => entry.destination).toBe('Dispatcher yard');
  await page.reload();
  await expect(destination).toHaveValue('Dispatcher yard');
  await expect(eta).toHaveText('');
  await destination.fill('');
  await expect.poll(() => entry.destination).toBe('');
  await expect(destination).toHaveValue(loads.automation.readyLocationAddress);
  loads.automation.readyLocationStale = true;
  await page.reload();
  await expect(destination).toHaveClass(/text-yellow-400/);
  loads.automation.readyLocationAddress = '';
  await page.reload();
  await expect(destination).toHaveValue('');
  await expect(destination).toHaveAttribute('placeholder', 'Current location unavailable');
  await expect(eta).toHaveText('');
  entry.status = 'ENROUTE'; entry.eta = '';
  await page.reload();
  await expect(destination).toHaveValue('Chicago, IL');
  await expect(eta).toContainText('ETA 13:00');
  entry.status = 'READY'; readOnly = true;
  loads.automation.readyLocationAddress = '456 Lake Street, Chicago, IL 60602';
  await page.reload();
  await expect(destination).toHaveValue(loads.automation.readyLocationAddress);
  await expect(destination).toBeDisabled();
  await expect(eta).toHaveText('');
  expect(retryCount).toBe(0);
  expect(errors).toEqual([]);
  console.log('READY browser checks passed: address, save/reload override, clear override, stale/missing GPS, manual/automatic ETA suppression, non-READY regression and read-only access.');
} finally { await browser.close(); await new Promise(done => server.close(done)); }
