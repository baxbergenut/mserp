// Synthetic browser regression: every API request is intercepted; no production writes.
import { chromium, expect } from '@playwright/test';
import { createServer } from 'node:http';
import { readFile, mkdir } from 'node:fs/promises';
import { extname } from 'node:path';
import { fileURLToPath } from 'node:url';
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
const week = '2026-11-02', driverId = 'inline-driver', name = 'Inline Driver';
const driver = { id: driverId, fullName: name, active: true, dispatcherId: '', dispatcherName: 'Unassigned', truckId: '', truckUnit: '42' };
const entry = (date, slot = 0) => ({ driverId, date, slot, deleted: false, dayStatus: '', note: 'Load note', loadNumber: `LOAD-${date}-${slot}`, loadRecordId: 100 + slot, loadTripId: null, split: false, tripReview: false, trips: [], originalRate: '400', driverRate: '350', miles: '100', enteredOriginalRate: '400', enteredMiles: '100', systemOriginalRate: '400', systemDriverRate: '350', systemMiles: '100', version: 1, duplicate: false });
let entries = [entry(week), entry('2026-11-06'), entry('2026-11-06', 1), entry('2026-11-07')];
let planning = [{ driverId, date: '2026-11-03', kind: 'destination', text: 'Legacy destination', version: 3 }, { driverId, date: '2026-11-03', kind: 'note', text: 'Legacy note', version: 2 }, { driverId, date: '2026-11-08', kind: 'destination', text: 'Legacy destination only', version: 1 }];
let readOnly = false, failPlanning = false;
const loadWrites = [], planningWrites = [], errors = [];
const same = (a, b) => a.driverId === b.driverId && a.date === b.date && a.kind === b.kind;
const snapshot = () => ({ weekStart: week, drivers: [driver], entries, planningEntries: planning, balances: [] });
const saved = (date, slot = 0) => entries.find(e => e.date === date && e.slot === slot);
const hasContent = e => !e.deleted && !!(e.dayStatus || e.loadNumber || e.loadRecordId != null || e.note || e.originalRate || e.driverRate || e.miles);
const hasLoad = e => !e.deleted && !e.dayStatus && hasContent(e);
try {
  const page = await browser.newPage({ viewport: { width: 1800, height: 1000 } });
  await page.clock.setFixedTime(new Date('2026-11-03T12:00:00Z'));
  page.on('pageerror', error => errors.push(error.message));
  await page.addInitScript(() => { window.EventSource = class { constructor() { setTimeout(() => this.onopen?.(), 0); } addEventListener() {} close() {} }; });
  await page.route('**/api/**', async route => {
    const req = route.request(), url = new URL(req.url()), path = url.pathname.replace('/lookups/', '/').slice(4);
    if (path.startsWith('/gross-board/presence')) return route.fulfill(req.method() === 'GET' ? { json: [] } : { status: 204 });
    if (path === '/gross-board/planning-entries' && req.method() === 'PUT') {
      const changes = req.postDataJSON().entries; planningWrites.push(changes);
      if (failPlanning) return route.fulfill({ status: 503, json: { error: 'Temporary planning save failure' } });
      if (changes.some(c => c.text.trim() && entries.some(e => e.date === c.date && hasContent(e)))) return route.fulfill({ status: 409, json: { error: 'Clear this day’s loads before adding a custom status.' } });
      const committed = [];
      for (const change of changes) {
        const prior = planning.find(e => same(e, change));
        if (prior && prior.version !== change.version && prior.text !== change.text.trim()) return route.fulfill({ status: 409, json: { error: 'Planning entry changed elsewhere' } });
        const next = prior?.text === change.text.trim() ? prior : { ...change, text: change.text.trim(), version: (prior?.version ?? 0) + 1 };
        planning = [...planning.filter(e => !same(e, next)), next]; committed.push(next);
      }
      return route.fulfill({ json: committed });
    }
    if (path === '/gross-board' && req.method() === 'PUT') {
      const changes = req.postDataJSON().entries; loadWrites.push(changes);
      const committed = changes.map(change => {
        const prior = saved(change.date, change.slot);
        const next = { ...prior, driverId, date: change.date, slot: change.slot, version: (prior?.version ?? 0) + 1 };
        for (const field of change.fields ?? Object.keys(change)) if (field !== 'version') next[field] = change[field];
        return next;
      });
      const proposed = [...entries.filter(e => !committed.some(c => c.date === e.date && c.slot === e.slot)), ...committed];
      for (const next of committed) {
        const prior = saved(next.date, next.slot);
        const isNewIdentity = hasContent(next) && (!prior || !hasContent(prior) || prior.loadNumber !== next.loadNumber || prior.dayStatus !== next.dayStatus);
        if (isNewIdentity && planning.some(p => p.date === next.date && p.text.trim())) return route.fulfill({ status: 409, json: { error: 'Clear the custom status before adding a load.' } });
        if (isNewIdentity && proposed.some(e => e.date === next.date && e.dayStatus && !e.deleted) && proposed.some(e => e.date === next.date && hasLoad(e))) return route.fulfill({ status: 409, json: { error: 'A day can have loads or a status, not both.' } });
      }
      entries = proposed;
      return route.fulfill({ json: { entries: committed, issues: [] } });
    }
    const fixtures = { '/auth/session': { user: { id: 'inline-test', username: 'Inline Test', permissions: ['board.read', ...(readOnly ? [] : ['board.write'])] }, csrfToken: 'fixture' }, '/gross-board': snapshot(), '/sidebar-counts': {}, '/tasks/count': { count: 0 } };
    return route.fulfill({ json: fixtures[path] ?? [] });
  });
  const base = `http://127.0.0.1:${server.address().port}`;
  const cell = date => page.locator(`td[data-gross-driver="${driverId}"][data-gross-date="${date}"]`);
  const fullDay = date => cell(date).locator('[data-gross-planning-full-day]');
  const statusButton = date => cell(date).getByRole('button', { name: /Custom status for/ });
  const editor = date => cell(date).locator('textarea');
  await page.goto(`${base}/gross-board?weekStart=${week}`);
  await expect(page.locator('[data-gross-board-grid] thead th').last()).toHaveText('Rate balance');
  expect((await page.locator('[data-gross-board-grid] thead th').allTextContents()).slice(-6)).toEqual(['Total gross', 'Driver gross', 'Total miles', 'Total RPM', 'Driver RPM', 'Rate balance']);
  await expect(page.getByLabel(`Driver RPM for ${name}`, { exact: true })).toHaveText('$3.50');
  await expect(page.getByRole('button', { name: /Add entry/ })).toHaveCount(0);
  await expect(page.getByRole('dialog', { name: 'Day entry', exact: true })).toHaveCount(0);
  await expect(statusButton('2026-11-03')).toHaveCount(2);
  await expect(cell('2026-11-03')).toContainText('Legacy destination');
  await expect(cell('2026-11-03')).toContainText('Legacy note');
  expect(planningWrites).toHaveLength(0);
  // Whole-day selection must not enter edit mode or save on one click.
  const legacyDate = '2026-11-08';
  const statusColors = () => statusButton(legacyDate).evaluate(element => {
    const style = getComputedStyle(element);
    return { background: style.backgroundColor, color: style.color };
  });
  await page.mouse.move(0, 0);
  const restingColors = await statusColors();
  await statusButton(legacyDate).hover();
  expect(await statusColors()).toEqual(restingColors);
  await statusButton(legacyDate).click();
  await expect(cell(legacyDate)).toBeFocused();
  await expect(editor(legacyDate)).toHaveCount(0);
  await cell(legacyDate).press('ArrowLeft');
  await expect(cell('2026-11-07').locator('[data-grid-cell]').first()).toBeFocused();
  for (const key of ['F2', 'Enter']) {
    await statusButton(legacyDate).click();
    await cell(legacyDate).press(key);
    await expect(editor(legacyDate)).toBeFocused();
    await editor(legacyDate).press('Escape');
  }
  await statusButton(legacyDate).focus();
  await statusButton(legacyDate).press('Space');
  await expect(editor(legacyDate)).toBeFocused();
  await editor(legacyDate).press('Escape');
  expect(planningWrites).toHaveLength(0);

  // Identifiers are loads by default: Enter and blur commit without an extra choice.
  const defaultDate = '2026-11-04', defaultInput = cell(defaultDate).getByRole('combobox');
  await expect(defaultInput).toHaveAttribute('placeholder', 'Load number');
  await defaultInput.fill('23423423');
  await expect(page.getByRole('option', { name: /Add as (?:manual )?load/i })).toHaveCount(0);
  await expect(page.getByRole('option', { name: 'Add as status/destination: 23423423', exact: true })).toBeVisible();
  await mkdir(new URL('../../artifacts/', import.meta.url), { recursive: true });
  await page.screenshot({ path: fileURLToPath(new URL('../../artifacts/gross-board-default-load-dropdown.png', import.meta.url)), fullPage: true });
  await defaultInput.blur();
  await expect.poll(() => saved(defaultDate)?.loadNumber).toBe('23423423');
  await expect(cell(defaultDate).getByText('Choose load or custom status', { exact: true })).toHaveCount(0);
  expect(planning.some(e => e.date === defaultDate && e.text)).toBe(false);
  await defaultInput.fill('23423424'); await defaultInput.press('Enter');
  await expect.poll(() => saved(defaultDate)?.loadNumber).toBe('23423424');
  await defaultInput.fill('AB-23423424'); await defaultInput.press('Enter');
  await expect.poll(() => saved(defaultDate)?.loadNumber).toBe('AB-23423424');
  await defaultInput.fill(''); await defaultInput.blur();
  await expect.poll(() => saved(defaultDate)?.loadNumber).toBe('');
  const beforeFreeText = [loadWrites.length, planningWrites.length];
  await defaultInput.fill('Available after noon'); await defaultInput.blur();
  // Wait beyond ordinary load autosave debounce; free text must remain only a local draft.
  await page.waitForTimeout(700);
  expect([loadWrites.length, planningWrites.length]).toEqual(beforeFreeText);
  expect(saved(defaultDate)?.loadNumber).toBe('');
  await expect(cell(defaultDate).getByText('Choose load or custom status', { exact: true })).toHaveCount(0);
  await defaultInput.focus(); await defaultInput.press('Escape');
  // Explicit alternative wins even for numeric text: menu selection must beat blur.
  const beforeNumericStatus = loadWrites.length;
  await defaultInput.fill('23423423');
  await page.getByRole('option', { name: 'Add as status/destination: 23423423', exact: true }).click();
  await expect.poll(() => planning.find(e => e.date === defaultDate)?.text).toBe('23423423');
  await expect(fullDay(defaultDate)).toContainText('23423423');
  await expect(cell(defaultDate).getByRole('combobox')).toHaveCount(0);
  expect(saved(defaultDate)?.loadNumber).toBe('');
  expect(loadWrites).toHaveLength(beforeNumericStatus);
  await statusButton(defaultDate).dblclick(); await editor(defaultDate).fill(''); await editor(defaultDate).press('Enter');
  await expect.poll(() => planning.find(e => e.date === defaultDate)?.text).toBe('');
  await expect(defaultInput).toHaveValue('');
  // ArrowDown explicitly chooses the alternative; plain Enter above stayed a load.
  const beforeKeyboardStatus = loadWrites.length;
  await defaultInput.fill('87654321');
  await expect(page.getByRole('option', { name: /Add as (?:manual )?load/i })).toHaveCount(0);
  await defaultInput.press('ArrowDown');
  await expect(page.getByRole('option', { name: 'Add as status/destination: 87654321', exact: true })).toHaveAttribute('aria-selected', 'true');
  await defaultInput.press('Enter');
  await expect.poll(() => planning.find(e => e.date === defaultDate)?.text).toBe('87654321');
  await expect(cell(defaultDate).getByRole('combobox')).toHaveCount(0);
  expect(saved(defaultDate)?.loadNumber).toBe('');
  expect(loadWrites).toHaveLength(beforeKeyboardStatus);
  await statusButton(defaultDate).dblclick(); await editor(defaultDate).fill(''); await editor(defaultDate).press('Enter');
  await expect.poll(() => planning.find(e => e.date === defaultDate)?.text).toBe('');
  await expect(defaultInput).toHaveValue('');
  const loadsBeforeCustom = loadWrites.length;

  // Status siblings must not stretch the row below the final Miles field.
  const loadCellBounds = await cell(week).boundingBox();
  const milesBounds = await cell(week).getByLabel(name + ', ' + week + ', miles', { exact: true }).boundingBox();
  expect(loadCellBounds).not.toBeNull(); expect(milesBounds).not.toBeNull();
  expect(Math.abs((loadCellBounds.y + loadCellBounds.height) - (milesBounds.y + milesBounds.height))).toBeLessThanOrEqual(2);

  // Free-form text is explicitly saved as planning, never as a dispatch load.
  const statusDate = '2026-11-04';
  await cell(statusDate).getByRole('combobox').fill('Dallas, TX');
  await expect(page.getByRole('option', { name: 'Add as manual load: Dallas, TX', exact: true })).toHaveCount(0);
  await page.getByRole('option', { name: 'Add as status/destination: Dallas, TX', exact: true }).click();
  await expect(fullDay(statusDate)).toBeVisible();
  await expect(fullDay(statusDate).getByRole('combobox')).toHaveCount(0);
  await expect.poll(() => planning.find(e => e.date === statusDate && e.kind === 'note')?.text).toBe('Dallas, TX');
  expect(planning.find(e => e.date === statusDate && e.kind === 'note').kind).toBe('note');
  expect(loadWrites).toHaveLength(loadsBeforeCustom);
  // Reproduce a lost acknowledgement deterministically: saved text is newer
  // than the matching recovery draft, which must rebase before the next edit.
  await expect(fullDay(statusDate).getByText('Unsaved', { exact: true })).toHaveCount(0);
  await expect(fullDay(statusDate).getByRole('status')).toHaveCount(0);
  const committedStatus = planning.find(e => e.date === statusDate && e.kind === 'note');
  await page.evaluate(({ key, draft }) => sessionStorage.setItem(key, JSON.stringify({ savedAt: Date.now(), drafts: { note: draft } })), {
    key: ['gross-board-planning-drafts:v1', 'inline-test', week, driverId, statusDate].join(':'),
    draft: { ...committedStatus, version: Math.max(0, committedStatus.version - 1) },
  });
  await page.reload(); await expect(fullDay(statusDate)).toContainText('Dallas, TX');
  await statusButton(statusDate).dblclick(); await editor(statusDate).fill('Chicago, IL'); await editor(statusDate).press('Enter');
  await expect.poll(() => planning.find(e => e.date === statusDate && e.kind === 'note')?.text).toBe('Chicago, IL');
  await statusButton(statusDate).dblclick(); await editor(statusDate).fill('Discard me'); await editor(statusDate).press('Escape');
  await expect(fullDay(statusDate)).toContainText('Chicago, IL');
  expect(planning.find(e => e.date === statusDate && e.kind === 'note').text).toBe('Chicago, IL');
  await statusButton(statusDate).dblclick();
  await editor(statusDate).press('End'); await editor(statusDate).press('Backspace');
  await expect(editor(statusDate)).toHaveValue('Chicago, I');
  await editor(statusDate).press('Escape');
  await statusButton(statusDate).dblclick(); await editor(statusDate).fill('Available Friday'); await editor(statusDate).blur();
  await expect.poll(() => planning.find(e => e.date === statusDate && e.kind === 'note')?.text).toBe('Available Friday');

  // A status day has no load input: explicitly clear status before switching types.
  await expect(cell(statusDate).getByRole('combobox')).toHaveCount(0);
  await statusButton(statusDate).click(); await expect(cell(statusDate)).toBeFocused();
  await cell(statusDate).press('Backspace');
  await expect.poll(() => planning.find(e => e.date === statusDate && e.kind === 'note')?.text).toBe('');
  await expect(cell(statusDate)).toBeFocused();
  await expect(cell(statusDate).getByRole('combobox')).toHaveValue('');
  await cell(statusDate).getByRole('combobox').fill('STATUS-123');
  await page.keyboard.press('Enter');
  await expect.poll(() => saved(statusDate)?.loadNumber).toBe('STATUS-123');
  await expect(fullDay(statusDate)).toHaveCount(0);
  expect(planning.find(e => e.date === statusDate && e.kind === 'note').text).toBe('');
  await cell(statusDate).getByRole('combobox').fill('');
  await expect.poll(() => saved(statusDate)?.loadNumber).toBe('');
  await cell(statusDate).getByRole('combobox').fill('Available Friday');
  await page.getByRole('option', { name: 'Add as status/destination: Available Friday', exact: true }).click();
  await expect(fullDay(statusDate)).toContainText('Available Friday');
  await expect(cell(statusDate).getByRole('combobox')).toHaveCount(0);

  // Unmatched identifiers become real manual loads by default on Enter.
  const manualDate = '2026-11-05';
  await cell(manualDate).getByRole('combobox').fill('998877');
  await page.keyboard.press('Enter');
  await expect.poll(() => saved(manualDate)?.loadNumber).toBe('998877');
  expect(saved(manualDate).loadRecordId).toBeNull();
  expect(planning.some(e => e.date === manualDate)).toBe(false);

  // Deleting a selected load is one direct action, clearing note and financial data.
  await cell(week).getByRole('combobox').fill('');
  await expect(cell(week).getByRole('combobox')).toHaveValue('');
  await expect(cell(week).getByText('Unconfirmed load text', { exact: true })).toHaveCount(0);
  for (const label of ['total gross', 'driver rate', 'miles']) await expect(cell(week).getByLabel(`${name}, ${week}, ${label}`, { exact: true })).toHaveValue('');
  await expect.poll(() => saved(week)).toMatchObject({ loadNumber: '', loadRecordId: null, note: '', originalRate: '', driverRate: '', miles: '' });
  await page.reload(); await expect(cell(week).getByRole('combobox')).toHaveValue('');

  // X also discards an unselected text draft, including its reload recovery copy.
  const removeDate = '2026-11-07';
  await cell(removeDate).getByRole('combobox').fill('Unselected draft');
  await cell(removeDate).getByRole('button', { name: name + ', ' + removeDate + ', remove load', exact: true }).click();
  await expect.poll(() => saved(removeDate)?.loadNumber).toBe('');
  await expect(cell(removeDate).getByRole('combobox')).toHaveValue('');
  await page.reload(); await expect(cell(removeDate).getByRole('combobox')).toHaveValue('');

  // Legacy destination-only days are statuses too, with no competing load input.
  await expect(fullDay('2026-11-08').getByRole('combobox')).toHaveCount(0);
  expect(planning.find(e => e.date === '2026-11-08' && e.kind === 'destination').text).toBe('Legacy destination only');

  // Clearing one daily slot preserves its sibling and remains an atomic draft.
  const multiDate = '2026-11-06', sibling = structuredClone(saved(multiDate, 1));
  await cell(multiDate).getByRole('button', { name: `${name}, ${multiDate}, load numbers and details`, exact: true }).click();
  const daily = page.getByRole('dialog', { name: 'Daily loads', exact: true });
  const pendingPlanning = planningWrites.length;
  await daily.locator('[data-gross-slot="0"]').getByRole('combobox').fill('Wait for dispatch');
  const customAlternative = page.getByRole('option', { name: /Add as status\/destination: Wait for dispatch/ });
  await expect(customAlternative).toHaveAttribute('aria-disabled', 'true');
  expect(planningWrites).toHaveLength(pendingPlanning);
  await daily.locator('[data-gross-slot="0"]').getByRole('combobox').press('Escape');
  await daily.locator('[data-gross-slot="0"]').getByRole('combobox').fill('HOME');
  const fixedAlternative = page.getByRole('option', { name: 'HOME', exact: true });
  if (await fixedAlternative.count()) await expect(fixedAlternative).toHaveAttribute('aria-disabled', 'true');
  await daily.locator('[data-gross-slot="0"]').getByRole('combobox').press('Escape');
  await daily.locator('[data-gross-slot="0"]').getByRole('combobox').fill('');
  await expect(daily.locator('[data-gross-slot="0"]').getByRole('combobox')).toHaveValue('');
  await daily.getByRole('button', { name: 'Apply changes', exact: true }).click();
  await expect.poll(() => saved(multiDate).loadNumber).toBe('');
  expect(saved(multiDate, 1)).toEqual(sibling);
  // Only after the last load is cleared can the day become a custom status.
  await cell(multiDate).getByRole('combobox').fill('');
  await expect.poll(() => saved(multiDate, 1)?.loadNumber).toBe('');
  await cell(multiDate).getByRole('combobox').fill('Wait for dispatch');
  await page.getByRole('option', { name: 'Add as status/destination: Wait for dispatch', exact: true }).click();
  await expect(fullDay(multiDate)).toContainText('Wait for dispatch');
  await expect(cell(multiDate).getByRole('combobox')).toHaveCount(0);

  // Existing legacy records retain both independent keys after editing one.
  await statusButton('2026-11-03').filter({ hasText: 'Legacy destination' }).dblclick();
  await editor('2026-11-03').fill('Updated legacy destination'); await editor('2026-11-03').press('Enter');
  await expect.poll(() => planning.find(e => e.date === '2026-11-03' && e.kind === 'destination')?.text).toBe('Updated legacy destination');
  expect(planning.find(e => e.date === '2026-11-03' && e.kind === 'note').text).toBe('Legacy note');
  await statusButton('2026-11-03').first().click();
  await cell('2026-11-03').press('Backspace');
  await expect.poll(() => planning.filter(e => e.date === '2026-11-03').every(e => !e.text)).toBe(true);
  await expect(fullDay('2026-11-03')).toHaveCount(0);

  // Failed saves retain a recoverable draft; reload and retry do not duplicate loads.
  failPlanning = true;
  await statusButton(statusDate).dblclick(); await editor(statusDate).fill('Recover after reload'); await editor(statusDate).press('Enter');
  await expect(cell(statusDate).getByRole('alert').filter({ hasText: 'Temporary planning save failure' })).toBeVisible();
  expect(planning.find(e => e.date === statusDate && e.kind === 'note').text).toBe('Available Friday');
  await page.reload(); await expect(fullDay(statusDate)).toContainText('Recover after reload');
  failPlanning = false;
  await statusButton(statusDate).dblclick(); await editor(statusDate).press('Enter');
  await expect.poll(() => planning.find(e => e.date === statusDate && e.kind === 'note')?.text).toBe('Recover after reload');

  // A stale editor cannot overwrite another user's newer text without review.
  await statusButton(statusDate).dblclick(); await editor(statusDate).fill('My concurrent draft');
  planning = planning.map(e => e.date === statusDate && e.kind === 'note' ? { ...e, text: 'Saved elsewhere', version: e.version + 1 } : e);
  await editor(statusDate).press('Enter');
  await expect(cell(statusDate).getByRole('button', { name: 'Replace with my text', exact: true })).toBeVisible();
  expect(planning.find(e => e.date === statusDate && e.kind === 'note').text).toBe('Saved elsewhere');
  await cell(statusDate).getByRole('button', { name: 'Use saved text', exact: true }).click();
  await expect(fullDay(statusDate)).toContainText('Saved elsewhere');
  await statusButton(statusDate).dblclick(); await editor(statusDate).fill('Reviewed replacement');
  planning = planning.map(e => e.date === statusDate && e.kind === 'note' ? { ...e, text: 'Second external edit', version: e.version + 1 } : e);
  await editor(statusDate).press('Enter');
  await cell(statusDate).getByRole('button', { name: 'Replace with my text', exact: true }).click();
  await expect.poll(() => planning.find(e => e.date === statusDate && e.kind === 'note')?.text).toBe('Reviewed replacement');

  // Clearing the only status returns a normal empty load cell without load writes.
  const beforeClear = loadWrites.length;
  await statusButton(statusDate).click(); await cell(statusDate).press('Delete');
  await expect.poll(() => planning.find(e => e.date === statusDate && e.kind === 'note')?.text).toBe('');
  await expect(fullDay(statusDate)).toHaveCount(0);
  expect(loadWrites).toHaveLength(beforeClear);
  await cell(statusDate).getByRole('combobox').fill('HOME');
  await page.getByRole('option', { name: 'HOME', exact: true }).click();
  await expect.poll(() => saved(statusDate)?.dayStatus).toBe('HOME');
  await expect(cell(statusDate).getByRole('button', { name: /add another load/ })).toHaveCount(0);
  await cell(statusDate).getByRole('combobox').fill('');
  await expect.poll(() => saved(statusDate)?.dayStatus).toBe('');

  // Old mixed records are disclosed for cleanup, never silently deleted/migrated.
  entries = entries.map(e => e.date === week ? entry(week) : e);
  planning.push({ driverId, date: week, kind: 'note', text: 'Historical mixed status', version: 1 });
  const oldLoad = structuredClone(saved(week)), oldPlanning = structuredClone(planning.find(e => e.date === week));
  await page.reload();
  const disclosure = cell(week).getByText('Review saved status', { exact: true });
  await expect(disclosure).toBeVisible();
  expect(saved(week)).toEqual(oldLoad); expect(planning.find(e => e.date === week)).toEqual(oldPlanning);
  await expect(statusButton(week)).not.toBeVisible();
  await disclosure.click(); await statusButton(week).click();
  await editor(week).fill(''); await editor(week).press('Enter');
  await expect.poll(() => planning.find(e => e.date === week)?.text).toBe('');
  expect(saved(week)).toEqual(oldLoad);

  // Capture desktop and narrow layouts with only synthetic data.
  await page.mouse.click(1050, 50);
  await page.mouse.move(20, 20);
  await mkdir(new URL('../../artifacts/', import.meta.url), { recursive: true });
  await page.screenshot({ path: fileURLToPath(new URL('../../artifacts/gross-board-inline-desktop.png', import.meta.url)), fullPage: true });
  await page.setViewportSize({ width: 900, height: 900 });
  await fullDay('2026-11-08').scrollIntoViewIfNeeded();
  expect(await fullDay('2026-11-08').evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true);
  await page.screenshot({ path: fileURLToPath(new URL('../../artifacts/gross-board-inline-tablet.png', import.meta.url)), fullPage: true });
  await page.setViewportSize({ width: 390, height: 844 });
  await fullDay('2026-11-08').scrollIntoViewIfNeeded();
  expect(await fullDay('2026-11-08').evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true);
  await page.screenshot({ path: fileURLToPath(new URL('../../artifacts/gross-board-inline-narrow.png', import.meta.url)), fullPage: true });
  await page.setViewportSize({ width: 1800, height: 1000 });

  // Read-only viewers cannot alter either load entries or custom statuses.
  readOnly = true; await page.reload();
  for (const input of await page.locator('[data-gross-board-grid]').getByRole('combobox').all()) await expect(input).toBeDisabled();
  const before = [loadWrites.length, planningWrites.length];
  await expect(statusButton('2026-11-08').first()).toBeDisabled();
  await cell('2026-11-08').click({ position: { x: 2, y: 2 } });
  await cell('2026-11-08').press('Backspace');
  const readEditor = editor('2026-11-08');
  if (await readEditor.count()) await expect(readEditor).toHaveAttribute('readonly', '');
  expect([loadWrites.length, planningWrites.length]).toEqual(before);
  expect(errors).toEqual([]);
  console.log('PASS gross-board exclusive day entries: default load, explicit status, clear-before-switch, multiple slots, legacy cleanup, conflicts and read-only');
} finally { await browser.close(); await new Promise(resolve => server.close(resolve)); }
