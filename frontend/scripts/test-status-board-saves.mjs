// Saved means the write committed; a slow load-detail read cannot hold up edits.
import { chromium, expect } from '@playwright/test';
import { createServer } from 'node:http';
import { readFile } from 'node:fs/promises';
import { extname } from 'node:path';

const server = createServer(async (req, res) => {
  try {
    const path = new URL(req.url, 'http://localhost').pathname;
    const file = extname(path) ? path.slice(1) : `${path.slice(1)}.html`;
    const body = await readFile(new URL(`../out/${file}`, import.meta.url));
    res.writeHead(200, { 'Content-Type': { '.html': 'text/html', '.js': 'application/javascript', '.css': 'text/css', '.txt': 'text/plain' }[extname(file)] ?? 'application/octet-stream' });
    res.end(body);
  } catch { res.writeHead(404); res.end(); }
});
await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
const browser = await chromium.launch();
let releaseDetails;
let releaseSave;
try {
  const page = await browser.newPage({ viewport: { width: 1440, height: 900 } });
  const errors = [];
  page.on('pageerror', e => errors.push(e.message));
  await page.addInitScript(() => { window.EventSource = class extends EventTarget { close() {} }; });
  const driver = { id: 'driver', fullName: 'Save Driver', driverType: 'M', truckId: 'truck', truckUnit: '101', truckOperatingStatus: 'in_use', availabilityStatus: 'active', statusVersion: 1, dispatcherId: '', dispatcherName: '', phone: '', location: null };
  let entry = { driverId: driver.id, currentLoad: '', trailerNumber: '', trailerNote: '', status: 'READY', destination: '', eta: '', notes: '', homeTime: '', version: 1 };
  const loads = { current: null, week: [], next: [], earlier: [], hidden: [], unavailable: [], destinationSource: false, sourceDestination: '', stopKey: '', fromDate: '2026-11-02', revision: 'fixture', customOrder: false };
  let writes = 0, detailReads = 0, conflict = false;
  let saveGate;
  await page.route('**/api/**', async route => {
    const req = route.request(), path = new URL(req.url()).pathname.slice(4);
    if (path === '/driver-board' && req.method() === 'PUT') {
      const input = req.postDataJSON().entries[0];
      expect(input.fleetStatus).toBeUndefined();
      if (saveGate) await saveGate;
      if (conflict || input.version !== entry.version) return route.fulfill({ status: 409, json: { error: 'Status board changed; your draft is retained.' } });
      writes++;
      entry = { ...input, statusEdited: false, version: entry.version + 1,
        fleetStatus: { availabilityStatus: 'active', statusVersion: 1, truckId: 'truck', truckOperatingStatus: input.status === 'SHOP' ? 'shop' : 'in_use' } };
      Object.assign(driver, entry.fleetStatus);
      return route.fulfill({ json: [entry] });
    }
    if (path === '/driver-board/loads/driver') {
      detailReads++;
      if (detailReads === 1) await new Promise(resolve => { releaseDetails = resolve; });
      return route.fulfill({ json: loads });
    }
    const fixtures = {
      '/auth/session': { user: { id: 'save-test', username: 'Save Test', permissions: ['driver_board.read', 'driver_board.write'] }, csrfToken: 'fixture' },
      '/driver-board': { statusSyncEnabled: true, weekStart: '2026-11-02', drivers: [driver], entries: [entry], grossEntries: [], loads: { driver: loads }, eld: { configured: false } },
      '/tasks/count': { count: 0 }, '/sidebar-counts': {},
      '/settings/theme-usage': { counts: {}, totalUsers: 0 },
    };
    return route.fulfill({ json: fixtures[path] ?? [] });
  });
  await page.goto(`http://127.0.0.1:${server.address().port}/driver-board`);
  const status = page.getByLabel('Save Driver · Status', { exact: true });
  const notes = page.getByLabel('Save Driver · Notes', { exact: true });
  const saved = () => expect(page.getByRole('status').filter({ hasText: /^Saved$/ })).toBeVisible();
  await status.selectOption('SHOP');
  await page.getByLabel('Truck status reason', { exact: true }).fill('Reported equipment problem');
  await page.getByRole('button', { name: 'Apply status', exact: true }).click();
  await expect.poll(() => detailReads).toBe(1);
  await saved();
  await expect(page.getByRole('button', { name: 'Save Driver · Advance load', exact: true })).toHaveText('Shop');
  await expect(page.locator('tr[data-driver-id="driver"]').locator('td').nth(1)).toHaveText('101');
  await notes.fill('Second edit while details are pending');
  await saved();
  expect(writes).toBe(2);
  expect(entry.notes).toBe('Second edit while details are pending');
  releaseDetails(); releaseDetails = undefined;
  // A server conflict preserves the draft and never claims it was saved.
  conflict = true;
  await notes.fill('Keep my conflicting draft');
  await expect(page.getByRole('status').filter({ hasText: /^Not saved$/ })).toBeVisible();
  await expect(notes).toHaveValue('Keep my conflicting draft');
  expect(entry.notes).toBe('Second edit while details are pending');

  const sidebar = page.getByRole('navigation', { name: 'Main navigation' });
  const settings = sidebar.getByRole('link', { name: 'Settings', exact: true });
  await sidebar.getByRole('link', { name: 'Status Board', exact: true }).click();
  await expect(notes).toBeEnabled();
  await settings.click();
  await expect(page.getByRole('status').filter({ hasText: 'Your edits could not be saved.' })).toBeVisible();
  await expect(page).toHaveURL(/\/driver-board$/);
  await page.getByRole('button', { name: 'Stay here' }).click();
  await expect(notes).toBeEnabled();
  await expect(notes).toHaveValue('Keep my conflicting draft');

  conflict = false;
  saveGate = new Promise(resolve => { releaseSave = resolve; });
  await settings.click();
  await page.getByRole('button', { name: 'Retry and continue' }).click();
  await expect(page.getByRole('status').filter({ hasText: 'Saving changes before leaving' })).toBeVisible();
  await expect(page).toHaveURL(/\/driver-board$/);
  releaseSave();
  saveGate = undefined;
  await expect(page).toHaveURL(/\/settings$/);
  expect(entry.notes).toBe('Keep my conflicting draft');

  await sidebar.getByRole('link', { name: 'Status Board', exact: true }).click();
  conflict = true;
  await notes.fill('Discard this draft');
  await expect(page.getByRole('status').filter({ hasText: /^Not saved$/ })).toBeVisible();
  await settings.click();
  await page.getByRole('button', { name: 'Discard edits and leave' }).click();
  await expect(page).toHaveURL(/\/settings$/);
  expect(entry.notes).toBe('Keep my conflicting draft');
  expect(errors).toEqual([]);
  console.log('Status Board saves passed, including sidebar navigation during delayed and failed saves.');
} finally {
  releaseDetails?.();
  releaseSave?.();
  await browser.close();
  await new Promise(resolve => server.close(resolve));
}
