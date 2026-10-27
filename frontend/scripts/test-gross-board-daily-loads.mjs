// Fixture-only browser regressions. Enforce real optimistic versions and decimal normalization.
// MSERP_GROSS_BOARD_BROWSER_URL can check deployed assets; all API requests remain mocked.
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
const week = '2026-10-19', secondDay = '2026-10-20', driverId = 'daily-driver';
const entry = (date, slot = 0) => ({ driverId, date, slot, deleted: false, dayStatus: '', note: '', loadNumber: `LOAD-${date}-${slot}`,
  loadRecordId: 100 + slot, loadTripId: null, split: false, tripReview: false, trips: [], originalRate: '900.00', driverRate: '800.00', miles: '450.00',
  enteredOriginalRate: '900.00', enteredMiles: '450.00', systemOriginalRate: '1000.00', systemMiles: '500.00', version: 3, duplicate: false, acceptSystemValues: false });
const removed = slot => ({ ...entry(secondDay, slot), deleted: true, loadNumber: '', loadRecordId: null,
  originalRate: '', driverRate: '', miles: '', enteredOriginalRate: '', enteredMiles: '', version: slot === 1 ? 5 : 10 });
let entries = [entry(week), entry(week, 1), entry(secondDay), removed(1), removed(2)];
const errors = [], conflicts = [], writes = [];
let holdNextSave = false, releaseSave;
let readOnly = false;
try {
  const page = await browser.newPage({ viewport: { width: 1800, height: 1000 } });
  await page.clock.setFixedTime(new Date('2026-10-20T12:00:00Z'));
  page.on('pageerror', e => errors.push(e.message));
  await page.route('**/api/**', async route => {
    const req = route.request(), path = new URL(req.url()).pathname.slice(4);
    if (req.method() === 'PUT' && path === '/gross-board') {
      const changes = req.postDataJSON().entries;
      writes.push(changes);
      if (holdNextSave) {
        holdNextSave = false;
        await new Promise(resolve => { releaseSave = resolve; });
      }
      if (changes.some(c => c.version !== (entries.find(e => e.date === c.date && e.slot === c.slot)?.version ?? 0))) {
        conflicts.push(changes);
        return route.fulfill({ status: 409, json: { error: 'another dispatcher changed one of these days; reload the board before saving again' } });
      }
      const committed = changes.map(c => {
        const next = { ...c, version: c.version + 1, acceptSystemValues: false };
        for (const field of ['originalRate', 'driverRate', 'miles', 'enteredOriginalRate', 'enteredMiles']) {
          if (next[field] !== '') next[field] = Number(next[field]).toFixed(2);
        }
        return next;
      });
      entries = [...entries.filter(e => !committed.some(c => c.date === e.date && c.slot === e.slot)), ...committed];
      return route.fulfill({ json: committed });
    }
    const fixtures = {
      '/auth/session': { user: { id: 'daily-test', username: 'Daily Test', permissions: ['board.read', ...(readOnly ? [] : ['board.write'])] }, csrfToken: 'fixture' },
      '/gross-board': { weekStart: week, entries, drivers: [{ id: driverId, fullName: 'Daily Driver', active: true, dispatcherId: '', dispatcherName: 'Unassigned' }], balances: [] },
      '/sidebar-counts': {}, '/tasks/count': { count: 0 },
    };
    return route.fulfill({ json: fixtures[path] ?? [] });
  });
  const daily = page.getByRole('dialog', { name: 'Daily loads', exact: true });
  const review = page.getByRole('dialog', { name: `Review load LOAD-${week}-0`, exact: true });
  const field = (date, name, slot = 0) => daily.locator(`[data-gross-slot="${slot}"]`).getByLabel(`Daily Driver, ${date}, ${name}`, { exact: true });
  const saved = (date, slot = 0) => entries.find(e => e.date === date && e.slot === slot);
  const apply = () => daily.getByRole('button', { name: 'Apply changes', exact: true }).click();
  const open = () => page.getByRole('button', { name: `Daily Driver, ${week}, load numbers and details`, exact: true }).click();
  const base = process.env.MSERP_GROSS_BOARD_BROWSER_URL ?? `http://127.0.0.1:${server.address().port}`;
  await page.goto(`${base}/gross-board?weekStart=${week}`);

  // Review/accept/close must never submit the surrounding Daily loads form.
  await open();
  await daily.getByRole('button', { name: `Review rate and miles for Daily Driver, ${week}`, exact: true }).first().click();
  await expect(review).toBeVisible(); await expect(daily).toBeVisible(); expect(writes).toHaveLength(0);
  await review.getByRole('button', { name: 'Close', exact: true }).click();
  await expect(review).toHaveCount(0); await expect(daily).toBeVisible(); expect(writes).toHaveLength(0);
  await daily.getByRole('button', { name: `Review rate and miles for Daily Driver, ${week}`, exact: true }).first().click();
  await review.getByRole('button', { name: 'Accept system values', exact: true }).click();
  await expect(review).toHaveCount(0); await expect(daily).toBeVisible(); expect(writes).toHaveLength(0);
  await expect(field(week, 'total gross')).toHaveValue('$1,000.00');
  await expect(field(week, 'driver rate')).toHaveValue('$800.00');
  await apply(); await expect.poll(() => saved(week).originalRate).toBe('1000.00');

  // Clearing overrides immediately restores source values when focus leaves the field.
  await open();
  await field(week, 'total gross', 1).click(); await page.keyboard.press('Delete'); await page.keyboard.press('Enter');
  await field(week, 'miles', 1).click(); await page.keyboard.press('Delete'); await page.keyboard.press('Enter');
  await field(week, 'driver rate', 1).focus();
  await expect(field(week, 'total gross', 1)).toHaveValue('$1,000.00');
  await expect(field(week, 'miles', 1)).toHaveValue('500.00');
  await expect(field(week, 'driver rate', 1)).toHaveValue('800');
  await apply(); await expect.poll(() => saved(week, 1).miles).toBe('500.00');
  await page.reload(); await open();
  await expect(field(week, 'total gross', 1)).toHaveValue('$1,000.00');
  await daily.getByRole('button', { name: 'Cancel', exact: true }).click();

  // Reuse two removed slots in one editor, matching the reported disappearing loads.
  await page.getByRole('button', { name: `Daily Driver, ${secondDay}, add another load`, exact: true }).click();
  await field(secondDay, 'load number or status', 1).fill('SECOND');
  await field(secondDay, 'total gross', 1).fill('250');
  await field(secondDay, 'miles', 1).fill('125');
  await daily.getByRole('button', { name: 'Add load', exact: true }).click();
  await field(secondDay, 'load number or status', 2).fill('THIRD');
  await field(secondDay, 'total gross', 2).fill('300');
  await field(secondDay, 'miles', 2).fill('150');
  holdNextSave = true;
  await apply(); await expect.poll(() => typeof releaseSave).toBe('function');
  await page.getByRole('button', { name: `Daily Driver, ${secondDay}, add another load`, exact: true }).click();
  await field(secondDay, 'load number or status', 3).fill('FOURTH');
  await field(secondDay, 'total gross', 3).fill('400');
  await field(secondDay, 'miles', 3).fill('200');
  releaseSave(); await expect.poll(() => saved(secondDay, 1)?.version).toBe(6);
  await expect(page.getByRole('status').filter({ hasText: /^Saved$/ })).toBeVisible();
  // Also change a field on the just-saved second load; retain its committed version.
  await field(secondDay, 'driver rate', 1).click(); await page.keyboard.type('200'); await page.keyboard.press('Enter');
  await apply();
  await expect.poll(() => saved(secondDay, 2)?.loadNumber).toBe('THIRD');
  await expect.poll(() => saved(secondDay, 3)?.loadNumber).toBe('FOURTH');
  await expect.poll(() => saved(secondDay, 1)?.driverRate).toBe('200.00');
  expect(conflicts).toEqual([]);
  expect(writes.at(-1).map(e => e.slot).sort()).toEqual([1, 3]);
  await page.reload();
  await page.getByRole('button', { name: `Daily Driver, ${secondDay}, load numbers and details`, exact: true }).click();
  await expect(field(secondDay, 'load number or status', 1)).toHaveValue('SECOND');
  await expect(field(secondDay, 'load number or status', 2)).toHaveValue('THIRD');
  await expect(field(secondDay, 'load number or status', 3)).toHaveValue('FOURTH');
  await daily.getByRole('button', { name: 'Cancel', exact: true }).click();

  // A genuinely stale server version still rejects writes and preserves the draft.
  await open(); await field(week, 'driver rate').click(); await page.keyboard.type('700'); await page.keyboard.press('Enter');
  saved(week).version++;
  await apply(); await expect.poll(() => conflicts.length).toBe(1);
  await expect(page.getByRole('alert').filter({ hasText: 'another dispatcher' })).toBeVisible();
  expect(saved(week).driverRate).toBe('800.00');

  saved(week).originalRate = '900.00'; saved(week).enteredOriginalRate = '900.00';
  readOnly = true; await page.reload(); await open();
  await expect(field(week, 'driver rate')).toBeDisabled();
  await daily.getByRole('button', { name: `Review rate and miles for Daily Driver, ${week}`, exact: true }).first().click();
  await expect(review).toBeVisible();
  await expect(review.getByRole('button', { name: 'Accept system values', exact: true })).toHaveCount(0);
  expect(errors).toEqual([]);
  console.log('Daily loads passed: review dialogs, clear-to-system fallback, second/third-load persistence during autosave, reload, genuine conflict and read-only guards.');
} finally { releaseSave?.(); await browser.close(); await new Promise(resolve => server.close(resolve)); }
