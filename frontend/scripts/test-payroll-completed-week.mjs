import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { createServer } from 'node:http';
import { extname } from 'node:path';
import ts from 'typescript';
import { chromium, expect } from '@playwright/test';

const source = await readFile(new URL('../app/accounting/payrollWeek.ts', import.meta.url), 'utf8');
const code = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.ESNext } }).outputText;
const { latestPayrollWeek, payrollWeekLabel, payrollWeekOptions, payrollWeekRangeLabel, payrollWeekGroups } = await import(`data:text/javascript;base64,${Buffer.from(code).toString('base64')}`);
for (const [date, week] of [['2026-11-02T04:59:59Z', '2026-10-19'], ['2026-11-02T05:00:00Z', '2026-10-26'], ['2026-03-09T03:59:59Z', '2026-02-23'], ['2026-03-09T04:00:00Z', '2026-03-02']]) {
  assert.equal(latestPayrollWeek(new Date(date)), week);
}
assert.deepEqual(payrollWeekLabel('2026-10-19'), { year: 2026, label: 'Week 43 - 10/19' });
assert.deepEqual(payrollWeekLabel('2026-12-28'), { year: 2026, label: 'Week 53 - 12/28' });
assert.deepEqual(payrollWeekLabel('2024-12-30'), { year: 2025, label: 'Week 1 - 12/30' });
assert.deepEqual(payrollWeekRangeLabel('2026-10-19'), { year: 2026, label: 'Week 43 · 10/19 → 10/25' });
assert.deepEqual(payrollWeekRangeLabel('2024-12-30'), { year: 2025, label: 'Week 1 · 12/30 → 01/05' });
assert.deepEqual(payrollWeekOptions('2026-12-28', '2027-01-04').map(([year]) => year), [2027, 2026]);
// Descending order applies inside and across ISO years, without mutating API input.
const unorderedWeeks = ['2026-12-21', '2027-01-11', '2026-12-28', '2027-01-04', '2026-12-21'];
const originalWeeks = [...unorderedWeeks];
assert.deepEqual(payrollWeekGroups(unorderedWeeks).map(([year, options]) => [year, options.map(option => option.date)]), [
  [2027, ['2027-01-11', '2027-01-04']],
  [2026, ['2026-12-28', '2026-12-21']],
]);
assert.deepEqual(unorderedWeeks, originalWeeks);
assert.deepEqual(payrollWeekOptions('2024-12-23', '2025-01-06').map(([year, options]) => [year, options.map(option => option.date)]), [
  [2025, ['2025-01-06', '2024-12-30']],
  [2024, ['2024-12-23']],
]);
assert.deepEqual(payrollWeekOptions('2026-10-19', '2026-10-19').flatMap(([, options]) => options.map(option => option.date)), ['2026-10-19']);
assert.deepEqual(payrollWeekOptions('2026-10-26', '2026-10-19'), []);
if (process.argv.includes('--unit-only')) {
  console.log('Payroll week ordering and New York completed-week boundary tests passed.');
  process.exit(0);
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
      if (path.endsWith('/availability')) return route.fulfill({ json: { firstWeek: '2024-12-23', currentWeek: '2026-10-26', latestWeek: '2026-10-19' } });
      if (path === `/${kind}-pay`) {
        const week = url.searchParams.get('weekStart'); weeks.push(week);
        return route.fulfill({ json: kind === 'dispatcher' ? { weekStart: week, statements: [], availableWeeks: ['2026-10-05', '2024-12-23', week, '2024-12-30', '2026-10-12', week], rule: null } : { weekStart: week, revision: 'fixture', drivers: [], preparation: { state: 'ready' } } });
      }
      return route.fulfill({ json: path === '/tasks/count' ? { count: 0 } : [] });
    });
    // A current/future deep link must never request an unfinished week.
    await page.goto(`http://127.0.0.1:${server.address().port}/accounting/${kind}-pay?weekStart=2026-11-02`);
    await expect.poll(() => weeks.at(-1)).toBe('2026-10-19');
    const picker = page.getByRole('combobox', { name: 'Payroll week', exact: true });
    await expect(picker).toHaveValue('2026-10-19');
    await expect(picker.locator('optgroup[label="2026"] option:checked')).toHaveText('Week 43 · 10/19 → 10/25');
    await expect(picker.locator('option[value="2026-10-26"]')).toHaveCount(0);
    const dates = await picker.locator('option').evaluateAll(options => options.map(option => option.value));
    assert.deepEqual(dates, [...new Set(dates)].sort().reverse(), kind + ' weeks descend across all year groups');
    assert.equal(dates[0], '2026-10-19');
    await picker.selectOption('2026-10-12');
    await expect.poll(() => weeks.at(-1)).toBe('2026-10-12');
    await page.clock.fastForward(61_000);
    // Monday unlocks the newly completed week without a page reload.
    await expect(picker.locator('option[value="2026-10-26"]')).toHaveText('Week 44 · 10/26 → 11/01');
    await expect(picker).toHaveValue('2026-10-12');
    await expect(picker.locator('option').first()).toHaveAttribute('value', '2026-10-26');
    await picker.selectOption('2026-10-26');
    await expect.poll(() => weeks.at(-1)).toBe('2026-10-26');
    await expect(picker.locator('option[value="2026-11-02"]')).toHaveCount(0);
    assert(weeks.every(week => week <= '2026-10-26'));
    await page.close();
  }
  console.log('Completed-week payroll: New York DST, default/deep-link limits, descending week order, selection retention, Monday rollover, all three pages passed.');
} finally { await browser.close(); await new Promise(resolve => server.close(resolve)); }
