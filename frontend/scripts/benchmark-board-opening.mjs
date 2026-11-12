// Synthetic 160-driver browser benchmark. API delay is explicit; these are not
// production network measurements. Run after a NEXT_PUBLIC_API_URL=/api build.
import { chromium } from '@playwright/test';
import { createServer } from 'node:http';
import { readFile } from 'node:fs/promises';
import { extname } from 'node:path';
const delay = Number(process.env.BOARD_API_DELAY_MS ?? 0);
const week = '2026-11-09';
const drivers = Array.from({ length: 160 }, (_, i) => ({ id: `a1000000-0000-0000-0000-${String(i + 1).padStart(12, '0')}`, fullName: `Driver ${String(i).padStart(3, '0')}`, active: true, driverType: 'M', truckId: '', truckUnit: `${i + 100}`, dispatcherId: 'dispatch', dispatcherName: 'Dispatch', phone: '', location: null }));
const entries = drivers.flatMap((d, i) => Array.from({ length: 4 }, (_, day) => ({ driverId: d.id, date: `2026-11-${String(day + 9).padStart(2, '0')}`, slot: 0, deleted: false, dayStatus: '', loadNumber: `LOAD-${i}-${day}`, loadRecordId: i * 4 + day + 1, loadTripId: null, originalRate: '1500.00', driverRate: '1200.00', miles: '450', enteredOriginalRate: '', enteredMiles: '', systemOriginalRate: '1500.00', systemDriverRate: '1200.00', systemMiles: '450', note: '', version: 1, duplicate: false, trips: [], sourceStatus: 'dispatched' })));
const emptyLoads = { current: null, week: [], next: [], earlier: [], hidden: [], unavailable: [], destinationSource: false, sourceDestination: '', stopKey: '', fromDate: week, revision: 'fixture', customOrder: false };
const gross = { weekStart: week, drivers, entries, balances: [], planningEntries: [], nextDayDestinations: [] };
const status = { weekStart: week, statusSyncEnabled: true, drivers, entries: drivers.map(d => ({ driverId: d.id, currentLoad: '', trailerNumber: '', trailerNote: '', status: 'READY', destination: '', eta: '', notes: '', homeTime: '', version: 1 })), grossEntries: entries, loads: Object.fromEntries(drivers.map(d => [d.id, emptyLoads])), eld: { configured: false }, driverOrderRevisions: {} };
const server = createServer(async (req, res) => {
  try {
    const path = new URL(req.url, 'http://localhost').pathname;
    if (path.startsWith('/api/')) {
      if (path === '/api/gross-board' || path === '/api/driver-board') await new Promise(resolve => setTimeout(resolve, delay));
      const fixtures = { '/api/auth/session': { user: { id: 'benchmark', username: 'Benchmark', permissions: ['driver_board.read', 'driver_board.write', 'board.read', 'board.write'] }, csrfToken: 'fixture' }, '/api/gross-board': gross, '/api/driver-board': status, '/api/tasks/count': { count: 0 }, '/api/sidebar-counts': {}, '/api/settings/theme-usage': { counts: {}, totalUsers: 0 } };
      res.writeHead(200, { 'Content-Type': 'application/json' }); res.end(JSON.stringify(fixtures[path] ?? [])); return;
    }
    const file = path === '/' ? 'index.html' : extname(path) ? path.slice(1) : `${path.slice(1)}.html`;
    const body = await readFile(new URL(`../out/${file}`, import.meta.url));
    res.writeHead(200, { 'Content-Type': { '.html': 'text/html', '.js': 'application/javascript', '.css': 'text/css', '.txt': 'text/plain' }[extname(file)] ?? 'application/octet-stream' }); res.end(body);
  } catch { res.writeHead(404); res.end(); }
});
await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
const browser = await chromium.launch();
try {
  const page = await browser.newPage({ viewport: { width: 1800, height: 1000 } });
  const cdp = await page.context().newCDPSession(page); await cdp.send('Performance.enable'); const cpu = [];
  const errors = []; page.on('pageerror', e => errors.push(e.message));
  await page.clock.install({ time: new Date('2026-11-12T14:00:00Z') });
  await page.addInitScript(() => {
    window.EventSource = class extends EventTarget { constructor() { super(); setTimeout(() => this.dispatchEvent(new Event('open')), 0); } close() {} };
    window.openingTimes = []; window.longTasks = [];
    new PerformanceObserver(list => window.longTasks.push(...list.getEntries().map(e => ({ start: e.startTime, duration: e.duration })))).observe({ type: 'longtask', buffered: true });
    window.measureBoard = (path, start = performance.now()) => {
      const ready = () => location.pathname === path && (path === '/driver-board' ? document.querySelectorAll('tr[data-driver-id]').length >= 24 : document.querySelectorAll('td[data-gross-field=truck]').length >= 6);
      const observer = new MutationObserver(() => {
        if (!ready()) return;
        observer.disconnect(); requestAnimationFrame(() => requestAnimationFrame(() => window.openingTimes.push({ path, ms: performance.now() - start, longTaskMS: window.longTasks.filter(t => t.start >= start).reduce((n, t) => n + t.duration, 0) })));
      }); observer.observe(document, { childList: true, subtree: true });
    };
    window.measureBoard('/driver-board', 0);
  });
  await page.goto(`http://127.0.0.1:${server.address().port}/driver-board`);
  await page.waitForFunction(() => window.openingTimes.length === 1);
  for (const path of ['/gross-board', '/driver-board', '/gross-board', '/driver-board', '/gross-board']) {
    await page.waitForFunction(() => !document.querySelector('[data-board-row-pending]'));
    const count = await page.evaluate(() => window.openingTimes.length);
    await page.evaluate(path => { window.measureBoard(path); document.querySelector(`a[href="${path}"]`).click(); }, path);
    await page.waitForFunction(count => window.openingTimes.length > count, count, { timeout: 10000 }).catch(async error => { console.error(JSON.stringify({ path, errors, state: await page.evaluate(() => ({ path: location.pathname, times: window.openingTimes, rows: document.querySelectorAll('td[data-gross-driver]').length, body: document.body.innerText.slice(-500) })) })); throw error; });
  }
  const selectionPreserved = await page.evaluate(() => {
    const input = document.querySelector('[data-gross-board-grid] input');
    input?.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true }));
    return !!input?.isConnected && !document.querySelector('[data-board-row-pending]')
      && document.querySelectorAll('td[data-gross-field=truck]').length === 160;
  });
  if (!selectionPreserved) throw new Error('Spreadsheet navigation must mount every row without replacing an existing editor');
  cpu.push(await cdp.send('Performance.getMetrics'));
  console.log(JSON.stringify({ cpu: cpu.map(v => v.metrics.filter(m => ['ScriptDuration','LayoutDuration','RecalcStyleDuration','TaskDuration','Nodes'].includes(m.name))), rowHeight: await page.locator('td[data-gross-driver]').first().evaluate(el => el.closest('tr').getBoundingClientRect().height), drivers: 160, entries: entries.length, apiDelayMS: delay, measurements: await page.evaluate(() => window.openingTimes), errors }, null, 2));
  if (errors.length) process.exitCode = 1;
} finally { await browser.close(); await new Promise(resolve => server.close(resolve)); }
