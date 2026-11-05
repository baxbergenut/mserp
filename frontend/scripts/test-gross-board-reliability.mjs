// Stateful browser regression: optimistic versions, delayed/failed saves and
// out-of-order live snapshots. Synthetic data only; no production access.
import { chromium, expect } from '@playwright/test';
import { createServer } from 'node:http';
import { readFile } from 'node:fs/promises';
import { extname } from 'node:path';

const server = createServer(async (req, res) => {
  try {
    const path = new URL(req.url, 'http://localhost').pathname;
    const file = extname(path) ? path.slice(1) : `${path.slice(1)}.html`;
    const body = await readFile(new URL(`../out/${file}`, import.meta.url));
    res.writeHead(200, { 'Content-Type': { '.html': 'text/html', '.js': 'application/javascript', '.css': 'text/css' }[extname(file)] ?? 'application/octet-stream' });
    res.end(body);
  } catch { res.writeHead(404); res.end(); }
});
await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
const browser = await chromium.launch();
const context = await browser.newContext({ viewport: { width: 1800, height: 1000 } });
const week = '2026-11-02', driverId = 'a1000000-0000-0000-0000-000000000001';
const blank = (date = week) => ({ driverId, date, slot: 0, deleted: false, dayStatus: '', loadNumber: '', loadRecordId: null, loadTripId: null, originalRate: '', driverRate: '', miles: '', enteredOriginalRate: '', enteredMiles: '', note: '', version: 0, duplicate: false, trips: [] });
let entries = [], writes = [], reads = 0, fail = false, gate = null, committedSignal = null;
let duplicateError = '';
let loadChoices = [];
const errors = [];
const board = () => ({ weekStart: week, drivers: [{ id: driverId, fullName: 'Reliable Driver', dispatcherId: '', dispatcherName: 'Unassigned', active: true }], entries: structuredClone(entries), balances: [] });
const push = async value => Promise.all(context.pages().map(page => page.evaluate(value => window.emitGross?.(value), value).catch(() => {})));
await context.addInitScript(() => {
  const streams = new Set();
  window.EventSource = class extends EventTarget {
    constructor(url) { super(); this.url = url; streams.add(this); setTimeout(() => this.dispatchEvent(new Event('open')), 0); }
    close() { streams.delete(this); }
  };
  window.emitGross = board => { for (const stream of streams) if (stream.url.includes('/gross-board/events')) stream.dispatchEvent(new MessageEvent('grossBoard', { data: JSON.stringify(board) })); };
});
await context.route('**/api/**', async route => {
  const req = route.request(), url = new URL(req.url()), path = url.pathname.slice(4);
  if (path === '/gross-board' && req.method() === 'PUT') {
    const edits = req.postDataJSON().entries;
    writes.push(structuredClone(edits));
    if (duplicateError) return route.fulfill({ status: 409, json: { error: duplicateError } });
    if (fail) return route.fulfill({ status: 503, json: { error: 'Temporary fixture outage' } });
    const committed = edits.map(edit => {
      const current = entries.find(e => e.date === edit.date && e.slot === edit.slot) ?? blank(edit.date);
      const next = { ...current, version: current.version + 1 };
      for (const field of edit.fields ?? Object.keys(edit)) if (field !== 'version') next[field] = edit[field];
      return next;
    });
    entries = [...entries.filter(e => !committed.some(c => c.date === e.date && c.slot === e.slot)), ...committed];
    committedSignal?.();
    const hold = gate;
    if (hold) await hold;
    await route.fulfill({ json: committed });
    if (!hold) await push(board());
    return;
  }
  if (path === '/gross-board') { reads++; return route.fulfill({ json: board() }); }
  if (path === '/gross-board/loads') return route.fulfill({ json: loadChoices });
  const fixtures = {
    '/auth/session': { user: { id: 'reliability-user', username: 'Reliable', permissions: ['board.read', 'board.write'] }, csrfToken: 'fixture' },
    '/tasks/count': { count: 0 }, '/sidebar-counts': {},
  };
  return route.fulfill({ json: fixtures[path] ?? [] });
});
const field = (page, name = 'load number or status', date = week) => page.getByLabel(`Reliable Driver, ${date}, ${name}`, { exact: true });
const fill = async (input, value) => { await input.focus(); await input.fill(value); };
const saved = page => expect(page.getByRole('status').filter({ hasText: /^Saved$/ })).toBeVisible();
const stored = (date = week) => entries.find(e => e.date === date && e.slot === 0);
const open = async () => {
  const page = await context.newPage();
  page.on('pageerror', error => errors.push(error.message));
  await page.clock.setFixedTime(new Date('2026-11-03T14:00:00Z'));
  await page.goto(`http://127.0.0.1:${server.address().port}/gross-board`);
  await expect(field(page)).toBeVisible();
  await push(board());
  return page;
};
try {
  const page = await open();
  const staleEmpty = board();
  // A burst is one write, not one transaction for every character.
  await field(page).focus(); await page.keyboard.type('PLAN-123456', { delay: 15 });
  await saved(page); expect(writes.length).toBe(1);
  const committedVersion = stored().version;
  await push(staleEmpty);
  await field(page).blur();
  await expect(field(page)).toHaveValue('PLAN-123456');
  await fill(field(page, 'driver rate'), '810'); await saved(page);
  expect(writes.at(-1)[0].version).toBe(committedVersion);
  await page.reload(); await expect(field(page)).toHaveValue('PLAN-123456');
  await expect(field(page, 'driver rate')).toHaveValue('$810.00');

  // Same-account second page receives changes even with an untouched field focused.
  const other = await open();
  await field(other, 'driver rate').focus();
  const readCount = reads;
  await fill(field(page, 'driver rate'), '825'); await saved(page);
  await expect(field(other, 'driver rate')).toHaveValue('825');
  await field(other, 'driver rate').blur();
  await expect(field(other, 'driver rate')).toHaveValue('$825.00');
  expect(reads).toBe(readCount);

  // A late acknowledgement never consumes newer keystrokes.
  let release;
  gate = new Promise(resolve => { release = resolve; });
  const committed = new Promise(resolve => { committedSignal = resolve; });
  await fill(field(page, 'driver rate'), '900'); await committed;
  await fill(field(page, 'driver rate'), '975');
  gate = null; committedSignal = null; release();
  await saved(page); expect(stored().driverRate).toBe('975');

  // Recovery survives a reload even when the user continues past unload warning.
  fail = true;
  await fill(field(page, 'driver rate'), '1111');
  await expect(page.getByRole('main').getByRole('alert')).toContainText('Temporary fixture outage');
  // Edits made after the first failure join the recovery copy in this tab.
  const queuedDay = '2026-11-04';
  await fill(field(page, 'load number or status', queuedDay), 'QUEUED-AFTER-ERROR');
  page.once('dialog', dialog => dialog.accept());
  await page.reload();
  await expect(field(page, 'driver rate')).toHaveValue('$1,111.00');
  await expect(field(page, 'load number or status', queuedDay)).toHaveValue('QUEUED-AFTER-ERROR');
  await expect(page.getByRole('main').getByRole('alert')).toContainText('Temporary fixture outage');
  fail = false;
  await page.getByRole('button', { name: 'Retry save', exact: true }).click();
  await saved(page); expect(stored().driverRate).toBe('1111');
  expect(stored(queuedDay).loadNumber).toBe('QUEUED-AFTER-ERROR');

  // Same-field writes from a stale tab use the last submitted value, without
  // blocking unrelated saves or requesting a version/conflict decision.
  fail = true;
  await fill(field(page, 'driver rate'), '1200');
  await expect(page.getByRole('main').getByRole('alert')).toContainText('Temporary fixture outage');
  entries = entries.map(e => e.date === week ? { ...e, driverRate: '1300', version: e.version + 1 } : e);
  fail = false;
  await page.getByRole('button', { name: 'Retry save', exact: true }).click();
  await saved(page); expect(stored().driverRate).toBe('1200');

  // Different fields can merge without overwriting the other user's work.
  fail = true;
  await fill(field(page, 'driver rate'), '1400');
  await expect(page.getByRole('main').getByRole('alert')).toContainText('Temporary fixture outage');
  entries = entries.map(e => e.date === week ? { ...e, note: 'Other dispatcher note', version: e.version + 1 } : e);
  fail = false;
  await page.getByRole('button', { name: 'Retry save', exact: true }).click();
  await saved(page); expect(stored().driverRate).toBe('1400'); expect(stored().note).toBe('Other dispatcher note');
  await page.reload(); await expect(field(page, 'driver rate')).toHaveValue('$1,400.00');

  // The server's duplicate guard names the existing assignment, retains the
  // draft for correction, and does not cause an autosave retry loop.
  const secondDay = '2026-11-03';
  duplicateError = 'Load PLAN-123456 is already on Gross Board for Reliable Driver on 2026-11-02 (load 1). Edit or move the existing entry instead of adding it again.';
  await fill(field(page, 'load number or status', secondDay), 'PLAN-123456');
  await expect(page.getByRole('main').getByRole('alert')).toContainText(duplicateError);
  await expect(field(page, 'load number or status', secondDay)).toHaveValue('PLAN-123456');
  expect(stored(secondDay)).toBeUndefined();
  const rejectedWrites = writes.length;
  await page.waitForTimeout(700);
  expect(writes.length).toBe(rejectedWrites);
  duplicateError = '';
  await fill(field(page, 'load number or status', secondDay), 'DIFFERENT-LOAD');
  await page.getByRole('button', { name: 'Retry save', exact: true }).click();
  await saved(page);
  expect(stored(secondDay).loadNumber).toBe('DIFFERENT-LOAD');
  expect(stored().driverRate).toBe('1400');
  // Missing source identity is explicit, and focusing the field cannot silently
  // replace a pinned historical link or overwrite the entered driver gross.
  entries = entries.map(e => e.date === week ? { ...e, loadRecordId: 101, sourceMissing: true, version: e.version + 1 } : e);
  loadChoices = [{ id: 102, loadNumber: 'PLAN-123456', originalRate: '1000.00', driverRate: '900.00', miles: '300.00', driverName: 'Reliable Driver', pickupDate: week }];
  await push(board());
  await expect(page.getByText('Not found in system', { exact: true })).toBeVisible();
  const writesBeforeMissingFocus = writes.length;
  await field(page).focus();
  await page.waitForTimeout(500);
  expect(writes.length).toBe(writesBeforeMissingFocus);
  expect(stored().loadRecordId).toBe(101);
  expect(stored().driverRate).toBe('1400');
  expect(errors).toEqual([]);
  console.log('Gross Board reliability passed: batched typing, stale snapshots, version continuity, same-user live tabs, queued typing, reload recovery, last-write field saves, independent-field merges and duplicate rejection/recovery.');
} finally { await browser.close(); await new Promise(resolve => server.close(resolve)); }
