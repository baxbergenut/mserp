// Fixture-only regressions with database-style decimal formatting and delayed saves.
import { chromium, expect } from '@playwright/test';
import { createServer } from 'node:http';
import { readFile } from 'node:fs/promises';
import { extname } from 'node:path';

const server = createServer(async (req, res) => {
  try {
    const path = new URL(req.url, 'http://localhost').pathname;
    const file = path === '/' ? 'index.html' : extname(path) ? path.slice(1) : `${path.slice(1)}.html`;
    const body = await readFile(new URL(`../out/${file}`, import.meta.url));
    res.writeHead(200, { 'Content-Type': { '.html': 'text/html', '.js': 'application/javascript', '.css': 'text/css' }[extname(file)] ?? 'application/octet-stream' });
    res.end(body);
  } catch { res.writeHead(404); res.end(); }
});
await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
const browser = await chromium.launch();
const week = '2026-10-12', driverId = 'number-driver';
const blank = (date, slot = 0) => ({ driverId, date, slot, deleted: false, dayStatus: '', loadNumber: '', loadRecordId: null, originalRate: '', driverRate: '', miles: '', enteredOriginalRate: '', enteredMiles: '', version: 0, duplicate: false });
let entries = [blank(week), blank('2026-10-13'), blank('2026-10-14'), blank('2026-10-14', 1)];
const source = { id: 67, loadNumber: 'SERVER-67', originalRate: '670.00', miles: '123.45' };
const selected = { id: 89, loadNumber: 'SELECT-89', originalRate: '890.00', miles: '234.56' };
const errors = [], invalidWrites = [];
const fields = ['originalRate', 'driverRate', 'miles', 'enteredOriginalRate', 'enteredMiles'];
try {
  const page = await browser.newPage({ viewport: { width: 1800, height: 1000 } });
  page.on('pageerror', error => errors.push(error.message));
  await page.route('**/api/**', async route => {
    const req = route.request(), url = new URL(req.url()), path = url.pathname.slice(4);
    if (req.method() === 'PUT') {
      const edits = req.postDataJSON().entries;
      if (edits.some(e => fields.some(f => e[f] && !/^-?\d{1,10}(\.\d{1,2})?$/.test(e[f])))) {
        invalidWrites.push(edits);
        return route.fulfill({ status: 400, json: { error: 'rates and miles must be decimal numbers with at most 10 whole digits and 2 decimal places' } });
      }
      const committed = edits.map(e => {
        const next = { ...e, version: e.version + 1 };
        if (e.loadNumber === source.loadNumber) {
          next.loadRecordId = source.id;
          next.originalRate ||= source.originalRate;
          next.driverRate ||= next.originalRate;
          next.miles ||= source.miles;
        }
        for (const field of fields) if (next[field]) next[field] = Number(next[field]).toFixed(2);
        return next;
      });
      await new Promise(resolve => setTimeout(resolve, 150));
      entries = [...entries.filter(e => !committed.some(c => c.date === e.date && c.slot === e.slot)), ...committed];
      return route.fulfill({ json: committed });
    }
    const fixtures = {
      '/auth/session': { user: { id: 'numbers', username: 'Numbers', permissions: ['board.read', 'board.write'] }, csrfToken: 'fixture' },
      '/gross-board': { weekStart: week, drivers: [{ id: driverId, fullName: 'Number Driver', dispatcherId: '', dispatcherName: 'Unassigned', active: true }], entries, balances: [] },
      '/gross-board/loads': url.searchParams.get('search') === selected.loadNumber ? [selected] : [],
      '/tasks/count': { count: 0 }, '/sidebar-counts': {},
    };
    return route.fulfill({ json: fixtures[path] ?? [] });
  });
  await page.goto(`http://127.0.0.1:${server.address().port}/gross-board?weekStart=${week}`);
  const amount = (name, date = week) => page.getByLabel(`Number Driver, ${date}, ${name}`, { exact: true });
  const stored = (field, date = week) => entries.find(e => e.date === date && e.slot === 0)?.[field];
  for (const [name, field] of [['original rate', 'originalRate'], ['driver rate', 'driverRate'], ['miles', 'miles']]) {
    const input = amount(name);
    await input.click();
    await page.keyboard.type('6');
    await expect.poll(() => stored(field)).toBe('6.00');
    await expect(input).toHaveValue('6');
    expect(await input.evaluate(el => el.selectionStart)).toBe(1);
    await page.keyboard.type('7');
    await expect.poll(() => stored(field)).toBe('67.00');
    await expect(input).toHaveValue('67');
    await page.keyboard.type('.');
    await expect(input).toHaveValue('67.');
    await page.keyboard.type('1');
    await expect.poll(() => stored(field)).toBe('67.10');
    await expect(input).toHaveValue('67.1');
    await page.keyboard.type('2');
    await expect.poll(() => stored(field)).toBe('67.12');
    await page.keyboard.type('3');
    await expect(input).toHaveValue('67.12');
    expect(await input.evaluate(el => el.selectionStart)).toBe(5);
    await input.fill('67.123'); // Paste-style replacement is rejected too.
    await expect(input).toHaveValue('67.12');
    await page.keyboard.press('Enter');
    await expect(page.getByRole('alert').filter({ hasText: /\S/ })).toHaveCount(0);
  }
  // Caret editing keeps both its text and position after the save response.
  const original = amount('original rate');
  await original.click(); await page.keyboard.press('F2');
  await page.keyboard.press('Home'); await page.keyboard.press('ArrowRight');
  await page.keyboard.type('8');
  await expect.poll(() => stored('originalRate')).toBe('687.12');
  await expect(original).toHaveValue('687.12');
  expect(await original.evaluate(el => el.selectionStart)).toBe(2);
  await page.keyboard.type('9'); await expect(original).toHaveValue('6897.12');
  await page.keyboard.press('Enter');
  // Manual original gross continues to follow into an untouched driver gross.
  const manual = amount('original rate', '2026-10-13');
  await manual.click(); await page.keyboard.type('6');
  await expect.poll(() => stored('driverRate', '2026-10-13')).toBe('6.00');
  const followingDriver = amount('driver rate', '2026-10-13');
  await expect(followingDriver).toHaveValue('6');
  await page.keyboard.type('7');
  await expect.poll(() => stored('driverRate', '2026-10-13')).toBe('67.00');
  await expect(followingDriver).toHaveValue('67');
  await page.keyboard.type('.'); await expect(followingDriver).toHaveValue('67.');
  await page.keyboard.type('0');
  await expect.poll(() => stored('originalRate', '2026-10-13')).toBe('67.00');
  await expect(followingDriver).toHaveValue('67.0');
  await page.keyboard.press('Enter');
  await expect(followingDriver).toHaveValue('$67.00');
  // A deliberately different Driver gross must keep its own value and format.
  await followingDriver.click(); await page.keyboard.type('50'); await page.keyboard.press('Enter');
  await expect.poll(() => stored('driverRate', '2026-10-13')).toBe('50.00');
  await manual.click(); await page.keyboard.type('80');
  await expect.poll(() => stored('originalRate', '2026-10-13')).toBe('80.00');
  await expect(followingDriver).toHaveValue('$50.00');
  await page.keyboard.press('Enter');
  // Server resolution can win before the browser's 250 ms lookup finishes.
  const loadNumber = amount('load number or status', '2026-10-15');
  await loadNumber.click(); await page.keyboard.type(source.loadNumber);
  await expect(amount('original rate', '2026-10-15')).toHaveValue('$670.00');
  await expect(amount('driver rate', '2026-10-15')).toHaveValue('$670.00');
  // Browser lookup/selection must apply the same default.
  const browserLoad = amount('load number or status', '2026-10-16');
  await browserLoad.click(); await page.keyboard.type(selected.loadNumber);
  await expect(amount('driver rate', '2026-10-16')).toHaveValue('$890.00');
  // Daily loads use the same decimal guard, including leading-dot input.
  await page.getByRole('button', { name: 'Number Driver, 2026-10-14, load numbers and details', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Daily loads', exact: true });
  const dailyOriginal = dialog.getByLabel('Number Driver, 2026-10-14, original rate', { exact: true }).first();
  const dailyDriver = dialog.getByLabel('Number Driver, 2026-10-14, driver rate', { exact: true }).first();
  await dailyOriginal.click(); await page.keyboard.type('67.0');
  await expect(dailyDriver).toHaveValue('67.0');
  const daily = dialog.getByLabel('Number Driver, 2026-10-14, miles', { exact: true }).first();
  await daily.click(); await page.keyboard.type('.12'); await page.keyboard.type('3');
  await expect(daily).toHaveValue('.12');
  await page.keyboard.press('Enter');
  await dialog.getByRole('button', { name: 'Apply changes', exact: true }).click();
  await expect.poll(() => stored('miles', '2026-10-14')).toBe('0.12');
  await page.reload();
  await expect(manual).toHaveValue('$80.00');
  await expect(followingDriver).toHaveValue('$50.00');
  await expect(amount('miles', '2026-10-15')).toHaveValue('123.45');
  // Existing whole numbers open without padding, so decimals can be appended.
  await manual.click(); await page.keyboard.press('F2'); await page.keyboard.type('.5');
  await expect(manual).toHaveValue('80.5');
  await page.keyboard.press('Enter');
  await expect.poll(() => stored('originalRate', '2026-10-13')).toBe('80.50');
  await expect(manual).toHaveValue('$80.50');
  await followingDriver.click(); await page.keyboard.type('0'); await page.keyboard.press('Enter');
  await expect.poll(() => stored('driverRate', '2026-10-13')).toBe('0.00');
  await expect(followingDriver).toHaveValue('$0.00');
  await original.click(); await page.keyboard.type('3400');
  await expect(original).toHaveValue('3400');
  await expect.poll(() => stored('originalRate')).toBe('3400.00');
  await expect(original).toHaveValue('3400');
  await page.keyboard.press('Enter');
  await expect(original).toHaveValue('$3,400.00');
  await expect(amount('driver rate')).toHaveValue('$3,400.00');
  await original.click(); await page.keyboard.press('F2');
  await expect(original).toHaveValue('3400');
  await page.keyboard.type('.5'); await expect(original).toHaveValue('3400.5');
  await page.keyboard.press('Enter'); await expect(original).toHaveValue('$3,400.50');
  await expect.poll(() => stored('originalRate')).toBe('3400.50');
  const footer = page.locator('tfoot tr').getByRole('cell');
  await expect(footer.nth(0)).toHaveText('$5,108.00');
  await expect(footer.nth(1)).toHaveText('$5,027.50');
  await expect(footer.nth(2)).toHaveText('425.25');
  await expect(amount('total original rate', '2026-10-14')).toHaveText('$67.00');
  await expect(amount('total driver rate', '2026-10-14')).toHaveText('$67.00');
  expect(invalidWrites).toEqual([]);
  expect(errors).toEqual([]);
  await expect(page.getByRole('alert').filter({ hasText: /\S/ })).toHaveCount(0);
  console.log('Gross Board numeric checks passed: delayed decimal saves, cursor stability, two-place input limits, caret editing, manual/source gross defaults, and Daily loads.');
} finally { await browser.close(); await new Promise(resolve => server.close(resolve)); }
