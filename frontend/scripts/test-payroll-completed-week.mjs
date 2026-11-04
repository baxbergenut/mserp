import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { createServer } from 'node:http';
import { extname } from 'node:path';
import ts from 'typescript';
import { chromium, expect } from '@playwright/test';

const source = await readFile(new URL('../app/accounting/payrollWeek.ts', import.meta.url), 'utf8');
const code = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.ESNext } }).outputText;
const { latestPayrollWeek } = await import(`data:text/javascript;base64,${Buffer.from(code).toString('base64')}`);
for (const [date, week] of [['2026-11-02T04:59:59Z', '2026-10-19'], ['2026-11-02T05:00:00Z', '2026-10-26'], ['2026-03-09T03:59:59Z', '2026-02-23'], ['2026-03-09T04:00:00Z', '2026-03-02']]) {
  assert.equal(latestPayrollWeek(new Date(date)), week);
}
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
try {
  for (const kind of ['driver', 'investor', 'dispatcher']) {
    const page = await browser.newPage();
    await page.clock.install({ time: new Date('2026-11-02T04:59:30Z') });
    const weeks = [];
    await page.route('**/api/**', async route => {
      const url = new URL(route.request().url()), path = url.pathname.slice(4);
      if (path === '/auth/session') return route.fulfill({ json: { user: { id: 'user', username: 'Payroll test', permissions: ['driver_pay.read', 'investor_pay.read', 'dispatcher_pay.read'], theme: 'solarized-dark' }, csrfToken: 'fixture' } });
      if (path.endsWith('/availability')) return route.fulfill({ json: { firstWeek: '2026-01-05', currentWeek: '2026-10-26', latestWeek: '2026-10-19' } });
      if (path === `/${kind}-pay`) {
        const week = url.searchParams.get('weekStart'); weeks.push(week);
        return route.fulfill({ json: kind === 'dispatcher' ? { weekStart: week, statements: [], availableWeeks: [week], rule: null } : { weekStart: week, revision: 'fixture', drivers: [], preparation: { state: 'ready' } } });
      }
      return route.fulfill({ json: path === '/tasks/count' ? { count: 0 } : [] });
    });
    // A current/future deep link must never request an unfinished week.
    await page.goto(`http://127.0.0.1:${server.address().port}/accounting/${kind}-pay?weekStart=2026-11-02`);
    await expect.poll(() => weeks.at(-1)).toBe('2026-10-19');
    await expect(page.getByRole('button', { name: 'Next week', exact: true })).toBeDisabled();
    await page.clock.fastForward(61_000);
    // Monday unlocks the newly completed week without a page reload.
    await expect(page.getByRole('button', { name: 'Next week', exact: true })).toBeEnabled();
    await page.getByRole('button', { name: 'Last week', exact: true }).click();
    await expect.poll(() => weeks.at(-1)).toBe('2026-10-26');
    await expect(page.getByRole('button', { name: 'Next week', exact: true })).toBeDisabled();
    assert(weeks.every(week => week <= '2026-10-26'));
    await page.close();
  }
  console.log('Completed-week payroll: New York DST, default/deep-link limits, Monday rollover, all three pages passed.');
} finally { await browser.close(); await new Promise(resolve => server.close(resolve)); }
