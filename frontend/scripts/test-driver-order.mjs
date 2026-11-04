// Fixture-only browser coverage: no live API, database, or vendor writes.
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
const base = `http://127.0.0.1:${server.address().port}`;


// Synthetic browser data only; every API request is intercepted.
const failures = [], assignments = [], sharedReorders = [];
let failTransfer = false;
let roster = [
  ['u1','', 'Unassigned'], ['u2','', 'Unassigned'],
  ['a1','a','Alpha'], ['a2','a','Alpha'], ['a3','a','Alpha'],
  ['b1','b','Beta'], ['b2','b','Beta'],
].map(([id, dispatcherId, dispatcherName]) => ({ id, fullName: id, dispatcherId, dispatcherName, active: true, driverType: 'M', truckId: '', truckUnit: '', city: '', state: '', phone: '', location: null }));
const loads = { current: null, week: [], next: [], earlier: [], hidden: [], unavailable: [], destinationSource: false, sourceDestination: '', stopKey: '', fromDate: '2026-11-02', revision: 'fixture', customOrder: false };
const snapshot = () => ({ weekStart: '2026-11-02', drivers: [...roster].sort((a,b) => a.dispatcherId.localeCompare(b.dispatcherId) || a.id.localeCompare(b.id)), entries: roster.map(d => ({ driverId: d.id, currentLoad: '', trailerNumber: '', status: '', destination: '', eta: '', notes: '', homeTime: '', version: 1 })), driverOrderRevisions: { '': 'u-revision', a: 'a-revision', b: 'b-revision' }, grossEntries: [], loads: Object.fromEntries(roster.map(d => [d.id, loads])), eld: { configured: false } });
const rows = page => page.locator('tr[data-driver-id]').evaluateAll(rows => rows.map(row => row.dataset.driverId));
const expectRows = (page, ids) => expect.poll(() => rows(page)).toEqual(ids);
async function setup(page, identity) {
  page.on('pageerror', error => failures.push(error.message));
  await page.addInitScript(() => {
    window.streams = [];
    window.EventSource = class extends EventTarget {
      constructor() { super(); window.streams.push(this); }
      close() { window.streams = window.streams.filter(stream => stream !== this); }
    };
  });
  await page.route('**/api/**', async route => {
    const request = route.request(), path = new URL(request.url()).pathname.slice(4);
    if (path === '/driver-board/order') {
      const input = request.postDataJSON();
      if (input.targetDispatcherId === undefined || input.targetDispatcherId === input.dispatcherId) sharedReorders.push(input);
      assignments.push(input);
      if (failTransfer) return route.fulfill({ status: 409, json: { error: 'Assignment changed. Reload the board.' } });
      const driver = roster.find(d => d.id === input.driverId);
      driver.dispatcherId = input.targetDispatcherId;
      driver.dispatcherName = roster.find(d => d.id !== driver.id && d.dispatcherId === driver.dispatcherId)?.dispatcherName ?? 'Unassigned';
      return route.fulfill({ json: { dispatcherId: driver.dispatcherId, driverIds: roster.filter(d => d.dispatcherId === driver.dispatcherId).map(d => d.id), revision: snapshot().driverOrderRevisions[driver.dispatcherId], sourceRevision: snapshot().driverOrderRevisions[input.dispatcherId] } });
    }
    const fixtures = {
      '/auth/session': { user: { id: identity.id, username: identity.id, permissions: ['driver_board.read', ...(identity.edit ? ['driver_board.write'] : [])] }, csrfToken: 'fixture' },
      '/driver-board': snapshot(), '/tasks/count': { count: 0 }, '/sidebar-counts': {},
    };
    await route.fulfill({ json: path.startsWith('/driver-board/loads/') ? loads : fixtures[path] ?? [] });
  });
  await page.goto(base + '/driver-board');
  await expect(page.locator('tr[data-driver-id]')).toHaveCount(roster.length);
}
const move = (page, id, key) => page.getByRole('button', { name: 'Move ' + id, exact: true }).press(key);
const live = (page) => page.evaluate(value => { for (const stream of window.streams) stream.dispatchEvent(new MessageEvent('board', { data: JSON.stringify(value) })); }, snapshot());
try {
  const context = await browser.newContext({ viewport: { width: 1800, height: 1000 } });
  const identity = { id: 'alice', edit: false };
  const page = await context.newPage(); await setup(page, identity);
  const baseline = ['u1','u2','a1','a2','a3','b1','b2'];
  await expectRows(page, baseline);
  await move(page, 'a3', 'ArrowUp');
  await expectRows(page, ['u1','u2','a1','a3','a2','b1','b2']);
  await move(page, 'u2', 'ArrowUp');
  await expectRows(page, ['u2','u1','a1','a3','a2','b1','b2']);
  await page.reload();
  await expectRows(page, ['u2','u1','a1','a3','a2','b1','b2']);
  // Another account in the very same browser has its own ordering.
  const bob = await context.newPage(); await setup(bob, { id: 'bob', edit: false });
  await expectRows(bob, baseline);
  await Promise.all([move(page, 'a3', 'ArrowUp'), move(bob, 'a2', 'ArrowUp')]);
  await expectRows(page, ['u2','u1','a3','a1','a2','b1','b2']);
  await expectRows(bob, ['u1','u2','a2','a1','a3','b1','b2']);
  await live(page); await live(bob);
  await expectRows(page, ['u2','u1','a3','a1','a2','b1','b2']);
  await expectRows(bob, ['u1','u2','a2','a1','a3','b1','b2']);
  // Reloading with a different identity cannot inherit the previous account's state.
  identity.id = 'bob'; await page.reload();
  await expectRows(page, ['u1','u2','a2','a1','a3','b1','b2']);
  identity.id = 'alice'; await page.reload();
  await expectRows(page, ['u2','u1','a3','a1','a2','b1','b2']);
  // Hidden rows retain their relative position when moving visible rows.
  await page.getByLabel('Search Status Board', { exact: true }).fill('a');
  roster.find(d => d.id === 'a1').fullName = 'Hidden driver';
  await live(page);
  // The dispatcher name also matches 'a', so use a unique driver-name filter.
  roster.find(d => d.id === 'a2').fullName = 'Visible a2';
  roster.find(d => d.id === 'a3').fullName = 'Visible a3';
  await live(page);
  await page.getByLabel('Search Status Board', { exact: true }).fill('Visible');
  await expectRows(page, ['a3','a2']);
  await move(page, 'Visible a2', 'ArrowUp');
  await page.getByLabel('Search Status Board', { exact: true }).fill('');
  await expectRows(page, ['u2','u1','a2','a3','a1','b1','b2']);
  // Read-only drags cannot cross group boundaries or submit assignments.
  const b1 = page.getByRole('button', { name: 'Move b1', exact: true });
  await b1.dragTo(page.locator('tr[data-driver-id="a2"]'));
  await expect(page.locator('[data-row-dragging]')).toHaveCount(0);
  expect(assignments).toEqual([]);
  await expectRows(page, ['u2','u1','a2','a3','a1','b1','b2']);
  // Pointer reordering remains available within a group for read-only users.
  await page.getByRole('button', { name: 'Move b2', exact: true }).dragTo(page.locator('tr[data-driver-id="b1"]'), { targetPosition: { x: 150, y: 2 } });
  await expectRows(page, ['u2','u1','a2','a3','a1','b2','b1']);
  const tab = await context.newPage(); await setup(tab, { id: 'alice', edit: false });
  await expectRows(tab, ['u2','u1','a2','a3','a1','b2','b1']);
  await move(tab, 'b1', 'ArrowUp');
  await expectRows(page, ['u2','u1','a2','a3','a1','b1','b2']);
  await tab.close();
  const fresh = await browser.newContext(); const freshPage = await fresh.newPage();
  await setup(freshPage, { id: 'alice', edit: false }); await expectRows(freshPage, baseline); await fresh.close();
  // New drivers append to customized groups; personal order survives live refresh.
  roster.push({ ...roster.find(d => d.id === 'a2'), id: 'a0', fullName: 'New driver' });
  await page.bringToFront(); await live(page);
  await expectRows(page, ['u2','u1','a2','a3','a1','a0','b1','b2']);
  identity.edit = true; await page.reload();
  // A real assignment is still shared, but its placement follows the mover's preference.
  await move(page, 'Visible a2', 'Alt+ArrowUp');
  await expect.poll(() => assignments.length).toBe(1);
  await expectRows(page, ['u2','u1','a2','a3','a1','a0','b1','b2']);
  await bob.bringToFront(); await live(bob);
  await expectRows(bob, ['a2','u1','u2','a1','a3','a0','b1','b2']);
  expect(assignments[0]).toMatchObject({ dispatcherId: 'a', targetDispatcherId: '', revision: 'a-revision', targetRevision: 'u-revision' });
  await page.bringToFront();
  failTransfer = true;
  await move(page, 'b1', 'Alt+ArrowUp');
  await expect(page.getByRole('alert').filter({ hasText: 'Assignment changed.' })).toBeVisible();
  await expectRows(page, ['u2','u1','a2','a3','a1','a0','b1','b2']);
  failTransfer = false;
  // Corrupt/blocked storage is recoverable and does not produce shared saves.
  await page.evaluate(() => localStorage.setItem('mserp:driver-order:v1:alice', '{invalid'));
  await page.reload();
  await expectRows(page, ['a2','u1','u2','a0','a1','a3','b1','b2']);
  await page.evaluate(() => { Storage.prototype.setItem = () => { throw new Error('blocked'); }; });
  await move(page, 'b2', 'ArrowUp');
  await expectRows(page, ['a2','u1','u2','a0','a1','a3','b2','b1']);
  await expect(page.getByText('Order changed for this tab. Browser storage is unavailable.', { exact: true })).toBeVisible();
  expect(sharedReorders).toEqual([]); expect(failures).toEqual([]);
  console.log('Personal driver order passed: keyboard/pointer, concurrent users, account/browser isolation, reloads/tabs, filters, live rosters, read-only restrictions, shared assignments and failed-transfer rollback, corrupt/blocked storage, zero shared reorder saves.');
} finally { await browser.close(); await new Promise(resolve => server.close(resolve)); }
