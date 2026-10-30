// Isolated API fixtures verify the three payroll pages without changing payroll.
import { createServer } from 'node:http';
import { readFile, mkdir } from 'node:fs/promises';
import { extname, join } from 'node:path';
import { tmpdir } from 'node:os';
import { chromium, expect } from '@playwright/test';

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
const base = `http://127.0.0.1:${server.address().port}`;
const artifacts = join(tmpdir(), 'mserp-pay-finalization');
await mkdir(artifacts, { recursive: true });
const browser = await chromium.launch();
try {
  for (const kind of ['driver', 'investor', 'dispatcher']) {
    const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
    const errors = [];
    page.on('pageerror', error => errors.push(error.message));
    let finalized = true, total = 4, finalizer = 'Payroll Reviewer';
    const week = '2026-10-26';
    await page.route('**/api/**', async route => {
      const path = new URL(route.request().url()).pathname.slice(4);
      let data = [];
      if (path === '/auth/session') data = { user: { id: 'test', username: 'Test', permissions: ['driver_pay.read', 'investor_pay.read', 'dispatcher_pay.read', 'drivers.read', 'investors.read'], expenseCategoryAccess: [], theme: 'default' }, csrfToken: 'fixture' };
      else if (path === '/auth/birthday') data = { eligible: false, serverTime: new Date().toISOString() };
      else if (path === '/driver-pay' || path === '/investor-pay') data = {
        weekStart: week, revision: 'fixture',
        pagination: { page: 1, pageSize: 25, total, totalPages: 1, dispatchers: [], finalized: finalized && total ? 1 : 0, statements: total, loads: 0, review: 0, fee: '0', payable: '0' },
        drivers: total ? [{ id: 'statement', fullName: 'Test Recipient', ...(kind === 'investor' ? { investorId: 'owner' } : {}), payType: 'cpm', payRate: '0.75', employmentType: 'unassigned', dispatcherName: 'Dispatch', truckUnit: '101', loads: [], fuelTotal: '0', tollTotal: '0', summary: { loads: 0, missingFees: 0, review: 0, fee: '0', payable: '0' }, settlement: { finalized, finalizedBy: finalizer, finalizedAt: '2026-10-30T12:00:00Z', version: 1 }, edits: { driverId: 'statement', weekStart: week, version: 1, comments: {}, adjustments: [], fuelOverride: null, tollOverride: null } }] : [],
      };
      else if (path === '/dispatcher-pay') data = { weekStart: week, availableWeeks: [week], rule: null, statements: total ? [
        ...Array.from({ length: total }, (_, i) => ({ dispatcherId: `dispatch-${i}`, dispatcherName: `Dispatch ${i}`, finalized: i === 0 && finalized, finalizedBy: i === 0 ? finalizer : '', rows: [], gross: 100, pay: 10, loadCount: 1, version: 1, revision: 'fixture' })),
        { dispatcherId: '', dispatcherName: 'Unassigned', finalized: false, rows: [], gross: 0, pay: 0, loadCount: 1, version: 0, revision: 'fixture' },
      ] : [] };
      if ((path === '/driver-pay' || path === '/investor-pay') && total > 1) {
        const template = data.drivers[0];
        data.drivers.push(...['Zulu', 'Alpha'].map(name => ({ ...template, id: `draft-${name}`, fullName: `Draft ${name}`, settlement: null })));
      }
      await route.fulfill({ json: data });
    });
    await page.goto(`${base}/accounting/${kind}-pay?weekStart=${week}`);
    const progress = page.getByRole('progressbar', { name: 'Finalization' });
    await expect(progress).toHaveAttribute('aria-valuenow', '25');
    await expect(progress).toHaveAttribute('aria-valuetext', '1 of 4 statements finalized');
    await expect(page.locator('.ui-metric')).toHaveCount(3);
    await expect(page.locator('.ui-metric').nth(2)).toContainText('Finalization');
    await expect(page.getByText('Loads needing review', { exact: true })).toHaveCount(0);
    await expect(page.getByText('Payroll Reviewer', { exact: true })).toBeVisible();
    if (kind !== 'dispatcher') {
      await expect(page.getByRole('columnheader', { name: 'Finalized by', exact: true })).toBeVisible();
      const rowNames = await page.locator('table[aria-label^="Weekly"] > tbody > tr > th').allTextContents();
      expect(rowNames[0]).toContain('Draft Alpha');
      expect(rowNames[1]).toContain('Draft Zulu');
      expect(rowNames[2]).toContain('Test Recipient');
      await expect(page.getByRole('row').filter({ hasText: 'Test Recipient' }).locator('th,td')).toHaveCount(8);
      await page.getByPlaceholder(kind === 'driver' ? 'Driver, truck, or load…' : 'Investor, truck, or load…').fill('Test');
      await expect(progress).toHaveAttribute('aria-valuenow', '25');
    } else {
      expect(await page.locator('section[aria-label^="Dispatch "]').evaluateAll(rows => rows.map(row => row.getAttribute('aria-label')))).toEqual(['Dispatch 1', 'Dispatch 2', 'Dispatch 3', 'Dispatch 0']);
      await page.getByRole('combobox', { name: 'Dispatcher', exact: true }).selectOption('dispatch-0');
      await expect(progress).toHaveAttribute('aria-valuenow', '25');
    }
    await page.screenshot({ path: join(artifacts, `${kind}-desktop.png`), fullPage: true });
    await page.setViewportSize({ width: 390, height: 844 });
    await expect(progress).toBeVisible();
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    await page.screenshot({ path: join(artifacts, `${kind}-mobile.png`), fullPage: true });
    await page.setViewportSize({ width: 1440, height: 1000 });
    finalized = false;
    await page.reload();
    await expect(progress).toHaveAttribute('aria-valuenow', '0');
    await expect(page.getByText('Payroll Reviewer', { exact: true })).toHaveCount(0);
    finalized = true; total = 1; finalizer = '';
    await page.reload();
    await expect(progress).toHaveAttribute('aria-valuenow', '100');
    await expect(page.getByText('Unknown', { exact: true })).toBeVisible();
    total = 0;
    await page.reload();
    await expect(progress).toHaveAttribute('aria-valuenow', '0');
    await expect(progress).toHaveAttribute('aria-valuetext', 'No statements to finalize');
    expect(errors).toEqual([]);
    await page.close();
  }
  console.log(`Payroll finalization passed: all pages, week-wide counts, filters, finalized/reopened/unknown/empty states, card order and responsive overflow. Screenshots: ${artifacts}`);
} finally {
  await browser.close();
  server.closeAllConnections();
  await new Promise(resolve => server.close(resolve));
}
