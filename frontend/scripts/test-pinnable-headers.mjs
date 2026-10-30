// Browser integration checks with isolated API fixtures; no production writes.
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
const week = '2026-10-26';
const drivers = Array.from({ length: 60 }, (_, i) => ({ id: `driver-${i}`, fullName: `Driver ${i}`, active: true,
  truckId: `truck-${i}`, truckUnit: `T${i}`, dispatcherId: 'a', dispatcherName: 'Alpha', phone: '', location: null }));
drivers[0].assignments = Array.from({ length: 7 }, (_, i) => ({ date: `2026-${i < 6 ? `10-${26 + i}` : '11-01'}`, dispatcherId: i ? 'b' : 'a', dispatcherName: i ? 'Bravo' : 'Alpha', truckId: 'truck-0', truckUnit: 'T0' }));
const entry = (driverId, date, slot, loadNumber, extra = {}) => ({ driverId, date, slot, loadNumber, deleted: false, dayStatus: '', loadRecordId: null, originalRate: '100', driverRate: '90', miles: '50', enteredOriginalRate: '100', enteredMiles: '50', version: 1, ...extra });
const grossEntries = [entry('driver-0', week, 0, 'FIRST-100'), entry('driver-0', week, 1, 'AbC-00987'),
  entry('driver-0', '2026-10-27', 0, 'SECOND-200'), entry('driver-1', week, 0, 'REMOVED-999', { deleted: true }),
  entry('driver-1', week, 1, 'STATUS-999', { dayStatus: 'HOME' }),
  entry('driver-0', '2026-10-27', 1, 'TRANSFER-300', { transferVersion: 1, creditedDispatcherId: 'a', creditedDispatcherName: 'Alpha' })];
const loads = { current: null, week: [], next: [], earlier: [], hidden: [], unavailable: [], destinationSource: false, sourceDestination: '', stopKey: '', fromDate: week, revision: 'fixture', customOrder: false };
const errors = [];
async function fixtures(page) {
  page.on('pageerror', error => errors.push(error.message));
  await page.clock.setFixedTime(new Date('2026-10-30T14:00:00Z'));
  await page.route('**/api/**', async route => {
    const path = new URL(route.request().url()).pathname.slice(4);
    let data = [];
    if (path === '/auth/session') data = { user: { id: 'pins', username: 'Pins', permissions: ['board.read', 'driver_board.read', 'driver_pay.read', 'investor_pay.read', 'drivers.read', 'investors.read'], expenseCategoryAccess: [], theme: 'default' }, csrfToken: 'fixture' };
    else if (path === '/auth/birthday') data = { eligible: false, serverTime: new Date().toISOString() };
    else if (path === '/gross-board') data = { weekStart: week, drivers, entries: grossEntries, balances: [] };
    else if (path === '/driver-board') data = { weekStart: week, drivers, entries: drivers.map(d => ({ driverId: d.id, currentLoad: '', trailerNumber: '', trailerNote: '', status: 'READY', destination: '', eta: '', notes: '', homeTime: '', version: 1 })), grossEntries: [], loads: Object.fromEntries(drivers.map(d => [d.id, loads])), eld: { configured: false } };
    else if (path === '/driver-pay' || path === '/investor-pay') data = { weekStart: week, revision: 'fixture', drivers: drivers.map(d => ({ ...d, ...(path === '/investor-pay' ? { investorId: `owner-${d.id}`, operatingDrivers: [{ id: d.id, name: d.fullName }] } : {}), payType: 'cpm', payRate: '0.75', employmentType: 'unassigned', loads: [], fuelTotal: '0', tollTotal: '0', summary: { loads: 0, missingFees: 0, review: 0, fee: '0', payable: '0' }, edits: { driverId: d.id, weekStart: week, version: 0, comments: {}, adjustments: [], fuelOverride: null, tollOverride: null } })) };
    await route.fulfill({ json: data });
  });
}

try {
  const page = await browser.newPage({ viewport: { width: 1600, height: 1000 } });
  await fixtures(page);
  for (const path of ['/driver-board', '/gross-board', '/accounting/driver-pay', '/accounting/investor-pay']) {
    await page.goto(base + path);
    const header = page.locator('[data-pinned-page-header]');
    const main = page.getByRole('main');
    await expect(page.getByRole('button', { name: 'Pin header cards and filters', exact: true })).toHaveAttribute('aria-pressed', 'false');
    const table = page.locator('main table').first();
    await expect(table.locator('tbody tr').last()).toBeAttached();
    await page.getByRole('button', { name: 'Pin header cards and filters', exact: true }).click();
    await expect(header).toHaveAttribute('data-pinned-page-header', 'true');
    // Measure from rest: checking only the final sticky position misses the
    // initial slide through the shell's padding and the board handoff gap.
    await main.evaluate(el => { el.scrollTop = 0; });
    await expect(header).toHaveCSS('top', '24px');
    const headerTop = await header.evaluate(el => el.getBoundingClientRect().top);
    const tableTop = await table.locator('thead').evaluate(el => el.getBoundingClientRect().top);
    const area = path === '/driver-board' ? page.locator('[data-scroll-key="driver-board"]') : path === '/gross-board' ? page.locator('[data-scroll-key="gross-board"]') : main;
    await area.hover(); await page.mouse.wheel(0, 10);
    await expect.poll(() => area.evaluate(el => el.scrollTop)).toBeGreaterThan(0);
    expect(await header.evaluate(el => el.getBoundingClientRect().top)).toBe(headerTop);
    if (area !== main) {
      expect(await main.evaluate(el => el.scrollTop)).toBe(0);
      expect(await table.locator('thead').evaluate(el => el.getBoundingClientRect().top)).toBe(tableTop);
    }
    await page.mouse.wheel(0, 1800);
    await expect.poll(() => area.evaluate(el => el.scrollTop)).toBeGreaterThan(0);
    expect(await header.evaluate(el => el.getBoundingClientRect().top)).toBe(headerTop);
    if (area !== main) {
      expect(await table.locator('thead').evaluate(el => el.getBoundingClientRect().top - el.closest('main').querySelector('[data-pinned-page-header]').getBoundingClientRect().bottom)).toBeGreaterThanOrEqual(7);
      await area.evaluate(el => { el.scrollTop = el.scrollHeight; });
      await expect(table.locator('tbody tr').last()).toBeInViewport();
    }
    await area.hover(); await page.mouse.wheel(0, -100000);
    await expect.poll(() => area.evaluate(el => el.scrollTop)).toBe(0);
    expect(await header.evaluate(el => el.getBoundingClientRect().top)).toBe(headerTop);
    if (path === '/gross-board' || path === '/accounting/driver-pay') await page.screenshot({ path: join(tmpdir(), `mserp-pinned-${path.split('/').at(-1)}.png`) });
    await page.reload();
    await expect(page.getByRole('button', { name: 'Unpin header cards and filters', exact: true })).toHaveAttribute('aria-pressed', 'true');
    // Keep rows usable and all controls reachable on a narrow screen.
    await page.setViewportSize({ width: 390, height: 844 });
    await expect.poll(() => header.evaluate(el => el.clientHeight <= el.closest('main').clientHeight / 2 + 1)).toBe(true);
    await header.evaluate(el => { el.scrollTop = el.scrollHeight; });
    if (path === '/gross-board') await page.screenshot({ path: join(tmpdir(), 'mserp-pinned-narrow.png') });
    await page.getByRole('button', { name: 'Unpin header cards and filters', exact: true }).click();
    await expect(header).toHaveAttribute('data-pinned-page-header', 'false');
    await page.reload();
    await expect(page.getByRole('button', { name: 'Pin header cards and filters', exact: true })).toHaveAttribute('aria-pressed', 'false');
    await page.setViewportSize({ width: 1600, height: 1000 });
  }

  await page.goto(base + '/gross-board');
  const search = page.getByPlaceholder('Search drivers or load numbers…');
  const rows = page.locator('[data-gross-field="driver"]');
  await search.fill('  abc-009  '); // Second daily slot, partial and case insensitive.
  await expect(rows).toHaveCount(1);
  await expect(rows).toHaveText(['Driver 0']);
  await expect(page.getByRole('row', { name: 'Alpha totals', exact: true })).toBeVisible();
  await expect(page.getByRole('row', { name: 'Bravo totals', exact: true })).toHaveCount(0);
  await search.fill('SECOND-200');
  await expect(rows).toHaveCount(1);
  await expect(page.getByRole('row', { name: 'Bravo totals', exact: true })).toBeVisible();
  await search.fill('TRANSFER-300');
  await expect(rows).toHaveCount(1);
  await expect(page.getByRole('row', { name: 'Alpha totals', exact: true })).toBeVisible();
  for (const value of ['REMOVED-999', 'STATUS-999', 'missing']) {
    await search.fill(value);
    await expect(page.getByText('No drivers match the selected filters.', { exact: true })).toBeVisible();
    await expect(search).toBeVisible();
  }
  await search.fill('abc-00987');
  await page.getByRole('button', { name: 'Dispatcher filter', exact: true }).click();
  const choices = page.getByRole('group', { name: 'Dispatcher choices', exact: true });
  await choices.getByRole('checkbox', { name: 'Alpha', exact: true }).uncheck();
  await expect(rows).toHaveCount(0);
  await choices.getByRole('button', { name: 'Select all', exact: true }).click();
  await page.keyboard.press('Escape');
  await expect(rows).toHaveCount(1);
  await page.reload();
  await expect(search).toHaveValue('abc-00987');
  await expect(rows).toHaveCount(1);
  await search.fill('');
  await expect(rows).toHaveCount(61);
  expect(errors).toEqual([]);
  console.log('Header pinning passed on all four pages: scrolling, table clearance, last rows, narrow screens, independent preferences and reload persistence. Gross Board load search passed multi-slot, case/partial, dated dispatcher, transfer, removed/status, combined filters and clear checks.');
} finally {
  await browser.close(); server.closeAllConnections();
  await new Promise(resolve => server.close(resolve));
}
