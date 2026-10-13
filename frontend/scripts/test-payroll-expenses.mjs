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
        expenseDeductions: [
          { expenseId: 'expense', name: 'Equipment', expenseDate: week, amount: '25.00', total: '100.00', available: '100.00', openingBalance: '100.00', remaining: '75.00', version: 1, saved: true },
          { source: 'escrow', expenseId: 'escrow', name: 'Escrow', expenseDate: '2026-09-01', amount: '100.00', total: '2500.00', available: '2500.00', openingBalance: '2500.00', remaining: '2400.00', version: 1, saved: true },
        ],
        generatedCharges: [{ scheduleId: 'schedule', kind: 'recurring', name: 'Insurance', amount: '-50.00', scheduledAmount: '-50.00', overridden: false, version: 1, scheduleVersion: 1, typeVersion: 1 }],
      },
    };
    let beforeSource;
    let writable = true;
    let expense = { id: 'expense', categoryId: 'category', category: 'Maintenance', expenseDate: '2026-09-28', expenseType: 'Equipment', amount: '100.00', baseAmount: null, paymentFee: '0.00', paymentType: 'Cash', coveredBy: 'Company', paidBy: 'Test', updatedAt: '2026-10-13T12:00:00Z' };
    let writes = 0;
    const options = { settings: [{id:'category',kind:'category',name:'Maintenance',active:true}], categories: [], paymentTypes:['EFS','Cash'], expenseTypes:[], paidBy:['Test'], coveredBy:[], filters:{drivers:[],trucks:[],paymentTypes:[],paidBy:[],coveredBy:[]} };
    const errors = [];
    const expenseReads = [];
    const page = await browser.newPage({ viewport: { width: 1800, height: 1000 } });
    page.on('pageerror', e => errors.push(e.message));
    await page.route('**/api/**', async route => {
      const request = route.request(), path = new URL(request.url()).pathname.slice(4);
      if (request.method() === 'GET' && path.startsWith('/expenses/')) expenseReads.push(path);
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
        '/auth/session': { user: { id: 'user', username: 'Test', expenseCategoryAccess:[{categoryId:'category',canView:true,canEdit:writable,canCreate:true}], permissions: ['payroll.read', 'payroll.write', 'payroll.finalize', 'fleet.read'], theme: 'solarized-dark' }, csrfToken: 'fixture' },
        '/driver-pay': { weekStart: week, revision: 'fixture', drivers: [driver] },
        '/investor-pay': { weekStart: week, revision: 'fixture', drivers: [driver] },
        '/tasks/count': { count: 0 },
      };
      return route.fulfill({ json: fixtures[path] ?? [] });
    });
    await page.goto(`${base}/accounting/${investor ? 'investor' : 'driver'}-pay?weekStart=${week}&${investor ? 'truckId' : 'driverId'}=statement`);
    const region = page.getByRole('region', { name: 'Grid Test reimbursements and charges' });
    await expect(region).toBeVisible();

    const escrowRow = region.getByRole('row').filter({ has: page.getByText('Escrow', {exact:true}) });
    await expect(escrowRow.getByRole('button')).toHaveCount(0);
    await expect(escrowRow.getByText('09/01', {exact:true})).toHaveCount(0);
    await expect(escrowRow.getByText('Left: $2,400.00', {exact:true})).toBeVisible();
    await expect(escrowRow.getByRole('textbox')).toBeEnabled();
    await escrowRow.getByText('Escrow', {exact:true}).click();
    await expect(page.getByRole('dialog')).toHaveCount(0);
    expect(expenseReads).toEqual([]);

    await expect(region.getByText('09/28', {exact:true})).toBeVisible();
    await page.getByRole('button',{name:'Open expense Equipment',exact:true}).click();
    let modal=page.getByRole('dialog',{name:'Edit entry'});
    await expect(modal).toBeVisible();
    await modal.getByLabel('Payment method',{exact:true}).fill('EFS');
    await modal.getByLabel('Amount',{exact:true}).fill('300');
    await expect(modal.getByRole('status')).toHaveText('EFS fee: $3.00 · Total: $303.00');
    await modal.getByLabel('Amount',{exact:true}).fill('500');
    await expect(modal.getByRole('status')).toHaveText('EFS fee: $3.00 · Total: $503.00');
    await modal.getByLabel('Amount',{exact:true}).fill('500.01');
    await expect(modal.getByRole('status')).toHaveText('EFS fee: $6.00 · Total: $506.01');
    await modal.getByLabel('Amount',{exact:true}).fill('634');
    await expect(modal.getByRole('status')).toHaveText('EFS fee: $6.00 · Total: $640.00');
    await modal.getByLabel('Name',{exact:true}).fill('Parking');
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
    await page.getByRole('button',{name:'Open expense Parking',exact:true}).click();
    modal=page.getByRole('dialog',{name:'Expense details'});
    await expect(modal.getByLabel('Amount',{exact:true})).toBeDisabled();
    await expect(modal.getByRole('button',{name:'Save changes',exact:true})).toHaveCount(0);
    expect(expenseReads.length).toBeGreaterThan(0);
    expect(expenseReads.every(path => path === '/expenses/expense')).toBe(true);
    expect(errors).toEqual([]); await page.close();
  }
  console.log('Driver and Investor Pay expense dates, editing, fee boundaries, refresh and view-only access passed.');
} finally { await browser.close(); await new Promise(resolve=>server.close(resolve)); }
