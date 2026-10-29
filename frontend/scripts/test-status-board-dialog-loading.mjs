// Cold editor downloads must never hide the board or the shared app shell.
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
const driver = { id: 'driver', fullName: 'Cold Load Driver', active: true, driverType: 'M', truckId: '', truckUnit: '101', dispatcherId: '', dispatcherName: '', phone: '', location: null };
const next = { planId: 'next', number: 'NEXT', date: '2026-10-29', slot: 0, loadId: 1, stops: [{ key: 'pickup', type: 'pickup', location: 'Atlanta, GA', schedule: { kind: 'appointment', label: 'PU 14:00 THU' } }] };
const loads = { current: null, week: [next], next: [next], earlier: [], hidden: [], unavailable: [], destinationSource: false, sourceDestination: '', stopKey: '', fromDate: '2026-10-29', revision: 'fixture', customOrder: false };
const entry = { driverId: driver.id, currentLoad: '', trailerNumber: '', trailerNote: '', status: 'DISPATCHED', destination: '', eta: '', notes: 'Keep this note', homeTime: '', version: 1 };
const errors = [];
try {
  for (const test of [
    { button: 'Next load', dialog: 'Loads', marker: 'Clear current selection', close: 'Close loads' },
    { button: 'Next pickup time', dialog: 'Next pickup time', marker: 'Could not save stop time', close: 'Close' },
  ]) {
    // Separate contexts force a cold download for each first-open path.
    const context = await browser.newContext({ viewport: { width: 1800, height: 900 } });
    const page = await context.newPage();
    page.on('pageerror', error => errors.push(error.message));
    let held = false, release;
    const download = new Promise(resolve => { release = resolve; });
    await page.route('**/_next/static/chunks/*.js', async route => {
      const response = await route.fetch();
      const body = await response.text();
      if (body.includes(test.marker)) { held = true; await download; }
      await route.fulfill({ response, body });
    });
    await page.route('**/api/**', async route => {
      const path = new URL(route.request().url()).pathname.slice(4);
      const fixtures = {
        '/auth/session': { user: { id: 'cold-test', username: 'Cold Test', permissions: ['driver_board.read', 'driver_board.write'] }, csrfToken: 'fixture' },
        '/driver-board': { weekStart: '2026-10-26', drivers: [driver], entries: [entry], grossEntries: [], loads: { [driver.id]: loads }, eld: { configured: false } },
        '/driver-board/loads/driver': loads, '/tasks/count': { count: 0 }, '/sidebar-counts': {},
      };
      await route.fulfill({ json: fixtures[path] ?? [] });
    });
    try {
      await page.goto(`http://127.0.0.1:${server.address().port}/driver-board`);
      const row = page.locator('tr[data-driver-id="driver"]');
      await expect(row).toBeVisible();
      await page.evaluate(() => {
        const shell = document.querySelector('.mserp-ui');
        const row = document.querySelector('tr[data-driver-id="driver"]');
        window.boardHiddenFrames = 0;
        const sample = () => {
          if (!shell?.isConnected || !row?.isConnected || !shell.getClientRects().length || !row.getClientRects().length) window.boardHiddenFrames++;
          window.boardFrame = requestAnimationFrame(sample);
        };
        sample();
      });
      await page.getByRole('button', { name: `${driver.fullName} · ${test.button}`, exact: true }).click();
      await expect.poll(() => held, { message: 'The editor chunk should be held before rendering' }).toBe(true);
      // Hold long enough for React to commit any enclosing Suspense fallback.
      await page.waitForTimeout(700);
      await expect(row).toBeVisible({ timeout: 1000 });
      expect(await page.evaluate(() => window.boardHiddenFrames)).toBe(0);
      release();
      const dialog = page.getByRole('dialog', { name: `${driver.fullName} · ${test.dialog}`, exact: true });
      await expect(dialog).toBeVisible();
      await expect(page.getByLabel(`${driver.fullName} · Notes`, { exact: true })).toHaveValue(entry.notes);
      await dialog.getByRole('button', { name: test.close, exact: true }).click();
      await expect(dialog).toHaveCount(0);
      // Warm reopen still works, and the original row stays mounted throughout.
      await page.getByRole('button', { name: `${driver.fullName} · ${test.button}`, exact: true }).click();
      await expect(dialog).toBeVisible();
      expect(await page.evaluate(() => window.boardHiddenFrames)).toBe(0);
      await page.evaluate(() => cancelAnimationFrame(window.boardFrame));
    } finally { release(); await context.close(); }
  }
  expect(errors).toEqual([]);
  console.log('Status Board cold dialog loading passed: load queue and pickup editor preserve the board/app shell during delayed downloads and warm reopen.');
} finally {
  await browser.close();
  await new Promise(resolve => server.close(resolve));
}
