// Fixture-only layout/filter checks; no database or upstream writes.
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
const week = '2026-10-12';
const bobId = '00000000-0000-4000-8000-000000000002';
const drivers = Array.from({ length: 14 }, (_, i) => ({ id: i === 1 ? bobId : `driver-${i}`, fullName: i === 0 ? 'Alice Layout' : i === 1 ? 'Bob Layout' : `Driver ${i}`, active: true,
  truckId: `truck-${i}`, truckUnit: `T${i}`, dispatcherId: i === 0 ? 'a' : i === 13 ? '' : 'b', dispatcherName: i === 0 ? 'Alpha' : i === 13 ? 'Unassigned' : 'Bravo' }));
const entry = (id, slot, originalRate, driverRate, miles) => ({ driverId: id, date: week, slot, deleted: false, dayStatus: '', loadNumber: `LOAD-${id}-${slot}`, loadRecordId: null,
  originalRate, driverRate, miles, enteredOriginalRate: originalRate, enteredMiles: miles, version: 1, duplicate: false });
let entries = [entry('driver-0', 0, '1800.00', '1000.00', '500.00'), entry('driver-0', 1, '400.00', '350.35', '25.60'), entry(bobId, 0, '500.00', '400.00', '0.00')];
const errors = [];
async function fixtures(page) {
  page.on('pageerror', error => errors.push(error.message));
  await page.clock.setFixedTime(new Date('2026-10-16T14:00:00Z'));
  await page.route('**/api/**', async route => {
    const req = route.request(), url = new URL(req.url()), path = url.pathname.replace('/lookups/','/').slice(4);
    if (req.method() === 'PUT' && path === '/gross-board') {
      const committed = req.postDataJSON().entries.map(e => ({ ...e, version: e.version + 1 }));
      await new Promise(resolve => setTimeout(resolve, 150));
      entries = [...entries.filter(e => !committed.some(c => c.driverId === e.driverId && c.date === e.date && c.slot === e.slot)), ...committed];
      return route.fulfill({ json: committed });
    }
    const data = {
      '/auth/session': { user: { id: 'layout', username: 'Layout', permissions: ['board.read', 'board.write', 'drivers.read','trucks.read','dispatchers.read','updaters.read','investors.read', 'loads.read'] }, csrfToken: 'fixture' },
      '/gross-board': { weekStart: url.searchParams.get('weekStart') ?? week, drivers, entries, balances: [] },
      '/gross-board/loads': [], '/tasks/count': { count: 0 }, '/sidebar-counts': {},
    };
    await route.fulfill({ json: data[path] ?? [] });
  });
}

try {
  const page = await browser.newPage({ viewport: { width: 1800, height: 1000 } });
  await fixtures(page);
  await page.goto(`${base}/gross-board`);
  const area = page.getByRole('region', { name: 'Weekly gross board', exact: true });
  const rows = area.locator('tbody > tr').filter({ has: page.locator('th[scope="row"]') });
  await expect(rows).toHaveCount(14);
  await expect(page.getByText('Green = confirmed load', { exact: false })).toHaveCount(0);
  expect(await area.getByRole('columnheader').allTextContents()).toEqual(['Driver', 'Truck', '', 'Mon10/12', 'Tue10/13', 'Wed10/14', 'Thu10/15', 'Fri10/16', 'Sat10/17', 'Sun10/18', 'Total gross', 'Total miles', 'RPM', 'Driver gross', 'Rate balance']);
  await expect(page.getByLabel('Total gross RPM for Alice Layout', { exact: true })).toHaveText('$4.19');
  await expect(page.getByLabel('Total gross RPM for Bob Layout', { exact: true })).toHaveText('—');
  const alice = rows.filter({ hasText: 'Alice Layout' });
  await expect(alice.locator('[data-gross-field=driverTotal]')).toHaveText('$1,350.35');
  await expect(alice.locator('[data-gross-field=milesTotal]')).toHaveText('525.60');
  await expect(alice.locator('td').last()).toContainText('+$849.65');
  const alpha = area.getByRole('row', { name: 'Alpha totals', exact: true });
  await expect(alpha.getByRole('cell').nth(1)).toHaveText('525.60');
  await expect(alpha.getByRole('cell').last()).toHaveText('+$849.65');
  await expect(alpha.getByRole('cell').nth(2)).toHaveText('$4.19');
  await expect(alpha.getByRole('cell').nth(3)).toHaveText('$1,350.35');
  const footer = area.locator('tfoot td');
  await expect(footer.nth(2)).toHaveText('$5.14');
  await expect(footer.nth(3)).toHaveText('$1,750.35');
  await expect(alice.locator('[data-gross-field=driverTotal]')).not.toContainText('RPM');
  await expect(footer.nth(1)).toHaveText('525.60');
  await expect(footer.nth(4)).toHaveText('+$949.65');
  await page.getByRole('button', { name: 'Rate balance for Alice Layout', exact: true }).click();
  await expect(page.getByRole('dialog', { name: 'Alice Layout · Rate balance' })).toBeVisible();
  await page.getByRole('button', { name: 'Close', exact: true }).click();

  const filter = page.getByRole('button', { name: 'Dispatcher filter', exact: true });
  await filter.click();
  const choices = page.getByRole('group', { name: 'Dispatcher choices', exact: true });
  await choices.getByRole('checkbox', { name: 'Bravo', exact: true }).uncheck();
  await expect(rows).toHaveCount(2); // Alpha + Unassigned stay selected together.
  await expect(choices.getByRole('checkbox', { name: 'Alpha', exact: true })).toBeChecked();
  await expect(choices.getByRole('checkbox', { name: 'Unassigned', exact: true })).toBeChecked();
  await page.keyboard.press('Escape');
  const search = page.getByPlaceholder('Search drivers…');
  await search.fill('Alice');
  await expect(rows).toHaveCount(1);
  await expect(page.getByText('Total gross', { exact: true }).first().locator('..').getByText('$2,200.00', { exact: true })).toBeVisible();
  await search.fill('Bob');
  await expect(page.getByText('No drivers match the selected filters.', { exact: true })).toBeVisible();
  await search.fill('');
  await filter.click(); await choices.getByRole('button', { name: 'Clear all', exact: true }).click();
  await expect(page.getByText('No drivers match the selected filters.', { exact: true })).toBeVisible();
  await choices.getByRole('checkbox', { name: 'Bravo', exact: true }).check();
  await choices.getByRole('checkbox', { name: 'Alpha', exact: true }).check();
  await expect(rows).toHaveCount(13);
  await page.keyboard.press('Escape');
  await page.reload(); await expect(rows).toHaveCount(13);
  await filter.click(); await choices.getByRole('button', { name: 'Select all', exact: true }).click();
  await page.keyboard.press('Escape'); await expect(rows).toHaveCount(14);

  // Editing a single-load day updates the weekly RPM through delayed autosave.
  const bobMiles = page.getByLabel(`Bob Layout, ${week}, miles`, { exact: true });
  await bobMiles.click(); await page.keyboard.type('200'); await page.keyboard.press('Enter');
  await expect(page.getByLabel('Total gross RPM for Bob Layout', { exact: true })).toHaveText('$2.50');
  await expect.poll(() => entries.find(e => e.driverId === bobId).miles).toBe('200');
  await expect(page.getByRole('status').filter({ hasText: 'All changes saved' })).toBeVisible();

  const main = page.getByRole('main');
  const pinDistance = () => area.locator('thead').evaluate(header => header.getBoundingClientRect().top - header.closest('main').getBoundingClientRect().top);
  // Page first, then rows: the header and page stop at the handoff.
  await area.evaluate(el => { el.scrollTop = 0; });
  await main.evaluate(el => { el.scrollTop = 0; });
  await page.mouse.move(700, 500); await page.mouse.wheel(0, 40);
  await expect.poll(() => main.evaluate(el => el.scrollTop)).toBe(40);
  expect(await area.evaluate(el => el.scrollTop)).toBe(0);
  await page.mouse.wheel(0, 600);
  await expect.poll(pinDistance).toBeCloseTo(8, 0);
  const pinnedPage = await main.evaluate(el => el.scrollTop);
  const firstInner = await area.evaluate(el => el.scrollTop);
  expect(firstInner).toBeGreaterThan(0);
  await page.mouse.wheel(0, 200);
  await expect.poll(() => area.evaluate(el => el.scrollTop)).toBe(firstInner + 200);
  expect(await main.evaluate(el => el.scrollTop)).toBe(pinnedPage);
  await expect.poll(pinDistance).toBeCloseTo(8, 0);
  await page.mouse.wheel(0, -100);
  await expect.poll(() => area.evaluate(el => el.scrollTop)).toBe(firstInner + 100);
  expect(await main.evaluate(el => el.scrollTop)).toBe(pinnedPage);
  expect(await page.getByText('Total gross', { exact: true }).first().evaluate(el => el.getBoundingClientRect().bottom < el.closest('main').getBoundingClientRect().top)).toBe(true);
  expect(await area.locator('thead').evaluate(el => { const r = el.getBoundingClientRect(); return !!document.elementFromPoint(r.left + 30, r.top + 15)?.closest('thead'); })).toBe(true);
  await page.screenshot({ path: join(tmpdir(), 'mserp-gross-board-layout-scrolled.png') });
  // Going up consumes table scroll before it reveals the cards.
  await page.mouse.wheel(0, -(firstInner + 160));
  await expect.poll(() => area.evaluate(el => el.scrollTop)).toBe(0);
  await expect.poll(() => main.evaluate(el => el.scrollTop)).toBe(pinnedPage - 60);
  await page.mouse.wheel(0, 500);
  await expect.poll(pinDistance).toBeCloseTo(8, 0);
  // Keyboard paging follows the same handoff and can reach the final totals.
  await area.focus(); await page.keyboard.press('End');
  await expect.poll(() => area.evaluate(el => el.scrollTop + el.clientHeight - el.scrollHeight)).toBe(0);
  expect(await main.evaluate(el => el.scrollTop)).toBe(pinnedPage);
  await page.keyboard.press('Home');
  await expect.poll(() => area.evaluate(el => el.scrollTop)).toBe(0);
  await expect.poll(() => main.evaluate(el => el.scrollTop)).toBe(0);
  await page.keyboard.press('PageDown');
  await expect.poll(pinDistance).toBeCloseTo(8, 0);
  expect(await area.evaluate(el => el.scrollTop)).toBeGreaterThan(0);
  // Horizontal scrolling keeps both frozen columns and the real header aligned.
  await area.evaluate(el => { el.scrollLeft = 400; });
  const columnLefts = () => area.evaluate(el => {
    const header = el.querySelector('thead tr').children;
    const row = el.querySelector('tbody th[scope="row"]').parentElement.children;
    return [0, 1].map(i => Math.abs(header[i].getBoundingClientRect().left - row[i].getBoundingClientRect().left));
  });
  expect((await columnLefts()).every(distance => distance < 2)).toBe(true);
  await page.setViewportSize({ width: 900, height: 700 });
  await expect.poll(pinDistance).toBeCloseTo(8, 0);
  expect((await columnLefts()).every(distance => distance < 2)).toBe(true);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);

  // Touch gestures use the same page-first handoff on a narrow viewport.
  await area.focus(); await page.keyboard.press('Home');
  await area.evaluate(el => {
    const send = (type, y) => {
      const touch = new Touch({ identifier: 1, target: el, clientX: 250, clientY: y });
      el.dispatchEvent(new TouchEvent(type, { bubbles: true, cancelable: true, touches: [touch], changedTouches: [touch] }));
    };
    send('touchstart', 1200); send('touchmove', 1160);
  });
  await expect.poll(() => main.evaluate(el => el.scrollTop)).toBe(40);
  expect(await area.evaluate(el => el.scrollTop)).toBe(0);
  await area.evaluate(el => {
    const touch = new Touch({ identifier: 1, target: el, clientX: 250, clientY: 160 });
    el.dispatchEvent(new TouchEvent('touchmove', { bubbles: true, cancelable: true, touches: [touch], changedTouches: [touch] }));
  });
  await expect.poll(pinDistance).toBeCloseTo(8, 0);
  expect(await area.evaluate(el => el.scrollTop)).toBeGreaterThan(0);
  await page.setViewportSize({ width: 390, height: 844 });
  await area.evaluate(el => { el.scrollLeft = 400; });
  await expect.poll(pinDistance).toBeCloseTo(8, 0);
  expect((await columnLefts()).every(distance => distance < 2)).toBe(true);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);

  // Browser Back restores the page scroll and horizontal table position.
  const previousInner = await area.evaluate(el => el.scrollTop);
  await page.goto(`${base}/gross-board?returnTest=1`);
  await expect(area).toBeVisible();
  await page.goBack();
  await expect(area).toBeVisible();
  await expect.poll(() => main.evaluate(el => el.scrollHeight - el.clientHeight - el.scrollTop)).toBe(0);
  await expect.poll(() => area.evaluate(el => el.scrollLeft)).toBe(400);
  await expect.poll(() => area.evaluate(el => el.scrollTop)).toBe(previousInner);
  await expect.poll(pinDistance).toBeCloseTo(8, 0);
  await area.focus(); await page.keyboard.press('Home');
  await expect.poll(() => main.evaluate(el => el.scrollTop)).toBe(0);
  await expect(page.getByText('Total gross', { exact: true }).first()).toBeVisible();
  await page.setViewportSize({ width: 1800, height: 1000 });
  await area.evaluate(el => { el.scrollLeft = 0; });
  // Arrow navigation keeps focused fields below the pinned header.
  const cell = page.getByLabel(`Driver 3, ${week}, total gross`, { exact: true }).locator('..');
  await cell.click();
  await cell.evaluate(el => {
    const main = el.closest('main');
    const header = el.closest('table').tHead;
    main.scrollTop += el.getBoundingClientRect().top - main.getBoundingClientRect().top - header.offsetHeight - 12;
  });
  await page.keyboard.press('ArrowUp');
  await expect.poll(() => area.evaluate(el => document.activeElement.getBoundingClientRect().top - el.querySelector('thead').getBoundingClientRect().bottom)).toBeGreaterThanOrEqual(0);
  await area.focus(); await page.keyboard.press('Home');
  await page.screenshot({ path: join(tmpdir(), 'mserp-gross-board-layout-top.png') });

  // A legacy single selection restores; explicit payroll links reveal all drivers.
  const legacy = await browser.newPage({ viewport: { width: 1800, height: 1000 } });
  await fixtures(legacy);
  await legacy.addInitScript(() => sessionStorage.setItem('mserp-navigation-v1:layout:/gross-board:view:page:dispatcher', JSON.stringify('a')));
  await legacy.goto(`${base}/gross-board`);
  await expect(legacy.getByLabel('Total gross RPM for Alice Layout', { exact: true })).toBeVisible();
  await expect(legacy.getByLabel('Total gross RPM for Bob Layout', { exact: true })).toHaveCount(0);
  expect(await legacy.getByRole('main').evaluate(el => el.scrollHeight <= el.clientHeight)).toBe(true);
  expect(await legacy.getByRole('region', { name: 'Weekly gross board' }).evaluate(el => el.scrollHeight <= el.clientHeight)).toBe(true);
  await legacy.getByRole('button', { name: 'Dispatcher filter', exact: true }).click();
  await expect(legacy.getByRole('checkbox', { name: 'Alpha', exact: true })).toBeChecked();
  await expect(legacy.getByRole('checkbox', { name: 'Bravo', exact: true })).not.toBeChecked();
  await legacy.goto(`${base}/gross-board?driverId=${bobId}&date=${week}&slot=0&loadNumber=LOAD-${bobId}-0&from=driver-pay`);
  await expect(legacy.getByLabel('Total gross RPM for Alice Layout', { exact: true })).toHaveCount(1);
  await expect(legacy.getByLabel('Total gross RPM for Bob Layout', { exact: true })).toHaveCount(1);
  await legacy.close();
  expect(errors).toEqual([]);
  console.log('Gross Board layout checks passed: checkbox filters, legacy views, filtered totals, weekly RPM/autosave, column order, balance history, two-stage wheel/keyboard scrolling, frozen columns, resize, and Back restoration.');
} catch (error) {
  const page = browser.contexts()[0]?.pages()[0];
  if (page) console.log(await page.evaluate(() => {
    const main = document.querySelector('main'), area = document.querySelector('[data-scroll-key="gross-board"]');
    return { main: main && { height: main.clientHeight, scroll: main.scrollTop, total: main.scrollHeight }, area: area && { top: area.getBoundingClientRect().top, height: area.clientHeight, scroll: area.scrollTop, total: area.scrollHeight, max: area.style.maxHeight }, header: area?.querySelector('thead')?.getBoundingClientRect().toJSON() };
  }));
  throw error;
} finally { await browser.close(); await new Promise(resolve => server.close(resolve)); }
