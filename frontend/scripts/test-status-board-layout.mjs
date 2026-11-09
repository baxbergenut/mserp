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
  truckId: '', truckUnit: `T${i}`, dispatcherId: 'dispatch', dispatcherName: 'Dispatch', dispatcherExtension: 202, phone: i === 0 ? '+1 (470) 334-4443' : i === 1 ? '+44 20 7946 0958' : '', location: null,
  city: i === 0 ? 'Louisville' : '', state: i === 0 ? 'Kentucky' : '' }));
let entries = drivers.map(d => ({ driverId: d.id, currentLoad: '', trailerNumber: '', trailerNote: '', status: 'ENROUTE', destination: '', eta: d.id === 'driver-0' ? '13:00 · 300 mi' : '', notes: '', homeTime: '', version: 1 }));
const grossEntries = ['9999.99', '10000.00', '11999.99', '12000.00'].map((originalRate, i) => ({
  driverId: drivers[i].id, date: '2026-10-12', slot: 1, loadNumber: `LOAD-${i}`, originalRate, driverRate: '6000.00', miles: i % 2 ? '10000' : '0',
}));
const loads = { current: null, week: [], next: [], earlier: [], hidden: [], unavailable: [], destinationSource: false, sourceDestination: '', stopKey: '', fromDate: '2026-10-12', revision: 'fixture', customOrder: false };
const errors = [];
let account = 'scroll';
let peers = [{ clientId: 'peer-client', userId: 'peer-user', userName: 'Peer dispatcher', driverId: drivers[0].id, column: 'currentLoad' }];
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
      '/auth/session': { user: { id: account, username: 'Scroll Test', permissions: ['driver_board.read', 'driver_board.write'] }, csrfToken: 'fixture' },
      '/driver-board': { weekStart: '2026-10-12', drivers, entries, grossEntries, grossTierMinimumCents: [1000000, 1200000], loads: Object.fromEntries(drivers.map(d => [d.id, loads])), eld: { configured: false } },
      '/driver-board/history': { items: [], nextCursor: 0 },
      '/driver-board/presence': peers,
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
  const grid = page.locator('[data-status-board-grid]');
  const header = column => grid.locator(`thead [data-board-column="${column}"]`);
  const columnOrder = () => grid.locator('thead th').evaluateAll(cells => cells.map(cell => cell.dataset.boardColumn));
  const defaults = await columnOrder();
  const dispatcherGroup = grid.locator('tr[data-dispatcher-id="dispatch"]');
  await expect(dispatcherGroup.getByText('Dispatch (202) - 100', { exact: true })).toBeVisible();
  await expect(grid.locator('tr[data-dispatcher-id=""]')).toContainText('Unassigned - 0');
  await page.getByLabel('Search Status Board').fill('Scroll Driver 99');
  await expect(grid.locator('tr[data-driver-id]')).toHaveCount(1);
  await expect(dispatcherGroup.getByText('Dispatch (202) - 100', { exact: true })).toBeVisible();
  await page.getByLabel('Search Status Board').fill('');
  await expect(grid.locator('tr[data-driver-id]')).toHaveCount(100);
  const autoOrder = dispatcherGroup.getByRole('switch', { name: 'Auto-order for Dispatch', exact: true });
  const assertAutoOrderColumn = async () => {
    await expect(dispatcherGroup.locator('[data-board-column="loadPlan"]')).toContainText('Auto-order');
    const column = await header('loadPlan').boundingBox();
    const cell = await dispatcherGroup.locator('[data-board-column="loadPlan"]').boundingBox();
    const toggle = await autoOrder.boundingBox();
    expect(Math.abs(cell.x - column.x)).toBeLessThan(1);
    expect(Math.abs(cell.width - column.width)).toBeLessThan(1);
    expect(Math.abs(toggle.x + toggle.width - cell.x - cell.width)).toBeLessThan(8);
  };
  await assertAutoOrderColumn();
  await header('loadPlan').press('ArrowLeft');
  await assertAutoOrderColumn();
  await header('loadPlan').press('ArrowRight');
  await expect.poll(columnOrder).toEqual(defaults);

  await expect(grid.locator('tr[data-driver-id="driver-0"] [data-board-column="phone"] button')).toHaveText('(470) 334-4443');
  await expect(grid.locator('tr[data-driver-id="driver-0"] [data-board-column="phone"] a')).toHaveAttribute('href', 'rcapp://r/call?number=%2B14703344443');
  await expect(grid.locator('tr[data-driver-id="driver-1"] [data-board-column="phone"]')).toHaveText('\u2014'); // Unsupported international values are not rewritten into US numbers.
  await page.context().grantPermissions(['clipboard-read', 'clipboard-write']);
  await grid.locator('tr[data-driver-id="driver-0"] [data-board-column="phone"] button').press('Enter');
  await expect.poll(() => page.evaluate(() => navigator.clipboard.readText())).toBe('+14703344443');
  const firstRow = grid.locator('tr[data-driver-id]').first();
  const grossCell = (i, column) => grid.locator(`tr[data-driver-id="driver-${i}"] [data-board-column="${column}"]`);
  const untieredPink = 'rgb(244, 204, 204)';
  const grossBackgrounds = [untieredPink, 'rgb(0, 255, 0)', 'rgb(0, 255, 0)', 'rgb(0, 255, 255)', untieredPink];
  for (const [i, color] of grossBackgrounds.entries()) {
    for (const column of ['originalGross', 'driverGross']) {
      await expect(grossCell(i, column)).toHaveCSS('background-color', color);
    }
  }
  await expect(grossCell(1, 'originalGross')).toHaveText('$10,000.00');
  await expect(grossCell(1, 'driverGross')).toHaveText('$6,000.00');
  await header("driver").scrollIntoViewIfNeeded();
  await page.locator('[data-scroll-key="driver-board"]').evaluate(area => { area.scrollLeft = 0; area.scrollTop = 0; });
  const writes = [];
  const trackWrites = request => {
    if (request.method() !== 'GET' && new URL(request.url()).pathname.startsWith('/api/driver-board')) writes.push(request.url());
  };
  page.on('request', trackWrites);
  await expect(header('driver')).toHaveCSS('cursor', 'grab');
  const driverHeader = await header('driver').boundingBox();
  const currentLoadHeader = await header('currentLoad').boundingBox();
  await page.mouse.move(driverHeader.x + 20, driverHeader.y + 15);
  await page.mouse.down();
  await page.mouse.move(currentLoadHeader.x + 18, driverHeader.y + 15, { steps: 8 });
  await expect(grid).toHaveAttribute('data-column-gesture', 'move');
  await expect.poll(() => firstRow.locator('[data-board-column="driver"]').evaluate(cell => getComputedStyle(cell).translate)).not.toBe('none');
  await expect.poll(columnOrder).toEqual(defaults); // The full grid stays mounted and ordered while dragging.
  await page.mouse.up();
  const reordered = [defaults[1], defaults[0], ...defaults.slice(2)];
  await expect.poll(columnOrder).toEqual(reordered);
  await expect.poll(() => firstRow.locator(':scope > *').evaluateAll(cells => cells.map(cell => cell.dataset.boardColumn))).toEqual(reordered);
  const currentLoad = firstRow.locator('[data-board-column="currentLoad"]');
  const peer = page.locator('[data-board-presence="Peer dispatcher"]');
  await expect(peer).toHaveAttribute('aria-label', /Scroll Driver 0.*Current load/);
  await expect.poll(async () => Math.abs((await peer.boundingBox()).x - (await currentLoad.boundingBox()).x)).toBeLessThan(1);

  const resizeEdge = header('driver').locator('span');
  const resizeBox = await resizeEdge.boundingBox();
  await page.mouse.move(resizeBox.x + 3, resizeBox.y + 12);
  await page.mouse.down();
  await page.mouse.move(resizeBox.x + 63, resizeBox.y + 12, { steps: 10 });
  await expect(grid).toHaveAttribute('data-column-gesture', 'resize');
  await expect.poll(async () => (await header('driver').boundingBox()).width).toBe(driverHeader.width + 60);
  await page.mouse.up();
  await expect(grid).not.toHaveAttribute('data-column-gesture', /./);
  expect(writes).toEqual([]);
  page.off('request', trackWrites);

  await page.reload();
  await expect.poll(columnOrder).toEqual(reordered);
  await expect.poll(async () => (await header('driver').boundingBox()).width).toBe(driverHeader.width + 60);
  await resizeEdge.dblclick();
  await expect.poll(async () => (await header('driver').boundingBox()).width).toBe(driverHeader.width);
  await currentLoad.click({ button: 'right' });
  await expect(page.getByLabel('History column')).toHaveValue('currentLoad');
  await page.getByRole('button', { name: 'Close history' }).click();

  await header('originalGross').press('ArrowLeft');
  await expect.poll(async () => (await columnOrder()).indexOf('originalGross')).toBe(3);
  const gross = await header('originalGross').boundingBox();
  const grossTotal = await grid.locator('tr[data-dispatcher-id="dispatch"] [data-board-column="originalGross"]').boundingBox();
  expect(Math.abs(grossTotal.x - gross.x)).toBeLessThan(1);
  await expect(page.getByRole('switch', { name: 'Auto-order for Dispatch', exact: true })).toBeEnabled();
  account = 'other-account';
  peers = [];
  await page.reload();
  await expect.poll(columnOrder).toEqual(defaults);

  account = 'scroll';
  await page.reload();
  await expect.poll(async () => (await columnOrder()).indexOf('originalGross')).toBe(3);
  page.on('request', trackWrites);
  await header('driver').press('Shift+ArrowRight');
  await expect.poll(async () => (await header('driver').boundingBox()).width).toBe(driverHeader.width + 12);
  const resetItem = page.getByRole('menuitem', { name: 'Reset column layout', exact: true });
  await header('truck').press('Shift+F10');
  await expect(resetItem).toBeFocused();
  await page.keyboard.press('Escape');
  await expect(resetItem).toBeHidden();
  await expect(header('truck')).toBeFocused();
  await header('status').click({ button: 'right' });
  await expect(resetItem).toBeVisible();
  await resetItem.click();
  await expect(resetItem).toBeHidden();
  await expect.poll(columnOrder).toEqual(defaults);
  await expect.poll(async () => (await header('driver').boundingBox()).width).toBe(driverHeader.width);
  expect(writes).toEqual([]);
  page.off('request', trackWrites);
  await page.reload();
  await expect.poll(columnOrder).toEqual(defaults);
  await expect.poll(async () => (await header('driver').boundingBox()).width).toBe(driverHeader.width);

  const resizePage = await browser.newPage({ viewport: { width: 2400, height: 900 } });
  await setup(resizePage);
  const scrollArea = resizePage.locator('[data-scroll-key="driver-board"]');
  const snapshot = () => scrollArea.evaluate(area => ({
    scroll: area.scrollLeft,
    headers: Object.fromEntries(Array.from(area.querySelectorAll('thead th'), th => {
      const { x, width } = th.getBoundingClientRect();
      return [th.dataset.boardColumn, { x, width }];
    })),
  }));
  const dragEdge = async (column, dx) => {
    const edge = await resizePage.locator(`thead [data-board-column="${column}"] span`).boundingBox();
    await resizePage.mouse.move(edge.x + 3, edge.y + 12);
    await resizePage.mouse.down();
    await resizePage.mouse.move(edge.x + 3 + dx, edge.y + 12, { steps: 8 });
  };
  // Zooming out or widening the window gives automatic columns spare width.
  await expect.poll(async () => (await snapshot()).headers.currentLoad.width).toBeGreaterThan(90);
  let before = await snapshot();
  await dragEdge('notes', 60);
  const notesDelta = async () => (await snapshot()).headers.notes.width - before.headers.notes.width;
  await expect.poll(notesDelta).toBe(60);
  await resizePage.mouse.up();
  await expect.poll(notesDelta).toBe(60);
  const after = await snapshot();
  expect(after.headers.notes.x).toBeCloseTo(before.headers.notes.x, 0);
  for (const column of defaults.filter(column => column !== 'notes')) {
    expect(after.headers[column].width).toBeCloseTo(before.headers[column].width, 0);
  }

  await resizePage.setViewportSize({ width: 1100, height: 900 });
  await expect.poll(async () => (await snapshot()).headers.currentLoad.width).toBe(90);
  await scrollArea.evaluate(area => { area.scrollLeft = area.scrollWidth; });
  before = await snapshot();
  await dragEdge('driverAddress', -60);
  const addressDelta = async () => (await snapshot()).headers.driverAddress.width - before.headers.driverAddress.width;
  await expect.poll(addressDelta).toBe(-60);
  await resizePage.mouse.up();
  await expect.poll(addressDelta).toBe(-60);
  const anchored = await snapshot();
  expect(anchored.scroll).toBe(before.scroll);
  expect(anchored.headers.driverAddress.x).toBeCloseTo(before.headers.driverAddress.x, 0);
  await scrollArea.evaluate(area => { area.scrollLeft = 0; });
  await expect.poll(() => scrollArea.evaluate(area => area.style.getPropertyValue('--board-resize-scroll-width'))).toBe('');
  await resizePage.close();

  // Typical addresses size the column; a single long address cannot stretch it.
  entries.slice(0, 10).forEach(entry => { entry.destination = 'Indianapolis, IN'; });
  await page.reload();
  const destinationColumnWidth = () => page.getByLabel('Scroll Driver 0 · Destination', { exact: true }).evaluate(el => el.closest('td').getBoundingClientRect().width);
  await expect.poll(destinationColumnWidth).toBeGreaterThanOrEqual(160);
  const typicalDestinationWidth = await destinationColumnWidth();
  entries[0].destination = '12345 Exceptionally Long Distribution Center Boulevard, Indianapolis, Indiana 46201';
  await page.reload();
  await expect.poll(destinationColumnWidth).toBe(typicalDestinationWidth);
  expect(await destinationColumnWidth()).toBeLessThanOrEqual(280);
  entries.slice(0, 10).forEach(entry => { entry.destination = ''; });
  await page.reload();
  await expect.poll(async () => (await page.locator('thead th').allTextContents()).slice(-2)).toEqual(['Home time', 'Driver address']);
  await expect(page.getByLabel('Scroll Driver 0 · Driver address', { exact: true })).toHaveText('Louisville, KY');
  await expect(page.getByLabel('Scroll Driver 1 · Driver address', { exact: true })).toHaveText('—');
  const sheetStatuses = ['READY', 'REST', 'COVERED', 'DISPATCHED', 'ENROUTE', 'RESERVED', 'VACATION', 'DEADHEAD', 'HOME', 'SHOP', 'STOP', 'In the YARD', 'Hold for Load', 'Left', 'Family issue', 'Issue with PU/Del', 'At PU', 'Health issue', 'At DEL', 'HOME (YARD)', 'NEW DRIVER', '34 RESET', 'ACCIDENT'];
  const status = page.getByLabel('Scroll Driver 0 · Status', { exact: true });
  expect(await status.locator('option').allTextContents()).toEqual(['—', ...sheetStatuses.map(s => s.toLowerCase().replace(/[a-z]/, c => c.toUpperCase()))]);
  const colors = { READY: [0, 0, 0], REST: [217, 217, 217], COVERED: [255, 255, 0], DISPATCHED: [251, 188, 4], ENROUTE: [61, 133, 198], RESERVED: [183, 225, 205], VACATION: [224, 102, 102], HOME: [255, 0, 0], SHOP: [255, 153, 0], STOP: [153, 0, 0], 'HOME (YARD)': [255, 0, 0] };
  for (const value of sheetStatuses) {
    if(value === "Left") continue; // Termination has its own permission-gated workflow.
    // Existing progress behavior derives RESERVED from an actual next load.
    if (value === 'RESERVED' || value === 'VACATION') {
      loads.next = value === 'RESERVED' ? [{ planId: 'next', number: 'NEXT', loadId: null, stops: [] }] : [];
      await page.reload();
    }
    await status.selectOption(value);
    if (["HOME","HOME (YARD)","VACATION"].includes(value)) { if(value!=="VACATION"){await page.getByLabel("Home start date",{exact:true}).fill("2026-10-23");await page.getByLabel("Back date",{exact:true}).fill("2026-10-27");} await page.getByRole("button",{name:"Apply dates",exact:true}).click(); }
    if (["SHOP","STOP","ACCIDENT"].includes(value)) { await page.getByLabel("Truck status reason",{exact:true}).fill("Reported equipment problem"); await page.getByRole("button",{name:"Apply status",exact:true}).click(); }
    await expect.poll(() => entries[0].status).toBe(value);
    await expect.poll(() => page.getByLabel('Scroll Driver 0 · Advance load', { exact: true }).evaluate(el => el.scrollWidth <= el.clientWidth + 1)).toBe(true);
    if (['HOME', 'HOME (YARD)', 'VACATION'].includes(value)) {
      await expect(page.getByLabel('Scroll Driver 0 · Destination from home address', { exact: true })).toHaveText('Louisville, KY');
      expect(entries[0].destination).toBe(''); // Displaying home never writes the dispatch destination.
    } else {
      await expect(page.getByLabel('Scroll Driver 0 · Destination', { exact: true })).toBeVisible();
    }
    const cell = status.locator('xpath=ancestor::td');
    await expect(cell).toHaveCSS('background-color', `rgb(${(colors[value] ?? [255, 255, 255]).join(', ')})`);
    await expect(cell).toHaveCSS('color', ['READY', 'SHOP'].includes(value) ? 'rgb(255, 255, 255)' : 'rgb(0, 0, 0)');
  }
  await page.reload();
  await expect(status).toHaveValue('ACCIDENT');
  // Home takes precedence over a retained load destination, including after reload.
  loads.destinationSource = true;
  loads.sourceDestination = 'Dallas, Texas, 75236';
  entries[0].status = 'VACATION';
  await page.reload();
  await expect(page.getByLabel('Scroll Driver 0 · Destination from home address', { exact: true })).toHaveText('Louisville, KY');
  await page.locator('[data-scroll-key="driver-board"]').evaluate(area => { area.scrollLeft = area.scrollWidth; });
  await page.getByLabel('Scroll Driver 0 · Driver address', { exact: true }).click();
  await page.screenshot({ path: join(tmpdir(), 'mserp-status-board-home-address.png') });
  await page.locator('[data-scroll-key="driver-board"]').evaluate(area => { area.scrollLeft = 0; });
  drivers[0].city = '';
  drivers[0].state = '';
  await page.reload();
  await expect(page.getByLabel('Scroll Driver 0 · Destination from home address', { exact: true })).toHaveText('—');
  drivers[0].state = 'KY';
  await page.reload();
  await expect(page.getByLabel('Scroll Driver 0 · Driver address', { exact: true })).toHaveText('KY');
  await expect(page.getByLabel('Scroll Driver 0 · Destination from home address', { exact: true })).toHaveText('KY');
  drivers[0].city = 'Lexington';
  await page.reload();
  await expect(page.getByLabel('Scroll Driver 0 · Destination from home address', { exact: true })).toHaveText('Lexington, KY');
  await status.selectOption('ACCIDENT');
  await page.getByLabel('Truck status reason', { exact: true }).fill('Reported equipment problem');
  await page.getByRole('button', { name: 'Apply status', exact: true }).click();
  await expect.poll(() => entries[0].status).toBe('ACCIDENT');
  await expect(page.getByLabel('Scroll Driver 0 · Destination', { exact: true })).toHaveValue('Dallas, TX');
  loads.destinationSource = false;
  entries[0].destination = 'Manual dispatch destination';
  entries[0].status = 'HOME';
  await page.reload();
  await expect(page.getByLabel('Scroll Driver 0 · Destination from home address', { exact: true })).toHaveText('Lexington, KY');
  await status.selectOption('READY');
  await expect.poll(() => entries[0].status).toBe('READY');
  await expect(page.getByLabel('Scroll Driver 0 · Destination', { exact: true })).toHaveValue('Manual dispatch destination');
  entries[0].status = 'ACCIDENT';
  await page.reload();
  await page.getByRole('button', { name: 'Status filter', exact: true }).click();
  const statusChoices = page.getByRole('group', { name: 'Status choices', exact: true });
  expect(await statusChoices.locator('label').allTextContents()).toEqual(['No status', ...sheetStatuses.map(s => s.toLowerCase().replace(/[a-z]/, c => c.toUpperCase()))]);
  await statusChoices.getByRole('button', { name: 'Clear all', exact: true }).click();
  await statusChoices.getByText('Accident', { exact: true }).click();
  await expect(page.locator('tr[data-driver-id]')).toHaveCount(1);
  await statusChoices.getByRole('button', { name: 'Select all', exact: true }).click();
  await page.getByRole('button', { name: 'Status filter', exact: true }).click();
  await expect(page.locator('tr[data-driver-id]')).toHaveCount(100);
  // Old values remain visible and disabled, allowing unrelated autosaves.
  entries[0].status = 'LOAD CANCELLED';
  await page.reload();
  await expect(status).toHaveValue('LOAD CANCELLED');
  await expect(status.locator('option[value="LOAD CANCELLED"]')).toBeDisabled();
  await status.selectOption('ENROUTE');
  await expect.poll(() => entries[0].status).toBe('ENROUTE');
  await page.screenshot({ path: join(tmpdir(), 'mserp-driver-sheet-statuses.png') });
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
  // Every row uses the same horizontal coordinates after scrolling, including
  // dispatcher totals that previously slid underneath the pinned driver cells.
  const alignment = await page.evaluate(() => {
    const table = document.querySelector('[data-status-board-grid]');
    const headings = table.querySelector('thead tr').children;
    const driver = table.querySelector('tr[data-driver-id]').children;
    const group = table.querySelector('tr[data-dispatcher-id="dispatch"]').children;
    return {
      columns: [...headings].map((heading, i) => Math.abs(heading.getBoundingClientRect().left - driver[i].getBoundingClientRect().left)),
      groupLeft: Math.abs(group[0].getBoundingClientRect().left - driver[0].getBoundingClientRect().left),
      totals: [4, 5].map((i, j) => Math.abs(group[j + 1].getBoundingClientRect().left - headings[i].getBoundingClientRect().left)),
    };
  });
  expect(Math.max(...alignment.columns, alignment.groupLeft, ...alignment.totals)).toBeLessThan(1);
  await page.setViewportSize({ width: 1200, height: 650 });
  await main.evaluate(e => { e.scrollTop = e.scrollHeight; });
  await expect.poll(async () => Math.abs((await positions()).header - 9)).toBeLessThan(2);
  await page.screenshot({ path: join(tmpdir(), 'mserp-status-board-docked.png') });
  const mobile = await browser.newPage({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true });
  await setup(mobile);
  await expect(mobile.getByRole('button', { name: 'Expand sidebar', exact: true })).toBeVisible();
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
  await page.setViewportSize({ width: 2400, height: 900 });
  await page.locator('[data-scroll-key="driver-board"]').evaluate(area => { area.scrollLeft = 0; area.scrollTop = 0; });
  await page.screenshot({ path: join(tmpdir(), 'mserp-status-board-header-layout.png') });
  expect(errors).toEqual([]);
  console.log('Status Board layout passed: home city/state column and destination precedence, missing/partial/updated addresses, restored load/manual destinations, 23 sheet statuses/colors, save/reload/filter, legacy preservation, persisted ETA, page/table scrolling, horizontal overflow, resize and touch.');
} finally { await browser.close(); await new Promise(resolve => server.close(resolve)); }
