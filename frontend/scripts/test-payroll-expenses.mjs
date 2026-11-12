// Isolated browser fixtures exercise shared payroll editing and table geometry.
import { chromium, expect } from '@playwright/test';
import { createServer } from 'node:http';
import { readFile } from 'node:fs/promises';
import { extname, join } from 'node:path';
import { tmpdir } from 'node:os';

const week = '2026-09-28';
const scaleTicket = { source: 'weighmytruck:ticket', name: 'CAT Scale · 2026-09-28 · 1234', amount: '-15.25', scaleDue: '-15.25', scaleOriginalAmount: '-15.25' };
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
      brokerReimbursements: [{expenseId:'expense',loadNumber:'L0',expenseType:'Equipment',expenseDate:week,amount:'100.00'}],
      loads: Array.from({ length: 4 }, (_, slot) => ({ date: week, slot, loadNumber: `L${slot}`, loadRecordId: slot + 1, commentKey: `load${slot}`, boardVersion: 1,
        originalRate: '400.00', systemOriginalRate: '401.00', driverGross: '350.00', totalMiles: '90.00', systemMiles: '91.00', loadedMiles: '80.00', deadheadMiles: '10.00', fee: '280.00', pickupDate: week, pickupLocation: 'Columbus, OH', deliveryLocation: 'Pittsburgh, PA', issues: [] })),
      edits: { driverId: 'statement', weekStart: week, version: 0, notes: '', comments: {}, adjustments: [], fuelOverride: null, tollOverride: null,
        expenseDeductions: [
          { expenseId: 'expense', name: 'Equipment', expenseDate: week, amount: '25.00', total: '100.00', available: '100.00', openingBalance: '100.00', remaining: '75.00', version: 1, saved: false, mode: 'fixed' },
          { source: 'escrow', expenseId: 'escrow', name: 'Escrow', expenseDate: '2026-09-01', amount: '100.00', total: '2500.00', available: '2500.00', openingBalance: '2500.00', remaining: '2400.00', version: 1, saved: true },
        ],
        generatedCharges: [{ scheduleId: 'schedule', kind: 'recurring', name: 'Insurance', amount: '-50.00', scheduledAmount: '-50.00', overridden: false, version: 1, scheduleVersion: 1, typeVersion: 1 }],
      },
    };
    driver.autoCharges = [...(driver.autoCharges ?? []), scaleTicket, { ...scaleTicket, source: 'weighmytruck:second', name: 'CAT Scale second ticket', amount: '-12.00', scaleDue: '-12.00', scaleOriginalAmount: '-12.00' }];
    driver.deferredScaleCharges = [{ ...scaleTicket, source: 'weighmytruck:deferred', name: 'CAT Scale deferred ticket', amount: '0.00', scaleDue: '-8.00', scaleOriginalAmount: '-8.00' }];
    driver.edits.scaleDeductions = { 'weighmytruck:deferred': '0.00' };
    if (!investor) driver.edits.expenseDeductions.push({source:'reimburse',expenseId:'reimbursement',name:'Parking refund',expenseDate:'2026-09-30',amount:'42.25',total:'42.25',available:'42.25',openingBalance:'42.25',remaining:'42.25',version:1,saved:false});
    let beforeSource;
    let writable = true;
    let expense = { id: 'expense', categoryId: 'category', category: 'Maintenance', expenseDate: '2026-09-28', expenseType: 'Equipment', amount: '100.00', baseAmount: null, paymentFee: '0.00', paymentType: 'Cash', coveredBy: 'Company', paidBy: 'Test', truckId: null, driverId: null, updatedAt: '2026-10-13T12:00:00Z' };
    let reimbursement = { ...expense, id: 'reimbursement', expenseType: 'Parking refund', expenseDate: '2026-09-30', amount: '42.25', paymentType: 'Reimburse' };
    let writes = 0, createdAccessorials = 0;
    const options = { settings: [{id:'category',kind:'category',name:'Maintenance',active:true},{id:'accessorial-category',kind:'category',name:'Accessorials',active:true,accessorials:true},{id:'bonus-type',kind:'name',categoryId:'accessorial-category',name:'DOT inspection bonus',active:true}], categories: [], paymentTypes:['EFS','Cash','Reimburse'], expenseTypes:[], paidBy:['Test'], coveredBy:[], filters:{drivers:[],trucks:[],paymentTypes:[],paidBy:[],coveredBy:[]} };
    const errors = [];
    const expenseReads = []; const expensiveLookups=[];
    const costReads = []; let costFailure = true; let payWrites = 0;
    const page = await browser.newPage({ viewport: { width: 1800, height: 1000 } });
    page.on('pageerror', e => { errors.push(e.message); console.error(e); });
    await page.route('**/api/**', async route => {
      const request = route.request(), path = new URL(request.url()).pathname.replace('/lookups/','/').slice(4);
      if (path.endsWith('/cost-transactions')) {
        costReads.push(path);
        const cost = new URL(request.url()).searchParams.get('cost');
        expect(new URL(request.url()).searchParams.get('weekStart')).toBe(week);
        if (cost === 'toll' && costFailure) { costFailure = false; return route.fulfill({status:500,json:{error:'Try loading tolls again'}}); }
        const carry = driver.edits.costs?.[`${cost}Carry`] ?? '0.00';
        const sourceTotal = driver[`${cost}Total`];
        return route.fulfill({json:{sourceTotal,carry,due:driver.edits.costs?.[`${cost}Due`] ?? sourceTotal,transactions:carry !== '0.00' ? [{id:'old-def',transactionId:'old-relay',date:'2026-09-22',weekStart:'2026-09-21',description:'Pilot · DEF',location:'Dayton, OH',amount:'50.00',remaining:carry,carried:true}] : cost === 'fuel' ? [{id:'diesel',transactionId:'relay-1',date:week,weekStart:week,description:'Pilot · DIESEL',location:'Columbus, OH',amount:'30.00',carried:false},{id:'def',transactionId:'relay-2',date:week,weekStart:week,description:'Pilot · DEF',location:'Columbus, OH',amount:'10.00',carried:false}] : []}});
      }
      // Investor Pay loads options for its page filter independently of expense dialogs.
      if (request.url().includes('/api/lookups/') && !(investor && path === '/investors')) expensiveLookups.push(path);
      if (request.method() === 'GET' && path.startsWith('/expenses/')) expenseReads.push(path);
      if (path === '/expenses/reimbursement') {
        if (request.method() === 'PUT') {
          const input = request.postDataJSON();
          expect(input.baseAmount).toBe('55.25');
          expect(input.updatedAt).toBe(reimbursement.updatedAt);
          reimbursement = { ...reimbursement, ...input, amount: '55.25', baseAmount: '55.25' };
          const credit=driver.edits.expenseDeductions.find(row=>row.expenseId==='reimbursement'); Object.assign(credit,{amount:'55.25',total:'55.25',available:'55.25',openingBalance:'55.25',expenseDate:input.expenseDate});
        }
        return route.fulfill({ json: reimbursement });
      }
      if (path === '/expenses/expense') {
        if (request.method() === 'PUT') {
          const input=request.postDataJSON(); expect(input.coveredBy).toBe('Broker'); expect(input.brokerLoadNumber).toBe('L0'); expect(input.efsReferenceNumber).toBe('PAYROLL-EFS-100'); expect(input.baseAmount).toBe('634'); expect(input.updatedAt).toBe(expense.updatedAt);
          expense={...expense,...input,amount:'640.00',baseAmount:'634.00',paymentFee:'6.00'}; writes++;
          driver.edits.expenseDeductions[0]={...driver.edits.expenseDeductions[0],name:input.expenseType,expenseDate:input.expenseDate,total:'640.00',available:'640.00',openingBalance:'640.00'};
        }
        return route.fulfill({json:expense});
      }
      if (path === '/expenses/form-choices') return route.fulfill({json:{options,drivers:[{id:'statement',fullName:'Grid Test',truckId:'truck'}],trucks:[{id:'truck',unitNumber:'T1',driverId:'statement',ownerId:'owner',isCompanyOwned:false}],owners:[{id:'owner',fullName:'Fixture owner',isCompany:false}]}});
      if (path === '/expenses' && request.method() === 'POST') {
        createdAccessorials++;
        const input=request.postDataJSON();
        expect(input).toMatchObject({categoryId:'accessorial-category',driverId:'statement',expenseType:'DOT inspection bonus',paymentType:'Driver payroll',coveredBy:'Truck Owner',ownerId:'owner',amount:'150',expenseDate:'2026-10-01'});
        expect(input.weekStart).toBeUndefined();
        expect(request.headers()['idempotency-key']).toMatch(/^[a-f0-9-]{36}$/);
        driver.edits.expenseDeductions.push({source:'accessorial',expenseId:'new-accessorial',name:input.expenseType,expenseDate:input.expenseDate,amount:'150.00',total:'150.00',available:'150.00',openingBalance:'150.00',remaining:'0.00',version:1,saved:false});
        return route.fulfill({status:201,json:{id:'new-accessorial',...input}});
      }
      if (path === '/expenses') return route.fulfill({json:{items:[],total:0,page:1,pageSize:1,totalPages:1,options}});
      if (request.method() === 'PUT') {
        payWrites++;
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
        '/auth/session': { user: { id: 'user', username: 'Test', expenseCategoryAccess:[{categoryId:'category',canView:true,canEdit:writable,canCreate:true},{categoryId:'accessorial-category',accessorials:true,canView:true,canCreate:writable}], permissions: ['driver_pay.read','investor_pay.read','dispatcher_pay.read', 'driver_pay.edit','investor_pay.edit', 'driver_pay.finalize','driver_pay.reopen','investor_pay.finalize','investor_pay.reopen', 'drivers.read','trucks.read','dispatchers.read','updaters.read','investors.read'], theme: 'solarized-dark' }, csrfToken: 'fixture' },
        '/driver-pay': { weekStart: week, revision: 'fixture', drivers: [driver] },
        '/investor-pay': { weekStart: week, revision: 'fixture', drivers: [driver] },
        '/tasks/count': { count: 0 },
        '/maintenance/options': { vendors: [], types: [] },
      };
      return route.fulfill({ json: fixtures[path.replace(/\/statements\/[^/]+$/, '')] ?? [] });
    });
    await page.goto(`${base}/accounting/${investor ? 'investor' : 'driver'}-pay?weekStart=${week}&${investor ? 'truckId' : 'driverId'}=statement`);
    const region = page.getByRole('region', { name: 'Grid Test reimbursements and charges' });
    await expect(region).toBeVisible();
    const group = region.getByRole('button', { name: 'Grid Test, CAT Scale, 3 expenses', exact: true });
    await expect(group).toHaveAttribute('aria-expanded', 'false');
    await expect(group.getByLabel('CAT Scale total')).toHaveText('-$27.25');
    await expect(region.getByRole('textbox', { name: /CAT Scale.*deduction/ })).toHaveCount(0);
    const writesBeforeToggle = payWrites;
    await group.focus(); await page.keyboard.press('Enter');
    await expect(group).toHaveAttribute('aria-expanded', 'true');
    await expect(region.getByRole('textbox', { name: /CAT Scale.*deduction/ })).toHaveCount(3);
    const second = region.getByRole('textbox', { name: 'Grid Test, CAT Scale second ticket, deduction', exact: true });
    await second.fill('5'); await second.press('Tab');
    await expect(group.getByLabel('CAT Scale total')).toHaveText('-$20.25');
    await expect.poll(() => payWrites).toBe(writesBeforeToggle + 1);
    await expect.poll(() => driver.edits.scaleDeductions['weighmytruck:second']).toBe('-5');
    await group.click(); await expect(group).toHaveAttribute('aria-expanded','false');
    await group.press('Space'); await expect(second).toHaveValue('-5.00');
    await region.getByRole('button',{name:'Grid Test, CAT Scale second ticket, reset to amount due'}).click();
    await expect(group.getByLabel('CAT Scale total')).toHaveText('-$27.25');
    await expect.poll(() => payWrites).toBe(writesBeforeToggle + 2);
    await expect.poll(() => driver.edits.scaleDeductions['weighmytruck:second']).toBe('-12.00');
    await page.screenshot({ path: join(tmpdir(), investor ? 'cat-scale-investor-expanded.png' : 'cat-scale-driver-expanded.png') });
    driver.autoCharges = driver.autoCharges.filter(row => row.source !== 'weighmytruck:second');
    driver.deferredScaleCharges = [];
    delete driver.edits.scaleDeductions['weighmytruck:second']; delete driver.edits.scaleDeductions['weighmytruck:deferred'];
    await page.reload();
    await expect(group).toHaveCount(0);
    const scale = region.getByRole('textbox', { name: `Grid Test, ${scaleTicket.name}, deduction`, exact: true });
    const scaleRow = region.getByRole('row').filter({ has: page.getByText(scaleTicket.name, { exact: true }) });
    await expect(scale).toHaveValue('-15.25');
    const saveScale = async (action, amount) => {
      const response = page.waitForResponse(response => response.request().method() === 'PUT' && response.ok());
      await action();
      expect((await response).request().postDataJSON().scaleDeductions[scaleTicket.source]).toBe(amount);
    };
    const enterScale = async amount => {
      await scale.fill(amount);
      await scale.press('Tab');
    };
    await saveScale(() => enterScale('5.25'), '-5.25');
    await expect(scaleRow.getByText('Left: $10.00')).toBeVisible();
    await saveScale(() => scaleRow.getByRole('button', { name: /reset to amount due/ }).click(), '-15.25');
    await expect(scale).toHaveValue('-15.25');
    await expect(scaleRow.getByText('Left: $0.00')).toBeVisible();
    await saveScale(() => enterScale('0'), '-0');
    driver.edits.scaleDeductions[scaleTicket.source] = '0.00';
    driver.autoCharges = driver.autoCharges.filter(row => row.source !== scaleTicket.source);
    driver.deferredScaleCharges = [scaleTicket];
    await page.reload();
    await expect(scale).toHaveValue('0.00');
    await expect(scaleRow.getByText('Left: $15.25')).toBeVisible();
    await expect(page.getByLabel('Broker reimbursement reminders')).toContainText('Broker reimbursement expected · Load L0 · $100.00');
    await page.screenshot({path:join(tmpdir(),investor ? 'mserp-broker-investor-reminder.png':'mserp-broker-driver-reminder.png'),fullPage:true,animations:'disabled'});
    expect(costReads).toEqual([]);
    const writesBeforeDetails = payWrites;
    await region.getByRole('button',{name:'Grid Test, Fuel transactions',exact:true}).click();
    let costsDialog=page.getByRole('dialog',{name:'Fuel transactions',exact:true});
    await expect(costsDialog.getByText('Pilot · DIESEL',{exact:true})).toBeVisible();
    await expect(costsDialog.getByText('Pilot · DEF',{exact:true})).toBeVisible();
    await expect(costsDialog.getByText('This week')).toContainText('$40.00');
    await page.keyboard.press('Escape');
    await expect(costsDialog).toHaveCount(0);
    await region.getByRole('button',{name:'Grid Test, Toll transactions',exact:true}).click();
    costsDialog=page.getByRole('dialog',{name:'Toll transactions',exact:true});
    await expect(costsDialog.getByRole('alert')).toContainText('Try loading tolls again');
    await costsDialog.getByRole('button',{name:'Try again',exact:true}).click();
    await expect(costsDialog.getByText('No toll transactions for this statement.',{exact:true})).toBeVisible();
    await costsDialog.getByRole('button',{name:'Close',exact:true}).last().click();
    expect(payWrites).toBe(writesBeforeDetails);
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

    const choice = region.getByRole('combobox', {name:'Grid Test, expense Equipment, deduction choice'});
    const deduction = region.getByRole('textbox', {name:'Grid Test, expense Equipment, deduction'});
    await expect(choice).toHaveCount(0);
    const enterDeduction = async (amount, expected = amount.replace(/^-/, '')) => {
      const [saved] = await Promise.all([
        page.waitForResponse(r => r.request().method() === 'PUT' && new URL(r.url()).pathname === '/api/' + (investor ? 'investor-pay' : 'driver-pay')),
        (async () => { await deduction.fill(amount); await deduction.press('Tab'); })(),
      ]);
      const input = saved.request().postDataJSON().expenseDeductions.find(row => row.expenseId === 'expense');
      expect(input.mode).toBe('fixed'); expect(input.apply).toBe(true); expect(input.amount).toBe(expected);
      expect(expense.amount).toBe('100.00');
    };
    const finalize = page.getByRole('button', { name: /^Finalize Grid Test/ });
    const deductionRow = region.getByRole('row').filter({ has: page.getByRole('textbox', { name: 'Grid Test, expense Equipment, deduction' }) });
    const beforeInvalid = payWrites;
    await deduction.fill('100.01');
    await deduction.press('Tab');
    await expect(deductionRow.getByText('Available: $100.00')).toBeVisible();
    await expect(page.getByRole('alert').filter({ hasText: 'Equipment: enter an amount from zero to $100.00' })).toBeVisible();
    await expect(finalize).toBeDisabled();
    expect(payWrites).toBe(beforeInvalid);
    await enterDeduction(' 0.00 ', '0.00');
    await expect(deduction).toHaveValue('0.00');
    await expect(deductionRow.getByText('Left: $100.00')).toBeVisible();
    await expect(finalize).toBeEnabled();
    await enterDeduction('-.50', '0.50');
    await expect(deductionRow.getByText('Left: $99.50')).toBeVisible();
    await enterDeduction('-100.00');
    await enterDeduction('-25.00');

    if (!investor) {
      const reimbursementRow = region.getByRole('row').filter({ has: page.getByRole('button', { name: 'Open expense Parking refund', exact: true }) });
      await expect(reimbursementRow.getByText('09/30', { exact: true })).toBeVisible();
      await expect(reimbursementRow.getByRole('textbox')).toHaveValue('+42.25');
      await expect(reimbursementRow.getByRole('textbox')).toHaveClass(/text-emerald-300/);
      await reimbursementRow.getByRole('button').click();
      const reimbursementModal = page.getByRole('dialog', { name: 'Edit entry' });
      await expect(reimbursementModal.getByLabel('Amount', { exact: true })).toHaveValue('42.25');
      await expect(reimbursementModal.getByLabel('Payment method', { exact: true })).toHaveValue('Reimburse');
      await reimbursementModal.getByLabel('Amount', { exact: true }).fill('55.25');
      await reimbursementModal.getByLabel('Expense date', { exact: true }).fill('2026-10-01');
      await reimbursementModal.getByRole('button', { name: 'Save changes', exact: true }).click();
      await expect(reimbursementModal).toHaveCount(0);
      await expect(reimbursementRow.getByText('10/01', { exact: true })).toBeVisible();
      await expect(reimbursementRow.getByRole('textbox')).toHaveValue('+55.25');
    }

    await expect(region.getByText('09/28', {exact:true})).toBeVisible();
    await region.getByRole('button',{name:'Open expense Equipment',exact:true}).click();
    let modal=page.getByRole('dialog',{name:'Edit entry'});
    await expect(modal).toBeVisible();
    await modal.getByLabel('Who will cover',{exact:true}).selectOption('Broker');
    await expect(modal.getByLabel('Broker load number')).toBeVisible();
    expect(await modal.getByLabel('Broker load number').evaluate(e=>e.checkValidity())).toBe(false);
    await modal.getByLabel('Broker load number').fill('L0');
    await modal.getByLabel('Payment method',{exact:true}).selectOption('EFS');
    await modal.getByLabel('EFS reference number', {exact:true}).fill('PAYROLL-EFS-100');
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
    await expect(modal.getByLabel('Broker load number')).toHaveValue('L0');
    await expect(modal.getByLabel('Amount',{exact:true})).toHaveValue('634.00');
    await expect(modal.getByLabel('EFS reference number',{exact:true})).toHaveValue('PAYROLL-EFS-100');
    await expect(modal.getByRole('status')).toHaveText('EFS fee: $6.00 · Total: $640.00');
    await modal.getByLabel('Broker load number').scrollIntoViewIfNeeded();
    await page.screenshot({path:join(tmpdir(), investor ? 'mserp-broker-investor-expense.png':'mserp-broker-driver-expense.png'),fullPage:true,animations:"disabled"});
    await modal.getByRole('button',{name:'Cancel',exact:true}).click();
    if (!investor) {
      const accessorialButton = page.locator('[data-pay-extra-row]').getByRole('button', {name:'Add accessorial',exact:true});
      await expect(accessorialButton).toBeVisible();
      const buttonBox = await accessorialButton.boundingBox();
      const cellBox = await accessorialButton.locator('..').boundingBox();
      const totalsBox = await page.getByRole('cell', {name:'Net adjustments',exact:true}).boundingBox();
      expect(buttonBox.x).toBeCloseTo(totalsBox.x, 0);
      expect(cellBox.width - buttonBox.width).toBeLessThanOrEqual(1);
      expect(Math.abs(buttonBox.y + buttonBox.height - totalsBox.y)).toBeLessThanOrEqual(1);
      await accessorialButton.press('Enter');
      const add = page.getByRole('dialog',{name:'Add accessorial'});
      await expect(add.getByLabel('Driver',{exact:true})).toHaveValue('statement');
      await expect(add.getByLabel('Who will cover',{exact:true})).toHaveValue('Truck Owner');
      await expect(add.getByText('Driver payroll',{exact:true})).toBeVisible();
      await expect(add.getByLabel(/payroll week/i)).toHaveCount(0);
      await add.getByLabel('Expense date',{exact:true}).fill('2026-10-01');
      await add.getByLabel('Expense type',{exact:true}).selectOption('DOT inspection bonus');
      await add.getByLabel('Amount',{exact:true}).fill('150');
      await expect(add.getByRole('status')).toContainText('Owner charge (Fixture owner): −$150');
      await add.getByRole('button',{name:'Save accessorial',exact:true}).click();
      await expect(add).toHaveCount(0);
      expect(createdAccessorials).toBe(1);
      await expect(region.getByRole('textbox',{name:'Grid Test, expense DOT inspection bonus, accessorial credit'})).toHaveValue('+150.00');
    } else await expect(page.getByRole('button',{name:'Add accessorial',exact:true})).toHaveCount(0);
    writable=false; await page.reload();
    await expect(page.getByRole('button',{name:'Add accessorial',exact:true})).toHaveCount(0);
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
    expect(expensiveLookups).toEqual([]);
    expect(expenseReads.every(path => ['/expenses/expense', '/expenses/expense/documents', '/expenses/expense/external-settlements', '/expenses/reimbursement', '/expenses/reimbursement/documents', '/expenses/reimbursement/external-settlements', '/expenses/form-choices'].includes(path))).toBe(true);
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
    await expect(fuelRow.getByText('+ carry',{exact:true})).toBeVisible();
    await fuelRow.getByRole('button',{name:'Grid Test, Fuel transactions',exact:true}).click();
    costsDialog=page.getByRole('dialog',{name:'Fuel transactions',exact:true});
    await expect(costsDialog.getByText('Carried from 09/21',{exact:true})).toBeVisible();
    await expect(costsDialog.getByText('$25.00',{exact:true})).toHaveCount(3);
    await costsDialog.screenshot({path:join(tmpdir(),`payroll-cost-${investor ? 'investor' : 'driver'}.png`)});
    await page.setViewportSize({width:390,height:844});
    expect(await page.evaluate(()=>document.documentElement.scrollWidth<=window.innerWidth)).toBe(true);
    await costsDialog.getByRole('button',{name:'Close',exact:true}).last().click();
    await page.setViewportSize({width:1800,height:1000});
    await expect(fuelRow.getByText('Left: $15.00', { exact: true })).toBeVisible();
    driver.edits.fuelOverride = null;
    await page.reload();
    await expect(fuelRow.getByText('Left: $0.00', { exact: true })).toBeVisible();
    writable = true;
    driver.settlement={finalized:true,version:1,finalizedAt:'2026-10-01T00:00:00Z',finalizedBy:'Test',reason:''};
    await page.reload();
    await expect(fuelRow.getByRole('textbox')).toBeDisabled();
    await expect(scale).toBeDisabled();
    await expect(page.getByRole('button',{name:'Add accessorial',exact:true})).toHaveCount(0);
    await fuelRow.getByRole('button',{name:'Grid Test, Fuel transactions',exact:true}).click();
    costsDialog=page.getByRole('dialog',{name:'Fuel transactions',exact:true});
    await expect(costsDialog.getByText('Carried from 09/21',{exact:true})).toBeVisible();
    await costsDialog.getByRole('button',{name:'Close',exact:true}).last().click();
    driver.autoCharges.push({ ...scaleTicket, source:'weighmytruck:frozen',name:'CAT Scale frozen ticket',amount:'-6.00',scaleDue:'-12.00' });
    await page.reload();
    const frozenGroup = region.getByRole('button',{name:'Grid Test, CAT Scale, 2 expenses'});
    await expect(frozenGroup).toBeEnabled();
    await expect(frozenGroup.getByLabel('CAT Scale total')).toHaveText('-$6.00');
    await frozenGroup.click();
    await expect(region.getByRole('textbox',{name:'Grid Test, CAT Scale frozen ticket, deduction',exact:true})).toBeDisabled();
    await expect(region.getByRole('textbox',{name:'Grid Test, CAT Scale frozen ticket, deduction',exact:true})).toHaveValue('-6.00');
    await expect(region.getByRole('button',{name:/CAT Scale.*reset to amount due/})).toHaveCount(0);
    await frozenGroup.click();
    await expect(region.getByRole('textbox',{name:/CAT Scale.*deduction/})).toHaveCount(0);
    expect(errors).toEqual([]); await page.close();
  }
  console.log('Driver and Investor Pay expense dates, editing, fee boundaries, refresh and view-only access passed.');
} finally { await browser.close(); await new Promise(resolve=>server.close(resolve)); }
