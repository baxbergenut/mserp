import { chromium, expect } from '@playwright/test';
import { createServer } from 'node:http';
import { readFile } from 'node:fs/promises';
import { extname, join } from 'node:path';
import { tmpdir } from 'node:os';

const drivers = [
  { id: 'hired', fullName: 'Hired Driver', truckId: 'investor-truck', active: true, isOwnerOperator: false },
  { id: 'owner-driver', fullName: 'Owner Driver', truckId: 'own-truck', active: true, isOwnerOperator: true },
  { id: 'company-driver', fullName: 'Company Driver', truckId: 'company-truck', active: true, isOwnerOperator: true },
];
const owners = [
  { id: 'company', fullName: 'MS Express', isCompany: true },
  { id: 'investor', fullName: 'Truck Investor', isCompany: false },
  { id: 'driver-owner', fullName: 'Owner Driver', driverId: 'owner-driver', isCompany: false },
  { id: 'unrelated', fullName: 'Unrelated Investor', isCompany: false },
];
const trucks = [
  { id: 'investor-truck', unitNumber: '101', ownerId: 'investor', driverId: 'hired', isCompanyOwned: false },
  { id: 'own-truck', unitNumber: '102', ownerId: 'driver-owner', driverId: 'owner-driver', isCompanyOwned: false },
  { id: 'company-truck', unitNumber: '103', ownerId: 'company', driverId: 'company-driver', isCompanyOwned: true },
];
const maintenanceNames = ['Electrical issue','Towing','PM service','Truck wash','Detailing','Road service','Body work','Leakage','Brake issue','Tire issue','Oil change'];
const settings = [{ id: 'maintenance', name: 'Maintenance', kind: 'category', active: true }, ...maintenanceNames.map((name,i)=>({id:'name-'+i, name,kind:'name',categoryId:'maintenance',active:true}))];
settings.push({id:'penalties',name:'Penalties',kind:'category',active:true,driverOnly:true});
const blankDraft = { company: 'Wrong company', categoryId: 'maintenance', expenseDate: '2026-09-28', truckId: 'investor-truck', driverId: 'hired', unitNumber: '101', driverName: 'Hired Driver', amount: '12.50', paymentType: '', expenseType: 'Repair', referenceNumber: '', description: '', coveredBy: 'Company', paidBy: 'Invented payer' };
let drafts = [{...blankDraft,costBreakdown:[{name:'Oil change',amount:'10.10'},{name:'Shop supplies',amount:'2.40'}]}];
const writes = [];
let entries = [], verificationRole = "admin";
const verified = [];
const cancelled = [];
const server = createServer(async (req, res) => {
  try {
    const path = new URL(req.url, 'http://localhost').pathname;
    const file = path === '/' ? 'index.html' : extname(path) ? path.slice(1) : `${path.slice(1)}.html`;
    const body = await readFile(new URL(`../out/${file}`, import.meta.url));
    res.writeHead(200, { 'Content-Type': { '.html': 'text/html', '.js': 'application/javascript', '.css': 'text/css' }[extname(file)] ?? 'application/octet-stream' }); res.end(body);
  } catch { res.writeHead(404); res.end(); }
});
await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
let browser;
try {
  browser = await chromium.launch();
  const page = await browser.newPage({ viewport: { width: 1280, height: 1000 } });
  const errors = [];
  page.on('pageerror', error => errors.push(error.message));
  await page.route('**/api/**', async route => {
    const path = new URL(route.request().url()).pathname.slice(4);
    if (route.request().method() === 'POST' && ['/expenses', '/expenses/bulk'].includes(path)) {
      writes.push(route.request().postDataJSON()); await route.fulfill({ status: 201, json: {} }); return;
    }
    if (path.endsWith('/verify')) {
      const id = path.split('/')[2];
      const item = entries.find(e => e.id === id);
      expect(route.request().postDataJSON()).toEqual({ updatedAt: item.updatedAt });
      item[verificationRole === 'admin' ? 'managerVerified' : 'accountingVerified'] = true;
      item.verifications.push({ id: 'verification-' + verified.length, role: verificationRole, userId: 'user', userName: 'Entry User', verifiedAt: '2026-10-13T12:00:00Z' });
      verified.push(id);
      await route.fulfill({ json: item }); return;
    }
    if (path.endsWith('/unverify')) {
      const item = entries.find(e => e.id === path.split('/')[2]);
      const id = route.request().postDataJSON().verificationId;
      const stamp = item.verifications.find(v => v.id === id);
      item[stamp.role === 'admin' ? 'managerVerified' : 'accountingVerified'] = false;
      item.verifications = item.verifications.filter(v => v.id !== id);
      cancelled.push(id);
      await route.fulfill({ json: item }); return;
    }
    const q = new URL(route.request().url()).searchParams;
    const filtered = entries.filter(e => ['unitNumber','driverName','paymentType','paidBy','coveredBy'].every(key => !q.get(key) || e[key] === q.get(key)) && (!q.get('accountantVerified') || e.accountingVerified === (q.get('accountantVerified') === 'true')) && (!q.get('adminVerified') || e.managerVerified === (q.get('adminVerified') === 'true')));
    const fixtures = {
      '/auth/session': { user: { id: 'user', username: 'Entry User', expenseVerificationRole: verificationRole, permissions: ['expenses.read', 'fleet.read'], expenseCategoryAccess: [{ categoryId: 'maintenance', canRead: true, canCreate: true, canEdit: true, canDelete: true },{categoryId:'penalties',canRead:true,canCreate:true,canEdit:true,canDelete:false}] }, csrfToken: 'fixture' },
      '/expenses': { items: filtered, total: filtered.length, page: 1, pageSize: 25, totalPages: 1, summary: { amount: String(filtered.reduce((sum, e) => sum + Number(e.amount), 0)), incompleteCount: 0 }, options: { settings, categories: settings, filters: Object.fromEntries(Object.entries({ drivers: 'driverName', trucks: 'unitNumber', paymentTypes: 'paymentType', paidBy: 'paidBy', coveredBy: 'coveredBy' }).map(([key, field]) => [key, [...new Set(entries.map(e => e[field]).filter(Boolean))]])), paymentTypes: [], expenseTypes: [], paidBy: ['Entry User', 'Other User'], coveredBy: [] } },
      '/drivers': drivers, '/trucks': trucks, '/investors': owners, '/expenses/extract': { expenses: drafts },
    };
    await route.fulfill({ json: fixtures[path] ?? {} });
  });
  await page.goto(`http://127.0.0.1:${server.address().port}/expenses`);
  await page.getByRole('button', { name: 'Add entry', exact: true }).click();
  await expect(page.getByLabel('Paid by', { exact: true })).toHaveValue('Entry User');
  await expect(page.getByText('Company', { exact: true }).filter({ visible: true })).toHaveCount(0);
  await expect(page.getByLabel('Responsible truck owner')).toHaveCount(0);
  await page.getByLabel('Unit number', { exact: true }).selectOption('investor-truck');
  await expect(page.getByLabel('Who will cover')).toHaveValue('Truck Owner');
  await page.getByLabel('Expense date', { exact: true }).fill('2026-09-28');
  await expect(page.getByLabel('Who will cover')).toHaveValue('Truck Owner');
  await expect(page.getByLabel('Who will cover').locator('option')).toHaveText(['Company', 'Driver', 'Truck Owner · Truck Investor']);
  await page.getByLabel('Unit number', { exact: true }).selectOption('own-truck');
  await expect(page.getByLabel('Who will cover')).toHaveValue('Driver');
  await expect(page.getByLabel('Who will cover').locator('option')).toHaveText(['Company', 'Driver']);
  await page.getByLabel('Driver', { exact: true }).selectOption('company-driver');
  await expect(page.getByLabel('Who will cover')).toHaveValue('Company');
  await page.getByLabel('Unit number', { exact: true }).selectOption('investor-truck');
  await expect(page.getByLabel('Expense type', { exact: true }).locator('option')).toHaveText(['Select expense type',...maintenanceNames,'Custom expense type…']);
  await page.getByLabel('Expense type', { exact: true }).selectOption('Oil change');
  await page.getByRole('button', {name:'Add cost',exact:true}).click();
  await page.getByLabel('Cost breakdown 1 name',{exact:true}).selectOption('Oil change');
  await page.getByLabel('Cost breakdown 1 amount',{exact:true}).fill('10.10');
  await page.getByRole('button', {name:'Add cost',exact:true}).click();
  await page.getByLabel('Cost breakdown 2 name',{exact:true}).selectOption('__custom');
  await page.getByLabel('Cost breakdown 2 name custom',{exact:true}).fill('Shop supplies');
  await page.getByLabel('Cost breakdown 2 amount',{exact:true}).fill('2.39');
  await page.getByLabel('Amount', { exact: true }).fill('12.50');
  await page.getByRole('button',{name:'Create entry',exact:true}).click();
  expect(writes).toHaveLength(0);
  await expect(page.getByText('Cost breakdown total must exactly equal the entered amount.',{exact:true})).toBeVisible();
  await page.getByLabel('Cost breakdown 2 amount',{exact:true}).fill('2.40');
  await expect(page.getByText('Matches entered amount',{exact:true})).toBeVisible();
  await page.getByLabel('Expense type', { exact: true }).selectOption('__custom');
  await page.getByLabel('Expense type custom', { exact: true }).pressSequentially('Oil change extra');
  await expect(page.getByLabel('Expense type custom', {exact:true})).toHaveValue('Oil change extra');
  await page.getByLabel('Expense type custom', { exact: true }).fill('Repair');
  const transfer = await page.evaluateHandle(() => { const data = new DataTransfer(); data.items.add(new File(['receipt'], 'receipt.txt', { type: 'text/plain' })); return data; });
  await page.getByLabel('Expense details for AI').dispatchEvent('drop', { dataTransfer: transfer });
  await expect(page.getByText('receipt.txt', { exact: true })).toBeVisible();
  const browse = await page.getByRole('button', { name: 'Browse', exact: true }).boundingBox();
  const analyze = await page.getByRole('button', { name: 'Analyze', exact: true }).boundingBox();
  expect(Math.abs(browse.y - analyze.y)).toBeLessThan(3);
  await page.getByRole('button', { name: 'Analyze', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Analyze', exact: true })).toHaveCount(0);
  await expect(page.getByLabel('Paid by', { exact: true })).toHaveValue('Entry User');
  await expect(page.getByLabel('Who will cover')).toHaveValue('Truck Owner');
  await page.screenshot({ path: join(tmpdir(), 'mserp-expense-modal.png'), fullPage: true });
  await page.getByRole('button', { name: 'Create entry', exact: true }).click();
  await expect.poll(() => writes.length).toBe(1);
  expect(writes[0]).not.toHaveProperty('company');
  expect(writes[0].costBreakdown).toEqual([{name:'Oil change',amount:'10.10'},{name:'Shop supplies',amount:'2.40'}]);
  expect(writes[0]).toMatchObject({ coveredBy: 'Truck Owner', ownerId: 'investor', paidBy: 'Entry User' });
  await page.getByRole('button', { name: 'Add entry', exact: true }).click();
  drafts = [{...blankDraft,costBreakdown:[{name:'Tire issue',amount:'12.50'}]}, { ...blankDraft, truckId: 'own-truck', driverId: 'owner-driver' }];
  await page.getByLabel('Expense details for AI').fill('two transactions');
  await page.getByRole('button', { name: 'Analyze', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Analyze', exact: true })).toHaveCount(0);
  await expect(page.getByLabel('Who will cover')).toHaveValue('Truck Owner');
  await page.getByRole('button',{name:'Save & review next',exact:true}).click();
  await expect(page.getByText('Review entry 2 of 2. Each item is saved separately.',{exact:true})).toBeVisible();
  await expect(page.getByLabel('Who will cover')).toHaveValue('Driver');
  await page.getByLabel('Expense type',{exact:true}).selectOption('__custom');
  await page.getByLabel('Expense type custom',{exact:true}).fill('Custom repair');
  await page.getByLabel('Paid by',{exact:true}).selectOption('Other User');
  await page.getByRole('button',{name:'Create entry',exact:true}).click();
  await expect.poll(()=>writes.length).toBe(3);
  expect(writes[1]).not.toHaveProperty('company');expect(writes[2]).not.toHaveProperty('company');
  expect(writes[1]).toMatchObject({ownerId:'investor',paidBy:'Entry User',costBreakdown:[{name:'Tire issue',amount:'12.50'}]});
  expect(writes[2]).toMatchObject({coveredBy:'Driver',ownerId:null,paidBy:'Other User'});
  await page.setViewportSize({ width: 390, height: 844 });
  await page.getByRole('button', { name: 'Add entry', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Browse', exact: true })).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  await page.getByRole('button', { name: 'Close', exact: true }).click();
  entries = Array.from({ length: 4 }, (_, i) => ({ ...blankDraft, company: 'MS Express', id: 'expense-' + i, expenseType: 'Repair ' + i, category: 'Maintenance', managerVerified: i === 2 || i === 3, accountingVerified: i === 1 || i === 3, verifications: [...(i === 1 || i === 3 ? [{ id: 'accountant-' + i, role: 'accountant', userId: 'other-accountant', userName: 'Avery Accountant', verifiedAt: '2026-10-12T11:00:00Z' }] : []), ...(i === 2 || i === 3 ? [{ id: 'admin-' + i, role: 'admin', userId: 'other-admin', userName: 'Alex Administrator', verifiedAt: '2026-10-12T12:00:00Z' }] : [])], updatedAt: '2026-10-12T12:00:00Z', paidAmount: '5.00', remainingAmount: '7.50' }));
  await page.setViewportSize({ width: 1600, height: 1000 });
  await page.reload();
  await expect(page.getByRole('columnheader')).toHaveCount(7);
  await expect(page.getByRole('columnheader').getByRole('button', { name: 'Filter Verification', exact: true })).toBeVisible();
  const row = page.getByRole('row', { name: 'Open Repair 0', exact: true });
  await expect(row).toBeVisible();
  const add = await page.getByRole('button', { name: 'Add entry', exact: true }).boundingBox();
  const search = await page.getByPlaceholder('Search expenses…').boundingBox();
  expect(Math.abs(add.y - search.y)).toBeLessThan(3);
  expect(add.y).toBeGreaterThan((await page.getByText('Filtered total', { exact: true }).boundingBox()).y);
  await expect(row.getByText('$12.50')).toHaveAttribute('title', 'Paid: $5.00\nRemaining: $7.50');
  await page.evaluate(() => Promise.all(document.getAnimations().filter(animation => animation.effect?.getComputedTiming().iterations !== Infinity).map(animation => animation.finished.catch(() => {}))));
  const tableBefore = await page.locator('table').boundingBox();
  await row.getByRole('button', { name: 'Verify', exact: true }).click();
  await expect(row.getByRole('img', { name: 'Administrator: Entry User', exact: true })).toBeVisible();
  await expect(page.getByRole('dialog')).toHaveCount(0);
  const notice = page.getByRole('status').filter({ hasText: 'Verified. Press Ctrl+Z to undo.' });
  await expect(notice).toBeVisible();
  expect((await page.locator('table').boundingBox()).y).toBe(tableBefore.y);
  expect(Math.abs((await notice.boundingBox()).y - (await page.getByRole('button', { name: 'Add entry', exact: true }).boundingBox()).y)).toBeLessThan(4);
  const accountantRow = page.getByRole('row', { name: 'Open Repair 1', exact: true });
  await expect(accountantRow.getByRole('img', { name: 'Accountant: Avery Accountant', exact: true })).toBeVisible();
  await accountantRow.getByRole('button', { name: 'Verify', exact: true }).click();
  await expect(accountantRow.getByRole('img', { name: 'Accountant: Avery Accountant; Administrator: Entry User' })).toBeVisible();
  await page.keyboard.press('Control+z');
  await expect(accountantRow.getByRole('img', { name: 'Accountant: Avery Accountant', exact: true })).toBeVisible();
  await expect(accountantRow.getByRole('button', { name: 'Verify', exact: true })).toBeVisible();
  expect(cancelled).toHaveLength(1);
  await row.focus(); await page.keyboard.press('Enter');
  await expect(page.getByRole('dialog', { name: 'Edit entry' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Delete entry' })).toBeVisible();
  await expect(page.getByText('Manager verified', { exact: true })).toHaveCount(0);
  await page.getByRole('button', { name: 'Close', exact: true }).click();
  verificationRole = 'accountant'; await page.reload();
  await page.getByRole('row', { name: 'Open Repair 2', exact: true }).getByRole('button', { name: 'Verify', exact: true }).click();
  await expect(page.getByRole('row', { name: 'Open Repair 2', exact: true }).getByRole('img', { name: 'Administrator: Alex Administrator; Accountant: Entry User' })).toBeVisible();
  await accountantRow.getByRole('button', { name: 'View verification: Accountant: Avery Accountant', exact: true }).click();
  await page.getByRole('button', { name: 'Remove Accountant verification', exact: true }).click();
  await expect(page.getByText('No active verifications.', { exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Done', exact: true }).click();
  await row.getByRole('button', { name: 'View verification: Administrator: Entry User', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Remove Administrator verification', exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Done', exact: true }).click();
  await page.getByRole('row', { name: 'Open Repair 3', exact: true }).getByRole('button', { name: /View verification:/ }).click();
  await expect(page.getByRole('button', { name: 'Remove Administrator verification', exact: true })).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Remove Accountant verification', exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Done', exact: true }).click();
  await page.getByRole('button', { name: 'Add entry', exact: true }).click();
  await expect(page.getByPlaceholder(/Enter an unlinked/)).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Analyze', exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Close', exact: true }).click();
  await page.getByRole('button', { name: 'Filter Verification', exact: true }).click();
  await page.getByLabel('Accountant verification', { exact: true }).selectOption('true');
  await expect(page.getByRole('row', { name: /^Open Repair/ })).toHaveCount(2);
  await page.getByLabel('Admin verification', { exact: true }).selectOption('false');
  await expect(page.getByText('No entries match these filters.', { exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Filter Verification', exact: true })).toBeVisible();
  await page.getByLabel('Accountant verification', { exact: true }).selectOption('false');
  await expect(page.getByRole('row', { name: 'Open Repair 1', exact: true })).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(page.getByLabel('Accountant verification', { exact: true })).not.toBeVisible();
  await page.getByRole('row', { name: 'Open Repair 1', exact: true }).getByRole('button', { name: 'Verify', exact: true }).click();
  await expect(page.getByText('No entries match these filters.', { exact: true })).toBeVisible();
  await page.keyboard.press('Control+z');
  await expect(page.getByRole('row', { name: 'Open Repair 1', exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Clear', exact: true }).click();
  await expect(page.getByRole('row', { name: /^Open Repair/ })).toHaveCount(4);
  entries[3] = { ...entries[3], unitNumber: '102', driverName: 'Owner Driver', paymentType: 'Retired card', paidBy: 'Former Payer', coveredBy: 'Broker' };
  await page.reload();
  for (const [header, label, value] of [['Unit / driver','Expense truck','102'],['Unit / driver','Expense driver','Owner Driver'],['Payment','Expense payment type','Retired card'],['Payment','Expense paid by','Former Payer'],['Responsibility','Expense responsibility','Broker']]) {
    await page.getByRole('button', { name: 'Filter ' + header, exact: true }).click();
    await page.getByLabel(label, { exact: true }).selectOption(value);
    await expect(page.getByRole('row', { name: /^Open Repair/ })).toHaveCount(1);
    await expect(page.getByRole('row', { name: 'Open Repair 3', exact: true })).toBeVisible();
    await page.keyboard.press('Escape');
    await page.getByRole('button', { name: 'Clear', exact: true }).click();
    await expect(page.getByRole('row', { name: /^Open Repair/ })).toHaveCount(4);
  }
  verificationRole = ''; await page.reload();
  await expect(page.getByRole('button', { name: 'Verify', exact: true })).toHaveCount(0);
  await page.setViewportSize({ width: 390, height: 844 });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  expect(verified).toEqual(['expense-0', 'expense-1', 'expense-2', 'expense-1']);
  await page.setViewportSize({width:1280,height:1000});
  await page.getByRole('button',{name:'Add entry',exact:true}).click();
  // Switching away from an EFS expense must discard its hidden payer and fee.
  await page.getByLabel('Payment method',{exact:true}).fill('EFS');
  await page.getByLabel('Paid by',{exact:true}).selectOption('Other User');
  await page.getByLabel('Category',{exact:true}).selectOption('penalties');
  await expect(page.getByLabel('Payment method',{exact:true})).toHaveCount(0);
  await expect(page.getByLabel('Paid by',{exact:true})).toHaveCount(0);
  await expect(page.getByLabel('Who will cover',{exact:true})).toHaveCount(0);
  await expect(page.getByLabel('Driver',{exact:true})).toHaveAttribute('required','');
  await page.getByLabel('Expense type',{exact:true}).selectOption('__custom');
  await page.getByLabel('Expense type custom',{exact:true}).fill('Late delivery');
  await page.getByLabel('Amount',{exact:true}).fill('500.01');
  const beforePenalty=writes.length;
  await page.getByRole('button',{name:'Create entry',exact:true}).click();
  expect(writes.length).toBe(beforePenalty); // Browser validation requires the debtor.
  await page.getByLabel('Driver',{exact:true}).selectOption('hired');
  await page.screenshot({path:join(tmpdir(),'mserp-penalty-modal.png'),fullPage:true});
  await page.getByRole('button',{name:'Create entry',exact:true}).click();
  await expect.poll(()=>writes.length).toBe(beforePenalty+1);
  expect(writes.at(-1)).toMatchObject({categoryId:'penalties',coveredBy:'Driver',driverId:'hired',ownerId:null,paymentType:'',paidBy:'',baseAmount:'500.01'});
  // Descriptions lead the directory; classifications and legacy fallbacks remain visible.
  entries[0].description = 'Replace damaged mirror and inspect mounting';
  entries[1].description = 'Saved invoice text';
  entries[1].workOrderDescription = 'Diagnosed intermittent electrical fault';
  entries[2].description = '   ';
  await page.reload();
  const described = page.getByRole('row', { name: 'Open ' + entries[0].description, exact: true });
  await expect(described.getByText(entries[0].description, { exact: true })).toBeVisible();
  await expect(described.getByText('Maintenance · Repair 0', { exact: true })).toBeVisible();
  await expect(page.getByRole('row', { name: 'Open ' + entries[1].workOrderDescription, exact: true })).toBeVisible();
  await expect(page.getByRole('row', { name: 'Open Repair 2', exact: true })).toBeVisible();
  await described.click();
  await expect(page.getByLabel('Expense type custom', { exact: true })).toHaveValue('Repair 0');
  await expect(page.getByLabel('Description', { exact: true })).toHaveValue(entries[0].description);
  expect((await page.getByLabel('Description', { exact: true }).boundingBox()).y).toBeLessThan((await page.getByLabel('Expense date', { exact: true }).boundingBox()).y);
  await page.getByRole('button', { name: 'Close', exact: true }).click();

  const service = { typeCode: 'electrical', name: 'Electrical', amount: '12.50', trackingCode: '' };
  const workRecords = [0, 1, 2].map(i => ({ id: 'work-' + i, expenseId: 'invoice-' + i, truckId: 'company-truck', unitNumber: '103', driverId: 'company-driver', driverName: 'Company Driver', vendorId: null, vendorName: 'Saved shop', vendorAddress: '', vendorPhone: '', vendorContact: '', serviceDate: '2026-10-01', odometer: 100000, location: 'shop', status: 'completed', workOrder: 'WO-' + i, notes: i === 0 ? 'Diagnosed intermittent electrical fault' : '', services: [service], version: 1, documents: [], oilType: '', expense: { ...entries[0], expenseType: 'Electrical', description: i < 2 ? 'Original invoice text' : '', amount: '12.50', baseAmount: '12.50', paymentFee: '0.00' } }));
  workRecords[1].services = [{...service,amount:'6.25'},{typeCode:'brakes',name:'Brakes',amount:'6.25',trackingCode:''}];
  const legacyServices = structuredClone(workRecords[1].services);
  const workWrites = [];
  await page.route('**/api/maintenance**', async route => {
    const path = new URL(route.request().url()).pathname.slice(4);
    if (route.request().method() === 'PUT') {
      const data = route.request().postDataJSON(); workWrites.push(data);
      const i = workRecords.findIndex(r => r.id === data.id);
      workRecords[i] = { ...workRecords[i], ...data, expense: { ...workRecords[i].expense, ...data.expense }, version: workRecords[i].version + 1 };
      await route.fulfill({ json: workRecords[i] }); return;
    }
    const fixture = {
      '/maintenance/access': { canView: true, canCreate: true, canEdit: true, canDelete: true },
      '/maintenance/options': { categoryId: 'maintenance', types: [{ code: 'electrical', name: 'Electrical', active: true },{code:'brakes',name:'Brakes',active:true}], vendors: [], schedules: [], names: [], paymentMethods: [], users: ['Entry User'], currentUser: 'Entry User', currentUserId: 'user', trucks, drivers: drivers.map(d => ({ id: d.id, name: d.fullName, active: true })), administrator: false, verificationRole: '' },
      '/maintenance/reports': { total: '37.50', fees: '0', serviceTypes: [], trucks: [], vendors: [], repeatedWork: [] },
      '/maintenance': { items: workRecords, total: 3, page: 1, pageSize: 25, totalPages: 1 },
    };
    await route.fulfill({ json: fixture[path] ?? workRecords.find(r => path === '/maintenance/' + r.id) ?? {} });
  });
  await page.goto('http://127.0.0.1:' + server.address().port + '/maintenance');
  const workRow = page.getByRole('row', { name: 'Open maintenance work-0', exact: true });
  await expect(workRow.getByText(workRecords[0].notes, { exact: true })).toBeVisible();
  await expect(page.getByRole('row', { name: 'Open maintenance work-1', exact: true }).getByText('Original invoice text', { exact: true })).toBeVisible();
  await expect(page.getByRole('row', { name: 'Open maintenance work-2', exact: true }).getByText('Electrical', { exact: true })).toHaveCount(2);
  await page.screenshot({ path: join(tmpdir(), 'mserp-work-order-descriptions.png'), fullPage: true, animations: 'disabled' });
  await workRow.click();
  await expect(page.getByLabel('Work order description')).toHaveValue(workRecords[0].notes);
  await expect(page.getByLabel('Service type', { exact: true })).toHaveCount(1);
  await expect(page.getByRole('button', { name: 'Add service', exact: true })).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Add cost', exact: true })).toBeVisible();
  expect((await page.getByLabel('Work order description').boundingBox()).y).toBeLessThan((await page.getByLabel('Maintenance truck', { exact: true }).boundingBox()).y);
  await page.getByRole('button', { name: 'Save work order', exact: true }).click();
  await expect(page.getByRole('dialog')).toHaveCount(0);
  expect(workWrites[0].expense.description).toBe('Original invoice text');
  expect(workWrites[0].notes).toBe('Diagnosed intermittent electrical fault');
  expect(workWrites[0].services).toEqual([service]);
  await workRow.click();
  await page.getByLabel('Work order description').fill('Replaced damaged wiring connector');
  await page.getByRole('button', { name: 'Add cost', exact: true }).click();
  await page.getByLabel('Cost breakdown 1 name', { exact: true }).selectOption('__custom');
  await page.getByLabel('Cost breakdown 1 name custom', { exact: true }).fill('Labor');
  await page.getByLabel('Cost breakdown 1 amount', { exact: true }).fill('12.50');
  await page.getByRole('button', { name: 'Save work order', exact: true }).click();
  await expect(page.getByRole('dialog')).toHaveCount(0);
  expect(workWrites[1].notes).toBe('Replaced damaged wiring connector');
  expect(workWrites[1].expense.description).toBe(workWrites[1].notes);
  expect(workWrites[1].services).toEqual([service]);
  expect(workWrites[1].expense.costBreakdown).toEqual([{name:'Labor',amount:'12.50'}]);
  await page.setViewportSize({ width: 390, height: 844 });
  await workRow.click();
  await expect(page.getByLabel('Work order description')).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  await page.screenshot({ path: join(tmpdir(), 'mserp-work-order-description-mobile.png'), fullPage: true, animations: 'disabled' });
  await page.getByRole('button', { name: 'Close', exact: true }).click();
  await page.setViewportSize({ width: 1280, height: 1000 });
  await page.getByRole('row', { name: 'Open maintenance work-1', exact: true }).click();
  await expect(page.getByLabel('Service type', { exact: true })).toHaveValue('__saved');
  await page.getByRole('button', { name: 'Save work order', exact: true }).click();
  await expect(page.getByRole('dialog')).toHaveCount(0);
  expect(workWrites[2].services).toEqual(legacyServices);
  expect(errors).toEqual([]);
  console.log('Expense browser checks passed: ownership, AI entry, layout, verification attribution, Ctrl+Z and cancellation permissions.');
} finally { await browser?.close(); await new Promise(resolve => server.close(resolve)); }
