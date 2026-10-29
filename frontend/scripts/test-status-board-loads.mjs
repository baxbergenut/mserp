// Isolated API fixtures exercise full load details across compact live snapshots.
import { chromium, expect } from '@playwright/test';
import { createServer } from 'node:http';
import { readFile } from 'node:fs/promises';
import { extname } from 'node:path';

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
const driver = { id: 'driver-1', fullName: 'Loads Driver', active: true, driverType: 'M', truckId: '', truckUnit: '101', dispatcherId: '', dispatcherName: '', phone: '', location: null };
const load = number => ({ planId: number, number, date: '2026-10-29', slot: 0, loadId: 1, sourceStatus: 'Active', sourceDriver: '', syncedAt: '', warning: '', stops: [{ key: 'pickup', type: 'pickup', location: 'Atlanta, GA', appointment: '' }] });
const current = load('CURRENT'), earlier = load('EARLIER'), next = load('NEXT');
let full = { current, week: [current, next], next: [next], earlier: [earlier], hidden: [load('HIDDEN')], unavailable: [load('DELIVERED')], destinationSource: true, sourceDestination: 'Atlanta, GA', stopKey: 'pickup', fromDate: '2026-10-29', revision: 'first', customOrder: false };
let entry = { driverId: driver.id, currentLoad: current.number, trailerNumber: '', trailerNote: '', status: 'DISPATCHED', destination: '', eta: '', notes: '', homeTime: '', version: 1 };
const overview = () => ({ ...full, detailsOmitted: true, earlier: [], hidden: [], unavailable: [] });
const board = () => ({ weekStart: '2026-10-26', drivers: [driver], entries: [entry], grossEntries: [], loads: { [driver.id]: overview() }, eld: { configured: false } });
let detailReads = 0, boardReads = 0, mutations = 0, holdDetails = false, failDetails = false;
const pending = [], errors = [];
try {
  const page = await browser.newPage({ viewport: { width: 1440, height: 900 } });
  page.on('pageerror', error => errors.push(error.message));
  await page.addInitScript(() => {
    window.boardStreams = [];
    window.EventSource = class extends EventTarget {
      constructor(url) { super(); if (url.includes('/driver-board/events')) window.boardStreams.push(this); }
      close() {}
    };
  });
  await page.route('**/api/**', async route => {
    const req = route.request(), path = new URL(req.url()).pathname.slice(4);
    if (path === '/driver-board/loads/' + driver.id) {
      if (req.method() === 'POST') {
        mutations++;
        const input = req.postDataJSON();
        expect(input.revision).toBe(full.revision);
        expect(input.version).toBe(entry.version);
        expect(input.action).toBe('manual');
        full = { ...full, destinationSource: false, revision: 'saved' };
        entry = { ...entry, version: entry.version + 1 };
        return route.fulfill({ json: { entry, loads: full } });
      }
      detailReads++;
      const response = structuredClone(full);
      const fail = failDetails;
      if (holdDetails) await new Promise(resolve => pending.push(resolve));
      return route.fulfill(fail ? { status: 503, json: { error: 'Temporary detail failure' } } : { json: response });
    }
    if (path === '/driver-board') { boardReads++; return route.fulfill({ json: board() }); }
    const fixtures = {
      '/auth/session': { user: { id: 'loads-test', username: 'Loads Test', permissions: ['driver_board.read', 'driver_board.write'] }, csrfToken: 'fixture' },
      '/tasks/count': { count: 0 }, '/sidebar-counts': {},
    };
    await route.fulfill({ json: fixtures[path] ?? [] });
  });
  const push = () => page.evaluate(value => window.boardStreams.at(-1).dispatchEvent(new MessageEvent('board', { data: JSON.stringify(value) })), board());
  const release = () => { holdDetails = false; pending.splice(0).forEach(resolve => resolve()); };
  const base = process.env.MSERP_STATUS_BOARD_BASE_URL ?? `http://127.0.0.1:${server.address().port}`;
  await page.goto(`${base}/driver-board`);
  await expect(page.locator('tr[data-driver-id]')).toHaveCount(1);
  await page.getByRole('button', { name: 'Loads Driver · Next load', exact: true }).click();
  const panel = page.getByRole('dialog', { name: 'Loads Driver · Loads', exact: true });
  const destination = panel.getByLabel('Current load destination source', { exact: true });
  const history = panel.getByRole('button', { name: 'EARLIER', exact: true });
  await expect(history).toBeVisible();
  await expect(destination).toBeEnabled();
  await expect(panel.getByRole('button', { name: 'CURRENT · Current load', exact: true })).toBeVisible();
  await expect(panel.getByText('Removed from queue (1)', { exact: true })).toBeVisible();
  await expect(panel.getByText('Delivered or cancelled (1)', { exact: true })).toBeVisible();

  // A same-revision overview must neither erase history nor leave editing locked.
  holdDetails = true;
  let before = detailReads;
  const beforeBoard = boardReads;
  await push();
  await expect.poll(() => detailReads).toBeGreaterThan(before);
  await expect(history).toBeVisible();
  await expect(destination).toBeDisabled();
  expect(boardReads).toBe(beforeBoard);
  release();
  await expect(destination).toBeEnabled();
  await expect(history).toBeVisible();

  // Source changes rehydrate the whole panel, retaining old details during the read.
  holdDetails = true;
  full = { ...full, current: next, next: [], earlier: [earlier, current], revision: 'second' };
  entry = { ...entry, currentLoad: next.number, version: 2 };
  before = detailReads;
  await push();
  await expect.poll(() => detailReads).toBeGreaterThan(before);
  await expect(history).toBeVisible();
  await expect(destination).toBeDisabled();
  release();
  await expect(panel.getByRole('button', { name: 'NEXT · Current load', exact: true })).toBeVisible();
  await expect(panel.getByRole('button', { name: 'CURRENT', exact: true })).toBeVisible();
  await expect(destination).toBeEnabled();

  // Failed background reads preserve history and can be retried explicitly.
  failDetails = true;
  full = { ...full, revision: 'third' };
  await push();
  await expect(panel.getByRole('alert')).toContainText('Temporary detail failure');
  await expect(history).toBeVisible();
  await expect(destination).toBeDisabled();
  failDetails = false;
  await panel.getByRole('button', { name: 'Refresh loads', exact: true }).click();
  await expect(destination).toBeEnabled();
  await expect(panel.getByRole('alert')).toHaveCount(0);
  await destination.selectOption('');
  await expect.poll(() => mutations).toBe(1);
  await expect(destination).toHaveValue('');
  await expect(history).toBeVisible();

  // Closing cancels the detail consumer; a delayed response cannot replace a new panel.
  holdDetails = true;
  before = detailReads;
  await push();
  await expect.poll(() => detailReads).toBeGreaterThan(before);
  await panel.getByRole('button', { name: 'Close loads', exact: true }).click();
  release();
  await expect(panel).toHaveCount(0);
  await page.getByRole('button', { name: 'Loads Driver · Next load', exact: true }).click();
  await expect(history).toBeVisible();
  await expect(panel.getByRole('button', { name: 'NEXT · Current load', exact: true })).toBeVisible();
  await expect(destination).toBeEnabled();
  expect(errors).toEqual([]);
  console.log('Status Board loads passed: retained current/history groups across compact live updates, full rehydration, stale edit guards, failure/retry, versioned save and close/reopen cancellation.');
} finally {
  pending.splice(0).forEach(resolve => resolve());
  await browser.close();
  await new Promise(resolve => server.close(resolve));
}
