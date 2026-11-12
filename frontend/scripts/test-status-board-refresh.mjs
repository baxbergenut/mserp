// Regression: stream updates release focused rows without a whole-board GET,
// connected boards do not poll, and an older GET cannot undo a newer stream.
import { chromium, expect } from '@playwright/test';
import { createServer } from 'node:http';
import { readFile } from 'node:fs/promises';
import { extname } from 'node:path';
const server = createServer(async (req, res) => {
  try {
    const path = new URL(req.url, 'http://localhost').pathname;
    const file = extname(path) ? path.slice(1) : `${path.slice(1)}.html`;
    const body = await readFile(new URL(`../out/${file}`, import.meta.url));
    res.writeHead(200, { 'Content-Type': { '.html': 'text/html', '.js': 'application/javascript', '.css': 'text/css', '.txt': 'text/plain' }[extname(file)] ?? 'application/octet-stream' }); res.end(body);
  } catch { res.writeHead(404); res.end(); }
});
await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
const browser = await chromium.launch();
let releaseRead;
try {
  const page = await browser.newPage({ viewport: { width: 1440, height: 900 } });
  await page.clock.install();
  const errors = []; page.on('pageerror', e => errors.push(e.message));
  await page.addInitScript(() => {
    const streams = new Set();
    window.EventSource = class extends EventTarget {
      constructor(url) { super(); this.url = url; streams.add(this); if (url.includes('/driver-board/events') && !url.includes('initial=none')) throw Error('Board startup must skip duplicate snapshot'); setTimeout(() => this.dispatchEvent(new Event('open')), 0); }
      close() { streams.delete(this); }
    };
    window.boardEvent = (event, value) => { for (const stream of streams) if (stream.url.includes('/driver-board/events')) stream.dispatchEvent(value ? new MessageEvent(event, { data: JSON.stringify(value) }) : new Event(event)); };
  });
  const driver = { id: 'a1000000-0000-0000-0000-000000000001', fullName: 'Refresh Driver', driverType: 'M', truckId: '', truckUnit: '101', availabilityStatus: 'active', statusVersion: 1, dispatcherId: '', dispatcherName: 'Unassigned', phone: '', location: null };
  const entry = { driverId: driver.id, currentLoad: '', trailerNumber: '', trailerNote: '', status: 'READY', destination: '', eta: '', notes: 'Initial', homeTime: '', version: 1 };
  const loads = { current: null, week: [], next: [], earlier: [], hidden: [], unavailable: [], destinationSource: false, sourceDestination: '', stopKey: '', fromDate: '2026-11-09', revision: 'fixture', customOrder: false };
  const board = (notes, version) => ({ statusSyncEnabled: true, weekStart: '2026-11-09', drivers: [driver], entries: [{ ...entry, notes, version }], grossEntries: [], loads: { [driver.id]: loads }, eld: { configured: false }, driverOrderRevisions: {} });
  let reads = 0, delayRead = false;
  await page.route('**/api/**', async route => {
    const req = route.request(), url = new URL(req.url()), path = url.pathname.slice(4);
    if (path === '/driver-board' && req.method() === 'GET') {
      reads++; expect(url.searchParams.get('compact')).toBe('1');
      if (delayRead) { delayRead = false; await new Promise(resolve => { releaseRead = resolve; }); return route.fulfill({ json: board('Stale HTTP response', 1) }); }
      return route.fulfill({ json: { ...board('Initial', 1), plans: [], loads: { [driver.id]: loads } } });
    }
    const fixtures = {
      '/auth/session': { user: { id: 'refresh-test', username: 'Refresh Test', permissions: ['driver_board.read', 'driver_board.write'] }, csrfToken: 'fixture' },
      '/tasks/count': { count: 0 }, '/sidebar-counts': {}, '/settings/theme-usage': { counts: {}, totalUsers: 0 },
    };
    return route.fulfill({ json: fixtures[path] ?? [] });
  });
  await page.goto(`http://127.0.0.1:${server.address().port}/driver-board`);
  const notes = page.getByLabel('Refresh Driver · Notes', { exact: true });
  await expect(notes).toHaveValue('Initial');
  await notes.focus();
  await page.evaluate(value => window.boardEvent('board', value), board('Live while focused', 2));
  await expect(notes).toHaveValue('Initial');
  await notes.evaluate(input => input.blur());
  await expect(notes).toHaveValue('Live while focused');
  expect(reads).toBe(1);
  await page.clock.fastForward(31000);
  expect(reads).toBe(1);
  delayRead = true;
  await page.evaluate(() => window.dispatchEvent(new Event('focus')));
  await expect.poll(() => reads).toBe(2);
  await page.evaluate(value => window.boardEvent('board', value), board('Newest stream', 3));
  await expect(notes).toHaveValue('Newest stream');
  releaseRead(); releaseRead = undefined;
  await page.waitForTimeout(100);
  await expect(notes).toHaveValue('Newest stream');
  await page.evaluate(() => window.boardEvent('error'));
  await page.clock.fastForward(31000);
  await expect.poll(() => reads).toBe(3);
  await expect(notes).toHaveValue('Newest stream');
  // Reconnecting after a fallback read reconciles the disconnected interval.
  await page.evaluate(() => window.boardEvent('open'));
  await expect.poll(() => reads).toBe(4);
  await expect(notes).toHaveValue('Newest stream');
  delayRead = true;
  await page.reload();
  await expect.poll(() => reads).toBe(5);
  await page.evaluate(() => { window.boardEvent('error'); window.boardEvent('open'); });
  releaseRead(); releaseRead = undefined;
  await expect.poll(() => reads).toBe(6);
  await expect(notes).toHaveValue('Initial');
  expect(errors).toEqual([]);
  console.log('Status Board refresh passed: compact initial read, no focus/poll amplification, stale GET protection and disconnected recovery.');
} finally { releaseRead?.(); await browser.close(); await new Promise(resolve => server.close(resolve)); }
