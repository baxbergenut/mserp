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

// Browser-only preferences: no database or live service is used by this fixture.
const errors = [], writes = [];
const makeDriver = (id, dispatcherId, dispatcherName) => ({ id, fullName: id, active: true, driverType: 'M', truckId: '', truckUnit: '', phone: '', location: null, dispatcherId, dispatcherName });
const original = [makeDriver('unassigned', '', 'Unassigned'), makeDriver('a2', 'a', 'Alpha'), makeDriver('a1', 'a', 'Alpha'), makeDriver('b1', 'b', 'Beta'), makeDriver('c1', 'c', 'Charlie')];
let snapshotDrivers = original;
let pendingDrivers = [];
const emptyLoads = { current: null, week: [], next: [{ planId: 'plan-1', number: '85A2193', loadId: 1, stops: [{ key: 'pickup', type: 'pickup', location: 'Chicago', schedule: { kind: 'window', label: 'PU 24/7 TUE', detail: 'Pickup any time Tuesday' } }] }], earlier: [], hidden: [], unavailable: [], destinationSource: false, sourceDestination: '', stopKey: '', fromDate: '2026-11-02', revision: 'fixture', customOrder: false };
const board = (drivers = original) => ({ weekStart: '2026-11-02', drivers, pendingDrivers, entries: drivers.map(d => ({ driverId: d.id, currentLoad: '', trailerNumber: '', trailerNote: '', status: '', destination: '', eta: '', notes: '', homeTime: '', version: 1 })), driverOrderRevisions: {}, dispatcherOrderRevision: '', grossEntries: [], loads: Object.fromEntries(drivers.map(d => [d.id, emptyLoads])), eld: { configured: false } });
async function setup(page, identity) {
  page.on('pageerror', error => errors.push(error.message));
  await page.addInitScript(() => {
    window.boardStreams = [];
    window.EventSource = class extends EventTarget {
      constructor(url) { super(); this.url = url; window.boardStreams.push(this); }
      close() { window.boardStreams = window.boardStreams.filter(stream => stream !== this); }
    };
  });
  await page.route('**/api/**', async route => {
    const request = route.request(), path = new URL(request.url()).pathname.slice(4);
    if (path === '/driver-board/dispatcher-order' || (path === '/driver-board' && request.method() !== 'GET')) writes.push(path);
    const fixtures = {
      '/auth/session': { user: { id: identity.id, username: identity.id, theme: identity.theme ?? 'default', permissions: ['driver_board.read', ...(identity.edit ? ['driver_board.write', 'drivers.read'] : [])] }, csrfToken: 'fixture' },
      '/driver-board': board(snapshotDrivers), '/tasks/count': { count: 0 }, '/sidebar-counts': {},
    };
    await route.fulfill({ json: path.startsWith('/driver-board/loads/') ? emptyLoads : fixtures[path] ?? [] });
  });
  await page.goto(base + '/driver-board');
  await expect(page.locator('tr[data-driver-id]')).toHaveCount(5);
}
const groupIDs = page => page.locator('tr[data-dispatcher-id]').evaluateAll(rows => rows.map(row => row.dataset.dispatcherId));
const expectOrder = (page, ids) => expect.poll(() => groupIDs(page)).toEqual(ids);
async function moveUp(page, name) {
  const filter = page.getByRole('button', { name: 'Dispatcher filter', exact: true });
  if (await filter.getAttribute('aria-expanded') !== 'true') await filter.click();
  const choices = page.getByRole('group', { name: 'Dispatcher choices', exact: true });
  await expect(choices.getByRole('button', { name: 'Move Unassigned', exact: true })).toHaveCount(1);
  await choices.getByRole('button', { name: 'Move ' + name, exact: true }).press('ArrowUp');
}
try {
  const context = await browser.newContext({ viewport: { width: 1600, height: 1000 } });
  const identity = { id: 'user-a' };
  const page = await context.newPage();
  await setup(page, identity);
  await expectOrder(page, ['', 'a', 'b', 'c']);
  // Read-only accounts may customize their own presentation.
  await moveUp(page, 'Charlie');
  await expectOrder(page, ['', 'a', 'c', 'b']);
  await expect(page.getByText('Your dispatcher order is saved in this browser.', { exact: true })).toBeVisible();
  await page.reload();
  await expectOrder(page, ['', 'a', 'c', 'b']);
  // A second account on the same browser gets a separate preference.
  identity.id = 'user-b';
  await page.reload();
  await expectOrder(page, ['', 'a', 'b', 'c']);
  await moveUp(page, 'Beta');
  await expectOrder(page, ['', 'b', 'a', 'c']);
  identity.id = 'user-a';
  await page.reload();
  await expectOrder(page, ['', 'a', 'c', 'b']);
  // Other tabs for the same account follow localStorage, while another browser does not.
  const tab = await context.newPage();
  await setup(tab, { id: 'user-a' });
  await expectOrder(tab, ['', 'a', 'c', 'b']);
  await moveUp(tab, 'Charlie');
  await expectOrder(tab, ['', 'c', 'a', 'b']);
  await expectOrder(page, ['', 'c', 'a', 'b']);
  const fresh = await browser.newContext();
  const freshPage = await fresh.newPage();
  await setup(freshPage, { id: 'user-a' });
  await expectOrder(freshPage, ['', 'a', 'b', 'c']);
  await fresh.close();
  await tab.close();
  await page.bringToFront();
  // Live snapshots keep local group order and preserve the server's driver order.
  snapshotDrivers = [...original, makeDriver('d1', 'd', 'Delta')];
  const incoming = board(snapshotDrivers);
  await page.evaluate(value => {
    for (const stream of window.boardStreams) stream.dispatchEvent(new MessageEvent('board', { data: JSON.stringify(value) }));
  }, incoming);
  await expectOrder(page, ['', 'c', 'a', 'b', 'd']);
  expect(await page.locator('tr[data-driver-id]').evaluateAll(rows => rows.map(row => row.dataset.driverId))).toEqual(['unassigned', 'c1', 'a2', 'a1', 'b1', 'd1']);
  // Filtering does not rewrite the order or lose hidden groups.
  await page.getByRole('button', { name: 'Dispatcher filter', exact: true }).click();
  const choices = page.getByRole('group', { name: 'Dispatcher choices', exact: true });
  await choices.getByLabel('Alpha', { exact: true }).uncheck();
  await expectOrder(page, ['', 'c', 'b', 'd']);
  await choices.getByLabel('Alpha', { exact: true }).check();
  await expectOrder(page, ['', 'c', 'a', 'b', 'd']);
  // Move Unassigned with keyboard and pointer, then retain it across reload and custom columns.
  await choices.getByRole('button', { name: 'Move Unassigned', exact: true }).press('ArrowDown');
  await expectOrder(page, ['c', '', 'a', 'b', 'd']);
  await page.keyboard.press('Escape');
  await page.locator('thead [data-board-column="driver"]').press('ArrowRight');
  await page.reload();
  await expectOrder(page, ['c', '', 'a', 'b', 'd']);
  await page.getByRole('button', { name: 'Dispatcher filter', exact: true }).click();
  await choices.getByRole('button', { name: 'Move Unassigned', exact: true }).dragTo(choices.getByRole('button', { name: 'Move Delta', exact: true }), { targetPosition: { x: 5, y: 25 } });
  await expectOrder(page, ['c', 'a', 'b', 'd', '']);
  await page.reload();
  await expectOrder(page, ['c', 'a', 'b', 'd', '']);
  pendingDrivers = [{ intakeId: 'pending-hire', fullName: 'Pending Hire' }];
  snapshotDrivers = snapshotDrivers.filter(driver => driver.dispatcherId !== '');
  await page.reload();
  await expectOrder(page, ['c', 'a', 'b', 'd', '']);
  await page.getByRole('button', { name: 'Dispatcher filter', exact: true }).click();
  await choices.getByLabel('Unassigned', { exact: true }).uncheck();
  await expectOrder(page, ['c', 'a', 'b', 'd']);
  await choices.getByLabel('Unassigned', { exact: true }).check();
  await expectOrder(page, ['c', 'a', 'b', 'd', '']);
  expect(await page.locator('tbody > tr').last().getAttribute('data-pending-intake-id')).toBe('pending-hire');
  pendingDrivers = [];
  // Corrupt preferences recover to the default order.
  snapshotDrivers = original;
  await page.evaluate(() => localStorage.setItem('mserp:dispatcher-order:v1:user-a', '{broken'));
  await page.reload();
  await expectOrder(page, ['', 'a', 'b', 'c']);
  // Storage failures leave the tab usable without claiming persistence.
  await page.evaluate(() => { Storage.prototype.setItem = () => { throw new Error('blocked'); }; });
  await moveUp(page, 'Beta');
  await expectOrder(page, ['', 'b', 'a', 'c']);
  await expect(page.getByText('Order changed for this tab. Browser storage is unavailable.', { exact: true })).toBeVisible();
  // Both light themes keep readable text during row and button hover.
  identity.edit = true;
  const contrast = locator => locator.evaluate(element => {
    const canvas = document.createElement('canvas'), ctx = canvas.getContext('2d');
    canvas.width = canvas.height = 1;
    const rgb = color => { ctx.clearRect(0, 0, 1, 1); ctx.fillStyle = color; ctx.fillRect(0, 0, 1, 1); return [...ctx.getImageData(0, 0, 1, 1).data]; };
    const blend = (fg, bg) => fg.slice(0, 3).map((value, i) => value * fg[3] / 255 + bg[i] * (1 - fg[3] / 255));
    const chain = []; for (let node = element; node; node = node.parentElement) chain.unshift(node);
    let background = [255, 255, 255];
    for (const node of chain) background = blend(rgb(getComputedStyle(node).backgroundColor), background);
    const color = blend(rgb(getComputedStyle(element).color), background);
    const luminance = values => values.map(v => { v /= 255; return v <= 0.04045 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4; }).reduce((sum, v, i) => sum + v * [0.2126, 0.7152, 0.0722][i], 0);
    const l = [luminance(color), luminance(background)].sort((a, b) => b - a);
    return (l[0] + 0.05) / (l[1] + 0.05);
  });
  for (const theme of ['default-light', 'solarized-light']) {
    identity.theme = theme;
    await page.reload();
    await expect(page.locator('html')).toHaveAttribute('data-theme', theme);
    const pickup = page.getByRole('button', { name: 'a2 · Next pickup time', exact: true });
    await expect(pickup).toBeEnabled();
    await page.mouse.move(0, 0);
    expect(await contrast(pickup)).toBeGreaterThanOrEqual(4.5);
    await pickup.hover();
    await expect(pickup).toHaveCSS('color', 'rgb(7, 89, 133)');
    expect(await contrast(pickup)).toBeGreaterThanOrEqual(4.5);
    const number = page.getByRole('button', { name: 'a2 · Next load', exact: true }).locator('span');
    await expect(number).toHaveCSS('color', 'rgb(7, 89, 133)');
    expect(await contrast(number)).toBeGreaterThanOrEqual(4.5);
    const driver = page.locator('tr[data-driver-id="a2"] a').filter({ hasText: 'a2' });
    await driver.hover();
    expect(await contrast(driver)).toBeGreaterThanOrEqual(4.5);
    await pickup.focus();
    expect(await contrast(pickup)).toBeGreaterThanOrEqual(4.5);
    await pickup.hover();
    await page.locator('tr[data-driver-id="a2"]').screenshot({ path: join(tmpdir(), 'dispatcher-order-' + theme + '.png') });
  }
  // Dark palettes retain their original hover blue.
  identity.theme = 'default';
  await page.reload();
  const darkPickup = page.getByRole('button', { name: 'a2 · Next pickup time', exact: true });
  await darkPickup.hover();
  expect(await darkPickup.evaluate(el => getComputedStyle(el).color)).not.toBe('rgb(7, 89, 133)');
  expect(writes).toEqual([]);
  expect(errors).toEqual([]);
  console.log('Light theme contrast passed: normal, hover, group hover, links and focus at least 4.5:1; dark hover unchanged.');
  console.log('Dispatcher order passed: read-only users, reloads, account/browser isolation, same-user tabs, live updates, driver order, filters, corrupt/blocked storage and zero shared saves.');
} finally { await browser.close(); await new Promise(resolve => server.close(resolve)); }
