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
      ...(investor ? { investorId: 'owner', truckId: 'statement', autoCharges: [{ source: 'driver:operator', name: 'Driver earnings · Operator', amount: '-80.00' }] } : { autoCharges: [{ source: 'expense-reimburse:reimbursement', name: 'Parking refund', amount: '42.25', expenseDate: '2026-09-30' }] }),
      loads: Array.from({ length: 4 }, (_, slot) => ({ date: week, slot, loadNumber: `L${slot}`, loadRecordId: slot + 1, commentKey: `load${slot}`, boardVersion: 1,
        originalRate: '400.00', systemOriginalRate: '401.00', driverGross: '350.00', totalMiles: '90.00', systemMiles: '91.00', loadedMiles: '80.00', deadheadMiles: '10.00', fee: '280.00', pickupDate: week, pickupLocation: 'Columbus, OH', deliveryLocation: 'Pittsburgh, PA', issues: [] })),
      edits: { driverId: 'statement', weekStart: week, version: 0, notes: '', comments: {}, adjustments: [], fuelOverride: null, tollOverride: null,
        expenseDeductions: [
          { expenseId: 'expense', name: 'Equipment', expenseDate: week, amount: '25.00', total: '100.00', available: '100.00', openingBalance: '100.00', remaining: '75.00', version: 1, saved: true },
          { source: 'escrow', expenseId: 'escrow', name: 'Escrow', expenseDate: '2026-09-01', amount: '100.00', total: '2500.00', available: '2500.00', openingBalance: '2500.00', remaining: '2400.00', version: 1, saved: true },
        ],
        generatedCharges: [{ scheduleId: 'schedule', kind: 'recurring', name: 'Insurance', amount: '-50.00', scheduledAmount: '-50.00', overridden: false, version: 1, scheduleVersion: 1, typeVersion: 1 }],
      },
    };
    let beforeSource;
    let writable = true;
    let expense = { id: 'expense', categoryId: 'category', category: 'Maintenance', expenseDate: '2026-09-28', expenseType: 'Equipment', amount: '100.00', baseAmount: null, paymentFee: '0.00', paymentType: 'Cash', coveredBy: 'Company', paidBy: 'Test', truckId: null, driverId: null, updatedAt: '2026-10-13T12:00:00Z' };
    let reimbursement = { ...expense, id: 'reimbursement', expenseType: 'Parking refund', expenseDate: '2026-09-30', amount: '42.25', paymentType: 'Reimburse' };
    let writes = 0;
    const options = { settings: [{id:'category',kind:'category',name:'Maintenance',active:true}], categories: [], paymentTypes:['EFS','Cash','Reimburse'], expenseTypes:[], paidBy:['Test'], coveredBy:[], filters:{drivers:[],trucks:[],paymentTypes:[],paidBy:[],coveredBy:[]} };
    const errors = [];
    const expenseReads = [];
    const page = await browser.newPage({ viewport: { width: 1800, height: 1000 } });
    page.on('pageerror', e => { errors.push(e.message); console.error(e); });
    await page.route('**/api/**', async route => {
      const request = route.request(), path = new URL(request.url()).pathname.replace('/lookups/','/').slice(4);
      if (request.method() === 'GET' && path.startsWith('/expenses/')) expenseReads.push(path);
      if (path === '/expenses/reimbursement') {
        if (request.method() === 'PUT') {
          const input = request.postDataJSON();
          expect(input.baseAmount).toBe('55.25');
          expect(input.updatedAt).toBe(reimbursement.updatedAt);
          reimbursement = { ...reimbursement, ...input, amount: '55.25', baseAmount: '55.25' };
          driver.autoCharges[0] = { ...driver.autoCharges[0], amount: '55.25', expenseDate: input.expenseDate };
        }
        return route.fulfill({ json: reimbursement });
      }
      if (path === '/expenses/expense') {
        if (request.method() === 'PUT') {
          const input=request.postDataJSON(); expect(input.baseAmount).toBe('634'); expect(input.updatedAt).toBe(expense.updatedAt);
          expense={...expense,...input,amount:'640.00',baseAmount:'634.00',paymentFee:'6.00'}; writes++;
          driver.edits.expenseDeductions[0]={...driver.edits.expenseDeductions[0],name:input.expenseType,expenseDate:input.expenseDate,total:'640.00',available:'640.00',openingBalance:'640.00'};
        }
        return route.fulfill({json:expense});
      }
      if (path === '/expenses') return route.fulfill({json:{items:[],total:0,page:1,pageSize:1,totalPages:1,options}});
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
        '/auth/session': { user: { id: 'user', username: 'Test', expenseCategoryAccess:[{categoryId:'category',canView:true,canEdit:writable,canCreate:true}], permissions: ['driver_pay.read','investor_pay.read','dispatcher_pay.read', 'driver_pay.edit','investor_pay.edit', 'driver_pay.finalize','driver_pay.reopen','investor_pay.finalize','investor_pay.reopen', 'drivers.read','trucks.read','dispatchers.read','updaters.read','investors.read'], theme: 'solarized-dark' }, csrfToken: 'fixture' },
        '/driver-pay': { weekStart: week, revision: 'fixture', drivers: [driver] },
        '/investor-pay': { weekStart: week, revision: 'fixture', drivers: [driver] },
        '/tasks/count': { count: 0 },
        '/maintenance/options': { vendors: [], types: [] },
      };
      return route.fulfill({ json: fixtures[path] ?? [] });
    });
    await page.goto(`${base}/accounting/${investor ? 'investor' : 'driver'}-pay?weekStart=${week}&${investor ? 'truckId' : 'driverId'}=statement`);
    const region = page.getByRole('region', { name: 'Grid Test reimbursements and charges' });
    await expect(region).toBeVisible();
    for (const label of ['Fuel', 'Toll']) {
      const row = region.getByRole('row').filter({ has: page.getByText(label, { exact: true }) });
      await expect(row.getByText('Left: $0.00', { exact: true })).toBeVisible();
      await expect(row.getByText(/^Source /)).toHaveCount(0);
    }

    const escrowRow = region.getByRole('row').filter({ has: page.getByText('Escrow', {exact:true}) });
    await expect(escrowRow.getByRole('button')).toHaveCount(0);
    await expect(escrowRow.getByText('09/01', {exact:true})).toHaveCount(0);
    await expect(escrowRow.getByText('Left: $2,400.00', {exact:true})).toBeVisible();
    await expect(escrowRow.getByRole('textbox')).toBeEnabled();
    await escrowRow.getByText('Escrow', {exact:true}).click();
    await expect(page.getByRole('dialog')).toHaveCount(0);
    expect(expenseReads).toEqual([]);

    if (!investor) {
      const reimbursementRow = region.getByRole('row').filter({ has: page.getByRole('button', { name: 'Open expense Parking refund', exact: true }) });
      await expect(reimbursementRow.getByText('09/30', { exact: true })).toBeVisible();
      await expect(reimbursementRow.locator('td').last()).toHaveText('+$42.25');
      await expect(reimbursementRow.locator('td').last()).toHaveClass(/text-emerald-300/);
      await reimbursementRow.getByRole('button').click();
      const reimbursementModal = page.getByRole('dialog', { name: 'Edit entry' });
      await expect(reimbursementModal.getByLabel('Amount', { exact: true })).toHaveValue('42.25');
      await expect(reimbursementModal.getByLabel('Payment method', { exact: true })).toHaveValue('Reimburse');
      await reimbursementModal.getByLabel('Amount', { exact: true }).fill('55.25');
      await reimbursementModal.getByLabel('Expense date', { exact: true }).fill('2026-10-01');
      await reimbursementModal.getByRole('button', { name: 'Save changes', exact: true }).click();
      await expect(reimbursementModal).toHaveCount(0);
      await expect(reimbursementRow.getByText('10/01', { exact: true })).toBeVisible();
      await expect(reimbursementRow.locator('td').last()).toHaveText('+$55.25');
    }

    await expect(region.getByText('09/28', {exact:true})).toBeVisible();
    await page.getByRole('button',{name:'Open expense Equipment',exact:true}).click();
    let modal=page.getByRole('dialog',{name:'Edit entry'});
    await expect(modal).toBeVisible();
    await modal.getByLabel('Payment method',{exact:true}).selectOption('EFS');
    await modal.getByLabel('Amount',{exact:true}).fill('300');
    await expect(modal.getByRole('status')).toHaveText('EFS fee: $3.00 · Total: $303.00');
    await modal.getByLabel('Amount',{exact:true}).fill('500');
    await expect(modal.getByRole('status')).toHaveText('EFS fee: $3.00 · Total: $503.00');
    await modal.getByLabel('Amount',{exact:true}).fill('500.01');
    await expect(modal.getByRole('status')).toHaveText('EFS fee: $6.00 · Total: $506.01');
    await modal.getByLabel('Amount',{exact:true}).fill('634');
    await expect(modal.getByRole('status')).toHaveText('EFS fee: $6.00 · Total: $640.00');
    await modal.getByLabel('Expense type',{exact:true}).selectOption('__custom');
    await modal.getByLabel('Expense type custom',{exact:true}).fill('Parking');
    await modal.getByLabel('Expense date',{exact:true}).fill('2026-09-14');
    await modal.getByRole('button',{name:'Save changes',exact:true}).click();
    await expect(modal).toHaveCount(0); expect(writes).toBe(1);
    await expect(region.getByText('09/14',{exact:true})).toBeVisible();
    await expect(region.getByText('Left: $615.00',{exact:true})).toBeVisible();
    expect(page.url()).toContain('/accounting/');
    await page.getByRole('button',{name:'Open expense Parking',exact:true}).click();
    await expect(modal.getByLabel('Amount',{exact:true})).toHaveValue('634.00');
    await expect(modal.getByRole('status')).toHaveText('EFS fee: $6.00 · Total: $640.00');
    await page.screenshot({path:join(tmpdir(), investor ? 'mserp-investor-expense-edit.png':'mserp-driver-expense-edit.png'),fullPage:true,animations:"disabled"});
    await modal.getByRole('button',{name:'Cancel',exact:true}).click();
    writable=false; await page.reload();
    if (!investor) {
      await page.getByRole('button', { name: 'Open expense Parking refund', exact: true }).click();
      const reimbursementDetails = page.getByRole('dialog', { name: 'Expense details' });
      await expect(reimbursementDetails.getByLabel('Amount', { exact: true })).toHaveValue('55.25');
      await expect(reimbursementDetails.getByLabel('Amount', { exact: true })).toBeDisabled();
      await expect(reimbursementDetails.getByRole('button', { name: 'Save changes', exact: true })).toHaveCount(0);
      await reimbursementDetails.getByRole('button', { name: 'Close', exact: true }).click();
    }
    await page.getByRole('button',{name:'Open expense Parking',exact:true}).click();
    modal=page.getByRole('dialog',{name:'Expense details'});
    await expect(modal.getByLabel('Amount',{exact:true})).toBeDisabled();
    await expect(modal.getByRole('button',{name:'Save changes',exact:true})).toHaveCount(0);
    expect(expenseReads.length).toBeGreaterThan(0);
    expect(expenseReads.every(path => ['/expenses/expense', '/expenses/expense/documents', '/expenses/reimbursement', '/expenses/reimbursement/documents'].includes(path))).toBe(true);
    // Dated non-paying driver classifications hide empty costs, but historical
    // balances remain collectable. Investor statements still show truck costs.
    for (const employmentType of ['company', 'investor_driver']) {
      driver = { ...driver, employmentType, isOwnerOperator: false, payType: 'gross_percentage', fuelTotal: '0.00', tollTotal: '0.00',
        edits: { ...driver.edits, fuelOverride: null, tollOverride: null, costs: { fuelDue: '0.00', fuelCarry: '0.00', tollDue: '0.00', tollCarry: '0.00' } } };
      await page.reload();
      await expect(region).toBeVisible();
      await expect(region.getByRole('textbox', { name: 'Grid Test, Fuel, amount', exact: true })).toHaveCount(investor ? 1 : 0);
      await expect(region.getByRole('textbox', { name: 'Grid Test, Toll, amount', exact: true })).toHaveCount(investor ? 1 : 0);
    }
    driver.edits.costs = { fuelDue: '25.00', fuelCarry: '25.00', tollDue: '0.00', tollCarry: '0.00' };
    driver.edits.fuelOverride = '-10.00';
    await page.reload();
    const fuelRow = region.getByRole('row').filter({ has: page.getByText('Fuel', { exact: true }) });
    await expect(fuelRow.getByRole('textbox')).toHaveValue('-10.00');
    await expect(fuelRow.getByText('Left: $15.00', { exact: true })).toBeVisible();
    driver.edits.fuelOverride = null;
    await page.reload();
    await expect(fuelRow.getByText('Left: $0.00', { exact: true })).toBeVisible();
    expect(errors).toEqual([]); await page.close();
  }
  console.log('Driver and Investor Pay expense dates, editing, fee boundaries, refresh and view-only access passed.');
} finally { await browser.close(); await new Promise(resolve=>server.close(resolve)); }
