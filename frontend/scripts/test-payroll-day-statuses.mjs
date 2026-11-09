// Display-only Gross Board metadata on both pay pages, using isolated API fixtures.
import { chromium, expect } from '@playwright/test';
import { createServer } from 'node:http';
import { readFile } from 'node:fs/promises';
import { extname, join } from 'node:path';
import { tmpdir } from 'node:os';

const week = '2026-09-28';
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
const browser = await chromium.launch();
try {
  for (const kind of ['driver', 'investor']) for (const mode of ['editable', 'read-only', 'finalized', 'legacy-finalized']) {
    const frozen = mode.endsWith('finalized');
    const note = frozen ? 'Saved status at finalization' : 'Available after 3 PM';
    const literal = '<b>Waiting</b> & ready\nCall dispatch';
    const fixed = [
      { date: week, status: 'SHOP' },
      { date: '2026-10-01', status: 'HOME' },
      { date: '2026-10-03', status: 'IN TRANSIT' },
    ];
    const custom = [
      { date: week, status: 'CUSTOM', kind: 'note', text: 'Hidden behind real load' },
      { date: '2026-09-29', status: 'CUSTOM', kind: 'note', text: note },
      { date: '2026-09-30', status: 'CUSTOM', kind: 'destination', text: 'Dallas, TX' },
      { date: '2026-10-02', status: 'CUSTOM', kind: 'note', text: 'HOME TIME' },
      { date: '2026-10-03', status: 'CUSTOM', kind: 'note', text: literal },
      { date: '2026-10-03', status: 'CUSTOM', kind: 'destination', text: literal },
      { date: '2026-10-04', status: 'CUSTOM', kind: 'note', text: '   ' },
    ];
    const driver = {
      id: 'statement', fullName: 'Status Test', truckUnit: 'T1', payType: 'gross_percentage', payRate: '80', isOwnerOperator: true,
      dispatcherId: '', dispatcherName: '', fuelTotal: '40.00', tollTotal: '10.00', issues: [],
      statementRevision: 'financial-unchanged', expenseRevision: 'expenses-unchanged', preparationState: 'ready',
      ...(kind === 'investor' ? { investorId: 'owner', truckId: 'statement' } : {}),
      ...(frozen ? { settlement: { finalized: true, finalizedBy: 'Fixture accountant' } } : {}),
      loads: [{ date: week, slot: 0, loadNumber: 'L1', loadRecordId: 1, commentKey: 'load1', boardVersion: 1,
        originalRate: '400.00', driverGross: '350.00', totalMiles: '90.00', loadedMiles: '80.00', deadheadMiles: '10.00', fee: '280.00', issues: [] }],
      edits: { driverId: 'statement', weekStart: week, version: 1, notes: '', comments: {}, adjustments: [], fuelOverride: null, tollOverride: null },
      dayStatuses: mode === 'legacy-finalized' ? fixed : [...fixed, ...custom],
    };
    const page = await browser.newPage({ viewport: { width: 1800, height: 1000 } });
    const errors = [], writes = [], boardReads = [];
    await page.addInitScript(() => {
      window.__payrollStreams = new Set();
      window.EventSource = class extends EventTarget {
        constructor(url) { super(); if (/\/(driver|investor)-pay\/events$/.test(url)) window.__payrollStreams.add(this); }
        close() { window.__payrollStreams.delete(this); }
      };
      window.__payrollNotify = () => { for (const stream of window.__payrollStreams) stream.dispatchEvent(new Event('changed')); };
    });
    page.on('pageerror', error => errors.push(error.message));
    let details = 0, holdDetail = mode === 'editable', releaseDetail, staleDetailReturned = false;
    await page.route('**/api/**', async route => {
      const request = route.request(), url = new URL(request.url()), path = url.pathname.slice(4).replace('/lookups/', '/');
      if (path === '/auth/birthday') return route.fulfill({ json: {} });
      if (request.method() !== 'GET') { writes.push(path); return route.fulfill({ status: 403, json: { error: 'This test never writes payroll.' } }); }
      if (path.startsWith('/gross-board')) boardReads.push(path);
      if (path === '/auth/session') return route.fulfill({ json: { user: { id: 'user', username: 'Test', permissions: [
        `${kind}_pay.read`, ...(mode === 'editable' || frozen ? [`${kind}_pay.edit`, `${kind}_pay.finalize`, `${kind}_pay.reopen`] : []),
      ] }, csrfToken: 'fixture' } });
      if (path === `/${kind}-pay/availability`) return route.fulfill({ json: { firstWeek: week, currentWeek: '2026-11-09' } });
      if (path === `/${kind}-pay` || path === `/${kind}-pay/statements/statement`) {
        const row = structuredClone(driver);
        let staleDetail = false;
        if (path.includes('/statements/')) {
          details++;
          if (holdDetail && details === 1) { staleDetail = true; await new Promise(resolve => { releaseDetail = resolve; }); }
          else holdDetail = false;
        } else if (holdDetail || !url.searchParams.get('detailIds')?.split(',').includes(driver.id)) {
          row.summary = { loads: 1, missingFees: 0, review: 0, fee: '280.00', payable: '230.00' };
          row.loads = [];
        }
        await route.fulfill({ json: { weekStart: week, revision: 'inputs-unchanged', drivers: [row], preparation: { state: 'ready', generation: 'generation-unchanged', calculatorVersion: 'fixture' } } });
        if (staleDetail) staleDetailReturned = true;
        return;
      }
      return route.fulfill({ json: path.endsWith('/outgoing-transfers') ? { transfers: [] } : path === '/tasks/count' ? { count: 0 } : [] });
    });
    await page.goto(`${base}/accounting/${kind}-pay?weekStart=${week}`);
    await page.getByRole('button', { name: kind === 'investor' ? 'Status Test T1' : 'Status Test', exact: true }).click();
    const loads = page.getByRole('region', { name: 'Status Test weekly loads' });
    if (mode === 'editable') {
      // An obsolete in-flight detail response must not replace newer summary metadata.
      await expect.poll(() => typeof releaseDetail).toBe('function');
      driver.dayStatuses = driver.dayStatuses.map(item => item.text === note ? { ...item, text: 'Changed while details were loading' } : item);
      await page.evaluate(() => window.__payrollNotify());
      await expect.poll(() => details).toBeGreaterThan(1);
      await expect(loads.getByRole('cell', { name: 'Changed while details were loading', exact: true })).toBeVisible();
      releaseDetail();
      await expect.poll(() => staleDetailReturned).toBe(true);
      await expect(loads.getByRole('cell', { name: 'Changed while details were loading', exact: true })).toBeVisible();
      driver.dayStatuses = driver.dayStatuses.map(item => item.text === 'Changed while details were loading' ? { ...item, text: note } : item);
      await page.evaluate(() => window.__payrollNotify());
      await expect(loads.getByRole('cell', { name: note, exact: true })).toBeVisible();
    }
    await expect(loads.getByRole('cell', { name: 'L1', exact: true })).toBeVisible();
    expect(details).toBeGreaterThan(0);
    const financialState = async () => ({
      totals: await loads.locator('[data-pay-totals]').innerText(),
      payable: await page.locator('td[title="Total payable"]').innerText(),
      charges: await page.getByRole('region', { name: 'Status Test reimbursements and charges' }).innerText(),
    });
    const baseline = await financialState();
    expect(baseline.payable).toBe('$230.00');
    await expect(loads.getByRole('cell', { name: 'HOME', exact: true })).toHaveClass(/bg-violet-500\/15/);
    await expect(loads.getByText('SHOP', { exact: true })).toHaveCount(0);
    await expect(loads.getByText('Hidden behind real load', { exact: true })).toHaveCount(0);
    await expect(loads.getByText('CUSTOM', { exact: true })).toHaveCount(0);
    await expect(page.locator('[title="Home every day of this week, with no loads"]')).toHaveCount(0);
    if (mode === 'legacy-finalized') {
      await expect(loads.getByRole('cell', { name: 'No load', exact: true })).toHaveCount(4);
      await expect(loads.getByText('Dallas, TX', { exact: true })).toHaveCount(0);
    } else {
      const status = loads.getByRole('cell', { name: note, exact: true });
      await expect(status).toBeVisible();
      await expect(status).toHaveClass(/text-zinc-300/);
      await expect(status.locator('input, textarea, button, [contenteditable]')).toHaveCount(0);
      await expect(loads.getByRole('cell', { name: 'Dallas, TX', exact: true })).toBeVisible();
      await expect(loads.getByRole('cell', { name: 'HOME TIME', exact: true })).not.toHaveClass(/bg-violet/);
      await expect(loads.getByText(literal, { exact: true })).toHaveCount(1);
      await expect(loads.getByText('IN TRANSIT', { exact: true })).toHaveClass(/bg-blue-500\/15/);
      await expect(loads.locator('b')).toHaveCount(0);
      await expect(loads.getByRole('cell', { name: 'No load', exact: true })).toHaveCount(1);
      if (!frozen) {
        // The backend changes display metadata without changing financial versions.
        driver.dayStatuses = driver.dayStatuses.map(item => item.text === note ? { ...item, text: 'Updated after dispatch call' } : item);
        await page.evaluate(() => window.__payrollNotify());
        await expect(loads.getByRole('cell', { name: 'Updated after dispatch call', exact: true })).toBeVisible();
        driver.dayStatuses = driver.dayStatuses.filter(item => item.date !== '2026-09-29');
        await page.evaluate(() => window.__payrollNotify());
        await expect(loads.getByRole('cell', { name: 'No load', exact: true })).toHaveCount(2);
        driver.dayStatuses.push({ date: '2026-09-29', status: 'CUSTOM', kind: 'note', text: note });
        await page.evaluate(() => window.__payrollNotify());
        await expect(status).toBeVisible();
      } else {
        // Refresh a frozen snapshot; the UI must not read or backfill live Gross Board text.
        await page.evaluate(() => window.__payrollNotify());
        await expect(status).toBeVisible();
      }
      await status.click();
      await page.keyboard.type('Do not edit payroll status');
      await expect(status).toHaveText(note);
    }
    expect(await financialState()).toEqual(baseline);
    if (mode === 'read-only' || frozen) await expect(page.getByLabel('Status Test, Fuel, amount', { exact: true })).toBeDisabled();
    await page.screenshot({ path: join(tmpdir(), `mserp-${kind}-pay-day-statuses-${mode}.png`), fullPage: true });
    await page.setViewportSize({ width: 390, height: 844 });
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    expect(writes).toEqual([]);
    expect(boardReads).toEqual([]);
    expect(errors).toEqual([]);
    await page.close();
  }
  console.log('Payroll day-status browser checks passed: both pay pages, custom/destination and fixed labels, real-load precedence, safe literal text, live edit/delete/add with unchanged financial versions, read-only/frozen/legacy views, unchanged money, and narrow layout.');
} finally { await browser.close(); await new Promise(resolve => server.close(resolve)); }
