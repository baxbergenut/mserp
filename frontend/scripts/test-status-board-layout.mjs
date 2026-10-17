// Fixture API: verifies saved ETA values and real browser scroll routing.
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
const drivers = Array.from({ length: 100 }, (_, i) => ({ id: `driver-${i}`, fullName: `Scroll Driver ${i}`, active: true, driverType: 'M',
  truckId: '', truckUnit: `T${i}`, dispatcherId: 'dispatch', dispatcherName: 'Dispatch', phone: '', location: null }));
let entries = drivers.map(d => ({ driverId: d.id, currentLoad: '', trailerNumber: '', trailerNote: '', status: 'ENROUTE', destination: '', eta: d.id === 'driver-0' ? '13:00 · 300 mi' : '', notes: '', homeTime: '', version: 1 }));
const loads = { current: null, week: [], next: [], earlier: [], hidden: [], unavailable: [], destinationSource: false, sourceDestination: '', stopKey: '', fromDate: '2026-10-12', revision: 'fixture', customOrder: false };
const errors = [];
async function setup(page) {
  page.on('pageerror', error => errors.push(error.message));
  await page.route('**/api/**', async route => {
    const req = route.request(), path = new URL(req.url()).pathname.slice(4);
    if (req.method() === 'PUT' && path === '/driver-board') {
      const input = req.postDataJSON().entries;
      if (input.some(e => e.version !== entries.find(current => current.driverId === e.driverId).version)) return route.fulfill({ status: 409, json: { error: 'Stale version' } });
      const committed = input.map(e => ({ ...e, version: e.version + 1 }));
      entries = entries.map(e => committed.find(c => c.driverId === e.driverId) ?? e);
      return route.fulfill({ json: committed });
    }
    const fixtures = {
      '/auth/session': { user: { id: 'scroll', username: 'Scroll Test', permissions: ['driver_board.read', 'driver_board.write'] }, csrfToken: 'fixture' },
      '/driver-board': { weekStart: '2026-10-12', drivers, entries, grossEntries: [], loads: Object.fromEntries(drivers.map(d => [d.id, loads])), eld: { configured: false } },
      '/tasks/count': { count: 0 }, '/sidebar-counts': {},
    };
    await route.fulfill({ json: path.startsWith('/driver-board/loads/') ? loads : fixtures[path] ?? [] });
  });
  await page.goto(base + '/driver-board');
  await expect(page.locator('tr[data-driver-id]')).toHaveCount(100);
}
try {
  const page = await browser.newPage({ viewport: { width: 1440, height: 900 } });
  await setup(page);
  const eta = page.getByLabel('Scroll Driver 0 · ETA', { exact: true });
  await expect(eta).toHaveText('300mi');
  await eta.click();
  await expect(page.getByLabel('Arrival time', { exact: true })).toHaveValue('');
  await page.getByLabel('Arrival time', { exact: true }).fill('13:00');
  await expect(page.getByLabel('Miles left', { exact: true })).toHaveValue('');
  await page.getByRole('button', { name: 'Apply ETA' }).click();
  await expect.poll(() => entries[0].eta).toBe('13:00');
  await page.reload();
  await expect(eta).toHaveText('ETA 13:00');
  await eta.click();
  await page.getByLabel('Miles left', { exact: true }).fill('0');
  await expect(page.getByLabel('Arrival time', { exact: true })).toHaveValue('');
  await page.getByRole('button', { name: 'Apply ETA' }).click();
  await expect.poll(() => entries[0].eta).toBe('0 mi');
  await page.reload();
  await expect(eta).toHaveText('0mi');
  const main = page.getByRole('main'), area = page.locator('[data-scroll-key="driver-board"]');
  const positions = () => page.evaluate(() => {
    const main = document.querySelector('main'), area = document.querySelector('[data-scroll-key="driver-board"]');
    return { page: main.scrollTop, rows: area.scrollTop, left: area.scrollLeft, header: area.querySelector('thead').getBoundingClientRect().top - main.getBoundingClientRect().top, max: main.scrollHeight - main.clientHeight };
  });
  await main.evaluate(e => { e.scrollTop = 0; });
  await area.evaluate(e => { e.scrollTop = 0; });
  const box = await area.boundingBox();
  await page.mouse.move(box.x + 300, box.y + 140);
  await page.mouse.wheel(0, 70);
  await expect.poll(async () => (await positions()).page).toBeGreaterThan(60);
  expect((await positions()).rows).toBe(0);
  await page.mouse.wheel(0, 600);
  await expect.poll(async () => (await positions()).rows).toBeGreaterThan(300);
  const docked = await positions();
  expect(Math.abs(docked.header - 9)).toBeLessThan(2);
  expect(docked.page).toBe(docked.max);
  await page.mouse.wheel(0, 500);
  await expect.poll(async () => (await positions()).rows).toBeGreaterThan(docked.rows + 450);
  expect((await positions()).page).toBe(docked.page);
  await area.evaluate(e => { e.scrollTop = e.scrollHeight; });
  await page.mouse.wheel(0, 1000);
  expect((await positions()).page).toBe(docked.page);
  const bottom = await positions();
  await page.mouse.wheel(0, -bottom.rows - 60);
  await expect.poll(async () => (await positions()).page).toBeLessThan(docked.page - 50);
  expect((await positions()).rows).toBe(0);
  await page.mouse.wheel(250, 0);
  await expect.poll(async () => (await positions()).left).toBeGreaterThan(0);
  await page.setViewportSize({ width: 1200, height: 650 });
  await main.evaluate(e => { e.scrollTop = e.scrollHeight; });
  await expect.poll(async () => Math.abs((await positions()).header - 9)).toBeLessThan(2);
  await page.screenshot({ path: join(tmpdir(), 'mserp-status-board-docked.png') });
  const mobile = await browser.newPage({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true });
  await setup(mobile);
  await mobile.getByRole('button', { name: 'Collapse sidebar', exact: true }).click();
  expect(await mobile.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await mobile.locator('[data-scroll-key="driver-board"]').evaluate(area => {
    const point = y => new Touch({ identifier: 1, target: area, clientX: 180, clientY: y });
    for (let swipe = 0; swipe < 3; swipe++) {
      area.dispatchEvent(new TouchEvent('touchstart', { bubbles: true, touches: [point(600)] }));
      area.dispatchEvent(new TouchEvent('touchmove', { bubbles: true, cancelable: true, touches: [point(100)] }));
      area.dispatchEvent(new TouchEvent('touchend', { bubbles: true, touches: [] }));
    }
  });
  await expect.poll(() => mobile.locator('[data-scroll-key="driver-board"]').evaluate(e => e.scrollTop)).toBeGreaterThan(0);
  expect(errors).toEqual([]);
  console.log('Status Board layout passed: mutually exclusive persisted ETA, zero miles, legacy display, page/table wheel handoff, reverse scrolling, bottom containment, horizontal overflow, resize and touch.');
} finally { await browser.close(); await new Promise(resolve => server.close(resolve)); }
