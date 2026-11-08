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
next.stops[0].streetAddress = '100 First Street';
next.stops.push(
  { key: 'second-pickup', type: 'pickup', location: 'Atlanta, GA', streetAddress: '200 Second Street', appointment: '' },
  { key: 'delivery', type: 'delivery', location: 'Dallas, TX', appointment: '' },
);
const later = load('LATER');
const completed = { ...load('COMPLETED-HERE'), completed: true, canRestoreToQueue: true };
const delivered = { ...load('DELIVERED'), completed: true, sourceStatus: 'Delivered' };
const canceled = { ...load('CANCELED'), sourceStatus: 'Canceled' };
let full = { current, week: [current, next, later], next: [next, later], earlier: [earlier], hidden: [{ ...load('HIDDEN'), manual: true, canRemoveFromQueue: true }], unavailable: [completed, delivered, canceled], destinationSource: true, sourceDestination: 'Atlanta, GA', stopKey: 'pickup', fromDate: '2026-10-29', revision: 'first', customOrder: false };
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
        const input = req.postDataJSON();
        expect(input.revision).toBe(full.revision);
        expect(input.version).toBe(entry.version);
        if (input.action === 'order') {
          expect(input.order).toEqual(full.next.map(load => load.planId).reverse());
          full = { ...full, next: [...full.next].reverse(), revision: `order-${entry.version}`, customOrder: true };
          entry = { ...entry, version: entry.version + 1 };
          return route.fulfill({ json: { entry, loads: full } });
        }
        mutations++;
        if (input.action === 'advance') {
          if (full.stopKey === 'pickup') {
            expect(input.stopKey).toBe('second-pickup');
            full = { ...full, stopKey: input.stopKey, completedStopKeys: ['pickup'], destinationSource: true, revision: 'second-pickup' };
          } else {
            expect(input.stopKey).toBeUndefined();
            full = { ...full, stopKey: 'delivery', completedStopKeys: ['pickup', 'second-pickup'], sourceDestination: 'Dallas, TX', revision: 'delivery' };
            entry = { ...entry, status: 'RESERVED' };
          }
        } else if (input.action === 'restore_previous') {
          expect(input.planId).toBe('PREVIOUSLY-ACTIVE');
          full = { ...full, next: [...full.next, { ...load(input.planId), sourceStatus: 'Dispatched' }], earlier: full.earlier.filter(load => load.planId !== input.planId), revision: 'previous-restored' };
        } else if (input.action === 'restore_completed') {
          expect(input.planId).toBe(completed.planId);
          full = { ...full, next: [load(completed.number)], unavailable: [delivered, canceled], revision: 'restored' };
        } else {
          expect(input.action).toBe('manual');
          full = { ...full, destinationSource: false, revision: 'saved' };
        }
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
  await expect(panel.getByText('Unavailable loads (1)', { exact: true })).toBeVisible();

  // Overdue plans remain visible in both the table and live-updated queue.
  full = { ...full, next: [{ ...next, date: '2026-10-27', overdue: true }, later], revision: 'overdue' };
  await push();
  await expect(page.getByRole('button', { name: 'Loads Driver · Next load', exact: true })).toContainText('Overdue');
  await expect(panel.getByRole('button', { name: 'NEXT · Overdue load', exact: true })).toBeVisible();
  await panel.getByRole('button', { name: 'NEXT · Overdue load', exact: true }).click();
  await expect(panel.getByText('Remove this plan in Gross Board.', { exact: true })).toBeVisible();
  await expect(panel.getByRole('button', { name: 'Remove from queue', exact: true })).toHaveCount(0);
  await page.screenshot({ path: '../artifacts/overdue-load-window.png', fullPage: true });
  // An upstream In Transit plan leaves Next and remains below Current in history.
  // An in-transit driver/current load must not suppress its other pending plans.
  full = { ...full, current: { ...current, sourceStatus: 'in_transit' }, next: [later], earlier: [earlier, { ...next, sourceStatus: ' In Transit ', overdue: false }], revision: 'in-transit-history' };
  entry = { ...entry, status: 'ENROUTE' };
  await push();
  await expect(page.getByRole('button', { name: 'Loads Driver · Next load', exact: true })).toHaveText('LATER');
  await expect(panel.locator('[data-queued-load]')).toHaveCount(1);
  await expect(panel.locator('[data-queued-load="NEXT"]')).toHaveCount(0);
  const transitHistory = panel.getByRole('button', { name: 'NEXT · In Transit load', exact: true });
  await expect(transitHistory).toBeVisible();
  await expect(panel.getByRole('button', { name: 'CURRENT · Current load', exact: true })).toBeVisible();
  const labels = await panel.getByRole('region', { name: 'Load sequence' }).locator('article > div > button[aria-expanded]').evaluateAll(buttons => buttons.map(button => button.getAttribute('aria-label')));
  expect(labels.indexOf('NEXT · In Transit load')).toBeGreaterThan(labels.indexOf('CURRENT · Current load'));
  await expect(transitHistory).not.toContainText('Overdue');
  await page.screenshot({ path: '../artifacts/in-transit-load-history.png', fullPage: true });
  await panel.getByLabel('Add next load', { exact: true }).fill('NEXT');
  await panel.getByRole('button', { name: 'Add next load', exact: true }).click();
  await expect(panel.getByRole('alert')).toHaveText('This load is already listed. Use its existing entry.');
  expect(mutations).toBe(0);
  await panel.getByLabel('Add next load', { exact: true }).fill('');
  full = { ...full, current, next: [next, later], earlier: [earlier], revision: 'in-transit-cleared' };
  entry = { ...entry, status: 'DISPATCHED' };
  await push();
  await expect(panel.locator('[data-queued-load]')).toHaveCount(2);
  await expect(panel.getByRole('button', { name: 'NEXT', exact: true })).toBeVisible();
  // A manual identity backed by Gross Board is protected too. Once Gross
  // removes that backing, a refreshed capability exposes manual-only removal.
  full = { ...full, next: [{ ...next, manual: true, canRemoveFromQueue: false }, later], revision: 'gross-backed-manual' };
  await push();
  await expect(panel.getByRole('button', { name: 'NEXT', exact: true })).toBeVisible();
  await expect(panel.getByRole('button', { name: 'Remove from queue', exact: true })).toHaveCount(0);
  full = { ...full, next: [{ ...next, manual: true, canRemoveFromQueue: true }, later], revision: 'manual-only' };
  await push();
  await expect(panel.getByRole('button', { name: 'Remove from queue', exact: true })).toBeVisible();
  full = { ...full, next: [next, later], revision: 'overdue-cleared' };
  await push();
  await expect(panel.getByRole('button', { name: 'NEXT', exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Loads Driver · Next load', exact: true })).not.toContainText('Overdue');

  await expect(panel.getByRole('button', { name: 'Remove from queue', exact: true })).toHaveCount(0);
  await panel.getByRole('button', { name: 'CURRENT · Current load', exact: true }).click();
  await expect(destination).toBeEnabled();

  const sequence = panel.getByRole('region', { name: 'Load sequence' });
  const cards = sequence.getByRole('button', { name: /^(LATER|NEXT|CURRENT · Current load|DELIVERED · Completed load|COMPLETED-HERE · Completed load|EARLIER)$/ });
  const historyOrder = [/^CURRENT/, /^DELIVERED/, /^COMPLETED-HERE/, 'EARLIER'];
  await expect(cards).toHaveText(['LATER', 'NEXT', ...historyOrder]);
  await expect(sequence.locator('[data-queued-load]')).toHaveCount(2);
  await expect(sequence.getByRole('button', { name: /^Reorder load / })).toHaveCount(0);

  // Identical live snapshots must not refetch, lock controls, or shift the panel.
  holdDetails = true;
  let before = detailReads;
  const beforeBoard = boardReads;
  const originalY = (await sequence.boundingBox()).y;
  await page.evaluate(() => {
    window.loadHistoryFlashes = 0;
    window.loadHistoryObserver = new MutationObserver(records => {
      for (const record of records) for (const node of record.addedNodes) {
        if (node.textContent?.includes('Loading load history')) window.loadHistoryFlashes++;
      }
    });
    window.loadHistoryObserver.observe(document.body, { childList: true, subtree: true });
  });
  for (let i = 0; i < 3; i++) {
    await push();
    await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))));
    await expect(destination).toBeEnabled();
    expect((await sequence.boundingBox()).y).toBe(originalY);
  }
  expect(detailReads).toBe(before);
  await expect(history).toBeVisible();
  expect(boardReads).toBe(beforeBoard);
  release();
  await expect(destination).toBeEnabled();
  await expect(history).toBeVisible();

  // Only upcoming cards can move. Dragging and the keyboard use the same queue.
  const laterButton = sequence.getByRole('button', { name: 'LATER', exact: true });
  const cardBox = await laterButton.boundingBox();
  const nextBox = await sequence.getByRole('button', { name: 'NEXT', exact: true }).boundingBox();
  const copyBox = await sequence.getByRole('button', { name: 'Copy load number LATER' }).boundingBox();
  await page.mouse.move(copyBox.x + copyBox.width / 2, copyBox.y + copyBox.height / 2);
  await page.mouse.down();
  await page.mouse.move(copyBox.x + copyBox.width / 2, nextBox.y + nextBox.height - 2, { steps: 5 });
  await expect(sequence.locator('[data-load-dragging]')).toHaveCount(0);
  await page.mouse.up();
  await expect(cards).toHaveText(['LATER', 'NEXT', ...historyOrder]);
  await page.mouse.move(cardBox.x + cardBox.width / 2, cardBox.y + cardBox.height / 2);
  await page.mouse.down();
  await page.mouse.move(cardBox.x + cardBox.width / 2, nextBox.y + nextBox.height - 2, { steps: 5 });
  await expect(sequence.locator('[data-queued-load="LATER"]')).toHaveAttribute('data-load-dragging', 'true');
  await expect(sequence.locator('[data-queued-load="NEXT"]')).toHaveCSS('transition-duration', '0.18s');
  await page.mouse.up();
  await expect(cards).toHaveText(['NEXT', 'LATER', ...historyOrder]);
  await expect(laterButton).toHaveAttribute('aria-expanded', 'false');
  await laterButton.click();
  await sequence.getByRole('button', { name: 'Move LATER up' }).press('Enter');
  await expect(cards).toHaveText(['LATER', 'NEXT', ...historyOrder]);
  await sequence.getByRole('button', { name: 'DELIVERED · Completed load' }).click();
  await expect(sequence.getByText('DataTruck: Delivered', { exact: false })).toBeVisible();
  await expect(sequence.getByRole('button', { name: 'Set current', exact: true })).toHaveCount(0);
  await expect(sequence.getByRole('button', { name: 'Restore to queue · DELIVERED' })).toHaveCount(0);
  await sequence.getByRole('button', { name: 'CURRENT · Current load', exact: true }).click();
  await expect(destination).toBeEnabled();

  // Source changes rehydrate the whole panel, retaining old details during the read.
  holdDetails = true;
  full = { ...full, current: next, next: [], earlier: [earlier, current], revision: 'second' };
  entry = { ...entry, currentLoad: next.number, version: entry.version + 1 };
  before = detailReads;
  await push();
  await expect.poll(() => detailReads).toBeGreaterThan(before);
  await expect(history).toBeVisible();
  await expect(destination).toBeDisabled();
  expect((await sequence.boundingBox()).y).toBe(originalY);
  await expect(panel.getByText('Loading load history…', { exact: true })).toHaveCount(0);
  expect(await page.evaluate(() => window.loadHistoryFlashes)).toBe(0);
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
  full = { ...full, revision: 'fourth' };
  await push();
  await expect.poll(() => detailReads).toBeGreaterThan(before);
  await panel.getByRole('button', { name: 'Close loads', exact: true }).click();
  release();
  await expect(panel).toHaveCount(0);
  await page.getByRole('button', { name: 'Loads Driver · Next load', exact: true }).click();
  await expect(history).toBeVisible();
  await expect(panel.getByRole('button', { name: 'NEXT · Current load', exact: true })).toBeVisible();
  await expect(destination).toBeEnabled();
  await panel.getByText('Unavailable loads (1)', { exact: true }).click();
  await expect(panel.getByText('DataTruck: Canceled', { exact: false })).toBeVisible();
  await sequence.getByRole('button', { name: 'COMPLETED-HERE · Completed load' }).click();
  await panel.getByRole('button', { name: 'Restore to queue · COMPLETED-HERE', exact: true }).click();
  await expect.poll(() => mutations).toBe(2);
  await expect(sequence.getByRole('button', { name: 'COMPLETED-HERE', exact: true })).toBeVisible();
  await expect(panel.getByRole('button', { name: 'NEXT · Current load', exact: true })).toBeVisible();
  await panel.getByRole('button', { name: 'Close loads', exact: true }).click();
  const advance = page.getByRole('button', { name: 'Loads Driver · Advance load', exact: true });
  const chooser = page.getByRole('dialog', { name: 'Next stop · Loads Driver' });
  await advance.click();
  await expect(chooser.getByRole('radio', { name: '100 First Street, Atlanta, GA' })).toBeVisible();
  await expect(chooser.getByRole('radio', { name: '200 Second Street, Atlanta, GA' })).toBeChecked();
  await chooser.getByRole('button', { name: 'Cancel' }).click();
  expect(mutations).toBe(2);
  await advance.click();
  await chooser.getByRole('button', { name: 'Apply destination' }).click();
  await expect(chooser).toHaveCount(0);
  await expect(advance).toHaveText('Dispatched');
  await expect(page.getByLabel('Loads Driver · Current load', { exact: true })).toHaveValue('NEXT');
  await advance.click();
  await expect(advance).toHaveText('Reserved');
  await expect(page.getByLabel('Loads Driver · Destination', { exact: true })).toHaveValue('Dallas, TX');
  await expect(chooser).toHaveCount(0);
  expect(mutations).toBe(4);
  // Proven prior work stays below Current without being called completed.
  const previous = { ...load('PREVIOUSLY-ACTIVE'), sourceStatus: 'Dispatched', previouslyActive: true, canRestoreToQueue: true };
  full = { ...full, earlier: [previous], revision: 'previous-history' };
  await push();
  await page.getByRole('button', { name: 'Loads Driver · Next load', exact: true }).click();
  const previousCard = panel.getByRole('button', { name: 'PREVIOUSLY-ACTIVE · Previously active load', exact: true });
  await expect(previousCard).toBeVisible();
  await expect(panel.locator('[data-queued-load="PREVIOUSLY-ACTIVE"]')).toHaveCount(0);
  await previousCard.click();
  await expect(panel.getByText('Previously worked and replaced on this board. Completion has not been confirmed.', { exact: true })).toBeVisible();
  await page.screenshot({ path: '../artifacts/previously-active-history.png', fullPage: true });
  await panel.getByLabel('Add next load', { exact: true }).fill('PREVIOUSLY-ACTIVE');
  await panel.getByRole('button', { name: 'Add next load', exact: true }).click();
  await expect(panel.getByRole('alert')).toHaveText('Open the load and use Restore to queue.');
  expect(mutations).toBe(4);
  await panel.getByRole('button', { name: 'Restore to queue · PREVIOUSLY-ACTIVE', exact: true }).click();
  await expect.poll(() => mutations).toBe(5);
  await expect(panel.locator('[data-queued-load="PREVIOUSLY-ACTIVE"]')).toBeVisible();
  await expect(previousCard).toHaveCount(0);
  await expect(panel.getByRole('button', { name: 'NEXT · Current load', exact: true })).toBeVisible();
  await panel.getByRole('button', { name: 'Close loads', exact: true }).click();
  await page.getByRole('button', { name: 'Loads Driver · Next load', exact: true }).click();
  await expect(panel.locator('[data-queued-load="PREVIOUSLY-ACTIVE"]')).toBeVisible();
  expect(errors).toEqual([]);
  console.log('Status Board loads passed: animated drag, keyboard reorder, completed/previously-active history restoration, live refresh, and multi-pickup progression.');
} finally {
  pending.splice(0).forEach(resolve => resolve());
  await browser.close();
  await new Promise(resolve => server.close(resolve));
}
