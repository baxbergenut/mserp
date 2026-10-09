// Isolated browser fixtures exercise shared payroll editing and table geometry.
import { chromium, expect } from '@playwright/test';
import { createServer } from 'node:http';
import { readFile } from 'node:fs/promises';
import { extname, join } from 'node:path';
import { tmpdir } from 'node:os';

const week = '2026-09-28';
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
const base = `http://127.0.0.1:${server.address().port}`;
const browser = await chromium.launch();
try {
  for (const investor of [false, true]) {
    let driver = {
      id: 'statement', fullName: 'Grid Test', truckUnit: 'T1', payType: 'gross_percentage', payRate: '80', isOwnerOperator: true,
      dispatcherId: '', dispatcherName: '', fuelTotal: '40.00', tollTotal: '10.00',
      ...(investor ? { investorId: 'owner', truckId: 'statement', autoCharges: [{ source: 'driver:operator', name: 'Driver earnings · Operator', amount: '-80.00' }] } : {}),
      loads: Array.from({ length: 4 }, (_, slot) => ({ date: week, slot, loadNumber: `L${slot}`, loadRecordId: slot + 1, commentKey: `load${slot}`, boardVersion: 1,
        originalRate: '400.00', systemOriginalRate: '401.00', driverGross: '350.00', totalMiles: '90.00', systemMiles: '91.00', loadedMiles: '80.00', deadheadMiles: '10.00', fee: '280.00', pickupDate: week, pickupLocation: 'Columbus, OH', deliveryLocation: 'Pittsburgh, PA', issues: [] })),
      edits: { driverId: 'statement', weekStart: week, version: 0, notes: '', comments: {}, adjustments: [], fuelOverride: null, tollOverride: null,
        expenseDeductions: [{ expenseId: 'expense', name: 'Equipment', expenseDate: week, amount: '25.00', total: '100.00', available: '100.00', openingBalance: '100.00', remaining: '75.00', version: 1, saved: true }],
        generatedCharges: [{ scheduleId: 'schedule', kind: 'recurring', name: 'Insurance', amount: '-50.00', scheduledAmount: '-50.00', overridden: false, version: 1, scheduleVersion: 1, typeVersion: 1 }],
      },
    };
    let beforeSource;
    const errors = [];
    const page = await browser.newPage({ viewport: { width: 1800, height: 1000 } });
    page.on('pageerror', e => errors.push(e.message));
    await page.route('**/api/**', async route => {
      const request = route.request(), path = new URL(request.url()).pathname.slice(4);
      if (request.method() === 'PUT') {
        const input = request.postDataJSON();
        expect(input.version).toBe(driver.edits.version);
        await new Promise(resolve => setTimeout(resolve, 100));
        driver.edits = { ...input, version: input.version + 1 };
        return route.fulfill({ json: driver.edits });
      }
      if (path.endsWith('/accept-system')) {
        beforeSource = structuredClone(driver.loads);
        const field = request.postDataJSON().field; expect(['originalRate','totalMiles']).toContain(field); driver.loads[0] = { ...driver.loads[0], [field]: field === 'originalRate' ? '401.00' : '91.00', boardVersion: 2 };
        return route.fulfill({ json: { undoId: 'receipt' } });
      }
      if (path.endsWith('/undo-system')) { driver.loads = beforeSource; return route.fulfill({ status: 204 }); }
      const fixtures = {
        '/auth/session': { user: { id: 'user', username: 'Test', permissions: ['payroll.read', 'payroll.write', 'payroll.finalize', 'fleet.read'], theme: 'solarized-dark' }, csrfToken: 'fixture' },
        '/driver-pay': { weekStart: week, revision: 'fixture', drivers: [driver] },
        '/investor-pay': { weekStart: week, revision: 'fixture', drivers: [driver] },
        '/tasks/count': { count: 0 },
      };
      return route.fulfill({ json: fixtures[path] ?? [] });
    });
    await page.goto(`${base}/accounting/${investor ? 'investor' : 'driver'}-pay?weekStart=${week}&${investor ? 'truckId' : 'driverId'}=statement`);
    const region = page.getByRole('region', { name: 'Grid Test reimbursements and charges' });
    await expect(region).toBeVisible();
    await page.evaluate(() => document.fonts.ready);
    const geometry = await region.evaluate(el => ({ height: el.clientHeight, scroll: el.scrollHeight, rows: [...el.querySelectorAll('tr')].map(row => row.getBoundingClientRect().height) }));
    expect(geometry.rows.every(height => height === 32), JSON.stringify(geometry)).toBe(true);
    expect(geometry.height).toBe(320);
    expect(geometry.scroll).toBe(geometry.height);
    const loads = page.getByRole('region', { name: 'Grid Test weekly loads' });
    await expect.poll(() => loads.locator('table').first().evaluate(table => [...table.tBodies[0].rows].map(row => row.getBoundingClientRect().height))).toEqual(Array(10).fill(32));
    expect(await loads.locator('[data-pay-totals] > th').evaluate(cell => getComputedStyle(cell).borderTopWidth)).toBe('1px');
    expect(await region.locator('tr:last-child > td').first().evaluate(cell => getComputedStyle(cell).borderBottomWidth)).toBe('1px');
    const fuel = page.getByLabel('Grid Test, Fuel, amount', { exact: true });
    await fuel.click(); await page.keyboard.type('9'); await page.keyboard.press('Control+z');
    await expect(fuel).toHaveValue('-40.00'); await expect(fuel.locator('..')).toBeFocused();
    await fuel.click(); await expect(fuel).not.toBeFocused(); await expect(fuel.locator('..')).toBeFocused(); await expect(fuel).toHaveValue('-40.00'); await page.keyboard.type('-12');
    await expect(fuel).toHaveValue('-12');
    await page.keyboard.press('ArrowDown');
    await expect(page.getByLabel('Grid Test, Toll, amount', { exact: true }).locator('..')).toBeFocused();
    await page.keyboard.press('ArrowDown');
    if (investor) await page.keyboard.press('ArrowDown');
    await expect(page.getByLabel('Grid Test, expense Equipment, deduction', { exact: true }).locator('..')).toBeFocused();
    await expect(page.getByRole('status').filter({ hasText: 'All changes saved' })).toBeVisible();
    await page.keyboard.press('Control+z');
    await expect(fuel).toHaveValue('-40.00');
    await expect.poll(() => driver.edits.fuelOverride).toBe(null);
    const fee = page.getByLabel('Grid Test, Insurance, charge amount', { exact: true });
    await fee.click(); await page.keyboard.type('-7'); await page.keyboard.press('ArrowLeft');
    await expect(page.getByLabel('Grid Test, Insurance, charge name', { exact: true }).locator('xpath=ancestor::td[1]')).toBeFocused();
    await expect.poll(() => driver.edits.generatedCharges[0].amount).toBe('-7');
    await page.keyboard.press('Control+z');
    await expect(fee).toHaveValue('-50.00');
    await expect.poll(() => driver.edits.generatedCharges[0].amount).toBe('-50.00');
    await fee.click();
    await page.keyboard.press('F2');
    await expect(fee).toBeFocused();
    expect(await fee.evaluate(input => input.selectionStart === input.selectionEnd)).toBe(true);
    await page.keyboard.press('ArrowLeft');
    await expect(fee).toBeFocused();
    await page.keyboard.press('Escape');
    await expect(fee.locator('..')).toBeFocused();
    await fee.dblclick(); await expect(fee).toBeFocused(); await page.keyboard.press('Escape');

    // Read-only cells select without editing, navigate merged rows, and copy.
    await page.context().grantPermissions(['clipboard-read', 'clipboard-write']);
    const sourceCell = page.getByRole('cell', { name: /^Original gross differs/ }).first();
    await sourceCell.click(); await expect(sourceCell).toBeFocused();
    await page.keyboard.press('Control+c');
    expect(await page.evaluate(() => navigator.clipboard.readText())).toBe('$400.00');
    await page.keyboard.press('ArrowRight');
    const driverGross = loads.getByRole('cell', { name: '$350.00', exact: true }).first();
    await expect(driverGross).toBeFocused();
    await page.keyboard.type('123'); await expect(driverGross).toHaveText('$350.00');
    await page.keyboard.press('ArrowDown');
    await expect(loads.getByRole('cell', { name: '$350.00', exact: true }).nth(1)).toBeFocused();
    await page.keyboard.press('ArrowLeft');
    await expect(page.getByRole('cell', { name: /^Original gross differs/ }).nth(1)).toBeFocused();
    await region.locator('tr').first().locator('td').first().click();
    await page.keyboard.press('ArrowLeft');
    await expect(loads.getByRole('cell', { name: '$280.00', exact: true }).first()).toBeFocused();
    await page.keyboard.press('ArrowRight');
    await expect(region.locator('tr').first().locator('td').first()).toBeFocused();
    // The menu previews only the selected value, and changes only that value.
    const gross = page.getByRole('cell', { name: /^Original gross differs/ }).first();
    await gross.click({ button: 'right' });
    await expect(page.getByRole('menu')).toContainText('$400.00 → $401.00');
    await expect(page.getByRole('menu')).toHaveText('Accept system values$400.00 → $401.00');
    await page.getByRole('menuitem', { name: 'Accept system values', exact: true }).click();
    await expect.poll(() => driver.loads[0].originalRate).toBe('401.00'); expect(driver.loads[0].totalMiles).toBe('90.00');
    await expect(page.getByRole('status').filter({ hasText: 'System values accepted' })).toBeVisible();
    await page.keyboard.press('Control+z');
    await expect.poll(() => driver.loads[0].originalRate).toBe('400.00');
    await expect(gross).toHaveText('$400.00');
    await page.screenshot({ path: join(tmpdir(), `mserp-${investor ? 'investor' : 'driver'}-pay-grid.png`), fullPage: true });
    // Overflow appears only when actual content exceeds the visible load rows.
    driver.edits.generatedCharges.push(...Array.from({ length: 12 }, (_, i) => ({ ...driver.edits.generatedCharges[0], scheduleId: `extra${i}`, name: `Extra ${i}` })));
    await page.reload(); await expect(region).toBeVisible();
    expect(await region.evaluate(el => el.scrollHeight > el.clientHeight)).toBe(true);
    await page.setViewportSize({ width: 390, height: 844 });
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    expect(errors).toEqual([]);
    await page.close();
  }
  console.log('Payroll grid browser checks passed for driver and investor: row geometry, borders, conditional scrolling, replacement typing, arrow keys, saved undo, source preview/undo, narrow layout.');
} finally { await browser.close(); await new Promise(resolve => server.close(resolve)); }
