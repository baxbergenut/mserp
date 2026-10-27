// Fixture-only browser test: no database, production, or provider writes.
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
const week = '2026-10-19';
const trips = [
  { id: 101, number: 'T-01', driverName: 'Split First', truckUnit: 'A', originalRate: '500', miles: '40', loadedMiles: '0', emptyMiles: '40', startDate: week, endDate: week },
  { id: 102, number: 'T-02', driverName: 'Split Second', truckUnit: 'B', originalRate: '1313', miles: '515', loadedMiles: '475', emptyMiles: '40', startDate: week, endDate: week },
];
const drivers = trips.map((t, i) => ({ id: `driver-${i}`, fullName: t.driverName, active: true, truckId: `truck-${i}`, truckUnit: t.truckUnit, dispatcherId: 'dispatcher', dispatcherName: 'Dispatch' }));
let entries = drivers.map((d, i) => ({ driverId: d.id, date: week, slot: 0, deleted: false, dayStatus: '', loadNumber: 'SPLIT-1', loadRecordId: 8101,
  loadTripId: null, split: true, tripReview: true, trips, originalRate: '', miles: '', driverRate: i ? '1000' : '200', enteredOriginalRate: '1813', enteredMiles: '555', systemOriginalRate: '', systemMiles: '', version: 1, duplicate: false }));
let readOnly = false;
let saves = 0;
try {
  const page = await browser.newPage({ viewport: { width: 1800, height: 1000 } });
  await page.clock.setFixedTime(new Date("2026-10-20T12:00:00Z"));
  const errors = [];
  page.on('pageerror', e => errors.push(e.message));
  await page.route('**/api/**', async route => {
    const req = route.request(), path = new URL(req.url()).pathname.slice(4);
    if (req.method() === 'PUT' && path === '/gross-board') {
      const changes = req.postDataJSON().entries.map(e => ({ ...e, version: e.version + 1, acceptSystemValues: false }));
      for (const c of changes) {
        const before = entries.find(e => e.driverId === c.driverId);
        expect(c.driverRate).toBe(before.driverRate);
        expect(c.acceptSystemValues).toBe(false);
      }
      entries = entries.map(e => changes.find(c => c.driverId === e.driverId) ?? e); saves++;
      return route.fulfill({ json: changes });
    }
    const fixtures = {
      '/auth/session': { user: { id: 'split-test', username: 'Split Test', theme: 'original', permissions: ['board.read', 'loads.read', 'drivers.read', 'trucks.read', 'dispatchers.read', ...(readOnly ? [] : ['board.write'])] }, csrfToken: 'fixture' },
      '/gross-board': { weekStart: week, entries, drivers, balances: [] },
      '/sidebar-counts': {}, '/tasks/count': { count: 0 },
    };
    return route.fulfill({ json: fixtures[path] ?? [] });
  });
  await page.goto(`${base}/gross-board?weekStart=${week}`);
  for (let i = 0; i < drivers.length; i++) {
    await page.getByRole('button', { name: `Split load SPLIT-1 for ${drivers[i].fullName}`, exact: true }).click();
    const dialog = page.getByRole('dialog', { name: 'Review load SPLIT-1', exact: true });
    await expect(dialog).toBeVisible();
    await expect(dialog).toContainText('$500.00'); await expect(dialog).toContainText('$1,313.00');
    if (!i) await page.screenshot({ path: join(tmpdir(), 'clustie-split-load-review.png'), fullPage: true });
    await dialog.getByRole('button', { name: 'Use trip', exact: true }).nth(i).click();
    await expect.poll(() => entries[i].loadTripId).toBe(trips[i].id);
    await expect(page.getByLabel(`${drivers[i].fullName}, ${week}, total gross`, { exact: true })).toHaveValue(i ? '$1,313.00' : '$500.00');
    await expect(page.getByLabel(`${drivers[i].fullName}, ${week}, miles`, { exact: true })).toHaveValue(i ? '515.00' : '40.00');
  }
  expect(entries.reduce((sum, e) => sum + Number(e.originalRate), 0)).toBe(1813);
  expect(entries.reduce((sum, e) => sum + Number(e.miles), 0)).toBe(555);
  const before = saves; readOnly = true; await page.reload();
  await page.getByRole('button', { name: 'Split load SPLIT-1 for Split First', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Review load SPLIT-1', exact: true });
  await expect(dialog.getByRole('button', { name: 'Use this trip', exact: true })).toBeDisabled();
  await expect(dialog.getByRole('button', { name: 'Use trip', exact: true })).toBeDisabled();
  expect(saves).toBe(before); expect(errors).toEqual([]);
  console.log('Split UI passed: explicit trip choices, separate allocations, preserved driver gross, autosave, and read-only protection.');
} finally { await browser.close(); await new Promise(resolve => server.close(resolve)); }
