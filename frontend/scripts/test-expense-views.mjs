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
const settings = [{ id: 'maintenance', name: 'Maintenance', kind: 'category', active: true }];
const blankDraft = { company: 'Wrong company', categoryId: 'maintenance', expenseDate: '2026-09-28', truckId: 'investor-truck', driverId: 'hired', unitNumber: '101', driverName: 'Hired Driver', amount: '12.50', paymentType: '', expenseType: 'Repair', referenceNumber: '', description: '', coveredBy: 'Company', paidBy: 'Invented payer' };
let drafts = [blankDraft];
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
      '/auth/session': { user: { id: 'user', username: 'Entry User', expenseVerificationRole: verificationRole, permissions: ['expenses.read', 'fleet.read'], expenseCategoryAccess: [{ categoryId: 'maintenance', canRead: true, canCreate: true, canEdit: true, canDelete: true }] }, csrfToken: 'fixture' },
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
  await page.getByLabel('Name', { exact: true }).fill('Repair');
  await page.getByLabel('Amount', { exact: true }).fill('12.50');
  const transfer = await page.evaluateHandle(() => { const data = new DataTransfer(); data.items.add(new File(['receipt'], 'receipt.txt', { type: 'text/plain' })); return data; });
  await page.getByLabel('Expense details for AI').dispatchEvent('drop', { dataTransfer: transfer });
  await expect(page.getByText('receipt.txt', { exact: true })).toBeVisible();
  const browse = await page.getByRole('button', { name: 'Browse', exact: true }).boundingBox();
  const analyze = await page.getByRole('button', { name: 'Analyze', exact: true }).boundingBox();
  expect(Math.abs(browse.y - analyze.y)).toBeLessThan(3);
  await page.getByRole('button', { name: 'Analyze', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Analyze', exact: true })).toBeEnabled();
  await expect(page.getByLabel('Paid by', { exact: true })).toHaveValue('Entry User');
  await expect(page.getByLabel('Who will cover')).toHaveValue('Truck Owner');
  await page.screenshot({ path: join(tmpdir(), 'mserp-expense-modal.png'), fullPage: true });
  await page.getByRole('button', { name: 'Create entry', exact: true }).click();
  await expect.poll(() => writes.length).toBe(1);
  expect(writes[0]).not.toHaveProperty('company');
  expect(writes[0]).toMatchObject({ coveredBy: 'Truck Owner', ownerId: 'investor', paidBy: 'Entry User' });
  await page.getByRole('button', { name: 'Add entry', exact: true }).click();
  drafts = [blankDraft, { ...blankDraft, truckId: 'own-truck', driverId: 'owner-driver' }];
  await page.getByLabel('Expense details for AI').fill('two transactions');
  await page.getByRole('button', { name: 'Analyze', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Analyze', exact: true })).toBeEnabled();
  await expect(page.getByLabel('Transaction 1 responsibility')).toHaveValue('Truck Owner');
  await expect(page.getByLabel('Transaction 2 responsibility')).toHaveValue('Driver');
  await page.getByLabel('Transaction 2 paid by').selectOption('Other User');
  await page.getByRole('button', { name: 'Create 2 entries', exact: true }).click();
  await expect.poll(() => writes.length).toBe(2);
  expect(writes[1].expenses.every(item => !('company' in item))).toBe(true);
  expect(writes[1].expenses[0]).toMatchObject({ ownerId: 'investor', paidBy: 'Entry User' });
  expect(writes[1].expenses[1]).toMatchObject({ coveredBy: 'Driver', ownerId: null, paidBy: 'Other User' });
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
  expect(errors).toEqual([]);
  console.log('Expense browser checks passed: ownership, AI entry, layout, verification attribution, Ctrl+Z and cancellation permissions.');
} finally { await browser?.close(); await new Promise(resolve => server.close(resolve)); }
