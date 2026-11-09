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
let retryCount = 0;
let retryFailure = false;
let releaseRetry;
let holdRetry = false;
loads.automation.travelArrivalAt = '2026-10-20T17:07:00Z';
loads.automation.travelStale = false;
loads.automation.reason = 'Last known miles remaining · GPS data stale · last observed · Assumed semi profile';
await page.context().grantPermissions(['clipboard-read', 'clipboard-write']);
await page.route('**/api/**', async route => {
  const req = route.request(), path = new URL(req.url()).pathname.slice(4);
  if (req.method() === 'POST' && path === '/driver-board/eta-retry/driver') {
    retryCount++;
    expect(req.postDataJSON()).toEqual({ context: loads.automation.context });
    if (holdRetry) await new Promise(resolve => { releaseRetry = resolve; });
    if (retryFailure) return route.fulfill({ status: 429, json: { error: 'Please wait before retrying automatic ETA' } });
    loads.automation.travelRetryPending = true;
    return route.fulfill({ status: 202, json: { queued: true } });
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
  const retry = page.getByRole('button', { name: /Appointment Driver .* Retry automatic ETA/, exact: true });
  const cell = page.getByRole('button', { name: /^Appointment Driver . ETA$/, exact: true });
  await page.goto(base + '/driver-board');
  await expect(cell).toHaveText('DEL 24/7 FRI ETA 13:00');
  await expect(retry).toHaveCount(0);
  loads.automation.travelValue = '';
  await page.reload();
  await expect(retry).toBeVisible();
  if (process.env.ETA_RETRY_SCREENSHOT) await page.screenshot({ path: process.env.ETA_RETRY_SCREENSHOT });
  const [cellBox, retryBox] = await Promise.all([cell.boundingBox(), retry.boundingBox()]);
  expect(retryBox.x).toBeGreaterThanOrEqual(cellBox.x + cellBox.width - 1);
  retryFailure = true;
  await retry.click();
  await expect(page.getByRole('alert').filter({ hasText: 'Please wait before retrying' })).toContainText('Please wait before retrying');
  await expect(retry).toBeEnabled();
  await expect(page.getByRole('dialog')).toHaveCount(0);
  retryFailure = false; holdRetry = true;
  await retry.click();
  await expect(retry).toBeDisabled();
  await expect(retry).toHaveAttribute('aria-busy', 'true');
  await expect.poll(() => retryCount).toBe(2);
  releaseRetry(); holdRetry = false;
  await expect(page.getByRole('status').filter({ hasText: 'automatic ETA retry queued' })).toBeVisible();
  await page.reload();
  await expect(retry).toBeDisabled();
  await expect(retry.locator('svg')).toHaveClass(/animate-spin/);
  loads.automation.travelRetryPending = false;
  loads.automation.travelValue = 'ETA 14:00';
  await page.reload();
  await expect(retry).toHaveCount(0);
  loads.automation.travelValue = '218mi';
  await page.reload(); await expect(retry).toHaveCount(0);
  loads.automation.travelValue = ''; loads.automation.arrived = true;
  await page.reload(); await expect(retry).toHaveCount(0);
  loads.automation.arrived = false; entry.eta = '133 mi';
  await page.reload(); await expect(retry).toHaveCount(0);
  entry.eta = ''; readOnly = true;
  await page.reload(); await expect(retry).toHaveCount(0);
  readOnly = false; loads.automation.routingEnabled = false;
  await page.reload(); await expect(retry).toHaveCount(0);
  expect(errors).toEqual([]);
  console.log('ETA retry passed: right-side layout, missing-only visibility, loading/repeat prevention, queue persistence, error recovery, permissions, manual/arrival/mileage guards.');
} finally { await browser.close(); await new Promise(done => server.close(done)); }